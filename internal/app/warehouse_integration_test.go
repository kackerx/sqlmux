//go:build integration

package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/clickhouse"
	"github.com/kackerx/sqlmux/internal/db/trino"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// tableOf opens table name of c's catalog in the focused pane and is its tab.
func tableOf(t *testing.T, a *App, c *Conn, schema, name string) *dataTab {
	t.Helper()
	i := slices.IndexFunc(c.Tables, func(tb db.Table) bool { return tb.Schema == schema && tb.Name == name })
	if i < 0 {
		t.Fatalf("no %s.%s in %v", schema, name, c.Tables)
	}
	settle(a, a.openTable(c, c.Tables[i]))
	tab := dataOf(a.focused())
	if tab == nil || len(tab.page.Rows) == 0 {
		t.Fatalf("%s.%s did not open: %+v", schema, name, tab)
	}
	return tab
}

// editCell puts val in the cell of column col on the tab's row r, as typing it would.
func editCell(t *testing.T, tab *dataTab, r int, col, val string) {
	t.Helper()
	i := slices.IndexFunc(tab.page.Cols, func(c db.Col) bool { return c.Name == col })
	if i < 0 {
		t.Fatalf("no column %s", col)
	}
	if tab.edits == nil {
		tab.edits = map[editKey]edit{}
	}
	tab.edits[editKey{tab.rowKey(r), col}] = edit{val: db.Val{S: val}, orig: tab.page.Rows[r][i]}
}

// A ClickHouse table in the app: the tree has its databases, the tab its key, a
// cell's change is a mutation, and a save that fails halfway says what was
// written and keeps only what was not (§8.9, §10.3).
func TestClickHouseTable(t *testing.T) {
	dsn := clickhouse.OwnDB(t)
	a := opened(t, config.Connection{Name: "ch", Engine: "clickhouse", DSN: dsn})
	ch := a.sess.Conns[0]
	if !slices.Contains(ch.Schemas, "sqlmux") || ch.Databases != nil {
		t.Fatalf("schemas %v, databases %v", ch.Schemas, ch.Databases)
	}
	own := strings.TrimPrefix(dsn[strings.LastIndex(dsn, "/"):], "/")
	tab := tableOf(t, a, ch, own, "t_order")
	if !slices.Equal(tab.cols.Key(), []string{"id"}) {
		t.Fatalf("key %v", tab.cols.Key())
	}
	editCell(t, tab, 0, "note", "from the app")
	settle(a, a.save())
	if tab.bar != nil || len(tab.edits) != 0 {
		t.Fatalf("save: bar %+v, edits %v", tab.bar, tab.edits)
	}
	r, err := ch.Meta.Query(context.Background(), "select note from `"+own+"`.t_order where id = 1")
	if err != nil || r.Rows[0][0].S != "from the app" {
		t.Fatalf("on the server: %v %v", r.Rows, err)
	}
	// rows 2 and 3 deleted, 3 gone meanwhile: 2 goes, 3 is said, its mark stays
	tab.deleted = map[string]bool{tab.rowKey(1): true, tab.rowKey(2): true}
	gone := tab.rowKey(2)
	a.confirmDangerous = false // the question before a delete is not what this is about
	if _, err := ch.Main.Query(context.Background(), "alter table `"+own+"`.t_order delete where id = 3"); err != nil {
		t.Fatal(err)
	}
	settle(a, a.save())
	if tab.bar == nil || tab.bar.First.Tail != i18n.T("，前 %d 条已写入，不能回滚", 1) || !strings.Contains(tab.bar.First.Head, "id = 3") {
		t.Errorf("a save that failed at its second: bar %+v", tab.bar)
	}
	if len(tab.deleted) != 1 || !tab.deleted[gone] {
		t.Errorf("what was written is still marked: %v", tab.deleted)
	}
	if cmd := a.setTxMode(ch, true); ch.Manual || cmd == nil {
		t.Error("ClickHouse took the manual transaction mode")
	}
	if got := colType(tab.typeOf("amount")); got != colType("numeric") {
		t.Errorf("amount's class %v", got)
	}
}

// A Trino connection in the app: its catalogs are the databases, each dialed
// as a child; a table of one reads, and a change saves through the connector
// (§8.10, §10.3).
func TestTrinoCatalogs(t *testing.T) {
	a := opened(t, config.Connection{Name: "tr", Engine: "trino", DSN: trino.IntegrationDSN(t)})
	tr := a.sess.Conns[0]
	if tr.DB != "postgresql" || !slices.Contains(tr.Databases, "tpch") || !slices.Contains(tr.Schemas, "public") {
		t.Fatalf("db %q, catalogs %v, schemas %v", tr.DB, tr.Databases, tr.Schemas)
	}
	tpch := tr.inDB("tpch")
	d, err := dial(context.Background(), tpch.Spec)
	if err != nil {
		t.Fatal(err)
	}
	tpch.take(d)
	defer d.discard()()
	settle(a, a.loadCatalog(tpch))
	if tpch.DB != "tpch" || !slices.Contains(tpch.Schemas, "tiny") {
		t.Fatalf("tpch: %q %v", tpch.DB, tpch.Schemas)
	}
	nation := tableOf(t, a, tpch, "tiny", "nation")
	if len(nation.page.Rows) != 25 || nation.cols.ReadOnly != "" && nation.cols.Key() != nil {
		t.Errorf("nation: %d rows, key %v", len(nation.page.Rows), nation.cols.Key())
	}
	schema := trino.OwnSchema(t)
	settle(a, a.loadCatalog(tr)) // the schema made since
	items := tableOf(t, a, tr, schema, "t_item")
	editCell(t, items, 1, "note", "via trino")
	settle(a, a.save())
	if items.bar != nil || len(items.edits) != 0 {
		t.Fatalf("save: bar %+v", items.bar)
	}
	r, err := tr.Meta.Query(context.Background(), `select note from "`+schema+`".t_item where id = 2`)
	if err != nil || r.Rows[0][0].S != "via trino" {
		t.Errorf("on the server: %v %v", r.Rows, err)
	}
}
