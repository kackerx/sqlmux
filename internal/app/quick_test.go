package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// quickResult is n rows of id and note, the note NULL on even ids.
func quickResult(n int) db.Result {
	r := db.Result{Cols: []db.Col{{Name: "id", Type: "int4"}, {Name: "note", Type: "text"}}, Took: 12 * time.Millisecond}
	for i := 1; i <= n; i++ {
		note := db.Val{S: fmt.Sprintf("note %d", i)}
		if i%2 == 0 {
			note = db.Val{Null: true}
		}
		r.Rows = append(r.Rows, []db.Val{{S: strconv.Itoa(i)}, note})
	}
	return r
}

// argDB records the text arguments of what it is asked.
type argDB struct {
	noDB
	args []string
}

func (d *argDB) Query(_ context.Context, _ string, args ...db.Val) (db.Result, error) {
	for _, v := range args {
		d.args = append(d.args, v.S)
	}
	return db.Result{}, nil
}

// answerQuick hands the palette the quick SQL's answer.
func answerQuick(a *App, r db.Result, err error) tea.Cmd {
	_, cmd := a.Update(quickMsg{a.palette, a.sess.Conns[0], "public", r, err})
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
	if q.running != "select 1" || a.sess.Conns[0].busy != 1 || !slices.Equal(a.state.SQL["doraemon"], []string{"select 1"}) {
		t.Fatalf("running %q, busy %d, history %v", q.running, a.sess.Conns[0].busy, a.state.SQL)
	}
	if r := a.paletteView().Result; r == nil || r.Title != "… 行 · 只读" {
		t.Fatalf("running: %+v", r)
	}
	if feed(t, a, "<CR>"); a.sess.Conns[0].busy != 1 {
		t.Error("↵ while one runs ran another")
	}
	answerQuick(a, quickResult(3), nil)
	if r := a.paletteView().Result; a.sess.Conns[0].busy != 0 || q.ran != "select 1" || r.Title != "3 行 · 12ms · 只读" || len(r.Grid.Rows) != 3 || r.Grid.Row != -1 {
		t.Fatalf("answered: busy %d, %+v", a.sess.Conns[0].busy, r)
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

// ; opens the palette in the SQL scope from the table, the tree and an
// empty pane (§12, §6.8).
func TestSemicolonOpensQuickSQL(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 3)
	a.sess.Conns[0].busy = 0
	feed(t, a, "<Space>%") // an empty pane
	for _, focus := range []int{1, 0, a.focused().ID} {
		a.win().focus(focus)
		feed(t, a, ";")
		if s, _ := a.paletteScope(); a.palette == nil || a.palette.input.Text != ";" || s != sqlScope {
			t.Fatalf("; on pane %d: %+v", focus, a.palette)
		}
		feed(t, a, "select 1 <CR>")
		if a.palette.quick == nil || a.palette.quick.running != "select 1 " {
			t.Errorf("pane %d: %+v", focus, a.palette.quick)
		}
		a.palette, a.sess.Conns[0].busy = nil, 0
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
	a.Update(quickMsg{p, a.sess.Conns[0], "public", quickResult(1), nil})
	if a.sess.Conns[0].busy != 0 || a.palette != nil {
		t.Errorf("an answer to a closed palette: busy %d", a.sess.Conns[0].busy)
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
		a.sess.Conns[0].busy = 0
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
	for i := range quickHistory + 5 {
		a.palette = nil
		feed(t, a, fmt.Sprintf("<C-p>;select %d<CR>", i))
	}
	if n := len(a.state.SQL["doraemon"]); n != quickHistory {
		t.Errorf("history keeps %d", n)
	}
}

// The word typed completes from the columns of the tables named, fetched
// once, then the tree's schema's tables and keywords, each starting as the
// word does; ↵ takes the selected one and does no more,
// Tab moves while the list is up and switches scopes when it isn't, esc
// closes the list first (§12「补全」, §9.7).
func TestQuickSQLCompletion(t *testing.T) {
	a := sized(160, 45, "nerd")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	feed(t, a, "<C-p>;select * from t_order where st")
	if !a.palette.asked[idOf(table)] {
		t.Fatal("t_order's columns were not asked for")
	}
	if a.mode() != keymap.Command || a.context().Overlay != "complete" {
		t.Errorf("mode %v, overlay %q", a.mode(), a.context().Overlay)
	}
	a.Update(colsMsg{a.sess.Conns[0], table, cols, nil})
	c := a.palette.comp
	if c == nil || c.items[0].label != "status" || c.items[0].note != "order_status · t_order" {
		t.Fatalf("after the columns: %+v", c)
	}
	feed(t, a, "<CR>")
	if a.palette.input.Text != ";select * from t_order where status " || a.palette.comp != nil || a.palette.quick != nil {
		t.Fatalf("↵ takes the first: %q", a.palette.input.Text)
	}
	feed(t, a, "= 1 and mt_t")
	if c := a.palette.comp; c == nil || c.items[0].label != "mt_task" || c.items[0].note != "表" {
		t.Fatalf("a table: %+v", c)
	}
	feed(t, a, "<Esc>")
	if a.palette == nil || a.palette.comp != nil {
		t.Fatal("esc closes the list first")
	}
	feed(t, a, "<BS>t<Tab>")
	if c := a.palette.comp; c == nil || c.sel != 1%len(c.items) || !strings.HasPrefix(a.palette.input.Text, ";") {
		t.Fatalf("Tab with the list up moves it: %+v, %q", c, a.palette.input.Text)
	}
	feed(t, a, "<Esc><CR>")
	if a.palette.quick == nil || a.palette.quick.running != "select * from t_order where status = 1 and mt_t" {
		t.Errorf("↵ with the list closed runs: %+v", a.palette.quick)
	}
	a.palette.quick = nil
	a.palette.input = ui.Input{Text: ";select 42 as", Pos: len(";select 42 as")}
	feed(t, a, " x")
	if a.palette.comp != nil {
		t.Fatalf("x: nothing starts with it, max no longer comes up: %+v", a.palette.comp)
	}
	if feed(t, a, "<CR>"); a.palette.quick == nil || a.palette.quick.running != "select 42 as x" {
		t.Errorf("↵ runs what is typed: %+v", a.palette.quick)
	}
	a.palette.quick = nil
	a.palette.input = ui.Input{Text: ";select * from tord", Pos: len(";select * from tord")}
	feed(t, a, "<BS>d")
	if c := a.palette.comp; c == nil || c.items[0].label != "t_order" {
		t.Fatalf("tord still finds t_order: %+v", c)
	}
	feed(t, a, "<CR><BS><BS>r")
	if c := a.palette.comp; c == nil || c.items[0].label != "t_order" {
		t.Fatalf("t_order typed out: %+v", c)
	}
	if feed(t, a, "<CR>"); a.palette.quick != nil || a.palette.input.Text != ";select * from t_order " {
		t.Fatalf("↵ takes t_order and does no more: %q, %+v", a.palette.input.Text, a.palette.quick)
	}
	if feed(t, a, "<CR>"); a.palette.quick == nil || a.palette.quick.running != "select * from t_order " {
		t.Fatalf("↵ with the list closed runs: %+v", a.palette.quick)
	}
	a.palette.quick = nil
	feed(t, a, "selec<CR>")
	if !strings.HasSuffix(a.palette.input.Text, " select ") {
		t.Errorf("↵ takes the first: %q", a.palette.input.Text)
	}
	c = a.palette.comp
	if c == nil || c.sel != -1 {
		t.Fatalf("select's list comes up at once, nothing selected: %+v", c)
	}
	first := c.items[0].label
	if feed(t, a, "<Tab><CR>"); a.palette.comp != nil || !strings.HasSuffix(a.palette.input.Text, " "+first+" ") || a.palette.quick != nil { // Tab selects the first, ↵ takes it
		t.Errorf("Tab ↵: %q, %+v", a.palette.input.Text, a.palette.quick)
	}
	a.palette.input = ui.Input{Text: ";select * from t_order select", Pos: len(";select * from t_order select")}
	feed(t, a, " ")
	if a.palette.comp == nil || a.palette.comp.sel != -1 {
		t.Fatalf("a space typed: %+v", a.palette.comp)
	}
	if feed(t, a, "<CR>"); a.palette.quick == nil || a.palette.comp != nil { // nothing selected: ↵ is the palette's, it runs
		t.Errorf("↵ with nothing selected: %+v", a.palette.quick)
	}
	a.palette.quick = nil
	if feed(t, a, "<Tab>"); strings.HasPrefix(a.palette.input.Text, ";") {
		t.Errorf("Tab with the list closed switches scopes: %q", a.palette.input.Text)
	}
	a.palette = nil
	feed(t, a, "<C-p>;sel<Left><Left><Left><Left><Right>") // into the ; and out: nothing to complete there
	if a.palette.input.Pos != 1 || a.palette.comp != nil {
		t.Errorf("pos %d, comp %v", a.palette.input.Pos, a.palette.comp)
	}
	a.palette.input = ui.Input{Text: ";select  from t_order o", Pos: len(";select ")}
	feed(t, a, "o.") // an alias's columns, nothing typed after its . (§9.7)
	if c := a.palette.comp; c == nil || c.items[0].label != "id" || len(c.items) != len(cols.Cols) {
		t.Errorf("o.: %+v", c)
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
	a := wide(160, 45)
	a.state.SQL = map[string][]string{"doraemon": {"select id, note, created_at from t_order where id < 3 order by id", "select 1"}}
	feed(t, a, "<C-p>;")
	golden.RequireEqual(t, a.render().String())
}

func TestGoldenQuickSQL160x45(t *testing.T) {
	a := wide(160, 45)
	feed(t, a, "<C-p>;select * from t<Esc>")
	r := quickResult(100)
	r.Truncated = true
	feed(t, a, "<CR>")
	answerQuick(a, r, nil)
	feed(t, a, "x")
	golden.RequireEqual(t, a.render().String())
}

// A write is not run, nor sent: the result area says so in warn's color,
// C-e's key read from the keymap; the history keeps it (F-05). WITH …
// DELETE is a write too; a typo, no keyword first, is sent.
func TestQuickSQLWrite(t *testing.T) {
	a := sized(160, 45, "nerd")
	rec := &recDB{}
	a.sess.Conns[0].Meta = db.NewWorker(rec)
	for _, sql := range []string{"delete from t_order", "with x as (delete from t_order returning *) select * from x"} {
		feed(t, a, "<C-p>;"+sql+"<Esc><CR>") // Esc closes the list ↵ would take
		r := a.paletteView().Result
		sent := slices.ContainsFunc(rec.sqls, func(s string) bool { return strings.Contains(s, "delete") }) // columns completion asked for aside
		if a.sess.Conns[0].busy != 0 || sent || r == nil || r.Title != "只读" || r.Err != "写语句不在这里执行 · C-e 在 query 中打开" || !r.Warn {
			t.Fatalf("%s: busy %d, sent %q, %+v", sql, a.sess.Conns[0].busy, rec.sqls, r)
		}
		if a.state.SQL["doraemon"][0] != sql || enterOf(a) != "↵ 执行" {
			t.Errorf("%s: history %v, enter %q", sql, a.state.SQL, enterOf(a))
		}
		feed(t, a, "<Esc>")
	}
	if feed(t, a, "<C-p>;selec 1<CR>"); a.sess.Conns[0].busy != 1 || a.palette.quick.write { // no keyword first: the database says what is wrong
		t.Errorf("a typo: busy %d", a.sess.Conns[0].busy)
	}
}

// C-t puts the rows shown in the result area as a pinned quick #n, n the
// session's run count, a line in the log, the palette open; again, another
// tab. With no rows nothing. R on it runs the SQL again as the quick SQL
// does, its rows in place; failing, the log says so as a query's run's,
// and shows (§12「C-t 送到结果区」).
func TestQuickSQLToResult(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>;select 1<CR>")
	if feed(t, a, "<C-t>"); a.win().Result != nil {
		t.Fatal("no rows yet: a tab")
	}
	answerQuick(a, quickResult(2), nil)
	feed(t, a, "<C-t>")
	res := a.win().Result
	if res == nil || tabNames(res) != "日志 quick #1" || res.Cur != 1 || !resultOf(res).pinned || a.palette == nil {
		t.Fatalf("C-t: %v, palette %v", res, a.palette)
	}
	if l := a.win().log[len(a.win().log)-1]; !strings.Contains(l.Head, "  quick  select 1  ") || l.Tail != "2 行 · 12ms" {
		t.Errorf("the log: %+v", l)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "palette.open.tab"}).Min) // the title's C-t 结果区
	if tabNames(res) != "日志 quick #1 quick #2" {
		t.Fatalf("again: %v", tabNames(res))
	}
	feed(t, a, "<Esc>")
	a.win().focus(res.ID)
	a.sess.Conns[0].Schema = "agentable" // R keeps the schema it ran in
	rec := &argDB{}
	a.sess.Conns[0].Meta = db.NewWorker(rec)
	_, cmd := a.Update(teaKey("R"))
	if cmd == nil || a.sess.Conns[0].busy != 1 || resultOf(res).run.done {
		t.Fatalf("R: busy %d", a.sess.Conns[0].busy)
	}
	if batch, ok := cmd().(tea.BatchMsg); ok { // the rerun and the dots' tick, when none is on its way; else the rerun ran
		batch[0]()
	}
	if !slices.Contains(rec.args, `"public"`) {
		t.Errorf("R's search_path: %v", rec.args)
	}
	feed(t, a, "gj") // the JSON view stays, the rows in it are the new ones (F14.5)
	a.Update(quickRerun{resultOf(res), quickResult(5), nil})
	if rt := resultOf(res); a.sess.Conns[0].busy != 0 || len(rt.page.Rows) != 5 || !rt.run.done || rt.jv == nil {
		t.Errorf("rerun: %d rows, JSON view %v", len(rt.page.Rows), rt.jv != nil)
	}
	rt := resultOf(res)
	a.Update(teaKey("R"))
	a.Update(quickRerun{rt, db.Result{}, &pgconn.PgError{Severity: "ERROR", Message: "gone", Hint: "look again"}})
	if l := a.win().log; len(rt.page.Rows) != 5 || l[len(l)-2].Tail != "ERROR: gone" || l[len(l)-1].Tail != "    HINT: look again" || res.Cur != 0 {
		t.Errorf("a failed rerun: the log %+v, on tab %d", l[len(l)-2:], res.Cur)
	}
}

// C-e puts the SQL scope's text, the ; off, in a new query in the pane a
// table would open in, which is empty, a number no file has (F8.21); the
// palette goes, the query has the focus, its cursor on the SQL in NORMAL
// (§12). The candidates up, it still does.
func TestQuickSQLEdit(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	if err := config.WriteQuery(config.QueryPath("query_2"), "select 1\n"); err != nil { // a file: its number is not new
		t.Fatal(err)
	}
	feed(t, a, "<C-p>;delete from t_order<Esc><CR><C-e>")
	c := queryOf(a.focused())
	if a.palette != nil || c == nil || a.focused().Object() != "query_3" || text(c) != "delete from t_order" {
		t.Fatalf("C-e: palette %v, %v", a.palette, a.focused().Object())
	}
	if c.ed.Cursor() != (editor.Pos{}) || c.ed.Mode() != editor.Normal {
		t.Errorf("cursor %v in %v", c.ed.Cursor(), c.ed.Mode())
	}
	if feed(t, a, "u"); text(c) != "" {
		t.Errorf("one undo step: %q", text(c))
	}
	feed(t, a, "<C-p>;select * from t_or") // the candidates up: C-e over them, as C-t and C-y
	if a.palette == nil || a.palette.comp == nil {
		t.Fatal("no candidates")
	}
	if feed(t, a, "<C-e>"); a.palette != nil || !strings.HasSuffix(text(queryOf(a.focused())), "select * from t_or") {
		t.Errorf("C-e over the candidates: palette %v", a.palette)
	}
}

// The palette goes by the connection current as it opened (F5.11): its
// quick SQL runs there, its history kept there, and C-e opens the query
// there, kept for the file, whatever the file last ran on and whatever is
// current meanwhile.
func TestQuickSQLConn(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, seed := withConns()
	doraemon := a.sess.Conns[0]
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "idle"}}
	feed(t, a, "<C-p>;select 1")
	a.win().focus(0)
	treeTo(t, a, "seed")
	if feed(t, a, "<CR>"); a.conn() != seed || doraemon.busy != 1 || seed.busy != 0 || len(a.state.SQL["doraemon"]) != 1 {
		t.Fatalf("on %s: busy %d / %d, history %v", a.conn().Spec.Name, doraemon.busy, seed.busy, a.state.SQL)
	}
	answerQuick(a, quickResult(1), nil)
	feed(t, a, "<C-e>")
	c := queryOf(a.focused())
	if a.palette != nil || c == nil || c.conn != doraemon || c.want != nil || a.state.Queries["query_2"].Conn != "doraemon" {
		t.Errorf("C-e: palette %v, query %+v, kept %+v", a.palette, c, a.state.Queries)
	}
}

// In quick SQL ↵ with the candidates up only takes the selected one, the
// text the same or not; a second runs it. A key that types nothing, an
// arrow, opens no list (§9.7, F8.1).
func TestQuickSQLEnterOnlyAccepts(t *testing.T) {
	a := sized(160, 45, "nerd")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
	feed(t, a, "<C-p>;select count")
	if a.palette.comp == nil {
		t.Fatal("no list for count")
	}
	if feed(t, a, "<CR>"); a.palette.quick != nil || a.palette.input.Text != ";select count" || a.palette.comp != nil {
		t.Fatalf("↵ takes count, the text the same: %q, ran %v", a.palette.input.Text, a.palette.quick != nil)
	}
	if feed(t, a, "<CR>"); a.palette.quick == nil || a.palette.quick.running != "select count" {
		t.Errorf("the second ↵ runs: %+v", a.palette.quick)
	}
	a.palette.quick = nil
	feed(t, a, "<C-u>;select * from t_order where id = 1 and paid"+strings.Repeat("<Left>", 4)+"<CR>")
	if a.palette.quick == nil || a.palette.quick.running != "select * from t_order where id = 1 and paid" {
		t.Errorf("←↵ runs: %q, %+v", a.palette.input.Text, a.palette.quick)
	}
}
