package app

import (
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
