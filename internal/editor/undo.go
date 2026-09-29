package editor

import (
	"slices"
	"strings"
)

// step is one undoable change: a NORMAL command, or an INSERT up to its
// esc or its first arrow key (§11). The text is kept whole on both sides;
// lines that did not change share their strings.
// ponytail: a snapshot per step copies the line slice, fine for SQL files;
// a file of some MB would want line-range entries like vim's.
type step struct {
	before, after []string
	cur           Pos // the cursor as the change started (vim's uh_cursor)
}

// The text changes only through setLine, insertLines, deleteLines and
// joinNext, which open a step when none is open. The ends of the last
// VISUAL (gv, '<,'>) follow the lines they are on, as vim's marks do
// (mark.c mark_adjust, mark_col_adjust).

func (e *Editor) setLine(n int, s string) {
	e.beginChange()
	e.eff.Changed = true
	e.lines[n] = s
}

func (e *Editor) insertLines(at int, ls ...string) {
	e.beginChange()
	e.eff.Changed = true
	e.lines = slices.Insert(e.lines, at, ls...)
	e.moveMarks(at, at, len(ls))
}

// deleteLines deletes lines [from, to); the text keeps one line at least.
func (e *Editor) deleteLines(from, to int) {
	e.beginChange()
	e.eff.Changed = true
	e.lines = slices.Delete(e.lines, from, to)
	if len(e.lines) == 0 {
		e.lines = []string{""}
	}
	e.moveMarks(from, to, 0)
}

// joinNext puts line n+1 on the end of line n, after sep and without its
// first skip bytes (ops.c do_join); marks on it go along (col_adjust): one
// in the blanks that go stays where sep starts, or at 0 when the blanks
// were longer than line n.
func (e *Editor) joinNext(n int, sep string, skip int) {
	head := e.lines[n]
	removed := skip - len(sep)
	amount := len(head) - removed
	for _, p := range e.marks() {
		if p.Line != n+1 {
			continue
		}
		switch {
		case amount < 0 && p.Col <= -amount:
			*p = Pos{n, 0}
		case p.Col < removed:
			*p = Pos{n, len(head)}
		default:
			*p = Pos{n, p.Col + amount}
		}
	}
	e.setLine(n, head+sep+e.lines[n+1][skip:])
	e.deleteLines(n+1, n+2)
}

func (e *Editor) marks() [2]*Pos { return [2]*Pos{&e.lastVisual.start, &e.lastVisual.end} }

// moveMarks is lines [from, to) replaced by n lines: a mark on them goes
// to the first, one after them moves along (one_adjust_nodel).
func (e *Editor) moveMarks(from, to, n int) {
	for _, p := range e.marks() {
		switch {
		case p.Line >= to:
			p.Line += n - (to - from)
		case p.Line >= from:
			p.Line = from
		}
	}
}

// beginChange opens a step, keeping the text and the cursor as they are
// before the change: commands call it before they move the cursor.
func (e *Editor) beginChange() {
	if e.snap == nil {
		e.snap, e.snapCur = slices.Clone(e.lines), e.cur
	}
}

// endChange closes the step being made. A command that saved for undo
// but changed nothing (x on an empty line) is a step too, as in vim.
func (e *Editor) endChange() {
	before := e.snap
	if before == nil {
		return
	}
	e.snap = nil
	e.done = append(e.done, step{before, slices.Clone(e.lines), e.snapCur})
	e.undone = nil
}

// trackLine keeps what U restores after what a NORMAL command did (undo.c
// u_saveline): a change within one line saves the line as it was, unless
// it is the line saved already; adding lines or only deleting them leaves
// nothing to restore; changes over several lines (J) leave it as it is.
// INSERT saves the line where typing starts (saveLine).
func (e *Editor) trackLine() {
	if e.snap == nil {
		return
	}
	p, oldN, newN := diff(e.snap, e.lines)
	switch {
	case oldN == 1 && newN == 1:
		e.saveLine(p, e.snap[p], e.snapCur)
	case newN > oldN || newN == 0 && oldN > 0:
		e.uLine = -1
	}
}

// saveLine keeps text as what U puts back on line n, unless n is kept
// already; the cursor goes back to its column if it was on n.
func (e *Editor) saveLine(n int, text string, cur Pos) {
	if n == e.uLine {
		return
	}
	e.uLine, e.uText, e.uCol = n, text, 0
	if cur.Line == n {
		e.uCol = cur.Col
	}
}

// diff is where a and b differ: from line p, oldN lines of a became newN
// lines of b.
func diff(a, b []string) (p, oldN, newN int) {
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	return p, len(a) - p - s, len(b) - p - s
}

func (e *Editor) undo(n int) {
	for ; n > 0 && len(e.done) > 0; n-- {
		s := e.done[len(e.done)-1]
		e.done = e.done[:len(e.done)-1]
		e.undone = append(e.undone, s)
		e.restore(s.before, s.cur)
	}
}

func (e *Editor) redo(n int) {
	for ; n > 0 && len(e.undone) > 0; n-- {
		s := e.undone[len(e.undone)-1]
		e.undone = e.undone[:len(e.undone)-1]
		e.done = append(e.done, s)
		e.restore(s.after, s.cur)
	}
}

// restore puts text back and the cursor where vim's u_undoredo puts it:
// where the change started when that is in the changed lines, else on the
// first changed line.
func (e *Editor) restore(text []string, saved Pos) {
	p, oldN, newN := diff(e.lines, text)
	e.lines = slices.Clone(text)
	e.eff.Changed = true
	// ponytail: the marks move with the lines, but vim also puts back the
	// selection the step saved (uh_visual), which gv then selects; add it if
	// gv after u is missed.
	if oldN != newN { // u_undoredo calls mark_adjust only then
		e.moveMarks(p, p+oldN, newN)
	}
	cur := Pos{Line: p}
	// a step that changed nothing saved the cursor's line
	if top := p; oldN+newN == 0 || saved.Line+1 >= top && saved.Line+1 <= top+newN+1 {
		cur = saved
	}
	if saved.Line+1 == cur.Line && cur.Line > 0 { // only one line off: where the change started (o)
		cur.Line--
	}
	switch {
	case cur.Line >= len(e.lines):
		cur = Pos{len(e.lines) - 1, 0}
	case saved.Line == cur.Line:
		cur.Col = saved.Col
	default:
		cur.Col = e.coladvance(cur.Line, e.want)
	}
	e.cur = cur
}

// undoLine is U: the last line changed goes back to how it was before its
// latest changes; a second U puts them back.
func (e *Editor) undoLine() {
	if e.uLine < 0 || e.uLine >= len(e.lines) {
		return
	}
	n := e.uLine
	e.beginChange()
	e.lines[n], e.uText = e.uText, e.lines[n]
	col := e.uCol
	if e.cur.Line == n {
		e.uCol = e.cur.Col
	}
	e.cur = Pos{n, col}
	e.endChange() // a step of its own, which u undoes; the U line stays
}

// Load puts text in place of the whole text, as reading the file again
// after another editor changed it: one undo step, in NORMAL, the cursor
// where it was as far as the new text allows.
func (e *Editor) Load(text string) Effect {
	e.eff = Effect{}
	switch {
	case e.cl != nil:
		e.closeCmdline()
	case e.ins != nil:
		e.escape()
	}
	if e.visual() {
		e.endVisual()
	}
	e.keys = nil
	if ls := strings.Split(strings.TrimSuffix(text, "\n"), "\n"); !slices.Equal(ls, e.lines) {
		e.beginChange()
		e.eff.Changed = true
		e.lines, e.uLine = ls, -1
		e.clampCursor()
		e.endChange()
	}
	e.scrollToCursor()
	return e.eff
}
