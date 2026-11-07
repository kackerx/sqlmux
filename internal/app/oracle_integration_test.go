//go:build integration

package app

import (
	"context"
	"slices"
	"testing"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/oracle"
	"github.com/kackerx/sqlmux/internal/i18n"
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
