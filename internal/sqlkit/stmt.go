package sqlkit

import (
	"sort"
	"strconv"
	"strings"
)

// Stmt is a statement, s[Start:End]: from its first token that is not a
// blank or a comment to its last, without the ; that ends it. From Lead
// the cursor is in it (§9.2): the comment lines right above it count.
type Stmt struct{ Start, End, Lead int }

// Statements splits s at each ; outside parentheses (§9.2), the way usql's
// stmt and psql's lexer do; empty statements are dropped.
// ponytail: PG 14's BEGIN ATOMIC … END is cut at the ; in its body, as
// psql tracks and this does not; split such a function by hand.
func Statements(s string, d Dialect) []Stmt {
	all := Scan(s, d)
	ts, depth := meaningful(s, all)
	var out []Stmt
	start := -1
	for i, t := range ts {
		if t.Kind == Punct && s[t.Start] == ';' && depth[i] == 0 {
			if start >= 0 {
				out = append(out, Stmt{Start: start, End: ts[i-1].End})
			}
			start = -1
			continue
		}
		if start < 0 {
			start = t.Start
		}
	}
	if start >= 0 {
		out = append(out, Stmt{Start: start, End: ts[len(ts)-1].End})
	}
	// the lines code is on, and those comments are on
	lines := []int{0}
	for i := range len(s) {
		if s[i] == '\n' {
			lines = append(lines, i+1)
		}
	}
	code, comment := make([]bool, len(lines)), make([]bool, len(lines))
	for _, t := range all {
		for l := t.Line; t.Kind != Space && l <= t.Line+strings.Count(s[t.Start:t.End], "\n"); l++ {
			code[l] = code[l] || t.Kind != Comment
			comment[l] = comment[l] || t.Kind == Comment
		}
	}
	for i := range out {
		st := &out[i]
		st.Lead = st.Start
		l := sort.SearchInts(lines, st.Start+1) - 1
		if strings.TrimSpace(s[lines[l]:st.Start]) != "" {
			continue
		}
		for l > 0 && comment[l-1] && !code[l-1] {
			l--
			st.Lead = lines[l]
		}
	}
	return out
}

// StmtAt is the index of the statement the cursor at pos in s is in (§9.2):
// one that starts on its line, the last there that starts by pos; else the
// last one it is past the Lead of, so a blank line after a statement is in
// it; the first when pos is before them all. -1 when there are none.
func StmtAt(s string, stmts []Stmt, pos int) int {
	from := strings.LastIndexByte(s[:pos], '\n') + 1
	to := len(s)
	if i := strings.IndexByte(s[pos:], '\n'); i >= 0 {
		to = pos + i
	}
	at := -1
	for i, st := range stmts {
		if st.Start >= from && st.Start < to && (at < 0 || st.Start <= pos) {
			at = i
		}
	}
	if at >= 0 || len(stmts) == 0 {
		return at
	}
	i := 0
	for i+1 < len(stmts) && stmts[i+1].Lead <= pos {
		i++
	}
	return i
}

// HasSemicolon is whether s has a ; outside strings and comments: a WHERE
// condition may not end its statement (§9.6).
func HasSemicolon(s string, d Dialect) bool {
	for _, t := range Scan(s, d) {
		if t.Kind == Punct && s[t.Start] == ';' {
			return true
		}
	}
	return false
}

// lower is token i's text in lower case, "" past the ends.
func lower(s string, ts []Token, i int) string {
	if i < 0 || i >= len(ts) {
		return ""
	}
	return strings.ToLower(s[ts[i].Start:ts[i].End])
}

// FirstWord is statement s's first word past opening parentheses, lower
// case: what it does, select or create; "" when it has none.
func FirstWord(s string, d Dialect) string {
	ts, _ := words(s, d)
	return lower(s, ts, first(s, ts))
}

// first is the index of the first token past opening parentheses.
func first(s string, ts []Token) int {
	i := 0
	for lower(s, ts, i) == "(" {
		i++
	}
	return i
}

// IsRead is whether statement s only reads (§9.3): its first word, past
// opening parentheses, is select, show, explain, table, values, desc or
// describe, but a select … into makes a table, a row lock (for update,
// for share, lock in share mode) writes, a with that has insert, update,
// delete or merge anywhere in it writes, and so does explain analyze of a
// statement that writes. For hints only: the database guards the data
// (§13).
func IsRead(s string, d Dialect) bool {
	ts, depth := words(s, d)
	return isRead(s, ts, depth)
}

func isRead(s string, ts []Token, depth []int) bool {
	i := first(s, ts)
	switch lower(s, ts, i) {
	case "show", "desc", "describe", "table", "values":
		return true
	case "explain":
		return explainRead(s, ts[i+1:], depth[i+1:])
	case "select", "with":
	default:
		return false
	}
	with := lower(s, ts, i) == "with"
	for j := i; j < len(ts); j++ {
		switch lower(s, ts, j) {
		case "into": // wherever it is: PG takes it nowhere but in the first select
			return false
		case "insert", "update", "delete", "merge":
			if with {
				return false
			}
		case "for": // for update, for no key update, for share, for key share
			if k := lower(s, ts, j+1); k == "update" || k == "share" || k == "no" || k == "key" {
				return false
			}
		case "lock": // MySQL's lock in share mode
			if lower(s, ts, j+1) == "in" && lower(s, ts, j+2) == "share" && lower(s, ts, j+3) == "mode" {
				return false
			}
		}
	}
	return true
}

// explainRead is whether EXPLAIN reads, ts following it: ANALYZE runs the
// statement, so it reads as that does; in (ANALYZE false) and the like it
// is off.
func explainRead(s string, ts []Token, depth []int) bool {
	analyze, i := false, 0
	for {
		switch lower(s, ts, i) {
		case "analyze", "analyse":
			analyze, i = true, i+1
			continue
		case "verbose":
			i++
			continue
		case "(":
			for i++; i < len(ts) && lower(s, ts, i) != ")"; i++ {
				if w := lower(s, ts, i); w == "analyze" || w == "analyse" {
					v := lower(s, ts, i+1)
					analyze = v != "false" && v != "off" && v != "0"
				}
			}
			i++
			continue
		}
		break
	}
	if !analyze || i >= len(ts) {
		return true
	}
	return isRead(s, ts[i:], depth[i:])
}

// AutoLimit is s with LIMIT n after it (§9.4) when it is a query that
// reads, select, with, table or values, with no LIMIT or FETCH at its top
// level; a ; at its end goes. When the whole query is in parentheses, or
// the one after a WITH list is, its top level is in them. The LIMIT is on
// a line of its own, so a -- comment at the end does not take it in.
func AutoLimit(s string, d Dialect, n int) string {
	ts, depth := words(s, d)
	if !isRead(s, ts, depth) {
		return s
	}
	switch lower(s, ts, first(s, ts)) {
	case "select", "with", "table", "values":
	default:
		return s
	}
	out := s
	if last := ts[len(ts)-1]; s[last.Start:last.End] == ";" {
		out, ts = s[:last.Start]+s[last.End:], ts[:len(ts)-1]
	}
	lo, hi, top := 0, len(ts)-1, 0
	for hi > lo && lower(s, ts, hi) == ")" {
		j := hi - 1
		for j >= lo && depth[j] != depth[hi] { // back to its (
			j--
		}
		if j < lo || lower(s, ts, j) != "(" ||
			j != lo && !(lower(s, ts, lo) == "with" && lower(s, ts, j-1) == ")" && depth[j-1] == depth[j]) {
			break
		}
		lo, hi, top = j+1, hi-1, depth[j]+1
	}
	for j := lo; j <= hi; j++ {
		if w := lower(s, ts, j); depth[j] == top && (w == "limit" || w == "fetch") {
			return s
		}
	}
	return out + "\nLIMIT " + strconv.Itoa(n)
}
