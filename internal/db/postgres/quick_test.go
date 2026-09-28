//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"sqlmux/internal/db"
)

func TestQuick(t *testing.T) {
	w := db.NewWorker(connect(t, true))
	ctx := context.Background()
	// the server stops at the 101st row: working out row 5000 would divide by zero
	r, err := Quick(ctx, w, "select i, 1/(i-5000) from generate_series(1, 10000) i", "public", 100)
	if err != nil || len(r.Rows) != 100 || !r.Truncated || r.Rows[99][0].S != "100" {
		t.Fatalf("select-like: %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
	if r, err = Quick(ctx, w, "select * from t_order", "public", 100); err != nil || len(r.Rows) != 100 || !r.Truncated {
		t.Errorf("select * from t_order: %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
	if r, err = Quick(ctx, w, "  -- a comment\n(table t_user)", "public", 100); err != nil || len(r.Rows) != 50 || r.Truncated {
		t.Errorf("(table t_user): %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
	// the rest run as they are
	if r, err = Quick(ctx, w, "show search_path", "agentable", 100); err != nil || r.Rows[0][0].S != `"agentable", "$user", public` {
		t.Errorf("show search_path: %v %v", r.Rows, err)
	}
	if r, err = Quick(ctx, w, "explain select * from t_order", "public", 100); err != nil || len(r.Rows) == 0 {
		t.Errorf("explain: %v %v", r.Rows, err)
	}
	// the tree's schema is searched first
	if r, err = Quick(ctx, w, "select * from agent", "agentable", 100); err != nil || len(r.Rows) == 0 {
		t.Errorf("agentable's agent: %v %v", r.Rows, err)
	}
	// one statement at a time, whichever way it runs
	for _, sql := range []string{"select 1; delete from t_order", "show search_path; delete from t_order"} {
		if _, err := Quick(ctx, w, sql, "public", 100); sqlState(err) != "42601" {
			t.Errorf("%q: %v", sql, err)
		}
	}
	// everything is rolled back: the search path, and what the statement set
	if _, err := Quick(ctx, w, "set application_name = 'quick'", "agentable", 100); err != nil {
		t.Fatal(err)
	}
	if r, _ := w.Query(ctx, "select current_setting('application_name'), current_setting('search_path')"); r.Rows[0][0].S != "sqlmux" || r.Rows[0][1].S != `"$user", public` {
		t.Errorf("after: %v", r.Rows)
	}
	// cancelled, the connection goes on, out of the transaction
	time.AfterFunc(200*time.Millisecond, w.Cancel)
	if _, err := Quick(ctx, w, "select pg_sleep(10)", "public", 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if r, err = Quick(ctx, w, "select 1", "public", 100); err != nil || r.Rows[0][0].S != "1" {
		t.Errorf("after the cancel: %v %v", r.Rows, err)
	}
}

// A write is refused by the read-only transaction and changes nothing. It
// runs on a database of its own (AGENTS.md「集成测试环境」).
func TestQuickRefusesWrites(t *testing.T) {
	ctx := context.Background()
	dsn := ownDB(t)
	own, err := Connect(ctx, dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer own.Close()
	if _, err := own.Exec(ctx, "create table t (x int); insert into t select generate_series(1, 10)", 0); err != nil {
		t.Fatal(err)
	}
	meta, err := Connect(ctx, dsn, "", true)
	if err != nil {
		t.Fatal(err)
	}
	w := db.NewWorker(meta)
	defer w.Close()
	if _, err := Quick(ctx, w, "delete from t", "public", 100); sqlState(err) != "25006" {
		t.Errorf("delete: %v", err)
	}
	if _, err := Quick(ctx, w, "with d as (delete from t returning *) select * from d", "public", 100); err == nil {
		t.Error("a delete in a WITH ran")
	}
	if r, _ := own.Query(ctx, "select count(*) from t"); r.Rows[0][0].S != "10" {
		t.Errorf("rows left: %v", r.Rows)
	}
}
