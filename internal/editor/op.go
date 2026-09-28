package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// span is the text an operator works on (ops.c oparg_T): from start to
// end, the character at end in when inclusive; whole lines when linewise.
type span struct {
	start, end Pos
	inclusive  bool
	linewise   bool
	visual     bool // selected in VISUAL, not reached by a motion
}

func (s span) lines() int { return s.end.Line - s.start.Line + 1 }

// empty is a charwise span with nothing in it.
func (s span) empty() bool { return !s.linewise && !s.inclusive && s.start == s.end }

var operators = map[string]bool{"d": true, "c": true, "y": true, ">": true, "<": true, "gu": true, "gU": true, "g~": true, "gc": true}

// shorthands are the commands that are an operator and a motion.
var shorthands = map[string][2]string{
	"x": {"d", "l"}, "X": {"d", "h"}, "D": {"d", "$"}, "C": {"c", "$"},
	"s": {"c", "l"}, "S": {"c", "_"}, "Y": {"y", "$"}, // Y is y$, as nvim maps it
}

// register is the unnamed register: its text and whether it is charwise
// ('v') or linewise ('V').
type register struct {
	text string
	kind byte
}

// Register is the unnamed register's text, what a yank or a delete put in
// it: the clipboard gets it too (§11).
func (e *Editor) Register() string { return e.reg.text }

func (e *Editor) setReg(text string, kind byte) {
	e.reg = register{text, kind}
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
			t.to.Col = head(e.lines[n], min(nonBlank(e.lines[n]), last(e.lines[n])))
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
	adjusted := false
	if !s.linewise && !s.inclusive && s.end.Col == 0 && s.lines() > 1 && c.name != "_" {
		adjusted = true
		s.end.Line--
		if nonBlank(e.lines[s.start.Line]) >= s.start.Col {
			s.linewise = true
		} else if l := e.lines[s.end.Line]; l != "" {
			s.end.Col, s.inclusive = last(l), true
		}
	}
	e.apply(c.op, s, 1)
	// 'nostartofline': back to the column the command started in
	if s.linewise && !adjusted && (c.op == "d" || c.op == ">" || c.op == "<") {
		e.want = oldWant
		e.cur.Col = e.coladvance(e.cur.Line, oldWant)
	}
}

// apply runs an operator; the cursor goes to the start of the text first.
// amount is how many shiftwidths > and < shift.
func (e *Editor) apply(op string, s span, amount int) {
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
	case "gu", "gU", "g~":
		e.setCase(s, op)
	case "gc":
		e.comment(s.start.Line, s.end.Line)
	case "J":
		if n := max(s.lines(), 2); s.start.Line+n-1 < len(e.lines) {
			e.join(n, true)
		}
	}
}

// text is what s covers, and its register kind.
func (e *Editor) text(s span) (string, byte) {
	if s.linewise {
		return strings.Join(e.lines[s.start.Line:s.end.Line+1], "\n") + "\n", 'V'
	}
	end := s.end.Col
	if l := e.lines[s.end.Line]; s.inclusive && end < len(l) {
		end = next(l, end)
	}
	if s.lines() == 1 {
		l := e.lines[s.start.Line]
		return l[s.start.Col:min(end, len(l))], 'v'
	}
	parts := []string{e.lines[s.start.Line][s.start.Col:]}
	parts = append(parts, e.lines[s.start.Line+1:s.end.Line]...)
	parts = append(parts, e.lines[s.end.Line][:end])
	return strings.Join(parts, "\n"), 'v'
}

func (e *Editor) yank(s span) {
	e.setReg(e.text(s))
}

// delete deletes s into the register (ops.c op_delete); change keeps the
// first line's indent of a linewise span, for c.
func (e *Editor) delete(s span, change bool) {
	if s.empty() {
		return
	}
	if !s.linewise && s.lines() > 1 && !change && !s.visual {
		// vi: to the end of a line from the indent is whole lines
		end := s.end.Col
		if l := e.lines[s.end.Line]; s.inclusive && end < len(l) {
			end = next(l, end)
		}
		if strings.TrimLeft(e.lines[s.end.Line][end:], " \t") == "" && nonBlank(e.lines[s.start.Line]) >= s.start.Col {
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
	end := s.end.Col
	if l := e.lines[s.end.Line]; s.inclusive && end < len(l) {
		end = next(l, end)
	}
	first, rest := e.lines[s.start.Line][:s.start.Col], e.lines[s.end.Line][min(end, len(e.lines[s.end.Line])):]
	e.setLine(s.start.Line, first+rest)
	if s.lines() > 1 {
		e.deleteLines(s.start.Line+1, s.end.Line+1)
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
	e.cur = Pos{s.start.Line, e.coladvance(s.start.Line, e.want)}
}

// setCase is gu, gU and g~ (ops.c op_tilde).
func (e *Editor) setCase(s span, op string) {
	for n := s.start.Line; n <= s.end.Line; n++ {
		l := e.lines[n]
		from, to := 0, len(l)
		if !s.linewise {
			if n == s.start.Line {
				from = s.start.Col
			}
			if n == s.end.Line {
				to = min(s.end.Col, len(l))
				if s.inclusive && to < len(l) {
					to = next(l, to)
				}
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
		case op == "gU":
			return unicode.ToUpper(r)
		case unicode.IsUpper(r):
			return unicode.ToLower(r)
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
	indent, commented := "", true
	width := -1
	for _, l := range e.lines[from : to+1] {
		n := len(l) - len(strings.TrimLeft(l, " \t\n\v\f\r"))
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
			n := len(l) - len(strings.TrimLeft(l, " \t\n\v\f\r"))
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
func (e *Editor) join(count int, space bool) {
	n := e.cur.Line
	out, col := e.lines[n], 0
	for _, l := range e.lines[n+1 : n+count] {
		if space {
			l = strings.TrimLeft(l, " \t")
		}
		sep := ""
		if last, _ := utf8.DecodeLastRuneInString(out); space && l != "" && l[0] != ')' && out != "" && last != '\t' && last != ' ' {
			sep = " "
		}
		col = len(out)
		out += sep + l
	}
	e.setLine(n, out)
	e.deleteLines(n+1, n+count)
	e.cur.Col = col
}

// put is p (after) and P, count times (register.c do_put): a charwise
// register goes in at the cursor, the cursor on its last character, or on
// its first when it has several lines; a linewise one goes below or above
// the cursor's line, the cursor on the first line's first non-blank.
func (e *Editor) put(after bool, count int) {
	r := e.reg
	if r.kind == 0 {
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
	text := strings.Repeat(r.text, count)
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		e.setLine(e.cur.Line, l[:col]+text+l[col:])
		if text != "" {
			e.cur.Col = prev(e.line(), col+len(text))
		}
		return
	}
	parts[len(parts)-1] += l[col:]
	e.setLine(e.cur.Line, l[:col]+parts[0])
	e.insertLines(e.cur.Line+1, parts[1:]...)
	e.cur.Col = col
}

// tilde is ~: the case of count characters toggles, the cursor going past
// them (normal.c n_swapchar).
func (e *Editor) tilde(count int) {
	l := e.line()
	if l == "" {
		return
	}
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
