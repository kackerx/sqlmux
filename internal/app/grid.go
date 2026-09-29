package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/sqlkit"
	"sqlmux/internal/ui"
)

// Tab is one tab of a pane (§5): a table's carries it, a console's the
// console, the result area's its result (§11); one with none is a landing
// tab.
type Tab struct {
	Name    string
	Data    *dataTab
	Console *consoleTab
	Result  *resultTab
}

// gridState is a grid's rows and where it is (§7.6): a table tab's, a
// result tab's. Positions are the data's, transposed or not, among the
// columns it shows.
type gridState struct {
	page      db.Result // the rows on screen
	row, col  int       // the cursor
	top, left int       // the first record and field shown
	transpose bool
}

// dataTab is a table open in a tab (§5, §7.6, §7.8「查询条」); its grid
// shows the columns COLS leaves.
type dataTab struct {
	gridState
	table db.Table
	cols  db.Columns // the catalog's, as fetched: PK and types for the grid
	next  bool       // a page follows (§8.5)
	seq   int        // the last request's; older answers are dropped (§8.3)
	bar   *errorBar  // the last fetch's or save's error, under the table (§7.8「错误栏」)

	// The query bar's. request is what the last request asked for, shown
	// what the rows on screen came from: row numbers and chips are shown's,
	// and a cancelled request goes back to it (§7.6).
	request
	shown    request
	where    ui.Input         // the WHERE input
	hidden   map[string]bool  // columns COLS hides
	pageIn   ui.Input         // PAGE's page number, while typed
	typing   string           // the input that has the keys: "where", "page", "cell" or ""
	cell     *cellEdit        // the cell being edited, while typing is "cell" (§10.1)
	edits    map[editKey]edit // changes not saved, of every page (§10.1)
	added    []*newRow        // rows added, not saved: those at one place in the order they show (§10.6)
	deleted  map[string]bool  // rows marked for deletion, by row key (§10.6)
	saving   bool             // a save is on its way (§10.3)
	closing  bool             // :wq: the tab closes once the save lands, if nothing is left unsaved (§11)
	note     ui.Note          // how the last save went, until the next fetch or change (Q-06)
	failed   *string          // the row key a failed save names (§10.3), nil for none
	wantCol  string           // the column the cursor goes to once a page is in: a column node's ↵ (§7.8)
	comp     *completion      // the WHERE's candidates, while typed (§9.7)
	hist     *histMenu        // the WHERE's history and favorites, while open (Q-02)
	recount  bool             // a count is owed once a page is in: a request asked for one (§8.3)
	count    int64            // the rows the WHERE keeps, as far as counted says
	counted  countState
	countSeq int // the last count's; older ones are dropped

	out     int           // its pages, counts and saves on their way: stop lights (§7.8「工具按钮」)
	auto    time.Duration // auto refresh's interval, 0 for off (§7.8「自动刷新」)
	autoGen int           // the auto refresh ticking; one set anew stops the old
}

// request is a page of a table as asked for.
type request struct {
	applied string // the WHERE in effect: the input shows it unless typing
	order   string // the column sorted by; "" for the row identity
	desc    bool   // ORDER's direction
	limit   int    // rows a page: 100, 500 or 1000 (§8.5)
	pageNo  int    // from 0
}

type countState int

const (
	counting  countState = iota // …
	countDone                   // n
	estimated                   // ~n: no WHERE and a big table (§8.5)
	countLost                   // ?: timed out or failed
)

// countTimeout bounds a count (§8.3).
var countTimeout = 3 * time.Second

// bigTable is past where a table without WHERE shows its estimate (§8.5).
const bigTable = 1_000_000

func newDataTab(t db.Table) *dataTab {
	r := request{limit: limits[0]}
	return &dataTab{table: t, request: r, shown: r, hidden: map[string]bool{}}
}

type tableID struct{ schema, name string }

func idOf(t db.Table) tableID { return tableID{t.Schema, t.Name} }

// pageMsg answers fetch.
type pageMsg struct {
	tab  *dataTab
	seq  int
	cols db.Columns // the catalog's, to cache
	page db.Result
	next bool
	err  error
}

// countMsg answers a count.
type countMsg struct {
	tab *dataTab
	seq int
	n   int64
	err error
}

// query is what t asks of its table.
func (t *dataTab) query() postgres.Query {
	return postgres.Query{
		Schema: t.table.Schema, Table: t.table.Name, Where: t.applied,
		Order: t.order, Desc: t.desc, Limit: t.limit, Offset: t.pageNo * t.limit,
	}
}

// fetch reads the page t.request asks for on Meta, after the table's
// columns when the catalog hasn't got them yet: they give the row identity
// it orders by (§8.4, §10.1). recount counts the rows again once a page
// is in (§8.3): owed by the tab, so a newer request taking this one's
// place still pays it.
func (a *App) fetch(t *dataTab, recount bool) tea.Cmd {
	t.note, t.failed = ui.Note{}, nil // until the next fetch the user asks for (§10.3)
	// what is on its way is older than this now, whether this goes or not
	t.seq++
	// a ; would end the statement and start another: 1=1; drop table t (§9.6)
	if sqlkit.HasSemicolon(t.applied, sqlkit.PG) {
		t.bar = &errorBar{ui.ErrorBar{First: ui.Note{Head: "WHERE 里不能有 ;"}}, "fetch"}
		t.backToShown()
		return nil
	}
	t.recount = t.recount || recount
	a.busy++
	t.out++
	seq, table, meta, q := t.seq, t.table, a.sess.Meta, t.query()
	cols, cached := a.sess.cols[idOf(table)]
	page := func() tea.Msg {
		ctx := context.Background()
		if !cached {
			var err error
			if cols, err = postgres.TableColumns(ctx, meta, table.Schema, table.Name); err != nil {
				return pageMsg{tab: t, seq: seq, err: err}
			}
		}
		q := q
		q.Key = cols.Key()
		r, next, err := postgres.Page(ctx, meta, q)
		return pageMsg{tab: t, seq: seq, cols: cols, page: r, next: next, err: err}
	}
	return page
}

// gotPage takes a page in: the rows, their request as what is shown, and
// then a count if one was asked for. An answer to an older request is
// dropped; a cancelled one goes back to what is shown, rows and all, with
// its count as it was (§7.6, §8.3).
func (a *App) gotPage(m pageMsg) tea.Cmd {
	a.busy--
	m.tab.out--
	if m.cols.Cols != nil {
		a.sess.cols[idOf(m.tab.table)] = m.cols
	}
	t := m.tab
	switch {
	case m.seq != t.seq: // a newer request is on its way
		return nil
	case errors.Is(m.err, context.Canceled): // shown's count is still right: none owed
		t.request, t.recount = t.shown, false
		if t.typing != "where" {
			t.where = ui.Input{Text: t.applied, Pos: len(t.applied)}
		}
		return a.showToast("查询已取消", toastTTL)
	case m.err != nil: // under the rows shown, which stay; its WHERE is there to be fixed (backToShown), the count owed
		// ponytail: no 位置 line, the SQL being ours, not the WHERE typed;
		// map Position into the WHERE if it is ever wanted
		t.bar = newErrorBar("fetch", postgres.ServerErrorOf(m.err), "", "")
		t.backToShown()
		return nil
	}
	clearBar(&t.bar, "fetch")
	t.cols, t.page, t.next, t.shown = m.cols, m.page, m.next, t.request
	t.row = max(min(t.row, len(t.shownRows())-1), 0)
	t.applyWantCol()
	t.col = max(min(t.col, len(t.shownCols())-1), 0)
	if t.recount {
		t.recount = false
		return a.count(t)
	}
	return nil
}

// backToShown puts ORDER, LIMIT and PAGE back to what the rows shown came
// from, a request having failed: what the chips show is what their keys
// act on. Its WHERE stays, to be fixed or tried again with R (§7.6).
func (t *dataTab) backToShown() {
	applied := t.applied
	t.request, t.applied = t.shown, applied
}

// applyWantCol puts the cursor on wantCol, a column node's ↵ (§7.8), once
// the page is in; hidden by COLS, the cursor stays where it is.
func (t *dataTab) applyWantCol() {
	if i := slices.IndexFunc(t.shownCols(), func(f int) bool { return t.page.Cols[f].Name == t.wantCol }); i >= 0 {
		t.col = i
	}
	t.wantCol = ""
}

// count counts the rows t's WHERE keeps, bounded by countTimeout (§8.3),
// unless an estimate stands for them: no WHERE on a big table (§8.5).
func (a *App) count(t *dataTab) tea.Cmd {
	if strings.TrimSpace(t.applied) == "" && t.table.Rows > bigTable {
		t.count, t.counted = int64(t.table.Rows), estimated
		return nil
	}
	t.countSeq++
	t.counted = counting
	t.out++
	seq, meta, q := t.countSeq, a.sess.Meta, t.query()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), countTimeout)
		defer cancel()
		n, err := postgres.Count(ctx, meta, q)
		return countMsg{tab: t, seq: seq, n: n, err: err}
	}
}

func (a *App) gotCount(m countMsg) {
	m.tab.out--
	if t := m.tab; m.seq == t.countSeq {
		t.count, t.counted = m.n, countDone
		if m.err != nil { // DeadlineExceeded past 3s, or the WHERE's own error
			t.counted = countLost
		}
	}
}

// pages is how many pages t's count makes, and whether that is known.
func (t *dataTab) pages() (int64, bool) {
	if t.counted != countDone && t.counted != estimated {
		return 0, false
	}
	return max((t.count+int64(t.shown.limit)-1)/int64(t.shown.limit), 1), true
}

// shownCols is the columns COLS lets through, as indexes into the page's.
func (t *dataTab) shownCols() []int {
	var out []int
	for i, c := range t.page.Cols {
		if !t.hidden[c.Name] {
			out = append(out, i)
		}
	}
	return out
}

// fieldAt is the page's index of shown column i, or -1.
func (t *dataTab) fieldAt(i int) int {
	if s := t.shownCols(); i >= 0 && i < len(s) {
		return s[i]
	}
	return -1
}

// nearestShown is the shown index of page column field, or of the first
// shown one after it, or else the last shown one.
func (t *dataTab) nearestShown(field int) int {
	s := t.shownCols()
	if i := slices.IndexFunc(s, func(f int) bool { return f >= field }); i >= 0 {
		return i
	}
	return max(len(s)-1, 0)
}

// typeOf is the catalog's type for column name, or "".
func (t *dataTab) typeOf(name string) string { return t.column(name).Type }

// column is what the catalog says of column name; zero when it doesn't know it.
func (t *dataTab) column(name string) db.Column {
	if i := slices.IndexFunc(t.cols.Cols, func(c db.Column) bool { return c.Name == name }); i >= 0 {
		return t.cols.Cols[i]
	}
	return db.Column{}
}

// stopTyping gives the keys back to the grid, the WHERE input showing what
// is in effect again (§7.8), a cell's edit kept (§10.1).
func (t *dataTab) stopTyping() {
	if t.cell != nil && t.cellHint() == "" { // no value of the column: dropped (§10.7)
		t.commitCell()
	}
	t.typing, t.where, t.comp, t.hist = "", ui.Input{Text: t.applied, Pos: len(t.applied)}, nil, nil
}

// dataOf is pane p's current table, or nil.
func dataOf(p *Pane) *dataTab {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur].Data
	}
	return nil
}

// bodyRect is where a pane at r shows its tab's content: inside the border,
// above the tab bar.
func bodyRect(r uv.Rectangle) uv.Rectangle {
	return uv.Rect(r.Min.X+1, r.Min.Y+1, max(r.Dx()-2, 0), max(r.Dy()-3, 0))
}

// gridRect is where a pane at r draws t's table: under the query bar,
// over its error bar.
func gridRect(r uv.Rectangle, t *dataTab) uv.Rectangle {
	b := bodyRect(r)
	b.Min.Y = min(b.Min.Y+ui.QueryBarRows, b.Max.Y)
	b.Max.Y = max(b.Max.Y-t.bar.rows(), b.Min.Y)
	return b
}

// grid is t as pane p draws it: the columns COLS shows, with what the
// catalog said of them.
func (a *App) grid(p *Pane, t *dataTab) ui.Grid {
	g := ui.Grid{
		Row: t.row, Col: t.col, Top: t.top, Left: t.left, Transpose: t.transpose, First: t.shown.pageNo * t.shown.limit,
		Focused: a.win().Focus == p.ID, Key: a.icons.Key, Pane: p.ID,
	}
	shown := t.shownCols()
	for _, i := range shown {
		name := t.page.Cols[i].Name
		g.Cols = append(g.Cols, ui.GridCol{Name: name, PK: slices.Contains(t.cols.PK, name), Type: colType(t.typeOf(name))})
	}
	keyed := (len(t.edits) > 0 || len(t.deleted) > 0) && t.cols.Key() != nil
	changed := map[string]bool{} // rows with changes, COLS hiding them or not
	for k := range t.edits {
		changed[k.row] = true
	}
	mark := func(m *map[int]bool, i int) {
		if *m == nil {
			*m = map[int]bool{}
		}
		(*m)[i] = true
	}
	edited := func(i, j int, e edit) db.Val {
		if g.Edited == nil {
			g.Edited = map[[2]int]bool{}
		}
		g.Edited[[2]int{i, j}] = true
		if e.def {
			return db.Val{S: "<default>"}
		}
		return e.val
	}
	rows := t.shownRows()
	if len(t.added) > 0 {
		g.Nums = make([]int, len(rows))
	}
	for i, sr := range rows {
		vals := make([]db.Val, len(shown))
		if sr.add != nil { // DEFAULT but for what is set (§10.6)
			mark(&g.Added, i)
			for j, c := range shown {
				vals[j] = db.Val{S: "<default>"}
				if e, ok := sr.add.cells[t.page.Cols[c].Name]; ok {
					vals[j] = edited(i, j, e)
				}
			}
			g.Rows = append(g.Rows, vals)
			continue
		}
		if g.Nums != nil {
			g.Nums[i] = g.First + sr.rec + 1
		}
		key := ""
		if keyed {
			key = t.rowKey(sr.rec)
		}
		for j, c := range shown {
			vals[j] = t.page.Rows[sr.rec][c]
			if e, ok := t.edits[editKey{key, t.page.Cols[c].Name}]; keyed && ok {
				vals[j] = edited(i, j, e)
			}
		}
		if keyed && t.failed != nil && key == *t.failed { // "" too: a text key may hold it
			mark(&g.Failed, i)
		}
		switch {
		case !keyed:
		case t.deleted[key]:
			mark(&g.Deleted, i)
		case changed[key]:
			mark(&g.Changed, i)
		}
		g.Rows = append(g.Rows, vals)
	}
	if t.cell != nil {
		g.Edit, g.EditMenu, g.EditBad = &t.cell.in, len(t.options()) > 0, t.cellHint() != ""
	}
	return g
}

// sortedBy is an ORDER as text: "id ↑", "amount ↓".
func sortedBy(col string, desc bool) string {
	if desc {
		return col + " ↓"
	}
	return col + " ↑"
}

// queryBar is the two rows over t's table (§7.8「查询条」).
func (a *App) queryBar(p *Pane, t *dataTab) ui.QueryBar {
	ic := a.icons
	order := ui.Chip{Label: "ORDER", Value: "—", Action: "grid.order", IconAction: "grid.order.toggle"} // no row identity to sort by
	switch s, key := t.shown, t.cols.Key(); {
	case s.order != "":
		order.Value, order.Icon = s.order, ic.SortAsc
		if s.desc {
			order.Icon = ic.SortDesc
		}
	case key != nil:
		order.Value, order.Icon = strings.Join(key, ","), ic.SortAsc
	}
	pages := "?"
	if n, ok := t.pages(); ok {
		pages = strconv.FormatInt(n, 10)
		if t.counted == estimated {
			pages = "~" + pages
		}
	}
	page := ui.Chip{Label: "PAGE", Value: fmt.Sprintf("%d/%s", t.shown.pageNo+1, pages), Action: "grid.page"}
	if t.typing == "page" {
		page.Input, page.Suffix = &t.pageIn, "/"+pages
	}
	rows := "…"
	switch t.counted {
	case countLost:
		rows = "?"
	case countDone:
		rows = strconv.FormatInt(t.count, 10)
	case estimated:
		rows = "~" + ui.Magnitude(float64(t.count))
	}
	right := "auto · " + rows + " 行"
	if t.page.Cols != nil {
		right += " · " + t.page.Took.Round(time.Millisecond).String()
	}
	names, _ := a.sqlNames(t.where.Text, t.table.Schema, &t.cols)
	return ui.QueryBar{
		Where: t.where, Typing: t.typing == "where", Pane: p.ID, Right: right, Note: t.note, Names: names,
		Chips: []ui.Chip{
			order,
			{Label: "LIMIT", Value: strconv.Itoa(t.shown.limit), Action: "grid.limit"},
			page,
			{Label: "COLS", Value: fmt.Sprintf("%d/%d", len(t.shownCols()), len(t.page.Cols)), Action: "grid.cols"},
		},
		Buttons: a.toolButtons(t),
	}
}

// toolButtons are t's query bar buttons (§7.8「工具按钮」): + −, save and
// refresh, auto refresh and stop, transpose, giving way in the order
// transpose, auto refresh and stop, + −, save and refresh. Stop and auto
// refresh go by their state: an [icon] color only when lit.
func (a *App) toolButtons(t *dataTab) []ui.ButtonGroup {
	th, ic := a.theme, a.icons
	save := ui.Button{Icon: ic.Save, Action: "save", Fg: th.Info} // Q-05
	if n := t.changes(); n > 0 {
		save.Tail = strconv.Itoa(n)
	}
	auto := ui.Button{Icon: ic.AutoRefresh, Action: "grid.refresh.auto", Fg: th.Info, Plain: true}
	if t.auto > 0 {
		auto.Fg, auto.Plain, auto.Tail = th.Warn, false, autoLabel(t.auto)
	}
	stop := ui.Button{Icon: ic.Stop, Fg: th.Dim, Plain: true}
	if t.out > 0 {
		stop.Fg, stop.Plain, stop.Action = th.Error, false, "grid.stop"
	}
	return []ui.ButtonGroup{
		{Buttons: []ui.Button{{Icon: ic.RowAdd, Action: "grid.row.add", Fg: th.Focus}, {Icon: ic.RowDelete, Action: "grid.row.delete", Fg: th.Error}}, Stay: 2},
		{Buttons: []ui.Button{save, {Icon: ic.Refresh, Action: "grid.refresh", Fg: th.Info}}, Stay: 3},
		{Buttons: []ui.Button{auto, stop}, Stay: 1},
		{Buttons: []ui.Button{{Icon: ic.Transpose, Action: "grid.transpose", Fg: th.Info}}, Bare: true},
	}
}

// autoIntervals are auto refresh's choices, off first (§7.8「自动刷新」).
var autoIntervals = []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}

// autoLabel is an interval as its dropdown and button say it: 关, 5s, 60s.
func autoLabel(d time.Duration) string {
	if d == 0 {
		return "关"
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// autoMsg is auto refresh's tick for t, from ticking gen.
type autoMsg struct {
	tab *dataTab
	gen int
}

// autoTick waits t's interval for the next auto refresh.
func autoTick(t *dataTab) tea.Cmd {
	gen := t.autoGen
	return tea.Tick(t.auto, func(time.Time) tea.Msg { return autoMsg{t, gen} })
}

// gotAuto refreshes t as R does, keeping what the last save said, when it
// shows in a pane on screen as its current tab, with no changes, no cell being
// edited and nothing on its way; else it skips a turn. The ticking stops
// with its interval set anew, or the tab closed (§7.8「自动刷新」).
func (a *App) gotAuto(m autoMsg) tea.Cmd {
	t := m.tab
	if m.gen != t.autoGen || !a.tabOpen(t) {
		return nil
	}
	var cmd tea.Cmd
	p, shown := a.paneShowing(t), false
	if p != nil {
		_, shown = a.layout()[p.ID] // not behind a zoomed pane
	}
	if shown && t.changes() == 0 && t.cell == nil && t.out == 0 {
		note, failed := t.note, t.failed
		cmd = a.fetch(t, true)
		t.note, t.failed = note, failed
	}
	return tea.Batch(cmd, autoTick(t))
}

// tabOpen reports whether t is a tab in some pane of the session.
func (a *App) tabOpen(t *dataTab) bool {
	for _, w := range a.sess.Windows {
		for _, p := range w.Root.Leaves() {
			if slices.ContainsFunc(p.Tabs, func(tb Tab) bool { return tb.Data == t }) {
				return true
			}
		}
	}
	return false
}

// chipRect is where pane p draws the chip of t's query bar running action.
func (a *App) chipRect(p *Pane, t *dataTab, action string) uv.Rectangle {
	return a.queryBar(p, t).ChipRect(bodyRect(a.layout()[p.ID]), action)
}

// focusedGrid is the focused pane's table, if it shows one: loaded, with
// columns to show.
func (a *App) focusedGrid() (*Pane, *dataTab, bool) {
	p := a.focused()
	t := dataOf(p)
	return p, t, t != nil && len(t.shownCols()) > 0
}

// typingTab is the focused table whose query bar input has the keys.
func (a *App) typingTab() *dataTab {
	if t := dataOf(a.focused()); t != nil && t.typing != "" {
		return t
	}
	return nil
}

// gridOf is the grid pane p's current tab shows, as drawn in area; ok is
// false when it shows none: no rows loaded.
func (a *App) gridOf(p *Pane) (s *gridState, g ui.Grid, area uv.Rectangle, ok bool) {
	switch t := p.tab(); {
	case t == nil:
	case t.Data != nil && len(t.Data.shownCols()) > 0:
		return &t.Data.gridState, a.grid(p, t.Data), gridRect(a.layout()[p.ID], t.Data), true
	case t.Result != nil && t.Result.page.Cols != nil:
		return &t.Result.gridState, a.resultGrid(p, t.Result), bodyRect(a.layout()[p.ID]), true
	}
	return nil, ui.Grid{}, uv.Rectangle{}, false
}

// resultGrid is result tab t as pane p draws it: every column, read only (§11).
func (a *App) resultGrid(p *Pane, t *resultTab) ui.Grid {
	g := ui.Grid{
		Rows: t.page.Rows, Row: t.row, Col: t.col, Top: t.top, Left: t.left, Transpose: t.transpose,
		Focused: a.win().Focus == p.ID, Key: a.icons.Key, Pane: p.ID,
	}
	for _, c := range t.page.Cols {
		g.Cols = append(g.Cols, ui.GridCol{Name: c.Name, Type: colType(c.Type)})
	}
	return g
}

// logMove is a grid move on the log: j, k, gg and G, the wheel scroll it (§11).
func (a *App) logMove(p *Pane, to func(r, c, rows, cols int) (int, int)) {
	lt, n := resultOf(p), len(a.win().log)
	h := bodyRect(a.layout()[p.ID]).Dy()
	top, _ := to(ui.LogTop(lt.top, n, h), 0, n, 1)
	lt.top = ui.LogTop(top, n, h)
}

// gridMove moves the focused grid's cursor to where to puts it, in screen
// terms (§7.6: j is always down, whichever way the data is turned), and
// scrolls it into view.
func (a *App) gridMove(to func(r, c, rows, cols int) (int, int)) {
	if rt := resultOf(a.focused()); rt != nil && rt.run == nil {
		a.logMove(a.focused(), to)
		return
	}
	s, g, area, ok := a.gridOf(a.focused())
	if !ok {
		return
	}
	rows, cols := len(g.Rows), len(g.Cols)
	r, c := s.row, s.col
	if s.transpose {
		rows, cols, r, c = cols, rows, c, r
	}
	r, c = to(r, c, rows, cols)
	r, c = max(min(r, rows-1), 0), max(min(c, cols-1), 0)
	if s.transpose {
		r, c = c, r
	}
	s.row, s.col = r, c
	g.Row, g.Col = r, c
	s.top, s.left = g.View(area)
}

// gridGoto is a click on a cell or a row number: "rec field".
func (a *App) gridGoto(arg string) {
	r, c, _ := strings.Cut(arg, " ")
	rec, err1 := strconv.Atoi(r)
	field, err2 := strconv.Atoi(c)
	if s, _, _, ok := a.gridOf(a.focused()); ok && err1 == nil && err2 == nil {
		s.row, s.col = rec, field
		a.gridMove(func(r, c, _, _ int) (int, int) { return r, c })
	}
}

func (a *App) gridTranspose() {
	if s, _, _, ok := a.gridOf(a.focused()); ok {
		s.transpose = !s.transpose
		a.gridMove(func(r, c, _, _ int) (int, int) { return r, c })
	}
}

// scrollGrid is the wheel over pane p: dr rows and dc columns of the view,
// the cursor pulled along (§7.6).
func (a *App) scrollGrid(p *Pane, dr, dc int) {
	if s, g, area, ok := a.gridOf(p); ok {
		s.top, s.left, s.row, s.col = g.Scroll(area, dr, dc)
	}
}

// turnPage is ] and [ (§7.8): not past the last page, nor before the first.
func (a *App) turnPage(d int) tea.Cmd {
	t := dataOf(a.focused())
	if t == nil || d > 0 && !t.next || d < 0 && t.pageNo == 0 {
		return nil
	}
	t.pageNo += d
	return a.fetch(t, false)
}

// typeKey edits the query bar input that has the keys (§7.8): ↵ runs it,
// esc drops it.
func (a *App) typeKey(t *dataTab, k keymap.Key) tea.Cmd {
	if t.cell != nil { // ↵ and esc are cell.accept and cell.done (§10.2)
		if editInput(&t.cell.in, k) {
			t.cell.sel = -1 // the options change with it: none picked (§10.2)
		}
		return nil
	}
	switch k {
	case keymap.Esc:
		if t.comp != nil || t.hist != nil { // a list goes first, then the input (§9.7)
			t.comp, t.hist = nil, nil
			return nil
		}
		t.stopTyping()
		return nil
	case "<CR>":
		if t.comp != nil { // ↵ runs only when taking it changes nothing (§9.7)
			if changed, _ := a.acceptCompletion(); changed {
				return nil
			}
		}
		if t.typing == "where" {
			return a.runWhere(t)
		}
		n, err := strconv.Atoi(strings.TrimSpace(t.pageIn.Text))
		t.stopTyping()
		if err != nil {
			return nil
		}
		if pages, ok := t.pages(); ok { // past the ends: to the end
			n = min(n, int(pages))
		}
		t.pageNo = max(n-1, 0)
		return a.fetch(t, false)
	}
	if t.typing == "page" {
		editInput(&t.pageIn, k)
		return nil
	}
	text := t.where.Text
	a.editPaired(&t.where, k)
	switch {
	case t.hist == nil:
		a.complete(t)
	case t.where.Text != text: // typed into: the list filters by it from now on (§9.7)
		t.hist.typed, t.hist.sel = true, 0
	}
	return nil
}

// baseType is a catalog type's name without its modifier: format_type
// puts one at most, "numeric(10,2)", "timestamp(3) with time zone".
func baseType(t string) string {
	if i, j := strings.Index(t, "("), strings.Index(t, ")"); 0 <= i && i < j {
		return t[:i] + t[j+1:]
	}
	return t
}

// timeKind is the parts a column of catalog type t steps (§10.2); interval
// has none.
func timeKind(t string) ui.TimeKind {
	return map[string]ui.TimeKind{
		"date": ui.Date, "time without time zone": ui.Time, "time with time zone": ui.TimeTZ,
		"timestamp without time zone": ui.Timestamp, "timestamp with time zone": ui.TimestampTZ,
	}[baseType(t)]
}

// colType is the grid's class for a column of the catalog's type (§7.6).
func colType(t string) ui.ColType {
	if strings.HasSuffix(t, "[]") {
		return ui.ColOther
	}
	t = baseType(t)
	// after format_type's names, pgx's for a result's OIDs (quick SQL, §12)
	switch t {
	case "smallint", "integer", "bigint", "numeric", "real", "double precision", "oid",
		"int2", "int4", "int8", "float4", "float8":
		return ui.ColNumber
	case "date", "time without time zone", "time with time zone",
		"timestamp without time zone", "timestamp with time zone",
		"time", "timetz", "timestamp", "timestamptz":
		return ui.ColTime
	case "boolean", "bool":
		return ui.ColBool
	case "json", "jsonb":
		return ui.ColJSON
	case "text", "character varying", "character", `"char"`, "name", "citext",
		"varchar", "bpchar", "char":
		return ui.ColString
	}
	if t == "interval" || strings.HasPrefix(t, "interval ") { // "interval day to second"
		return ui.ColTime
	}
	return ui.ColOther
}
