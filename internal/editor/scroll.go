package editor

// Scrolling follows nvim's move.c for a window without folds or wrapping,
// where every line is one row, and 'scrolloff' 0, 'scrolljump' 1.

// rows is how many lines the console shows; the whole text before the
// first draw.
func (e *Editor) rows() int {
	if e.height > 0 {
		return e.height
	}
	return len(e.lines)
}

// scrollToCursor brings the cursor on screen after a command, sideways
// too (update_topline): a line or so away scrolls just enough, further
// away puts it in the middle.
func (e *Editor) scrollToCursor() {
	e.scrollSideways()
	h, n := e.height, len(e.lines)
	if h <= 0 {
		return
	}
	e.top = min(e.top, n-1)
	if n == 1 && e.lines[0] == "" {
		e.top = 0
		return
	}
	cur := e.cur.Line
	if e.top > 0 && cur < e.top {
		if e.top-cur >= max(h/2-1, 2) {
			e.halfway(false, false)
			return
		}
		e.top = cur
	}
	if bot := e.top + h; bot < n && cur >= bot {
		if cur-bot+1 <= h+1 {
			e.scrollBottom1()
		} else {
			e.halfway(false, false)
		}
	}
}

// scrollBottom1 scrolls the cursor, below the screen, up to its bottom
// (scroll_cursor_bot with set_topbot false); too far off it goes to the
// middle instead. Line numbers are from 1 here, as in vim.
func (e *Editor) scrollBottom1() {
	h, n := e.height, len(e.lines)
	cln, topline := e.cur.Line+1, e.top+1
	botline := min(topline+h, n+1)
	empty := max(topline+h-(n+1), 0)
	used, scrolled := 1, 0
	if cln >= botline {
		scrolled = used
		if cln == botline {
			scrolled -= empty
		}
	}
	loff, boff := cln, cln
	for loff > 1 {
		if loff <= botline {
			break
		}
		loff--
		if used++; used > h {
			break
		}
		if loff >= botline {
			scrolled++
			if loff == botline {
				scrolled -= empty
			}
		}
		if boff < n {
			boff++
			if used++; used > h {
				break
			}
			if scrolled < 1 {
				if boff >= botline {
					scrolled++
					if boff == botline {
						scrolled -= empty
					}
				}
			}
		}
	}
	lines := max(scrolled, 0) // every line is one row
	if used > h && scrolled > 0 {
		lines = used
	}
	if lines >= h && lines > 1 {
		e.halfway(false, true)
	} else if lines > 0 {
		e.top = min(e.top+lines, n-1)
	}
}

// halfway puts the cursor line in the middle of the screen
// (scroll_cursor_halfway): lines are added below and above it in turn.
// With atend (zz) the rows past the end of the text count as lines too.
func (e *Editor) halfway(atend, preferAbove bool) {
	h, n := e.height, len(e.lines)
	loff, boff := e.cur.Line, e.cur.Line
	used, top := 1, loff
	below := func() bool {
		if boff < n-1 {
			boff++
			used++
		} else if atend {
			used++
		}
		return used > h
	}
	above := func() bool {
		loff--
		used++
		if used > h {
			return true
		}
		top = loff
		return false
	}
	for top > 0 {
		first, second := below, above
		if preferAbove {
			first, second = above, below
		}
		if first() || second() {
			break
		}
	}
	e.top = top
}

// scrollTop is zt: the cursor line at the top.
func (e *Editor) scrollTop() { e.top = e.cur.Line }

// scrollBottom is zb: the cursor line at the bottom.
func (e *Editor) scrollBottom() { e.top = max(e.cur.Line-e.rows()+1, 0) }

// zet runs zt, zz or zb, after going to the count's line if one was typed.
func (e *Editor) zet(c cmd, f func()) {
	if c.count > 0 {
		e.cur.Line = min(c.count, len(e.lines)) - 1
		e.clampCursor()
	}
	if e.height > 0 {
		f()
	}
}

// scrollUp shows lines further down (scrollup); the cursor stays on
// screen.
func (e *Editor) scrollUp(k int) {
	e.top = min(e.top+k, len(e.lines)-1)
	if e.cur.Line < e.top {
		e.cur.Line = e.top
		e.cur.Col = e.coladvance(e.top, e.curswant())
	}
}

// scrollDown shows lines further up (scrolldown); the cursor stays on
// screen.
func (e *Editor) scrollDown(k int) {
	e.top = max(e.top-k, 0)
	if bottom := e.top + e.rows() - 1; e.cur.Line > bottom {
		e.cur.Line = bottom
		e.cur.Col = e.coladvance(bottom, e.curswant())
	}
}

// halfPage is C-d (dir 1) and C-u: scroll by 'scroll' lines, half the
// screen unless a count set it, and move the cursor as many (move.c
// pagescroll). C-d stops short of showing rows past the end.
func (e *Editor) halfPage(dir, count int) {
	h, n := e.rows(), len(e.lines)
	prevCur := e.cur
	if count > 0 {
		e.scroll = min(h, count)
	}
	k := max(h/2, 1)
	if e.scroll > 0 {
		k = e.scroll
	}
	moves := k
	if dir > 0 && e.top+1+h+k > n {
		if left := n - e.top; left < h+k {
			k = left - h
		}
	}
	top := e.top
	if k > 0 {
		if dir > 0 {
			e.scrollUp(k)
		} else {
			e.scrollDown(k)
		}
		e.cur = prevCur
	}
	if dir > 0 {
		e.cur.Line = min(e.cur.Line+moves, n-1)
	} else {
		e.cur.Line = max(e.cur.Line-moves, 0)
	}
	if e.top == top && e.cur == prevCur {
		return
	}
	e.cur.Col = e.coladvance(e.cur.Line, e.want)
}

// page is C-f (dir 1) and C-b: a screen less two lines of overlap, the
// cursor to the top or the bottom of the new screen.
func (e *Editor) page(dir, count int) {
	h, n := e.rows(), len(e.lines)
	top := e.top
	e.top = max(min(e.top+dir*count*e.overlap(dir), n-1), 0)
	if e.top == top {
		return
	}
	if dir > 0 {
		e.cur.Line = e.top
	} else {
		e.cur.Line = min(e.top+h, n) - 1
	}
	e.cur.Col = e.coladvance(e.cur.Line, e.want)
}

// overlap is how far C-f and C-b scroll (get_scroll_overlap): two lines
// fewer than the screen, keeping two lines of the last one in view, but a
// whole screen once the end in that direction shows.
func (e *Editor) overlap(dir int) int {
	h, n := e.rows(), len(e.lines)
	if dir < 0 && e.top == 0 || dir > 0 && e.top+h >= n {
		return h
	}
	// the lines from the edge of the screen inwards: 1 row each, or none
	// past an end of the text
	s := e.top - 1
	if dir > 0 {
		s = e.top + h
	}
	rows := func(k int) int {
		if i := s - dir*k; i < 0 || i >= n {
			return 1 << 30
		}
		return 1
	}
	h1, h2, h3, h4, minH := rows(0), rows(1), rows(2), rows(3), h-2
	switch {
	case h1 > minH, h2+h1 > minH, h3+h2 > minH:
		return h
	case h4+h3+h2 > minH, h3+h2+h1 > minH:
		return minH + 1
	}
	return minH
}

// scrollSideways keeps the cursor's character on screen as nvim does with
// 'sidescroll' 1 and 'sidescrolloff' 0 (move.c curs_columns): a few
// columns off scrolls just enough, half the width or more puts the cursor
// in the middle.
func (e *Editor) scrollSideways() {
	w := e.width
	if w <= 0 {
		return
	}
	start, end := e.charCols(e.cur)
	offLeft, offRight := start-e.left, end-e.left-w+1
	if offLeft >= 0 && offRight <= 0 {
		return
	}
	diff := offRight
	if offLeft < 0 {
		diff = -offLeft
	}
	switch {
	case diff >= w/2 || offRight >= offLeft: // far off, or wider than the screen
		e.left = e.cursorVcol() - w/2
	case offLeft < 0:
		e.left -= diff
	default:
		e.left += diff
	}
	e.left = max(e.left, 0)
}
