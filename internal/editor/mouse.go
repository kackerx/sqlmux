package editor

// The mouse (tech-design §11, F3.6): the console turns a click, a drag or
// the wheel over its text into a line and a display column.

// Click puts the cursor where a click on line n at display column vcol
// does (normal.c jump_to_mouse): on the character there, the last one of
// a shorter line, and j and k then aim for vcol. VISUAL ends; INSERT goes
// on from there as after an arrow key.
func (e *Editor) Click(n, vcol int) {
	if e.mode == Command {
		return
	}
	if e.visual() {
		e.endVisual()
	}
	e.keys = nil
	n = min(max(n, 0), len(e.lines)-1)
	if e.ins != nil && n != e.cur.Line {
		e.dropIndent() // edit.c stop_insert, when leaving the line
	}
	e.cur, e.want = Pos{n, e.coladvance(n, vcol)}, vcol
	if e.ins != nil {
		e.jumped()
	}
	e.scrollToCursor()
}

// Drag selects from where the button went down, where Click put the
// cursor, to line n at display column vcol, in VISUAL. An INSERT ends
// first: nvim would select in "(insert) VISUAL", which the editor lacks.
func (e *Editor) Drag(n, vcol int) {
	if e.mode == Command {
		return
	}
	if !e.visual() {
		if at := e.cur; e.ins != nil {
			e.escape()
			e.cur = at
		}
		e.keys = nil
		e.startVisual(Visual, 0)
	}
	n = min(max(n, 0), len(e.lines)-1)
	e.cur, e.want = Pos{n, e.coladvance(n, vcol)}, vcol
	e.clampCursor()
	e.scrollToCursor()
}

// Scroll moves the view n lines down, up for a negative n, as the wheel
// does: until the last line is at the top at most; the cursor stays in view.
func (e *Editor) Scroll(n int) {
	switch {
	case e.mode == Command:
		return
	case n > 0:
		e.scrollUp(n)
	case n < 0:
		e.scrollDown(-n)
	}
	e.scrollSideways()
}
