// Package editor is the query's vim (tech-design §11): the text, the
// motions and commands, INSERT and REPLACE, undo. What it does is checked
// against nvim itself (testdata/cases.txt, §15). It draws nothing: the
// query is drawn by ui/query.go.
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
	Select // a snippet's tabstop selected: what is typed takes its place (F15.8)
)

// String is the name the status bar shows (§7.8).
func (m Mode) String() string {
	return [...]string{"NORMAL", "INSERT", "REPLACE", "VISUAL", "V-LINE", "COMMAND", "V-BLOCK", "SELECT"}[m]
}

// Effect is what a key did that the query acts on.
type Effect struct {
	Changed bool        // the text changed: save it, drop the failed ▶ (§11)
	Clip    *string     // what "+ (or "*) took: the system clipboard gets it (F3.38)
	Paste   *ClipPut    // a "+p waiting for the system clipboard: PutClip it (F3.38)
	Ex      string      // a : command the editor does not run itself (:w, :q)
	Error   string      // what went wrong, for a toast: 找不到：foo
	Format  *FormatSpan // gq asked to lay out this text, which the query does (§9.5)
	Yank    *Yank       // what a yank took, not a delete: the query flashes it (F3.32)
	// ReadOnly: a command that changes the text was not run, the editor
	// being ReadOnly.
	ReadOnly bool
}

// Yank is what a yank took, as vim's TextYankPost has it: From to To, To
// not in, charwise; their whole lines, Linewise; display columns Left to
// Right, both in, of their lines, Block.
type Yank struct {
	From, To        Pos
	Linewise, Block bool
	Left, Right     int
}

// FormatSpan is the text gq covers: From to To, To not in. Selected is a
// VISUAL selection, taken as it is; Current (gqq, gqgq) stands for the
// statement at the cursor; else a motion's, for the statements it touches.
type FormatSpan struct {
	From, To          Pos
	Selected, Current bool
}

const (
	wantUnset = -1          // take the column from the cursor when it is needed (vim's w_set_curswant)
	wantEnd   = math.MaxInt // after $: the end of every line
)

// Editor is one query's text and vim state. Keys come in keymap's
// notation ("a", "<Esc>", "<C-d>", "<Space>").
type Editor struct {
	TabWidth int // tabstop and shiftwidth; Tab inserts spaces (expandtab)
	// AutoPairs puts the other half of a bracket or quote typed in INSERT
	// (§7.9); nvim has no such thing, so its diff tests keep it off.
	AutoPairs bool
	// OneLine keeps the text one line, a WHERE's (F3.39): o, O, r<CR> and
	// the / ? : lines do nothing, and a put is charwise, newlines spaces.
	// The app runs INSERT's <CR> and turns a paste's newlines to spaces.
	OneLine bool
	// ReadOnly lets nothing change the text, a value shown but not to be
	// edited (F12.2): the commands that would are not run and say so in
	// Effect.ReadOnly; moving, searching, VISUAL and yanking go on.
	ReadOnly bool

	// plain is the modeless editing (SetPlain), anchor the end its selection
	// began at.
	plain  bool
	anchor *Pos

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

	yanked  bool // the command running put text in reg
	regClip bool // reg is what a "+ yank or delete took: vim's unnamed register points at "+
	// clipText is what the editor last wrote on the system clipboard, clipReg that
	// register with its type: nvim's provider cache (§11)
	clipText string
	clipReg  register

	vstart     Pos // where VISUAL started: the other end of the selection
	lastVisual visualArea

	cl         *cmdline // the / ? or : line being typed
	lastSearch search
	ver        int         // counts the changes of the text, for what is kept by it (Version)
	snip       *snippet    // the snippet being filled in, nil for none (F15.8)
	hl         bool        // the last search's matches are lit, until esc in NORMAL (hlsearch, F15.9)
	stat       *searchStat // what a search or n N left for the line under the text, until the next key

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

// Version changes with every change of the text, which a view that keeps what
// it worked out of the text goes by.
func (e *Editor) Version() int { return e.ver }
func (e *Editor) Cursor() Pos  { return e.cur }
func (e *Editor) Mode() Mode   { return e.mode }
func (e *Editor) Top() int     { return e.top }
func (e *Editor) Left() int    { return e.left }

// Pending is the command typed so far, as nvim's showcmd shows it.
func (e *Editor) Pending() string { return strings.Join(e.keys, "") }

// SetHeight tells how many lines the query shows: H M L, C-d and the
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
	e.eff, e.stat = Effect{}, nil
	e.snipAlive()
	switch {
	case e.plain:
		e.plainKey(k)
	case e.mode == Select:
		e.selectKey(k)
	case e.mode == Insert || e.mode == Replace:
		e.insertKey(k)
	case e.mode == Command:
		e.cmdKey(k)
		e.settle()
	default:
		if k == "<lt>" {
			k = "<"
		}
		e.keys = append(e.keys, k)
		e.normal()
	}
	e.snipAlive()
	e.scrollToCursor()
	if e.stat != nil && e.height > 1 { // the line of it is the text's last: the cursor is not on it
		h := e.height
		e.height--
		e.scrollToCursor()
		e.height = h
	}
	return e.eff
}

// cmd is one NORMAL command: [count] ["x] [count] operator [count]
// motion, or [count] ["x] [count] command.
type cmd struct {
	count int    // 0 when none was typed
	reg   string // the register "x names: "+ for the system clipboard, "" for the unnamed, else one not kept (§11)
	op    string // the operator waiting for the motion: "d", "gU"
	name  string // the motion, text object or command: "w", "iw", "gg", "f"
	arg   string // the character f, t, r take
	// implicit: no register was typed and `clipboard = "unnamedplus"` made it "+.
	// missing: the put found no clipboard to read (ClipPut.Missing).
	implicit, missing bool
}

// UnnamedPlus is nvim's `clipboard=unnamedplus`: a command with no register
// named takes "+, the system clipboard (§11, F20.5). Set once at start, from
// config.toml's `clipboard`.
// ponytail: one switch for the process, not per editor; upgrade if two configs
// ever live in one process.
var UnnamedPlus bool

// ClipPut is a "+p or "+P waiting for the system clipboard's text.
type ClipPut struct{ c cmd }

// Implicit is whether the put named no register, and `clipboard` made it "+.
func (p ClipPut) Implicit() bool { return p.c.implicit }

// Missing is p for a clipboard that could not be read, no tool or a failing
// one: PutClip puts the unnamed register then, as nvim does without a provider
// (an empty clipboard is no such thing: nothing is put).
func (p ClipPut) Missing() ClipPut { p.c.missing = true; return p }

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
	switch {
	case c.reg == `"`: // typed, the unnamed register stays the editor's whatever `clipboard` says (nvim)
		c.reg = ""
	case UnnamedPlus && c.reg == "":
		c.reg, c.implicit = "+", true
	}
	if c.reg == "_" && st == complete { // the black hole: the command runs, no register is touched, a put has nothing to put
		if c.name != "p" && c.name != "P" {
			kept, keptClip := e.reg, e.regClip
			e.run(c)
			e.reg, e.regClip = kept, keptClip
		}
	} else if st == complete && (c.reg == "" || c.reg == "+") { // a register of the others: none here (§11)
		e.yanked = false
		e.run(c)
		if e.yanked {
			e.regClip = c.reg == "+"
		}
		if c.reg == "+" && e.yanked { // the unnamed register too, as vim's
			clip := e.reg.text
			if e.reg.kind == blockKind { // as nvim's provider gets it: every line ends, the last too
				clip += "\n"
			}
			e.eff.Clip, e.clipText, e.clipReg = &clip, clip, e.reg
		}
	}
	e.settle()
}

// PutClip is the put p asked the clipboard for, text what it holds: put as
// a register charwise, or linewise when it ends in a newline, as vim takes
// the clipboard; under UnnamedPlus text the editor wrote itself last comes
// back with the type it had, a block still a block (nvim's provider cache);
// empty, nothing is put (vim's E353), but for a put p says is Missing, which
// puts the unnamed register; nor once the editor left NORMAL or has keys pending. The
// unnamed register stays as it was, but for one a "+ yank or delete filled:
// that stands for "+ in vim, and so holds what came, unless the put named no
// register, where nvim keeps its own.
func (e *Editor) PutClip(text string, p ClipPut) Effect {
	e.eff = Effect{}
	if e.mode != Normal || len(e.keys) > 0 { // come too late: the editor went on
		return e.eff
	}
	own := p.c.missing // no clipboard to read: the unnamed register as it is (nvim's fallback)
	kept := e.reg
	if !own {
		e.reg = register{text: text, kind: 'v'}
		switch {
		case UnnamedPlus && text == e.clipText: // nvim's provider uses its cache for the type only then
			e.reg = e.clipReg
		case text == "":
			e.reg = register{}
		case strings.HasSuffix(text, "\n"):
			e.reg.kind = 'V'
		}
	}
	p.c.reg = ""
	e.run(p.c)
	if !e.regClip || p.c.implicit { // nvim's unnamed register is not what a put of the clipboard read
		e.reg = kept
	}
	e.settle()
	e.scrollToCursor()
	return e.eff
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

// registers are the names vim's registers go by, besides " + and *.
const registers = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_/:.%#="

// withArg are the commands that take the character typed next.
var withArg = map[string]bool{"f": true, "F": true, "t": true, "T": true, "r": true}

// parse reads keys as vim's NORMAL and VISUAL modes do.
func parse(keys []string, visual bool) (c cmd, st status) {
	c.count, keys = count(keys)
	if len(keys) > 0 && keys[0] == `"` { // a register: "+ and "* the clipboard (§11), "" the unnamed
		if len(keys) == 1 {
			return c, waiting
		}
		switch r := keys[1]; {
		case r == "+" || r == "*":
			c.reg = "+"
		case r == `"`:
			c.reg = `"`
		case len(r) == 1 && strings.Contains(registers, r): // one of vim's others: read whole, not run (normal)
			c.reg = r
		default: // "<Esc>: no register, the command goes, as in vim
			return c, invalid
		}
		n, rest := count(keys[2:])
		if n > 0 {
			c.count = max(c.count, 1) * n
		}
		keys = rest
	}
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

// changes are the NORMAL commands that change the text; in VISUAL the
// visualCommands but those that only move the selection or take it.
var changes = map[string]bool{"i": true, "a": true, "I": true, "A": true, "o": true, "O": true, "R": true, "p": true, "P": true,
	"J": true, "~": true, "r": true, "u": true, "<C-r>": true, "U": true}

var visualMoves = map[string]bool{"<Esc>": true, "v": true, "V": true, "<C-v>": true, "<C-q>": true, "o": true, "O": true, "gv": true, "y": true, "Y": true}

// edits is whether c would change the text.
func (e *Editor) edits(c cmd) bool {
	switch sh, ok := shorthands[c.name]; {
	case c.op != "":
		return c.op != "y"
	case e.visual():
		return visualCommands[c.name] != nil && !visualMoves[c.name]
	case ok:
		return sh[0] != "y"
	}
	return changes[c.name]
}

func (e *Editor) run(c cmd) {
	if e.ReadOnly && e.edits(c) {
		e.eff.ReadOnly = true
		if e.visual() { // nvim ends VISUAL, the cursor at the start of what was selected
			from, _, _ := e.Selection()
			e.endVisual()
			e.cur, e.want = from, wantUnset
		}
		return
	}
	e.curswant()                                                                       // vim settles it before each command (update_topline_cursor)
	if c.reg == "+" && c.op == "" && (c.name == "p" || c.name == "P") && !e.visual() { // the text comes later: PutClip
		e.eff.Paste = &ClipPut{c}
		return
	}
	if e.OneLine && (c.name == "/" || c.name == "?" || c.name == ":" || !e.visual() && (c.name == "o" || c.name == "O")) {
		return
	}
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
		"<Esc>": func(e *Editor, _ cmd) { e.hl = false }, // :noh, which LazyVim has on esc (F15.9)
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
	if e.visual() || e.plain { // the end of the line is a place there
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
