package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
)

// gd on the column of a foreign key opens the table it points at in the
// pane, its WHERE the referenced columns = this row's values, one table's tab
// going to it again (F22.13).
func TestFollowForeignKey(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 60)
	c, p := tab.conn, a.focused()
	c.keys[idOf(tab.table)] = []db.Key{
		{Name: "order_pkey", Cols: []string{"id"}},
		{Name: "fk_user", Cols: []string{"id"}, Ref: &db.Ref{Schema: "public", Table: "t_user", Cols: []string{"uid"}}},
		{Name: "fk_note", Cols: []string{"note"}, Ref: &db.Ref{Schema: "agentable", Table: "planner", Cols: []string{"title"}}},
		{Name: "fk_two", Cols: []string{"id", "status"}, Ref: &db.Ref{Schema: "public", Table: "t_sku", Cols: []string{"a", "b"}}},
		{Name: "fk_gone", Cols: []string{"paid"}, Ref: &db.Ref{Schema: "public", Table: "nope", Cols: []string{"x"}}},
	}
	at := func(col string, row int) {
		tab.col = slices.IndexFunc(tab.shownCols(), func(i int) bool { return tab.page.Cols[i].Name == col })
		tab.row = row
		a.win().focus(p.ID)
		selectTab(p, 0)
	}
	where := func() string { return dataOf(p).applied }
	tabs := func() int { return len(p.Tabs) }

	at("status", 0) // in the second key only, which is two columns: the first of the keys that has it
	feed(t, a, "gd")
	if tabs() != 2 || where() != `"a" = 1 and "b" = 'running'` || p.Tabs[p.Cur].Name != "t_sku" {
		t.Fatalf("two columns: %d tabs, %q, %q", tabs(), p.Tabs[p.Cur].Name, where())
	}
	if h := a.tableState(dataOf(p)).History; len(h) != 1 || h[0].Where != where() {
		t.Errorf("history %+v", h)
	}

	at("id", 1) // a number is bare, and the tab of t_user is new
	feed(t, a, "gd")
	if tabs() != 3 || p.Tabs[p.Cur].Name != "t_user" || where() != `"uid" = 2` {
		t.Fatalf("one column: %d tabs, %q, %q", tabs(), p.Tabs[p.Cur].Name, where())
	}
	at("id", 4) // it is there: the pane goes to it, the WHERE is this one's
	feed(t, a, "gd")
	if tabs() != 3 || p.Tabs[p.Cur].Name != "t_user" || where() != `"uid" = 5` {
		t.Errorf("again: %d tabs, %q, %q", tabs(), p.Tabs[p.Cur].Name, where())
	}

	at("note", 0) // a value changed and not saved is the value; a quote is doubled; another schema is opened by its own
	tab.setEdit(editKey{tab.rowKey(0), "note"}, edit{val: db.Val{S: "O'Brien"}})
	feed(t, a, "gd")
	if p.Tabs[p.Cur].Name != "planner" || where() != `"title" = 'O''Brien'` {
		t.Errorf("another schema: %q, %q", p.Tabs[p.Cur].Name, where())
	}

	n := tabs()
	for _, c := range []struct {
		col  string
		row  int
		want string
	}{
		{"note", 2, "外键为空，没有被引用的行"},
		{"amount", 0, "这一列不是外键"},
		{"paid", 0, "doraemon 里没有表 public.nope"},
	} {
		at(c.col, c.row)
		a.toast = ""
		feed(t, a, "gd")
		if a.toast != c.want || tabs() != n {
			t.Errorf("%s: toast %q, %d tabs", c.col, a.toast, tabs())
		}
	}

	at("id", 0) // the menu has the item on a column of a key only
	has := func() bool {
		return slices.ContainsFunc(a.columnItems(tab, true), func(it menuItem) bool { return it.action == "grid.fk.follow" })
	}
	if !has() {
		t.Error("no item on a foreign key's column")
	}
	at("amount", 0)
	if has() {
		t.Error("an item on a column that is none")
	}

	// the keys not read yet: they are, then it goes on
	delete(c.keys, idOf(tab.table))
	at("id", 6)
	if cmd := a.followFK(); cmd == nil {
		t.Fatal("the keys are read first")
	}
	a.Update(objectsMsg{conn: c, table: tab.table, keys: []db.Key{{Cols: []string{"id"}, Ref: &db.Ref{Schema: "public", Table: "t_user", Cols: []string{"uid"}}}}, then: a.followFK})
	if where() != `"uid" = 7` {
		t.Errorf("after the keys: %q", where())
	}
}

// A page that comes brings the table's keys with it when the tree has not read
// them, once: it is what shows the menu's item (F22.13).
func TestPageReadsKeysOnce(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 5)
	c := tab.conn
	delete(c.keys, idOf(tab.table))
	if a.wantKeys(pageMsg{tab: tab, err: errors.New("boom")}) != nil {
		t.Error("a page that failed reads none")
	}
	cmd := a.wantKeys(pageMsg{tab: tab})
	if cmd == nil {
		t.Fatal("the keys are not read")
	}
	msg := cmd().(objectsMsg)
	msg.keys = []db.Key{{Cols: []string{"id"}, Ref: &db.Ref{Schema: "public", Table: "t_user", Cols: []string{"uid"}}}}
	a.Update(msg)
	if a.wantKeys(pageMsg{tab: tab}) != nil {
		t.Error("read again with the keys in")
	}
	has := func() bool {
		return slices.ContainsFunc(a.columnItems(tab, true), func(it menuItem) bool { return it.action == "grid.fk.follow" })
	}
	tab.col = 0
	if !has() {
		t.Error("no item once the keys are in")
	}
}

// gd on the tab of a connection that was disconnected dials it again, as a
// fetch does, and reads nothing on the Meta that is gone; so does the page that
// was on its way (F22.13, F22.12).
func TestFollowForeignKeyDisconnected(t *testing.T) {
	a, c, _ := withAlpha(t)
	a.openTable(c, c.Tables[1])
	tab := dataOf(a.focused())
	a.disconnect(c)
	if cmd := a.wantKeys(pageMsg{tab: tab}); cmd != nil {
		t.Error("a page after the disconnect reads keys")
	}
	tab.page.Cols = []db.Col{{Name: "id"}}
	tab.page.Rows = [][]db.Val{{{S: "1"}}}
	if cmd := a.followFK(); cmd == nil || c.State != connConnecting {
		t.Errorf("gd: state %v", c.State)
	}
}

// A database that was dialing when its connection was disconnected is not
// kept when the dial comes in (F22.12).
func TestDisconnectWhileDialing(t *testing.T) {
	a, c, alpha := withAlpha(t)
	alpha.State, alpha.Main, alpha.Meta = connIdle, nil, nil
	dial := a.connect(alpha)
	a.disconnect(c)
	answered(a, dial)
	if alpha.State != connIdle || len(alpha.Tables) != 0 {
		t.Errorf("state %v, %d tables", alpha.State, len(alpha.Tables))
	}
}

// A Redis connection disconnected has no key index, no scan and no keys in the
// palette; a PG one no columns or keys cached (F22.12).
func TestDisconnectDropsWhatIsKept(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.index = indexOfTree(c.scan)
	feed(t, a, "<C-p>:str")
	if len(rowsOf(a)) == 0 {
		t.Fatal("the palette has no key to begin with")
	}
	feed(t, a, "<Esc>")
	a.disconnect(c)
	if c.index != nil || c.scan != nil || c.State != connIdle {
		t.Errorf("index %v, scan %v, state %v", c.index, c.scan, c.State)
	}
	feed(t, a, "<C-p>:str")
	if rows := rowsOf(a); len(rows) != 0 {
		t.Errorf("the palette still lists %q", rows)
	}
	feed(t, a, "<Esc>")

	a = sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	pg := tab.conn
	pg.cols[idOf(tab.table)], pg.keys[idOf(tab.table)] = db.Columns{}, nil
	a.disconnect(pg)
	if len(pg.cols) != 0 || len(pg.keys) != 0 || len(pg.idx) != 0 {
		t.Errorf("cached: %d cols, %d keys", len(pg.cols), len(pg.keys))
	}
}
