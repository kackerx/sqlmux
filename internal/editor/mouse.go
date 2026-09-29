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
	// ponytail: a pending operator goes; nvim applies it up to the click
	// (d, a click, deletes to there). Do that if anyone asks for it.
	e.keys = nil
	n = min(max(n, 0), len(e.lines)-1)
	was := e.cur
	e.cur, e.want = Pos{n, e.coladvance(n, vcol)}, vcol
	e.insMoved(was)
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
	e.scrollToCursor()
}

// Scroll moves the view n lines down, up for a negative n, as the wheel
// does: until the last line is at the top at most; the cursor stays in view.
// Pending keys go, and with an operator among them the view stays
// (nv_scroll_line's checkclearop).
func (e *Editor) Scroll(n int) {
	if e.mode == Command {
		return
	}
	c, _ := parse(e.keys, e.visual())
	if e.keys = nil; c.op != "" {
		return
	}
	was := e.cur
	if n > 0 {
		e.scrollUp(n)
	} else if n < 0 {
		e.scrollDown(-n)
	}
	e.scrollSideways()
	e.insMoved(was)
}
