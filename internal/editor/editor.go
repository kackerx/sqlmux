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
)

// String is the name the status bar shows (§7.8).
func (m Mode) String() string { return [...]string{"NORMAL", "INSERT", "REPLACE"}[m] }

// Effect is what a key did that the console acts on.
type Effect struct {
	Changed bool // the text changed: save it, drop the failed ▶ (§11)
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
	scroll int // how far C-d and C-u go, 0 for half the screen ('scroll')

	keys []string   // the NORMAL command typed so far
	ins  *insertion // the INSERT or REPLACE going on

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

// Pending is the command typed so far, as nvim's showcmd shows it.
func (e *Editor) Pending() string { return strings.Join(e.keys, "") }

// SetHeight tells how many lines the console shows: H M L, C-d and the
// scrolling that keeps the cursor on screen depend on it.
func (e *Editor) SetHeight(n int) {
	e.height = n
	e.scrollToCursor()
}

// Feed takes one key.
func (e *Editor) Feed(k string) Effect {
	e.eff = Effect{}
	switch e.mode {
	case Insert, Replace:
		e.insertKey(k)
	default:
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
	name  string // the motion or command: "w", "gg", "f"
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
	c, st := parse(e.keys)
	if st == waiting {
		return
	}
	e.keys = nil
	if st == complete {
		e.run(c)
	}
	if e.mode == Normal {
		e.trackLine()
		e.endChange()
		e.clampCursor()
	}
}

// withArg are the commands that take the character typed next.
var withArg = map[string]bool{"f": true, "F": true, "t": true, "T": true, "r": true}

// parse reads keys as vim's NORMAL mode does.
func parse(keys []string) (c cmd, st status) {
	c.count, keys = count(keys)
	if len(keys) == 0 {
		return c, waiting
	}
	c.name, keys = keys[0], keys[1:]
	if c.name == "g" || c.name == "z" {
		if len(keys) == 0 {
			return c, waiting
		}
		c.name, keys = c.name+keys[0], keys[1:]
	}
	if withArg[c.name] {
		if len(keys) == 0 {
			return c, waiting
		}
		if c.arg = keyText(keys[0]); keys[0] == "<Tab>" {
			c.arg = "\t"
		}
		if c.arg == "" {
			return c, invalid
		}
	}
	if motions[c.name] == nil && commands[c.name] == nil {
		return c, invalid
	}
	return c, complete
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
	if m := motions[c.name]; m != nil {
		t := m(e, c, "")
		e.cur = t.to
		if !t.keepWant {
			e.want = wantUnset
		}
		return
	}
	commands[c.name](e, c)
}

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

// clampCursor keeps a NORMAL cursor on a character: the last one at most.
func (e *Editor) clampCursor() {
	e.cur.Line = min(max(e.cur.Line, 0), len(e.lines)-1)
	l := e.line()
	e.cur.Col = head(l, min(max(e.cur.Col, 0), last(l)))
}

// cursorVcol is the display column of the cursor as curswant takes it: the
// end of a tab in NORMAL, where vim draws the cursor on it.
func (e *Editor) cursorVcol() int {
	l, col := e.line(), e.cur.Col
	v := vcol(l, col, e.TabWidth)
	if col < len(l) && l[col] == '\t' && e.mode == Normal {
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
