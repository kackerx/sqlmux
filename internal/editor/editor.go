// Package editor is the console's vim (tech-design §11): the text, the
// motions and commands, INSERT and REPLACE, undo. What it does is checked
// against nvim itself (testdata/cases.txt, §15). It draws nothing: the
// console is drawn by ui/console.go.
package editor

//go:generate go test -run TestNvim -update

import (
	"math"
	"strconv"
	"strings"
)

// Pos is a place in the text: a line and a byte offset into it, both from
// 0 (nvim's getpos() counts from 1).
type Pos struct{ Line, Col int }

func (p Pos) less(q Pos) bool { return p.Line < q.Line || p.Line == q.Line && p.Col < q.Col }

// Mode is the editor's vim mode.
type Mode uint8

const (
	Normal Mode = iota
	Insert
	Replace
	Visual
	VisualLine
	Command // the / ? or : line is open
	VisualBlock
)

// String is the name the status bar shows (§7.8).
func (m Mode) String() string {
	return [...]string{"NORMAL", "INSERT", "REPLACE", "VISUAL", "V-LINE", "COMMAND", "V-BLOCK"}[m]
}

// Effect is what a key did that the console acts on.
type Effect struct {
	Changed bool   // the text changed: save it, drop the failed ▶ (§11)
	Yanked  bool   // the register changed: the clipboard gets it (§11)
	Ex      string // a : command the editor does not run itself (:w, :q)
	Error   string // what went wrong, for a toast: 找不到：foo
}

const (
	wantUnset = -1          // take the column from the cursor when it is needed (vim's w_set_curswant)
	wantEnd   = math.MaxInt // after $: the end of every line
)

// Editor is one console's text and vim state. Keys come in keymap's
// notation ("a", "<Esc>", "<C-d>", "<Space>").
type Editor struct {
	TabWidth int // tabstop and shiftwidth; Tab inserts spaces (expandtab)

	lines  []string
	cur    Pos
	want   int // the display column j and k aim for (curswant)
	mode   Mode
	height int // lines on screen; 0 before the first draw
	top    int // the first line on screen
	width  int // columns on screen for the text; 0 before the first draw
	left   int // the first display column on screen (leftcol)
	scroll int // how far C-d and C-u go, 0 for half the screen ('scroll')

	keys []string   // the NORMAL or VISUAL command typed so far
	ins  *insertion // the INSERT or REPLACE going on
	reg  register

	vstart     Pos // where VISUAL started: the other end of the selection
	lastVisual visualArea

	cl         *cmdline // the / ? or : line being typed
	lastSearch search

	done, undone []step
	snap         []string // the text before the change being made; nil when none is
	snapCur      Pos
	uLine        int // the line U restores, -1 for none, and what it restores
	uText        string
	uCol         int

	lastFind struct{ cmd, ch string } // the last f F t T, for ; and ,

	eff Effect
}

// New is an editor on text, the cursor at its start. One trailing newline
// ends the last line rather than starting another, as in a file.
func New(text string) *Editor {
	return &Editor{
		TabWidth: 2,
		lines:    strings.Split(strings.TrimSuffix(text, "\n"), "\n"),
		want:     wantUnset,
		uLine:    -1,
	}
}

func (e *Editor) Lines() []string { return e.lines }
func (e *Editor) Cursor() Pos     { return e.cur }
func (e *Editor) Mode() Mode      { return e.mode }
func (e *Editor) Top() int        { return e.top }
func (e *Editor) Left() int       { return e.left }

// Pending is the command typed so far, as nvim's showcmd shows it.
func (e *Editor) Pending() string { return strings.Join(e.keys, "") }

// SetHeight tells how many lines the console shows: H M L, C-d and the
// scrolling that keeps the cursor on screen depend on it.
func (e *Editor) SetHeight(n int) {
	e.height = n
	e.scrollToCursor()
}

// SetWidth tells how many columns the text has on screen: the view
// scrolls sideways to keep the cursor in it.
func (e *Editor) SetWidth(n int) {
	e.width = n
	e.scrollToCursor()
}

// Feed takes one key.
func (e *Editor) Feed(k string) Effect {
	e.eff = Effect{}
	switch e.mode {
	case Insert, Replace:
		e.insertKey(k)
	case Command:
		e.cmdKey(k)
		e.settle()
	default:
		if k == "<lt>" {
			k = "<"
		}
		e.keys = append(e.keys, k)
		e.normal()
	}
	e.scrollToCursor()
	return e.eff
}

// cmd is one NORMAL command: [count] operator [count] motion, or [count]
// command.
type cmd struct {
	count int    // 0 when none was typed
	op    string // the operator waiting for the motion: "d", "gU"
	name  string // the motion, text object or command: "w", "iw", "gg", "f"
	arg   string // the character f, t, r take
}

func (c cmd) n() int { return max(c.count, 1) }

type status uint8

const (
	complete status = iota
	waiting
	invalid
)

// normal runs the keys typed once they make a whole command.
func (e *Editor) normal() {
	c, st := parse(e.keys, e.visual())
	if st == waiting {
		return
	}
	e.keys = nil
	if st == complete {
		e.run(c)
	}
	e.settle()
}

// settle ends a command back in NORMAL or VISUAL: its change is one undo
// step, and the cursor is on the text.
func (e *Editor) settle() {
	if e.mode == Normal || e.visual() {
		e.trackLine()
		e.endChange()
		e.clampCursor()
	}
}

// withArg are the commands that take the character typed next.
var withArg = map[string]bool{"f": true, "F": true, "t": true, "T": true, "r": true}

// parse reads keys as vim's NORMAL and VISUAL modes do.
func parse(keys []string, visual bool) (c cmd, st status) {
	c.count, keys = count(keys)
	if c.name, c.arg, keys, st = word(keys, visual); st != complete {
		return c, st
	}
	if operators[c.name] && !visual {
		c.op = c.name
		n, keys := count(keys)
		if n > 0 {
			c.count = max(c.count, 1) * n
		}
		if c.name, c.arg, _, st = word(keys, true); st != complete {
			return c, st
		}
		// dd, gUU, gUgU: the line. gcc is a mapping of its own in nvim, which a
		// count does not split.
		// ponytail: gcgc is gc on nvim's o_gc text object, the block of
		// comment lines around the cursor; here it is the line, the same on
		// one line. Add o_gc (and dgc with it) to the objects if blocks of
		// comments are to be taken whole.
		if c.name == c.op || c.name == c.op[len(c.op)-1:] && (c.op != "gc" || n == 0) {
			c.name = "_"
			return c, complete
		}
		if motions[c.name] == nil && objects[c.name] == nil && c.name != "/" && c.name != "?" {
			return c, invalid
		}
		return c, complete
	}
	known := motions[c.name] != nil || commands[c.name] != nil || shorthands[c.name][0] != ""
	if visual {
		known = motions[c.name] != nil || objects[c.name] != nil || visualCommands[c.name] != nil
	}
	known = known || c.name == "/" || c.name == "?" || c.name == ":"
	if !known {
		return c, invalid
	}
	return c, complete
}

// word reads a command's name off keys: g and z take a second key, and so
// do i and a where they start a text object (textObj); f, t and r then
// take a character.
func word(keys []string, textObj bool) (name, arg string, rest []string, st status) {
	if len(keys) == 0 {
		return "", "", nil, waiting
	}
	name, keys = keys[0], keys[1:]
	if name == "g" || name == "z" || textObj && (name == "i" || name == "a") {
		if len(keys) == 0 {
			return "", "", nil, waiting
		}
		name, keys = name+keys[0], keys[1:]
	}
	if !withArg[name] {
		return name, "", keys, complete
	}
	if len(keys) == 0 {
		return "", "", nil, waiting
	}
	switch arg = keyText(keys[0]); keys[0] {
	case "<Tab>":
		arg = "\t"
	case "<CR>":
		arg = "\r"
	}
	if arg == "" {
		return "", "", nil, invalid
	}
	return name, arg, keys[1:], complete
}

// count reads a count off the front of keys: a lone 0 is a motion.
func count(keys []string) (int, []string) {
	n := 0
	for len(keys) > 0 && len(keys[0]) == 1 && keys[0] >= "0" && keys[0] <= "9" && (n > 0 || keys[0] != "0") {
		d, _ := strconv.Atoi(keys[0])
		n, keys = min(n*10+d, 99999999), keys[1:]
	}
	return n, keys
}

func (e *Editor) run(c cmd) {
	e.curswant() // vim settles it before each command (update_topline_cursor)
	if c.name == "/" || c.name == "?" || c.name == ":" && c.op == "" {
		e.openCmdline(c.name, c)
		return
	}
	if c.op != "" {
		e.operate(c)
		return
	}
	if e.visual() {
		if f := visualCommands[c.name]; f != nil {
			ok := !blockOnly[c.name]
			if e.mode == VisualBlock {
				ok = inBlock[c.name]
			}
			if ok {
				f(e, c)
			}
			return
		}
		if o := objects[c.name]; o != nil {
			if e.mode == VisualBlock { // no text objects in a block (§11)
				return
			}
			start, vstart := e.cur, e.vstart
			if _, ok := o(e, c.n(), c.name[0] == 'a'); !ok {
				e.cur, e.vstart = start, vstart
			}
			e.want = wantUnset
			return
		}
	} else if sh, ok := shorthands[c.name]; ok {
		e.operate(cmd{count: c.count, op: sh[0], name: sh[1]})
		return
	}
	if m := motions[c.name]; m != nil {
		t := m(e, c, "")
		e.cur = t.to
		if !t.keepWant {
			e.want = wantUnset
		}
		return
	}
	commands[c.name](e, c)
	if resetWant[c.name] {
		e.want = wantUnset
	}
}

// resetWant are the commands after which j and k aim for the cursor's
// column again (vim sets w_set_curswant).
var resetWant = map[string]bool{"u": true, "<C-r>": true, "U": true, "J": true, "r": true, "~": true, "p": true, "P": true}

// commands are the NORMAL commands that are not motions.
var commands map[string]func(*Editor, cmd)

func init() {
	commands = map[string]func(*Editor, cmd){
		"i": func(e *Editor, c cmd) { e.startInsert(Insert, c) },
		"a": func(e *Editor, c cmd) {
			if l := e.line(); l != "" {
				e.cur.Col = next(l, e.cur.Col)
			}
			e.startInsert(Insert, c)
		},
		"I": func(e *Editor, c cmd) {
			e.cur.Col = nonBlank(e.line())
			e.startInsert(Insert, c)
		},
		"A": func(e *Editor, c cmd) {
			e.cur.Col = len(e.line())
			e.startInsert(Insert, c)
		},
		"o": func(e *Editor, c cmd) { e.openLine(1, c) },
		"O": func(e *Editor, c cmd) { e.openLine(0, c) },
		"R": func(e *Editor, c cmd) { e.startInsert(Replace, c) },

		"p": func(e *Editor, c cmd) { e.put(true, c.n()) },
		"P": func(e *Editor, c cmd) { e.put(false, c.n()) },
		"J": func(e *Editor, c cmd) {
			n := max(c.count, 2)
			if left := len(e.lines) - e.cur.Line; n > left {
				if n == 2 {
					return
				}
				n = left
			}
			e.join(n)
		},
		"~":     func(e *Editor, c cmd) { e.tilde(c.n()) },
		"r":     func(e *Editor, c cmd) { e.replace(c) },
		"v":     func(e *Editor, c cmd) { e.startVisual(Visual, c.count) },
		"V":     func(e *Editor, c cmd) { e.startVisual(VisualLine, c.count) },
		"<C-v>": func(e *Editor, c cmd) { e.startVisual(VisualBlock, c.count) },
		"<C-q>": func(e *Editor, c cmd) { e.startVisual(VisualBlock, c.count) }, // as in nvim: where the terminal takes C-v
		"gv":    func(e *Editor, _ cmd) { e.reselect() },

		"u":     func(e *Editor, c cmd) { e.undo(c.n()) },
		"<C-r>": func(e *Editor, c cmd) { e.redo(c.n()) },
		"U":     func(e *Editor, _ cmd) { e.undoLine() },

		"<C-d>": func(e *Editor, c cmd) { e.halfPage(1, c.count) },
		"<C-u>": func(e *Editor, c cmd) { e.halfPage(-1, c.count) },
		"<C-f>": func(e *Editor, c cmd) { e.page(1, c.n()) },
		"<C-b>": func(e *Editor, c cmd) { e.page(-1, c.n()) },
		"zt":    func(e *Editor, c cmd) { e.zet(c, e.scrollTop) },
		"zz":    func(e *Editor, c cmd) { e.zet(c, func() { e.halfway(true, false) }) },
		"zb":    func(e *Editor, c cmd) { e.zet(c, e.scrollBottom) },
		"<Esc>": func(*Editor, cmd) {},
	}
}

// line is the cursor's line.
func (e *Editor) line() string { return e.lines[e.cur.Line] }

// clampCursor keeps a NORMAL cursor on a character: the last one at most;
// in VISUAL it may be on the end of the line.
func (e *Editor) clampCursor() {
	e.cur.Line = min(max(e.cur.Line, 0), len(e.lines)-1)
	l := e.line()
	end := last(l)
	if e.visual() {
		end = len(l)
	}
	e.cur.Col = head(l, min(max(e.cur.Col, 0), end))
}

// CursorCol is the display column the cursor is drawn at, from the start
// of its line: the end of a tab in NORMAL, as in vim.
func (e *Editor) CursorCol() int { return e.cursorVcol() }

// cursorVcol is the display column of the cursor as curswant takes it: the
// end of a tab in NORMAL, where vim draws the cursor on it.
func (e *Editor) cursorVcol() int {
	l, col := e.line(), e.cur.Col
	v := vcol(l, col, e.TabWidth)
	if col < len(l) && l[col] == '\t' && (e.mode == Normal || e.visual() && e.vstart.less(e.cur)) {
		v += width(l, col, v, e.TabWidth) - 1
	}
	return v
}

// curswant is the display column j and k aim for.
func (e *Editor) curswant() int {
	if e.want == wantUnset {
		e.want = e.cursorVcol()
	}
	return e.want
}

// coladvance is the offset in line n of the character at display column
// want: the last one when the line is shorter, or its end in INSERT.
func (e *Editor) coladvance(n, want int) int {
	l := e.lines[n]
	if want == wantEnd {
		if e.mode == Normal {
			return last(l)
		}
		return len(l)
	}
	v, col := 0, 0
	for col < len(l) {
		w := width(l, col, v, e.TabWidth)
		if v+w > want {
			return col
		}
		v, col = v+w, next(l, col)
	}
	if e.mode == Normal {
		return last(l)
	}
	return len(l)
}
