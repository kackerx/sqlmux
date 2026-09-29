package editor

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Search and the : line (§11): patterns are Go's RE2, which reads close to
// vim's \v; ignorecase with smartcase, and searches wrap around the end.

// cmdline is the / ? or : line being typed, drawn on the console's last
// row.
type cmdline struct {
	prompt string // "/", "?" or ":"
	text   string
	pos    int  // the cursor, a byte offset into text
	c      cmd  // the count, and the operator / or ? is the motion of
	back   Mode // the mode to go back to: NORMAL, or VISUAL for a search
}

// CmdLine is the line being typed after / ? or :, and the cursor in it;
// ok is false when none is.
func (e *Editor) CmdLine() (prompt, text string, pos int, ok bool) {
	if e.cl == nil {
		return "", "", 0, false
	}
	return e.cl.prompt, e.cl.text, e.cl.pos, true
}

func (e *Editor) openCmdline(prompt string, c cmd) {
	back := Normal
	if e.visual() && prompt != ":" {
		back = e.mode
	}
	if e.visual() && prompt == ":" {
		e.endVisual()
		c.arg = "'<,'>"
	}
	e.cl = &cmdline{prompt: prompt, text: c.arg, pos: len(c.arg), c: c, back: back}
	e.mode = Command
}

func (e *Editor) cmdKey(k string) {
	cl := e.cl
	t, p := cl.text, cl.pos
	switch k {
	case "<Esc>":
		e.closeCmdline()
	case "<CR>":
		e.closeCmdline()
		if cl.prompt == ":" {
			e.ex(cl.text)
		} else {
			e.searchCmd(cl)
		}
	case "<BS>", "<C-h>":
		if t == "" {
			e.closeCmdline()
			return
		}
		if p > 0 {
			q := prev(t, p)
			cl.text, cl.pos = t[:q]+t[p:], q
		}
	case "<C-w>":
		q := p
		for q > 0 && t[q-1] == ' ' {
			q--
		}
		for q > 0 && t[q-1] != ' ' {
			q = prev(t, q)
		}
		cl.text, cl.pos = t[:q]+t[p:], q
	case "<C-u>":
		cl.text, cl.pos = t[p:], 0
	case "<Left>":
		cl.pos = prev(t, p)
	case "<Right>":
		cl.pos = next(t, p)
	default:
		if s := keyText(k); s != "" {
			cl.text, cl.pos = t[:p]+s+t[p:], p+len(s)
		}
	}
}

func (e *Editor) closeCmdline() {
	e.mode, e.cl = e.cl.back, nil
}

// search is what n and N repeat: the pattern and how it was searched.
type search struct {
	pattern   string
	ignore    bool // ignore case
	backwards bool
}

// compile makes the pattern's regexp; smart is 'smartcase': a pattern with
// an upper case letter matches case.
func compile(pattern string, ignore, smart bool) (*regexp.Regexp, error) {
	if smart && strings.IndexFunc(pattern, unicode.IsUpper) >= 0 {
		ignore = false
	}
	if ignore {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}

// searchCmd runs / or ? with what cl holds: an empty pattern is the last
// one.
func (e *Editor) searchCmd(cl *cmdline) {
	s := search{pattern: cl.text, ignore: true, backwards: cl.prompt == "?"}
	if s.pattern == "" {
		s.pattern, s.ignore = e.lastSearch.pattern, e.lastSearch.ignore
	} else if strings.IndexFunc(s.pattern, unicode.IsUpper) >= 0 {
		s.ignore = false
	}
	if s.pattern == "" {
		return
	}
	e.lastSearch = s
	c := cl.c
	c.name = "n"
	if c.op != "" {
		e.operate(c)
	} else {
		e.run(c)
	}
}

// findNext is n and N (reverse): the last search again, count times.
func (e *Editor) findNext(c cmd, reverse bool) target {
	s := e.lastSearch
	fail := target{to: e.cur}
	if s.pattern == "" {
		return fail
	}
	re, err := compile(s.pattern, s.ignore, false)
	if err != nil {
		e.eff.Error = "正则有误：" + err.Error()
		return fail
	}
	back := s.backwards != reverse
	p := e.cur
	for n := c.n(); n > 0; n-- {
		var ok bool
		if p, ok = e.searchFrom(re, p, back); !ok {
			e.eff.Error = "找不到：" + s.pattern
			return fail
		}
	}
	return target{to: p, ok: true}
}

// searchFrom finds the next match of re after p (before it, back),
// wrapping around the end of the text.
func (e *Editor) searchFrom(re *regexp.Regexp, p Pos, back bool) (Pos, bool) {
	n := len(e.lines)
	for i := 0; i <= n; i++ {
		line := p.Line + i
		if back {
			line = p.Line - i
		}
		line = (line%n + n) % n
		// on the cursor's line only past the cursor, and back on it after
		// going all the way round only up to it
		ok := func(col int) bool {
			switch {
			case i == 0 && back:
				return col < p.Col
			case i == 0:
				return col > p.Col
			case i == n && back:
				return col >= p.Col
			case i == n:
				return col <= p.Col
			}
			return true
		}
		ms := re.FindAllStringIndex(e.lines[line], -1)
		if back {
			for j := len(ms) - 1; j >= 0; j-- {
				if ok(ms[j][0]) {
					return Pos{line, ms[j][0]}, true
				}
			}
			continue
		}
		for _, m := range ms {
			if ok(m[0]) {
				return Pos{line, m[0]}, true
			}
		}
	}
	return p, false
}

// star is * and # (normal.c nv_ident): the keyword under or after the
// cursor as a whole word, else the run of non-blanks there; case is
// ignored, not smart.
func (e *Editor) star(c cmd, back bool) {
	l := e.line()
	i := e.cur.Col
	keyword := func(j int) bool { r, _ := utf8.DecodeRuneInString(l[j:]); return class(r) >= 2 }
	for i < len(l) && !keyword(i) {
		i = next(l, i)
	}
	var start, end int
	if i < len(l) {
		start, end = i, i
		for start > 0 && keyword(prev(l, start)) {
			start = prev(l, start)
		}
		for end < len(l) && keyword(end) {
			end = next(l, end)
		}
	} else {
		start = e.cur.Col
		for start < len(l) && white(l[start]) {
			start++
		}
		end = start
		for end < len(l) && !white(l[end]) {
			end = next(l, end)
		}
	}
	if start == end {
		return
	}
	pattern := regexp.QuoteMeta(l[start:end])
	if i < len(l) {
		pattern = `\b` + pattern + `\b`
	}
	e.lastSearch = search{pattern: pattern, ignore: true, backwards: back}
	e.cur.Col = start
	e.run(cmd{count: c.count, name: "n"})
}

// ex runs a : line: {n} goes to line n and s substitutes; the rest is the
// console's (:w, :q), handed on as Effect.Ex.
func (e *Editor) ex(line string) {
	line = strings.TrimSpace(line)
	from, to := e.cur.Line, e.cur.Line
	rest := line
	switch {
	case strings.HasPrefix(line, "%"):
		from, to, rest = 0, len(e.lines)-1, line[1:]
	case strings.HasPrefix(line, "'<,'>"):
		v := e.lastVisual
		from, to, rest = min(v.start.Line, v.end.Line), max(v.start.Line, v.end.Line), line[5:]
	default:
		if n := len(line) - len(strings.TrimLeft(line, "0123456789")); n > 0 {
			l, _ := strconv.Atoi(line[:n])
			from, to, rest = l-1, l-1, line[n:]
		}
	}
	from, to = max(min(from, len(e.lines)-1), 0), max(min(to, len(e.lines)-1), 0)
	switch {
	case rest == "" && line != "":
		e.cur.Line = to
		e.cur.Col = e.coladvance(to, e.want)
	case len(rest) >= 2 && rest[0] == 's' && strings.IndexByte(`\"| `, rest[1]) < 0 &&
		!unicode.IsLetter(rune(rest[1])) && !unicode.IsDigit(rune(rest[1])):
		e.substitute(from, to, rest[1:])
	case line != "":
		e.eff.Ex = line
	}
}

// substitute is :s/pattern/replacement/flags over lines from to to: g for
// every match on a line, i to ignore case; & and \1 to \9 in the
// replacement as in vim. The cursor ends on the last line changed.
func (e *Editor) substitute(from, to int, arg string) {
	delim := arg[:1]
	pattern, rest := splitDelim(arg[1:], delim)
	repl, flags := splitDelim(rest, delim)
	ignore, smart := true, true
	if pattern == "" {
		pattern, ignore, smart = e.lastSearch.pattern, e.lastSearch.ignore, false
	}
	if strings.Contains(flags, "i") {
		ignore, smart = true, false
	}
	re, err := compile(pattern, ignore, smart)
	if err != nil {
		e.eff.Error = "正则有误：" + err.Error()
		return
	}
	e.lastSearch = search{pattern: pattern, ignore: ignore && !(smart && strings.IndexFunc(pattern, unicode.IsUpper) >= 0)}
	template := replacement(repl)
	all, changed := strings.Contains(flags, "g"), -1
	for n := from; n <= to; n++ {
		l := e.lines[n]
		var b []byte
		done, last := false, 0
		for _, m := range re.FindAllStringSubmatchIndex(l, -1) {
			if done && !all {
				break
			}
			b = append(b, l[last:m[0]]...)
			b = re.ExpandString(b, template, l, m)
			last, done = m[1], true
		}
		if done {
			if changed < 0 { // undo and U come back to the first match (ex_cmds.c do_sub)
				e.cur = Pos{n, re.FindStringIndex(l)[0]}
				e.saveLine(n, l, e.cur)
			}
			e.setLine(n, string(b)+l[last:])
			changed = n
		}
	}
	if changed < 0 {
		e.eff.Error = "找不到：" + pattern
		return
	}
	e.cur = Pos{changed, head(e.lines[changed], min(nonBlank(e.lines[changed]), last(e.lines[changed])))}
	e.endChange() // U keeps the first line, whatever the lines after did
}

// splitDelim cuts s at the first delim not after a backslash; \delim
// stands for delim itself.
func splitDelim(s, delim string) (before, after string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1:i+2] == delim:
			b.WriteString(delim)
			i++
		case s[i] == '\\' && i+1 < len(s):
			b.WriteString(s[i : i+2])
			i++
		case s[i:i+1] == delim:
			return b.String(), s[i+1:]
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), ""
}

// replacement turns vim's replacement into regexp.Expand's template: & is
// the match, \1 a group, \& and \\ the characters themselves.
func replacement(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '&':
			b.WriteString("${0}")
		case c == '$':
			b.WriteString("$$")
		case c == '\\' && i+1 < len(s):
			i++
			if d := s[i]; d >= '0' && d <= '9' {
				b.WriteString("${" + string(d) + "}")
			} else {
				b.WriteByte(d)
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
