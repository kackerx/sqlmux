package app

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/sqlkit"
	"sqlmux/internal/ui"
)

// completion is the candidate list under a WHERE being typed (§9.7).
type completion struct {
	items  []candidate
	sel    int
	chosen bool // moved to by key or pointer: then ↵ takes it
	start  int  // where the text it replaces starts in the input
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
	c := &completion{start: w.Start}
	for _, g := range groups {
		labels := make([]string, len(g))
		for i, cd := range g {
			labels[i] = cd.label
		}
		for _, m := range ui.Filter(pattern, labels) { // best first within the group
			cd := g[m.Index]
			cd.pos = m.Pos
			c.items = append(c.items, cd)
		}
	}
	if len(c.items) > 0 {
		t.comp = c
	}
}

// acceptCompletion puts the selected candidate in place of what it completes.
func (t *dataTab) acceptCompletion() {
	c, in := t.comp, &t.where
	cd := c.items[c.sel]
	in.Text = in.Text[:c.start] + cd.insert + in.Text[in.Pos:]
	in.Pos = c.start + len(cd.insert)
	t.comp = nil
}

// completeMove moves the selection by d; with none picked yet, down picks
// the first and up the last, as vim's popup menu does.
func (t *dataTab) completeMove(d int) {
	c := t.comp
	switch {
	case c.chosen:
		c.sel = max(min(c.sel+d, len(c.items)-1), 0)
	case d > 0:
		c.sel = 0
	default:
		c.sel = len(c.items) - 1
	}
	c.chosen = true
}

// whereAt is where pane p's WHERE input starts: lists open under it.
func (a *App) whereAt(p *Pane, t *dataTab) uv.Position {
	return a.queryBar(p, t).InputRect(bodyRect(a.layout()[p.ID])).Min
}

func (a *App) completeView(p *Pane, t *dataTab) (ui.Complete, uv.Rectangle, int) {
	v := ui.Complete{Sel: -1} // nothing looks picked until something is: ↵ runs then (§9.7)
	if t.comp.chosen {
		v.Sel = t.comp.sel
	}
	w := 20
	for _, cd := range t.comp.items {
		v.Items = append(v.Items, ui.CompleteItem{Text: cd.label, Pos: cd.pos, Note: cd.note})
		w = max(w, ui.Width(cd.label+"  "+cd.note)+4)
	}
	at := a.whereAt(p, t)
	at.X += ui.Width(t.where.Text[:t.comp.start]) // under what it completes
	// ponytail: off by the input's scroll once the text outgrows it
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

// historyRows is how many WHERE runs a table keeps (§9.7).
const historyRows = 50

// runWhere runs the WHERE typed, from the first page, counting again; a
// condition goes into the history, a repeat moving up to the top (§9.7).
func (a *App) runWhere(t *dataTab) tea.Cmd {
	t.applied = t.where.Text
	t.stopTyping()
	t.pageNo = 0
	cmds := []tea.Cmd{a.fetch(t, true)}
	if strings.TrimSpace(t.applied) != "" {
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
		dir := " ↑"
		if q.Desc {
			dir = " ↓"
		}
		parts = append(parts, q.Order+dir)
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
