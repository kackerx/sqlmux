package app

import (
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/postgres"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// A connection with no SQL engine, as a Redis one has (F16.1 f): its tree, a
// query on it and a table pane over a Source draw and take keys, and what is
// SQL's says it is not there, with no word to the engine that is not (§5).
func TestNoEngineConnection(t *testing.T) {
	a := wide(160, 45)
	c := a.sess.Conns[0]
	c.Spec.Engine, c.drv, c.eng = "redis", kvDriver{}, nil
	c.building = &keyIndex{} // typing in a console starts no index on the fake's server (F20.11)
	a.render()
	says := func(keys, toast string) {
		t.Helper()
		a.toast = ""
		feed(t, a, keys)
		a.render()
		if toast != "" && a.toast != i18n.T(toast) {
			t.Errorf("%s: toast %q, want %q", keys, a.toast, i18n.T(toast))
		}
	}
	// a query
	a.run("query.new", 0)
	for _, keys := range []string{"iGET key<Esc>", "<C-n>", "<Esc>", "ione<Space>two<C-n><Esc>"} {
		says(keys, "")
	}
	says("gqq", "这个连接的命令没有格式化")
	says("<CR>", "")                                 // it runs (F16.4): its connection is not up here, which dials
	says("<C-p>;select 1<CR>", "还在读 doraemon 的命令列表") // a command now (F17.2): this one has not the server's list
	says("<Esc>", "")
	for _, keys := range []string{"<C-p>", "<Esc>", "<C-p>k<Tab><CR>", "<Esc>", "<C-p>;<C-n>", "<Esc>", "gc", "<Esc>"} {
		says(keys, "")
	}
	// the tree
	says("<C-h>", "")
	for _, keys := range []string{"j", "k", "l", "h", "<CR>", "dd", "<Esc>", "t", "/", "x", "<Esc>"} {
		says(keys, "")
	}
	// a table pane: a key tab (F16.3) is made with its source, here the tab is made as a table's
	c.eng = postgres.Engine{}
	a.openTable(c, db.Table{Schema: "0", Name: "k"})
	c.eng = nil
	tab := dataOf(a.focused())
	tab.src = &fakeSource{n: 3}
	tab.limit = 3
	a.Update(a.fetch(tab, true)())
	a.render()
	says("*", "这一列的类型不支持按值过滤")
	says("yi", "只有表里的行能复制为 INSERT")
	for _, keys := range []string{"j", "l", "gj", "/", "<Esc>", "yy", "i<Esc>", "gS", ":", "<Esc>", "/ab", "<C-n>", "<C-r>", "<Esc><Esc>", "gD", "<Esc>", "ge", "<Esc>"} {
		says(keys, "")
	}
}
