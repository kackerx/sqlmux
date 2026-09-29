package editor

// VISUAL and VISUAL LINE (§11): motions and text objects move the cursor
// end of the selection, operators work on it.

func (e *Editor) visual() bool { return e.mode == Visual || e.mode == VisualLine }

// Selection is what VISUAL has selected, from one end to the other in text
// order, both ends in; whole lines in V-LINE. ok is false outside VISUAL.
func (e *Editor) Selection() (from, to Pos, ok bool) {
	if !e.visual() {
		return Pos{}, Pos{}, false
	}
	from, to = e.vstart, e.cur
	if to.less(from) {
		from, to = to, from
	}
	return from, to, true
}

// startVisual starts VISUAL; a count selects that many characters or
// lines (nv_visual).
// ponytail: after an operator on a selection vim's count reselects that
// many times its size instead; add it if anyone relies on it.
func (e *Editor) startVisual(m Mode, count int) {
	e.vstart, e.mode = e.cur, m
	if count > 1 {
		move := right
		if m == VisualLine {
			move = vertical(1)
		}
		t := move(e, cmd{count: count - 1}, "")
		e.cur = t.to
		if !t.keepWant {
			e.want = wantUnset
		}
	}
}

// endVisual leaves VISUAL, keeping the selection for gv.
func (e *Editor) endVisual() {
	e.lastVisual = visualArea{e.vstart, e.cur, e.mode, e.want}
	e.mode = Normal
}

type visualArea struct {
	start, end Pos
	mode       Mode // 0 before any VISUAL
	want       int
}

// gv selects what VISUAL selected last; in VISUAL the two swap.
func (e *Editor) reselect() {
	last := e.lastVisual
	if last.mode == Normal {
		return
	}
	if e.visual() {
		e.lastVisual = visualArea{e.vstart, e.cur, e.mode, e.want}
	}
	clamp := func(p Pos) Pos {
		p.Line = min(p.Line, len(e.lines)-1)
		p.Col = min(p.Col, len(e.lines[p.Line]))
		return p
	}
	e.vstart, e.cur, e.mode, e.want = clamp(last.start), clamp(last.end), last.mode, last.want
}

// selection is the span an operator takes from VISUAL (do_pending_operator):
// ending on the end of a line takes the line break in, except for the
// operators on whole lines. In V-LINE it starts at column 0, or where the
// cursor is when that is the upper end.
func (e *Editor) selection(op string) span {
	from, to, _ := e.Selection()
	s := span{start: from, end: to, inclusive: true, visual: true}
	if e.mode == VisualLine {
		s.linewise = true
		if v := (Pos{e.vstart.Line, 0}); v.less(e.cur) {
			s.start = v
		}
		return s
	}
	if to.Col >= len(e.lines[to.Line]) {
		s.inclusive = false
		if op != ">" && op != "<" && op != "J" && op != "gc" && to.Line < len(e.lines)-1 {
			s.end = Pos{to.Line + 1, 0}
		}
	}
	return s
}

// visualOp runs an operator on the selection and leaves VISUAL.
func (e *Editor) visualOp(op string, amount int) {
	want := e.want
	s := e.selection(op)
	e.endVisual()
	e.apply(op, s, amount, want)
}

var visualCommands map[string]func(*Editor, cmd)

func init() {
	switchTo := func(m Mode) func(*Editor, cmd) {
		return func(e *Editor, _ cmd) {
			if e.mode == m {
				e.endVisual()
			} else {
				e.mode = m
			}
		}
	}
	op := func(name string) func(*Editor, cmd) {
		return func(e *Editor, c cmd) { e.visualOp(name, c.n()) }
	}
	swap := func(e *Editor, _ cmd) {
		e.vstart, e.cur = e.cur, e.vstart
		e.want = wantUnset
	}
	visualCommands = map[string]func(*Editor, cmd){
		"<Esc>": func(e *Editor, _ cmd) { e.endVisual() },
		"v":     switchTo(Visual),
		"V":     switchTo(VisualLine),
		"o":     swap,
		"O":     swap,
		"gv":    func(e *Editor, _ cmd) { e.reselect() },
		"d":     op("d"), "x": op("d"),
		"c": op("c"), "s": op("c"),
		"y": op("y"),
		">": op(">"), "<": op("<"),
		"u": op("gu"), "U": op("gU"), "~": op("g~"),
		"J":  op("J"),
		"gc": op("gc"),
	}
}
