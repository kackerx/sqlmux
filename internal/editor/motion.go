package editor

import (
	"strings"
	"unicode/utf8"
)

// target is where a motion lands and how an operator takes the text up to
// it. A motion that fails leaves to where the cursor ends up anyway (b
// stops at the start of the text) and cancels an operator.
type target struct {
	to        Pos
	ok        bool
	inclusive bool // the character at to is in the range
	linewise  bool
	keepWant  bool // a vertical motion: the column j and k aim for stays
}

// A motion moves from the cursor; op is the operator waiting for it, ""
// for none: some motions stop differently for one (w, l).
type motion func(e *Editor, c cmd, op string) target

var motions map[string]motion

func init() {
	motions = map[string]motion{
		"h": left, "<Left>": left,
		"l": right, "<Right>": right,
		"j": vertical(1), "<Down>": vertical(1),
		"k": vertical(-1), "<Up>": vertical(-1),
		"0": func(e *Editor, _ cmd, _ string) target { return target{to: Pos{e.cur.Line, 0}, ok: true} },
		"^": func(e *Editor, _ cmd, _ string) target {
			return target{to: Pos{e.cur.Line, nonBlank(e.line())}, ok: true}
		},
		"$": dollar,
		"w": fwdWord(false), "W": fwdWord(true),
		"b": bckWord(false), "B": bckWord(true),
		"e": endWord(false), "E": endWord(true),
		"ge": bckendWord(false), "gE": bckendWord(true),
		"gg": goLine(false), "G": goLine(true),
		"f": find, "F": find, "t": find, "T": find, ";": find, ",": find,
		"%": percent,
		"{": para(-1), "}": para(1),
		"H": screenLine('H'), "M": screenLine('M'), "L": screenLine('L'),
	}
}

func left(e *Editor, c cmd, op string) target {
	p := e.cur
	for n := c.n(); n > 0 && p.Col > 0; n-- {
		p.Col = prev(e.line(), p.Col)
	}
	return target{to: p, ok: p != e.cur || op != ""}
}

// right does not go past the last character; with an operator, running
// into it takes the character in instead (normal.c nv_right).
func right(e *Editor, c cmd, op string) target {
	t, l := target{to: e.cur, ok: true}, e.line()
	for n := c.n(); n > 0; n-- {
		if nx := next(l, t.to.Col); nx < len(l) {
			t.to.Col = nx
			continue
		}
		if op == "" {
			t.ok = n < c.n()
		} else if l != "" {
			t.inclusive = true
		}
		break
	}
	return t
}

func vertical(d int) motion {
	return func(e *Editor, c cmd, _ string) target {
		if d > 0 && e.cur.Line == len(e.lines)-1 || d < 0 && e.cur.Line == 0 {
			return target{to: e.cur}
		}
		n := min(max(e.cur.Line+d*c.n(), 0), len(e.lines)-1)
		return target{to: Pos{n, e.coladvance(n, e.curswant())}, ok: true, linewise: true, keepWant: true}
	}
}

func dollar(e *Editor, c cmd, _ string) target {
	n := e.cur.Line + c.n() - 1
	if n >= len(e.lines) {
		return target{to: e.cur}
	}
	e.want = wantEnd
	return target{to: Pos{n, last(e.lines[n])}, ok: true, inclusive: true, keepWant: true}
}

// goLine is gg (last false) and G: the count's line, else the first or
// the last; the column stays ('nostartofline').
func goLine(last bool) motion {
	return func(e *Editor, c cmd, _ string) target {
		n := 0
		if last {
			n = len(e.lines) - 1
		}
		if c.count > 0 {
			n = min(c.count, len(e.lines)) - 1
		}
		return target{to: Pos{n, e.coladvance(n, e.curswant())}, ok: true, linewise: true, keepWant: true}
	}
}

// screenLine is H, M and L (normal.c nv_scroll): lines of the screen.
func screenLine(which byte) motion {
	return func(e *Editor, c cmd, _ string) target {
		h, n := e.rows(), len(e.lines)
		var l int
		switch which {
		case 'H':
			l = min(e.top+c.n()-1, n-1)
		case 'L':
			l = min(e.top+h, n) - 1
			if c.n()-1 > l {
				l = 0
			} else {
				l -= c.n() - 1
			}
		case 'M':
			half := (h - max(e.top+h-n, 0) + 1) / 2
			used := 0
			for l = 0; e.top+l < n-1; l++ {
				if used++; used >= half {
					break
				}
			}
			l += e.top
		}
		return target{to: Pos{l, e.coladvance(l, e.curswant())}, ok: true, linewise: true, keepWant: true}
	}
}

// inc moves p on by one character as vim's inc() does: onto the end of
// the line (Col == len) before going to the next line. It returns 0
// within the line, 2 arriving at its end, 1 going to the next line and -1
// at the end of the text.
func (e *Editor) inc(p *Pos) int {
	if l := e.lines[p.Line]; p.Col < len(l) {
		if p.Col = next(l, p.Col); p.Col < len(l) {
			return 0
		}
		return 2
	}
	if p.Line < len(e.lines)-1 {
		*p = Pos{p.Line + 1, 0}
		return 1
	}
	return -1
}

// dec moves p back by one character, from the start of a line onto the
// end of the one before: 0, 1 when it changed lines, -1 at the start of
// the text.
func (e *Editor) dec(p *Pos) int {
	if p.Col > 0 {
		p.Col = prev(e.lines[p.Line], p.Col)
		return 0
	}
	if p.Line > 0 {
		p.Line--
		p.Col = len(e.lines[p.Line])
		return 1
	}
	return -1
}

// cls is the class of the character at p for word motions: the end of a
// line is blank; for W, B and E every non-blank is one class.
func (e *Editor) cls(p Pos, big bool) int {
	l := e.lines[p.Line]
	if p.Col >= len(l) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l[p.Col:])
	if c := class(r); c == 0 || !big {
		return c
	}
	return 1
}

func (e *Editor) empty(p Pos) bool { return p.Col == 0 && e.lines[p.Line] == "" }

// The word motions follow textobject.c's fwd_word, bck_word, end_word and
// bckend_word, and normal.c's rules around them.

func fwdWord(big bool) motion {
	return func(e *Editor, c cmd, op string) target {
		if op == "c" && e.cls(e.cur, false) != 0 { // cw is ce, but on a word's last character changes just it
			t := target{to: e.cur, ok: true, inclusive: true}
			e.end(&t.to, c.n(), big, true, false)
			e.adjust(&t)
			return t
		}
		t := target{to: e.cur, ok: true}
		t.ok = e.fwd(&t.to, c.n(), big, op != "") || op != ""
		e.adjust(&t)
		return t
	}
}

func (e *Editor) fwd(p *Pos, count int, big, eol bool) bool {
	for count--; count >= 0; count-- {
		sclass := e.cls(*p, big)
		lastLine := p.Line == len(e.lines)-1
		i := e.inc(p)
		if i == -1 || i >= 1 && lastLine {
			return false
		}
		if i >= 1 && eol && count == 0 {
			return true
		}
		if sclass != 0 {
			for e.cls(*p, big) == sclass {
				if i = e.inc(p); i == -1 || i >= 1 && eol && count == 0 {
					return true
				}
			}
		}
		for e.cls(*p, big) == 0 {
			if e.empty(*p) {
				break
			}
			if i = e.inc(p); i == -1 || i >= 1 && eol && count == 0 {
				return true
			}
		}
	}
	return true
}

// adjust keeps a motion that went forward off the end of a line: it lands
// on the last character, which it then takes in (normal.c adjust_cursor).
func (e *Editor) adjust(t *target) {
	if e.cur.less(t.to) && t.to.Col > 0 && t.to.Col >= len(e.lines[t.to.Line]) {
		t.to.Col = last(e.lines[t.to.Line])
		t.inclusive = true
	}
}

func bckWord(big bool) motion {
	return func(e *Editor, c cmd, _ string) target {
		t := target{to: e.cur}
		t.ok = e.bck(&t.to, c.n(), big, false)
		return t
	}
}

func (e *Editor) bck(p *Pos, count int, big, stop bool) bool {
	for count--; count >= 0; count-- {
		sclass := e.cls(*p, big)
		if e.dec(p) == -1 {
			return false
		}
		if !stop || sclass == e.cls(*p, big) || sclass == 0 {
			finished := false
			for e.cls(*p, big) == 0 {
				if e.empty(*p) {
					finished = true
					break
				}
				if e.dec(p) == -1 {
					return true
				}
			}
			if !finished {
				if e.skip(p, e.cls(*p, big), -1, big) {
					return true
				}
				e.inc(p)
			}
		} else {
			e.inc(p)
		}
		stop = false
	}
	return true
}

func endWord(big bool) motion {
	return func(e *Editor, c cmd, op string) target {
		t := target{to: e.cur, inclusive: true}
		t.ok = e.end(&t.to, c.n(), big, false, false) || op != ""
		e.adjust(&t)
		return t
	}
}

func (e *Editor) end(p *Pos, count int, big, stop, empty bool) bool {
	for count--; count >= 0; count-- {
		sclass := e.cls(*p, big)
		if e.inc(p) == -1 {
			return false
		}
		switch {
		case e.cls(*p, big) == sclass && sclass != 0:
			if e.skip(p, sclass, 1, big) {
				return false
			}
		case !stop || sclass == 0:
			finished := false
			for e.cls(*p, big) == 0 {
				if empty && e.empty(*p) {
					finished = true
					break
				}
				if e.inc(p) == -1 {
					return false
				}
			}
			if finished {
				stop = false
				continue
			}
			if e.skip(p, e.cls(*p, big), 1, big) {
				return false
			}
		}
		e.dec(p)
		stop = false
	}
	return true
}

func bckendWord(big bool) motion {
	return func(e *Editor, c cmd, _ string) target {
		t := target{to: e.cur, inclusive: true}
		t.ok = e.bckend(&t.to, c.n(), big, false)
		return t
	}
}

func (e *Editor) bckend(p *Pos, count int, big, eol bool) bool {
	for count--; count >= 0; count-- {
		sclass := e.cls(*p, big)
		i := e.dec(p)
		if i == -1 {
			return false
		}
		if eol && i == 1 {
			return true
		}
		if sclass != 0 {
			for e.cls(*p, big) == sclass {
				if i = e.dec(p); i == -1 || eol && i == 1 {
					return true
				}
			}
		}
		for e.cls(*p, big) == 0 {
			if e.empty(*p) {
				break
			}
			if i = e.dec(p); i == -1 || eol && i == 1 {
				return true
			}
		}
	}
	return true
}

// skip moves p past characters of class c in direction dir; true when it
// ran into the end of the text.
func (e *Editor) skip(p *Pos, c, dir int, big bool) bool {
	for e.cls(*p, big) == c {
		step := e.inc
		if dir < 0 {
			step = e.dec
		}
		if step(p) == -1 {
			return true
		}
	}
	return false
}

// find is f F t T and their repeats ; and , (search.c searchc): within the
// line; t and T stop a character short, and a repeated t does not stay
// stuck right before the character it found.
func find(e *Editor, c cmd, _ string) target {
	fail := target{to: e.cur}
	name, ch, stop := c.name, c.arg, true
	switch name {
	case ";", ",":
		if e.lastFind.ch == "" {
			return fail
		}
		ch = e.lastFind.ch
		if name = e.lastFind.cmd; c.name == "," {
			name = strings.NewReplacer("f", "F", "F", "f", "t", "T", "T", "t").Replace(name)
		}
		stop = c.n() > 1 || name != "t" && name != "T" // cpoptions without ;
	default:
		e.lastFind.cmd, e.lastFind.ch = name, ch
	}
	fwd, till := name == "f" || name == "t", name == "t" || name == "T"
	l, col := e.line(), e.cur.Col
	for n := c.n(); n > 0; n-- {
		for {
			if fwd {
				if col = next(l, col); col >= len(l) {
					return fail
				}
			} else {
				if col == 0 {
					return fail
				}
				col = prev(l, col)
			}
			if stop && strings.HasPrefix(l[col:], ch) {
				break
			}
			stop = true
		}
	}
	if till {
		if fwd {
			col = prev(l, col)
		} else {
			col = next(l, col)
		}
	}
	return target{to: Pos{e.cur.Line, col}, ok: true, inclusive: fwd}
}

// percent is %: from the first bracket at or after the cursor on its line
// to the one matching it.
// ponytail: brackets inside quotes and comments count too; vim skips
// quoted ones. Add that if SQL with ')' in strings trips it up.
func percent(e *Editor, c cmd, _ string) target {
	fail := target{to: e.cur}
	l := e.line()
	i := strings.IndexAny(l[e.cur.Col:], "()[]{}")
	if c.count > 0 || i < 0 {
		return fail
	}
	p := Pos{e.cur.Line, e.cur.Col + i}
	b := l[p.Col]
	j := strings.IndexByte("([{)]}", b)
	open, shut, step := b, "([{)]}"[(j+3)%6], e.inc
	if j >= 3 {
		step = e.dec
	}
	depth := 0
	for step(&p) != -1 {
		if l := e.lines[p.Line]; p.Col < len(l) {
			switch l[p.Col] {
			case open:
				depth++
			case shut:
				if depth == 0 {
					return target{to: p, ok: true, inclusive: true}
				}
				depth--
			}
		}
	}
	return fail
}

// para is { and } (textobject.c findpar): to the next empty line; } at the
// end of the text goes to its last character, and takes it in.
func para(d int) motion {
	return func(e *Editor, c cmd, _ string) target {
		cur := e.cur.Line
		for n := c.n(); n > 0; n-- {
			skipped := false
			for first := true; ; first = false {
				if e.lines[cur] != "" {
					skipped = true
				}
				if !first && skipped && e.lines[cur] == "" {
					break
				}
				if cur += d; cur < 0 || cur >= len(e.lines) {
					if n > 1 {
						return target{to: e.cur}
					}
					cur -= d
					break
				}
			}
		}
		t := target{to: Pos{cur, 0}, ok: true}
		if l := e.lines[cur]; cur == len(e.lines)-1 && d > 0 && l != "" {
			t.to.Col, t.inclusive = last(l), true
		}
		return t
	}
}
