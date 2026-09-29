package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// insertion is an INSERT or REPLACE going on (nvim's edit.c).
type insertion struct {
	cmd      string     // what started it: "i", "o", "R"...
	count    int        // 3ix<Esc> types x three times
	keys     []string   // what was typed, for the count
	start    Pos        // where typing started: C-w and C-u stop there (Insstart_orig)
	fresh    bool       // nothing typed yet, or not since an arrow key: the next change saves the line for U
	arrowed  bool       // an arrow key moved the cursor: no count, and a new undo step
	ai       bool       // the line's indent came from autoindent and nothing is typed after it (did_ai)
	replaced []string   // REPLACE: what each character typed replaced, "" for one added
	block    *blockEdit // a VISUAL BLOCK I, A or c
}

func (e *Editor) startInsert(m Mode, c cmd) {
	e.trackLine() // what the command did before typing (cw deletes)
	e.mode, e.want = m, wantUnset
	e.ins = &insertion{cmd: c.name, count: c.n(), start: e.cur, fresh: true}
}

// openLine is o (below 1) and O: a new line with the indent of the
// cursor's, in INSERT on it.
func (e *Editor) openLine(below int, c cmd) {
	indent := e.indentOf(e.line())
	e.insertLines(e.cur.Line+below, indent)
	e.cur = Pos{e.cur.Line + below, len(indent)}
	e.startInsert(Insert, c)
	e.ins.ai = true
}

// indentOf is the indent autoindent gives the line after l: as wide as
// l's, in spaces ('expandtab').
func (e *Editor) indentOf(l string) string {
	return strings.Repeat(" ", vcol(l, nonBlank(l), e.TabWidth))
}

// insertKey takes a key typed in INSERT or REPLACE. nvim maps C-w and C-u
// to <C-G>u first: what was typed so far is an undo step of its own. M-BS
// is C-w (§7.9).
func (e *Editor) insertKey(k string) {
	if k == "<M-BS>" {
		k = "<C-w>"
	}
	if k == "<C-w>" || k == "<C-u>" {
		e.endChange()
		e.ins.fresh = true
	}
	e.editKey(k)
}

// editKey does what key k does in INSERT or REPLACE, unmapped. The keys
// that did something are kept for a count to type again (edit.c keeps them
// in the redo buffer, which is not mapped).
func (e *Editor) editKey(k string) {
	in := e.ins
	e.curswant()
	did := true
	switch k {
	case "<Esc>":
		e.escape()
		return
	case "<CR>":
		e.newline()
	case "<BS>", "<C-h>":
		l, c := e.line(), e.cur.Col
		pair := e.AutoPairs && e.mode == Insert && EmptyPair(l[:c], l[c:])
		if did = e.backspace(bsChar); did && pair { // the closing half goes too
			l := e.line()
			e.setLine(e.cur.Line, l[:e.cur.Col]+l[e.cur.Col+1:])
		}
	case "<C-w>", "<C-u>":
		mode := bsWord
		if k == "<C-u>" {
			mode = bsLine
		}
		did = e.backspace(mode)
	case "<Del>":
		e.del()
	case "<Tab>":
		v := vcol(e.line(), e.cur.Col, e.TabWidth)
		e.typeText(strings.Repeat(" ", e.TabWidth-v%e.TabWidth))
	case "<Left>", "<Right>", "<Up>", "<Down>":
		e.arrow(k)
		return
	default:
		if t, ok := strings.CutPrefix(k, pasteKey); ok {
			want := wantUnset // do_put sets curswant
			if e.mode == Replace {
				want = e.want // vim.paste sets REPLACE's lines, which leaves it
			}
			e.putText(strings.Split(t, "\n"))
			in.keys = append(in.keys, k)
			e.want = want
			return
		}
		t := keyText(k)
		if did = t != ""; did {
			e.typePaired(t)
		}
	}
	if did {
		in.keys = append(in.keys, k)
	}
	e.want = wantUnset
}

// Complete puts text in place of the cursor's line from col start to the
// cursor, in INSERT, as typing it would: the cursor after it, part of the
// INSERT's undo step, kept with the keys as a BS a character taken back
// and text put in, for a count to do again (a completion taken, §9.7;
// nvim keeps it in the redo buffer so).
func (e *Editor) Complete(start int, text string) Effect {
	e.eff = Effect{}
	if e.mode != Insert || start > e.cur.Col {
		return e.eff
	}
	e.arrived()
	l := e.line()
	for i := start; i < e.cur.Col; i = next(l, i) {
		e.ins.keys = append(e.ins.keys, "<BS>")
	}
	e.ins.keys = append(e.ins.keys, pasteKey+text)
	e.setLine(e.cur.Line, l[:start]+text+l[e.cur.Col:])
	e.cur.Col, e.want = start+len(text), wantUnset
	e.ins.ai = false
	e.scrollToCursor()
	return e.eff
}

// arrived is called as INSERT is about to change the text, before the
// cursor moves (edit.c stop_arrow): typing starts anew where an arrow key
// left the cursor, and the first change of a run keeps the line for U.
func (e *Editor) arrived() {
	in := e.ins
	e.beginChange()
	if in.arrowed {
		in.start, in.arrowed = e.cur, false
	}
	if in.fresh {
		e.saveLine(e.cur.Line, e.line(), e.cur)
		in.fresh = false
	}
}

// typePaired types t, with AutoPairs in INSERT: an opening half gets its
// other after the cursor, a closing one there is stepped over (§7.9).
func (e *Editor) typePaired(t string) {
	if r, n := utf8.DecodeRuneInString(t); e.AutoPairs && e.mode == Insert && n == len(t) {
		l, c := e.line(), e.cur.Col
		switch close, skip := Pair(l[:c], l[c:], r); {
		case skip:
			e.cur.Col += n
			e.ins.ai = false
			return
		case close != "":
			e.typeText(t + close)
			e.cur.Col -= len(close)
			return
		}
	}
	e.typeText(t)
}

// closers are the halves autopairs puts after an opening one (§7.9).
var closers = map[rune]string{'(': ")", '[': "]", '{': "}", '\'': "'", '"': `"`, '`': "`"}

// Pair is what typing r does with autopairs (§7.9), before and after
// being the text on each side of the cursor: skip when r is a closing
// half or a quote the cursor is right before, which it steps over; close
// is the other half to put after the cursor when r opens a pair there:
// with the end, a blank or a closing bracket after it, and for a quote
// no letter, digit or the same quote before it: don't, and a quote
// typed twice to escape it.
func Pair(before, after string, r rune) (close string, skip bool) {
	next, _ := utf8.DecodeRuneInString(after)
	if next == r && strings.ContainsRune(")]}'\"`", r) {
		return "", true
	}
	close, ok := closers[r]
	if !ok || after != "" && !unicode.IsSpace(next) && !strings.ContainsRune(")]}", next) {
		return "", false
	}
	if p, _ := utf8.DecodeLastRuneInString(before); close == string(r) && before != "" && (unicode.IsLetter(p) || unicode.IsDigit(p) || p == r) {
		return "", false
	}
	return close, false
}

// EmptyPair reports whether the cursor is between the halves of an empty
// pair, which BS takes both of with autopairs (§7.9).
func EmptyPair(before, after string) bool {
	p, _ := utf8.DecodeLastRuneInString(before)
	close, ok := closers[p]
	return ok && before != "" && strings.HasPrefix(after, close)
}

func (e *Editor) typeText(t string) {
	e.arrived()
	l, c := e.line(), e.cur.Col
	if e.mode == Replace {
		// the first character typed replaces one; the rest of a tab's
		// spaces go in (edit.c ins_tab)
		end := next(l, c)
		e.ins.replaced = append(e.ins.replaced, l[c:end])
		for i := next(t, 0); i < len(t); i = next(t, i) {
			e.ins.replaced = append(e.ins.replaced, "")
		}
		e.setLine(e.cur.Line, l[:c]+t+l[end:])
	} else {
		e.setLine(e.cur.Line, l[:c]+t+l[c:])
	}
	e.cur.Col += len(t)
	e.ins.ai = false
}

// newline splits the line at the cursor (change.c open_line): the new
// line gets the indent of the part before it, and the blanks the cursor
// was before go; an indent nothing was typed after is taken off the line
// left behind.
func (e *Editor) newline() {
	e.arrived()
	l, c := e.line(), e.cur.Col
	before, after := l[:c], strings.TrimLeft(l[c:], " \t")
	indent := e.indentOf(before)
	if e.ins.ai {
		before = strings.TrimRight(before, " \t")
	}
	e.setLine(e.cur.Line, before)
	e.insertLines(e.cur.Line+1, indent+after)
	e.cur = Pos{e.cur.Line + 1, len(indent)}
	e.ins.ai = true
	e.uLine = -1 // no U after adding a line (change.c open_line)
}

const (
	bsChar = iota
	bsWord
	bsLine
	bsWordNotSpace
)

// backspace is BS, C-w and C-u (edit.c ins_bs), with backspace=
// indent,eol,start and 'smarttab': in the indent BS goes back a
// shiftwidth; C-w and C-u stop where typing started, then go on. It is
// false at the start of the text, where there is nothing to delete.
func (e *Editor) backspace(mode int) bool {
	in := e.ins
	if e.cur.Line == 0 && e.cur.Col == 0 {
		return false
	}
	e.arrived()
	if e.cur.Col == 0 { // join with the line above
		startLine := in.start.Line
		if startLine == e.cur.Line {
			in.start = Pos{e.cur.Line - 1, len(e.lines[e.cur.Line-1])}
		}
		if e.mode == Replace && e.cur.Line <= startLine {
			e.dec(&e.cur)
		} else {
			above := e.lines[e.cur.Line-1]
			e.joinNext(e.cur.Line-1, "", 0)
			e.cur = Pos{e.cur.Line - 1, len(above)}
		}
		in.ai = false
		return true
	}
	l := e.line()
	mincol := 0
	if mode == bsLine {
		if nb := nonBlank(l); nb < e.cur.Col {
			mincol = nb
		}
	}
	if mode == bsChar && nonBlank(l) >= e.cur.Col { // in the indent: back to a multiple of shiftwidth
		v := vcol(l, e.cur.Col, e.TabWidth)
		want := max(v-1, 0)
		want -= want % e.TabWidth
		col, sv := 0, 0 // the start of the blanks before the cursor
		for i, cv := 0, 0; i < e.cur.Col; i = next(l, i) {
			if (i == 0 || !white(l[i-1])) && white(l[i]) {
				col, sv = i, cv
			}
			cv += width(l, i, cv, e.TabWidth)
		}
		for sv+width(l, col, sv, e.TabWidth) <= want {
			sv += width(l, col, sv, e.TabWidth)
			col = next(l, col)
		}
		for e.cur.Col > col {
			e.cur.Col = prev(e.line(), e.cur.Col)
			e.deleteChar()
		}
	} else {
		cls := e.cls(e.cur, false)
		word := false
		for {
			e.cur.Col = prev(e.line(), e.cur.Col)
			prevCls := cls
			cls = e.cls(e.cur, false)
			space := isSpace(rune(e.line()[e.cur.Col]))
			if mode == bsWord && !space {
				mode, word = bsWordNotSpace, cls >= 2
			} else if mode == bsWordNotSpace && (space || (cls >= 2) != word || prevCls != cls) {
				e.cur.Col = next(e.line(), e.cur.Col)
				break
			}
			e.deleteChar()
			if mode == bsChar || e.cur.Col <= mincol || e.cur == in.start {
				break
			}
		}
	}
	if e.cur.Col <= 1 {
		in.ai = false
	}
	if e.cur.Line == in.start.Line && e.cur.Col < in.start.Col {
		in.start.Col = e.cur.Col
	}
	return true
}

func isSpace(r rune) bool { return r == ' ' || r >= '\t' && r <= '\r' }

// deleteChar deletes the character at the cursor, or in REPLACE puts back
// the one typed over there.
func (e *Editor) deleteChar() {
	l, c := e.line(), e.cur.Col
	if e.mode == Replace {
		rs := e.ins.replaced
		if len(rs) == 0 {
			return
		}
		was := rs[len(rs)-1]
		e.ins.replaced = rs[:len(rs)-1]
		e.setLine(e.cur.Line, l[:c]+was+l[next(l, c):])
		return
	}
	e.setLine(e.cur.Line, l[:c]+l[next(l, c):])
}

// del is <Del>: the character at the cursor, or the line break at the end.
func (e *Editor) del() {
	e.arrived()
	l := e.line()
	switch {
	case e.cur.Col < len(l):
		e.setLine(e.cur.Line, l[:e.cur.Col]+l[next(l, e.cur.Col):])
	case e.cur.Line < len(e.lines)-1:
		e.joinNext(e.cur.Line, "", 0)
	}
	e.ins.ai = false
}

// arrow moves the cursor in INSERT, which ends the change so far.
func (e *Editor) arrow(k string) {
	was, l := e.cur, e.line()
	switch k {
	case "<Left>":
		if e.cur.Col > 0 {
			e.cur.Col, e.want = prev(l, e.cur.Col), wantUnset
		}
	case "<Right>":
		if e.cur.Col < len(l) {
			e.cur.Col, e.want = next(l, e.cur.Col), wantUnset
		}
	case "<Up>", "<Down>":
		n := e.cur.Line + 1
		if k == "<Up>" {
			n -= 2
		}
		if n >= 0 && n < len(e.lines) {
			e.cur = Pos{n, e.coladvance(n, e.want)}
		}
	}
	e.insMoved(was)
}

// insMoved ends the INSERT's change so far when an arrow key or the mouse
// moved the cursor off was (edit.c start_arrow, which ins_mouse and
// ins_mousescroll call only when the cursor moved): undo takes back what
// is typed after it separately, no count repeats, and an indent nothing
// was typed after goes from the line left.
func (e *Editor) insMoved(was Pos) {
	if e.ins == nil || e.cur == was {
		return
	}
	if e.cur.Line != was.Line {
		at := e.cur
		e.cur = was
		e.dropIndent()
		e.cur = at
	}
	in := e.ins
	in.arrowed, in.fresh, in.ai, in.keys, in.count = true, true, false, nil, 1
	e.endChange()
}

// dropIndent takes off the blanks at the cursor when they are an indent
// nothing was typed after (edit.c stop_insert).
func (e *Editor) dropIndent() {
	if !e.ins.ai {
		return
	}
	l, c := e.line(), e.cur.Col
	was := c
	for {
		if c >= len(l) && c > 0 {
			c = prev(l, c)
		}
		if c >= len(l) || !white(l[c]) {
			break
		}
		l = l[:c] + l[c+1:]
	}
	if c < was && c < len(l) && next(l, c) >= len(l) {
		c++ // back on the end of the line
	}
	if l != e.line() {
		e.setLine(e.cur.Line, l)
	}
	e.cur.Col = c
}

// escape ends INSERT: what was typed goes in count-1 times more, an indent
// nothing was typed after goes, and the cursor steps back onto the last
// character typed.
func (e *Editor) escape() {
	in := e.ins
	if !in.arrowed {
		keys := in.keys
		for i := 1; i < in.count; i++ {
			if in.cmd == "o" || in.cmd == "O" {
				e.newline()
			}
			for _, k := range keys {
				e.editKey(k)
			}
		}
		e.dropIndent()
	}
	e.mode, e.ins = Normal, nil
	if e.cur.Col > 0 {
		e.cur.Col = prev(e.line(), e.cur.Col)
	}
	e.want = wantUnset
	if in.block != nil {
		e.blockDone(in.block)
	}
	e.endChange()
}
