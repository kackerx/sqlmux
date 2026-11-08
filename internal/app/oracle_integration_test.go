//go:build integration

package app

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/oracle"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
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

// A table's properties, as DBeaver has them: the table's own tab shows them
// or its rows, switched by gi and the first chip, the rows' changes kept; its
// columns, keys, foreign keys, indexes a section at a time, the filter text
// over them; its DDL a read-only text in SQL's colors, with no filter over it,
// esc back to the rows. The tree has the table's size (§7.8「属性」).
func TestOracleProperties(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := opened(t, config.Connection{Name: "ora", Engine: "oracle", DSN: oracle.IntegrationDSN(t), Temp: true})
	ora := a.sess.Conns[0]
	order := ora.Tables[slices.IndexFunc(ora.Tables, func(tb db.Table) bool { return tb.Schema == "SQLMUX" && tb.Name == "T_ORDER" })]
	if order.Bytes <= 0 {
		t.Errorf("T_ORDER's size: %d", order.Bytes)
	}
	settle(a, a.openTable(ora, order))
	p := a.focused()
	rows, tabs := dataOf(p), len(p.Tabs)
	editCell(t, rows, 0, "NOTE", "kept")
	settle(a, a.openProps(ora, order))
	tab := dataOf(p)
	if len(p.Tabs) != tabs || tab == rows || tab.props != "columns" || tab.other != rows || p.tab().table() != rows || len(tab.page.Rows) != 9 {
		t.Fatalf("the properties in the table's tab: %d tabs, %+v", len(p.Tabs), tab)
	}
	if id := tab.page.Rows[0]; id[1].S != "ID" || id[2].S != "NUMBER(10)" || id[3].S != "✓" || id[5].S != "PK" {
		t.Errorf("ID: %v", id)
	}
	if bar := a.queryBar(p, tab); len(bar.Info) != len(propsSections)+1 || bar.Info[0].Action != "table.properties" || bar.Label != "FILTER" {
		t.Errorf("the section row: %+v, label %q", bar.Info, bar.Label)
	}
	if unsaved(p) != 1 {
		t.Error("the rows' change is not counted with the properties shown")
	}
	settle(a, a.run("props.section foreign_keys", 0))
	if len(tab.page.Rows) != 1 || tab.page.Rows[0][1].S != "USER_ID" || tab.page.Rows[0][2].S != "T_USER" {
		t.Errorf("foreign keys: %v", tab.page.Rows)
	}
	settle(a, a.run("props.section ddl", 0))
	jv := a.jsonSync(p)
	if jv == nil || !strings.HasPrefix(jv.text, `CREATE TABLE "SQLMUX"."T_ORDER"`) || !jv.ed.ReadOnly || tab.barRows() != ui.InfoRows {
		t.Fatalf("ddl: view %v, bar %d", jv, tab.barRows())
	}
	f := a.render()
	text := gridRect(a.layout()[p.ID], tab)
	if fg := f.Buf.CellAt(text.Min.X+5, text.Min.Y).Style.Fg; fg != a.theme.Keyword { // CREATE, past the gutter
		t.Errorf("CREATE is %v, not a keyword's color", fg)
	}
	settle(a, a.press(keymap.Esc))
	if dataOf(p) != rows || rows.edits == nil || len(rows.edits) != 1 {
		t.Errorf("esc: back to the rows, their change kept: %+v", dataOf(p))
	}
	settle(a, a.run("table.properties", 0)) // gi: the properties again, as they were left
	if dataOf(p) != tab || tab.props != "ddl" {
		t.Errorf("gi: %+v", dataOf(p))
	}
	settle(a, a.run("table.properties", 0))
	settle(a, a.run("props.section columns", 0))
	if dataOf(p) != rows {
		t.Error("gi twice is the rows")
	}
	for _, lt := range paneLayout(p).Tabs {
		if lt.Table == "T_ORDER" && lt.Kind != "table" {
			t.Errorf("the layout: %+v", lt)
		}
	}
}

// All of an Oracle table to Excel: every row, NUMBER(10,2) as numbers, a CLOB
// as text, the file one Excel and `file` take for a workbook.
func TestOracleExportExcel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := inDir(t)
	a := opened(t, config.Connection{Name: "ora", Engine: "oracle", DSN: oracle.IntegrationDSN(t), Temp: true})
	ora := a.sess.Conns[0]
	settle(a, a.openTable(ora, ora.Tables[slices.IndexFunc(ora.Tables, func(tb db.Table) bool { return tb.Schema == "SQLMUX" && tb.Name == "T_ORDER" })]))
	exportAt(t, a, exportXLSXAll)
	path := filepath.Join(dir, "T_ORDER.xlsx")
	sheet, rows, _ := readXLSX(t, path)
	if sheet != "T_ORDER" || len(rows) != 5201 {
		t.Fatalf("sheet %q, %d rows, toast %q", sheet, len(rows), a.toast)
	}
	if amount := rows[1][3]; amount.V != "0.25" || amount.T != "" {
		t.Errorf("AMOUNT: %+v", amount)
	}
	if out, err := exec.Command("file", "-b", path).Output(); err == nil && !strings.Contains(string(out), "Excel") {
		t.Errorf("file says %s", out)
	}
}
