// Package sqlkit reads SQL text without parsing it (tech-design §9).
// ponytail: only what the WHERE input's completion (M1 F1.5) and quick SQL
// (F1.7) need; M3's scanner (§9.1) grows it for statements, highlighting
// and the console.
package sqlkit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is what a token is.
type Kind int

const (
	Space  Kind = iota
	Word        // an identifier or keyword
	Quoted      // a "quoted identifier"
	String      // a 'string literal', maybe unterminated
	Number
	Op      // = <> != < <= > >= || and the like
	Punct   // ( ) , ; .
	Comment // -- to the line's end, or /* */
)

// Token is s[Start:End], byte offsets.
type Token struct {
	Kind       Kind
	Start, End int
}

// Tokens splits s, after lazysql's components/sql_lexer.go: ” and ""
// escape their quote, and what is left open runs to the end.
func Tokens(s string) []Token {
	var out []Token
	for i := 0; i < len(s); {
		start := i
		r, n := utf8.DecodeRuneInString(s[i:])
		kind := Space
		switch {
		case strings.HasPrefix(s[i:], "--"):
			kind, i = Comment, strings.IndexByte(s[i:]+"\n", '\n')+i
		case strings.HasPrefix(s[i:], "/*"):
			kind = Comment
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = len(s)
			}
		case r == '\'' || r == '"':
			kind = String
			if r == '"' {
				kind = Quoted
			}
			i += n
			for i < len(s) {
				if s[i] == byte(r) {
					if i+1 < len(s) && s[i+1] == byte(r) { // doubled: an escaped quote
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case unicode.IsDigit(r):
			kind, i = Number, advance(s, i, func(r rune) bool { return unicode.IsDigit(r) || r == '.' })
		case r == '_' || unicode.IsLetter(r):
			kind, i = Word, advance(s, i, func(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) })
		case strings.ContainsRune("()[],;.", r):
			kind, i = Punct, i+n
		case unicode.IsSpace(r):
			i = advance(s, i, unicode.IsSpace)
		default:
			kind = Op
			for i < len(s) && strings.ContainsRune("=<>!~+-*/%|&^#@:", rune(s[i])) && !strings.HasPrefix(s[i:], "--") {
				i++
			}
			if i == start {
				i += n
			}
		}
		out = append(out, Token{kind, start, i})
	}
	return out
}

// SelectLike reports whether s is a statement a cursor can be declared for:
// its first word, past spaces, comments and opening parentheses, is select,
// values, table or with, as psql's FETCH_COUNT tells (common.c
// is_select_command) (§12 快速 SQL).
// ponytail: the first word only; M3's read / write judgment (§9.3) replaces it.
func SelectLike(s string) bool {
	for _, t := range Tokens(s) {
		switch w := strings.ToLower(s[t.Start:t.End]); {
		case t.Kind == Space, t.Kind == Comment, w == "(":
		case t.Kind == Word:
			return w == "select" || w == "values" || w == "table" || w == "with"
		default:
			return false
		}
	}
	return false
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

// Where is what the cursor of a WHERE input is at (§9.7): Prefix, from
// Start, is the word or the value being typed that a completion replaces;
// Column is set where a value of that column goes (col =, col <>,
// col in ( … ,). OK is false where nothing completes: in a comment or a
// quoted identifier, or a string outside a value's place.
type Where struct {
	Prefix string
	Start  int
	Column string
	OK     bool
}

// WhereContext reads s up to its byte offset pos.
func WhereContext(s string, pos int) Where {
	ts := Tokens(s[:pos])
	w := Where{Start: pos, OK: true}
	if n := len(ts); n > 0 && ts[n-1].End == pos {
		switch last := ts[n-1]; last.Kind {
		case Word, String, Number:
			w.Prefix, w.Start, ts = s[last.Start:pos], last.Start, ts[:n-1]
		case Comment, Quoted:
			return Where{}
		}
	}
	// the meaningful tokens before the prefix, last first
	var back []Token
	for i := len(ts) - 1; i >= 0; i-- {
		if ts[i].Kind != Space && ts[i].Kind != Comment {
			back = append(back, ts[i])
		}
	}
	text := func(i int) string {
		if i < len(back) {
			return strings.ToLower(s[back[i].Start:back[i].End])
		}
		return ""
	}
	word := func(i int) bool { return i < len(back) && back[i].Kind == Word }
	switch {
	case (text(0) == "=" || text(0) == "<>" || text(0) == "!=") && word(1):
		w.Column = s[back[1].Start:back[1].End]
	default: // in ( a, b, … : back to the (
		i := 0
		for text(i) == "," && i+1 < len(back) && back[i+1].Kind != Punct && back[i+1].Kind != Op {
			i += 2
		}
		if text(i) == "(" && text(i+1) == "in" && word(i+2) {
			w.Column = s[back[i+2].Start:back[i+2].End]
		}
	}
	if w.Column == "" && strings.HasPrefix(w.Prefix, "'") { // a string that is no value of a column
		return Where{}
	}
	return w
}
