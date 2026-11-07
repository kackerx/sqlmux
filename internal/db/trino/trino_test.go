//go:build integration

package trino

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
)

func connect(t *testing.T, dsn string) *Conn {
	t.Helper()
	c, _, err := Connect(context.Background(), dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// Each value is the text the server wrote in its JSON: a string's own, a
// number's digits, an array's or a row's JSON, a varbinary by its size (§8.10).
func TestValues(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	r, err := c.Query(context.Background(), `select id, amount, meta, created_at, status, paid, 1.5e0, array[1, 2], map(array['k'], array[1]), row(1, 'x'), cast(null as varchar), X'00ff'
from t_order where id = 1`)
	if err != nil {
		t.Fatal(err)
	}
	want := []db.Val{{S: "1"}, {S: "1.99"}, {S: `{"n":1,"tags":["a","b"]}`}, {S: "2026-09-01 00:01:00.000 UTC"}, {S: "running"}, {S: "false"},
		{S: "1.5"}, {S: "[1,2]"}, {S: `{"k":1}`}, {S: `[1,"x"]`}, {Null: true}, {S: db.BinaryShown(2)}}
	if !reflect.DeepEqual(r.Rows, [][]db.Val{want}) {
		t.Errorf("row\n%v\nwant\n%v", r.Rows, want)
	}
	if r.Cols[1].Type != "decimal(10, 2)" || r.Cols[3].Type != "timestamp with time zone" || r.Tag != "SELECT 1" {
		t.Errorf("cols %v, tag %q", r.Cols, r.Tag)
	}
	r, err = c.Query(context.Background(), "select $1, $2, $1 = 'it''s'", db.Val{S: "it's"}, db.Val{Null: true})
	if err != nil || !reflect.DeepEqual(r.Rows[0], []db.Val{{S: "it's"}, {Null: true}, {S: "true"}}) {
		t.Errorf("args: %v %v", r.Rows, err)
	}
}

// A statement the server refuses is its error: the error's name its code, the
// place it points at the character of the statement (§11).
func TestErrors(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	ctx := context.Background()
	_, err := c.Query(ctx, "select\nfrom t_user")
	se := Engine{}.ErrorOf(err)
	if se.Code != "SYNTAX_ERROR" || se.Position != 8 || strings.HasPrefix(se.Message, "line ") {
		t.Errorf("syntax error %+v", se)
	}
	_, err = c.Query(ctx, "select * from nope")
	if se := (Engine{}).ErrorOf(err); se.Code != "TABLE_NOT_FOUND" {
		t.Errorf("no table %+v", se)
	}
	if (Engine{}).Broken(err) != db.NotLost {
		t.Error("a server's error is no lost connection")
	}
	if _, _, err := Connect(ctx, "trino://u@localhost:1/x", "", false); (Engine{}).Broken(err) != db.LostUnsent {
		t.Errorf("a refused dial: %v", err)
	}
}

// Statements run one after the other; what USE, SET SESSION and START
// TRANSACTION do to the session holds for the statements after them.
func TestSession(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	ctx := context.Background()
	rs, err := c.Exec(ctx, "use tpch.tiny; select count(*) from nation; set session query_max_run_time = '1h'; show session like 'query_max_run_time'", 0)
	if err != nil || len(rs) != 4 || rs[1].Rows[0][0].S != "25" || rs[3].Rows[0][1].S != "1h" {
		t.Fatalf("%v %v", rs, err)
	}
	if c.catalog != "tpch" || c.schema != "tiny" {
		t.Errorf("after use: %s.%s", c.catalog, c.schema)
	}
	if _, err := c.Exec(ctx, "start transaction", 0); err != nil || c.TxStatus() != 'T' {
		t.Fatalf("begin: %v %c", err, c.TxStatus())
	}
	if _, err := c.Exec(ctx, "rollback", 0); err != nil || c.TxStatus() != 'I' {
		t.Errorf("rollback: %v %c", err, c.TxStatus())
	}
	rs, err = c.Exec(ctx, "select name from nation order by 1;", 3)
	if err != nil || len(rs[0].Rows) != 3 || !rs[0].Truncated {
		t.Errorf("maxRows: %v %v", rs, err)
	}
	if err := (Engine{}).UseSchema(ctx, c, "sf1", ""); err != nil || c.schema != "sf1" {
		t.Errorf("UseSchema: %v %s", err, c.schema)
	}
	if dsn, _ := WithDatabase("trino://u@h:8080/postgresql/public?ssl=true", "tpch"); dsn != "trino://u@h:8080/tpch?ssl=true" {
		t.Errorf("WithDatabase %s", dsn)
	}
	if dsn, _ := WithDatabase("http://u@h:8080?catalog=a&schema=b", "tpch"); dsn != "http://u@h:8080?catalog=tpch" {
		t.Errorf("WithDatabase %s", dsn)
	}
}

// The catalogs are the databases; a table's row identity is its NOT NULL
// columns; a view, and what has no such column, is not to edit (§8.4, §10.1).
func TestCatalog(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	ctx, e := context.Background(), Engine{}
	dbs, err := e.Databases(ctx, c)
	if err != nil || !slices.Contains(dbs, "postgresql") || !slices.Contains(dbs, "tpch") {
		t.Errorf("catalogs %v %v", dbs, err)
	}
	schemas, current, err := e.Schemas(ctx, c)
	if err != nil || !slices.Contains(schemas, "public") || slices.Contains(schemas, "information_schema") || current != "public" {
		t.Errorf("schemas %v %q %v", schemas, current, err)
	}
	tables, err := e.Tables(ctx, c, []string{"public"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, tb := range tables {
		kinds[tb.Name] = tb.Kind
	}
	if kinds["t_user"] != "r" { // the postgresql connector shows PG's views as tables: its own way
		t.Errorf("kinds %v", kinds)
	}
	cols, err := e.TableColumns(ctx, c, "public", "t_order")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cols.Key(), []string{"id", "user_id", "status", "amount", "created_at"}) || cols.ReadOnly != "" {
		t.Errorf("key %v %q", cols.Key(), cols.ReadOnly)
	}
	// a view of Trino's own, in the memory catalog
	mem := connect(t, strings.Replace(IntegrationDSN(t), "/postgresql/public", "/memory/default", 1))
	if _, err := mem.Exec(ctx, "create or replace view v_one as select 1 as x", 0); err != nil {
		t.Fatal(err)
	}
	if mtables, _ := e.Tables(ctx, mem, nil); !slices.ContainsFunc(mtables, func(tb db.Table) bool { return tb.Name == "v_one" && tb.Kind == "v" }) {
		t.Errorf("memory's view: %v", mtables)
	}
	if v, _ := e.TableColumns(ctx, mem, "default", "v_one"); v.Key() != nil {
		t.Errorf("a view has a key: %v", v.Key())
	}
	ddl, err := e.DDL(ctx, c, "public", "t_user")
	if err != nil || !strings.HasPrefix(ddl, "CREATE TABLE postgresql.public.t_user") {
		t.Errorf("ddl %q %v", ddl, err)
	}
	if ddl, err := e.DDL(ctx, mem, "default", "v_one"); err != nil || !strings.HasPrefix(ddl, "CREATE VIEW") {
		t.Errorf("view ddl %q %v", ddl, err)
	}
}

// A page of a table, its OFFSET before its LIMIT as Trino has them, and a
// count (§8.5).
func TestPage(t *testing.T) {
	c := connect(t, strings.Replace(IntegrationDSN(t), "/postgresql/public", "/tpch/tiny", 1))
	ctx := context.Background()
	src := db.SQLTable{E: Engine{}, Schema: "tiny", Name: "nation", Meta: db.NewWorker(c)}
	cols, err := src.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := src.Page(ctx, db.PageQuery{Cols: cols, Filter: "regionkey = 1", Order: []db.OrderCol{{Col: "name"}}, Limit: 2, Offset: 1})
	if err != nil || len(p.Rows) != 2 || !p.Next || p.Rows[0][1].S != "BRAZIL" {
		t.Fatalf("page %v %v", p.Rows, err)
	}
	if n, err := src.Count(ctx, "regionkey = 1"); err != nil || n != 5 {
		t.Errorf("count %d %v", n, err)
	}
}

// A save through Trino: each change alone in autocommit, a count first that
// says the row is there as it was read (§10.3).
func TestSave(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	ctx, e := context.Background(), Engine{}
	schema := OwnSchema(t)
	w := db.NewWorker(c)
	src := db.SQLTable{E: e, Schema: schema, Name: "t_item", Meta: w, Main: w}
	cols, err := src.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cols.Key(), []string{"id", "name", "price", "at"}) {
		t.Fatalf("key %v", cols.Key())
	}
	src.Known = cols
	read := func(id string) []db.Val {
		r, err := c.Query(ctx, `select id, name, price, at, note, meta, ok from `+e.Quote(schema, "t_item")+` where id = cast($1 as bigint)`, db.Val{S: id})
		if err != nil || len(r.Rows) != 1 {
			t.Fatalf("read %s: %v %v", id, r.Rows, err)
		}
		return r.Rows[0]
	}
	key := func(row []db.Val) []string { return []string{row[0].S, row[1].S, row[2].S, row[3].S} }
	one := read("1")
	upd := db.Row{Key: key(one), Cols: []db.Change{
		{Name: "price", Val: db.Val{S: "9.99"}, Old: one[2]},
		{Name: "at", Val: db.Val{S: "2026-06-07 08:09:10.111 UTC"}, Old: one[3]},
		{Name: "note", Val: db.Val{S: "it's new"}, Old: one[4]},
		{Name: "meta", Val: db.Val{S: `{"k":2}`}, Old: one[5]},
		{Name: "ok", Val: db.Val{Null: true}, Old: one[6]},
	}}
	if failed, _, err := src.Save(ctx, db.Changes{Updates: []db.Row{upd}}, false); err != nil || failed != -1 {
		t.Fatalf("save: %d %v", failed, err)
	}
	want := []db.Val{{S: "1"}, {S: "a"}, {S: "9.99"}, {S: "2026-06-07 08:09:10.111 UTC"}, {S: "it's new"}, {S: `{"k":2}`}, {Null: true}}
	if got := read("1"); !reflect.DeepEqual(got, want) {
		t.Errorf("after the save\n%v\nwant\n%v", got, want)
	}
	if failed, _, err := src.Save(ctx, db.Changes{Updates: []db.Row{upd}}, false); !errors.Is(err, db.ErrGone) && !errors.Is(err, db.ErrStale) || failed != 0 {
		t.Errorf("stale: %d %v", failed, err)
	}
	two, three := read("2"), read("3")
	ch := db.Changes{
		Deletes: [][]string{key(two)},
		Inserts: [][]db.Change{{{Name: "id", Val: db.Val{S: "9"}}, {Name: "name", Val: db.Val{S: "z"}}, {Name: "price", Val: db.Val{S: "0.01"}}, {Name: "at", Val: db.Val{S: "2026-01-09 00:00:00.000 UTC"}}}},
	}
	if failed, _, err := src.Save(ctx, ch, false); err != nil || failed != -1 {
		t.Fatalf("delete and insert: %d %v", failed, err)
	}
	if r, _ := c.Query(ctx, `select count(*) from `+e.Quote(schema, "t_item")+` where id in (2, 9)`); r.Rows[0][0].S != "1" {
		t.Errorf("after: %v", r.Rows)
	}
	failed, _, err := src.Save(ctx, db.Changes{Deletes: [][]string{key(three), key(two)}}, false)
	var written db.Written
	if failed != 1 || !errors.Is(err, db.ErrGone) || !errors.As(err, &written) || written.N != 1 {
		t.Errorf("a delete after one that went: %d %v", failed, err)
	}
	p := src.Preview(db.Changes{Deletes: [][]string{key(one)}})
	if len(p) != 1 || !strings.HasPrefix(p[0], `delete from "`+schema+`"."t_item" where "id" = CAST('1' AS bigint) and "name" = CAST('a' AS varchar)`) {
		t.Errorf("preview %q", p)
	}
}

// Quick SQL reads in a read-only transaction; a write does not go (§12).
func TestQuick(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	ctx, w := context.Background(), db.NewWorker(c)
	r, err := Engine{}.Quick(ctx, w, "select id from t_order order by id", "public", 5)
	if err != nil || len(r.Rows) != 5 || !r.Truncated {
		t.Errorf("read: %d %v", len(r.Rows), err)
	}
	if _, err := (Engine{}).Quick(ctx, w, "delete from t_user where id = 1", "", 5); err == nil {
		t.Error("quick SQL wrote")
	}
	if c.TxStatus() != 'I' {
		t.Error("quick SQL left its transaction open")
	}
}

// A cancel stops the statement on the server, not only the wait for it (§8.3).
func TestCancel(t *testing.T) {
	c := connect(t, IntegrationDSN(t))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := c.Query(ctx, "select count(*), 'sqlmux_cancel_marker' from tpch.sf100.lineitem a cross join tpch.sf100.lineitem b")
		done <- err
	}()
	time.Sleep(1500 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel did not stop the request")
	}
	other := connect(t, IntegrationDSN(t))
	for range 60 {
		r, err := other.Query(context.Background(), "select count(*) from system.runtime.queries where query like '%sqlmux_cancel_marker%' and query not like '%system.runtime%' and state not in ('FAILED', 'FINISHED')")
		if err == nil && r.Rows[0][0].S == "0" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("the statement still runs on the server")
}
