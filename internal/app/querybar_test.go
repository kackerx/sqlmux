package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	if a.drop == nil || a.drop.kind != dropOrder || a.dropView().Items[a.drop.sel] != orderDefault {
		t.Fatalf("go: %+v", a.drop)
	}
	chip := a.chipRects(a.focused(), tab)[0]
	if box, _ := a.dropBox(8); box.Min.X != chip.Min.X || box.Min.Y != chip.Max.Y {
		t.Errorf("the dropdown opens under its chip: %v, chip %v", box, chip)
	}
	feed(t, a, "stat<CR>")
	if tab.order != "status" || tab.desc || tab.pageNo != 0 || a.drop != nil {
		t.Fatalf("status: order %q desc %v page %d", tab.order, tab.desc, tab.pageNo)
	}
	feed(t, a, "gostat<CR>")
	if !tab.desc || !strings.Contains(a.render().String(), " ORDER status ↓ ") {
		t.Errorf("status again: desc %v", tab.desc)
	}
	feed(t, a, "go默认<CR>")
	if tab.order != "" || !strings.Contains(a.render().String(), " ORDER id ↑ ") {
		t.Errorf("default: order %q", tab.order)
	}
	feed(t, a, "gl<C-n><CR>")
	if tab.limit != 500 || !strings.Contains(a.render().String(), " LIMIT 500 ") {
		t.Errorf("gl: limit %d", tab.limit)
	}
	click(a, a.chipRects(a.focused(), tab)[1].Min)
	if a.drop == nil || a.drop.kind != dropLimit {
		t.Error("clicking the LIMIT chip opens its dropdown")
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
	if a.mode() != keymap.Insert || len(a.colsMatches()) != 3 { // status, amount, created_at
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
	a.Update(countMsg{tab: tab, seq: tab.countSeq - 1, n: 1})
	a.Update(countMsg{tab: tab, seq: tab.countSeq, err: context.DeadlineExceeded})
	if !strings.HasPrefix(right(), "auto · ? 行") || !strings.Contains(a.render().String(), " PAGE 1/? ") {
		t.Errorf("timed out: %q", right())
	}
	tab.table.Rows = 2.5e6
	a.fetch(tab, true)
	if tab.counted != estimated || !strings.HasPrefix(right(), "auto · ~2.5m 行") || !strings.Contains(a.render().String(), " PAGE 1/~25000 ") {
		t.Errorf("estimated: %q", right())
	}
	tab.applied = "id > 1"
	a.fetch(tab, true)
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
