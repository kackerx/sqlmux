package app

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/db"
	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
	"sqlmux/internal/sqlkit"
	"sqlmux/internal/ui"
)

// completion is the candidate list under a WHERE or a quick SQL being
// typed (§9.7, §12).
type completion struct {
	items []candidate
	sel   int // the first as it opens: ↵ takes it (§9.7)
	start int // where the text it replaces starts in the input
}

type candidate struct {
	label, insert, note string
	pos                 []int
}

// whereKeywords are what a condition is made of besides columns (§9.7).
var whereKeywords = []string{"and", "or", "not", "is null", "is not null", "in", "like", "ilike", "between", "true", "false", "null"}

// complete finds the candidates for where t's WHERE cursor is: columns and
// keywords while a word is typed, an enum's or a boolean's values where a
// value of that column goes (§9.7).
func (a *App) complete(t *dataTab) {
	t.comp = nil
	in := t.where
	w := sqlkit.WhereContext(in.Text, in.Pos)
	if !w.OK {
		return
	}
	var groups [][]candidate
	pattern := w.Prefix
	if w.Column != "" {
		var vals []candidate
		for _, c := range t.cols.Cols {
			if !strings.EqualFold(c.Name, w.Column) {
				continue
			}
			for _, e := range c.Enum {
				vals = append(vals, candidate{label: e, insert: "'" + strings.ReplaceAll(e, "'", "''") + "'", note: "值"})
			}
			if c.Type == "boolean" {
				vals = append(vals, candidate{label: "true", insert: "true", note: "值"}, candidate{label: "false", insert: "false", note: "值"})
			}
		}
		groups, pattern = [][]candidate{vals}, strings.TrimPrefix(w.Prefix, "'")
	} else {
		if w.Prefix == "" { // nothing typed yet: no list
			return
		}
		var cols, kws []candidate
		for _, c := range t.cols.Cols {
			cols = append(cols, candidate{label: c.Name, insert: c.Name, note: c.Type})
		}
		for _, k := range whereKeywords {
			kws = append(kws, candidate{label: k, insert: k, note: "关键字"})
		}
		groups = [][]candidate{cols, kws}
	}
	t.comp = ranked(pattern, w.Start, groups...)
}

// ranked is the candidates of groups that pattern matches, group by group
// and best first within each (§9.7), completing what starts at start in the
// input; nil when none does. Pattern's first character matches at a word
// start, fuzzy past it as VS Code's does: evt finds mt_event and tord
// t_order, but x no longer max.
func ranked(pattern string, start int, groups ...[]candidate) *completion {
	c := &completion{start: start}
	// no smart case: an upper-case letter would make fzf exact about case
	// (§9.7); the palette, tree and COLS keep it
	pattern = strings.ToLower(pattern)
	for _, g := range groups {
		labels := make([]string, len(g))
		for i, cd := range g {
			labels[i] = cd.label
		}
		for _, m := range ui.Filter(pattern, labels) {
			cd := g[m.Index]
			if cd.pos = atWordStart(pattern, cd.label, m.Pos); pattern != "" && cd.pos == nil {
				continue
			}
			c.items = append(c.items, cd)
		}
	}
	if len(c.items) == 0 {
		return nil
	}
	return c
}

// atWordStart is where pattern matches s with its first character at a
// word start (§9.7), given fzf's best match at pos; nil when it can't.
// fzf's match starts elsewhere when a run of consecutive characters
// outscores the word start: the rest is matched again from each one.
func atWordStart(pattern, s string, pos []int) []int {
	rs := []rune(s)
	if len(pos) == 0 || wordStart(rs, pos[0]) {
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

// sqlComplete is the candidates for the cursor at pos in sql, a console's
// or the quick SQL's (§9.7): by what CompletionContext says goes there,
// tables of schema (the console's; the tree's for the quick SQL, §8.6), of
// X. after a schema X, a table's columns
// after its name or alias, else the statement's tables' columns, those at
// the cursor's depth first, the other tables and keywords. It opens with a
// word typed, or right after a qualifier that resolves; manual (C-n) with
// nothing. The columns the cache lacks are fetched, a table once per asked.
// ponytail: names go in bare, as the catalog has them; one that wants
// quotes (upper case, a space) needs them typed.
func (a *App) sqlComplete(sql string, pos int, schema string, asked map[tableID]bool, manual bool) (*completion, []tea.Cmd) {
	c := sqlkit.CompletionContext(sql, pos, sqlkit.PG)
	if !c.OK {
		return nil, nil
	}
	var cmds []tea.Cmd
	table := func(in, name string) (db.Table, bool) { // PG folds what isn't quoted
		in = cmp.Or(in, schema)
		for _, t := range a.sess.Tables {
			if strings.EqualFold(t.Schema, in) && strings.EqualFold(t.Name, name) {
				return t, true
			}
		}
		return db.Table{}, false
	}
	columns := func(t db.Table) (out []candidate) {
		cs, ok := a.sess.cols[idOf(t)]
		if !ok && !asked[idOf(t)] {
			asked[idOf(t)] = true
			cmds = append(cmds, a.fetchCols(t))
		}
		for _, col := range cs.Cols {
			out = append(out, candidate{label: col.Name, insert: col.Name, note: col.Type + " · " + t.Name})
		}
		return out
	}
	tables := func(in string, skip map[tableID]bool) (out []candidate) {
		for _, t := range a.sess.Tables {
			if t.Schema == in && !skip[idOf(t)] {
				note := "表"
				if t.View() {
					note = "视图"
				}
				out = append(out, candidate{label: t.Name, insert: t.Name, note: note})
			}
		}
		return out
	}
	var ctes []candidate
	isCTE := map[string]bool{}
	for _, n := range c.CTEs {
		ctes, isCTE[strings.ToLower(n)] = append(ctes, candidate{label: n, insert: n, note: "CTE"}), true
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
			} else if j := slices.IndexFunc(a.sess.Schemas, func(s string) bool { return strings.EqualFold(s, q) }); j >= 0 {
				groups, resolved = [][]candidate{tables(a.sess.Schemas[j], nil)}, true
			}
		}
	case sqlkit.CompTables:
		groups = [][]candidate{append(ctes, tables(schema, nil)...)}
	default:
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
		kws := make([]candidate, len(sqlkit.Common))
		for i, k := range sqlkit.Common {
			kws[i] = candidate{label: k, insert: k, note: "关键字"}
		}
		groups = [][]candidate{near, far, append(ctes, tables(schema, named)...), kws}
	}
	if c.Prefix == "" && !manual && !resolved {
		return nil, cmds
	}
	return ranked(c.Prefix, c.Start, groups...), cmds
}

// completing is the candidate list that is up, if any: the palette's quick
// SQL's, else the focused console's or the WHERE's being typed.
func (a *App) completing() *completion {
	if a.palette != nil {
		return a.palette.comp
	}
	if t := a.focusedConsole(); t != nil {
		return t.comp
	}
	if t := a.typingTab(); t != nil {
		return t.comp
	}
	return nil
}

// acceptCompletion puts the selected candidate in place of what it
// completes, closes the list, and reports whether the text changed: ↵ runs
// as usual when it would not (§9.7, VS Code's acceptSuggestionOnEnter
// smart). A console's change is saved as typing is (cmd).
func (a *App) acceptCompletion() (changed bool, cmd tea.Cmd) {
	if p := a.palette; p != nil {
		changed = p.comp.accept(&p.input)
		p.comp = nil
		return changed, nil
	}
	if t := a.focusedConsole(); t != nil {
		return a.consoleAccept(t)
	}
	t := a.typingTab()
	changed = t.comp.accept(&t.where)
	t.comp = nil
	return changed, nil
}

// accept leaves in as it is when the candidate differs from the word only
// in case, as SQL's keywords and bare names do: NULL stays NULL.
func (c *completion) accept(in *ui.Input) bool {
	cd := c.items[c.sel]
	after := in.Text[in.Pos:]
	if n := len(cd.insert); n > 0 && strings.ContainsRune("'\"`", rune(cd.insert[n-1])) && strings.HasPrefix(after, cd.insert[n-1:]) {
		after = after[1:] // the closing quote autopairs put there, or one typed: the insert has its own (§7.9)
	}
	text := in.Text[:c.start] + cd.insert + after
	if strings.EqualFold(text, in.Text) {
		return false
	}
	in.Text, in.Pos = text, c.start+len(cd.insert)
	return true
}

// move moves the selection by d, around the ends (§9.7).
func (c *completion) move(d int) {
	n := len(c.items)
	c.sel = ((c.sel+d)%n + n) % n
}

// editPaired is editInput with autopairs, a WHERE's and the quick SQL's
// (§7.9): an opening half gets its other, a closing one after the cursor
// is stepped over, and BS takes an empty pair.
func (a *App) editPaired(in *ui.Input, k keymap.Key) bool {
	before, after := in.Text[:in.Pos], in.Text[in.Pos:]
	t := keymap.Text(k)
	switch r, n := utf8.DecodeRuneInString(t); {
	case !a.autoPairs:
	case k == "<BS>" && editor.EmptyPair(before, after):
		in.Text, in.Pos = before[:len(before)-1]+after[1:], in.Pos-1
		return true
	case n > 0 && n == len(t):
		switch close, skip := editor.Pair(before, after, r); {
		case skip:
			in.Pos += n
			return false
		case close != "":
			in.Insert(t + close)
			in.Pos -= len(close)
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
		v.Items = append(v.Items, ui.CompleteItem{Text: cd.label, Pos: cd.pos, Note: cd.note})
		w = max(w, ui.Width(cd.label+"  "+cd.note)+4)
	}
	box, rows := ui.CompleteBox(a.window(), at, w, len(v.Items))
	v.Top = max(0, v.Sel-rows+1)
	return v, box, rows
}

// histMenu is the WHERE history and favorites list (Q-02, §9.7), filtered
// by the WHERE input itself.
type histMenu struct{ sel int }

// histEntry is one of its rows: a favorite, or a history entry.
type histEntry struct {
	q   config.Query
	fav bool
}

// tableState is t's history and favorites, made on first use.
func (a *App) tableState(t *dataTab) *config.TableState {
	key := a.sess.Name + "/" + t.table.Schema + "." + t.table.Name
	if a.state.Tables == nil {
		a.state.Tables = map[string]*config.TableState{}
	}
	if a.state.Tables[key] == nil {
		a.state.Tables[key] = &config.TableState{}
	}
	return a.state.Tables[key]
}

// histEntries is the favorites, then the history, that the WHERE input
// matches, newest first in each.
func (a *App) histEntries(t *dataTab) (entries []histEntry, pos [][]int) {
	st := a.tableState(t)
	for _, g := range []struct {
		qs  []config.Query
		fav bool
	}{{st.Favorites, true}, {st.History, false}} {
		wheres := make([]string, len(g.qs))
		for i, q := range g.qs {
			wheres[i] = q.Where
		}
		ms := ui.Filter(t.where.Text, wheres)
		slices.SortFunc(ms, func(x, y ui.Match) int { return x.Index - y.Index })
		for _, m := range ms {
			entries, pos = append(entries, histEntry{g.qs[m.Index], g.fav}), append(pos, m.Pos)
		}
	}
	return entries, pos
}

func (a *App) histMove(t *dataTab, d int) {
	n, _ := a.histEntries(t)
	t.hist.sel = max(min(t.hist.sel+d, len(n)-1), 0)
}

// histApply runs the selected entry: its WHERE, ORDER and LIMIT, from the
// first page.
func (a *App) histApply(t *dataTab, i int) tea.Cmd {
	es, _ := a.histEntries(t)
	t.hist = nil
	if i >= len(es) {
		return nil
	}
	q := es[i].q
	t.where.Text, t.order, t.desc, t.limit = q.Where, q.Order, q.Desc, max(q.Limit, limits[0])
	return a.runWhere(t)
}

// histStar makes the selected entry a favorite, or no longer one.
func (a *App) histStar(t *dataTab) tea.Cmd {
	es, _ := a.histEntries(t)
	if t.hist.sel >= len(es) {
		return nil
	}
	e, st := es[t.hist.sel], a.tableState(t)
	same := func(q config.Query) bool { return sameQuery(q, e.q) }
	if e.fav {
		st.Favorites = slices.DeleteFunc(st.Favorites, same)
	} else if !slices.ContainsFunc(st.Favorites, same) {
		st.Favorites = slices.Insert(st.Favorites, 0, e.q)
	}
	a.histMove(t, 0)
	return a.saveState()
}

func sameQuery(x, y config.Query) bool {
	return x.Where == y.Where && x.Order == y.Order && x.Desc == y.Desc && x.Limit == y.Limit
}

// historyRows is how many runs a history keeps: a table's WHERE's (§9.7),
// a connection's quick SQL's (§12).
const historyRows = 50

// runWhere runs the WHERE typed, from the first page, counting again; a
// condition goes into the history, a repeat moving up to the top (§9.7).
func (a *App) runWhere(t *dataTab) tea.Cmd {
	t.applied = t.where.Text
	t.stopTyping()
	t.pageNo = 0
	fetch := a.fetch(t, true)
	cmds := []tea.Cmd{fetch}
	if strings.TrimSpace(t.applied) != "" && fetch != nil { // not one fetch refuses
		st := a.tableState(t)
		q := config.Query{Where: t.applied, Order: t.order, Desc: t.desc, At: time.Now()}
		if t.limit != limits[0] {
			q.Limit = t.limit
		}
		st.History = slices.Insert(slices.DeleteFunc(st.History, func(h config.Query) bool { return sameQuery(h, q) }), 0, q)
		st.History = st.History[:min(len(st.History), historyRows)]
		cmds = append(cmds, a.saveState())
	}
	return tea.Batch(cmds...)
}

func (a *App) histView(p *Pane, t *dataTab) (ui.Complete, uv.Rectangle, int) {
	es, pos := a.histEntries(t)
	v := ui.Complete{Sel: -1}
	w := 40
	for i, e := range es {
		if i == 0 || e.fav != es[i-1].fav { // a group starts
			head := "历史"
			if e.fav {
				head = "收藏"
			}
			v.Items = append(v.Items, ui.CompleteItem{Text: head, Head: true})
		}
		note := e.q.At.Local().Format("01-02 15:04")
		if e.fav {
			note = queryNote(e.q)
		}
		if i == t.hist.sel {
			v.Sel = len(v.Items)
		}
		v.Items = append(v.Items, ui.CompleteItem{Text: e.q.Where, Pos: pos[i], Note: note})
		w = max(w, ui.Width(e.q.Where+"  "+note)+4)
	}
	box, rows := ui.CompleteBox(a.window(), a.whereAt(p, t), w, len(v.Items))
	v.Top = max(0, v.Sel-rows+1)
	return v, box, rows
}

// queryNote is what a favorite ran with besides its WHERE, when it isn't
// the default: "status ↓ · 500".
func queryNote(q config.Query) string {
	var parts []string
	if q.Order != "" {
		parts = append(parts, sortedBy(q.Order, q.Desc))
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

// histIndex is the entry at row of the list as drawn, its group titles
// skipped.
func (a *App) histIndex(t *dataTab, row int) int {
	es, _ := a.histEntries(t)
	r := 0
	for i, e := range es {
		if i == 0 || e.fav != es[i-1].fav {
			r++
		}
		if r == row {
			return i
		}
		r++
	}
	return len(es)
}
