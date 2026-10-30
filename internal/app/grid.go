package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// Tab is one tab of a pane (§5): a table's carries it, a query's the
// query, the result area's its result (§11); one with none is a landing
// tab.
type Tab struct {
	Name   string
	Data   *dataTab
	Query  *queryTab
	Result *resultTab
}

// gridState is a grid's rows and where it is (§7.6): a table tab's, a
// result tab's. Positions are the data's, transposed or not, among the
// columns it shows.
type gridState struct {
	page            db.Result // the rows on screen
	row, col        int       // the cursor
	top, left       int       // the first record and field shown
	transpose       bool
	colWidth        map[string]int // the columns set to a width by hand, by name (F17.14)
	transposeWidths map[int]int    // the records set to a width by hand when it is turned, by index on the page (F17.15)
	jv              *jsonView      // the JSON view, when it is in the table's place (F14.5)
	// the records marked with m, or by VISUAL from anchor to the cursor, by
	// position among the rows shown (F10.7); a new page or row puts them back.
	// unset: the range takes records out of the marked ones (F12.12)
	marked map[int]bool
	visual bool
	unset  bool
	anchor int
	widest *ui.Widest // what the grid measured of the rows on screen (F11.4)
}

// widestCache is the grid's memory of the widths of the rows on screen.
func (s *gridState) widestCache() *ui.Widest {
	if s.widest == nil {
		s.widest = &ui.Widest{}
	}
	return s.widest
}

// dataTab is a table open in a tab (§5, §7.6, §7.8「查询条」); its grid
// shows the columns COLS leaves.
type dataTab struct {
	gridState
	conn  *Conn // the connection the table is on (§5)
	table db.Table
	cols  db.Columns // the catalog's, as fetched: PK and types for the grid
	next  bool       // a page follows (§8.5)
	info  []db.Attr  // a key's type, TTL, encoding, memory and length, as the last page said (§8.7)
	// src is where the rows come from when it is not the table's own, a SQL
	// one built as each request needs it (§5「接口缝」); cursors, once a page
	// came with a cursor, are the cursor each page of the source starts at,
	// page 0's "": it pages by cursor, to the pages seen and one more.
	src     db.Source
	cursors []string
	seq     int       // the last request's; older answers are dropped (§8.3)
	bar     *errorBar // the last fetch's or save's error, under the table (§7.8「错误栏」)

	// The query bar's. request is what the last request asked for, shown
	// what the rows on screen came from: row numbers and chips are shown's,
	// and a cancelled request goes back to it (§7.6).
	request
	shown    request
	where    ui.Input         // the WHERE input
	ed       *editor.Editor   // its vim while typing is "where" (F3.39): where shows its line
	hidden   map[string]bool  // columns COLS hides
	colOrder []string         // the columns' names in the order COLS puts them; one it lacks goes after (F10.3)
	pageIn   ui.Input         // PAGE's page number, while typed
	typing   string           // the input that has the keys: "where", "page", "cell" or ""
	cell     *cellEdit        // the cell being edited, while typing is "cell" (§10.1)
	edits    map[editKey]edit // changes not saved, of every page (§10.1)
	added    []*newRow        // rows added, not saved: those at one place in the order they show (§10.6)
	deleted  map[string]bool  // rows marked for deletion, by row key (§10.6)
	saving   bool             // a save is on its way (§10.3)
	pending  bool             // restored from a layout: its connection's catalog is to come, then it fetches (F6.5)
	closing  bool             // :wq: the tab closes once the save lands, if nothing is left unsaved (§11)
	note     ui.Note          // how the last save went, until the next fetch or change (Q-06)
	failed   *string          // the row key a failed save names (§10.3), nil for none
	wantCol  string           // the column the cursor goes to once a page is in: a column node's ↵ (§7.8)
	wantRec  int              // the page row the cursor goes to once it is in, from 1; 0 for none: {N}G (F3.36)
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
	applied string        // the WHERE in effect: the input shows it unless typing
	order   []db.OrderCol // the columns sorted by, in order; none for the row identity
	desc    bool          // with none: the row identity turned
	limit   int           // rows a page: 100, 500 or 1000 (§8.5)
	pageNo  int           // from 0
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

func newDataTab(c *Conn, t db.Table) *dataTab {
	r := request{limit: limits[0]}
	return &dataTab{conn: c, table: t, request: r, shown: r, hidden: map[string]bool{}}
}

// setRequest makes r what t asks and shows, its WHERE typed as it is.
func (t *dataTab) setRequest(r request) {
	t.request, t.shown, t.where.Text = r, r, r.applied
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
	// cursor is where the page after it starts, for a source that pages by cursor;
	// info the line a key tab shows under its title (§8.7)
	cursor string
	info   []db.Attr
	err    error
}

// countMsg answers a count.
type countMsg struct {
	tab *dataTab
	seq int
	n   int64
	err error
}

// source is where t's rows come from; known is the catalog's columns of a
// SQL table, when the connection has them.
func (a *App) source(t *dataTab, known db.Columns) db.Source {
	if t.src != nil {
		return t.src
	}
	c := t.conn
	if c.redis() {
		return redis.Key{Meta: c.Meta, Main: c.Main, Name: t.table.Name, LoadAll: a.loadAll, ScanKeys: a.scanKeys, ReadOnly: c.readOnly, Type: t.keyType()}
	}
	return db.SQLTable{E: c.eng, Schema: t.table.Schema, Name: t.table.Name, Meta: c.Meta, Main: c.Main, Known: known, ReadOnly: c.readOnly}
}

// live is whether t's reads see what its connection wrote and has not
// committed (§8.2).
func (t *dataTab) live() bool { return t.conn.Manual || t.conn.inTx() }

// pageQueryOf is the page request r asks of a source whose columns are cols, the
// cursor of that page among cursors (those of the page before it).
func pageQueryOf(r request, cursors []string, live bool, cols db.Columns) db.PageQuery {
	q := db.PageQuery{Cols: cols, Filter: r.applied, Order: r.order, Limit: r.limit, Offset: r.pageNo * r.limit, Live: live}
	if len(q.Order) == 0 && r.desc { // the row identity turned
		for _, k := range cols.Key() {
			q.Order = append(q.Order, db.OrderCol{Col: k, Desc: true})
		}
	}
	if r.pageNo < len(cursors) {
		q.Cursor = cursors[r.pageNo]
	}
	return q
}

// keepCursor notes where the page after the one shown starts when the source
// pages by cursor, and drops what a page 0 starts anew.
func (t *dataTab) keepCursor(next string) {
	if t.shown.pageNo == 0 {
		t.cursors = nil
	}
	switch {
	case next != "":
		if len(t.cursors) == 0 {
			t.cursors = []string{""}
		}
		t.cursors = append(t.cursors[:min(len(t.cursors), t.shown.pageNo+1)], next)
	case len(t.cursors) > 0: // the last page
		t.cursors = t.cursors[:min(len(t.cursors), t.shown.pageNo+1)]
	}
}

// reach is page n as far as t can get to it: a source that pages by cursor
// goes to the pages it has seen and the next.
func (t *dataTab) reach(n int) int {
	if len(t.cursors) > 0 {
		return min(n, len(t.cursors)-1)
	}
	return n
}

// fetch reads the page t.request asks for on Meta, or in the manual mode
// or a transaction on Main, to see what was saved and not committed
// (§8.2), in a savepoint in a transaction: a WHERE that fails fails it
// alone. The table's columns
// come first, from Meta, when the catalog hasn't got them yet: they give
// the row identity it orders by (§8.4, §10.1). recount counts the rows
// again once a page is in (§8.3): owed by the tab, so a newer request
// taking this one's place still pays it. A tab restored from a layout waits
// for its connection and catalog: nothing goes out yet, the request it has
// by then is the one that does.
func (a *App) fetch(t *dataTab, recount bool) tea.Cmd {
	if t.pending || t.conn.State != connReady { // a layout's, or one reopened meanwhile: it fetches once its catalog is in (F6.5)
		t.pending = true
		return a.connect(t.conn) // R on the error page tries again (F12.5); nil while it connects
	}
	t.note, t.failed = ui.Note{}, nil // until the next fetch the user asks for (§10.3)
	// what is on its way is older than this now, whether this goes or not
	t.seq++
	// a ; would end the statement and start another: 1=1; drop table t (§9.6)
	if t.src == nil && t.conn.sql() && sqlkit.HasSemicolon(t.applied, t.conn.eng.Dialect()) {
		t.bar = &errorBar{ui.ErrorBar{First: ui.Note{Head: i18n.T("WHERE 里不能有 ;")}}, "fetch"}
		t.backToShown()
		return nil
	}
	t.recount = t.recount || recount
	t.conn.busy++
	t.out++
	seq, table := t.seq, t.table
	r, cursors, live := t.request, slices.Clone(t.cursors), t.live()
	cols := t.conn.cols[idOf(table)] // zero when the catalog has none yet
	src := a.source(t, cols)
	return func() tea.Msg {
		ctx := context.Background()
		cols, err := src.Columns(ctx)
		if err != nil {
			return pageMsg{tab: t, seq: seq, err: err}
		}
		p, err := src.Page(ctx, pageQueryOf(r, cursors, live, cols))
		return pageMsg{tab: t, seq: seq, cols: cols, page: p.Result, next: p.Next, cursor: p.Cursor, info: p.Info, err: err}
	}
}

// gotPage takes a page in: the rows, their request as what is shown, and
// then a count if one was asked for. An answer to an older request is
// dropped; a cancelled one goes back to what is shown, rows and all, with
// its count as it was (§7.6, §8.3).
func (a *App) gotPage(m pageMsg) tea.Cmd {
	if a.sessionOf(m.tab.conn) == nil { // its session closed, its connection with it: nothing to say (F5.1)
		return nil
	}
	m.tab.conn.busy--
	m.tab.out--
	if m.cols.Cols != nil {
		m.tab.conn.cols[idOf(m.tab.table)] = m.cols
	}
	t := m.tab
	switch {
	case m.seq != t.seq: // a newer request is on its way
		return nil
	case errors.Is(m.err, context.Canceled): // shown's count is still right: none owed
		t.request, t.recount, t.wantRec = t.shown, false, 0
		if t.typing != "where" {
			t.where = ui.Input{Text: t.applied, Pos: len(t.applied)}
		}
		return a.showToast(i18n.T("查询已取消"))
	case m.err != nil: // under the rows shown, which stay; its WHERE is there to be fixed (backToShown), the count owed
		// ponytail: no 位置 line, the SQL being ours, not the WHERE typed;
		// map Position into the WHERE if it is ever wanted
		t.bar = newErrorBar("fetch", t.conn.drv.ErrorOf(m.err), "", "")
		t.backToShown()
		return nil
	}
	clearBar(&t.bar, "fetch")
	t.cols, t.page, t.next, t.shown, t.info = m.cols, m.page, m.next, t.request, m.info
	t.keepCursor(m.cursor)
	t.deselect()                                                        // the rows are others
	t.colOrder = slices.DeleteFunc(t.colOrder, func(name string) bool { // a column that is gone (F10.3)
		return !slices.ContainsFunc(t.page.Cols, func(c db.Col) bool { return c.Name == name })
	})
	maps.DeleteFunc(t.colWidth, func(name string, _ int) bool { // and its width (F17.14)
		return !slices.ContainsFunc(t.page.Cols, func(c db.Col) bool { return c.Name == name })
	})
	t.row = max(min(t.row, len(t.shownRows())-1), 0)
	if t.wantRec > 0 {
		t.goRec(t.wantRec - 1)
		if t.wantRec = 0; dataOf(a.focused()) == t {
			a.gridMove(func(r, c, _, _ int) (int, int) { return r, c }) // into view
		}
	}
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
	t.request, t.applied, t.wantRec = t.shown, applied, 0
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
// ponytail: on Meta even in the manual mode or a transaction, so rows
// added or deleted there and not committed are not counted, the pages
// going by next all the same; count on Main, in Guarded, while in one if
// that misleads
func (a *App) count(t *dataTab) tea.Cmd {
	if strings.TrimSpace(t.applied) == "" && t.table.Rows > bigTable {
		t.count, t.counted = int64(t.table.Rows), estimated
		return nil
	}
	t.countSeq++
	t.counted = counting
	t.out++
	seq, filter, src := t.countSeq, t.applied, a.source(t, t.cols)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), countTimeout)
		defer cancel()
		n, err := src.Count(ctx, filter)
		return countMsg{tab: t, seq: seq, n: n, err: err}
	}
}

func (a *App) gotCount(m countMsg) {
	m.tab.out--
	if t := m.tab; m.seq == t.countSeq {
		t.count, t.counted = m.n, countDone
		if m.err != nil || m.n < 0 { // DeadlineExceeded past 3s, the WHERE's own error, or a source that cannot say
			t.counted = countLost
		}
	}
}

// fits is whether the first page has every row there is, a table's: what the
// query bar says is the rows of the page, and keeps saying when it is narrow (F19.4).
func (t *dataTab) fits() bool {
	return t.table.Kind != "k" && t.page.Cols != nil && t.shown.pageNo == 0 && !t.next
}

// counting is whether the query bar shows the dots where the count will be.
func (t *dataTab) counting() bool { return t.counted == counting && !t.fits() }

// pages is how many pages t's count makes, and whether that is known.
func (t *dataTab) pages() (int64, bool) {
	if t.counted != countDone && t.counted != estimated {
		return 0, false
	}
	return max((t.count+int64(t.shown.limit)-1)/int64(t.shown.limit), 1), true
}

// ordered is every column of the page, as indexes into the page's, in the
// order COLS puts them: those colOrder names first, the others after as the
// page has them, so a column that came with a refresh goes last (F10.3).
func (t *dataTab) ordered() []int {
	at := make(map[string]int, len(t.page.Cols))
	for i, c := range t.page.Cols {
		at[c.Name] = i
	}
	out, seen := make([]int, 0, len(t.page.Cols)), make([]bool, len(t.page.Cols))
	for _, name := range t.colOrder {
		if i, ok := at[name]; ok && !seen[i] {
			out, seen[i] = append(out, i), true
		}
	}
	for i := range t.page.Cols {
		if !seen[i] {
			out = append(out, i)
		}
	}
	return out
}

// shownCols is the columns COLS lets through, in its order, as indexes into
// the page's.
func (t *dataTab) shownCols() []int {
	var out []int
	for _, i := range t.ordered() {
		if !t.hidden[t.page.Cols[i].Name] {
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
// shown one after it in COLS's order, or else the last shown one.
func (t *dataTab) nearestShown(field int) int {
	o, s := t.ordered(), t.shownCols()
	at := slices.Index(o, field)
	if i := slices.IndexFunc(s, func(f int) bool { return slices.Index(o, f) >= at }); i >= 0 {
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
	t.typing, t.where, t.comp, t.hist, t.ed = "", ui.Input{Text: t.applied, Pos: len(t.applied)}, nil, nil, nil
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
	b.Min.Y = min(b.Min.Y+t.barRows(), b.Max.Y)
	b.Max.Y = max(b.Max.Y-t.bar.rows(), b.Min.Y)
	return b
}

// barRows is how tall t's query bar is: a key's has its Info row (§8.7).
func (t *dataTab) barRows() int {
	if t.table.Kind == "k" {
		return ui.QueryBarRows + ui.InfoRows
	}
	return ui.QueryBarRows
}

// keyLabels are the words of the Info row that are short of their name's.
var keyLabels = map[string]string{"length": "LEN", "encoding": "ENC", "memory": "MEM"}

// keyInfo is the row over a key tab's WHERE: what the key is, as its last page
// said, and what is done to it (§8.7).
func (a *App) keyInfo(t *dataTab) []ui.Chip {
	var chips []ui.Chip
	for _, at := range t.info {
		c := ui.Chip{Label: cmp.Or(keyLabels[at.Name], strings.ToUpper(at.Name)), Value: at.Value}
		switch at.Name {
		case "type":
			c.Icon = a.icons.KeyType(at.Value)
			c.Icon.Fg = a.keyColor(at.Value)
		case "ttl":
			c.Action = "key.ttl"
		}
		chips = append(chips, c)
	}
	return append(chips, ui.Chip{Label: "RENAME", Action: "key.rename"}, ui.Chip{Label: "DELETE", Action: "key.delete"})
}

// grid is t as pane p draws it: the columns COLS shows, with what the
// catalog said of them.
func (a *App) grid(p *Pane, t *dataTab) ui.Grid {
	g := ui.Grid{
		Row: t.row, Col: t.col, Top: t.top, Left: t.left, Transpose: t.transpose, FullWidth: a.fullWidth, First: t.shown.pageNo * t.shown.limit,
		Focused: a.win().Focus == p.ID, Key: a.icons.Key, Pane: p.ID, Selected: t.marked, Ranged: t.ranged(), FlashNums: a.flashNums(p), Widest: t.widestCache(), Heads: true, Edges: true, RecordWidths: t.transposeWidths,
	}
	shown := t.shownCols()
	for _, i := range shown {
		name := t.page.Cols[i].Name
		col := ui.GridCol{Name: name, PK: slices.Contains(t.cols.PK, name), Type: colType(t.typeOf(name)), Width: t.colWidth[name]}
		if at := slices.IndexFunc(t.shown.order, func(o db.OrderCol) bool { return o.Col == name }); at >= 0 { // not the default, which every table's id would show
			col.Sort = a.icons.SortAsc
			if t.shown.order[at].Desc {
				col.Sort = a.icons.SortDesc
			}
			if len(t.shown.order) > 1 {
				col.Rank = at + 1
			}
		}
		g.Cols = append(g.Cols, col)
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
	g.Yank = a.gridFlash(p)
	return g
}

// flashNums are the numbers of the records of a selection yanked in pane p,
// while it flashes (F10.7).
func (a *App) flashNums(p *Pane) map[int]bool {
	if f := a.flash; f != nil && f.pane == p.ID {
		return f.nums
	}
	return nil
}

// gridFlash is what a yank flashes of pane p's grid, if anything.
func (a *App) gridFlash(p *Pane) *[2]int {
	if f := a.flash; f != nil && f.nums != nil {
		return nil
	}
	if f := a.flash; f != nil && f.pane == p.ID && f.text.Mode == ui.SelNone {
		return &f.cell
	}
	return nil
}

// arrow is a way of sorting as the character that shows it.
func arrow(desc bool) string {
	if desc {
		return "↓"
	}
	return "↑"
}

// sortedBy is an ORDER as text: "id ↑", "amount ↓".
func sortedBy(col string, desc bool) string { return col + " " + arrow(desc) }

// orderText is a list of columns sorted by as text: "source ↓, id ↑".
func orderText(order []db.OrderCol) string {
	parts := make([]string, len(order))
	for i, o := range order {
		parts[i] = sortedBy(o.Col, o.Desc)
	}
	return strings.Join(parts, ", ")
}

// queryBar is the two rows over t's table, a key's three (§7.8「查询条」).
func (a *App) queryBar(p *Pane, t *dataTab) ui.QueryBar {
	ic := a.icons
	order := ui.Chip{Label: "ORDER", Value: "—", Action: "grid.order", IconAction: "grid.order.toggle", Stay: stayOrder} // no row identity to sort by
	switch s, key := t.shown, t.cols.Key(); {
	case len(s.order) == 1:
		order.Value, order.Icon = s.order[0].Col, ic.SortAsc
		if s.order[0].Desc {
			order.Icon = ic.SortDesc
		}
	case len(s.order) > 1: // each column with its arrow; the icon is the first's, a click turns it (F12.1)
		order.Value, order.Icon = orderText(s.order), ic.SortAsc
		if s.order[0].Desc {
			order.Icon = ic.SortDesc
		}
		if len(s.order) > 2 {
			order.Short = orderText(s.order[:2]) + ", …"
		}
	case key != nil:
		order.Value, order.Icon = strings.Join(key, ","), ic.SortAsc
		if s.desc {
			order.Icon = ic.SortDesc
		}
	}
	pages := "?"
	if n, ok := t.pages(); ok {
		pages = strconv.FormatInt(n, 10)
		if t.counted == estimated {
			pages = "~" + pages
		}
	}
	page := ui.Chip{Label: "PAGE", Value: fmt.Sprintf("%d/%s", t.shown.pageNo+1, pages), Action: "grid.page", Stay: stayPage}
	if t.typing == "page" {
		page.Input, page.Suffix = &t.pageIn, "/"+pages
	}
	rows, keep := a.icons.Spinner(a.spin), ""
	switch {
	case t.fits(): // all there is is on the page: no count to wait for, and the part of the bar that stays (F19.4)
		rows = strconv.Itoa(len(t.page.Rows))
		keep = i18n.T("%s 行", rows)
	case t.counted == countLost:
		rows = "?"
	case t.counted == countDone:
		rows = strconv.FormatInt(t.count, 10)
	case t.counted == estimated:
		rows = "~" + ui.Magnitude(float64(t.count))
	}
	right := i18n.T("auto · %s 行", rows)
	if t.page.Cols != nil {
		right += " · " + t.page.Took.Round(time.Millisecond).String()
	}
	names, _ := a.sqlNames(t.conn, t.where.Text, "", &t.cols)
	where := t.where
	if t.typing == "where" {
		where.Sel = vimSel(t.ed)
		if f := a.flash; f != nil && f.pane == p.ID {
			where.Yank = f.text
		}
	}
	var selected string // a segment of its own, not the row count's (F10.7)
	if n := len(t.selected()); n > 0 {
		selected = i18n.T("已选 %d", n)
	}
	chips := []ui.Chip{
		order,
		{Label: "LIMIT", Value: strconv.Itoa(t.shown.limit), Action: "grid.limit", Stay: stayLimit},
		page,
		{Label: "COLS", Value: fmt.Sprintf("%d/%d", len(t.shownCols()), len(t.page.Cols)), Action: "grid.cols", Stay: stayCols},
	}
	if t.jv != nil { // the view says so, and a click takes the table back (F14.5)
		chips = append(chips, ui.Chip{Label: "JSON", Action: "grid.view.json", Stay: stayOrder})
	}
	bar := ui.QueryBar{
		Selected: selected, SelectedStay: staySelected,
		Where: where, Typing: t.typing == "where", Pane: p.ID, Right: right, Keep: keep, Note: t.note, Names: names, Dialect: t.conn.dialect(),
		History: ic.History, HistKey: a.keys.Hint("where.history", "input"),
		Chips:   chips,
		Buttons: a.toolButtons(t),
	}
	if t.table.Kind == "k" { // its filter is a MATCH pattern (§8.7)
		bar.Info, bar.Label = a.keyInfo(t), "MATCH"
	}
	return bar
}

// What gives way on the query bar's second row, the least first (§7.8「第二行
// 放不下时谁让位」), the only place that says: transpose, auto refresh and stop,
// + −, then COLS, the count of records selected, PAGE, LIMIT, then save and refresh, which has the changes'
// count, then ORDER.
const (
	stayTranspose = iota
	stayAuto
	stayRows
	stayCols
	staySelected
	stayPage
	stayLimit
	staySave
	stayOrder
)

// toolButtons are t's query bar buttons (§7.8「工具按钮」): + −, save and
// refresh, auto refresh and stop, transpose and export, giving way in the order above.
// Stop and auto refresh go by their state: an [icon] color only when lit.
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
		{Buttons: []ui.Button{{Icon: ic.RowAdd, Action: "grid.row.add", Fg: th.Focus}, {Icon: ic.RowDelete, Action: "grid.row.delete", Fg: th.Error}}, Stay: stayRows},
		{Buttons: []ui.Button{save, {Icon: ic.Refresh, Action: "grid.refresh", Fg: th.Info}}, Stay: staySave},
		{Buttons: []ui.Button{auto, stop}, Stay: stayAuto},
		{Buttons: []ui.Button{{Icon: ic.Transpose, Action: "grid.transpose", Fg: th.Info}, {Icon: ic.Export, Action: "grid.export", Fg: th.Info}}, Stay: stayTranspose},
	}
}

// autoIntervals are auto refresh's choices, off first (§7.8「自动刷新」).
var autoIntervals = []time.Duration{0, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}

// autoLabel is an interval as its dropdown and button say it: 关, 5s, 60s.
func autoLabel(d time.Duration) string {
	if d == 0 {
		return i18n.T("关")
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
// edited, nothing on its way and no record selected; else it skips a turn. The ticking stops
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
	if shown && t.changes() == 0 && t.cell == nil && t.out == 0 && !t.selecting() {
		note, failed := t.note, t.failed
		cmd = a.fetch(t, true)
		t.note, t.failed = note, failed
	}
	return tea.Batch(cmd, autoTick(t))
}

// tabOpen reports whether t is a tab in some pane of any session: one in
// the background skips its turns, not stops (§7.8「自动刷新」).
func (a *App) tabOpen(t *dataTab) bool {
	for _, s := range a.sessions {
		for _, p := range s.panes() {
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
func (a *App) focusedGrid() (*dataTab, bool) {
	t := dataOf(a.focused())
	return t, t != nil && len(t.shownCols()) > 0
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
		Rows: t.page.Rows, Row: t.row, Col: t.col, Top: t.top, Left: t.left, Transpose: t.transpose, FullWidth: a.fullWidth,
		Focused: a.win().Focus == p.ID, Key: a.icons.Key, Pane: p.ID, Yank: a.gridFlash(p), Selected: t.marked, Ranged: t.ranged(), FlashNums: a.flashNums(p), Widest: t.widestCache(), Edges: true, RecordWidths: t.transposeWidths,
	}
	for _, c := range t.page.Cols {
		g.Cols = append(g.Cols, ui.GridCol{Name: c.Name, Type: colType(c.Type), Width: t.colWidth[c.Name]})
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
		s.transpose, s.jv = !s.transpose, nil // the JSON view and the turned table are two views of it, one at a time (F14.5)
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

// gridLine is gg and G (§7.6): the first or the last row; with a count N
// as nvim's, row N, by the numbers a table's rows show: on its page,
// fetched when that isn't the one shown or another is on its way, the
// pages' count as PAGE's input takes it (§7.8), N past the rows counted
// the last row. Transposed, and
// in the result area, the Nth row on screen.
func (a *App) gridLine(n int, bottom bool) tea.Cmd {
	t := dataOf(a.focused())
	if n == 0 || t == nil || t.transpose {
		a.gridMove(func(_, c, rows, _ int) (int, int) {
			if n == 0 && bottom {
				return rows - 1, c
			}
			return max(n-1, 0), c
		})
		return nil
	}
	if t.counted == countDone {
		n = min(n, int(max(t.count, 1)))
	}
	page := (n - 1) / t.shown.limit
	if pages, ok := t.pages(); ok {
		page = min(page, int(pages)-1)
	}
	page = t.reach(page)
	rec := n - 1 - page*t.shown.limit
	if page != t.shown.pageNo || t.pageNo != t.shown.pageNo { // another page on its way: this one instead
		t.pageNo, t.wantRec = page, rec+1
		return a.fetch(t, false)
	}
	t.goRec(rec)
	a.gridMove(func(r, c, _, _ int) (int, int) { return r, c }) // into view
	return nil
}

// goRec puts the cursor on page row rec, or the page's last when it has
// fewer rows.
func (t *dataTab) goRec(rec int) {
	rows := t.shownRows()
	if i := slices.IndexFunc(rows, func(sr shownRow) bool { return sr.add == nil && sr.rec == min(rec, len(t.page.Rows)-1) }); i >= 0 {
		t.row = i
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

// typeKey edits the query bar input that has the keys (§7.8): the WHERE's
// vim, a cell's edit, or PAGE's, which ↵ runs and esc drops.
func (a *App) typeKey(t *dataTab, k keymap.Key) tea.Cmd {
	switch {
	case t.typing == "where":
		return a.whereKey(t, k)
	case t.cell != nil: // ↵ and esc are cell.accept and cell.done (§10.2)
		if editInput(&t.cell.in, k) {
			t.cell.sel = -1 // the options change with it: none picked (§10.2)
		}
		return nil
	case k == keymap.Esc:
		t.stopTyping()
		return nil
	case k != "<CR>":
		editInput(&t.pageIn, k)
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(t.pageIn.Text))
	t.stopTyping()
	if err != nil {
		return nil
	}
	if pages, ok := t.pages(); ok { // past the ends: to the end
		n = min(n, int(pages))
	}
	t.pageNo = t.reach(max(n-1, 0))
	return a.fetch(t, false)
}

// baseType is a catalog type's name without its modifier: format_type
// puts one at most, "numeric(10,2)", "timestamp(3) with time zone".
func baseType(t string) string {
	if i, j := strings.Index(t, "("), strings.Index(t, ")"); 0 <= i && i < j {
		return t[:i] + t[j+1:]
	}
	return t
}

// plainType is catalog type t in lower case without its (n), nor the
// unsigned and zerofill of MySQL's, which the driver puts first and the
// catalog last: the name that says what the values are.
func plainType(t string) string {
	t = strings.ToLower(baseType(t))
	return strings.TrimPrefix(strings.TrimSuffix(strings.TrimSuffix(t, " zerofill"), " unsigned"), "unsigned ")
}

// timeKind is the parts a column of catalog type t steps (§10.2), PG's
// and MySQL's; interval has none.
func timeKind(t string) ui.TimeKind {
	return map[string]ui.TimeKind{
		"date": ui.Date, "time without time zone": ui.Time, "time with time zone": ui.TimeTZ,
		"timestamp without time zone": ui.Timestamp, "timestamp with time zone": ui.TimestampTZ,
		"time": ui.Time, "datetime": ui.Timestamp, "timestamp": ui.Timestamp,
	}[baseType(t)]
}

// colType is the grid's class for a column of the catalog's type (§7.6).
func colType(t string) ui.ColType {
	switch {
	case strings.HasSuffix(t, "[]"):
		return ui.ColOther
	case t == "tinyint(1)": // MySQL's boolean
		return ui.ColBool
	}
	t = plainType(t)
	// after format_type's names, pgx's for a result's OIDs (quick SQL, §12),
	// then MySQL's COLUMN_TYPE and the driver's
	switch t {
	case "smallint", "integer", "bigint", "numeric", "real", "double precision", "oid",
		"int2", "int4", "int8", "float4", "float8",
		"tinyint", "mediumint", "int", "decimal", "float", "double":
		return ui.ColNumber
	case "date", "time without time zone", "time with time zone",
		"timestamp without time zone", "timestamp with time zone",
		"time", "timetz", "timestamp", "timestamptz",
		"datetime", "year":
		return ui.ColTime
	case "boolean", "bool":
		return ui.ColBool
	case "json", "jsonb":
		return ui.ColJSON
	case "text", "character varying", "character", `"char"`, "name", "citext",
		"varchar", "bpchar", "char",
		"tinytext", "mediumtext", "longtext", "enum", "set":
		return ui.ColString
	}
	if t == "interval" || strings.HasPrefix(t, "interval ") { // "interval day to second"
		return ui.ColTime
	}
	return ui.ColOther
}

// keyType is the type of the key a key tab shows, as the last page said, "" before it.
func (t *dataTab) keyType() string {
	for _, at := range t.info {
		if at.Name == "type" {
			return at.Value
		}
	}
	return ""
}

// resizeCol is > and < (F17.14): the column the cursor is on is set to the width
// it is drawn at, d columns wider or narrower, 1 at least, and stays that wide; turned, the
// column is the record's (F17.15).
func (a *App) resizeCol(d int) {
	s, g, area, ok := a.gridOf(a.focused())
	if !ok {
		return
	}
	i := s.col
	if s.transpose {
		i = s.row
	}
	if ws := g.ColWidths(area); i < len(ws) { // a turned grid with no records has none
		name := ""
		if !s.transpose {
			name = g.Cols[i].Name
		}
		a.setColWidth(s, i, name, ws[i]+d)
	}
}

// setColWidth sets the column called name to w columns, 1 at least; turned, the column is
// record i and name is not read.
func (a *App) setColWidth(s *gridState, i int, name string, w int) {
	if s.transpose {
		if s.transposeWidths == nil {
			s.transposeWidths = map[int]int{}
		}
		s.transposeWidths[i] = max(w, 1)
		return
	}
	if s.colWidth == nil {
		s.colWidth = map[string]int{}
	}
	s.colWidth[name] = max(w, 1)
}

// fitCols is =: every column of the focused grid is automatic again, the records' too (F17.15).
func (a *App) fitCols() {
	if s, _, _, ok := a.gridOf(a.focused()); ok {
		s.colWidth, s.transposeWidths = nil, nil
	}
}

// colDrag is a column's width being dragged by its head's right edge (F17.14): the width
// it had and where the pointer was when it was pressed.
type colDrag struct {
	pane, col int
	name      string
	w0, x0    int
}

// startColDrag begins the drag of a column by its edge, pressed at column x of the screen.
func (a *App) startColDrag(pane, col, x int) {
	a.focusPane(pane)
	s, g, area, ok := a.gridOf(a.win().pane(pane))
	if !ok {
		return
	}
	name, n := "", len(g.Rows) // turned, the columns are the records (F17.15)
	if !s.transpose {
		n = len(g.Cols)
	}
	if col >= n {
		return
	}
	if !s.transpose {
		name = g.Cols[col].Name
	}
	a.dragCol = &colDrag{pane: pane, col: col, name: name, w0: g.ColWidths(area)[col], x0: x}
}

// dragColTo follows the pointer: the column is as wide as it was and the pointer moved.
func (a *App) dragColTo(x int) {
	d := a.dragCol
	if s, _, _, ok := a.gridOf(a.win().pane(d.pane)); ok {
		a.setColWidth(s, d.col, d.name, d.w0+x-d.x0)
	}
}
