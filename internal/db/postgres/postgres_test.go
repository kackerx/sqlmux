//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"sqlmux/internal/db"
)

// testDSN is the seeded database from docker-compose.yml; unset, the tests skip.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMUX_TEST_PG")
	if dsn == "" {
		t.Skip("SQLMUX_TEST_PG is not set")
	}
	return dsn
}

func connect(t *testing.T, readOnly bool) *Conn {
	t.Helper()
	c, err := Connect(context.Background(), testDSN(t), "", readOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestConnectSetsStartupParams(t *testing.T) {
	c := connect(t, false)
	r, err := c.Query(context.Background(),
		"select current_setting('application_name'), current_setting('DateStyle'), current_setting('default_transaction_read_only')")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Rows[0]; got[0].S != "sqlmux" || got[1].S != "ISO, YMD" || got[2].S != "off" {
		t.Fatalf("startup params %v", got)
	}
	if c.SearchPath != `"$user", public` {
		t.Fatalf("search_path %q", c.SearchPath)
	}
	if cfg, _ := pgconn.ParseConfig(testDSN(t)); c.Addr != fmt.Sprintf("%s@%s:%d", cfg.User, cfg.Host, cfg.Port) {
		t.Fatalf("addr %q", c.Addr)
	}
}

// Every value comes back as the server's text (§8.1): enums and json as
// written, timestamptz in ISO order, NULL apart from "".
func TestQueryReturnsText(t *testing.T) {
	c := connect(t, false)
	r, err := c.Query(context.Background(),
		"select status, paid, meta, raw, note, created_at, amount from t_order where id = $1", db.Val{S: "3"})
	if err != nil {
		t.Fatal(err)
	}
	want := []db.Val{
		{S: "failed"}, {Null: true}, {S: `{"n": 3, "tags": ["a", "b"]}`}, {S: `{"n" : 3}`},
		{Null: true}, {S: "2026-09-01 00:03:00+00"}, {S: "3.99"},
	}
	if len(r.Rows) != 1 {
		t.Fatalf("rows %v", r.Rows)
	}
	for i, v := range r.Rows[0] {
		if v != want[i] {
			t.Errorf("%s = %+v, want %+v", r.Cols[i].Name, v, want[i])
		}
	}
	types := []string{"", "bool", "jsonb", "json", "text", "timestamptz", "numeric"} // an enum has no built-in name
	for i, col := range r.Cols {
		if col.Type != types[i] {
			t.Errorf("%s type %q, want %q", col.Name, col.Type, types[i])
		}
	}
	if r.Tag != "SELECT 1" {
		t.Errorf("tag %q", r.Tag)
	}
	if _, err := c.Query(context.Background(), "select $1::int", db.Val{Null: true}); err != nil {
		t.Errorf("a NULL argument: %v", err)
	}
}

func TestExecRunsEveryStatement(t *testing.T) {
	c := connect(t, false)
	rs, err := c.Exec(context.Background(), "select 1 as a union all select 2; select 'x' as b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || len(rs[0].Rows) != 1 || !rs[0].Truncated || rs[1].Rows[0][0].S != "x" || rs[1].Truncated {
		t.Fatalf("results %+v", rs)
	}
	// Exec's simple protocol takes several statements; Query's extended one refuses them.
	if _, err := c.Query(context.Background(), "select 1; select 2"); sqlState(err) != "42601" {
		t.Fatalf("two statements over Query: %v", err)
	}
}

// Worker.Cancel cancels the statement on the server and keeps the
// connection (§8.3).
func TestCancelKeepsTheConnection(t *testing.T) {
	w := db.NewWorker(connect(t, false))
	time.AfterFunc(200*time.Millisecond, w.Cancel)
	start := time.Now()
	_, err := w.Query(context.Background(), "select pg_sleep(10)")
	if sqlState(err) != "57014" { // query_canceled
		t.Fatalf("cancelled query: %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("cancel took %v", took)
	}
	r, err := w.Query(context.Background(), "select 1")
	if err != nil || r.Rows[0][0].S != "1" {
		t.Fatalf("after cancel: %v %v", r.Rows, err)
	}
}

func TestReadOnlyRefusesWrites(t *testing.T) {
	c := connect(t, true)
	_, err := c.Query(context.Background(), "select nextval('t_order_id_seq')")
	if sqlState(err) != "25006" || !strings.Contains(err.Error(), "read-only transaction") {
		t.Fatalf("nextval on a read-only connection: %v", err)
	}
}
