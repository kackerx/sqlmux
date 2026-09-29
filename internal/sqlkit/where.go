package sqlkit

import "strings"

// SelectLike reports whether s is a statement to declare a cursor for
// (§12 快速 SQL): its first word, past spaces, comments and opening
// parentheses, is select, values, table or with. It follows PG 16 psql's
// is_select_command (common.c, for FETCH_COUNT; PG 17 dropped it), which
// takes select and values only; table and with are added here, a WITH …
// SELECT over a big table being common.
// ponytail: the first word only, so WITH … DELETE goes to DECLARE and fails
// with `syntax error at or near "delete"` rather than the read-only
// transaction's error; M4 F4.3 puts IsRead (§9.3) in its place.
func SelectLike(s string) bool {
	for _, t := range Scan(s, PG) {
		switch w := strings.ToLower(s[t.Start:t.End]); {
		case t.Kind == Space, t.Kind == Comment, w == "(":
		case t.Kind == Ident || t.Kind == Keyword:
			return w == "select" || w == "values" || w == "table" || w == "with"
		default:
			return false
		}
	}
	return false
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
// ponytail: as PG; M5 passes the session's dialect with MySQL.
func WhereContext(s string, pos int) Where {
	ts := Scan(s[:pos], PG)
	w := Where{Start: pos, OK: true}
	if n := len(ts); n > 0 && ts[n-1].End == pos {
		switch last := ts[n-1]; last.Kind {
		case Ident, Keyword, String, Number:
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
	word := func(i int) bool { return i < len(back) && (back[i].Kind == Ident || back[i].Kind == Keyword) }
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
