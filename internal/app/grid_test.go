package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/ui"
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
			{S: strconv.Itoa(i)}, {S: status[i%4]}, {S: fmt.Sprintf("%d.99", i%1000)}, {S: "t"},
			{S: fmt.Sprintf(`{"n": %d, "tags": ["a", "b"]}`, i)}, note,
			{S: fmt.Sprintf("2026-09-01 00:%02d:00+00", i%60)},
		})
	}
	return t, cols, page
}

// loadOrders opens ordersTable(n) in the focused data pane through the
// real path, answering the fetch it starts as the database would.
func loadOrders(t testing.TB, a *App, n int) *dataTab { return loadOrdersNext(t, a, n, true) }

// loadOrdersNext is loadOrders with next, whether a page is left after it.
func loadOrdersNext(t testing.TB, a *App, n int, next bool) *dataTab {
	t.Helper()
	table, cols, page := ordersTable(n)
	if a.openTable(a.sess.Conns[0], table) == nil {
		t.Fatal("opening a table fetches it")
	}
	tab := dataOf(a.focused())
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page, next: next})
	return tab
}

func TestGoldenTable160x45(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 60)
	golden.RequireEqual(t, a.render().String())
}

// In the default layout ① is 61 columns: the query bar's count gives way
// first; a save's note goes before the buttons and then the chips, from the
// right, and cut in its middle past that; with 6000 rows (PAGE 1/60) and
// changes to save, transpose, auto refresh and + − give way and the save
// group stays after COLS, PAGE and LIMIT (§7.8「查询条」, F8.9).
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
	tab.note = ui.Note{}
	tab.count, tab.counted = 6000, countDone
	tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}, {"2", "note"}: {val: db.Val{S: "y"}}, {"3", "note"}: {val: db.Val{S: "z"}}}
	rows = append(rows, row())
	tab.note = ui.Note{Head: "已保存 1 行 · 3ms"}
	rows = append(rows, row())
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

// In the default layout ① is too narrow for "auto · 3 行 · 12ms": a table
// under a page says "3 行" in what is left, a longer one has the count go
// first as it did; transposed it is the same bar (§7.8「查询条」, F19.4).
func TestGoldenQueryBarUnderAPage(t *testing.T) {
	var out []string
	for _, turn := range []bool{false, true} {
		for _, rows := range []int{3, 1203} {
			a := sized(160, 45, "nerd")
			tab := loadOrdersNext(t, a, min(rows, 60), rows != 3)
			if rows != 3 {
				tab.count, tab.counted = int64(rows), countDone
			}
			if turn {
				feed(t, a, "T")
			}
			r := a.layout()[a.focused().ID]
			lines := strings.Split(a.render().String(), "\n")
			out = append(out, fmt.Sprintf("%d rows, transposed %v", rows, turn), ansi.Cut(lines[r.Min.Y+1], r.Min.X, r.Max.X), ansi.Cut(lines[r.Min.Y+2], r.Min.X, r.Max.X))
		}
	}
	golden.RequireEqual(t, strings.Join(out, "\n")+"\n")
}

// Transposed, every value is left-aligned, numbers too: a column mixes
// types (§7.6, F5.7).
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
// not moving: here over the previous, an unmarked and the current tab;
// on the × it is lit, and the blank after it (F3.35, F5.6).
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
	for _, i := range []int{1, 2} { // the pointer on the ×: lit with the blank after it, an unmarked tab's in the next │'s cell (F5.6)
		at := find(t, a, ui.Target{Kind: ui.KindTab, Pane: p.ID, I: i}).Min
		a.Update(tea.MouseMotionMsg{X: at.X, Y: at.Y})
		x := find(t, a, ui.Target{Kind: ui.KindButton, Pane: p.ID, Action: fmt.Sprintf("tab.close.at %d %d", p.ID, i)})
		a.Update(tea.MouseMotionMsg{X: x.Min.X, Y: x.Min.Y})
		f := a.render()
		if x.Dx() != 2 || f.Buf.CellAt(x.Max.X-1, x.Min.Y).Style.Bg != a.theme.Select {
			t.Errorf("tab %d: the × and its blank, %v, not lit", i+1, x)
		}
		rows = append(rows, bar(), bgs(a, f, a.layout()[p.ID].Max.Y-2, 0, a.w))
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

// A click on a tab's × closes it as x does, asking first when it has
// changes, the current and previous tabs staying so; the log has none
// (F3.35). Tabs past the bar's end take no clicks: the pane's border is
// no tab's.
func TestTabCloseClick(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	feed(t, a, "lix<Esc><C-p>@t_user<CR><C-p>@t_sku<CR>1gt3gt") // t_order changed and -, t_user, t_sku current
	p := a.focused()
	x := func(i int) {
		at := find(t, a, ui.Target{Kind: ui.KindTab, Pane: p.ID, I: i}).Min
		a.Update(tea.MouseMotionMsg{X: at.X, Y: at.Y})
		click(a, find(t, a, ui.Target{Kind: ui.KindButton, Pane: p.ID, Action: fmt.Sprintf("tab.close.at %d %d", p.ID, i)}).Min)
	}
	if x(1); tabNames(p) != "t_order t_sku" || p.Cur != 1 || p.Prev != 0 {
		t.Fatalf("t_user closed: %v, cur %d prev %d", tabNames(p), p.Cur, p.Prev)
	}
	if x(0); a.confirm == nil || len(p.Tabs) != 2 {
		t.Fatal("changes: it asks")
	}
	if feed(t, a, "n"); len(p.Tabs) != 2 || p.Cur != 1 {
		t.Fatalf("n: %v, cur %d", tabNames(p), p.Cur)
	}
	x(0)
	if feed(t, a, "y"); len(p.Tabs) != 1 || dataOf(p).table.Name != "t_sku" || dataOf(p) == tab {
		t.Fatalf("closed: %v", tabNames(p))
	}
	for _, n := range []string{"t_user", "t_payment", "t_refund", "t_user_address", "t_user_profile", "mt_task_log", "agent_version"} {
		feed(t, a, "<C-p>@"+n+"<CR>")
	}
	r := a.layout()[p.ID]
	border := uv.Pos(r.Max.X-1, r.Max.Y-2)
	a.Update(tea.MouseMotionMsg{X: border.X, Y: border.Y})
	a.View()
	if hit, _ := ui.HitAt(a.hits, border); hit.Kind == ui.KindTab || hit.Kind == ui.KindButton {
		t.Errorf("the border takes a tab not shown: %+v", hit)
	}
}

// A fetch shows busy until it answers; an answer to an older request is
// dropped (§8.3).
func TestGridFetch(t *testing.T) {
	a := sized(160, 45, "nerd")
	table, cols, page := ordersTable(3)
	a.openTable(a.sess.Conns[0], table)
	tab := dataOf(a.focused())
	if a.sess.Conns[0].busy != 1 || !strings.Contains(statusRow(a), " busy ●•• · C-c 取消 ") {
		t.Fatalf("while fetching: busy %d, status %q", a.sess.Conns[0].busy, statusRow(a))
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq - 1, cols: cols, page: page})
	if len(tab.page.Rows) != 0 || a.sess.Conns[0].cols[idOf(table)].PK == nil {
		t.Fatalf("a stale answer: rows %d; its columns are still worth caching", len(tab.page.Rows))
	}
	a.fetch(tab, false)
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if a.sess.Conns[0].busy != 0 || len(tab.page.Rows) != 3 || strings.Contains(statusRow(a), "busy") {
		t.Fatalf("answered: busy %d, rows %d", a.sess.Conns[0].busy, len(tab.page.Rows))
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
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: errors.New("permission denied for table t_order")})
	f := a.render()
	if st := styleOf(t, f, "permission denied"); st.Fg != a.theme.Error || st.Bg != a.theme.ErrorBg || !strings.Contains(f.String(), "running") {
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

// C-r on the grid goes into the WHERE and opens its history; esc closes the
// list, esc again leaves the input for the grid (F9.4).
func TestGridCtrlRHistory(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 10)
	if tab.typing != "" {
		t.Fatalf("typing %q", tab.typing)
	}
	feed(t, a, "<C-r>")
	if tab.typing != "where" || tab.hist == nil || tab.ed.Mode() != editor.Insert {
		t.Fatalf("C-r: typing %q, list %v, mode %v", tab.typing, tab.hist != nil, tab.ed.Mode())
	}
	feed(t, a, "<Esc>")
	if tab.typing != "where" || tab.hist != nil {
		t.Fatalf("esc: typing %q, list %v", tab.typing, tab.hist != nil)
	}
	feed(t, a, "<Esc>")
	feed(t, a, "<Esc>")
	if tab.typing != "" {
		t.Errorf("esc again: typing %q", tab.typing)
	}
}

// ^ is 0 (F9.3): to the first column, in the turned view too, and a user's
// _ = "^" is it as well.
func TestGridCaretIsZero(t *testing.T) {
	a := configured(t, 100, 20, "[map.grid.normal]\n_ = \"^\"")
	tab := loadOrders(t, a, 60)
	for _, keys := range []string{"^", "_"} {
		feed(t, a, "$")
		if tab.col == 0 {
			t.Fatal("$ did not move")
		}
		if feed(t, a, keys); tab.col != 0 {
			t.Errorf("%s: column %d", keys, tab.col)
		}
	}
	feed(t, a, "T$")
	feed(t, a, "0")
	row, col := tab.row, tab.col
	feed(t, a, "$")
	if feed(t, a, "^"); tab.row != row || tab.col != col {
		t.Errorf("turned: ^ at %d,%d, 0 at %d,%d", tab.row, tab.col, row, col)
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
	a := twoPanes(160, 45)
	tab := loadOrders(t, a, 10)
	a.win().focus(2)
	r := find(t, a, ui.Target{Kind: ui.KindCell, Pane: 1, Action: "grid.goto 4 2"})
	click(a, r.Min)
	if a.win().Focus != 1 || tab.row != 4 || tab.col != 2 {
		t.Fatalf("focus %d, at %d,%d", a.win().Focus, tab.row, tab.col)
	}
}

func TestGridCells(t *testing.T) {
	a := wide(200, 45) // every column whole
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
	if len(a.sess.Conns[0].cols) != 0 {
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
		// MySQL's
		"bigint unsigned": ui.ColNumber, "unsigned bigint": ui.ColNumber, "unsigned int": ui.ColNumber, "int unsigned zerofill": ui.ColNumber, "decimal(10,2)": ui.ColNumber, "tinyint(1)": ui.ColBool, "tinyint(4)": ui.ColNumber,
		"datetime(3)": ui.ColTime, "varchar(64)": ui.ColString, "enum('a','b')": ui.ColString, "blob": ui.ColOther,
	} {
		if got := colType(typ); got != want {
			t.Errorf("colType(%q) = %v, want %v", typ, got, want)
		}
	}
}

// The tool buttons at a few widths, auto refresh on and a request out,
// each row over its backgrounds: one sep strip, a │ between groups; whole
// groups give way, transpose, auto refresh and stop, + −, then save and
// refresh; the pointer on refresh lights it alone (§7.8「工具按钮」).
func TestGoldenToolButtons(t *testing.T) {
	var rows []string
	for _, c := range []struct {
		w     int
		hover string // the button the pointer is on
	}{{160, ""}, {110, ""}, {90, ""}, {70, ""}, {160, "grid.refresh"}} {
		a := wide(c.w, 20)
		tab := loadOrders(t, a, 3)
		tab.auto, tab.out = 5*time.Second, 1
		tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}, {"2", "note"}: {val: db.Val{S: "y"}}}
		r := a.layout()[a.focused().ID]
		if c.hover != "" {
			at := find(t, a, ui.Target{Kind: ui.KindHint, Pane: a.focused().ID, Action: c.hover}).Min
			a.Update(tea.MouseMotionMsg{X: at.X, Y: at.Y})
		}
		f := a.render()
		rows = append(rows, ansi.Cut(strings.Split(f.String(), "\n")[r.Min.Y+2], r.Min.X, r.Max.X), bgs(a, f, r.Min.Y+2, r.Min.X, r.Max.X))
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

// {N}G and {N}gg go to row N by its number: on this page, or fetching its
// own, also with another page on its way, N past the rows counted the last, a count not known taken as PAGE's
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
	if feed(t, a, "]3G"); tab.pageNo != 0 || tab.wantRec != 3 {
		t.Fatalf("3G while ] is on its way: page %d, row %d", tab.pageNo, tab.wantRec)
	}
	if answer(10); tab.row != 2 || tab.shown.pageNo != 0 {
		t.Fatalf("3G after ]: row %d page %d", tab.row, tab.shown.pageNo)
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

// Tab and S-Tab move a cell right and left in NORMAL, on any row (F8.6);
// an edit's own keep their meaning (TestNewRowTab).
func TestGridTab(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "<Tab><Tab>")
	if tab.row != 0 || tab.col != 2 {
		t.Errorf("Tab Tab: %d,%d", tab.row, tab.col)
	}
	feed(t, a, "<S-Tab>")
	if tab.col != 1 {
		t.Errorf("S-Tab: col %d", tab.col)
	}
	feed(t, a, "o<Esc><S-Tab><Tab><Tab>") // the added row, not editing
	if tab.cell != nil || tab.col != 2 {
		t.Errorf("on an added row: cell %+v, col %d", tab.cell, tab.col)
	}
}

// `[grid] full_width` is every grid's: a table's, a result's, and quick SQL's
// (§7.6, F8.5).
func TestFullWidthReachesEveryGrid(t *testing.T) {
	a := sizedWith(160, 45, userConfig(t, "[grid]\nfull_width = 8", ""))
	tab := loadOrders(t, a, 3)
	if g := a.grid(a.focused(), tab); g.FullWidth != 8 {
		t.Errorf("a table's: %d", g.FullWidth)
	}
	r := db.Result{Cols: []db.Col{{Name: "a"}}, Rows: [][]db.Val{{{S: "x"}}}}
	b, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": r}}, "select 1;")
	press(t, b, "<CR>")
	b.fullWidth = 8
	res := b.win().Result
	if g := b.resultGrid(res, res.Tabs[res.Cur].Result); g.FullWidth != 8 {
		t.Errorf("a result's: %d", g.FullWidth)
	}
	if v := b.quickView(&quickSQL{res: r}); v.Grid.FullWidth != 8 {
		t.Errorf("quick SQL's: %d", v.Grid.FullWidth)
	}
}

// The query bar's second row gives way in the order of §7.8: transpose, auto
// refresh and stop, + −, COLS, PAGE, LIMIT, save and refresh, ORDER (F8.9).
func TestQueryBarGivesWayInOrder(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	q := a.queryBar(a.focused(), tab)
	stay := map[string]int{}
	for _, c := range q.Chips {
		stay[c.Label] = c.Stay
	}
	for _, g := range q.Buttons {
		stay[g.Buttons[0].Action] = g.Stay
	}
	names := []string{"grid.transpose", "grid.refresh.auto", "grid.row.add", "COLS", "PAGE", "LIMIT", "save", "ORDER"}
	if len(stay) != len(names) {
		t.Errorf("stays %v, for %v", stay, names)
	}
	for i := 1; i < len(names); i++ {
		if stay[names[i-1]] >= stay[names[i]] {
			t.Errorf("%s (%d) is not before %s (%d)", names[i-1], stay[names[i-1]], names[i], stay[names[i]])
		}
	}
}

// The columns follow the data through the memory of the widest values: a
// longer value in a new page, a hidden column and the turned table each show
// as they are measured (F11.4).
func TestGridWidthsFollowTheData(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	a.View()
	const long = "a note longer than the others"
	_, cols, page := ordersTable(3) // a new page, as a refresh brings it
	page.Rows[1][5] = db.Val{S: long}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if !strings.Contains(a.render().String(), long) {
		t.Error("the new page's longest value is cut")
	}
	tab.hidden["status"] = true
	if !strings.Contains(a.render().String(), long) || strings.Contains(a.render().String(), "pending") {
		t.Error("a column hidden")
	}
	tab.transpose = true
	if !strings.Contains(a.render().String(), long[:20]) {
		t.Error("the turned table")
	}
}

// The same page is measured once however many keys are pressed and frames
// drawn: a table's, whose grid is made again each frame, and a result's
// (F11.4).
func TestGridMeasuresAPageOnce(t *testing.T) {
	a, tab := scrollTable(t)
	if !strings.Contains(a.render().String(), "任务") {
		t.Fatal("the table has no heavy rows")
	}
	r := scrollResult(t)
	for name, c := range map[string]struct {
		a *App
		w *ui.Widest
	}{"table": {a, tab.widest}, "result": {r, resultOf(r.focused()).widest}} {
		if c.w == nil || c.w.Passes != 1 {
			t.Fatalf("%s: measured %+v", name, c.w)
		}
		feed(t, c.a, "jjjkj")
		c.a.View()
		if c.w.Passes != 1 {
			t.Errorf("%s: measured %d times", name, c.w.Passes)
		}
	}
}

// A column hidden, a page with no rows (a WHERE nothing matches), the column
// shown again: the page of no values is of three columns now (F11.4).
func TestGridColumnShownOverAnEmptyPage(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.hidden["status"] = true
	a.render()
	_, cols, page := ordersTable(0)
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	a.render()
	delete(tab.hidden, "status")
	a.render()
}
