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
	SQLite // read as PG, but for its $tag$ strings, E'…' and nested comments, and with `…` identifiers too (F14.1)
	MSSQL  // read as PG, but for its [name] and "name" identifiers and N'…' strings, @x and #x names, and its GO batches (§9.2, F18.6)
	// ClickHouse is read as MySQL, but for its "name" identifiers, which MySQL
	// takes for strings, a # comment only before a blank or a !, and names
	// that go by their case (§8.9).
	ClickHouse
	Trino // read as PG, but for its $tag$ strings, E'…' and nested comments, none of which it has (§8.10)
	// Oracle is read as PG, but for its q'[…]' and N'…' strings, # in a name,
	// no $tag$ strings, E'…', nested comments or `quoted` names, and its
	// PL/SQL blocks, which a / on a line of its own ends (§8.11).
	Oracle
)

// clickhouseWords are the first words of ClickHouse's statements that write
// and no other dialect has as keywords, so IsWrite sees them (§13); kept apart,
// as a PG column called system is no keyword there.
var clickhouseWords = map[string]bool{"system": true, "attach": true, "detach": true, "exchange": true}

// CaseInsensitive is whether names go by any case, `quoted` ones too: MySQL's
// and SQLite's; PG folds an unquoted name's to lower case and takes a quoted one
// as it is.
// ponytail: MySQL's tables on Linux go by case; SQLite's ASCII letters only
// ClickHouse's names go by case, quoted or not, as PG's quoted ones do;
// Trino's go by none, quoted ones too.
// Oracle's unquoted names fold to upper case, a quoted one is as it is: PG's way, the other case.
func (d Dialect) CaseInsensitive() bool { return d != PG && d != ClickHouse && d != Oracle }

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
//   - PG: a doubled quote in '…', \ in E'…', $tag$…$tag$, "…"
//     identifiers, -- and /* */ that nest;
//   - MySQL: \ and doubled quotes in '…' and "…", `…` identifiers, #, --
//     with a blank after it, /* */ that do not nest; /*! … */ is code, its
//     /*! with the version after it and its */ comments, as the server's
//     lexer reads it (sql_lex.cc): a /*! in one is another, the first */
//     ending both.
//   - MSSQL: doubled quotes in '…' and N'…', [name] (]] for a ]) and "…"
//     identifiers, -- and /* */ that nest, @x and #x among the names, and
//     a GO of its own line as a batch's end, which Statements cuts at (§9.2).
//
// What is left open runs to the end.
// ponytail: /*!90000 … */ for a server older than that is code here, a
// comment to it; after SET sql_mode = 'NO_BACKSLASH_ESCAPES' a \ escapes
// nothing to the server, still something here (§13); so a WHERE in a
// /*!99999 … */ that the server skips counts for Dangerous
func Scan(s string, d Dialect) []Token {
	var out []Token
	line := 0
	exec := false // in MySQL's /*! … */
	for i := 0; i < len(s); {
		start := i
		r, n := utf8.DecodeRuneInString(s[i:])
		kind, tag := Space, ""
		if d == PG && r == '$' {
			tag = dollarTag(s[i:])
		}
		switch {
		case exec && strings.HasPrefix(s[i:], "*/"):
			kind, i, exec = Comment, i+2, false
		case d == MySQL && strings.HasPrefix(s[i:], "/*!"):
			kind, i, exec = Comment, i+3+version(s[i+3:]), true
		case strings.HasPrefix(s[i:], "--") && (d != MySQL || i+2 >= len(s) || s[i+2] <= ' '),
			d == MySQL && r == '#', d == ClickHouse && hashComment(s, i):
			line, _, _ := strings.Cut(s[i:], "\n")
			kind, i = Comment, i+len(line)
		case strings.HasPrefix(s[i:], "/*"):
			kind, i = Comment, blockComment(s, i, d == PG)
		case r == '\'':
			kind, i = String, quoted(s, i, d == MySQL || d == ClickHouse)
		case d == Oracle && qQuote(s, i) > 0: // q'[…]', nq'{…}': up to the delimiter's match and a '
			kind, i = String, qQuote(s, i)
		case (d == MSSQL || d == Oracle) && (r == 'N' || r == 'n') && strings.HasPrefix(s[i+1:], "'"): // N'…'
			kind, i = String, quoted(s, i+1, false)
		case r == '[' && d == MSSQL: // [name], a ] in it doubled
			kind, i = Quoted, bracket(s, i)
		case r == '"' && d != MySQL, r == '`' && d != PG && d != Trino && d != Oracle:
			kind, i = Quoted, quoted(s, i, false)
		case r == '"':
			kind, i = String, quoted(s, i, true)
		case d == PG && (r == 'e' || r == 'E') && strings.HasPrefix(s[i+1:], "'"): // E'…'
			kind, i = String, quoted(s, i+1, true)
		case tag != "":
			kind = String
			if j := strings.Index(s[i+len(tag):], tag); j >= 0 {
				i += len(tag) + j + len(tag)
			} else {
				i = len(s)
			}
		case d == MSSQL && (r == '@' || r == '#'): // @x, @@x, a #temp table
			i = advance(s, i, func(r rune) bool { return identStart(r) || r == '$' || r == '@' || r == '#' || r >= '0' && r <= '9' })
			kind = Ident
		case identStart(r): // before digits and blanks: past ASCII, those are a name's too
			i = advance(s, i, func(r rune) bool { return identStart(r) || r == '$' || r >= '0' && r <= '9' || d == Oracle && r == '#' })
			kind = Ident
			if w := strings.ToLower(s[start:i]); keywords[w] || d == ClickHouse && clickhouseWords[w] {
				kind = Keyword
			}
		case r >= '0' && r <= '9', r == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			kind, i = Number, number(s, i)
		case strings.ContainsRune("()[],;.", r):
			kind, i = Punct, i+n
		case unicode.IsSpace(r):
			i = advance(s, i, unicode.IsSpace)
		default:
			kind = Op
			for i < len(s) && strings.IndexByte("=<>!~+-*/%|&^#@:?", s[i]) >= 0 &&
				!strings.HasPrefix(s[i:], "--") && !strings.HasPrefix(s[i:], "/*") && (d != MySQL || s[i] != '#') && !(d == ClickHouse && hashComment(s, i)) &&
				!(exec && strings.HasPrefix(s[i:], "*/")) {
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

// hashComment is whether a ClickHouse comment starts at i: # before a blank,
// the end, or a ! (#! for a script's first line); # is an operator otherwise.
func hashComment(s string, i int) bool {
	return s[i] == '#' && (i+1 >= len(s) || s[i+1] <= ' ' || s[i+1] == '!')
}

// qQuote is the end of Oracle's alternative quoting at i, q'X…X' or nq'X…X'
// (X a bracket, its match closing it, or any other character itself), 0 when
// none starts there; one left open runs to the end.
func qQuote(s string, i int) int {
	j := i
	if j < len(s) && (s[j] == 'n' || s[j] == 'N') {
		j++
	}
	if j+2 >= len(s) || s[j] != 'q' && s[j] != 'Q' || s[j+1] != '\'' {
		return 0
	}
	open := s[j+2]
	closing := map[byte]byte{'[': ']', '{': '}', '(': ')', '<': '>'}[open]
	if closing == 0 {
		closing = open
	}
	if k := strings.Index(s[j+3:], string(closing)+"'"); k >= 0 {
		return j + 3 + k + 2
	}
	return len(s)
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

// version is the length of the version s, what follows a /*!, starts
// with: 5 digits, 6 when a blank follows them; 0 for none.
func version(s string) int {
	n := 0
	for n < min(len(s), 6) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	switch {
	case n == 6 && len(s) > 6 && unicode.IsSpace(rune(s[6])):
		return 6
	case n >= 5:
		return 5
	}
	return 0
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

// bracket is the end of the [name] at i, a ] in it doubled (§9.2).
func bracket(s string, i int) int {
	for i++; i < len(s); i++ {
		if s[i] == ']' {
			if i+1 < len(s) && s[i+1] == ']' {
				i++
				continue
			}
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

// identStart is whether r starts a name, as PG's scan.l has it: a letter,
// _, or any byte past ASCII, a full-width space or ✓ too, so PG reads
// ✓$a$ as a name, not $a$ opening a string (§9.1). MySQL takes those in
// a name as well.
func identStart(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= utf8.RuneSelf
}

// IdentByte reports whether b can be inside an identifier or keyword: a
// letter, digit, _ or $, or a byte of a rune past ASCII.
func IdentByte(b byte) bool {
	return identStart(rune(b)) || b >= '0' && b <= '9' || b == '$'
}

// advance is past the runes from i that are in: whole runes, so a
// rune past ASCII moves it as one.
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
	return meaningful(s, Scan(s, d))
}

func meaningful(s string, all []Token) (ts []Token, depth []int) {
	n := 0
	for _, t := range all {
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
