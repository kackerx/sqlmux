package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"sqlmux/internal/db"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// quickResult is n rows of id and note, the note NULL on even ids.
func quickResult(n int) db.Result {
	r := db.Result{Cols: []db.Col{{Name: "id", Type: "int4"}, {Name: "note", Type: "text"}}, Took: 12 * time.Millisecond}
	for i := 1; i <= n; i++ {
		note := db.Val{S: fmt.Sprintf("note %d", i)}
		if i%2 == 0 {
			note = db.Val{Null: true}
		}
		r.Rows = append(r.Rows, []db.Val{{S: fmt.Sprint(i)}, note})
	}
	return r
}

// answerQuick hands the palette the quick SQL's answer.
func answerQuick(a *App, r db.Result, err error) tea.Cmd {
	_, cmd := a.Update(quickMsg{a.palette, r, err})
	return cmd
}

func enterOf(a *App) string {
	var hs []string
	for _, h := range a.paletteView().Enter {
		hs = append(hs, h.Key+" "+h.Label)
	}
	return strings.Join(hs, " · ")
}

// ↵ runs what follows the ; on Meta, once at a time; the result area shows
// what came back, a row count past 100 as 100+, a statement with no rows
// by what it did, an error in place of the table (§12「快速 SQL」).
func TestQuickSQL(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>;select 1")
	if a.paletteView().Result != nil || enterOf(a) != "↵ 执行" || len(rowsOf(a)) != 0 {
		t.Fatalf("before running: enter %q, rows %v", enterOf(a), rowsOf(a))
	}
	feed(t, a, "<CR>")
	q := a.palette.quick
	if q.running != "select 1" || a.busy != 1 || !slices.Equal(a.state.SQL["doraemon"], []string{"select 1"}) {
		t.Fatalf("running %q, busy %d, history %v", q.running, a.busy, a.state.SQL)
	}
	if r := a.paletteView().Result; r == nil || r.Title != "… 行 · 只读" {
		t.Fatalf("running: %+v", r)
	}
	if feed(t, a, "<CR>"); a.busy != 1 {
		t.Error("↵ while one runs ran another")
	}
	answerQuick(a, quickResult(3), nil)
	if r := a.paletteView().Result; a.busy != 0 || q.ran != "select 1" || r.Title != "3 行 · 12ms · 只读" || len(r.Grid.Rows) != 3 || r.Grid.Row != -1 {
		t.Fatalf("answered: busy %d, %+v", a.busy, r)
	}
	if feed(t, a, " "); enterOf(a) != "已修改，↵ 重新执行" {
		t.Errorf("modified: %q", enterOf(a))
	}
	for _, c := range []struct {
		res   db.Result
		err   error
		title string
	}{
		{db.Result{Cols: quickResult(0).Cols, Rows: quickResult(100).Rows, Truncated: true, Took: time.Millisecond}, nil, "100+ 行 · 1ms · 只读"},
		{db.Result{Tag: "SET", Took: time.Millisecond}, nil, "SET · 1ms · 只读"},
		{db.Result{}, errors.New(`ERROR: syntax error at or near "selec"`), "只读"},
	} {
		feed(t, a, "<CR>")
		answerQuick(a, c.res, c.err)
		if r := a.paletteView().Result; r.Title != c.title || c.err != nil && r.Err != c.err.Error() {
			t.Errorf("%s: title %q err %q", c.title, r.Title, r.Err)
		}
	}
	if feed(t, a, "<Esc>"); a.palette != nil {
		t.Error("esc keeps the palette")
	}
}

// C-c cancels a quick SQL running and keeps the palette and the last
// result; idle, it closes the palette as esc does (§12, §8.3).
func TestQuickSQLCancel(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>;select 1<CR>")
	answerQuick(a, quickResult(3), nil)
	feed(t, a, "<CR><C-c>")
	if a.palette == nil {
		t.Fatal("C-c while running closed the palette")
	}
	answerQuick(a, db.Result{}, context.Canceled)
	if r := a.paletteView().Result; a.toast != "查询已取消" || r.Title != "3 行 · 12ms · 只读" || a.palette.quick.running != "" {
		t.Errorf("cancelled: toast %q, %+v", a.toast, r)
	}
	if st := styleOf(t, a.render(), "查询已取消"); st.Fg != a.theme.Warn || st.Bg != a.theme.Bar {
		t.Errorf("the toast is dimmed with the rest: %+v", st) // over the mask (§7.5)
	}
	feed(t, a, "<CR>")
	p := a.palette
	if feed(t, a, "<Esc>"); a.palette != nil {
		t.Fatal("esc while running keeps the palette")
	}
	a.Update(quickMsg{p, quickResult(1), nil})
	if a.busy != 0 || a.palette != nil {
		t.Errorf("an answer to a closed palette: busy %d", a.busy)
	}
	if feed(t, a, "<C-p>;<C-c>"); a.palette != nil {
		t.Error("idle C-c keeps the palette")
	}
}

// With nothing after the ;, the SQL scope lists the connection's history,
// newest first, and ↵ runs the pick; the 所有 scope never lists it (§12).
func TestQuickSQLHistory(t *testing.T) {
	a := sized(160, 45, "nerd")
	for _, sql := range []string{"select 1", "select 2", "select 1"} {
		feed(t, a, "<C-p>;"+sql+"<CR><Esc>")
		a.busy = 0
	}
	if h := a.state.SQL["doraemon"]; !slices.Equal(h, []string{"select 1", "select 2"}) {
		t.Fatalf("history %v: a repeat moves up", h)
	}
	feed(t, a, "<C-p>;")
	if got := strings.Join(namesOf(a), " "); got != ":select 1 :select 2" || enterOf(a) != "↵ 执行" { // one kind: no tag
		t.Fatalf("empty: %s, enter %q", got, enterOf(a))
	}
	feed(t, a, "<C-n><CR>")
	if a.palette.input.Text != ";select 2" || a.palette.quick.running != "select 2" {
		t.Errorf("↵ on select 2: input %q running %q", a.palette.input.Text, a.palette.quick.running)
	}
	feed(t, a, "<Esc><C-p>select")
	if slices.ContainsFunc(namesOf(a), func(n string) bool { return strings.HasPrefix(n, ":") }) {
		t.Error("所有 lists the SQL history")
	}
	for i := range historyRows + 5 {
		a.palette = nil
		feed(t, a, fmt.Sprintf("<C-p>;select %d<CR>", i))
	}
	if n := len(a.state.SQL["doraemon"]); n != historyRows {
		t.Errorf("history keeps %d", n)
	}
}

// The word typed completes from the columns of the tables named, fetched
// once, then the tree's schema's tables and keywords; ↵ takes the pick,
// Tab moves it while the list is up and switches scopes when it isn't, esc
// closes the list first (§12「补全」, §9.7).
func TestQuickSQLCompletion(t *testing.T) {
	a := sized(160, 45, "nerd")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Tables {
		if tb.Name == "t_order" {
			a.sess.Tables[i] = table
		}
	}
	feed(t, a, "<C-p>;select * from t_order where st")
	if !a.palette.asked[idOf(table)] {
		t.Fatal("t_order's columns were not asked for")
	}
	if a.mode() != keymap.Command || a.context().Overlay != "complete" {
		t.Errorf("mode %v, overlay %q", a.mode(), a.context().Overlay)
	}
	a.Update(colsMsg{table, cols, nil})
	c := a.palette.comp
	if c == nil || c.items[0].label != "status" || c.items[0].note != "order_status · t_order" {
		t.Fatalf("after the columns: %+v", c)
	}
	feed(t, a, "<CR>")
	if a.palette.input.Text != ";select * from t_order where status" || a.palette.comp != nil || a.palette.quick != nil {
		t.Fatalf("↵: %q", a.palette.input.Text)
	}
	feed(t, a, " = 1 and mt_t")
	if c := a.palette.comp; c == nil || c.items[0].label != "mt_task" || c.items[0].note != "表" {
		t.Fatalf("a table: %+v", c)
	}
	feed(t, a, "<Esc>")
	if a.palette == nil || a.palette.comp != nil {
		t.Fatal("esc closes the list first")
	}
	feed(t, a, "<BS>t<Tab>")
	if c := a.palette.comp; c == nil || c.sel != 1 || !strings.HasPrefix(a.palette.input.Text, ";") {
		t.Fatalf("Tab with the list up moves it: %+v, %q", c, a.palette.input.Text)
	}
	feed(t, a, "<Esc><CR>")
	if a.palette.quick == nil || a.palette.quick.running != "select * from t_order where status = 1 and mt_t" {
		t.Errorf("↵ with the list closed runs: %+v", a.palette.quick)
	}
	feed(t, a, " selec<CR>")
	if !strings.HasSuffix(a.palette.input.Text, " select") {
		t.Errorf("↵ takes the first: %q", a.palette.input.Text)
	}
	if feed(t, a, "<Tab>"); strings.HasPrefix(a.palette.input.Text, ";") {
		t.Errorf("Tab with the list closed switches scopes: %q", a.palette.input.Text)
	}
	a.palette = nil
	feed(t, a, "<C-p>;sel<Left><Left><Left><Left><Right>") // into the ; and out: nothing to complete there
	if a.palette.input.Pos != 1 || a.palette.comp != nil {
		t.Errorf("pos %d, comp %v", a.palette.input.Pos, a.palette.comp)
	}
}

// C-y copies the result as CSV, NULL as nothing (F-04).
func TestQuickSQLCopy(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>;select 1<CR>")
	answerQuick(a, quickResult(2), nil)
	_, cmd := a.Update(teaKey("<C-y>"))
	if cmd == nil {
		t.Fatal("no copy")
	}
	if got := fmt.Sprint(cmd()); got != "id,note\n1,note 1\n2,\n" {
		t.Errorf("copied %q", got)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "quicksql.copy"}).Min) // the title's C-y CSV
}

// The wheel over the result scrolls it (§12).
func TestQuickSQLScroll(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>;select 1<CR>")
	answerQuick(a, quickResult(100), nil)
	_, _, area := a.paletteBox(0)
	a.Update(tea.MouseWheelMsg{X: area.Min.X + 5, Y: area.Min.Y + 3, Button: tea.MouseWheelDown})
	if a.palette.quick.top != wheelStep {
		t.Errorf("top %d", a.palette.quick.top)
	}
}

// With nothing after the ;, the history's rows take the whole width: the
// list has no locations to line up (§12「列对齐」).
func TestGoldenQuickSQLHistory160x45(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.state.SQL = map[string][]string{"doraemon": {"select id, note, created_at from t_order where id < 3 order by id", "select 1"}}
	feed(t, a, "<C-p>;")
	golden.RequireEqual(t, a.render().String())
}

func TestGoldenQuickSQL160x45(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>;select * from t<Esc>")
	r := quickResult(100)
	r.Truncated = true
	feed(t, a, "<CR>")
	answerQuick(a, r, nil)
	feed(t, a, "x")
	golden.RequireEqual(t, a.render().String())
}
