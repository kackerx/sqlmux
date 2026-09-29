package sqlkit

import (
	"strconv"
	"strings"
)

// Stmt is a statement, s[Start:End]: from its first token that is not a
// blank or a comment to its last, without the ; that ends it.
type Stmt struct{ Start, End int }

// Statements splits s at each ; outside parentheses (§9.2), the way usql's
// stmt and psql's lexer do; empty statements are dropped.
// ponytail: PG 14's BEGIN ATOMIC … END is cut at the ; in its body, as
// psql tracks and this does not; split such a function by hand.
func Statements(s string, d Dialect) []Stmt {
	var out []Stmt
	ts, depth := words(s, d)
	start := -1
	for i, t := range ts {
		if t.Kind == Punct && s[t.Start] == ';' && depth[i] == 0 {
			if start >= 0 {
				out = append(out, Stmt{start, ts[i-1].End})
			}
			start = -1
			continue
		}
		if start < 0 {
			start = t.Start
		}
	}
	if start >= 0 {
		out = append(out, Stmt{start, ts[len(ts)-1].End})
	}
	return out
}

// StmtAt is the index of the statement pos is in (§9.2): the last one
// starting at pos or before it, the first when pos is before them all; -1
// when there are none.
func StmtAt(stmts []Stmt, pos int) int {
	i := 0
	for i+1 < len(stmts) && stmts[i+1].Start <= pos {
		i++
	}
	return min(i, len(stmts)-1)
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

// IsRead is whether statement s only reads (§9.3): its first word, past
// opening parentheses, is select, show, explain, table, values or desc,
// but a select … into makes a table, a row lock (for update, for share)
// writes, a with that has insert, update, delete or merge anywhere in it
// writes, and so does explain analyze of a statement that writes. For
// hints only: the database guards the data (§13).
func IsRead(s string, d Dialect) bool {
	ts, depth := words(s, d)
	return isRead(s, ts, depth)
}

func isRead(s string, ts []Token, depth []int) bool {
	w := func(i int) string {
		if i < len(ts) && (ts[i].Kind == Ident || ts[i].Kind == Keyword) {
			return strings.ToLower(s[ts[i].Start:ts[i].End])
		}
		return ""
	}
	i := 0
	for i < len(ts) && s[ts[i].Start:ts[i].End] == "(" {
		i++
	}
	switch w(i) {
	case "show", "desc", "table", "values":
		return true
	case "explain":
		return explainRead(s, ts[i+1:], depth[i+1:])
	case "select", "with":
	default:
		return false
	}
	with := w(i) == "with"
	for j := i; j < len(ts); j++ {
		switch w(j) {
		case "into":
			if depth[j] == depth[i] {
				return false
			}
		case "insert", "update", "delete", "merge":
			if with {
				return false
			}
		case "for": // for update, for no key update, for share, for key share
			if k := w(j + 1); k == "update" || k == "share" || k == "no" || k == "key" {
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
	word := func(i int) string {
		if i < len(ts) {
			return strings.ToLower(s[ts[i].Start:ts[i].End])
		}
		return ""
	}
	for {
		switch word(i) {
		case "analyze", "analyse":
			analyze, i = true, i+1
			continue
		case "verbose":
			i++
			continue
		case "(":
			for i++; i < len(ts) && word(i) != ")"; i++ {
				if w := word(i); w == "analyze" || w == "analyse" {
					v := word(i + 1)
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
// reads, select, with, table or values, with no LIMIT or FETCH of its own
// outside parentheses; a ; at its end goes. The LIMIT is on a line of its
// own, so a -- comment at the end does not take it in.
func AutoLimit(s string, d Dialect, n int) string {
	ts, depth := words(s, d)
	i := 0
	for i < len(ts) && s[ts[i].Start:ts[i].End] == "(" {
		i++
	}
	if i == len(ts) || !isRead(s, ts, depth) {
		return s
	}
	switch strings.ToLower(s[ts[i].Start:ts[i].End]) {
	case "select", "with", "table", "values":
	default:
		return s
	}
	for j, t := range ts {
		if w := strings.ToLower(s[t.Start:t.End]); depth[j] == 0 && (w == "limit" || w == "fetch") {
			return s
		}
	}
	if last := ts[len(ts)-1]; s[last.Start:last.End] == ";" {
		s = s[:last.Start] + s[last.End:]
	}
	return s + "\nLIMIT " + strconv.Itoa(n)
}
