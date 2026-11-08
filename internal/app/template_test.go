package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/mysql"
	"github.com/kackerx/sqlmux/internal/db/postgres"
	"github.com/kackerx/sqlmux/internal/db/sqlite"
	"github.com/kackerx/sqlmux/internal/editor"
)

// Each template, as each engine writes it; a column or an index the cursor
// is on is written in; a view has the rename only, and SQLite none (F14.4).
func TestAlterSQL(t *testing.T) {
	table, view, matview := db.Table{Schema: "s", Name: "t", Kind: "r"}, db.Table{Schema: "s", Name: "v", Kind: "v"}, db.Table{Schema: "s", Name: "m", Kind: "m"}
	pg, my, lite := &Conn{eng: postgres.Engine{}}, &Conn{eng: mysql.Engine{}}, &Conn{eng: sqlite.Engine{}}
	for _, tc := range []struct {
		c          *Conn
		kind       alterKind
		table      db.Table
		col, index string
		want       string
	}{
		{pg, addColumn, table, "", "", `alter table "s"."t" add column <name> <type>;`},
		{my, addColumn, table, "", "", "alter table `s`.`t` add column <name> <type>;"},
		{lite, addColumn, table, "", "", `alter table "s"."t" add column <name> <type>;`},
		{pg, addIndex, table, "", "", `create index <name> on "s"."t" (<column>);`},
		{pg, addIndex, table, "qty", "", `create index <name> on "s"."t" ("qty");`},
		{my, addIndex, table, "qty", "", "create index <name> on `s`.`t` (`qty`);"},
		{lite, addIndex, table, "qty", "", `create index <name> on "t" ("qty");`}, // no schema before the table of an index
		{pg, renameTbl, table, "", "", `alter table "s"."t" rename to <new_name>;`},
		{pg, renameTbl, view, "", "", `alter view "s"."v" rename to <new_name>;`},
		{pg, renameTbl, matview, "", "", `alter materialized view "s"."m" rename to <new_name>;`},
		{my, renameTbl, table, "", "", "rename table `s`.`t` to `s`.<new_name>;"},
		{my, renameTbl, view, "", "", "rename table `s`.`v` to `s`.<new_name>;"},
		{lite, renameTbl, table, "", "", `alter table "s"."t" rename to <new_name>;`},
		{lite, renameTbl, view, "", "", ""},
		{pg, dropColumn, table, "", "", `alter table "s"."t" drop column <column>;`},
		{pg, dropColumn, table, "qty", "", `alter table "s"."t" drop column "qty";`},
		{my, dropColumn, table, "qty", "", "alter table `s`.`t` drop column `qty`;"},
		{pg, dropIndex, table, "", "", `drop index "s".<index>;`},
		{pg, dropIndex, table, "", "t_idx", `drop index "s"."t_idx";`},
		{my, dropIndex, table, "", "t_idx", "drop index `t_idx` on `s`.`t`;"},
		{lite, dropIndex, table, "", "t_idx", `drop index "t_idx";`},
		{pg, addColumn, view, "", "", ""}, // a view has no column to add
		{pg, dropIndex, view, "", "", ""},
	} {
		got, why := alterSQL(tc.c, tc.kind, tc.table, tc.col, tc.index)
		if got != tc.want || (got == "") != (why != "") {
			t.Errorf("%v %s %s: %q (%s), want %q", tc.c.eng.Dialect(), tc.kind, tc.table.Name, got, why, tc.want)
		}
	}
}

// A template opens in a new query on the table's connection, the first
// placeholder selected, so c writes over it; from the tree's table node, its
// column node, or the table tab in the grid, by the palette as by the menu (F14.4).
func TestAlterTemplates(t *testing.T) {
	a := inObjects(t)
	treeTo(t, a, "t_order")
	a.run("table.alter.add_column", 0)
	q := a.focusedQuery()
	if q == nil || text(q) != `alter table "public"."t_order" add column <name> <type>;` || q.schema != "public" || q.conn != a.sess.Conns[0] {
		t.Fatalf("add column: %v", q)
	}
	from, to, ok := q.ed.Selection()
	if !ok || q.ed.Mode() != editor.Select || from != (editor.Pos{Line: 0, Col: 42}) || to != (editor.Pos{Line: 0, Col: 47}) || !strings.Contains(statusRow(a), "SELECT") { // <name>
		t.Fatalf("selection %v..%v %v, mode %v", from, to, ok, q.ed.Mode())
	}
	feed(t, a, "id")
	if want := `alter table "public"."t_order" add column id <type>;`; text(q) != want || q.ed.Mode() != editor.Insert {
		t.Errorf("what is typed writes over it: %q, mode %v", text(q), q.ed.Mode())
	}
	feed(t, a, "<Tab>bigint<Tab>") // the next placeholder, selected; the last Tab ends the snippet, and types nothing
	if want := `alter table "public"."t_order" add column id bigint;`; text(q) != want || q.ed.Mode() != editor.Insert {
		t.Errorf("Tab goes on: %q, mode %v", text(q), q.ed.Mode())
	}
	feed(t, a, "<Esc>")
	// on a column: written in; nothing left to select
	a.win().focus(0)
	treeTo(t, a, "user_id")
	a.run("table.alter.drop_column", 0)
	if q = a.focusedQuery(); q == nil || text(q) != `alter table "public"."t_order" drop column "user_id";` || q.ed.Mode() != editor.Normal {
		t.Errorf("drop column: %v", text(q))
	}
	// by the palette
	a.win().focus(0)
	treeTo(t, a, "t_order")
	feed(t, a, "<C-p>加索引<CR>")
	if q = a.focusedQuery(); q == nil || text(q) != `create index <name> on "public"."t_order" (<column>);` {
		t.Errorf("by the palette: %v", q)
	}
	a.win().focus(0)
	treeTo(t, a, "t_order")
	feed(t, a, "<C-p>改名表<CR>") // the palette says 改名表…, apart from a query's and a session's renames
	if q = a.focusedQuery(); q == nil || text(q) != `alter table "public"."t_order" rename to <new_name>;` {
		t.Errorf("rename by the palette: %v", q)
	}
	// from the table tab: the column under the cursor
	order := tableNamed(a, "t_order")
	a.openTable(a.sess.Conns[0], order)
	d := dataOf(a.focused())
	d.page = db.Result{Cols: []db.Col{{Name: "id"}, {Name: "qty"}}, Rows: [][]db.Val{{{S: "1"}, {S: "2"}}}}
	a.run("grid.right", 0)
	a.run("table.alter.add_index", 0)
	if q = a.focusedQuery(); q == nil || text(q) != `create index <name> on "public"."t_order" ("qty");` {
		t.Errorf("from the grid: %v", q)
	}
	// nothing under the cursor
	b := inTree(45)
	b.win().focus(2)
	if b.run("table.alter.rename", 0); !strings.Contains(b.toast, "先选一张表") {
		t.Errorf("no table: toast %q", b.toast)
	}
}

// The modeless editor selects the placeholder too: what is typed takes its place (F14.4).
func TestAlterTemplatePlain(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sizedWith(160, 45, userConfig(t, "vim = false", ""))
	a.win().focus(0)
	treeTo(t, a, "t_order")
	a.run("table.alter.rename", 0)
	q := a.focusedQuery()
	if q == nil || text(q) != `alter table "public"."t_order" rename to <new_name>;` {
		t.Fatalf("%q", text(q))
	}
	if from, to, ok := q.ed.SelectionSpan(); !ok || from != (editor.Pos{Line: 0, Col: 41}) || to != (editor.Pos{Line: 0, Col: 51}) {
		t.Fatalf("selection %v..%v %v", from, to, ok)
	}
	feed(t, a, "t_orders")
	if want := `alter table "public"."t_order" rename to t_orders;`; text(q) != want {
		t.Errorf("typing writes over it: %q", text(q))
	}
	a.win().focus(0)
	treeTo(t, a, "t_order")
	a.run("table.alter.add_index", 0) // two placeholders: Tab, S-Tab and the last Tab
	q = a.focusedQuery()
	feed(t, a, "idx<Tab>qty<S-Tab>")
	if from, to, ok := q.ed.SelectionSpan(); !ok || text(q) != `create index idx on "public"."t_order" (qty);` || text(q)[from.Col:to.Col] != "idx" {
		t.Errorf("S-Tab back to the first: %q %v %v", text(q), from, to)
	}
}

// Each template selects its first placeholder, in the vim and in the editor
// without modes, and the ones with a column or an index of the cursor's written
// in have none; on an index node or a column node the name is written in; on a
// node that is no table the template says to select one (F14.4).
func TestAlterTemplateSelection(t *testing.T) {
	for _, plain := range []bool{false, true} {
		cfg := ""
		if plain {
			cfg = "vim = false"
		}
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		a := sizedWith(160, 45, userConfig(t, cfg, ""))
		for _, kind := range []alterKind{addColumn, addIndex, renameTbl, dropColumn, dropIndex} {
			a.win().focus(0)
			treeTo(t, a, "t_order")
			a.run("table.alter."+string(kind), 0)
			q := a.focusedQuery()
			sql := text(q)
			loc := placeholder.FindStringIndex(sql)
			if loc == nil {
				t.Fatalf("plain %v %s: no placeholder in %q", plain, kind, sql)
			}
			var from, to editor.Pos
			var ok bool
			if plain {
				from, to, ok = q.ed.SelectionSpan()
			} else if from, to, ok = q.ed.Selection(); ok {
				to.Col++ // the last character in
			}
			if !ok || from != (editor.Pos{Col: loc[0]}) || to != (editor.Pos{Col: loc[1]}) {
				t.Errorf("plain %v %s: %q selects %v..%v %v, want %v", plain, kind, sql, from, to, ok, loc)
			}
		}
	}
	a := inObjects(t)
	treeTo(t, a, "Indexes (2)")
	feed(t, a, "l")
	treeTo(t, a, "t_order_user (user_id, created_at)")
	a.run("table.alter.drop_index", 0)
	if q := a.focusedQuery(); q == nil || text(q) != `drop index "public"."t_order_user";` {
		t.Errorf("on an index: %v", text(q))
	}
	a.win().focus(0)
	ns, _ := a.treeNodes()
	for _, kind := range []nodeKind{nodeConn, nodeSchema, nodeGroup} {
		i := slices.IndexFunc(ns, func(n node) bool { return n.kind == kind })
		a.treeGo(i)
		a.toast = ""
		if cmd := a.alter(addColumn); cmd == nil || !strings.Contains(a.toast, "先选一张表") {
			t.Errorf("node kind %v: toast %q", kind, a.toast)
		}
	}
	a.openTable(a.sess.Conns[0], tableNamed(a, "t_order")) // a table tab, the connection lost after
	a.sess.Conns[0].State = connFailed
	a.toast = ""
	if a.alter(addColumn); !strings.Contains(a.toast, "连接还没连上") {
		t.Errorf("not connected: toast %q", a.toast)
	}
}

// A table's menu has the templates, a view's the rename only; SQLite's has no view's (F14.4).
func TestAlterMenu(t *testing.T) {
	a := inTree(45)
	labels := func(n node) string {
		var out []string
		for _, it := range a.menuItems(n) {
			out = append(out, it.label)
		}
		return strings.Join(out, "|")
	}
	c := a.sess.Conns[0]
	table := node{kind: nodeTable, conn: c, table: db.Table{Schema: "public", Name: "t", Kind: "r"}}
	view := node{kind: nodeTable, conn: c, table: db.Table{Schema: "public", Name: "v", Kind: "v"}}
	if got, want := labels(table), "打开|在新 tab 打开|属性|删除表…|加列…|加索引…|改名…|删除列…|删除索引…"; got != want {
		t.Errorf("table: %s", got)
	}
	if got, want := labels(view), "打开|在新 tab 打开|属性|删除视图…|改名…"; got != want {
		t.Errorf("view: %s", got)
	}
	lite := &Conn{eng: sqlite.Engine{}}
	view.conn = lite
	if got, want := labels(view), "打开|在新 tab 打开|属性|删除视图…"; got != want {
		t.Errorf("a SQLite view: %s", got)
	}
}

// The tabstops still to come are on the visual color, the one being filled in
// is the selection; the mode block says SELECT in VISUAL's color; Tab belongs to
// the candidates when they are up, and to the snippet when they are not (F15.8).
func TestSnippetInTheView(t *testing.T) {
	a := inObjects(t)
	treeTo(t, a, "t_order")
	a.run("table.alter.add_column", 0)
	q := a.focusedQuery()
	f := a.render()
	area := bodyRect(a.layout()[a.focused().ID])
	name, typ := textCells(f, area, "<name>"), textCells(f, area, "<type>")
	if len(name) != 1 || len(typ) != 1 {
		t.Fatalf("the placeholders: %v %v", name, typ)
	}
	th := a.theme
	if bg := f.Buf.CellAt(typ[0][0], typ[0][1]).Style.Bg; bg != th.Visual {
		t.Errorf("a tabstop to come: %v, want the visual color", bg)
	}
	if bg := f.Buf.CellAt(name[0][0], name[0][1]).Style.Bg; bg != th.Visual {
		t.Errorf("the selected one: %v", bg)
	}
	if cell := f.Buf.CellAt(a.w-2, a.h-1); cell.Style.Bg != th.Keyword || !strings.Contains(statusRow(a), "SELECT") {
		t.Errorf("the mode block: %q on %v", statusRow(a), cell.Style.Bg)
	}
	feed(t, a, "t") // what is typed over a selection completes, as in INSERT
	if q.comp == nil {
		t.Fatal("no candidates after typing over the tabstop")
	}
	feed(t, a, "_o")
	sel := q.comp.sel
	feed(t, a, "<Tab>")
	if q.comp == nil || q.comp.sel == sel || !strings.Contains(text(q), "add column t_o <type>") {
		t.Errorf("Tab with candidates up is theirs: sel %d -> %d, %q", sel, q.comp.sel, text(q))
	}
	feed(t, a, "<Esc>") // closes them and leaves INSERT; the snippet is still there
	if q.comp != nil {
		t.Fatal("esc leaves the candidates up")
	}
	f = a.render()
	typ = textCells(f, area, "<type>")
	if len(typ) != 1 || f.Buf.CellAt(typ[0][0], typ[0][1]).Style.Bg != th.Visual {
		t.Errorf("after esc the tabstop to come is still marked: %v", typ)
	}
	feed(t, a, "a<Tab>")
	if from, to, ok := q.ed.Selection(); !ok || q.ed.Mode() != editor.Select || text(q)[from.Col:to.Col+1] != "<type>" {
		t.Errorf("Tab with no candidates: %v %v %v", from, to, ok)
	}
}

// A key the query's scope binds (↵ runs) is text over a selected
// tabstop: SELECT has INSERT's keys, not NORMAL's and VISUAL's (F15.8).
func TestSnippetTypesWhatTheQueryScopeBinds(t *testing.T) {
	a := inObjects(t)
	treeTo(t, a, "t_order")
	a.run("table.alter.add_column", 0)
	q := a.focusedQuery()
	feed(t, a, "<CR>") // query.run in NORMAL and VISUAL: here a line break
	if lines := q.ed.Lines(); len(lines) != 2 || lines[1] != "<type>;" || q.ed.Mode() != editor.Insert {
		t.Errorf("↵ over the tabstop: %q, mode %v", lines, q.ed.Mode())
	}
}
