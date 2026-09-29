package sqlkit

import "strings"

// CompKind is what goes where the cursor is (§9.7).
type CompKind int

const (
	CompAny     CompKind = iota // the statement's tables' columns, tables, keywords
	CompTables                  // after FROM, JOIN, UPDATE, INTO, TABLE, or a comma of FROM's list
	CompColumns                 // after Qualifier.: its columns, or its tables when it is a schema
)

// TableRef is a table a statement names, as Schema.Name, and the alias it
// gets there; Depth is the parentheses it is in.
type TableRef struct {
	Schema, Name, Alias string
	Depth               int
}

// Completion is what the cursor is at, for completion (§9.7).
type Completion struct {
	OK        bool // a place to complete: not in a string, a comment or a quoted name
	Kind      CompKind
	Prefix    string // the word the cursor is at the end of, maybe ""
	Start     int    // where Prefix starts in the text
	Qualifier string // the X of X.Prefix, quotes off
	Depth     int    // the parentheses the cursor is in
	Tables    []TableRef
	CTEs      []string
}

// CompletionContext tells what the cursor at pos in text is at (§9.7):
// within the statement it is in, by the ; around it, the words before it,
// the tables the statement names with their aliases, and its WITH's names.
// It reads tokens only, after lazysql's components/sql_context.go: no
// grammar, so a guess past what it knows.
func CompletionContext(text string, pos int, d Dialect) Completion {
	all := Scan(text, d)
	for _, t := range all {
		inside := t.Start < pos && (pos < t.End || pos == t.End && t.Kind == Comment && !strings.HasSuffix(text[t.Start:t.End], "*/"))
		if inside && (t.Kind == String || t.Kind == Comment || t.Kind == Quoted) {
			return Completion{}
		}
	}
	ts, depth := meaningful(text, all)
	// the statement: between the ; before pos and the one after it
	lo, hi := 0, len(ts)
	for i, t := range ts {
		if t.Kind == Punct && text[t.Start] == ';' && depth[i] == 0 {
			if t.End <= pos {
				lo = i + 1
			} else if hi == len(ts) {
				hi = i
			}
		}
	}
	ts, depth = ts[lo:hi], depth[lo:hi]
	c := Completion{OK: true, Start: pos}
	before := len(ts) // the tokens before the prefix: ts[:before]
	for i, t := range ts {
		if t.End > pos {
			before = i
			break
		}
	}
	if before > 0 {
		if t := ts[before-1]; t.End == pos && (t.Kind == Ident || t.Kind == Keyword) {
			c.Prefix, c.Start, before = text[t.Start:pos], t.Start, before-1
		}
	}
	for _, t := range ts[:before] {
		switch text[t.Start:t.End] {
		case "(":
			c.Depth++
		case ")":
			c.Depth = max(c.Depth-1, 0)
		}
	}
	word := func(i int) string {
		if i < 0 || i >= len(ts) {
			return ""
		}
		return strings.ToLower(text[ts[i].Start:ts[i].End])
	}
	switch p := before - 1; {
	case word(p) == "." && p > 0 && ts[p-1].End == ts[p].Start && ts[p-1].Kind != Punct:
		c.Kind, c.Qualifier = CompColumns, unquote(text[ts[p-1].Start:ts[p-1].End])
	case word(p) == "from" || word(p) == "join" || word(p) == "update" || word(p) == "into" || word(p) == "table":
		c.Kind = CompTables
	case word(p) == ",":
		for j := p - 1; j >= 0; j-- { // a FROM list's comma: its clause is FROM
			if depth[j] != depth[p] {
				continue
			}
			if w := word(j); fromEnds[w] || w == "select" || w == "from" {
				if w == "from" {
					c.Kind = CompTables
				}
				break
			}
		}
	}
	for i := 0; i < len(ts); i++ {
		switch w := word(i); {
		case w == "from" || w == "join" || w == "update" || w == "into":
			i = c.readTables(text, ts, depth, i+1) - 1
		case w == "with" && depth[i] == 0: // the names; the FROMs in them are read on
			c.readCTEs(text, ts, i+1)
		}
	}
	return c
}

// fromEnds are the words a FROM list stops at.
var fromEnds = map[string]bool{
	"where": true, "group": true, "order": true, "having": true, "limit": true, "offset": true,
	"union": true, "intersect": true, "except": true, "returning": true, "values": true, "set": true,
	"on": true, "using": true, "join": true, "inner": true, "left": true, "right": true, "cross": true,
	"full": true, "outer": true, "natural": true, "lateral": true, "window": true, "for": true,
	"select": true, "(": true, ")": true, "as": true,
}

// readTables reads the table list from ts[i] on, "schema.name [as] alias"
// by commas, into c.Tables, and returns where it stopped.
func (c *Completion) readTables(text string, ts []Token, depth []int, i int) int {
	w := func(i int) string {
		if i >= len(ts) {
			return ""
		}
		return strings.ToLower(text[ts[i].Start:ts[i].End])
	}
	for i < len(ts) {
		if w(i) == "only" || w(i) == "lateral" { // FROM ONLY t
			i++
		}
		if i >= len(ts) || ts[i].Kind == Punct || ts[i].Kind == Op || fromEnds[w(i)] {
			return i
		}
		ref := TableRef{Name: unquote(text[ts[i].Start:ts[i].End]), Depth: depth[i]}
		i++
		if w(i) == "." && i+1 < len(ts) {
			ref.Schema, ref.Name, i = ref.Name, unquote(text[ts[i+1].Start:ts[i+1].End]), i+2
		}
		if w(i) == "(" { // INTO's columns, or a function FROM calls: no alias, no more
			c.Tables = append(c.Tables, ref)
			return i
		}
		if w(i) == "as" {
			i++
		}
		if i < len(ts) && (ts[i].Kind == Ident || ts[i].Kind == Quoted) {
			ref.Alias, i = unquote(text[ts[i].Start:ts[i].End]), i+1
		}
		c.Tables = append(c.Tables, ref)
		if w(i) != "," {
			return i
		}
		i++
	}
	return i
}

// readCTEs reads WITH's "name [(cols)] as (…)" by commas from ts[i] on
// into c.CTEs.
func (c *Completion) readCTEs(text string, ts []Token, i int) {
	skip := func(i int) int { // past the parentheses opening at i
		for n := 0; i < len(ts); i++ {
			switch text[ts[i].Start:ts[i].End] {
			case "(":
				n++
			case ")":
				if n--; n == 0 {
					return i + 1
				}
			}
		}
		return i
	}
	if i < len(ts) && strings.EqualFold(text[ts[i].Start:ts[i].End], "recursive") {
		i++
	}
	for i < len(ts) && (ts[i].Kind == Ident || ts[i].Kind == Quoted) {
		c.CTEs = append(c.CTEs, unquote(text[ts[i].Start:ts[i].End]))
		i++
		if i < len(ts) && text[ts[i].Start:ts[i].End] == "(" {
			i = skip(i)
		}
		if i < len(ts) && strings.EqualFold(text[ts[i].Start:ts[i].End], "as") {
			i++
		}
		i = skip(i) // its query
		if i >= len(ts) || text[ts[i].Start:ts[i].End] != "," {
			return
		}
		i++
	}
}

// unquote is a name without the quotes around it.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '`') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
