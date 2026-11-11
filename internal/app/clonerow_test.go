package app

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
)

// cloned is the cells of the row added the cursor is in, by column, as their
// values say.
func cloned(t *testing.T, tab *dataTab) map[string]string {
	t.Helper()
	sr := tab.shownRows()[tab.row]
	if sr.add == nil {
		t.Fatal("the cursor is in no row added")
	}
	out := map[string]string{}
	for name, e := range sr.add.cells {
		out[name] = e.val.S
	}
	return out
}

// yp adds a row under the cursor's with its values, the cursor in the same
// column; the columns the database makes the value of are left to it: a
// serial key, an identity, a generated column, an auto_increment; a key typed
// by hand is copied (F22.4).
func TestCloneRow(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.cols.Cols[0].Default = "nextval('t_order_id_seq'::regclass)"
	tab.cols.Cols[3].Generated = true // paid
	feed(t, a, "jl")
	want := map[string]string{"status": "done", "amount": "2.99", "meta": `{"n": 2, "tags": ["a", "b"]}`, "note": tab.page.Rows[1][5].S, "created_at": "2026-09-01 00:02:00+00"}
	if feed(t, a, "yp"); len(tab.added) != 1 || tab.row != 2 || tab.col != 1 {
		t.Fatalf("yp: added %d, cursor %d, %d", len(tab.added), tab.row, tab.col)
	}
	if got := cloned(t, tab); !maps.Equal(got, want) {
		t.Errorf("cloned %v, want %v", got, want)
	}
	// a key typed by hand, an auto_increment: the first is copied, the second is not
	tab.cols.Cols[0].Default = ""
	feed(t, a, "jyp")
	if got := cloned(t, tab); got["id"] != "3" || len(tab.added) != 2 {
		t.Errorf("a key with no default: %v", got)
	}
	tab.cols.Cols[0].Default = "auto_increment"
	feed(t, a, "gg")
	feed(t, a, "yp")
	if got := cloned(t, tab); got["id"] != "" || got["status"] != "pending" && got["status"] != "running" {
		t.Errorf("an auto_increment: %v", got)
	}
}

// What is on the screen is what is cloned: a change not yet saved, and a row
// added, which is cloned under its own (F22.4).
func TestCloneRowOfWhatIsShown(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	feed(t, a, "jlli9<Esc>") // row 2's amount
	feed(t, a, "yp")
	if got := cloned(t, tab); got["amount"] != "9" {
		t.Errorf("an edited row: %v", got)
	}
	feed(t, a, "i7<Esc>") // the clone's amount: the cursor is in the same column
	feed(t, a, "yp")
	if len(tab.added) != 2 || tab.row != 3 || cloned(t, tab)["amount"] != "7" {
		t.Errorf("a row added: %d rows, cursor %d, %v", len(tab.added), tab.row, cloned(t, tab))
	}
	if tab.added[0].after != tab.added[1].after || slices.Index(tab.added, tab.added[1]) != 1 {
		t.Errorf("the clone of a row added goes under it: %+v %+v", tab.added[0], tab.added[1])
	}
	feed(t, a, "r") // r takes a row added out, as on any
	if len(tab.added) != 1 {
		t.Errorf("r: %d rows added", len(tab.added))
	}
	feed(t, a, "yp")
	feed(t, a, "dd") // so does dd
	if len(tab.added) != 1 {
		t.Errorf("dd: %d rows added", len(tab.added))
	}
}

// The turned table clones the record the cursor is in (F22.4).
func TestCloneRowTurned(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	a.run("grid.transpose", 0)
	feed(t, a, "l")
	if feed(t, a, "yp"); len(tab.added) != 1 || cloned(t, tab)["status"] != "done" {
		t.Errorf("turned: added %d", len(tab.added))
	}
}

// A table that takes no change says so as o does, and a row with a binary
// column is not cloned as p does not (F22.4).
func TestCloneRowRefused(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.conn.readOnly = true
	feed(t, a, "yp")
	if len(tab.added) != 0 || !strings.Contains(a.toast, "只读连接") {
		t.Errorf("read only: %d added, toast %q", len(tab.added), a.toast)
	}
	tab.page.Rows[0][5] = db.Val{S: db.BinaryShown(4)}
	if feed(t, a, "yp"); len(tab.added) != 0 || !strings.Contains(a.toast, "只读连接") {
		t.Errorf("read only with a binary column: %d added, toast %q", len(tab.added), a.toast)
	}
	tab.conn.readOnly = false
	if feed(t, a, "yp"); len(tab.added) != 0 || !strings.Contains(a.toast, "二进制") {
		t.Errorf("a binary column: %d added, toast %q", len(tab.added), a.toast)
	}
}

// The grid's menu has the row's three after the others, each with its key; 取消删除
// where the row is marked; dim where the table takes no change, and not picked (F22.4).
func TestRowMenu(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	items := a.columnItems(tab, true)
	rows := items[len(items)-3:]
	for i, want := range []struct{ label, action, key string }{{"添加行", "grid.row.add", "o"}, {"删除行", "grid.row.delete", "dd"}, {"克隆行", "grid.row.clone", "yp"}} {
		if rows[i].label != want.label || rows[i].action != want.action || rows[i].run == nil || a.menuKey(want.action) != want.key {
			t.Errorf("item %d: %q %q key %q", i, rows[i].label, rows[i].action, a.menuKey(rows[i].action))
		}
	}
	if a.columnItems(tab, false)[0].action == "grid.row.add" {
		t.Error("a head has no row items")
	}
	feed(t, a, "dd")
	if rows = a.columnItems(tab, true); rows[len(rows)-2].label != "取消删除" {
		t.Errorf("a row marked: %q", rows[len(rows)-2].label)
	}
	tab.conn.readOnly = true
	rows = a.columnItems(tab, true)
	for _, it := range rows[len(rows)-3:] {
		if it.run != nil {
			t.Errorf("%s is not dim on a read only connection", it.label)
		}
	}
	a.menu = &menu{items: rows[len(rows)-3:]}
	if a.menuMove(1); a.menu.sel != 0 {
		t.Errorf("the selection went to a dim item: %d", a.menu.sel)
	}
	if a.menuRun(0); a.menu == nil {
		t.Error("a dim item closed the menu")
	}
}

// y waits for its second key, and which-key lists p 克隆行 under it (F22.4).
func TestCloneRowWhichKey(t *testing.T) {
	a := wide(160, 45)
	loadOrders(t, a, 3)
	feed(t, a, "y")
	due(a)
	if !a.whichKey {
		t.Fatal("no which-key after y")
	}
	found := false
	for _, it := range a.whichKeyOverlay().Items {
		found = found || it.Key == "p" && it.Title == "克隆行"
	}
	if !found {
		t.Errorf("no p 克隆行: %+v", a.whichKeyOverlay().Items)
	}
	feed(t, a, "<Esc><C-p>>克隆行")
	if got := namesOf(a); len(got) != 1 || got[0] != "命令:克隆行" {
		t.Errorf("the palette: %v", got)
	}
}
