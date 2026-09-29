package editor

import (
	"strings"
	"unicode"
)

// span is the text an operator works on (ops.c oparg_T): from start to
// end, the character at end in when inclusive; whole lines when linewise.
type span struct {
	start, end Pos
	inclusive  bool
	linewise   bool
	visual     bool   // selected in VISUAL, not reached by a motion
	adjusted   bool   // its end moved back from the start of a line (end_adjusted)
	blk        *block // a VISUAL BLOCK
}

func (s span) lines() int { return s.end.Line - s.start.Line + 1 }

// empty is a charwise span with nothing in it.
func (s span) empty() bool { return !s.linewise && !s.inclusive && s.start == s.end }

var operators = map[string]bool{"d": true, "c": true, "y": true, ">": true, "<": true, "gu": true, "gU": true, "g~": true, "gc": true, "gq": true}

// shorthands are the commands that are an operator and a motion.
var shorthands = map[string][2]string{
	"x": {"d", "l"}, "X": {"d", "h"}, "D": {"d", "$"}, "C": {"c", "$"},
	"s": {"c", "l"}, "S": {"c", "_"}, "Y": {"y", "$"}, // Y is y$, as nvim maps it
}

// register is the unnamed register: its text and whether it is charwise
// ('v'), linewise ('V') or a block (blockKind, width columns wide).
type register struct {
	text  string
	kind  byte
	width int
}

// Register is the unnamed register's text, what a yank or a delete put in
// it: the clipboard gets it too (§11).
func (e *Editor) Register() string { return e.reg.text }

func (e *Editor) setReg(text string, kind byte) {
	e.reg = register{text: text, kind: kind}
	e.eff.Yanked = true
}

// operate runs operator c.op over the text c's motion or text object
// covers (normal.c, ops.c do_pending_operator).
func (e *Editor) operate(c cmd) {
	oldWant := e.want
	var t target
	var s span
	switch {
	case c.name == "_": // dd, cc, yy...: the line and count-1 below (normal.c nv_lineop)
		if c.n() > 1 && e.cur.Line == len(e.lines)-1 {
			return
		}
		n := min(e.cur.Line+c.n()-1, len(e.lines)-1)
		t = target{to: Pos{n, e.coladvance(n, e.want)}, ok: true, linewise: true}
		if c.op != "d" && c.op != "<" && c.op != ">" && c.op != "y" { // the others start at the first non-blank
			t.to.Col = firstNonBlank(e.lines[n])
		}
	case objects[c.name] != nil:
		start := e.cur
		var ok bool
		if s, ok = objects[c.name](e, c.n(), c.name[0] == 'a'); !ok {
			e.cur = start
			return
		}
	default:
		if t = motions[c.name](e, c, c.op); !t.ok {
			return
		}
	}
	if objects[c.name] == nil {
		s = span{start: e.cur, end: t.to, inclusive: t.inclusive, linewise: t.linewise}
		if s.end.less(s.start) {
			s.start, s.end = s.end, s.start
		}
	}
	// An exclusive motion to the start of a line ends at the end of the
	// line before, and takes whole lines when it starts in the indent.
	if !s.linewise && !s.inclusive && s.end.Col == 0 && s.lines() > 1 && c.name != "_" {
		s.adjusted = true
		s.end.Line--
		if nonBlank(e.lines[s.start.Line]) >= s.start.Col {
			s.linewise = true
		} else if l := e.lines[s.end.Line]; l != "" {
			s.end.Col, s.inclusive = last(l), true
		}
	}
	if c.op == "gq" && c.name == "_" { // gqq, gqgq: the statement, not the line
		e.eff.Format = &FormatSpan{From: e.cur, To: e.cur, Current: true}
		return
	}
	e.apply(c.op, s, 1, oldWant)
}

// apply runs an operator; the cursor goes to the start of the text first.
// amount is how many shiftwidths > and < shift; want is the column j and
// k aimed for as the command started.
func (e *Editor) apply(op string, s span, amount, want int) {
	e.cur, e.want = s.start, wantUnset
	switch op {
	case "d":
		e.delete(s, false)
	case "y":
		e.yank(s)
	case "c":
		e.change(s)
	case ">", "<":
		e.shift(s, op == "<", amount)
		e.cur.Col = e.coladvance(e.cur.Line, want) // op_shift's beginline(BL_SOL | BL_FIX)
	case "gu", "gU", "g~":
		e.setCase(s, op)
	case "gc":
		e.comment(s.start.Line, s.end.Line)
	case "gq":
		f := &FormatSpan{From: s.start, To: Pos{s.end.Line, e.endCol(s)}, Selected: s.visual}
		if s.linewise {
			f.From.Col, f.To.Col = 0, len(e.lines[s.end.Line])
		}
		e.eff.Format = f
	case "J":
		if n := max(s.lines(), 2); s.start.Line+n-1 < len(e.lines) {
			e.join(n)
		}
	}
	// 'nostartofline': back to the column the command started in
	// (do_pending_operator); j and k go by where that is, as beginline set
	// w_set_curswant
	if s.linewise && !s.adjusted && (op == "d" || op == ">" || op == "<") {
		e.cur.Col = e.coladvance(e.cur.Line, want)
	}
}

// endCol is where a charwise s ends on its last line: past the character
// at s.end when inclusive.
func (e *Editor) endCol(s span) int {
	l := e.lines[s.end.Line]
	end := min(s.end.Col, len(l))
	if s.inclusive && end < len(l) {
		end = next(l, end)
	}
	return end
}

// text is what s covers, and its register kind.
func (e *Editor) text(s span) (string, byte) {
	if s.linewise {
		return strings.Join(e.lines[s.start.Line:s.end.Line+1], "\n") + "\n", 'V'
	}
	end := e.endCol(s)
	if s.lines() == 1 {
		return e.lines[s.start.Line][s.start.Col:end], 'v'
	}
	parts := []string{e.lines[s.start.Line][s.start.Col:]}
	parts = append(parts, e.lines[s.start.Line+1:s.end.Line]...)
	parts = append(parts, e.lines[s.end.Line][:end])
	return strings.Join(parts, "\n"), 'v'
}

func (e *Editor) yank(s span) {
	e.setReg(e.text(s))
	e.eff.Yank = &Yank{From: s.start, To: Pos{s.end.Line, e.endCol(s)}, Linewise: s.linewise}
}

// delete deletes s into the register (ops.c op_delete); change keeps the
// first line's indent of a linewise span, for c.
func (e *Editor) delete(s span, change bool) {
	if s.empty() {
		if len(e.lines) > 1 || e.lines[0] != "" { // vim saves for undo all the same, but not in an empty text
			e.beginChange()
		}
		return
	}
	if !s.linewise && s.lines() > 1 && !change && !s.visual {
		// vi: to the end of a line from the indent is whole lines
		if strings.TrimLeft(e.lines[s.end.Line][e.endCol(s):], " \t") == "" && nonBlank(e.lines[s.start.Line]) >= s.start.Col {
			s.linewise = true
		}
	}
	if !s.linewise && s.lines() == 1 && !change && e.lines[s.start.Line] == "" {
		return
	}
	e.setReg(e.text(s))
	if s.linewise {
		if change {
			if s.lines() > 1 {
				e.deleteLines(s.start.Line+1, s.end.Line+1)
				e.uLine = -1 // no U after 2cc
			}
			l := e.lines[s.start.Line]
			e.cur = Pos{s.start.Line, nonBlank(l)}
			e.setLine(e.cur.Line, l[:e.cur.Col])
			return
		}
		e.deleteLines(s.start.Line, s.end.Line+1)
		e.cur.Line = min(s.start.Line, len(e.lines)-1)
		e.cur.Col = nonBlank(e.line())
		e.uLine = -1 // no U after dd
		return
	}
	first, rest := e.lines[s.start.Line][:s.start.Col], e.lines[s.end.Line][e.endCol(s):]
	if s.lines() == 1 {
		e.setLine(s.start.Line, first+rest)
	} else { // as op_delete: the lines between go, then the two ends join
		e.deleteLines(s.start.Line+1, s.end.Line)
		e.setLine(s.start.Line, first)
		e.setLine(s.start.Line+1, rest)
		e.joinNext(s.start.Line, "", 0)
	}
	e.cur = s.start
}

// change deletes s and types in its place (ops.c op_change); a linewise
// change keeps the indent, which esc takes off if nothing is typed.
func (e *Editor) change(s span) {
	e.delete(s, true)
	e.startInsert(Insert, cmd{name: "c"})
	e.ins.ai = s.linewise
}

// shift moves the lines' indents a shiftwidth (ops.c op_shift, shift_line);
// empty lines stay.
func (e *Editor) shift(s span, left bool, amount int) {
	e.beginChange() // an undo step even when nothing moves, as in vim
	for n := s.start.Line; n <= s.end.Line; n++ {
		l := e.lines[n]
		if l == "" {
			continue
		}
		indent := vcol(l, nonBlank(l), e.TabWidth)
		if left {
			indent = max(indent-e.TabWidth*amount, 0)
		} else {
			indent += e.TabWidth * amount
		}
		if nl := strings.Repeat(" ", indent) + l[nonBlank(l):]; nl != l {
			e.setLine(n, nl)
		}
	}
}

// setCase is gu, gU and g~ (ops.c op_tilde).
func (e *Editor) setCase(s span, op string) {
	e.beginChange()
	for n := s.start.Line; n <= s.end.Line; n++ {
		l := e.lines[n]
		from, to := 0, len(l)
		if !s.linewise {
			if n == s.start.Line {
				from = s.start.Col
			}
			if n == s.end.Line {
				to = e.endCol(s)
			}
		}
		if nl := l[:from] + convertCase(l[from:to], op) + l[to:]; nl != l {
			e.setLine(n, nl)
		}
	}
}

func convertCase(s, op string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case op == "gu":
			return unicode.ToLower(r)
		case op != "gU" && unicode.IsUpper(r):
			return unicode.ToLower(r)
		case r == 'ß': // nvim's utf8proc has it, Go's simple case mapping not
			return 'ẞ'
		}
		return unicode.ToUpper(r)
	}, s)
}

// commentLeft is what 'commentstring' "-- %s" puts before a line.
const commentLeft = "-- "

// comment is gc (nvim's runtime/lua/vim/_comment.lua toggle_lines): lines
// that all start with -- lose it; otherwise every line gets -- at the
// smallest indent, blank lines just "--".
func (e *Editor) comment(from, to int) {
	e.beginChange()
	lead := func(l string) int { return len(l) - len(strings.TrimLeft(l, " \t\n\v\f\r")) } // Lua's %s*
	indent, commented := "", true
	width := -1
	for _, l := range e.lines[from : to+1] {
		n := lead(l)
		if n == len(l) {
			continue
		}
		if width < 0 || n < width {
			width, indent = n, l[:n]
		}
		commented = commented && strings.HasPrefix(l[n:], strings.TrimSpace(commentLeft))
	}
	for i, l := range e.lines[from : to+1] {
		nl := l
		switch {
		case commented:
			n := lead(l)
			rest, ok := strings.CutPrefix(l[n:], commentLeft)
			if !ok {
				rest, ok = strings.CutPrefix(l[n:], strings.TrimSpace(commentLeft))
			}
			if ok {
				if nl = l[:n] + rest; strings.TrimSpace(rest) == "" {
					nl = rest
				}
			}
		case strings.TrimSpace(l) == "":
			nl = indent + strings.TrimSpace(commentLeft)
		default:
			nl = indent + commentLeft + l[len(indent):]
		}
		if nl != l {
			e.setLine(from+i, nl)
		}
	}
}

// join joins count lines from the cursor's (ops.c do_join): the blanks at
// the start of each joined line go, and one space is put between unless
// the line ends in a space already, the next starts with ')', or either is
// empty. The cursor goes where the last line was joined.
func (e *Editor) join(count int) {
	n, col := e.cur.Line, 0
	for range count - 1 {
		out, l := e.lines[n], e.lines[n+1]
		skip := len(l) - len(strings.TrimLeft(l, " \t"))
		sep := ""
		if skip < len(l) && l[skip] != ')' && out != "" && !white(out[len(out)-1]) {
			sep = " "
		}
		col = len(out)
		e.joinNext(n, sep, skip)
	}
	e.cur.Col = col
}

// put is p (after) and P, count times (register.c do_put): a charwise
// register goes in at the cursor, the cursor on its last character, or on
// its first when it has several lines; a linewise one goes below or above
// the cursor's line, the cursor on the first line's first non-blank.
func (e *Editor) put(after bool, count int) {
	r := e.reg
	switch r.kind {
	case 0: // nothing to put, but vim has saved for undo
		e.beginChange()
		return
	case blockKind:
		e.putBlock(after, count)
		return
	}
	if r.kind == 'V' {
		ls := strings.Split(strings.TrimSuffix(r.text, "\n"), "\n")
		var all []string
		for range count {
			all = append(all, ls...)
		}
		at := e.cur.Line
		if after {
			at++
		}
		e.insertLines(at, all...)
		e.cur = Pos{at, nonBlank(e.lines[at])}
		return
	}
	l, col := e.line(), e.cur.Col
	if after && l != "" && r.text != "" {
		col = next(l, col)
	}
	end := e.insertText(col, strings.Split(strings.Repeat(r.text, count), "\n"))
	e.cur.Col = col
	if end.Line == e.cur.Line && r.text != "" {
		e.cur.Col = prev(e.line(), end.Col)
	}
}

// tilde is ~: the case of count characters toggles, the cursor going past
// them (normal.c n_swapchar).
func (e *Editor) tilde(count int) {
	l := e.line()
	if l == "" {
		return
	}
	e.beginChange()
	from := e.cur.Col
	to := from
	for ; count > 0 && to < len(l); count-- {
		to = next(l, to)
	}
	toggled := convertCase(l[from:to], "g~")
	if nl := l[:from] + toggled + l[to:]; nl != l {
		e.setLine(e.cur.Line, nl)
	}
	e.cur.Col = from + len(toggled)
}

// replace is r: count characters become ch, the cursor on the last
// (normal.c nv_replace). r<CR> breaks the line there instead, and r<Tab>
// types the tab in REPLACE, both as vim does.
func (e *Editor) replace(c cmd) {
	l, col := e.line(), e.cur.Col
	end := col
	for n := c.n(); n > 0; n-- {
		if end >= len(l) {
			return
		}
		end = next(l, end)
	}
	switch c.arg {
	case "\r":
		e.setLine(e.cur.Line, l[:col]+l[end:])
		e.startInsert(Insert, cmd{name: "r"})
		e.insertKey("<CR>")
		e.insertKey("<Esc>")
	case "\t":
		e.startInsert(Replace, cmd{name: "R", count: c.count})
		e.insertKey("<Tab>")
		e.insertKey("<Esc>")
	default:
		e.setLine(e.cur.Line, l[:col]+strings.Repeat(c.arg, c.n())+l[end:])
		e.cur.Col = col + len(c.arg)*(c.n()-1)
	}
}
