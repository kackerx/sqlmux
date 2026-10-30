package app

import (
	"cmp"
	"image/color"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// completion is the candidate list under a WHERE or a quick SQL being
// typed (§9.7, §12).
type completion struct {
	items []candidate
	sel   int  // the first as it opens: ↵ takes it; -1 when nothing is typed and it came up on its own, ↵ then is the input's (§9.7, F8.17)
	start int  // where the text it replaces starts in the input
	moved bool // the user moved the selection: a list made again for columns that came keeps the pick
	top   int  // the first candidate shown, held while the selection is in view (F13.10)
}

// candKind is what a candidate is, which the icon before it says (F16.11).
type candKind int

const (
	kindNone candKind = iota
	kindKeyword
	kindValue
	kindSnippet
	kindTable
	kindView
	kindColumn
	kindKey     // a Redis key
	kindCommand // a Redis command
)

// candIcon is the icon and its color of kind k: the
// keyword, value and snippet ones those of the code, the others as in the
// tree (§7.7).
func (a *App) candIcon(k candKind) ui.Icon {
	ic, th := a.icons, a.theme
	var i ui.Icon
	var fg color.Color
	switch k {
	case kindKeyword:
		i, fg = ic.Keyword, th.Keyword
	case kindValue:
		i, fg = ic.Value, th.String
	case kindSnippet:
		i, fg = ic.Snippet, th.Info
	case kindTable:
		i, fg = ic.Table, th.Func
	case kindView:
		i, fg = ic.View, th.Func
	case kindColumn:
		i, fg = ic.Column, th.Dim
	case kindKey:
		i, fg = ic.Key, th.PK
	case kindCommand:
		i, fg = ic.Command, th.Func
	default:
		return ui.Icon{}
	}
	i.Fg = cmp.Or(i.Fg, fg)
	return i
}

type candidate struct {
	label, insert, note string
	pos                 []int
	kind                candKind // the icon before it; kindKeyword: a keyword, not by what the list says of it
	space               bool     // a word, not a value, a function or a list's item, so the next word comes after a space (§9.7)
	stops               []int    // a snippet: the tabstops, each empty, at these bytes of insert (F15.11)
}

// whereKeywords are what a condition is made of besides columns (§9.7).
var whereKeywords = []string{"and", "or", "not", "is null", "is not null", "in", "like", "ilike", "between", "true", "false", "null"}

// complete finds the candidates for where t's WHERE cursor is: columns and
// keywords while a word is typed, an enum's or a boolean's values where a
// value of that column goes (§9.7).
func (a *App) complete(t *dataTab) {
	t.comp = nil
	if !t.conn.sql() { // a MATCH pattern has no completion (§5)
		return
	}
	in := t.where
	w := sqlkit.WhereContext(in.Text, in.Pos, t.conn.eng.Dialect())
	if !w.OK {
		return
	}
	var groups [][]candidate
	pattern := w.Prefix
	if w.Column != "" {
		var vals []candidate
		for _, c := range t.cols.Cols {
			if strings.EqualFold(c.Name, w.Column) {
				vals = append(vals, valueChoices(c, t.conn.eng.Dialect())...)
			}
		}
		groups, pattern = [][]candidate{vals}, strings.TrimLeft(w.Prefix, `'"`) // MySQL's "…" too
	} else {
		if w.Prefix == "" && !w.WantsColumn { // nothing typed yet: no list
			return
		}
		var cols, kws []candidate
		for _, c := range t.cols.Cols {
			cols = append(cols, candidate{label: c.Name, insert: t.conn.ident(c.Name), note: c.Type, space: true, kind: kindColumn})
		}
		for _, k := range whereKeywords {
			if k != "ilike" || t.conn.eng.Dialect() == sqlkit.PG { // PG's alone
				kws = append(kws, candidate{label: k, insert: k, note: i18n.T("关键字"), kind: kindKeyword, space: sqlkit.SpaceAfter(k)})
			}
		}
		if w.WantsColumn { // the column is next, not a keyword
			kws = nil
		}
		groups = [][]candidate{cols, kws}
	}
	t.comp = ranked(in.Text, in.Pos, pattern, w.Start, groups...)
	if t.comp != nil && pattern == "" && w.Column == "" { // a column's values are what is asked for after its operator: the first is picked (F8.17)
		t.comp.sel = noSelection
	}
}

// valueChoices are the values of col that a literal can be: an enum's labels
// quoted, a boolean's true and false (§9.7).
func valueChoices(col db.Column, d sqlkit.Dialect) (out []candidate) {
	labels, _ := choices(col)
	for _, l := range labels {
		insert := l
		if len(col.Enum) > 0 {
			insert = sqlkit.Literal(l, d)
		}
		out = append(out, candidate{label: l, insert: insert, note: i18n.T("值"), kind: kindValue})
	}
	return out
}

// valueCandidates are what the value of col can be where a statement sets or
// compares it (F15.10): the cell options' (§10.2): a time's now() and
// current_timestamp, an enum's labels quoted or a boolean's, NULL when it takes
// one, DEFAULT when it has one, both in an INSERT or an UPDATE and not a
// condition, where `= null` and `in (null)` are never true.
// ponytail: SQLite has no now(); a date or a time gets the timestamp too
func valueCandidates(col db.Column, kind sqlkit.ValueKind, d sqlkit.Dialect) []candidate {
	var out []candidate
	if timeKind(col.Type) != ui.NotTime {
		if d != sqlkit.SQLite {
			out = append(out, candidate{label: "now()", insert: "now()", note: i18n.T("值"), kind: kindValue})
		}
		out = append(out, candidate{label: "current_timestamp", insert: "current_timestamp", note: i18n.T("值"), kind: kindValue})
	}
	out = append(out, valueChoices(col, d)...)
	if !col.NotNull && kind != sqlkit.ValueWhere {
		out = append(out, candidate{label: "null", insert: "null", note: i18n.T("关键字"), kind: kindKeyword})
	}
	if kind != sqlkit.ValueWhere && hasDefault(col, kind == sqlkit.ValueInsert) {
		out = append(out, candidate{label: "default", insert: "default", note: i18n.T("关键字"), kind: kindKeyword})
	}
	return out
}

// noSelection is the sel of a list that has no item selected yet: Tab, C-n or
// ↓ select the first, S-Tab, C-p or ↑ the last (F8.17, nvim's noselect).
const noSelection = -1

// ranked is the candidates of groups that pattern matches, completing what
// starts at start in the text, the cursor at pos; nil when none does. By how
// closely they match first (§9.7, F12.6): one that is just what is typed, or
// a table's name would take the place of a finished set or to; one that
// starts with it; one with a word that does; the rest. Within each, group by
// group and best first within each group. Pattern's first character matches at a word start,
// fuzzy past it as VS Code's does: evt finds mt_event and tord t_order, but
// x no longer max.
func ranked(text string, pos int, pattern string, start int, groups ...[]candidate) *completion {
	c := &completion{start: start}
	// no smart case: an upper-case letter would make fzf exact about case
	// (§9.7); the palette, tree and COLS keep it
	pattern = strings.ToLower(pattern)
	var tiers [4][]candidate
	add := func(cd candidate, from int) {
		t := tierOf(pattern, cd.label)
		if pattern != "" && strings.EqualFold(text[from:pos], cd.insert) {
			t = 0
		}
		tiers[t] = append(tiers[t], cd)
	}
	for _, g := range groups {
		labels := make([]string, len(g))
		cover := make([]int, len(g)) // where each one replaces from
		for i, cd := range g {
			labels[i], cover[i] = cd.label, c.startOf(text, cd)
		}
		matched := make([]bool, len(g))
		for _, m := range ui.Filter(pattern, labels) {
			cd := g[m.Index]
			if cd.pos = atWordStart(pattern, cd.label, m.Pos); pattern != "" && cd.pos == nil {
				continue
			}
			matched[m.Index] = true
			add(cd, cover[m.Index])
		}
		// a keyword of several words, all typed: a word after a space is no
		// word start, so rename to is not found by the to, and is what is
		// typed (§9.7)
		for i, cd := range g {
			if cover[i] != start && !matched[i] && strings.EqualFold(text[cover[i]:pos], cd.insert) {
				cd.pos = nil
				for j := range []rune(cd.label) {
					cd.pos = append(cd.pos, j)
				}
				add(cd, cover[i])
			}
		}
	}
	if c.items = slices.Concat(tiers[:]...); len(c.items) == 0 {
		return nil
	}
	return c
}

// tierOf is how closely label matches pattern (lower case), 1 the closest
// of what is not the same: it starts with it, 2 a word of it does, 3 the
// rest, as fuzzy as fzf was (§9.7). With nothing typed all are alike.
// Reference: lazysql components/sql_completer.go scores each candidate by how
// close its match is (exact, prefix, the rest) and breaks a tie by group
// (column, table, keyword), as here; it has the one group order everywhere,
// where a statement's start puts keywords first here, and it drops a candidate
// whose text was seen and cuts the list at 20.
func tierOf(pattern, label string) int {
	if pattern == "" {
		return 3
	}
	rs := []rune(label)
	for i := range rs {
		if (i == 0 || wordStart(rs, i)) && strings.HasPrefix(strings.ToLower(string(rs[i:])), pattern) {
			return min(i, 1) + 1
		}
	}
	return 3
}

// startOf is where the text cd replaces starts: the list's, or further back
// when cd is a keyword of several words and its first are written already
// (insert in, the list's word is in): those are taken in, not typed again.
func (c *completion) startOf(text string, cd candidate) int {
	words := strings.Fields(cd.insert)
	if cd.kind != kindKeyword || len(words) < 2 {
		return c.start
	}
	for n := len(words) - 1; n >= 1; n-- {
		s := c.start
		for k := n - 1; k >= 0; k-- {
			e := s
			for e > 0 && (text[e-1] == ' ' || text[e-1] == '\t') {
				e--
			}
			w := words[k]
			if e == s || e < len(w) || !strings.EqualFold(text[e-len(w):e], w) || e > len(w) && sqlkit.IdentByte(text[e-len(w)-1]) {
				s = -1
				break
			}
			s = e - len(w)
		}
		if s >= 0 {
			return s
		}
	}
	return c.start
}

// atWordStart is where pattern matches s with its first character at a
// word start (§9.7), given fzf's best match at pos; nil when it can't.
// fzf's match starts elsewhere when a run of consecutive characters
// outscores the word start, and pos[0] may be a later term's when a space
// splits the pattern (a value typed, 'in p'): the rest is matched again
// from each word start.
func atWordStart(pattern, s string, pos []int) []int {
	rs := []rune(s)
	if len(pos) == 0 || !strings.Contains(pattern, " ") && wordStart(rs, pos[0]) {
		return pos
	}
	first, n := utf8.DecodeRuneInString(pattern)
	for i, r := range rs {
		if !wordStart(rs, i) || unicode.ToLower(r) != first {
			continue
		}
		if ms := ui.Filter(pattern[n:], []string{string(rs[i+1:])}); len(ms) > 0 {
			out := []int{i}
			for _, p := range ms[0].Pos {
				out = append(out, i+1+p)
			}
			return out
		}
	}
	return nil
}

// wordStart reports whether rune i of rs starts a word: the first, one
// after _ . - or $, or an upper case after a lower case.
func wordStart(rs []rune, i int) bool {
	return i == 0 || strings.ContainsRune("_.-$", rs[i-1]) || unicode.IsLower(rs[i-1]) && unicode.IsUpper(rs[i])
}

// ident is name as SQL text on c: quoted when it has to be (F15.11).
func (c *Conn) ident(name string) string {
	if c.eng.Dialect().NeedsQuote(name) {
		return c.eng.Quote(name)
	}
	return name
}

// table is c's table name of schema, as PG folds what isn't quoted.
func (c *Conn) table(schema, name string) (db.Table, bool) {
	for _, t := range c.Tables {
		if strings.EqualFold(t.Schema, schema) && strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return db.Table{}, false
}

// sqlNames tells the names in SQL text on c apart for their colors
// (§7.3): a table is one of c's, any schema's; a column is one of the
// tables its statement names, of schema when unqualified, or with cols
// given, one of those (a WHERE's table's). missing is the statements'
// tables whose columns the cache lacks.
// ponytail: by the tokens alone: a CTE or an alias named as a table is
// colored as one, as is a column; a table's name wins over a column's
func (a *App) sqlNames(c *Conn, text, schema string, cols *db.Columns) (names ui.SQLNames, missing []db.Table) {
	// PG folds a name's ASCII letters unless it is "quoted"
	// (downcase_identifier), its catalog's as they are; MySQL's and SQLite's go
	// by any case on both sides, quoted too
	if !c.sql() {
		return
	}
	fold := func(s string) string { return s }
	if c.eng.Dialect().CaseInsensitive() {
		fold = strings.ToLower
	}
	tables := map[string]bool{}
	for _, t := range c.Tables {
		tables[fold(t.Name)] = true
	}
	type span struct {
		end  int
		cols map[string]bool
	}
	var spans []span // by statement, in order
	add := func(end int, cs db.Columns) {
		if len(spans) == 0 || spans[len(spans)-1].end != end {
			spans = append(spans, span{end, map[string]bool{}})
		}
		for _, c := range cs.Cols {
			spans[len(spans)-1].cols[fold(c.Name)] = true
		}
	}
	var stmts []sqlkit.Stmt
	if cols != nil {
		add(math.MaxInt, *cols)
	} else {
		stmts = sqlkit.Statements(text, c.eng.Dialect())
	}
	for _, st := range stmts {
		add(st.End, db.Columns{})
		cc := sqlkit.CompletionContext(text[st.Start:st.End], 0, c.eng.Dialect())
		for _, r := range cc.Tables {
			if r.Schema == "" && slices.ContainsFunc(cc.CTEs, func(n string) bool { return strings.EqualFold(n, r.Name) }) {
				continue
			}
			t, ok := c.table(cmp.Or(r.Schema, schema), r.Name)
			cs, cached := c.cols[idOf(t)]
			if ok && !cached {
				missing = append(missing, t)
			}
			add(st.End, cs)
		}
	}
	schemas := map[string]bool{}
	for _, s := range c.Schemas {
		schemas[fold(s)] = true
	}
	return func(at int, word string) ui.SQLName {
		name, q := word, word[:1]
		if q == `"` || q == "`" {
			name = strings.ReplaceAll(strings.TrimSuffix(word[1:], q), q+q, q)
		}
		if q != `"` {
			name = strings.Map(func(r rune) rune {
				if 'A' <= r && r <= 'Z' {
					r += 'a' - 'A'
				}
				return r
			}, name)
		}
		name = fold(name)
		i := sort.Search(len(spans), func(i int) bool { return spans[i].end > at })
		switch {
		case schemas[name] && strings.HasPrefix(text[min(at+len(word), len(text)):], "."): // s of s.t: a schema's, whatever table shares its name
			return ui.OtherName
		case tables[name]:
			return ui.TableName
		case i < len(spans) && spans[i].cols[name]:
			return ui.ColumnName
		}
		return ui.OtherName
	}, missing
}

// wantCols asks for the columns of the tables the queries on screen name
// that the cache lacks, and the palette's previewed view does, for their
// names' colors (§7.3, F4.2): each once, till the cache is dropped.
// ponytail: the queries are scanned after every message, as they are
// drawn; keep the tables a query names per change if a big file lags
func (a *App) wantCols() tea.Cmd {
	var cmds []tea.Cmd
	want := func(c *Conn, text, schema string) {
		_, missing := a.sqlNames(c, text, schema, nil)
		for _, t := range missing {
			if !c.colsAsked[idOf(t)] {
				c.colsAsked[idOf(t)] = true
				cmds = append(cmds, a.fetchCols(c, t))
			}
		}
	}
	for id := range a.layout() {
		if t := queryOf(a.win().pane(id)); t != nil {
			want(t.conn, strings.Join(t.ed.Lines(), "\n"), cmp.Or(t.schema, t.conn.Schema))
		}
	}
	if c, text, schema := a.previewedView(); text != "" {
		want(c, text, schema)
	}
	return tea.Batch(cmds...)
}

// sqlComplete is the candidates for the cursor at pos in sql on conn, a
// query's or the quick SQL's (§9.7): by what CompletionContext says goes
// there, tables of schema (the query's; the connection's for the quick
// SQL, §8.6), of
// X. after a schema X, a table's columns
// after its name or alias, else the statement's tables' columns, those at
// the cursor's depth first, the other tables and keywords. It opens with a
// word typed, or right after a qualifier that resolves; manual (C-n) with
// nothing. The columns the cache lacks are fetched, a table once per asked.
// A name that has to be quoted goes in quoted (F15.11).
func (a *App) sqlComplete(conn *Conn, sql string, pos int, schema string, asked map[tableID]bool, manual bool) (*completion, []tea.Cmd) {
	if !conn.sql() {
		return nil, nil
	}
	c := sqlkit.CompletionContext(sql, pos, conn.eng.Dialect())
	vc := sqlkit.ValueContext(sql, pos, conn.eng.Dialect())
	if !c.OK && !vc.OK { // a comment, a quoted name; a string is a value's place maybe
		return nil, nil
	}
	var cmds []tea.Cmd
	table := func(in, name string) (db.Table, bool) { return conn.table(cmp.Or(in, schema), name) }
	columns := func(t db.Table) (out []candidate) {
		cs, ok := conn.cols[idOf(t)]
		if !ok && !asked[idOf(t)] {
			asked[idOf(t)] = true
			cmds = append(cmds, a.fetchCols(conn, t))
		}
		for _, col := range cs.Cols {
			out = append(out, candidate{label: col.Name, insert: conn.ident(col.Name), note: col.Type + " · " + t.Name, space: true, kind: kindColumn})
		}
		return out
	}
	tables := func(in string, skip map[tableID]bool) (out []candidate) {
		for _, t := range conn.Tables {
			if t.Schema == in && !skip[idOf(t)] {
				note, kind := i18n.T("表"), kindTable
				if t.View() {
					note, kind = i18n.T("视图"), kindView
				}
				out = append(out, candidate{label: t.Name, insert: conn.ident(t.Name), note: note, space: true, kind: kind})
			}
		}
		return out
	}
	// a value's place: the column's values, and a string typed there is no other
	// word's (F15.10)
	var vals []candidate
	if vc.OK && vc.Begins && (vc.Kind != sqlkit.ValueInsert || vc.Depth == 1) && (vc.Kind != sqlkit.ValueUpdate || vc.Depth == 0) {
		for _, r := range vc.Tables {
			if t, ok := table(r.Schema, r.Name); ok {
				columns(t) // asks for them when the cache lacks them
			}
		}
		if col, ok := conn.valueColumn(schema, vc); ok {
			vals = valueCandidates(col, vc.Kind, conn.eng.Dialect())
		}
	}
	if !c.OK || vals != nil && vc.Start != c.Start { // in a string, or after a number: the values alone
		if vals == nil {
			return nil, cmds
		}
		pattern := strings.TrimLeft(vc.Prefix, `'"`)
		if vc.Prefix != "" && vc.Prefix != pattern { // in a string: the literals, which end with the quote that is there
			vals = slices.DeleteFunc(vals, func(cd candidate) bool { return vc.Prefix[0] != '\'' || cd.insert[0] != '\'' })
		}
		comp := ranked(sql, pos, pattern, vc.Start, vals)
		if comp != nil && pattern == "" && !manual {
			comp.sel = noSelection
		}
		return comp, cmds
	}
	var ctes []candidate
	isCTE := map[string]bool{}
	for _, n := range c.CTEs {
		ctes, isCTE[strings.ToLower(n)] = append(ctes, candidate{label: n, insert: conn.ident(n), note: "CTE", space: true, kind: kindTable}), true
	}
	// setColumns are the columns of the tables an UPDATE names before its SET at
	// the cursor's depth, a MySQL join's both, each table's in turn
	// known is whether the catalog has one of them: its columns may still be
	// on their way
	setColumns := func() (out []candidate, known bool) {
		named := map[tableID]bool{}
		for _, r := range c.Updated {
			if t, ok := table(r.Schema, r.Name); ok && r.Depth == c.Depth && !named[idOf(t)] {
				named[idOf(t)], known = true, true
				out = append(out, columns(t)...)
			}
		}
		return out, known
	}
	var groups [][]candidate
	resolved := false
	switch c.Kind {
	case sqlkit.CompColumns: // an alias or a table named, a CTE (no columns), a table, a schema
		q := c.Qualifier
		named := func(is func(sqlkit.TableRef) bool) (sqlkit.TableRef, bool) {
			for i := len(c.Tables) - 1; i >= 0; i-- { // at the cursor's depth or out of it, the later first (lazysql's resolveAliases)
				if r := c.Tables[i]; r.Depth <= c.Depth && is(r) {
					return r, true
				}
			}
			return sqlkit.TableRef{}, false
		}
		r, ok := named(func(r sqlkit.TableRef) bool { return strings.EqualFold(r.Alias, q) })
		if !ok {
			r, ok = named(func(r sqlkit.TableRef) bool { return strings.EqualFold(r.Name, q) })
		}
		t, found := table(r.Schema, r.Name)
		switch {
		case ok && r.Schema == "" && isCTE[strings.ToLower(r.Name)], !ok && isCTE[strings.ToLower(q)]:
		case ok && found:
			groups, resolved = [][]candidate{columns(t)}, true
		default: // what the statement names is not a table, or it names none: a table, else a schema (from agentable.)
			if t, ok := table("", q); ok {
				groups, resolved = [][]candidate{columns(t)}, true
			} else if j := slices.IndexFunc(conn.Schemas, func(s string) bool { return strings.EqualFold(s, q) }); j >= 0 {
				groups, resolved = [][]candidate{tables(conn.Schemas[j], nil)}, true
			}
		}
	case sqlkit.CompTables:
		groups = [][]candidate{append(ctes, tables(schema, nil)...)}
	case sqlkit.CompInsertCols: // what the parentheses lack, and all of it at once (§9.7)
		resolved = true
		t, found := table(c.Into.Schema, c.Into.Name)
		if !found {
			break
		}
		columns(t) // fetches them when the cache lacks them
		var rest []candidate
		for _, col := range conn.cols[idOf(t)].Cols {
			if !col.Generated && !slices.ContainsFunc(c.Listed, func(l string) bool { return strings.EqualFold(l, col.Name) }) {
				rest = append(rest, candidate{label: col.Name, insert: conn.ident(col.Name), note: col.Type, kind: kindColumn})
			}
		}
		if len(rest) > 1 && c.Prefix == "" {
			names, inserts := make([]string, len(rest)), make([]string, len(rest))
			for i, r := range rest {
				names[i], inserts[i] = r.label, r.insert
			}
			rest = append([]candidate{{label: ui.Truncate(strings.Join(names, ", "), 40), insert: strings.Join(inserts, ", "), note: i18n.T("全部 %d 列", len(rest)), kind: kindSnippet}}, rest...)
		}
		groups = [][]candidate{rest}
	case sqlkit.CompInsertRest: // the columns and VALUES of the table, the cursor in the parentheses of the values (F15.11)
		resolved = true
		t, found := table(c.Into.Schema, c.Into.Name)
		if !found {
			break
		}
		columns(t) // fetches them when the cache lacks them
		var names, inserts []string
		for _, col := range conn.cols[idOf(t)].Cols {
			if !col.Generated {
				names, inserts = append(names, col.Name), append(inserts, conn.ident(col.Name))
			}
		}
		if len(names) > 0 {
			insert := "(" + strings.Join(inserts, ", ") + ") values ()"
			label := "(" + ui.Truncate(strings.Join(names, ", "), 24) + ") values ()"
			groups = [][]candidate{{{label: label, insert: insert, note: i18n.T("全部 %d 列", len(names)), stops: []int{len(insert) - 1}, kind: kindSnippet}}}
		}
	default:
		if c.InSet { // an UPDATE's SET: the columns of its tables, no other, none while they are asked for (F15.11)
			if cols, known := setColumns(); known {
				groups, resolved = [][]candidate{cols}, true
				break
			}
		}
		var near, far []candidate
		named := map[tableID]bool{}
		for _, r := range c.Tables {
			t, ok := table(r.Schema, r.Name)
			if !ok || named[idOf(t)] || r.Schema == "" && isCTE[strings.ToLower(r.Name)] {
				continue
			}
			named[idOf(t)] = true
			if r.Depth == c.Depth {
				near = append(near, columns(t)...)
			} else {
				far = append(far, columns(t)...)
			}
		}
		if c.WantsColumn && !manual { // a column is next, the statement's tables' alone, though nothing is typed (F8.4); C-n lists all
			groups, resolved = [][]candidate{near, far}, true
			break
		}
		var kws []candidate
		for _, k := range sqlkit.Common(conn.eng.Dialect()) {
			kws = append(kws, candidate{label: k, insert: k, note: i18n.T("关键字"), kind: kindKeyword, space: sqlkit.SpaceAfter(k)})
		}
		groups = [][]candidate{near, far, append(ctes, tables(schema, named)...), kws}
		if c.AtStart { // what is typed there is almost always a keyword (F12.6)
			groups = [][]candidate{kws, groups[2], near, far}
		}
	}
	if vals != nil {
		if groups, resolved = append([][]candidate{vals}, groups...), true; c.Prefix == "" && !manual {
			groups = groups[:1]
		}
	}
	if c.Prefix == "" && !manual && !resolved {
		return nil, cmds
	}
	comp := ranked(sql, pos, c.Prefix, c.Start, groups...)
	if comp != nil && c.Prefix == "" && !manual && c.Kind != sqlkit.CompInsertCols { // came up on its own with nothing typed; (↵ adds all the columns of an INSERT's, F5.13)
		comp.sel = noSelection
	}
	return comp, cmds
}

// completing is the candidate list that is up, if any: the palette's quick
// SQL's, else the focused query's or the WHERE's being typed.
func (a *App) completing() *completion {
	if a.palette != nil {
		return a.palette.comp
	}
	if t := a.focusedQuery(); t != nil {
		return t.comp
	}
	if t := a.typingTab(); t != nil {
		return t.comp
	}
	return nil
}

// acceptCompletion puts the selected candidate in place of what it
// completes and closes the list. ↵ with the list up only ever does this,
// whether or not the text changes (§9.7, M8 F8.1). A query's change is
// saved as typing is (cmd).
func (a *App) acceptCompletion() tea.Cmd {
	if c := a.completing(); c == nil || c.sel < 0 {
		return nil
	}
	if p := a.palette; p != nil {
		space := p.comp.items[p.comp.sel].space
		p.comp.accept(&p.input)
		p.comp = nil
		if space { // the next word may be a column: its list comes up at once (F8.17)
			return a.completeSQL()
		}
		return nil
	}
	if t := a.focusedQuery(); t != nil {
		return a.queryAccept(t)
	}
	a.whereAccept(a.typingTab())
	return nil
}

// whereAccept is accept in the WHERE's vim, as typing it would (F3.39); a
// word with its space after it has the next list come up (F8.17).
func (a *App) whereAccept(t *dataTab) {
	c, in := t.comp, t.where
	t.comp = nil
	space := c.items[c.sel].space
	c.plan(in.Text, in.Pos).put(t.ed)
	t.syncWhere()
	if space {
		a.complete(t)
	}
}

// accepted is what taking a candidate does: ins goes in place of the text
// from start to the cursor, and drop bytes after the cursor go.
type accepted struct {
	start, drop int
	ins         string
}

// plan is what taking the selected candidate does to text with the cursor at
// pos (§9.7): what is typed in the candidate's case stays as it is, a word
// like NULL is not made null; a word gets a space after it, or the cursor
// moves past the whitespace that is there. The closing quote after the
// cursor, autopairs' or typed, goes when the insert has its own (§7.9).
func (c *completion) plan(text string, pos int) accepted {
	cd := c.items[c.sel]
	p := accepted{start: c.startOf(text, cd), ins: cd.insert}
	if typed := text[p.start:pos]; strings.EqualFold(typed, p.ins) {
		p.ins = typed
	}
	after := text[pos:]
	if n := len(p.ins); n > 0 && strings.ContainsRune("'\"`", rune(p.ins[n-1])) && strings.HasPrefix(after, p.ins[n-1:]) {
		p.drop = 1
	}
	if cd.space {
		switch {
		case after != "" && (after[0] == ' ' || after[0] == '\t'):
			p.ins, p.drop = p.ins+after[:1], 1
		case after != "" && strings.IndexByte("),;.", after[0]) >= 0: // what comes next is there: none
		default:
			p.ins += " "
		}
	}
	return p
}

// put does p in e, an INSERT, as typing it would.
func (p accepted) put(e *editor.Editor) editor.Effect {
	eff := e.Complete(p.start, p.ins)
	for range p.drop {
		eff = e.Feed("<Del>")
	}
	return eff
}

// accept takes the selected candidate into in.
func (c *completion) accept(in *ui.Input) {
	p := c.plan(in.Text, in.Pos)
	end := p.start + len(p.ins)
	if stops := c.items[c.sel].stops; len(stops) > 0 { // a snippet in an input with no tabstops: the cursor at the first
		end = p.start + stops[0]
	}
	in.Text, in.Pos = in.Text[:p.start]+p.ins+in.Text[in.Pos+p.drop:], end
}

// move moves the selection by d, around the ends (§9.7).
func (c *completion) move(d int) {
	c.sel, c.moved = step(c.sel, d, len(c.items)), true
}

// step is sel moved by d in a list of n, around the ends; with none
// selected (sel < 0), down selects the first and up the last (§9.7, §10.2).
func step(sel, d, n int) int {
	switch {
	case sel >= 0:
		return ((sel+d)%n + n) % n
	case d > 0:
		return 0
	}
	return n - 1
}

// startWhere gives the keys to t's WHERE input, its vim in INSERT at the
// end (F3.39); made anew, so its undo starts here and goes with it.
func (a *App) startWhere(t *dataTab) {
	t.typing, t.comp, t.hist = "where", nil, nil
	t.ed = editor.New(t.where.Text)
	t.ed.TabWidth, t.ed.AutoPairs, t.ed.OneLine = a.tabWidth, a.autoPairs, true
	if t.ed.SetPlain(a.plain); a.plain {
		t.ed.Goto(editor.Pos{Col: len(t.where.Text)})
	} else {
		t.ed.Feed("A")
	}
	t.syncWhere()
}

// syncWhere shows the WHERE's vim in the input: its line and cursor.
func (t *dataTab) syncWhere() {
	t.where = ui.Input{Text: t.ed.Lines()[0], Pos: t.ed.Cursor().Col}
}

// whereKey gives key k to the WHERE's vim (F3.39): in INSERT ↵ takes the
// selected candidate when the list is up, else runs the WHERE (§9.7),
// and esc leaves INSERT, the list closing with it, as a query's.
func (a *App) whereKey(t *dataTab, k keymap.Key) tea.Cmd {
	if m := t.ed.Mode(); k == "<CR>" && (m == editor.Insert || m == editor.Replace) {
		if t.comp != nil && t.comp.sel >= 0 {
			a.whereAccept(t)
			return nil
		}
		return a.runWhere(t)
	}
	if k == keymap.Esc && t.ed.Plain() { // there is no mode to leave: the list, else the input (§6.9)
		if t.comp != nil {
			t.comp = nil
		} else {
			t.stopTyping()
		}
		return nil
	}
	return a.whereDid(t, t.ed.Feed(string(k)))
}

// whereDid acts on what the WHERE's vim did: the input shows it; in INSERT
// the candidates follow the cursor, or the history list filters by the
// text once typed into (§9.7); the clipboard and a yank's flash as a
// query's. gq has nothing to lay out.
func (a *App) whereDid(t *dataTab, eff editor.Effect) tea.Cmd {
	was := t.where.Text
	t.syncWhere()
	switch {
	case t.hist != nil: // in INSERT or REPLACE, as where.history puts it
		if t.where.Text != was {
			t.hist.typed, t.hist.sel = true, 0
		}
	case t.ed.Mode() == editor.Insert && t.where.Text != was: // only typing opens the list: an arrow does not (§9.7)
		a.complete(t)
	default:
		t.comp = nil
	}
	ed := t.ed
	return tea.Batch(a.vimCmds(eff, func(text string, p editor.ClipPut) tea.Cmd {
		if t.typing != "where" || t.ed != ed { // the edit is over
			return nil
		}
		return a.whereDid(t, ed.PutClip(text, p))
	})...)
}

// whereClick puts the WHERE's cursor where p is on pane pn's input.
// ponytail: the column is the input's, where a tab is one cell, and the
// editor's tabs go to the tabstop: past a pasted tab a click lands short;
// map through the byte offset if tabs ever show up in a WHERE
func (a *App) whereClick(pn *Pane, t *dataTab, p uv.Position) {
	t.ed.Click(0, a.whereCol(pn, t, p))
	t.syncWhere()
	t.comp = nil
	if t.ed.Plain() { // dragging from there selects (§6.9)
		a.dragWhere = pn.ID
	}
}

// whereDrag is the pointer dragged to p with the button down from the WHERE
// of pane id: the selection's far end follows it.
func (a *App) whereDrag(id int, p uv.Position) {
	if pn := a.win().pane(id); pn != nil {
		if t := dataOf(pn); t != nil && t.typing == "where" {
			t.ed.Drag(0, a.whereCol(pn, t, p))
			t.syncWhere()
		}
	}
}

// whereCol is the display column of the WHERE's text that p, a place on
// pane pn's input, is at.
func (a *App) whereCol(pn *Pane, t *dataTab, p uv.Position) int {
	r := a.queryBar(pn, t).InputRect(bodyRect(a.layout()[pn.ID]))
	shown := t.where.Text[:t.where.Start(r.Dx())]
	return ui.Width(ui.Printable(shown)) + p.X - r.Min.X
}

// editPaired is editInput with autopairs, the quick SQL's (§7.9): an
// opening half gets its other, a closing one after the cursor is stepped
// over, and BS takes an empty pair.
func (a *App) editPaired(in *ui.Input, k keymap.Key) bool {
	before, after := in.Text[:in.Pos], in.Text[in.Pos:]
	t := keymap.Text(k)
	switch r, n := utf8.DecodeRuneInString(t); {
	case !a.autoPairs:
	case (k == "<BS>" || k == "<C-h>") && editor.EmptyPair(before, after):
		in.Text, in.Pos = before[:len(before)-1]+after[1:], in.Pos-1
		return true
	case n > 0 && n == len(t):
		switch shut, skip := editor.Pair(before, after, r); {
		case skip:
			in.Pos += n
			return false
		case shut != "":
			in.Insert(t + shut)
			in.Pos -= len(shut)
			return true
		}
	}
	return editInput(in, k)
}

// whereAt is where pane p's WHERE input starts: lists open under it.
func (a *App) whereAt(p *Pane, t *dataTab) uv.Position {
	return a.queryBar(p, t).InputRect(bodyRect(a.layout()[p.ID])).Min
}

// completeView is c as it opens under the input cell at, where what it
// completes starts.
func (a *App) completeView(c *completion, at uv.Position) (ui.Complete, uv.Rectangle, int) {
	v := ui.Complete{Sel: c.sel}
	w := 20
	for _, cd := range c.items {
		icon := a.candIcon(cd.kind)
		v.Items = append(v.Items, ui.CompleteItem{Icon: icon, Text: cd.label, Pos: cd.pos, Note: cd.note})
		w = max(w, ui.Width(icon.Text+" "+cd.label+"  "+cd.note)+4)
	}
	box, rows := ui.CompleteBox(a.window(), at, w, len(v.Items), 0, ui.CompleteRows)
	_, c.top = ui.ListView(c.sel, c.top, len(v.Items), rows)
	v.Top = c.top
	return v, box, rows
}

// histMenu is the WHERE history and favorites list (Q-02, §9.7), all of
// it as it opens, filtered by the WHERE input once typed into.
type histMenu struct {
	sel   int
	top   int // the first row shown, held while the selection is in view (F13.10)
	typed bool
}

// histEntry is one of its rows: a favorite, or a history entry.
type histEntry struct {
	q   config.Query
	fav bool
}

// tableState is t's history and favorites, made on first use.
func (a *App) tableState(t *dataTab) *config.TableState {
	key := t.conn.tableKey(t.table)
	if a.state.Tables == nil {
		a.state.Tables = map[string]*config.TableState{}
	}
	if a.state.Tables[key] == nil {
		a.state.Tables[key] = &config.TableState{}
	}
	return a.state.Tables[key]
}

// histEntries is the history, then the favorites, that the WHERE input
// matches once typed into, newest first in each (F8.14).
func (a *App) histEntries(t *dataTab) (entries []histEntry, pos [][]int) {
	st, pattern := a.tableState(t), ""
	if t.hist != nil && t.hist.typed {
		pattern = t.where.Text
	}
	for _, g := range []struct {
		qs  []config.Query
		fav bool
	}{{st.History, false}, {st.Favorites, true}} {
		wheres := make([]string, len(g.qs))
		for i, q := range g.qs {
			wheres[i] = q.Where
		}
		ms := ui.Filter(pattern, wheres)
		slices.SortFunc(ms, func(x, y ui.Match) int { return x.Index - y.Index })
		for _, m := range ms {
			entries, pos = append(entries, histEntry{g.qs[m.Index], g.fav}), append(pos, m.Pos)
		}
	}
	return entries, pos
}

// histEmpty is whether t has no history and no favorites at all, not whether
// what is typed leaves none (F18.9).
func (a *App) histEmpty(t *dataTab) bool {
	st := a.tableState(t)
	return len(st.History)+len(st.Favorites) == 0
}

// histMove moves the selection by d around the ends; 0 keeps it in the
// list, which got shorter.
func (a *App) histMove(t *dataTab, d int) {
	es, _ := a.histEntries(t)
	if n := len(es); d != 0 && n > 0 {
		t.hist.sel = ((t.hist.sel+d)%n + n) % n
	}
	t.hist.sel = max(min(t.hist.sel, len(es)-1), 0)
}

// histApply runs the selected entry: its WHERE, ORDER and LIMIT, from the
// first page.
func (a *App) histApply(t *dataTab, i int) tea.Cmd {
	es, _ := a.histEntries(t)
	if a.histEmpty(t) { // the "no history" row: ↵ and a click on it do nothing
		return nil
	}
	t.hist = nil
	if i < 0 || i >= len(es) {
		return nil
	}
	q := es[i].q
	t.where.Text, t.order, t.desc, t.limit = q.Where, q.Order, q.Desc, max(q.Limit, limits[0])
	return a.runWhere(t)
}

// histEdit puts the selected entry's WHERE in the input to be edited, in INSERT
// at the end, and runs nothing: its ORDER and LIMIT are applied only by running
// the entry (F9.1).
func (a *App) histEdit(t *dataTab) {
	es, _ := a.histEntries(t)
	if t.hist.sel >= len(es) {
		return
	}
	t.where.Text = es[t.hist.sel].q.Where
	a.startWhere(t)
}

// histStar makes the selected entry a favorite, or no longer one.
func (a *App) histStar(t *dataTab) tea.Cmd {
	es, _ := a.histEntries(t)
	if t.hist.sel >= len(es) {
		return nil
	}
	e, st := es[t.hist.sel], a.tableState(t)
	same := func(q config.Query) bool { return q.Key() == e.q.Key() }
	if e.fav {
		st.Favorites = slices.DeleteFunc(st.Favorites, same)
	} else if !slices.ContainsFunc(st.Favorites, same) {
		st.Favorites = slices.Insert(st.Favorites, 0, e.q)
	}
	a.histMove(t, 0)
	return a.saveState()
}

// runWhere runs the WHERE typed, from the first page, counting again; a
// condition goes into the history, a repeat moving up to the top (§9.7).
func (a *App) runWhere(t *dataTab) tea.Cmd {
	t.applied = strings.TrimSpace(t.where.Text) // paid↵↵ is not "paid " in the history
	t.stopTyping()
	t.pageNo = 0
	fetch := a.fetch(t, true)
	cmds := []tea.Cmd{fetch}
	if t.applied != "" && fetch != nil { // not one fetch refuses
		cmds = append(cmds, a.rememberWhere(t))
	}
	return tea.Batch(cmds...)
}

// rememberWhere puts t's WHERE, with its ORDER and LIMIT, on top of the
// table's history; the one already there moves up (§9.7).
func (a *App) rememberWhere(t *dataTab) tea.Cmd {
	st := a.tableState(t)
	q := t.savedQuery(t.applied)
	st.History = slices.Insert(slices.DeleteFunc(st.History, func(h config.Query) bool { return h.Key() == q.Key() }), 0, q)
	st.History = st.History[:min(len(st.History), config.WhereRows)]
	return a.saveState()
}

// savedQuery is where, with the ORDER and LIMIT the tab has, as the history and the
// favorites keep it: a limit that is the first of the choices is none.
func (t *dataTab) savedQuery(where string) config.Query {
	q := config.Query{Where: where, Order: t.order, Desc: t.desc, At: time.Now()}
	if t.limit != limits[0] {
		q.Limit = t.limit
	}
	return q
}

// starWhere is C-f in the WHERE input: the condition typed becomes a favorite
// with the tab's ORDER and LIMIT, or, when that very one is, no longer; none
// when nothing is typed (F9.2).
func (a *App) starWhere(t *dataTab) tea.Cmd {
	where := strings.TrimSpace(t.where.Text)
	if where == "" {
		return nil
	}
	q, st := t.savedQuery(where), a.tableState(t)
	same := func(f config.Query) bool { return f.Key() == q.Key() }
	toast := i18n.T("已取消收藏")
	if slices.ContainsFunc(st.Favorites, same) {
		st.Favorites = slices.DeleteFunc(st.Favorites, same)
	} else {
		st.Favorites, toast = slices.Insert(st.Favorites, 0, q), i18n.T("已收藏")
	}
	return tea.Batch(a.saveState(), a.showToast(toast))
}

// histSep is where the rule between the history and the favorites goes: the
// index of the first favorite, when something comes before it; -1 for none
// (F8.14). The list draws a row more past it, which is no entry.
func histSep(es []histEntry) int {
	if i := slices.IndexFunc(es, func(e histEntry) bool { return e.fav }); i > 0 {
		return i
	}
	return -1
}

// histRow is the entry the list's row stands for, the rule not counted;
// -1 for the rule itself.
func histRow(es []histEntry, row int) int {
	switch sep := histSep(es); {
	case sep < 0 || row < sep:
		return row
	case row == sep:
		return -1
	}
	return row - 1
}

func (a *App) histView(p *Pane, t *dataTab) (ui.Complete, uv.Rectangle, int) {
	es, pos := a.histEntries(t)
	v := ui.Complete{Sel: t.hist.sel, Foot: bound(
		ui.Hint{Key: a.hints("where", "/", "where.up", "where.down"), Label: i18n.T("移动")},
		ui.Hint{Key: a.keys.Hint("where.apply", "where"), Label: i18n.T("应用"), Action: "where.apply"},
		ui.Hint{Key: a.keys.Hint("where.edit", "where"), Label: i18n.T("填入"), Action: "where.edit"},
		ui.Hint{Key: a.keys.Hint("where.star", "where"), Label: i18n.T("收藏 / 取消收藏"), Action: "where.star"},
		ui.Hint{Key: a.keys.Hint("where.close", "where"), Label: i18n.T("关闭"), Action: "where.close"},
	)}
	w, sep := 40, histSep(es)
	for i, e := range es { // an icon each, no group titles (F3.31)
		note, icon := e.q.At.Local().Format("01-02 15:04"), a.icons.History
		if e.fav {
			note, icon = queryNote(e.q), a.icons.Star
			icon.Fg = cmp.Or(icon.Fg, a.theme.Warn)
		}
		if i == sep {
			v.Items = append(v.Items, ui.CompleteItem{Sep: true})
		}
		v.Items = append(v.Items, ui.CompleteItem{Icon: icon, Text: e.q.Where, Pos: pos[i], Note: note})
		w = max(w, ui.Width(icon.Text+" "+e.q.Where+"  "+note)+4)
	}
	if sep >= 0 && v.Sel >= sep {
		v.Sel++
	}
	if a.histEmpty(t) { // nothing to pick: say so, as the list is not drawn without a row; a list the typing filtered empty is not drawn (F18.9)
		v.Sel, v.Items = -1, []ui.CompleteItem{{Text: i18n.T("没有历史"), Dim: true}}
	}
	foot := 0
	if len(v.Foot) > 0 {
		foot, w = 1, max(w, v.FootWidth())
	}
	// down to the pane's bottom edge, or as far as the list always went, over the
	// result area, when the pane is too short for that (F10.1)
	at, n := a.whereAt(p, t), len(v.Items)
	inPane := a.window()
	inPane.Max.Y = min(inPane.Max.Y, a.layout()[p.ID].Max.Y-1)
	box, rows := ui.CompleteBox(inPane, at, w, n, foot, n)
	if b, r := ui.CompleteBox(a.window(), at, w, n, foot, ui.CompleteRows); r > rows {
		box, rows = b, r
	}
	_, t.hist.top = ui.ListView(v.Sel, t.hist.top, n, rows)
	v.Top = t.hist.top
	return v, box, rows
}

// queryNote is what a favorite ran with besides its WHERE, when it isn't
// the default: "status ↓ · 500".
func queryNote(q config.Query) string {
	var parts []string
	if len(q.Order) > 0 {
		parts = append(parts, orderText(q.Order))
	}
	if q.Limit != 0 {
		parts = append(parts, strconv.Itoa(q.Limit))
	}
	return strings.Join(parts, " · ")
}

// saveState writes the state out (§14): snapshot here, in Update, written
// on the Cmd's goroutine.
func (a *App) saveState() tea.Cmd {
	write := config.Snapshot(a.state)
	return func() tea.Msg {
		if err := write(); err != nil {
			return stateErr{err}
		}
		return nil
	}
}

type stateErr struct{ err error }
