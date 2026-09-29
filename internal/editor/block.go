package editor

import (
	"math"
	"strings"
)

// VISUAL BLOCK (§11): the selection is display columns on a run of lines,
// and an operator works on each line's part of it, as nvim's ops.c does
// for a blockwise oparg_T.

// blockKind is the register kind of a block, vim's CTRL-V.
const blockKind = '\x16'

// block is what a VISUAL BLOCK covers (ops.c get_op_vcol): lines top to
// bot, display columns left to right, both in. toEnd is after $, each line
// to its end; start is the upper left corner.
type block struct {
	top, bot, left, right int
	toEnd                 bool
	start                 Pos
}

// Block is the VISUAL BLOCK being selected: lines top to bottom, display
// columns left to right, both in; right is math.MaxInt after $, which
// takes every line to its end. ok is false outside V-BLOCK.
func (e *Editor) Block() (top, bottom, left, right int, ok bool) {
	if e.mode != VisualBlock {
		return 0, 0, 0, 0, false
	}
	b := e.opBlock()
	if b.toEnd {
		b.right = math.MaxInt
	}
	return b.top, b.bot, b.left, b.right, true
}

// charCols are the first and last display columns of the character at p;
// the end of a line is one column.
func (e *Editor) charCols(p Pos) (first, last int) {
	l := e.lines[p.Line]
	v := vcol(l, p.Col, e.TabWidth)
	if p.Col >= len(l) {
		return v, v
	}
	return v, v + width(l, p.Col, v, e.TabWidth) - 1
}

// spanCols are the columns the characters at p and q cover together
// (getvcols).
func (e *Editor) spanCols(p, q Pos) (left, right int) {
	f1, l1 := e.charCols(p)
	f2, l2 := e.charCols(q)
	return min(f1, f2), max(l1, l2)
}

// opBlock is the block VISUAL BLOCK has selected; the columns of both
// ends, or the longest line after $.
func (e *Editor) opBlock() block {
	from, to, _ := e.Selection()
	left, right := e.spanCols(from, to)
	b := block{top: from.Line, bot: to.Line, left: left, right: right, toEnd: e.want == wantEnd}
	if b.toEnd {
		b.right = 0
		for _, l := range e.lines[b.top : b.bot+1] {
			b.right = max(b.right, vcol(l, len(l), e.TabWidth))
		}
	}
	b.start = Pos{b.top, e.coladvance(b.top, b.left)}
	return b
}

// blockLine is how a block falls on one line (ops.c block_def): the bytes
// from textcol, textlen long, and the columns of a tab or wide character
// it cuts at either end, which become spaces.
type blockLine struct {
	textcol, textlen             int
	startspaces, endspaces       int
	startVcol, endVcol           int
	startCharVcols, endCharVcols int
	preWhitesp, preWhitespC      int // the blanks right before the block, in columns and characters
	short                        bool
}

// prep is ops.c block_prep for line n: del is whether the operator
// deletes the text; op matters for I, A, < and r.
func (e *Editor) prep(b block, n int, del bool, op string) blockLine {
	var bd blockLine
	l, ts := e.lines[n], e.TabWidth
	incr, col, vcol, prevStart := 0, 0, 0, 0
	for vcol < b.left && col < len(l) {
		incr = width(l, col, vcol, ts)
		vcol += incr
		if white(l[col]) {
			bd.preWhitesp += incr
			bd.preWhitespC++
		} else {
			bd.preWhitesp, bd.preWhitespC = 0, 0
		}
		prevStart, col = col, next(l, col)
	}
	bd.startVcol, bd.startCharVcols = vcol, incr
	start := col
	if bd.startVcol < b.left { // the line ends before the block
		bd.endVcol, bd.short = bd.startVcol, true
		if !del || op == "A" {
			bd.endspaces = b.right - b.left + 1
		}
		bd.textcol = start
		return bd
	}
	bd.startspaces = bd.startVcol - b.left
	if del && bd.startspaces != 0 {
		bd.startspaces = bd.startCharVcols - bd.startspaces
	}
	end := start
	bd.endVcol = bd.startVcol
	if bd.endVcol > b.right { // all in one character
		switch {
		case op == "I":
			bd.endspaces = bd.startCharVcols - bd.startspaces
		case op == "A":
			bd.startspaces += b.right - b.left + 1
			bd.endspaces = bd.startCharVcols - bd.startspaces
		default:
			bd.startspaces = b.right - b.left + 1
			if del && op != "<" {
				bd.startspaces = bd.startCharVcols - (bd.startVcol - b.left)
				bd.endspaces = bd.endVcol - b.right - 1
			}
		}
	} else {
		prevEnd := end
		vcol = bd.endVcol
		for vcol <= b.right && end < len(l) {
			prevEnd = end
			incr = width(l, end, vcol, ts)
			vcol += incr
			end = next(l, end)
		}
		bd.endVcol = vcol
		switch {
		case bd.endVcol <= b.right && (!del || op == "A" || op == "r"): // the line ends in the block
			bd.short = true
			if op == "A" {
				bd.endspaces = b.right - bd.endVcol + 1
			}
		case bd.endVcol > b.right:
			bd.endspaces = bd.endVcol - b.right - 1
			if !del && bd.endspaces != 0 {
				bd.endspaces = incr - bd.endspaces
				if end != start {
					end = prevEnd
				}
			}
		}
	}
	bd.endCharVcols = incr
	if del && bd.startspaces != 0 {
		start = prevStart
	}
	bd.textcol, bd.textlen = start, end-start
	return bd
}

func spaces(n int) string { return strings.Repeat(" ", max(n, 0)) }

// blockOp runs operator op on block b; the cursor is on its upper left
// corner (do_pending_operator).
func (e *Editor) blockOp(op string, b block, c cmd) {
	switch op {
	case "d":
		e.blockDelete(b, false)
	case "y":
		e.blockYank(b)
		e.eff.Yank = &Yank{From: Pos{b.top, 0}, To: Pos{b.bot, 0}, Block: true, Left: b.left, Right: b.right}
	case "c":
		e.blockChange(b)
	case "I", "A":
		e.blockInsert(b, op, c.n())
	case "r":
		e.blockReplace(b, c.arg)
	case "gu", "gU", "g~":
		e.blockCase(b, op)
	case ">", "<":
		e.blockShift(b, op, c.n())
	case "gc":
		e.comment(b.top, b.bot)
	}
}

// blockYank yanks the block (register.c op_yank_reg), what a tab or wide
// character it cuts covers of it as spaces.
func (e *Editor) blockYank(b block) {
	var ls []string
	for n := b.top; n <= b.bot; n++ {
		bd := e.prep(b, n, false, "y")
		ls = append(ls, spaces(bd.startspaces)+e.lines[n][bd.textcol:bd.textcol+bd.textlen]+spaces(bd.endspaces))
	}
	w := b.right - b.left + 1
	if b.toEnd && w > 1 {
		w--
	}
	e.reg = register{text: strings.Join(ls, "\n"), kind: blockKind, width: w}
	e.eff.Yanked = true
}

// blockDelete yanks the block and deletes it (ops.c op_delete); of a tab
// or wide character cut in two what is left of it stays as spaces. d on
// one empty line does nothing, not even yank; c goes on to type.
func (e *Editor) blockDelete(b block, change bool) {
	if !change && b.top == b.bot && e.lines[b.top] == "" {
		return
	}
	e.blockYank(b)
	e.beginChange()
	for n := b.top; n <= b.bot; n++ {
		bd := e.prep(b, n, true, "d")
		if bd.textlen == 0 {
			continue
		}
		if n == b.top {
			e.cur.Col = bd.textcol + bd.startspaces
		}
		l := e.lines[n]
		e.setLine(n, l[:bd.textcol]+spaces(bd.startspaces+bd.endspaces)+l[bd.textcol+bd.textlen:])
	}
}

// blockEdit is a block I, A or c being typed on its first line: esc types
// the same on the others (blockDone).
type blockEdit struct {
	b       block
	op      string
	first   blockLine // the first line's part as typing started
	preLen  int       // the length of the first line after the text typed goes
	textcol int       // c: where the text typed starts
	indent  int       // c: the first line's indent before typing
}

// blockChange deletes the block and types in its place (ops.c op_change).
func (e *Editor) blockChange(b block) {
	e.blockDelete(b, true)
	e.clampCursor() // op_delete's check_cursor_col
	if b.start.Col > e.cur.Col && e.line() != "" {
		e.inc(&e.cur)
	}
	l := e.lines[b.top]
	be := &blockEdit{b: b, op: "c", preLen: len(l), textcol: e.cur.Col, indent: nonBlank(l)}
	e.startInsert(Insert, cmd{name: "c"})
	e.ins.block = be
}

// blockInsert is I and A (ops.c op_insert): typing before or after the
// block on its first line; A first fills a short line up to the block's
// right edge, unless $ took it to the ends of the lines.
// ponytail: an arrow key before typing moves where nvim takes the block to
// start; here it is not followed.
func (e *Editor) blockInsert(b block, op string, count int) {
	bd := e.prep(b, b.top, true, op)
	be := &blockEdit{b: b, op: op, preLen: len(e.lines[b.top]) - bd.textcol}
	if op == "A" {
		be.preLen -= bd.textlen
		for e.cur.Col < len(e.line()) && e.cur.Col < bd.textcol+bd.textlen {
			e.cur.Col++
		}
		if bd.short && !b.toEnd {
			l := e.line()
			e.setLine(e.cur.Line, l[:e.cur.Col]+spaces(bd.endspaces)+l[e.cur.Col:])
			e.cur.Col += bd.endspaces
			bd.textlen += bd.endspaces
		}
	}
	be.first = bd
	e.startInsert(Insert, cmd{name: op, count: count})
	e.ins.block = be
}

// blockDone types what was typed on the first line of a block on the
// others, as esc ends INSERT: nothing when the cursor left the line.
func (e *Editor) blockDone(be *blockEdit) {
	b := be.b
	if be.op == "c" {
		first := e.lines[b.top]
		if be.textcol > be.indent { // typing past the indent: what BS took of it is no text typed
			d := nonBlank(first) - be.indent
			be.preLen, be.textcol = be.preLen+d, be.textcol+d
		}
		n := len(first) - be.preLen
		if b.top == b.bot || n <= 0 {
			return
		}
		text := first[be.textcol : be.textcol+n]
		for n := b.top + 1; n <= b.bot; n++ {
			if bd := e.prep(b, n, true, "c"); !bd.short {
				l := e.lines[n]
				e.setLine(n, l[:bd.textcol]+text+l[bd.textcol:])
			}
		}
		e.clampCursor()
		return
	}
	if e.cur.Line != b.top {
		return
	}
	bd, pre := be.first, be.preLen
	if bd2 := e.prep(b, b.top, true, be.op); !b.toEnd || bd2.textlen < bd.textlen {
		if be.op == "A" {
			pre += bd2.textlen - bd.textlen
			if bd2.endspaces != 0 {
				bd2.textlen--
			}
		}
		bd.textcol, bd.textlen = bd2.textcol, bd2.textlen
	}
	first := e.lines[b.top]
	add := bd.textcol
	if be.op == "A" {
		add += bd.textlen
	}
	add = min(add, len(first))
	n := len(first) - add - pre
	if pre < 0 || n <= 0 {
		return
	}
	if b.bot == b.top+1 { // block_insert's u_save is of that one line: U puts it back
		e.saveLine(b.bot, e.lines[b.bot], e.cur)
	}
	e.typeOnBlock(b, first[add:add+n], be.op)
	e.cur.Col = b.start.Col
	e.clampCursor()
}

// typeOnBlock puts s before (I) or after (A) the block on its lines below
// the first (ops.c block_insert): a short line gets I not at all, and A
// after spaces up to the block's edge; a tab the block cuts is split into
// spaces.
func (e *Editor) typeOnBlock(b block, s, op string) {
	before := op == "I"
	for n := b.top + 1; n <= b.bot; n++ {
		bd := e.prep(b, n, true, op)
		if bd.short && before {
			continue
		}
		l := e.lines[n]
		tsVal, pad, offset := 0, 0, 0
		switch {
		case before:
			tsVal, pad, offset = bd.startCharVcols, bd.startspaces, bd.textcol
		case !bd.short:
			tsVal = bd.endCharVcols
			if bd.endspaces != 0 {
				pad = tsVal - bd.endspaces
			}
			offset = bd.textcol + bd.textlen
			if pad != 0 {
				offset--
			}
		default:
			if !b.toEnd {
				pad = b.right - bd.endVcol + 1
			}
			offset = bd.textcol + bd.textlen
		}
		if pad > 0 {
			offset = head(l, offset)
		}
		rest, post := l[offset:], ""
		if pad > 0 && !bd.short && rest != "" && rest[0] == '\t' { // split the tab
			rest, post = rest[1:], spaces(tsVal-pad)
		}
		e.setLine(n, l[:offset]+spaces(pad)+s+post+rest)
	}
}

// blockReplace is r in VISUAL BLOCK (ops.c op_replace): every column of
// the block becomes ch, half as many for a wide one.
// ponytail: r<CR> breaks no lines here as it does in vim.
func (e *Editor) blockReplace(b block, ch string) {
	if ch == "\r" {
		return
	}
	e.beginChange()
	for n := b.top; n <= b.bot; n++ {
		bd := e.prep(b, n, true, "r")
		if bd.textlen == 0 {
			continue
		}
		numc := b.right - b.left + 1
		if bd.short {
			numc -= b.right - bd.endVcol + 1
		}
		if ch != "\t" && width(ch, 0, 0, e.TabWidth) > 1 { // a tab is one cell to utf_char2cells
			if numc%2 == 1 && !bd.short {
				bd.endspaces++
			}
			numc /= 2
		}
		l := e.lines[n]
		nl := l[:bd.textcol] + spaces(bd.startspaces) + strings.Repeat(ch, numc)
		if !bd.short {
			nl += spaces(bd.endspaces) + l[bd.textcol+bd.textlen:]
		}
		e.setLine(n, nl)
	}
	e.cur = b.start
	e.clampCursor()
}

// blockCase is ~ u U in VISUAL BLOCK (ops.c op_tilde).
func (e *Editor) blockCase(b block, op string) {
	e.beginChange()
	for n := b.top; n <= b.bot; n++ {
		bd := e.prep(b, n, false, op)
		l, end := e.lines[n], bd.textcol+bd.textlen
		if nl := l[:bd.textcol] + convertCase(l[bd.textcol:end], op) + l[end:]; nl != l {
			e.setLine(n, nl)
		}
	}
}

// blockShift is > and < in VISUAL BLOCK (ops.c op_shift, shift_block):
// the text from the block's left edge moves; < only takes blanks.
func (e *Editor) blockShift(b block, op string, amount int) {
	e.beginChange()
	col, ts, left := e.cur.Col, e.TabWidth, op == "<"
	for n := b.top; n <= b.bot; n++ {
		bd := e.prep(b, n, true, op)
		l := e.lines[n]
		if l == "" || bd.short {
			continue
		}
		total := amount * ts
		var nl string
		if !left {
			total += bd.preWhitesp
			text := bd.textcol
			if bd.startspaces != 0 {
				if next(l, text)-text == 1 {
					text++
				} else {
					bd.startspaces = 0
				}
			}
			for v := bd.startVcol; text < len(l) && white(l[text]); text++ {
				w := width(l, text, v, ts)
				total, v = total+w, v+w
			}
			pre := bd.preWhitespC
			if bd.startspaces != 0 {
				pre--
			}
			nl = l[:bd.textcol-pre] + spaces(total) + l[text:]
		} else {
			nonWhite, v := bd.textcol, bd.startVcol
			if bd.startspaces != 0 {
				nonWhite = next(l, nonWhite)
			}
			for ; nonWhite < len(l) && white(l[nonWhite]); nonWhite++ {
				v += width(l, nonWhite, v, ts)
			}
			dest := v - min(v-b.left, total)
			end, w := bd.textcol, bd.startVcol
			if bd.startspaces != 0 {
				w -= bd.startCharVcols
			}
			for w < dest {
				cw := width(l, end, w, ts)
				if w+cw > dest {
					break
				}
				w, end = w+cw, next(l, end)
			}
			nl = l[:end] + spaces(dest-w) + l[nonWhite:]
		}
		e.setLine(n, nl)
	}
	e.cur = Pos{b.top, col}
}

// putBlock is p (after) and P with a block in the register, count times
// side by side (register.c do_put): each of its lines goes in at the same
// display column on a line of its own, lines added past the end, a short
// line filled up with spaces.
func (e *Editor) putBlock(after bool, count int) {
	e.beginChange()
	if e.cur.Line == len(e.lines)-1 { // do_put's u_save is of this one line only here
		e.saveLine(e.cur.Line, e.line(), e.cur)
	}
	defer e.endChange() // U as saved, not as trackLine would take the change
	r, ts := e.reg, e.TabWidth
	l := e.line()
	col, _ := e.charCols(e.cur)
	if after && e.cur.Col < len(l) {
		_, last := e.charCols(e.cur)
		col = last + 1
		e.cur.Col = next(l, e.cur.Col)
	}
	top := e.cur.Line
	for i, y := range strings.Split(r.text, "\n") {
		n := top + i
		if n >= len(e.lines) {
			e.insertLines(n, "")
		}
		old := e.lines[n]
		v, c, incr := 0, 0, 0
		for v < col && c < len(old) {
			incr = width(old, c, v, ts)
			v, c = v+incr, next(old, c)
		}
		short := v < col || v == col && c >= len(old)
		startspaces, endspaces, del := 0, 0, 0
		switch {
		case v < col:
			startspaces = col - v
		case v > col: // in a tab: split it; another wide character moves on
			endspaces = v - col
			startspaces = incr - endspaces
			c = prev(old, c)
			del = 1
			if old[c] != '\t' {
				del, endspaces = 0, 0
			}
		}
		fill := r.width
		for j := 0; j < len(y); j = next(y, j) {
			fill -= width(y, j, 0, ts)
		}
		var b strings.Builder
		b.WriteString(old[:c] + spaces(startspaces))
		for j := range count {
			b.WriteString(y)
			if j < count-1 || !short { // no trailing blanks
				b.WriteString(spaces(fill))
			}
		}
		b.WriteString(spaces(endspaces) + old[c+del:])
		e.setLine(n, b.String())
		if i == 0 {
			e.cur.Col += startspaces
		}
	}
	e.cur.Line = top
}

// swapCorners is O in VISUAL BLOCK (normal.c v_swap_corners): the cursor
// goes to the other end of its line of the block, the other end of the
// selection to the other end of its own.
func (e *Editor) swapCorners() {
	old := e.cur
	left, right := e.spanCols(old, e.vstart)
	e.vstart.Col = e.coladvance(e.vstart.Line, left)
	e.cur.Col, e.want = e.coladvance(old.Line, right), right
	if e.cur.Col == old.Col {
		e.vstart.Col = e.coladvance(e.vstart.Line, right)
		e.cur.Col, e.want = e.coladvance(old.Line, left), left
	}
}
