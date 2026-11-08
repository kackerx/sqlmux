//go:build integration

package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/oracle"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// An Oracle table in the app: the tree has its schemas, the tab its key, a
// save is one transaction, and one that fails is rolled back whole and said
// so; the manual transaction mode is there (§8.11, §10.3).
func TestOracleTable(t *testing.T) {
	name := oracle.OwnTable(t, "create table %s (id number(10) primary key, note varchar2(50), at date)",
		"insert into %s values (1, 'a', date '2026-01-01')", "insert into %s values (2, 'b', date '2026-01-02')")
	a := opened(t, config.Connection{Name: "ora", Engine: "oracle", DSN: oracle.IntegrationDSN(t)})
	ora := a.sess.Conns[0]
	if !slices.Contains(ora.Schemas, "SQLMUX") || ora.Databases != nil || ora.Schema != "SQLMUX" {
		t.Fatalf("schemas %v, databases %v, schema %q", ora.Schemas, ora.Databases, ora.Schema)
	}
	tab := tableOf(t, a, ora, "SQLMUX", name)
	if !slices.Equal(tab.cols.Key(), []string{"ID"}) || timeKind(tab.typeOf("AT")) != timeKind("timestamp") {
		t.Fatalf("key %v, at %q", tab.cols.Key(), tab.typeOf("AT"))
	}
	editCell(t, tab, 0, "NOTE", "from the app")
	editCell(t, tab, 1, "AT", "2026-03-04 05:06:07")
	settle(a, a.save())
	if tab.bar != nil || len(tab.edits) != 0 {
		t.Fatalf("save: bar %+v, edits %v", tab.bar, tab.edits)
	}
	r, err := ora.Meta.Query(context.Background(), "select note, (select at from "+name+" where id = 2) from "+name+" where id = 1")
	if err != nil || r.Rows[0][0].S != "from the app" || r.Rows[0][1].S != "2026-03-04 05:06:07" {
		t.Fatalf("on the server: %v %v", r.Rows, err)
	}
	// row 1 deleted, row 2 gone meanwhile: neither goes, as one transaction
	a.confirmDangerous = false
	tab.deleted = map[string]bool{tab.rowKey(0): true, tab.rowKey(1): true}
	if _, err := ora.Meta.Query(context.Background(), "delete from "+name+" where id = 2"); err != nil {
		t.Fatal(err)
	}
	settle(a, a.save())
	if tab.bar == nil || tab.bar.First.Tail != i18n.T("，已回滚") || len(tab.deleted) != 2 {
		t.Errorf("a failed save: bar %+v, marks %v", tab.bar, tab.deleted)
	}
	if r, _ := ora.Meta.Query(context.Background(), "select count(*) from "+name+" where id = 1"); r.Rows[0][0].S != "1" {
		t.Error("the delete that went is not rolled back")
	}
	if a.setTxMode(ora, true); !ora.Manual {
		t.Error("Oracle has the manual transaction mode")
	}
}

// The schemas Oracle keeps are out of the tree, but in the list to pick from,
// after the user's, off and marked as the system's; one picked has its tables
// read, and all the user's again with none of the system's is no line at all
// (F17.13).
func TestOraclePickSystemSchemas(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := opened(t, config.Connection{Name: "ora", Engine: "oracle", DSN: oracle.IntegrationDSN(t), Temp: true})
	ora := a.sess.Conns[0]
	if slices.Contains(ora.Schemas, "SYS") || !slices.Contains(ora.all.system, "DVSYS") {
		t.Fatalf("schemas %v, system %v", ora.Schemas, ora.all.system)
	}
	ns, _ := a.treeNodes()
	settle(a, a.openPick(ns[slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeConn })]))
	p := a.cols.pick
	dv := slices.Index(p.names, "DVSYS")
	if dv < p.system || p.on[dv] || p.note(dv) != i18n.T("系统") || p.names[0] == "DVSYS" {
		t.Fatalf("DVSYS in the list: at %d of %d user ones, on %v, note %q", dv, p.system, p.on[dv], p.note(dv))
	}
	if hints := a.pickView().Hints; len(hints) == 0 || hints[0].Action != "cols.filter" {
		t.Errorf("no search among the hints: %+v", hints)
	}
	p.on[slices.Index(p.names, "SYS")] = true // its dictionary's views are everyone's to read; DVSYS's tables are no one's but its own
	settle(a, a.applyPick(p))
	if !slices.Contains(ora.Schemas, "SYS") || !slices.ContainsFunc(ora.Tables, func(tb db.Table) bool { return tb.Schema == "SYS" && tb.Name == "ALL_USERS" }) {
		t.Fatalf("SYS picked: schemas %v", ora.Schemas)
	}
	if !slices.Contains(ora.Spec.Schemas, "SYS") || !slices.Contains(ora.Spec.Schemas, "SQLMUX") {
		t.Errorf("the setting %v", ora.Spec.Schemas)
	}
	ns, _ = a.treeNodes()
	settle(a, a.openPick(ns[slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeConn })]))
	p = a.cols.pick
	p.on[slices.Index(p.names, "SYS")] = false
	settle(a, a.applyPick(p))
	if ora.Spec.Schemas != nil || slices.Contains(ora.Schemas, "SYS") {
		t.Errorf("back to the user's: setting %v, schemas %v", ora.Spec.Schemas, ora.Schemas)
	}
}

// A table's properties, as DBeaver's tab has them: its columns, keys, foreign
// keys, indexes and DDL a section at a time, the chips over the filter
// switching them, the filter text over the rows, read only; the table's own
// rows are a tab apart, and a layout keeps no properties tab (§7.8「属性」).
func TestOracleProperties(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := opened(t, config.Connection{Name: "ora", Engine: "oracle", DSN: oracle.IntegrationDSN(t), Temp: true})
	ora := a.sess.Conns[0]
	order := ora.Tables[slices.IndexFunc(ora.Tables, func(tb db.Table) bool { return tb.Schema == "SQLMUX" && tb.Name == "T_ORDER" })]
	settle(a, a.openProps(ora, order))
	tab := dataOf(a.focused())
	if tab == nil || tab.props != "columns" || len(tab.page.Rows) != 9 {
		t.Fatalf("columns: %+v", tab)
	}
	row := func(name string) []db.Val {
		for _, r := range tab.page.Rows {
			if r[1].S == name {
				return r
			}
		}
		t.Fatalf("no row %s in %v", name, tab.page.Rows)
		return nil
	}
	if id := row("ID"); id[2].S != "NUMBER(10)" || id[3].S != "✓" || id[5].S != "PK" {
		t.Errorf("ID: %v", id)
	}
	if st := row("STATUS"); !strings.Contains(st[4].S, "pending") {
		t.Errorf("STATUS's default: %v", st)
	}
	if bar := a.queryBar(a.focused(), tab); len(bar.Info) != len(propsSections) || bar.Label != "FILTER" {
		t.Errorf("the section row: %+v, label %q", bar.Info, bar.Label)
	}
	if s, ok := tab.cellAtCursor(); !ok || s.why == "" {
		t.Errorf("a cell takes an edit: %+v", s)
	}
	settle(a, a.run("props.section foreign_keys", 0))
	if len(tab.page.Rows) != 1 || tab.page.Rows[0][1].S != "USER_ID" || tab.page.Rows[0][2].S != "T_USER" {
		t.Errorf("foreign keys: %v", tab.page.Rows)
	}
	settle(a, a.run("props.section ddl", 0))
	if len(tab.page.Rows) < 3 || !strings.Contains(tab.page.Rows[0][0].S+tab.page.Rows[1][0].S, `CREATE TABLE "SQLMUX"."T_ORDER"`) {
		t.Errorf("ddl: %v", tab.page.Rows[:min(3, len(tab.page.Rows))])
	}
	settle(a, a.run("props.section columns", 0))
	tab.where = ui.Input{Text: "amount"}
	settle(a, a.runWhere(tab))
	if len(tab.page.Rows) != 1 || tab.page.Rows[0][1].S != "AMOUNT" {
		t.Errorf("filtered: %v", tab.page.Rows)
	}
	if st := a.tableState(tab); len(st.History) != 0 || a.state.Tables[ora.tableKey(order)] != nil {
		t.Error("the filter went into the table's WHERE history")
	}
	settle(a, a.openTable(ora, order))
	if d := dataOf(a.focused()); d == nil || d == tab || d.props != "" || len(d.page.Rows) == 0 {
		t.Errorf("the table's rows are not a tab of their own: %+v", d)
	}
	for _, p := range a.win().Root.Leaves() {
		for _, lt := range paneLayout(p).Tabs {
			if lt.Kind == "table" && lt.Table == "T_ORDER" && lt.Where != "" {
				t.Errorf("a properties tab in the layout: %+v", lt)
			}
		}
	}
}
