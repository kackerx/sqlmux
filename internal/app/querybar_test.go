package app

import (
	"context"
	"errors"
	"image/color"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// recDB records the SQL it is sent and answers with nothing.
type recDB struct {
	mu   sync.Mutex
	sqls []string
}

func (r *recDB) Query(_ context.Context, sql string, _ ...db.Val) (db.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sqls = append(r.sqls, sql)
	return db.Result{}, nil
}
func (r *recDB) Exec(context.Context, string, int) ([]db.Result, error) { return nil, nil }
func (r *recDB) Close() error                                           { return nil }

// lastSQL runs the fetch cmd starts, when it is the page's alone, and is
// the SQL it sent.
func lastSQL(t *testing.T, rec *recDB, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("no fetch")
	}
	if _, ok := cmd().(pageMsg); !ok {
		t.Fatal("not a page fetch")
	}
	return rec.sqls[len(rec.sqls)-1]
}

// withRecorder is sized with a t_order loaded and Meta recording SQL.
func withRecorder(t *testing.T) (*App, *dataTab, *recDB) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 100)
	rec := &recDB{}
	a.sess.Conns[0].Meta = db.NewWorker(rec)
	return a, tab, rec
}

// / types a WHERE (INSERT, -- editing WHERE --); ↵ runs it from the first
// page and counts again; esc goes to its vim's NORMAL, esc again puts back
// what is in effect (§7.8, F3.39).
func TestWhere(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.pageNo = 3
	feed(t, a, "/id > 5")
	if a.mode() != keymap.Insert || a.statusLine().Info != "-- editing WHERE --" || tab.applied != "" {
		t.Fatalf("typing: mode %v info %q applied %q", a.mode(), a.statusLine().Info, tab.applied)
	}
	f := a.render()
	if f.Cursor == nil || !strings.Contains(f.String(), "WHERE id > 5") {
		t.Fatalf("the input and its cursor: %v", f.Cursor)
	}
	_, cmd := a.Update(teaKey("<CR>"))
	if tab.applied != "id > 5" || tab.pageNo != 0 || tab.typing != "" || tab.counted != counting || cmd == nil {
		t.Fatalf("↵: applied %q page %d typing %q counted %v", tab.applied, tab.pageNo, tab.typing, tab.counted)
	}
	feed(t, a, "/<BS><BS>9<Esc>")
	if tab.where.Text != "id >9" || tab.typing != "where" || a.mode() != keymap.Normal || a.statusLine().Info != "-- editing WHERE --" {
		t.Fatalf("esc: input %q typing %q mode %v", tab.where.Text, tab.typing, a.mode())
	}
	feed(t, a, "<Esc>")
	if tab.where.Text != "id > 5" || tab.applied != "id > 5" || a.mode() != keymap.Normal || tab.typing != "" {
		t.Errorf("esc: input %q applied %q", tab.where.Text, tab.applied)
	}
	feed(t, a, "/x")
	a.win().focus(0)
	if tab.typing != "" || tab.where.Text != "id > 5" {
		t.Errorf("leaving the pane: typing %q input %q", tab.typing, tab.where.Text)
	}
}

// A WHERE with a ; outside strings goes nowhere: the error bar says so
// under the table, and the history does not keep it (§9.6).
func TestWhereSemicolon(t *testing.T) {
	a, tab, rec := withRecorder(t)
	sent := len(rec.sqls)
	feed(t, a, "/1=1; drop table t_log")
	if _, cmd := a.Update(teaKey("<CR>")); cmd != nil {
		cmd()
	}
	if len(rec.sqls) != sent || tab.bar == nil || tab.bar.First.Head != "WHERE 里不能有 ;" || len(a.tableState(tab).History) != 0 {
		t.Fatalf("sent %q, bar %+v, history %+v", rec.sqls[sent:], tab.bar, a.tableState(tab).History)
	}
	if !strings.Contains(a.render().String(), "WHERE 里不能有 ;") {
		t.Error("not drawn")
	}
	feed(t, a, "/"+strings.Repeat("<BS>", 30)+"note = ';'")
	if _, cmd := a.Update(teaKey("<CR>")); cmd == nil {
		t.Error("a ; in a string goes")
	}
}

// A page asked for before a WHERE was refused does not take its place when
// it comes: the refusal stays, and no count follows it with the ; (§9.6).
func TestWhereSemicolonLatePage(t *testing.T) {
	a, tab, rec := withRecorder(t)
	feed(t, a, "/id > 5")
	_, early := a.Update(teaKey("<CR>"))
	feed(t, a, "/"+strings.Repeat("<BS>", 10)+"1=1; drop table t_log<CR>")
	var page tea.Msg
	for _, m := range early().(tea.BatchMsg) {
		if msg := m(); msg != nil {
			if _, ok := msg.(pageMsg); ok {
				page = msg
			}
		}
	}
	if _, cmd := a.Update(page); cmd != nil || tab.bar == nil {
		t.Fatalf("the late page: bar %+v, a cmd after it %v", tab.bar, cmd != nil)
	}
	for _, s := range rec.sqls {
		if strings.Contains(s, "drop table") {
			t.Errorf("sent: %q", s)
		}
	}
}

// orderOf is columns sorted by, a trailing - for descending: "status-", "id".
func orderOf(specs ...string) []db.OrderCol {
	var out []db.OrderCol
	for _, s := range specs {
		col, desc := strings.CutSuffix(s, "-")
		out = append(out, db.OrderCol{Col: col, Desc: desc})
	}
	return out
}

// The page query carries the WHERE, the ORDER with the row identity after
// it, the LIMIT and the page (§8.5, §9.6).
func TestPageSQL(t *testing.T) {
	a, tab, rec := withRecorder(t)
	tab.applied, tab.order, tab.limit, tab.pageNo = "status = 'done' -- 备注", orderOf("status-"), 500, 2
	sql := lastSQL(t, rec, a.fetch(tab, false))
	want := `select * from "public"."t_order" where (` + "\nstatus = 'done' -- 备注\n" + `) order by "status" desc, "id" limit 501 offset 1000`
	if sql != want {
		t.Errorf("sql\n%s\nwant\n%s", sql, want)
	}
}

// ] and [ turn pages, not past either end; row numbers and row,col count
// from the page's start (§7.8).
func TestPaging(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "[")
	if tab.pageNo != 0 {
		t.Fatal("[ on the first page")
	}
	tab.next = true
	feed(t, a, "]")
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: tab.cols, page: tab.page, next: false})
	feed(t, a, "jj")
	if tab.pageNo != 1 || !strings.Contains(statusRow(a), " 103,1 ") || !strings.Contains(a.render().String(), "│ 103 │") {
		t.Fatalf("page 2: page %d, status %q", tab.pageNo, statusRow(a))
	}
	feed(t, a, "]")
	if tab.pageNo != 1 {
		t.Error("] on the last page")
	}
	feed(t, a, "gp")
	if tab.typing != "page" || !strings.Contains(a.render().String(), "PAGE [2]/?") {
		t.Fatalf("gp: typing %q", tab.typing)
	}
	tab.count, tab.counted = 250, countDone // 3 pages of 100
	feed(t, a, "<BS>9<CR>")
	if tab.pageNo != 2 || tab.typing != "" {
		t.Errorf("gp 9: page %d, want the last, 3", tab.pageNo+1)
	}
}

// ORDER is a list under its chip, LIMIT a dropdown (§7.8).
func TestOrderAndLimit(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.pageNo = 4
	feed(t, a, "go")
	if a.cols == nil || !a.cols.order || a.cols.sel != 0 || a.mode() != keymap.Command {
		t.Fatalf("go: %+v", a.cols)
	}
	chip := a.chipRect(a.focused(), tab, "grid.order")
	if box, _ := a.colsBox(a.colsView()); chip.Empty() || box.Min.X != chip.Min.X || box.Min.Y != chip.Max.Y {
		t.Errorf("the list opens under its chip: %v, chip %v", box, chip)
	}
	feed(t, a, "j<CR>") // status, the second column: keys, not characters
	answer(a, tab)
	if !slices.Equal(tab.order, orderOf("status")) || tab.pageNo != 0 || a.cols != nil {
		t.Fatalf("status: order %v page %d", tab.order, tab.pageNo)
	}
	feed(t, a, "go")
	if a.cols.sel != 1 {
		t.Errorf("it opens on the column sorted by: %d", a.cols.sel)
	}
	feed(t, a, "<CR>")
	answer(a, tab)
	if !slices.Equal(tab.order, orderOf("status-")) || !strings.Contains(a.render().String(), " ORDER status "+ui.NerdIcons.SortDesc.Text+" ") {
		t.Errorf("status again: %v", tab.order)
	}
	feed(t, a, "goA<Esc>")
	answer(a, tab)
	if len(tab.order) != 0 || a.cols != nil || !strings.Contains(a.render().String(), " ORDER id "+ui.NerdIcons.SortAsc.Text+" ") {
		t.Errorf("default: order %v", tab.order)
	}
	feed(t, a, "gl<C-n><CR>")
	answer(a, tab)
	if tab.limit != 500 || !strings.Contains(a.render().String(), " LIMIT 500 ") {
		t.Errorf("gl: limit %d", tab.limit)
	}
	click(a, a.chipRect(a.focused(), tab, "grid.limit").Min)
	if a.drop == nil || a.drop.kind != dropLimit {
		t.Error("clicking the LIMIT chip opens its dropdown")
	}
}

// The buttons' icons are info, ORDER's direction warn, unless a theme
// gives them a color (§7.7); a button is " <icon> ", lit whole under the
// pointer (§7.8).
func TestQueryBarIconColors(t *testing.T) {
	a, _, _ := withRecorder(t)
	a.removePane(2) // wide enough for every group
	iconAt := func(action string) uv.Style {
		r := find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: action})
		return a.render().Buf.CellAt(r.Min.X+1, r.Min.Y).Style // past the space before it
	}
	if fg := iconAt("grid.refresh").Fg; fg != a.theme.Info {
		t.Errorf("refresh: %v", fg)
	}
	r := find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.order.toggle"})
	if fg := a.render().Buf.CellAt(r.Min.X, r.Min.Y).Style.Fg; fg != a.theme.Warn {
		t.Errorf("sort: %v", fg)
	}
	icons := *a.icons
	icons.Refresh = ui.Icon{Text: "R", Fg: a.theme.Error}
	a.icons = &icons
	if fg := iconAt("grid.refresh").Fg; fg != a.theme.Error {
		t.Errorf("a theme's refresh: %v", fg)
	}
	r = find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.refresh"})
	a.Update(tea.MouseMotionMsg{X: r.Min.X + 2, Y: r.Min.Y})
	f := a.render()
	for x := r.Min.X; x < r.Max.X; x++ {
		if bg := f.Buf.CellAt(x, r.Min.Y).Style.Bg; bg != a.theme.Select {
			t.Errorf("hovered refresh, column %d: bg %v", x-r.Min.X, bg)
		}
	}
	if r.Dx() != 3 {
		t.Errorf("refresh hits %d columns, want \" <icon> \"", r.Dx())
	}
}

// Clicking ORDER's direction turns it; sorted by default, it is the row
// identity descending. The rest of the chip opens the dropdown (§7.8).
func TestOrderToggle(t *testing.T) {
	a, tab, rec := withRecorder(t)
	icon := func() uv.Position {
		return find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.order.toggle"}).Min
	}
	tab.pageNo = 2
	if sql := lastSQL(t, rec, click(a, icon())); !slices.Equal(tab.order, orderOf("id-")) || tab.pageNo != 0 || !strings.Contains(sql, `order by "id" desc limit`) {
		t.Fatalf("default: order %v page %d, %s", tab.order, tab.pageNo, sql)
	}
	answer(a, tab)
	if sql := lastSQL(t, rec, click(a, icon())); !slices.Equal(tab.order, orderOf("id")) || !strings.Contains(sql, `order by "id" asc limit`) {
		t.Errorf("again: %s", sql)
	}
	answer(a, tab)
	if !strings.Contains(a.render().String(), " ORDER id "+ui.NerdIcons.SortAsc.Text+" ") {
		t.Error("the icon follows")
	}
	// two buttons, each lit on its own under the pointer
	chip, ir := a.chipRect(a.focused(), tab, "grid.order"), find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.order.toggle"})
	for _, c := range []struct {
		at         uv.Position
		chip, icon bool
	}{{ir.Min, false, true}, {chip.Min, true, false}} {
		a.Update(tea.MouseMotionMsg{X: c.at.X, Y: c.at.Y})
		f := a.render()
		if lit := f.Buf.CellAt(chip.Min.X+1, chip.Min.Y).Style.Bg == a.theme.Select; lit != c.chip {
			t.Errorf("pointer at %v: the chip lit %v", c.at, lit)
		}
		if lit := f.Buf.CellAt(ir.Min.X, ir.Min.Y).Style.Bg == a.theme.Select; lit != c.icon {
			t.Errorf("pointer at %v: the icon lit %v", c.at, lit)
		}
	}
	click(a, chip.Min)
	if a.cols == nil || !a.cols.order {
		t.Error("the chip's label opens the list")
	}
	a.cols = nil
	tab.cols = db.Columns{PK: []string{"occurred_at", "id"}, Cols: tab.cols.Cols}
	tab.order, tab.desc = nil, false
	if sql := lastSQL(t, rec, a.run("grid.order.toggle", 0)); !slices.Equal(tab.order, orderOf("occurred_at-")) || !strings.Contains(sql, `order by "occurred_at" desc, "id" limit`) {
		t.Errorf("a composite key: %v, %s", tab.order, sql)
	}
	tab.cols = db.Columns{PK: []string{"note", "id"}, Cols: tab.cols.Cols}
	tab.order, tab.desc = nil, false
	feed(t, a, "gojjjjj<CR>") // the list: the key's first column is the chip's, so it turns (§7.8)
	if !slices.Equal(tab.order, orderOf("note-")) {
		t.Errorf("list on a composite key's first column: %v", tab.order)
	}
	tab.cols = db.Columns{Cols: tab.cols.Cols}
	tab.order = nil
	if a.run("grid.order.toggle", 0) != nil || strings.Contains(a.render().String(), " ORDER — "+ui.NerdIcons.SortAsc.Text) {
		t.Error("no row identity: nothing to turn, no icon")
	}
}

// COLS (Q-04): the list has the keys, / the filter; hidden columns leave
// the grid and the cursor moves off them; esc clears, then closes.
func TestCols(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "ll") // on amount
	feed(t, a, "gc")
	if a.cols == nil || a.mode() != keymap.Command {
		t.Fatalf("gc: %+v mode %v", a.cols, a.mode())
	}
	feed(t, a, "jj<Space>")
	if !tab.hidden["amount"] || tab.col != 2 || tab.page.Cols[tab.fieldAt(tab.col)].Name != "paid" {
		t.Fatalf("hiding amount: hidden %v, cursor on %d", tab.hidden, tab.col)
	}
	if f := a.render().String(); !strings.Contains(f, " COLS 6/7 ") || strings.Contains(f, " amount ") && !strings.Contains(f, "[ ] amount") {
		t.Errorf("the chip and the grid:\n%s", f)
	}
	feed(t, a, "/at")
	if a.mode() != keymap.Command || len(a.colsMatches()) != 3 { // status, amount, created_at
		t.Fatalf("filter: mode %v, %d matches", a.mode(), len(a.colsMatches()))
	}
	if !strings.Contains(a.render().String(), "3/7") {
		t.Error("the match count")
	}
	feed(t, a, "<Esc>A")
	if !tab.hidden["status"] || !tab.hidden["created_at"] || tab.hidden["id"] || tab.hidden["meta"] {
		t.Errorf("A hides what the filter shows: %v", tab.hidden)
	}
	feed(t, a, "a<Esc>")
	if a.cols == nil || a.cols.filter.Text != "" || tab.hidden["status"] {
		t.Fatalf("esc clears first: %+v", a.cols)
	}
	feed(t, a, "<Esc>")
	if a.cols != nil {
		t.Error("then closes")
	}
}

// J and K in COLS move the column under the selection down and up a place,
// the table's order and the cursor's column with it; yy, yi and Y go by that
// order, hidden columns in Y and yi too; with the filter on J does nothing; a
// column the refresh brings goes last, one it takes away leaves the order
// (F10.3).
func TestColsMove(t *testing.T) {
	a, tab, _ := withRecorder(t)
	names := func() string {
		var s []string
		for _, f := range tab.shownCols() {
			s = append(s, tab.page.Cols[f].Name)
		}
		return strings.Join(s, " ")
	}
	feed(t, a, "ll") // on amount
	feed(t, a, "gcjjK")
	if names() != "id amount status paid meta note created_at" || tab.col != 1 || a.cols.sel != 1 {
		t.Fatalf("K on amount: %s, cursor %d, selected %d", names(), tab.col, a.cols.sel)
	}
	if feed(t, a, "KK"); names() != "amount id status paid meta note created_at" || a.cols.sel != 0 {
		t.Errorf("K at the top stays: %s, selected %d", names(), a.cols.sel)
	}
	if feed(t, a, "JJJ"); names() != "id status paid amount meta note created_at" || tab.col != 3 {
		t.Errorf("J three times: %s, cursor %d", names(), tab.col)
	}
	if v := a.colsView(); v.Items[3] != "amount" || v.Items[0] != "id" {
		t.Errorf("the list follows the order: %v", v.Items)
	}
	feed(t, a, "/am<CR>J") // filtered: not all of the list is there
	if names() != "id status paid amount meta note created_at" {
		t.Errorf("J with the filter on: %s", names())
	}
	feed(t, a, "<Esc>")

	text := func(cmd tea.Cmd) string { s, _ := clipText(cmd); return s }
	// the copies go by the order: yy the shown columns, yi and Y every one
	feed(t, a, "<Esc>")
	tab.hidden["status"] = true
	if cmd := keys(t, a, "yy"); !strings.HasPrefix(text(cmd), "1\tt\t1.99\t") {
		t.Errorf("yy: %q", text(cmd))
	}
	if cmd := keys(t, a, "yi"); !strings.Contains(text(cmd), `("id", "status", "paid", "amount", "meta", "note", "created_at")`) {
		t.Errorf("yi: %q", text(cmd))
	}
	keysOf := regexp.MustCompile(`(?m)^  "(\w+)":`).FindAllStringSubmatch(text(keys(t, a, "Y")), -1)
	var got []string
	for _, k := range keysOf {
		got = append(got, k[1])
	}
	if strings.Join(got, " ") != "id status paid amount meta note created_at" {
		t.Errorf("Y: %v", got)
	}

	// a refresh: a column it brings goes last, one it takes away leaves the order
	page := db.Result{}
	for _, c := range tab.page.Cols {
		if c.Name != "meta" {
			page.Cols = append(page.Cols, c)
		}
	}
	page.Cols = append(page.Cols, db.Col{Name: "extra"})
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: tab.cols, page: page})
	if got := strings.Join(tab.colOrder, " "); got != "id status paid amount note created_at" || names() != "id paid amount note created_at extra" {
		t.Errorf("refreshed: order %q, shown %q", got, names())
	}
}

// A column hidden has the cursor go to the next one shown in COLS's order,
// not the page's: amount moved to the front and hidden leaves the cursor on
// id, which is after it now, not on paid (F10.3).
func TestColsHiddenInOrder(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "llgcjjKK") // on amount, which goes to the front
	if tab.page.Cols[tab.fieldAt(tab.col)].Name != "amount" || tab.col != 0 {
		t.Fatalf("amount first, the cursor on it: %d", tab.col)
	}
	feed(t, a, "<Space>")
	if got := tab.page.Cols[tab.fieldAt(tab.col)].Name; got != "id" || tab.col != 0 {
		t.Errorf("amount hidden: the cursor is on %q", got)
	}
}

// J and K with the list's selection past what the table has left, a refresh
// having taken a column, do nothing (F10.3).
func TestColsShiftAfterShrink(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "gcjjjjjj") // the last of 7
	page := db.Result{Cols: slices.Clone(tab.page.Cols[:6])}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: tab.cols, page: page})
	feed(t, a, "K")
	feed(t, a, "J")
	if len(tab.colOrder) != 0 || a.cols == nil {
		t.Errorf("the order %v, the list %v", tab.colOrder, a.cols != nil)
	}
}

// A name twice in the order, as a layout file written by hand may have it,
// shows its column once; the ORDER list has the columns as the grid does
// (F10.3).
func TestColsOrderRepeated(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.colOrder = []string{"id", "id", "amount", "gone"}
	var names []string
	for _, f := range tab.shownCols() {
		names = append(names, tab.page.Cols[f].Name)
	}
	if got := strings.Join(names, " "); got != "id amount status paid meta note created_at" {
		t.Errorf("shown: %s", got)
	}
	feed(t, a, "go")
	var items []string
	for _, m := range a.colsMatches() {
		items = append(items, tab.page.Cols[m.Index].Name)
	}
	if strings.Join(items, " ") != "id amount status paid meta note created_at" {
		t.Errorf("the ORDER list: %v", items)
	}
}

// The list as it shows after a K: the order in it and in the table's header,
// and the J/K hint under it (F10.3).
func TestGoldenColsMoved(t *testing.T) {
	a, _, _ := withRecorder(t)
	feed(t, a, "gcjjK")
	golden.RequireEqual(t, a.render().String())
}

// The count comes in after the page: n, ? past the timeout, ~n with no
// WHERE on a big table (§8.5).
func TestCount(t *testing.T) {
	a, tab, _ := withRecorder(t)
	right := func() string {
		return a.queryBar(a.focused(), tab).Right
	}
	if !strings.HasPrefix(right(), "auto · ●•• 行 · ") {
		t.Fatalf("counting: %q", right())
	}
	a.Update(countMsg{tab: tab, seq: tab.countSeq, n: 6000})
	if !strings.HasPrefix(right(), "auto · 6000 行") || !strings.Contains(a.render().String(), " PAGE 1/60 ") {
		t.Errorf("counted: %q", right())
	}
	a.fetch(tab, true)
	answer(a, tab)
	a.Update(countMsg{tab: tab, seq: tab.countSeq - 1, n: 1})
	a.Update(countMsg{tab: tab, seq: tab.countSeq, err: context.DeadlineExceeded})
	if !strings.HasPrefix(right(), "auto · ? 行") || !strings.Contains(a.render().String(), " PAGE 1/? ") {
		t.Errorf("timed out: %q", right())
	}
	tab.table.Rows = 2.5e6
	a.fetch(tab, true)
	answer(a, tab)
	if tab.counted != estimated || !strings.HasPrefix(right(), "auto · ~2.5m 行") || !strings.Contains(a.render().String(), " PAGE 1/~25000 ") {
		t.Errorf("estimated: %q", right())
	}
	tab.applied = "id > 1"
	a.fetch(tab, true)
	answer(a, tab)
	if tab.counted != counting {
		t.Error("a WHERE is counted for real")
	}
}

// The row under the pointer is lit, the cursor stays; a row number moves
// the cursor to its row, the column kept (G-04, G-06).
func TestRowHoverAndNumber(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 10)
	feed(t, a, "l")
	r := find(t, a, ui.Target{Kind: ui.KindRowNo, Pane: 1, Action: "grid.goto 4 1"})
	a.Update(tea.MouseMotionMsg{X: r.Min.X + 30, Y: r.Min.Y})
	if st := a.render().Buf.CellAt(r.Min.X+1, r.Min.Y).Style; st.Bg != a.theme.Row || tab.row != 0 {
		t.Errorf("hover: bg %v, row %d", st.Bg, tab.row)
	}
	click(a, r.Min)
	if tab.row != 4 || tab.col != 1 {
		t.Errorf("row number: at %d,%d", tab.row, tab.col)
	}
}

// answer is the database answering t's last request with the rows it had;
// what it returns is what the app does next (a count, say).
func answer(a *App, t *dataTab) tea.Cmd {
	_, cmd := a.Update(pageMsg{tab: t, seq: t.seq, cols: t.cols, page: t.page, next: t.next})
	return cmd
}

// Until a page is in, what is on screen stays whole: its rows, row numbers
// and PAGE; a cancel goes back to it, count and all (§7.6, §8.3).
func TestFetchKeepsWhatIsShown(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.next = true
	tab.count, tab.counted = 6000, countDone
	feed(t, a, "]")
	if f := a.render().String(); !strings.Contains(f, " PAGE 1/60 ") || !strings.Contains(f, "│   1 │") || !strings.Contains(statusRow(a), " 1,1 ") {
		t.Fatalf("while page 2 loads:\n%s", f)
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: context.Canceled})
	if tab.pageNo != 0 || tab.counted != countDone {
		t.Fatalf("cancelled: page %d counted %v", tab.pageNo, tab.counted)
	}
	feed(t, a, "]")
	if tab.pageNo != 1 {
		t.Errorf("] after the cancel asks for page 2: %d", tab.pageNo+1)
	}
	feed(t, a, "R")
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: context.Canceled})
	if tab.counted != countDone || !strings.HasPrefix(a.queryBar(a.focused(), tab).Right, "auto · 6000 行") {
		t.Errorf("a cancelled R keeps the count: %v", tab.counted)
	}
}

// With the default order, the chip's key column is the one sorted by:
// picking it turns it the other way (§7.8).
func TestOrderDefaultKeyFlips(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "go<C-n><CR>") // id
	if !slices.Equal(tab.order, orderOf("id-")) {
		t.Errorf("order %v", tab.order)
	}
}

// A WHERE's count is owed by the tab: a request that takes the WHERE's
// place before it is in still counts once its page is (§8.3).
func TestRecountOutlivesAStaleRequest(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.count, tab.counted, tab.next = 6000, countDone, true
	feed(t, a, "/id < 3<CR>")
	s1 := tab.seq
	feed(t, a, "]")
	a.Update(pageMsg{tab: tab, seq: s1, cols: tab.cols, page: tab.page})
	if tab.counted != countDone {
		t.Fatal("the stale answer starts nothing")
	}
	if cmd := answer(a, tab); cmd == nil || tab.counted != counting {
		t.Errorf("the newer page's answer counts the new WHERE: counted %v", tab.counted)
	}
	if answer(a, tab) != nil {
		t.Error("and only once")
	}
}

// A whole number typed in LIMIT's filter is the first pick, capped at
// 10000; ↵ takes it from the first page (§7.8).
func TestCustomLimit(t *testing.T) {
	a, tab, rec := withRecorder(t)
	tab.pageNo = 3
	feed(t, a, "gl250")
	if v := a.dropView(); len(v.Items) != 1 || v.Items[0] != "250" || v.Sel != 0 {
		t.Fatalf("250: %v", v.Items)
	}
	if sql := lastSQL(t, rec, a.press("<CR>")); tab.limit != 250 || tab.pageNo != 0 || !strings.HasSuffix(sql, "limit 251 offset 0") {
		t.Fatalf("↵: limit %d page %d, %s", tab.limit, tab.pageNo, sql)
	}
	answer(a, tab)
	if !strings.Contains(a.render().String(), " LIMIT 250 ") {
		t.Error("the chip")
	}
	feed(t, a, "gl")
	if v := a.dropView(); strings.Join(v.Items, " ") != "100 500 1000" || v.Mark != -1 {
		t.Errorf("a custom size is not in the list: %v mark %d", v.Items, v.Mark)
	}
	feed(t, a, "99999")
	if v := a.dropView(); v.Items[0] != "10000" {
		t.Errorf("capped: %v", v.Items)
	}
	feed(t, a, strings.Repeat("<BS>", 5)+"100")
	if v := a.dropView(); strings.Join(v.Items, " ") != "100 1000" {
		t.Errorf("a preset typed is not listed twice: %v", v.Items)
	}
	feed(t, a, strings.Repeat("<BS>", 3)+"0")
	if v := a.dropView(); len(v.Items) != 3 || v.Items[0] == "0" {
		t.Errorf("0 is no size, only a filter: %v", v.Items)
	}
}

// The tool buttons: + −, save and refresh, auto refresh and stop,
// transpose (§7.8「工具按钮」). Auto refresh picks
// an interval from its dropdown and shows it lit; stop lights while a
// request of the tab's is out and runs grid.stop. An [icon] color is the
// lit one: stop idle stays dim. With the button given way, auto refresh's
// dropdown opens at the query bar's right end.
func TestToolButtons(t *testing.T) {
	a, tab, _ := withRecorder(t)
	a.removePane(2)
	tab.out = 0 // the count loadOrders asked for: never answered here
	bs := a.toolButtons(tab)
	if len(bs) != 4 || len(bs[0].Buttons) != 2 || bs[1].Buttons[0].Action != "save" || bs[1].Buttons[1].Action != "grid.refresh" || bs[3].Buttons[0].Action != "grid.transpose" {
		t.Fatalf("groups %+v", bs)
	}
	if stop := bs[2].Buttons[1]; stop.Action != "" || stop.Fg != a.theme.Dim || !stop.Plain {
		t.Errorf("stop idle: %+v", stop)
	}
	a.fetch(tab, false)
	if stop := a.toolButtons(tab)[2].Buttons[1]; stop.Action != "grid.stop" || stop.Fg != a.theme.Error || stop.Plain {
		t.Errorf("stop with a page out: %+v", stop)
	}
	answer(a, tab)
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.refresh.auto"}).Min)
	if a.drop == nil || a.drop.kind != dropAuto {
		t.Fatal("auto refresh opens its dropdown")
	}
	if v := a.dropView(); strings.Join(v.Items, " ") != "关 2s 5s 10s 30s 60s" || v.Mark != 0 {
		t.Fatalf("items %v, mark %d", v.Items, v.Mark)
	}
	a.Update(teaKey("<Down>"))
	_, cmd := a.Update(teaKey("<CR>"))
	if auto := a.toolButtons(tab)[2].Buttons[0]; tab.auto != 2*time.Second || cmd == nil || auto.Tail != "2s" || auto.Fg != a.theme.Warn || auto.Plain {
		t.Errorf("2s: auto %v, button %+v", tab.auto, auto)
	}
	tab.note = ui.Note{Head: strings.Repeat("x", 150)} // the buttons give way to it
	a.run("grid.refresh.auto", 0)
	r := a.layout()[1]
	if box, _ := a.dropBox(a.dropView(), a.hits); box.Max.X != r.Max.X || box.Min.Y != bodyRect(r).Min.Y+2 {
		t.Errorf("from the palette, the button given way: at %v, the pane at %v", box, r)
	}
}

// The buttons are one sep strip, transpose too, a border │ between
// groups; the pointer lights one button alone (§7.8「工具按钮」).
func TestToolButtonStrip(t *testing.T) {
	a, tab, _ := withRecorder(t)
	a.removePane(2)
	q, body := a.queryBar(a.focused(), tab), bodyRect(a.layout()[1])
	add, del, save, tr := q.ButtonRect(body, "grid.row.add"), q.ButtonRect(body, "grid.row.delete"), q.ButtonRect(body, "save"), q.ButtonRect(body, "grid.transpose")
	bg := func(x int) color.Color { return a.render().Buf.CellAt(x, add.Min.Y).Style.Bg }
	if add.Max.X != del.Min.X || bg(add.Max.X-1) != a.theme.Sep || bg(del.Min.X) != a.theme.Sep || bg(tr.Min.X) != a.theme.Sep {
		t.Errorf("one strip: + %v, − %v, transpose %v", add, del, tr)
	}
	if c := a.render().Buf.CellAt(del.Max.X, add.Min.Y); save.Min.X != del.Max.X+1 || c.Content != "│" || c.Style.Fg != a.theme.Border || c.Style.Bg != a.theme.Sep {
		t.Errorf("between − %v and save %v: %q %+v", del, save, c.Content, c.Style)
	}
	a.Update(tea.MouseMotionMsg{X: del.Min.X, Y: del.Min.Y})
	if bg(del.Min.X) != a.theme.Select || bg(del.Max.X-1) != a.theme.Select || bg(add.Max.X-1) != a.theme.Sep {
		t.Error("the pointer on − lights it alone")
	}
}

// Auto refresh fetches the page and the count again, the last save's
// note kept, only for a tab on screen with no changes, no edit and nothing
// out; else it waits a turn. A new interval, or the tab closed, ends the
// ticking (§7.8「自动刷新」).
func TestAutoRefresh(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.out = 0 // the count loadOrders asked for: never answered here
	tab.auto, tab.autoGen = 2*time.Second, 1
	tab.note = ui.Note{Head: "已保存 1 行 · 3ms"}
	if cmd := a.gotAuto(autoMsg{tab, 1}); cmd == nil || tab.note.Head == "" || tab.out != 1 || !tab.recount {
		t.Fatalf("a refresh: note %+v, out %d, recount %v", tab.note, tab.out, tab.recount)
	}
	answer(a, tab)
	tab.out = 0 // and the count it asks for
	tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}}
	if cmd := a.gotAuto(autoMsg{tab, 1}); cmd == nil || tab.out != 0 {
		t.Errorf("changes: no refresh, the next turn waits: out %d", tab.out)
	}
	tab.edits = nil
	a.win().Zoom = 2
	if cmd := a.gotAuto(autoMsg{tab, 1}); cmd == nil || tab.out != 0 {
		t.Errorf("behind a zoomed pane: no refresh, the next turn waits: out %d", tab.out)
	}
	a.win().Zoom = 0
	if a.gotAuto(autoMsg{tab, 0}) != nil {
		t.Error("an old ticking goes on")
	}
	a.closeTab(a.focused(), a.focused().Cur)
	if a.gotAuto(autoMsg{tab, 1}) != nil {
		t.Error("a closed tab ticks on")
	}
}

// `[grid] order = "desc"` and `transpose = true` are what a table opened
// anew starts with: its default order, the row identity, goes down, the
// chip says so, the first column picked sorts down too; the toggle turns
// the default the other way (§14, F8.8).
func TestNewTableDefaults(t *testing.T) {
	a := sizedWith(160, 45, userConfig(t, "[grid]\norder = \"desc\"\ntranspose = true", ""))
	table, cols, page := ordersTable(5)
	rec := &recDB{}
	conn := a.sess.Conns[0]
	conn.Meta = db.NewWorker(rec)
	conn.cols[idOf(table)] = cols
	sql := lastSQL(t, rec, a.openTable(conn, table))
	tab := dataOf(a.focused())
	if !tab.transpose || len(tab.order) != 0 || !tab.desc || !strings.Contains(sql, `order by "id" desc limit`) {
		t.Fatalf("a new tab: transpose %v, order %v, desc %v, %s", tab.transpose, tab.order, tab.desc, sql)
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if !strings.Contains(a.render().String(), " ORDER id "+ui.NerdIcons.SortDesc.Text+" ") {
		t.Errorf("the chip says down")
	}
	if sql := lastSQL(t, rec, a.run("grid.order.toggle", 0)); !slices.Equal(tab.order, orderOf("id")) || !strings.Contains(sql, `order by "id" asc limit`) {
		t.Errorf("toggle: order %v, %s", tab.order, sql)
	}
	answer(a, tab)
	a.run("grid.order", 0)
	feed(t, a, "j<CR>")
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Errorf("a column picked: %v", tab.order)
	}
	a.run("grid.order", 0)
	feed(t, a, "A<Esc>") // the default order
	if len(tab.order) != 0 || !tab.desc || !strings.Contains(rec.sqls[len(rec.sqls)-1], `order by "id" desc limit`) {
		t.Errorf("the default picked: order %v desc %v, %s", tab.order, tab.desc, rec.sqls[len(rec.sqls)-1])
	}
	a.run("grid.transpose", 0)
	if tab.transpose {
		t.Error("T turns it back")
	}
}

// A tab closed and reopened is as it was, turned or not, whatever
// `[grid] transpose` says of new ones (§14, F8.8).
func TestReopenKeepsTranspose(t *testing.T) {
	for _, conf := range []string{"[grid]\ntranspose = true", ""} {
		a := sizedWith(160, 45, userConfig(t, conf, ""))
		table, cols, page := ordersTable(3)
		conn := a.sess.Conns[0]
		conn.Meta = db.NewWorker(&recDB{})
		conn.cols[idOf(table)] = cols
		a.openTable(conn, table)
		tab := dataOf(a.focused())
		a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
		was := tab.transpose
		a.run("grid.transpose", 0)
		if tab.transpose == was {
			t.Fatalf("%q: not turned", conf)
		}
		turned := tab.transpose
		a.run("tab.close", 0)
		a.run("tab.reopen", 0)
		if re := dataOf(a.focused()); re == nil || re.transpose != turned {
			t.Errorf("%q: reopened turned %v, was %v", conf, re != nil && re.transpose, turned)
		}
	}
}

// space in the ORDER list takes the column under the cursor into the sorting
// at the end, or turns it, and never out; a takes it out, A all of them; the
// list stays, and the SQL has them in that order with the row identity after
// (F12.9).
func TestOrderToggleKeys(t *testing.T) {
	a, tab, rec := withRecorder(t)
	tab.pageNo = 3
	feed(t, a, "goj") // status
	cmd := a.run("order.toggle", 0)
	if !slices.Equal(tab.order, orderOf("status")) || a.cols == nil || tab.pageNo != 0 {
		t.Fatalf("status in: %v", tab.order)
	}
	if sql := lastSQL(t, rec, cmd); !strings.Contains(sql, `order by "status" asc, "id" limit`) {
		t.Errorf("sql %s", sql)
	}
	feed(t, a, "<Space>j")
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Fatalf("status turned: %v", tab.order)
	}
	cmd = a.run("order.toggle", 0) // amount
	if !slices.Equal(tab.order, orderOf("status-", "amount")) {
		t.Fatalf("amount in: %v", tab.order)
	}
	if sql := lastSQL(t, rec, cmd); !strings.Contains(sql, `order by "status" desc, "amount" asc, "id" limit`) {
		t.Errorf("sql %s", sql)
	}
	if got := a.colsView().Icons; len(got) != 7 || got[0].Text != "  " || got[1].Text != "1↓" || got[2].Text != "2↑" {
		t.Errorf("the marks: %+v", got)
	}
	answer(a, tab)
	if got := a.render().String(); !strings.Contains(got, " ORDER status ↓, amount ↑ ") {
		t.Errorf("the chip: %s", got)
	}
	feed(t, a, "<Space><Space>") // amount: turned, turned back
	if !slices.Equal(tab.order, orderOf("status-", "amount")) {
		t.Errorf("space twice: %v", tab.order)
	}
	feed(t, a, "a") // amount out
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Errorf("amount out: %v", tab.order)
	}
	feed(t, a, "a") // not in it: nothing
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Errorf("a on a column that is not in: %v", tab.order)
	}
	feed(t, a, "kA")
	if len(tab.order) != 0 || tab.desc || a.cols == nil {
		t.Errorf("A: %v %v, the list open %v", tab.order, tab.desc, a.cols != nil)
	}
	sent := len(rec.sqls)
	feed(t, a, "A")
	if len(rec.sqls) != sent {
		t.Error("A on the default fetches")
	}
	feed(t, a, "/st<Space>") // the filter: its characters, space too
	if a.cols == nil || a.cols.filter.Text != "st " || len(tab.order) != 0 {
		t.Errorf("space in the filter: %q %v", a.cols.filter.Text, tab.order)
	}
	feed(t, a, "<Esc>") // the list has the keys again, the filter kept; esc clears it, then closes
	if a.cols == nil || a.cols.typing || a.cols.filter.Text != "st " {
		t.Fatalf("esc in the filter: %+v", a.cols)
	}
	feed(t, a, "<Esc>")
	if a.cols == nil || a.cols.filter.Text != "" {
		t.Fatalf("esc: %+v", a.cols)
	}
	feed(t, a, "<Esc>")
	if a.cols != nil {
		t.Error("esc closes")
	}
}

// ↵ on a column of several leaves it alone; ↵ on the only one turns it; the
// icon turns the first of several; `[grid] order = "desc"` is the way a column
// comes in and the way back from it (F12.1).
func TestOrderPickAndToggle(t *testing.T) {
	a, tab, rec := withRecorder(t)
	tab.order = orderOf("status-", "amount")
	feed(t, a, "go<CR>") // opens on status
	if !slices.Equal(tab.order, orderOf("status")) {
		t.Errorf("↵ on a column of several: %v", tab.order)
	}
	feed(t, a, "go<CR>")
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Errorf("↵ on the only one: %v", tab.order)
	}
	tab.order = orderOf("status-", "amount")
	if sql := lastSQL(t, rec, a.run("grid.order.toggle", 0)); !slices.Equal(tab.order, orderOf("status", "amount")) || !strings.Contains(sql, `order by "status" asc, "amount" asc, "id" limit`) {
		t.Errorf("the first turned: %v, %s", tab.order, sql)
	}
	a.newDesc = true
	tab.order = nil
	feed(t, a, "goj<Space>")
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Errorf("in the way [grid] order says: %v", tab.order)
	}
	feed(t, a, "<Space>")
	if !slices.Equal(tab.order, orderOf("status")) {
		t.Errorf("turned: %v", tab.order)
	}
	feed(t, a, "a")
	if len(tab.order) != 0 || !tab.desc {
		t.Errorf("out: %v, default desc %v", tab.order, tab.desc)
	}
}

// The ORDER chip with two columns, and the list with their marks and keys (F12.9).
func TestGoldenOrderMulti(t *testing.T) {
	a, tab, _ := withRecorder(t)
	tab.order, tab.shown.order = orderOf("status-", "amount"), orderOf("status-", "amount")
	feed(t, a, "goj")
	golden.RequireEqual(t, a.render().String())
}

// The ORDER box is as wide as its columns, not the six hints under them: they
// wrap in two rows; the COLS box is the same box, as wide as its columns less
// the mark (F15.6).
func TestOrderBoxWidthByColumns(t *testing.T) {
	a, tab, _ := withRecorder(t)
	pane := a.layout()[a.focused().ID]
	longest := 0
	for _, c := range tab.page.Cols {
		longest = max(longest, ui.Width(c.Name+"  "+tab.typeOf(c.Name)))
	}
	feed(t, a, "go")
	box, _ := a.colsBox(a.colsView())
	if want := longest + 10 + ui.Width(orderMark(tab.order, "")) + 1; box.Dx() != want {
		t.Errorf("ORDER: %d wide, want %d (pane %d)", box.Dx(), want, pane.Dx())
	}
	rows := strings.Split(a.render().String(), "\n")
	hint := 0
	for _, r := range rows {
		if strings.Contains(r, "只按这一列") || strings.Contains(r, "SPC 方向") {
			hint++
		}
	}
	if hint != 2 {
		t.Errorf("the hints are in %d rows, want 2:\n%s", hint, strings.Join(rows[:20], "\n"))
	}
	feed(t, a, "<Esc>gc")
	if cols, _ := a.colsBox(a.colsView()); cols.Dx() != longest+10 {
		t.Errorf("COLS: %d wide, want %d", cols.Dx(), longest+10)
	}
	tab.page.Cols[0].Name = strings.Repeat("c", 90) // wider than the pane: the pane's width at most
	if cols, _ := a.colsBox(a.colsView()); cols.Dx() != pane.Dx() {
		t.Errorf("a long name: %d wide, the pane %d", cols.Dx(), pane.Dx())
	}
}

// With several columns the chip's icon is the first one's and a click turns
// it; too many to leave room for LIMIT, PAGE and COLS, it shows two and … (F12.1).
func TestOrderChipMulti(t *testing.T) {
	a, tab, rec := withRecorder(t)
	tab.order, tab.shown.order = orderOf("status-", "amount"), orderOf("status-", "amount")
	if sql := lastSQL(t, rec, click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.order.toggle"}).Min)); !slices.Equal(tab.order, orderOf("status", "amount")) || !strings.Contains(sql, `order by "status" asc, "amount" asc`) {
		t.Fatalf("clicked the icon: %v, %s", tab.order, sql)
	}

	a = sized(190, 45, "nerd")
	tab = loadOrders(t, a, 100)
	tab.order, tab.shown.order = orderOf("c1-", "c2", "c3", "c4", "c5"), orderOf("c1-", "c2", "c3", "c4", "c5")
	bar := a.render().String()
	for _, want := range []string{" ORDER c1 ↓, c2 ↑, … " + ui.NerdIcons.SortDesc.Text + " ", " LIMIT ", " PAGE ", " COLS "} {
		if !strings.Contains(bar, want) {
			t.Errorf("190 columns, five sorted: no %q", want)
		}
	}
	if strings.Contains(bar, "c3") {
		t.Error("the third column shows")
	}
}

// The marks of the ORDER list line up, ten columns in the sorting or more: a
// mark is padded to the longest (F12.1).
func TestOrderMarkWidth(t *testing.T) {
	var order []db.OrderCol
	for i := range 10 {
		order = append(order, db.OrderCol{Col: "c" + strconv.Itoa(i), Desc: i%2 == 0})
	}
	w := ui.Width(orderMark(order, "c9"))
	for i := range 10 {
		if got := ui.Width(orderMark(order, "c"+strconv.Itoa(i))); got != w {
			t.Errorf("c%d: %d wide, want %d", i, got, w)
		}
	}
	if got := ui.Width(orderMark(order, "x")); got != w {
		t.Errorf("not in it: %d wide", got)
	}
}

// The ORDER the tab asks for, what it shows and the history's entries are one
// slice after a fetch: turning one column must not turn the others' (F12.1).
func TestOrderChangesDoNotShare(t *testing.T) {
	a, tab, _ := withRecorder(t)
	shared := orderOf("status")
	tab.order, tab.shown.order = shared, shared
	entry := tab.savedQuery("x")
	feed(t, a, "go<Space>") // opens on status: turned
	if !slices.Equal(tab.order, orderOf("status-")) {
		t.Fatalf("status turned: %v", tab.order)
	}
	if !slices.Equal(entry.Order, orderOf("status")) {
		t.Errorf("a history entry changed with it: %v", entry.Order)
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: errors.New("gone")})
	if !slices.Equal(tab.order, orderOf("status")) || !slices.Equal(tab.shown.order, orderOf("status")) {
		t.Errorf("a fetch that failed: asks %v, shows %v", tab.order, tab.shown.order)
	}
	feed(t, a, "<Esc>")
	if a.run("grid.order.toggle", 0); !slices.Equal(tab.order, orderOf("status-")) || !slices.Equal(entry.Order, orderOf("status")) {
		t.Errorf("the icon: %v, an entry %v", tab.order, entry.Order)
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: errors.New("gone")})
	if !slices.Equal(tab.shown.order, orderOf("status")) {
		t.Errorf("a fetch that failed: shows %v", tab.shown.order)
	}
}

// A sorting that has the row identity in it does not get it again after the
// columns (§7.8): the SQL has "id" once, with the way it was given.
func TestOrderKeyOnceInSQL(t *testing.T) {
	a, tab, rec := withRecorder(t)
	feed(t, a, "goj<Space><Space>k") // status in and turned, on id
	cmd := a.run("order.toggle", 0)
	if sql := lastSQL(t, rec, cmd); !strings.Contains(sql, `order by "status" desc, "id" asc limit`) || strings.Contains(sql, `"id" asc, "id"`) {
		t.Errorf("sql %s, order %v", sql, tab.order)
	}
}

// A click on a row of the ORDER list is ↵ on it, sorting by that column alone
// and closing the list, not a box of COLS to tick; a on a column that is not in
// the sorting does nothing, no fetch and no first page (F12.9).
func TestOrderListClickAndRemove(t *testing.T) {
	a, tab, rec := withRecorder(t)
	feed(t, a, "go")
	a.View()
	click(a, find(t, a, ui.Target{Kind: ui.KindRow, I: 1}).Min) // status
	if !slices.Equal(tab.order, orderOf("status")) || a.cols != nil || tab.hidden["status"] {
		t.Errorf("a click: order %v, list open %v, hidden %v", tab.order, a.cols != nil, tab.hidden)
	}
	answer(a, tab)
	tab.pageNo = 2
	feed(t, a, "gojj") // paid (it opens on status), which is not in it
	sent := len(rec.sqls)
	feed(t, a, "a")
	if len(rec.sqls) != sent || tab.pageNo != 2 || !slices.Equal(tab.order, orderOf("status")) {
		t.Errorf("a on a column not in the sorting: %d sent after %d, page %d, order %v", len(rec.sqls), sent, tab.pageNo, tab.order)
	}
}

// The ORDER list's keys are its own: with COLS open they do nothing, and with
// `[grid] order = "desc"` a head clicked back to the default sorts the row
// identity down, as a table opened anew does (F12.9).
func TestOrderActionsOnlyInOrderList(t *testing.T) {
	a, tab, rec := withRecorder(t)
	feed(t, a, "gc")
	sel := a.cols.sel
	a.run("order.down", 0)
	a.run("order.filter", 0)
	a.run("order.close", 0)
	if a.cols == nil || a.cols.sel != sel || a.cols.typing {
		t.Errorf("COLS open, order.* moved it: %+v", a.cols)
	}
	feed(t, a, "<Esc>")
	a.newDesc = true
	feed(t, a, "l")
	for i, want := range []string{`order by "status" desc, "id" limit`, `order by "status" asc, "id" limit`, `order by "id" desc limit`} {
		if sql := lastSQL(t, rec, a.run("grid.order.cycle", 0)); !strings.Contains(sql, want) {
			t.Fatalf("gs %d: %s, order %v desc %v", i+1, sql, tab.order, tab.desc)
		}
		answer(a, tab)
	}
	if len(tab.order) != 0 || !tab.desc {
		t.Errorf("back to the default: order %v, desc %v", tab.order, tab.desc)
	}
}
