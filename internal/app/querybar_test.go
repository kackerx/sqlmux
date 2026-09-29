package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
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
func lastSQL(t *testing.T, a *App, rec *recDB, cmd tea.Cmd) string {
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
func withRecorder(t *testing.T, w, h int) (*App, *dataTab, *recDB) {
	a := sized(w, h, "nerd")
	tab := loadOrders(t, a, 100)
	rec := &recDB{}
	a.sess.Meta = db.NewWorker(rec)
	return a, tab, rec
}

// / types a WHERE (INSERT, -- editing WHERE --); ↵ runs it from the first
// page and counts again; esc puts back what is in effect (§7.8).
func TestWhere(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
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
	if tab.where.Text != "id > 5" || tab.applied != "id > 5" || a.mode() != keymap.Normal {
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
	a, tab, rec := withRecorder(t, 160, 45)
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
	a, tab, rec := withRecorder(t, 160, 45)
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

// The page query carries the WHERE, the ORDER with the row identity after
// it, the LIMIT and the page (§8.5, §9.6).
func TestPageSQL(t *testing.T) {
	a, tab, rec := withRecorder(t, 160, 45)
	tab.applied, tab.order, tab.desc, tab.limit, tab.pageNo = "status = 'done' -- 备注", "status", true, 500, 2
	sql := lastSQL(t, a, rec, a.fetch(tab, false))
	want := `select * from "public"."t_order" where (` + "\nstatus = 'done' -- 备注\n" + `) order by "status" desc, "id" limit 501 offset 1000`
	if sql != want {
		t.Errorf("sql\n%s\nwant\n%s", sql, want)
	}
}

// ] and [ turn pages, not past either end; row numbers and row,col count
// from the page's start (§7.8).
func TestPaging(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
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

// ORDER and LIMIT pick from a dropdown under their chip (§7.8).
func TestOrderAndLimit(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	tab.pageNo = 4
	feed(t, a, "go")
	if a.drop == nil || a.drop.kind != dropOrder || a.dropView().Items[a.drop.sel] != "默认" {
		t.Fatalf("go: %+v", a.drop)
	}
	chip := a.chipRect(a.focused(), tab, "grid.order")
	if box, _ := a.dropBox(a.dropView(), a.hits); chip.Empty() || box.Min.X != chip.Min.X || box.Min.Y != chip.Max.Y {
		t.Errorf("the dropdown opens under its chip: %v, chip %v", box, chip)
	}
	feed(t, a, "stat<CR>")
	answer(a, tab)
	if tab.order != "status" || tab.desc || tab.pageNo != 0 || a.drop != nil {
		t.Fatalf("status: order %q desc %v page %d", tab.order, tab.desc, tab.pageNo)
	}
	feed(t, a, "gostat<CR>")
	answer(a, tab)
	if !tab.desc || !strings.Contains(a.render().String(), " ORDER status "+ui.NerdIcons.SortDesc.Text+" ") {
		t.Errorf("status again: desc %v", tab.desc)
	}
	feed(t, a, "go默认<CR>")
	answer(a, tab)
	if tab.order != "" || !strings.Contains(a.render().String(), " ORDER id "+ui.NerdIcons.SortAsc.Text+" ") {
		t.Errorf("default: order %q", tab.order)
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

// A column named 默认 sorts by itself, not by the row identity: the first
// item is the default by its place, not its text.
func TestOrderByAColumnNamedDefault(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	tab.page.Cols[1].Name = "默认"
	feed(t, a, "go<C-n><C-n><CR>") // past the default and id
	if tab.order != "默认" {
		t.Fatalf("order %q", tab.order)
	}
	feed(t, a, "go")
	if a.drop.sel != 2 {
		t.Errorf("the current one is that column: sel %d", a.drop.sel)
	}
}

// The buttons' icons are info, ORDER's direction warn, unless a theme
// gives them a color (§7.7); a button is " <icon> ", lit whole under the
// pointer (§7.8).
func TestQueryBarIconColors(t *testing.T) {
	a, _, _ := withRecorder(t, 160, 45)
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
	a, tab, rec := withRecorder(t, 160, 45)
	icon := func() uv.Position {
		return find(t, a, ui.Target{Kind: ui.KindHint, Pane: 1, Action: "grid.order.toggle"}).Min
	}
	tab.pageNo = 2
	if sql := lastSQL(t, a, rec, click(a, icon())); tab.order != "id" || !tab.desc || tab.pageNo != 0 || !strings.Contains(sql, `order by "id" desc limit`) {
		t.Fatalf("default: order %q desc %v page %d, %s", tab.order, tab.desc, tab.pageNo, sql)
	}
	answer(a, tab)
	if sql := lastSQL(t, a, rec, click(a, icon())); tab.desc || !strings.Contains(sql, `order by "id" asc limit`) {
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
	if a.drop == nil || a.drop.kind != dropOrder {
		t.Error("the chip's label opens the dropdown")
	}
	a.drop = nil
	tab.cols = db.Columns{PK: []string{"occurred_at", "id"}, Cols: tab.cols.Cols}
	tab.order, tab.desc = "", false
	if sql := lastSQL(t, a, rec, a.run("grid.order.toggle", 0)); tab.order != "occurred_at" || !tab.desc || !strings.Contains(sql, `order by "occurred_at" desc, "id" limit`) {
		t.Errorf("a composite key: %q %v, %s", tab.order, tab.desc, sql)
	}
	tab.cols = db.Columns{PK: []string{"note", "id"}, Cols: tab.cols.Cols}
	tab.order, tab.desc = "", false
	feed(t, a, "gonote<CR>") // the dropdown: the key's first column is the chip's, so it turns (§7.8)
	if tab.order != "note" || !tab.desc {
		t.Errorf("dropdown on a composite key's first column: %q desc %v", tab.order, tab.desc)
	}
	tab.cols = db.Columns{Cols: tab.cols.Cols}
	tab.order = ""
	if a.run("grid.order.toggle", 0) != nil || strings.Contains(a.render().String(), " ORDER — "+ui.NerdIcons.SortAsc.Text) {
		t.Error("no row identity: nothing to turn, no icon")
	}
}

// COLS (Q-04): the list has the keys, / the filter; hidden columns leave
// the grid and the cursor moves off them; esc clears, then closes.
func TestCols(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
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

// The count comes in after the page: n, ? past the timeout, ~n with no
// WHERE on a big table (§8.5).
func TestCount(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	right := func() string {
		return a.queryBar(a.focused(), tab).Right
	}
	if !strings.HasPrefix(right(), "auto · … 行 · ") {
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
	a, tab, _ := withRecorder(t, 160, 45)
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
	a, tab, _ := withRecorder(t, 160, 45)
	feed(t, a, "go<C-n><CR>") // id
	if tab.order != "id" || !tab.desc {
		t.Errorf("order %q desc %v", tab.order, tab.desc)
	}
}

// A WHERE's count is owed by the tab: a request that takes the WHERE's
// place before it is in still counts once its page is (§8.3).
func TestRecountOutlivesAStaleRequest(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
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
	a, tab, rec := withRecorder(t, 160, 45)
	tab.pageNo = 3
	feed(t, a, "gl250")
	if v := a.dropView(); len(v.Items) != 1 || v.Items[0] != "250" || v.Sel != 0 {
		t.Fatalf("250: %v", v.Items)
	}
	if sql := lastSQL(t, a, rec, a.press("<CR>")); tab.limit != 250 || tab.pageNo != 0 || !strings.HasSuffix(sql, "limit 251 offset 0") {
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

// The tool buttons: data, query, view (§7.8「工具按钮」). Auto refresh picks
// an interval from its dropdown and shows it lit; stop lights while a
// request of the tab's is out and runs grid.stop. An [icon] color is the
// lit one: stop idle stays dim.
func TestToolButtons(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
	a.removePane(2)
	tab.out = 0 // the count loadOrders asked for: never answered here
	bs := a.toolButtons(tab)
	if len(bs) != 3 || len(bs[0]) != 3 || bs[0][2].Action != "save" || bs[1][0].Action != "grid.refresh" || bs[2][0].Action != "grid.transpose" {
		t.Fatalf("groups %+v", bs)
	}
	if stop := bs[1][2]; stop.Action != "" || stop.Fg != a.theme.Dim || !stop.Plain {
		t.Errorf("stop idle: %+v", stop)
	}
	a.fetch(tab, false)
	if stop := a.toolButtons(tab)[1][2]; stop.Action != "grid.stop" || stop.Fg != a.theme.Error || stop.Plain {
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
	_, cmd := a.Update(teaKey("<Down>"))
	_, cmd = a.Update(teaKey("<CR>"))
	if auto := a.toolButtons(tab)[1][1]; tab.auto != 2*time.Second || cmd == nil || auto.Tail != "2s" || auto.Fg != a.theme.Warn || auto.Plain {
		t.Errorf("2s: auto %v, button %+v", tab.auto, auto)
	}
}

// Auto refresh fetches the page and the count again, the last save's
// note kept, only for a tab showing with no changes, no edit and nothing
// out; else it waits a turn. A new interval, or the tab closed, ends the
// ticking (§7.8「自动刷新」).
func TestAutoRefresh(t *testing.T) {
	a, tab, _ := withRecorder(t, 160, 45)
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
	if a.gotAuto(autoMsg{tab, 0}) != nil {
		t.Error("an old ticking goes on")
	}
	a.closeTab(a.focused())
	if a.gotAuto(autoMsg{tab, 1}) != nil {
		t.Error("a closed tab ticks on")
	}
}
