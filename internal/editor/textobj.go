package editor

import "strings"

// An object is a text object (iw, a(, ip...): with an operator it gives
// the text to work on; in VISUAL it moves the selection instead
// (textobject.c current_word, current_block, current_quote, current_par).
// include is a (an object with its blanks or brackets), else i.
type object func(e *Editor, count int, include bool) (span, bool)

var objects map[string]object

func init() {
	objects = map[string]object{}
	for _, ia := range []string{"i", "a"} {
		for k, o := range map[string]object{
			"w": wordObject(false), "W": wordObject(true),
			"(": blockObject('(', ')'), ")": blockObject('(', ')'), "b": blockObject('(', ')'),
			"[": blockObject('[', ']'), "]": blockObject('[', ']'),
			"{": blockObject('{', '}'), "}": blockObject('{', '}'), "B": blockObject('{', '}'),
			"'": quoteObject('\''), `"`: quoteObject('"'), "`": quoteObject('`'),
			"p": paraObject,
		} {
			objects[ia+k] = o
		}
	}
}

// incl and decl are inc and dec that do not stop on the end of a
// non-empty line.
func (e *Editor) incl(p *Pos) int {
	r := e.inc(p)
	if r >= 1 && p.Col > 0 {
		r = e.inc(p)
	}
	return r
}

func (e *Editor) decl(p *Pos) int {
	r := e.dec(p)
	if r == 1 && p.Col > 0 {
		r = e.dec(p)
	}
	return r
}

func (e *Editor) oneleft() bool {
	if e.cur.Col == 0 {
		return false
	}
	e.cur.Col = prev(e.line(), e.cur.Col)
	return true
}

// backInLine moves the cursor back to the start of the word or the blanks
// it is in, within its line.
func (e *Editor) backInLine(big bool) {
	c := e.cls(e.cur, big)
	for e.cur.Col > 0 {
		p := e.cur
		e.dec(&e.cur)
		if e.cls(e.cur, big) != c {
			e.cur = p
			break
		}
	}
}

func wordObject(big bool) object {
	return func(e *Editor, count int, include bool) (span, bool) {
		visual := e.visual()
		inclusive, white := true, false
		var start Pos
		if !visual || e.cur == e.vstart {
			e.backInLine(big)
			start = e.cur
			if (e.cls(e.cur, big) == 0) == include {
				if !e.end(&e.cur, 1, big, true, true) {
					return span{}, false
				}
			} else {
				e.fwd(&e.cur, 1, big, true)
				if e.cur.Col == 0 {
					e.decl(&e.cur)
				} else {
					e.oneleft()
				}
				white = include
			}
			if visual {
				e.vstart = start
			}
			count--
		}
		for ; count > 0; count-- {
			inclusive = true
			if visual && e.cur.less(e.vstart) {
				if e.decl(&e.cur) == -1 {
					return span{}, false
				}
				if include != (e.cls(e.cur, big) != 0) {
					if !e.bck(&e.cur, 1, big, true) {
						return span{}, false
					}
				} else {
					if !e.bckend(&e.cur, 1, big, true) {
						return span{}, false
					}
					e.incl(&e.cur)
				}
				continue
			}
			if e.incl(&e.cur) == -1 {
				return span{}, false
			}
			if include != (e.cls(e.cur, big) == 0) {
				if !e.fwd(&e.cur, 1, big, true) && count > 1 {
					return span{}, false
				}
				if !e.oneleft() {
					inclusive = false
				}
			} else if !e.end(&e.cur, 1, big, true, true) {
				return span{}, false
			}
		}
		// aw on a word with no blanks after it takes the blanks before
		if white && (e.cls(e.cur, big) != 0 || e.cur.Col == 0 && !inclusive) {
			end := e.cur
			e.cur = start
			if e.oneleft() {
				e.backInLine(big)
				if e.cls(e.cur, big) == 0 && e.cur.Col > 0 {
					if start = e.cur; visual {
						e.vstart = e.cur
					}
				}
			}
			e.cur = end
		}
		if visual {
			if e.mode == VisualLine {
				e.mode = Visual
			}
			return span{}, true
		}
		return span{start: start, end: e.cur, inclusive: inclusive}, true
	}
}

// unmatched finds, from p but not at it, the bracket find that the
// brackets between do not match: other opens (or closes) one more level.
// Quotes do not count, as in current_block.
func (e *Editor) unmatched(p Pos, find, other byte, dir int) (Pos, bool) {
	step, depth := e.inc, 0
	if dir < 0 {
		step = e.dec
	}
	for step(&p) != -1 {
		l := e.lines[p.Line]
		if p.Col >= len(l) {
			continue
		}
		switch l[p.Col] {
		case find:
			if depth == 0 {
				return p, true
			}
			depth--
		case other:
			depth++
		}
	}
	return p, false
}

func blockObject(open, shut byte) object {
	return func(e *Editor, count int, include bool) (span, bool) {
		old := e.cur
		oldStart, oldEnd := e.cur, e.cur
		visual := e.visual()
		fail := func() (span, bool) { e.cur = old; return span{}, false }
		switch {
		case !visual || e.cur == e.vstart:
			if open == '{' { // ignore the indent
				for e.inIndent(1) && e.inc(&e.cur) == 0 {
				}
			}
			if l := e.line(); e.cur.Col < len(l) && l[e.cur.Col] == open {
				e.cur.Col++
			}
		case e.vstart.less(e.cur):
			oldStart, e.cur = e.vstart, e.vstart
		default:
			oldEnd = e.vstart
		}
		var start Pos
		ok := false
		if _, ok = e.unmatched(e.cur, open, shut, -1); ok {
			for ; count > 0; count-- {
				if e.cur, ok = e.unmatched(e.cur, open, shut, -1); !ok {
					break
				}
				start = e.cur
			}
		} else { // not in a block: the next one on
			for ; count > 0; count-- {
				if e.cur, ok = e.unmatched(e.cur, open, shut, 1); !ok {
					break
				}
				start = e.cur
			}
		}
		if !ok {
			return fail()
		}
		end, ok := e.unmatched(e.cur, shut, open, 1)
		if !ok {
			return fail()
		}
		e.cur = end
		sol := false // the end bracket is at the start of its line, after its indent at most
		for !include {
			e.incl(&start)
			sol = e.cur.Col == 0
			e.decl(&e.cur)
			for e.inIndent(1) {
				sol = true
				if e.decl(&e.cur) != 0 {
					break
				}
			}
			if start == end && visual { // nothing inside
				return fail()
			}
			if !start.less(oldStart) && !oldEnd.less(e.cur) && start != e.cur && visual {
				// no bigger than the selection: the block around it
				e.cur = oldStart
				e.decl(&e.cur)
				if start, ok = e.unmatched(e.cur, open, shut, -1); !ok {
					return fail()
				}
				e.cur = start
				if end, ok = e.unmatched(e.cur, shut, open, 1); !ok {
					return fail()
				}
				e.cur = end
				continue
			}
			break
		}
		if visual {
			if sol && e.cur.Col < len(e.line()) {
				e.inc(&e.cur) // the line break too
			}
			e.vstart, e.mode = start, Visual
			return span{}, true
		}
		s := span{start: start, end: e.cur}
		switch {
		case sol:
			e.incl(&s.end)
		case !e.cur.less(start):
			s.inclusive = true
		default: // nothing between the brackets
			s.end = start
		}
		return s, true
	}
}

// inIndent is whether the cursor is within the line's indent, extra
// columns before its end (misc inindent).
func (e *Editor) inIndent(extra int) bool {
	return nonBlank(e.line()) >= e.cur.Col+extra
}

func nextQuote(l string, col int, q byte, escape bool) int {
	for col < len(l) {
		switch {
		case escape && l[col] == '\\':
			if col++; col >= len(l) {
				return -1
			}
		case l[col] == q:
			return col
		}
		col = next(l, col)
	}
	return -1
}

func prevQuote(l string, col int, q byte, escape bool) int {
	for col > 0 {
		col = prev(l, col)
		n := 0
		for escape && col-n > 0 && l[col-n-1] == '\\' {
			n++
		}
		if n&1 == 1 {
			col -= n
		} else if l[col] == q {
			break
		}
	}
	return col
}

func quoteObject(q byte) object {
	return func(e *Editor, count int, include bool) (span, bool) {
		l := e.line()
		at := func(i int) byte {
			if i < len(l) {
				return l[i]
			}
			return 0
		}
		colStart, colEnd := e.cur.Col, 0
		inclusive, visEmpty, visBefore, inside, selectedQuote := false, true, false, false, false
		visual := e.visual()
		if visual {
			if e.vstart.Line != e.cur.Line {
				return span{}, false
			}
			visBefore, visEmpty = e.vstart.less(e.cur), e.vstart == e.cur
		}
		if !visEmpty {
			var i int
			if visBefore {
				inside = e.vstart.Col > 0 && l[e.vstart.Col-1] == q && at(e.cur.Col) != 0 && at(e.cur.Col+1) == q
				i, colEnd = e.vstart.Col, e.cur.Col
			} else {
				inside = e.cur.Col > 0 && l[e.cur.Col-1] == q && at(e.vstart.Col) != 0 && at(e.vstart.Col+1) == q
				i, colEnd = e.cur.Col, e.vstart.Col
			}
			for ; i <= colEnd && i < len(l); i++ {
				if l[i] == q {
					selectedQuote = true
					break
				}
			}
		}
		switch {
		case !visEmpty && at(colStart) == q:
			if visBefore {
				if colStart = nextQuote(l, colStart+1, q, false); colStart < 0 {
					return span{}, false
				}
				if colEnd = nextQuote(l, colStart+1, q, true); colEnd < 0 {
					colEnd, colStart = colStart, e.cur.Col
				}
			} else {
				if colEnd = prevQuote(l, colStart, q, false); at(colEnd) != q {
					return span{}, false
				}
				if colStart = prevQuote(l, colEnd, q, true); at(colStart) != q {
					colStart, colEnd = colEnd, e.cur.Col
				}
			}
		case at(colStart) == q || !visEmpty:
			first := colStart
			if !visEmpty {
				if visBefore {
					first = nextQuote(l, colStart, q, false)
				} else {
					first = prevQuote(l, colStart, q, false)
				}
			}
			// the quoted string the cursor's quote is in, counting from the start of the line
			for colStart = 0; ; colStart = colEnd + 1 {
				if colStart = nextQuote(l, colStart, q, false); colStart < 0 || colStart > first {
					return span{}, false
				}
				if colEnd = nextQuote(l, colStart+1, q, true); colEnd < 0 {
					return span{}, false
				}
				if colStart <= first && first <= colEnd {
					break
				}
			}
		default:
			if colStart = prevQuote(l, colStart, q, true); at(colStart) != q {
				if colStart = nextQuote(l, colStart, q, false); colStart < 0 {
					return span{}, false
				}
			}
			if colEnd = nextQuote(l, colStart+1, q, true); colEnd < 0 {
				return span{}, false
			}
		}
		if include { // the blanks after, else those before
			if white(at(colEnd + 1)) {
				for white(at(colEnd + 1)) {
					colEnd++
				}
			} else {
				for colStart > 0 && white(l[colStart-1]) {
					colStart--
				}
			}
		}
		if !include && count < 2 && (visEmpty || !inside) {
			colStart++
		}
		start := Pos{e.cur.Line, colStart}
		if visual && (visEmpty || visBefore && !selectedQuote &&
			(inside || at(e.vstart.Col) != q && (e.vstart.Col == 0 || l[e.vstart.Col-1] != q))) {
			e.vstart = start
		}
		e.cur.Col = colEnd
		if (include || count > 1 || !visEmpty && inside) && e.inc(&e.cur) == 2 {
			inclusive = true
		}
		if !visual {
			return span{start: start, end: e.cur, inclusive: inclusive}, true
		}
		if visEmpty || visBefore {
			e.dec(&e.cur)
		} else {
			if inside || !selectedQuote && at(e.vstart.Col) != q && (at(e.vstart.Col) == 0 || at(e.vstart.Col+1) != q) {
				e.dec(&e.cur)
				e.vstart = e.cur
			}
			e.cur.Col = colStart
		}
		if e.mode == VisualLine {
			e.mode = Visual
		}
		return span{}, true
	}
}

func white(b byte) bool { return b == ' ' || b == '\t' }

func (e *Editor) blank(n int) bool { return strings.TrimLeft(e.lines[n], " \t") == "" }

func paraObject(e *Editor, count int, include bool) (span, bool) {
	start := e.cur.Line
	last := len(e.lines) - 1
	if e.visual() && start != e.vstart.Line {
		return span{}, e.extendPara(start, count, include)
	}
	whiteFront := e.blank(start)
	for start > 0 {
		if whiteFront {
			if !e.blank(start - 1) {
				break
			}
		} else if e.blank(start - 1) {
			break
		}
		start--
	}
	end := start
	for end <= last && e.blank(end) {
		end++
	}
	end--
	i := count
	if !include && whiteFront {
		i--
	}
	for ; i > 0; i-- {
		if end == last {
			return span{}, false
		}
		doWhite := false
		if !include {
			doWhite = e.blank(end + 1)
		}
		if include || !doWhite {
			end++
			for end < last && !e.blank(end+1) {
				end++
			}
		}
		if i == 1 && whiteFront && include {
			break
		}
		if include || doWhite {
			for end < last && e.blank(end+1) {
				end++
			}
		}
	}
	if !whiteFront && !e.blank(end) && include {
		for start > 0 && e.blank(start-1) {
			start--
		}
	}
	if e.visual() {
		if e.mode == VisualLine && start == e.cur.Line {
			return span{}, e.extendPara(e.cur.Line, count, include)
		}
		if e.vstart.Line != start {
			e.vstart = Pos{start, 0}
		}
		e.mode = VisualLine
		e.cur = Pos{end, 0}
		return span{}, true
	}
	e.cur = Pos{end, 0}
	return span{start: Pos{start, 0}, end: e.cur, linewise: true}, true
}

// extendPara is ip and ap in VISUAL over lines already: the selection
// grows by paragraphs toward the cursor's side.
func (e *Editor) extendPara(start, count int, include bool) bool {
	dir, edge := 1, len(e.lines)-1
	if start < e.vstart.Line {
		dir, edge = -1, 0
	}
	ok := true
	for i := count; i > 0; i-- {
		if start == edge {
			ok = false
			break
		}
		prevWhite := -1
		for t := 0; t < 2; t++ {
			start += dir
			w := 0
			if e.blank(start) {
				w = 1
			}
			if prevWhite == w {
				start -= dir
				break
			}
			for start != edge && e.blank(start+dir) == (w == 1) {
				start += dir
			}
			if !include || start == edge {
				break
			}
			prevWhite = w
		}
	}
	e.cur = Pos{start, 0}
	return ok
}
