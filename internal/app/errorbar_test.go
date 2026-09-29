package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/jackc/pgx/v5/pgconn"

	"sqlmux/internal/db"
	"sqlmux/internal/ui"
)

// PG's Position, a character of the statement from 1, as a line and a
// column of the console's text; past the statement's end, its end.
func TestErrorAt(t *testing.T) {
	text := "select 1;\n\n  select * frm 中文 t;"
	off := strings.Index(text, "select *")
	stmt := "select * frm 中文 t"
	for pos, want := range map[int]string{1: "位置：第 3 行第 3 列", 10: "位置：第 3 行第 12 列", 16: "位置：第 3 行第 18 列", 99: "位置：第 3 行第 20 列"} {
		if got := errorAt(text, off, stmt, pos); got != want {
			t.Errorf("%d: %s, want %s", pos, got, want)
		}
	}
}

// A console's failed run says so on its error bar: [SQLSTATE], DETAIL's
// lines, HINT and where; the text takes the rows it has. esc in NORMAL
// closes it, not with a count typed, nor in VISUAL; the next run takes it
// away. A console closed by then keeps it in the log alone (§7.8).
func TestConsoleErrorBar(t *testing.T) {
	fail := &pgconn.PgError{Severity: "ERROR", Code: "42P01", Message: `relation "nope" does not exist`, Detail: "one\ntwo", Hint: "look again", Position: 15}
	d := &execDB{res: map[string]db.Result{"select 1": rows(1)}, fail: map[string]error{"select * from nope": fail}}
	a, c := inRun(t, d, "select 1;\n\nselect * from nope;")
	p := a.focused()
	press(t, a, "G<CR>")
	if c.bar == nil || c.bar.First.Head != "[42P01] " || !slices.Equal(c.bar.More, []string{"DETAIL: one", "two", "HINT: look again", "位置：第 3 行第 15 列"}) {
		t.Fatalf("bar %+v", c.bar)
	}
	if _, body := a.consoleView(p, c); body.Dy() != bodyRect(a.layout()[p.ID]).Dy()-5 {
		t.Errorf("the text is %d rows, %d without the bar", body.Dy(), bodyRect(a.layout()[p.ID]).Dy())
	}
	if st := styleOf(t, a.render(), `relation "nope"`); st.Fg != a.theme.Error || st.Bg != a.theme.ErrorBg {
		t.Errorf("drawn: %+v", st)
	}
	if press(t, a, "v<Esc>3<Esc>"); c.bar == nil || c.ed.Mode() != 0 {
		t.Fatal("VISUAL's esc and a count's are no close")
	}
	if press(t, a, "<Esc>"); c.bar != nil {
		t.Fatal("esc in NORMAL closes it")
	}
	press(t, a, "<CR>")
	if press(t, a, "gg<CR>"); c.bar != nil {
		t.Error("the next run takes it away")
	}
	d.fail["select 1"] = fail // R on select 1's result, the console closed
	a.closeTab(p)
	a.win().focus(a.win().Result.ID)
	if press(t, a, "R"); c.bar != nil || !strings.HasSuffix(logText(a), "HINT: look again") {
		t.Errorf("rerun of a closed console: bar %+v, log %q", c.bar, logText(a))
	}
}

// A table's error bar: esc and its × close it (§7.8); the table above
// gives it the rows it takes.
func TestTableErrorBar(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	h := gridRect(a.layout()[p.ID], tab).Dy()
	a.fetch(tab, false)
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: &pgconn.PgError{Code: "42703", Message: "column x does not exist", Hint: "y?"}})
	if gridRect(a.layout()[p.ID], tab).Dy() != h-2 {
		t.Fatal("the grid gives the bar its two rows")
	}
	if feed(t, a, "<Esc>"); tab.bar != nil {
		t.Fatal("esc closes it")
	}
	a.fetch(tab, false)
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: fmt.Errorf("gone")})
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: fmt.Sprintf("pane.error.close %d", p.ID)}).Min)
	if tab.bar != nil {
		t.Error("× closes it")
	}
}

// A failed fetch puts ORDER, LIMIT and PAGE back to what the rows shown
// came from, so the chips' keys act on what they show; its WHERE stays, in
// the input too, for R to try again. A ; turned down likewise (§7.6).
func TestFailedFetchBackToShown(t *testing.T) {
	a, tab, rec := withRecorder(t, 160, 45)
	tab.next = true
	fail := func() { a.Update(pageMsg{tab: tab, seq: tab.seq, err: &pgconn.PgError{Code: "42883", Message: "no"}}) }
	feed(t, a, "gonote<CR>")
	fail()
	if sql := lastSQL(t, a, rec, a.run("grid.order.toggle", 0)); !strings.Contains(sql, `order by "id" desc`) {
		t.Errorf("the chip's id ↑ turned: %s", sql)
	}
	fail()
	feed(t, a, "]")
	fail()
	if feed(t, a, "]"); tab.pageNo != 1 {
		t.Errorf("] from the page shown: page %d", tab.pageNo+1)
	}
	fail()
	feed(t, a, "/bad<CR>")
	fail()
	if tab.request != (request{applied: "bad", limit: 100}) || tab.where.Text != "bad" {
		t.Errorf("the WHERE stays: %+v, input %q", tab.request, tab.where.Text)
	}
	if sql := lastSQL(t, a, rec, a.run("grid.refresh", 0)); !strings.Contains(sql, "bad") {
		t.Errorf("R tries it again: %s", sql)
	}
	fail()
	n := len(rec.sqls)
	feed(t, a, "/<C-u>1=1; x<CR>gl500<CR>")
	if tab.request != (request{applied: "1=1; x", limit: 100}) || len(rec.sqls) != n || tab.bar == nil {
		t.Errorf("a ; turned down: %+v, sent %q", tab.request, rec.sqls[n:])
	}
}

// The bar at most six rows, the last … past them, each cut at the right;
// the first's message cut in its middle (§7.8「错误栏」).
func TestGoldenErrorBar160x45(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 20)
	a.fetch(tab, false)
	a.Update(pageMsg{tab: tab, seq: tab.seq, err: &pgconn.PgError{
		Code: "23505", Message: strings.Repeat("duplicate key value violates unique constraint ", 4),
		Detail: "Key (id)=(1) already exists.\nsecond line of the detail\nthird " + strings.Repeat("long ", 40) + "\nfourth\nfifth", Hint: "one more",
	}})
	golden.RequireEqual(t, a.render().String())
}
