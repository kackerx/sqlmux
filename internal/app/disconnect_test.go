package app

import (
	"slices"
	"strings"
	"testing"
)

// conn.disconnect on a connection node (F22.12): the connection and its
// databases are idle, with no catalog, nothing of it in the tree or the
// palette; its tab stays, and the next fetch dials it again. A transaction
// going is asked about first.
func TestDisconnect(t *testing.T) {
	a, c, alpha := withAlpha(t)
	a.openTable(c, c.Tables[1])
	tab := dataOf(a.focused())
	a.win().focus(0)
	item := func() menuItem {
		t.Helper()
		a.menu = nil
		rightClickNode(t, a, "doraemon")
		i := slices.IndexFunc(a.menu.items, func(it menuItem) bool { return it.action == "conn.disconnect" })
		if i < 0 {
			t.Fatalf("no disconnect in %q", menuLabels(a))
		}
		return a.menu.items[i]
	}
	run := item().run
	if run == nil {
		t.Fatal("dim on a connection that is up")
	}

	a.menu = nil
	c.tx = 'T'
	run()
	if a.confirm == nil || !strings.Contains(a.confirm.text, "doraemon 有未提交的事务，断开会回滚。") || c.State != connReady {
		t.Fatalf("asked %+v, state %v", a.confirm, c.State)
	}
	feed(t, a, "n")
	if c.State != connReady {
		t.Fatal("no is no")
	}

	c.tx = 'I'
	run()
	for _, o := range []*Conn{c, alpha} {
		if o.State != connIdle || o.Main != nil || o.Meta != nil || len(o.Tables) != 0 || len(o.Schemas) != 0 {
			t.Errorf("%s: state %v, catalog %d tables", o.label(), o.State, len(o.Tables))
		}
	}
	a.Update(catalogMsg{conn: c, schemas: []string{"public"}, tables: alpha.Tables}) // one that was on its way
	if len(c.Schemas) != 0 {
		t.Error("a catalog read before the disconnect came in after it")
	}
	if got := treeTexts(a); strings.Contains(got, "Tables") || !strings.Contains(got, "▸ doraemon") {
		t.Errorf("the tree:\n%s", got)
	}
	if !slices.ContainsFunc(a.sess.panes(), func(p *Pane) bool { return dataOf(p) == tab }) {
		t.Error("the tab went with it")
	}
	feed(t, a, "<C-p>@t_")
	if rows := rowsOf(a); len(rows) != 0 {
		t.Errorf("the palette lists %q", rows)
	}
	feed(t, a, "<Esc>")
	if item().run != nil {
		t.Error("not dim on a connection that is not up")
	}
	a.menu = nil
	feed(t, a, "<Esc>")
	a.run("conn.disconnect", 0)
	if a.toast != "doraemon 没有连上" {
		t.Errorf("toast %q", a.toast)
	}

	answered(a, a.fetch(tab, true)) // the tab asks: the connection is dialed again
	if c.State != connReady || len(c.Tables) == 0 {
		t.Errorf("after a fetch: state %v, %d tables", c.State, len(c.Tables))
	}
}
