package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"sqlmux/internal/db"
	"sqlmux/internal/ui"
)

// ordersTable is the t_order the grid tests open: n rows, a NULL, an enum,
// json, timestamptz and a note with a newline, a tab and an escape.
func ordersTable(n int) (db.Table, db.Columns, db.Result) {
	t := db.Table{Schema: "public", Name: "t_order", Rows: 6000}
	cols := db.Columns{PK: []string{"id"}, Cols: []db.Column{
		{Name: "id", Type: "bigint", NotNull: true},
		{Name: "status", Type: "order_status", Enum: []string{"pending", "running", "done", "failed"}},
		{Name: "amount", Type: "numeric(10,2)"},
		{Name: "paid", Type: "boolean"},
		{Name: "meta", Type: "jsonb"},
		{Name: "note", Type: "text"},
		{Name: "created_at", Type: "timestamp with time zone"},
	}}
	page := db.Result{}
	for _, c := range cols.Cols {
		page.Cols = append(page.Cols, db.Col{Name: c.Name})
	}
	status := []string{"pending", "running", "done", "failed"}
	for i := 1; i <= n; i++ {
		note := db.Val{Null: true}
		switch i % 3 {
		case 1:
			note = db.Val{S: fmt.Sprintf("note %d", i)}
		case 2:
			note = db.Val{S: "line one\nline two\tafter tab \x1b[31mred"}
		}
		page.Rows = append(page.Rows, []db.Val{
			{S: fmt.Sprint(i)}, {S: status[i%4]}, {S: fmt.Sprintf("%d.99", i%1000)}, {S: "t"},
			{S: fmt.Sprintf(`{"n": %d, "tags": ["a", "b"]}`, i)}, note,
			{S: fmt.Sprintf("2026-09-01 00:%02d:00+00", i%60)},
		})
	}
	return t, cols, page
}

// loadOrders opens ordersTable(n) in the focused data pane through the
// real path, answering the fetch it starts as the database would.
func loadOrders(t *testing.T, a *App, n int) *dataTab {
	t.Helper()
	table, cols, page := ordersTable(n)
	if a.openTable(table, false) == nil {
		t.Fatal("opening a table fetches it")
	}
	tab := dataOf(a.focused())
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page, next: true})
	return tab
}

func TestGoldenTable160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	golden.RequireEqual(t, a.render().String())
}

// In the default layout ① is some 70 columns: the query bar's count gives
// way first; a save's note goes before the buttons and then the chips,
// from the right, and cut in its middle past that (§7.8「查询条」).
func TestGoldenQueryBarNarrow(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 60)
	r := a.layout()[a.focused().ID]
	row := func() string { return ansi.Cut(strings.Split(a.render().String(), "\n")[r.Min.Y+2], r.Min.X, r.Max.X) }
	rows := []string{row()}
	for _, n := range []ui.Note{
		{Head: "已保存 1 行 · 3ms"},
		{Head: "id = 1：", Mid: `invalid input syntax for type numeric: "abc"`, Tail: "，已回滚", Fg: a.theme.Error},
		{Head: "id = 1：", Mid: strings.Repeat("x", 80), Tail: "，已回滚", Fg: a.theme.Error},
		{},
	} {
		tab.note = n
		rows = append(rows, row())
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

func TestGoldenTableTransposed160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	feed(t, a, "jlT")
	golden.RequireEqual(t, a.render().String())
}

// Two tabs, back on the first: the tab bar marks the current one * and the
// previous one - (T-01).
func TestGoldenTabs160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	feed(t, a, "<C-p>@t_user<C-t>gT")
	golden.RequireEqual(t, a.render().String())
}

// The pointer on a tab shows × at its mark, or after its name, the bar
// not moving: here over the previous, an unmarked and the current tab
// (F3.35).
func TestGoldenTabClose(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 3)
	feed(t, a, "<C-p>@t_user<CR><C-p>@t_sku<CR>1gt3gt") // t_order, t_user -, t_sku *
	p := a.focused()
	bar := func() string { return strings.Split(a.render().String(), "\n")[a.layout()[p.ID].Max.Y-2] }
	rows := []string{bar()}
	for i := range 3 {
		at := find(t, a, ui.Target{Kind: ui.KindTab, Pane: p.ID, I: i}).Min
		a.Update(tea.MouseMotionMsg{X: at.X, Y: at.Y})
		rows = append(rows, bar())
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

// A click on a tab's × closes it as x does, asking first when it has
// changes, the tab current before it back; the log has none (F3.35).
func TestTabCloseClick(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	feed(t, a, "lix<Esc><C-p>@t_user<CR>") // t_order changed, t_user current
	p := a.focused()
	x := func(i int) {
		at := find(t, a, ui.Target{Kind: ui.KindTab, Pane: p.ID, I: i}).Min
		a.Update(tea.MouseMotionMsg{X: at.X, Y: at.Y})
		click(a, find(t, a, ui.Target{Kind: ui.KindButton, Pane: p.ID, Action: fmt.Sprintf("tab.close.at %d %d", p.ID, i)}).Min)
	}
	if x(0); a.confirm == nil || len(p.Tabs) != 2 {
		t.Fatal("changes: it asks")
	}
	if feed(t, a, "y"); len(p.Tabs) != 1 || dataOf(p).table.Name != "t_user" || dataOf(p) == tab {
		t.Fatalf("closed: %v", tabNames(p))
	}
}

// A table open in two tabs, one of them filtered: the palette lists them
// to pick one (§7.8「打开已有的表」).
func TestGoldenTabPick160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60).shown.applied = "status = 'done'"
	feed(t, a, "<C-p>@t_order<C-t><C-p>@t_order<CR>")
	golden.RequireEqual(t, a.render().String())
}

// A fetch shows busy until it answers; an answer to an older request is
// dropped (§8.3).
func TestGridFetch(t *testing.T) {
	a := sized(160, 45, "nerd")
	table, cols, page := ordersTable(3)
	a.openTable(table, false)
	tab := dataOf(a.focused())
	if a.busy != 1 || !strings.Contains(statusRow(a), " busy · C-c 取消 ") {
		t.Fatalf("while fetching: busy %d, status %q", a.busy, statusRow(a))
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq - 1, cols: cols, page: page})
	if len(tab.page.Rows) != 0 || a.sess.cols[idOf(table)].PK == nil {
		t.Fatalf("a stale answer: rows %d; its columns are still worth caching", len(tab.page.Rows))
	}
	a.fetch(tab, false)
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if a.busy != 0 || len(tab.page.Rows) != 3 || strings.Contains(statusRow(a), "busy") {
		t.Fatalf("answered: busy %d, rows %d", a.busy, len(tab.page.Rows))
	}
	if st := statusRow(a); !strings.Contains(st, " 1,1 ") {
		t.Errorf("row,col with a table loaded: %q", st)
	}
}

// A cancel keeps the old rows and says so; so does an error, on the error
// bar under them, which the next page in takes away (§7.6, §7.8, §8.3).
func TestGridCancelAndError(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	a.fetch(tab, false)
	feed(t, a, "<C-c>")
	if a.quitToast != 0 {
		t.Fatal("C-c while a query runs cancels it, not the first of two to quit")
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: context.Canceled})
	if a.toast != "查询已取消" || len(tab.page.Rows) != 3 || tab.bar != nil {
		t.Fatalf("cancelled: toast %q rows %d bar %+v", a.toast, len(tab.page.Rows), tab.bar)
	}
	a.fetch(tab, false)
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: fmt.Errorf("permission denied for table t_order")})
	f := a.render()
	if st := styleOf(t, f, "permission denied"); st.Fg != a.theme.Error || st.Bg != a.theme.ErrorBg || !strings.Contains(f.String(), "note 1") {
		t.Errorf("error bar: %+v", st)
	}
	if !strings.Contains(statusRow(a), " 1,1 ") {
		t.Error("the table stays, and its row,col")
	}
	answer(a, tab)
	if tab.bar != nil {
		t.Error("a page in takes the fetch's error away")
	}
}

// Moves, counts and user maps; the view follows the cursor, keeping its
// column whole (§7.6).
func TestGridMoves(t *testing.T) {
	a := configured(t, 100, 20, "[map.grid.normal]\nL = \"5l\"")
	tab := loadOrders(t, a, 60)
	at := func() string { return fmt.Sprintf("%d,%d", tab.row+1, tab.col+1) }
	for _, c := range []struct{ keys, want string }{
		{"j", "2,1"}, {"3j", "5,1"}, {"l", "5,2"}, {"$", "5,7"}, {"0", "5,1"},
		{"L", "5,6"}, {"G", "60,6"}, {"gg", "1,6"}, {"k", "1,6"}, {"h", "1,5"},
	} {
		feed(t, a, c.keys)
		if at() != c.want {
			t.Errorf("%s: at %s, want %s", c.keys, at(), c.want)
		}
		if st := statusRow(a); !strings.Contains(st, " "+c.want+" ") {
			t.Errorf("%s: status %q", c.keys, st)
		}
	}
	feed(t, a, "G")
	if tab.top != 60-12 { // 20 rows less the status bar, the borders, the tab bar, the query bar, the header and its rule
		t.Errorf("G: top %d", tab.top)
	}
	feed(t, a, "$")
	body := gridRect(a.layout()[a.win().Focus], tab)
	lines := strings.Split(a.render().String(), "\n")
	if tab.left == 0 || !strings.Contains(lines[body.Min.Y], "created_at") {
		t.Errorf("$: left %d, header %q", tab.left, lines[body.Min.Y])
	}
}

// T only turns the drawing: the cursor stays on its cell, and j walks the
// fields (§7.6).
func TestGridTranspose(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 60)
	feed(t, a, "3jlT")
	if tab.row != 3 || tab.col != 1 || !tab.transpose {
		t.Fatalf("T: at %d,%d transpose %v", tab.row, tab.col, tab.transpose)
	}
	feed(t, a, "jl")
	if tab.row != 4 || tab.col != 2 {
		t.Errorf("transposed jl: at %d,%d, want record 4, field 2", tab.row, tab.col)
	}
	feed(t, a, "G$")
	if tab.row != 59 || tab.col != 6 {
		t.Errorf("transposed G$: at %d,%d, want the last record, the last field", tab.row, tab.col)
	}
	feed(t, a, "T")
	if tab.row != 59 || tab.col != 6 || tab.transpose {
		t.Errorf("back: at %d,%d", tab.row, tab.col)
	}
}

// The wheel scrolls rows by 3 up to the last one at the bottom, Shift and
// the sideways wheel by a column; the cursor follows into view (§7.6).
func TestGridWheel(t *testing.T) {
	a := sized(100, 20, "nerd")
	tab := loadOrders(t, a, 20) // 12 rows show
	r := a.layout()[1]
	wheel := func(b tea.MouseButton, mod tea.KeyMod) {
		a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: b, Mod: mod})
	}
	wheel(tea.MouseWheelDown, 0)
	if tab.top != 3 || tab.row != 3 {
		t.Fatalf("down: top %d row %d", tab.top, tab.row)
	}
	wheel(tea.MouseWheelDown, 0)
	wheel(tea.MouseWheelDown, 0)
	if tab.top != 20-12 {
		t.Errorf("the last row stops at the bottom: top %d", tab.top)
	}
	wheel(tea.MouseWheelDown, tea.ModShift)
	wheel(tea.MouseWheelRight, 0)
	if tab.left != 2 || tab.col != 2 {
		t.Errorf("sideways: left %d col %d", tab.left, tab.col)
	}
	wheel(tea.MouseWheelLeft, 0)
	if tab.left != 1 {
		t.Errorf("left: %d", tab.left)
	}
}

// A click on a cell focuses its pane and moves the cursor there (§7.4).
func TestGridClickCell(t *testing.T) {
	a := twoPanes(160, 45, "nerd")
	tab := loadOrders(t, a, 10)
	a.win().focus(2)
	r := find(t, a, ui.Target{Kind: ui.KindCell, Pane: 1, Action: "grid.goto 4 2"})
	click(a, r.Min)
	if a.win().Focus != 1 || tab.row != 4 || tab.col != 2 {
		t.Fatalf("focus %d, at %d,%d", a.win().Focus, tab.row, tab.col)
	}
}

func TestGridCells(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	f := a.render()
	th := a.theme
	for s, fg := range map[string]any{
		"<null>":      th.Dim,
		"1.99":        th.Number,
		"done ":       th.Fg, // an enum is no string (§7.6)
		`{"n": 1`:     th.JSON,
		"2026-09-01 ": th.Time,
		"note 1 ":     th.String,
	} {
		if st := styleOf(t, f, s); st.Fg != fg {
			t.Errorf("%q: fg %v, want %v", s, st.Fg, fg)
		}
	}
	if st := styleOf(t, f, "↵line two"); st.Fg != th.Dim {
		t.Errorf("↵: fg %v, want dim", st.Fg)
	}
	if strings.Contains(f.String(), "\x1b") || !strings.Contains(f.String(), "line two after tab [31") {
		t.Error("control characters go, tabs are spaces")
	}
}

// Clearing the column cache (R) leaves open tables as they are: each keeps
// the columns it was fetched with.
func TestGridKeepsItsColumns(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 3)
	before := styleOf(t, a.render(), "1.99")
	a.run("tree.refresh", 0)
	if len(a.sess.cols) != 0 {
		t.Fatal("R clears the cache")
	}
	f := a.render()
	if st := styleOf(t, f, "1.99"); st.Fg != before.Fg || st.Fg != a.theme.Number || !strings.Contains(f.String(), a.icons.Key.Text+" id") {
		t.Errorf("after R: fg %v, want %v, and the key icon", st.Fg, before.Fg)
	}
}

func TestColType(t *testing.T) {
	for typ, want := range map[string]ui.ColType{
		"bigint": ui.ColNumber, "numeric(10,2)": ui.ColNumber, "double precision": ui.ColNumber,
		"timestamp(3) with time zone": ui.ColTime, "date": ui.ColTime, "interval day to second": ui.ColTime,
		"time without time zone": ui.ColTime, "boolean": ui.ColBool, "jsonb": ui.ColJSON,
		"character varying(20)": ui.ColString, "text": ui.ColString, `"char"`: ui.ColString,
		"order_status": ui.ColOther, "uuid": ui.ColOther, "integer[]": ui.ColOther, "timeline": ui.ColOther,
	} {
		if got := colType(typ); got != want {
			t.Errorf("colType(%q) = %v, want %v", typ, got, want)
		}
	}
}

// The tool buttons at a few widths, auto refresh on and a request out:
// a group on one box, transpose on none; whole groups give way,
// transpose, auto refresh and stop, + −, then save and refresh. Last, the
// pointer on refresh lights it alone (§7.8「工具按钮」).
func TestGoldenToolButtons(t *testing.T) {
	var rows []string
	for _, w := range []int{160, 110, 90, 70, 160} {
		a := wide(w, 20)
		tab := loadOrders(t, a, 3)
		tab.auto, tab.out = 5*time.Second, 1
		tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}, {"2", "note"}: {val: db.Val{S: "y"}}}
		r := a.layout()[a.focused().ID]
		if len(rows) == 4 {
			at := a.queryBar(a.focused(), tab).ButtonRect(bodyRect(r), "grid.refresh").Min
			a.Update(tea.MouseMotionMsg{X: at.X, Y: at.Y})
		}
		rows = append(rows, ansi.Cut(strings.Split(a.render().String(), "\n")[r.Min.Y+2], r.Min.X, r.Max.X))
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

// {N}G and {N}gg go to row N by its number: on this page, or fetching its
// own, N past the rows counted the last, a count not known taken as PAGE's
// input does; transposed, field N. G and gg alone: the page's last and
// first rows (F3.36).
func TestGridLine(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 10)
	tab.limit, tab.shown.limit, tab.count, tab.counted = 10, 10, 25, countDone
	answer := func(n int) {
		_, _, page := ordersTable(n)
		a.Update(pageMsg{tab: tab, seq: tab.seq, cols: tab.cols, page: page})
	}
	if feed(t, a, "5G"); tab.row != 4 || tab.pageNo != 0 {
		t.Fatalf("5G: row %d page %d", tab.row, tab.pageNo)
	}
	if feed(t, a, "15gg"); tab.pageNo != 1 {
		t.Fatalf("15gg: page %d", tab.pageNo)
	}
	if answer(10); tab.row != 4 || tab.shown.pageNo != 1 {
		t.Fatalf("15gg: row %d on page %d", tab.row, tab.shown.pageNo+1)
	}
	feed(t, a, "99G")
	if answer(5); tab.shown.pageNo != 2 || tab.row != 4 {
		t.Fatalf("99G of 25: row %d on page %d", tab.row, tab.shown.pageNo+1)
	}
	tab.counted = countLost
	if feed(t, a, "99G"); tab.pageNo != 9 {
		t.Errorf("99G, not counted: page %d", tab.pageNo+1)
	}
	answer(0)
	feed(t, a, "15G")
	answer(10)
	if feed(t, a, "G"); tab.row != 9 {
		t.Errorf("G: row %d", tab.row)
	}
	if feed(t, a, "gg"); tab.row != 0 {
		t.Errorf("gg: row %d", tab.row)
	}
	if feed(t, a, "T3G"); tab.col != 2 || tab.pageNo != 1 {
		t.Errorf("transposed 3G: field %d, page %d", tab.col, tab.pageNo)
	}
}
