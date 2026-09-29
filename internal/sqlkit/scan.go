// Package sqlkit reads SQL text without parsing it (tech-design §9): a
// scanner by dialect, statements, whether one reads or writes, the LIMIT
// a read gets, and what the WHERE input's cursor is at.
package sqlkit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Dialect is the SQL a text is read as.
type Dialect int

const (
	PG Dialect = iota
	MySQL
)

// Kind is what a token is.
type Kind int

const (
	Space   Kind = iota
	Ident        // an identifier
	Keyword      // a word of the keyword table (keywords.go)
	Quoted       // a "quoted" identifier, `quoted` in MySQL
	String       // a string literal, maybe unterminated
	Number
	Op      // = <> != < <= > >= || :: and the like
	Punct   // ( ) [ ] , ; .
	Comment // to the line's end, or /* */
)

// Token is s[Start:End], byte offsets, starting on line Line (from 0).
type Token struct {
	Kind       Kind
	Start, End int
	Line       int
}

// Scan splits s into tokens (§9.1), after lazysql's components/sql_lexer.go,
// with each dialect's strings and comments:
//   - PG: ” in '…', \ in E'…', $tag$…$tag$, "…" identifiers, -- and
//     /* */ that nest;
//   - MySQL: \ and doubled quotes in '…' and "…", `…` identifiers, #, --
//     with a blank after it, /* */ that do not nest.
//
// What is left open runs to the end.
func Scan(s string, d Dialect) []Token {
	var out []Token
	line := 0
	for i := 0; i < len(s); {
		start := i
		r, n := utf8.DecodeRuneInString(s[i:])
		kind := Space
		switch {
		case strings.HasPrefix(s[i:], "--") && (d == PG || i+2 >= len(s) || s[i+2] <= ' '),
			d == MySQL && r == '#':
			kind, i = Comment, strings.IndexByte(s[i:]+"\n", '\n')+i
		case strings.HasPrefix(s[i:], "/*"):
			kind, i = Comment, blockComment(s, i, d == PG)
		case r == '\'':
			kind, i = String, quoted(s, i, d == MySQL)
		case r == '"' && d == PG, r == '`' && d == MySQL:
			kind, i = Quoted, quoted(s, i, false)
		case r == '"':
			kind, i = String, quoted(s, i, true)
		case d == PG && (r == 'e' || r == 'E') && strings.HasPrefix(s[i+1:], "'"): // E'…'
			kind, i = String, quoted(s, i+1, true)
		case d == PG && r == '$' && dollarTag(s[i:]) != "":
			tag := dollarTag(s[i:])
			kind = String
			if j := strings.Index(s[i+len(tag):], tag); j >= 0 {
				i += len(tag) + j + len(tag)
			} else {
				i = len(s)
			}
		case unicode.IsDigit(r), r == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			kind, i = Number, number(s, i)
		case r == '_' || unicode.IsLetter(r):
			i = advance(s, i, func(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) })
			kind = Ident
			if keywords[strings.ToLower(s[start:i])] {
				kind = Keyword
			}
		case strings.ContainsRune("()[],;.", r):
			kind, i = Punct, i+n
		case unicode.IsSpace(r):
			i = advance(s, i, unicode.IsSpace)
		default:
			kind = Op
			for i < len(s) && strings.IndexByte("=<>!~+-*/%|&^#@:?", s[i]) >= 0 &&
				!strings.HasPrefix(s[i:], "--") && !strings.HasPrefix(s[i:], "/*") && (d == PG || s[i] != '#') {
				i++
			}
			if i == start {
				i += n
			}
		}
		out = append(out, Token{kind, start, i, line})
		line += strings.Count(s[start:i], "\n")
	}
	return out
}

// blockComment is the end of the /* */ comment at i; PG's nest.
func blockComment(s string, i int, nest bool) int {
	depth := 0
	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "/*") && (nest || depth == 0):
			depth, i = depth+1, i+2
		case strings.HasPrefix(s[i:], "*/"):
			if depth, i = depth-1, i+2; depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(s)
}

// quoted is the end of the literal quoted at i: a doubled quote stands for
// itself, and so does what a backslash escapes when backslash is set.
func quoted(s string, i int, backslash bool) int {
	q := s[i]
	for i++; i < len(s); i++ {
		switch {
		case backslash && s[i] == '\\':
			i++
		case s[i] == q && i+1 < len(s) && s[i+1] == q:
			i++
		case s[i] == q:
			return i + 1
		}
	}
	return len(s)
}

// dollarTag is the $tag$ or $$ that s starts with, "" for none: a tag is an
// identifier without $, so $1 is none.
func dollarTag(s string) string {
	for i := 1; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '$':
			return s[:i+1]
		case r == '_' || unicode.IsLetter(r) || i > 1 && unicode.IsDigit(r):
			i += n
		default:
			return ""
		}
	}
	return ""
}

// number is the end of the number at i: digits with _ between, a
// fraction, an exponent; or 0x, 0o, 0b and their digits; or digits of
// another script, whole runes.
func number(s string, i int) int {
	if s[i] >= utf8.RuneSelf {
		return advance(s, i, unicode.IsDigit)
	}
	digits := func(i int, ok func(byte) bool) int {
		for i < len(s) && (ok(s[i]) || s[i] == '_') {
			i++
		}
		return i
	}
	dec := func(c byte) bool { return c >= '0' && c <= '9' }
	if len(s) > i+2 && s[i] == '0' && strings.IndexByte("xXoObB", s[i+1]) >= 0 {
		return digits(i+2, func(c byte) bool { return dec(c) || strings.IndexByte("abcdefABCDEF", c) >= 0 })
	}
	i = digits(i, dec)
	if i < len(s) && s[i] == '.' {
		i = digits(i+1, dec)
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && dec(s[j]) {
			i = digits(j, dec)
		}
	}
	return i
}

// advance is past the runes from i that are in: whole runes, so a
// full-width space or digit moves it too.
func advance(s string, i int, in func(rune) bool) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !in(r) {
			break
		}
		i += n
	}
	return i
}

// words are the tokens of s that are neither blanks nor comments, and the
// depth of parentheses each is at.
func words(s string, d Dialect) (ts []Token, depth []int) {
	n := 0
	for _, t := range Scan(s, d) {
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if t.Kind == Punct && s[t.Start] == ')' {
			n = max(n-1, 0)
		}
		ts, depth = append(ts, t), append(depth, n)
		if t.Kind == Punct && s[t.Start] == '(' {
			n++
		}
	}
	return ts, depth
}
