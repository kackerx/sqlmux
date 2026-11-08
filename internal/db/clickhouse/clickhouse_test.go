//go:build integration

package clickhouse

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

func connect(t *testing.T, dsn string, readOnly bool) *Conn {
	t.Helper()
	c, _, err := Connect(context.Background(), dsn, "", readOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// Each value is the server's own text: NULL apart, the escapes of
// TabSeparated undone, a type of every kind as the server writes it (§8.9).
func TestValues(t *testing.T) {
	c := connect(t, IntegrationDSN(t), false)
	r, err := c.Query(context.Background(), "select id, note, tags, attrs, ref, amount, ratio, created_at, paid, status from sqlmux.t_order where id in (1, 2) order by id")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]db.Val{
		{{S: "1"}, {S: "tab\tand\nnewline\\back"}, {S: "['a','b']"}, {S: "{'k':'0'}"}, {S: "00000000-0000-0000-0000-000000000000"}, {S: "0.25"}, {S: "0"}, {S: "2026-01-01 00:00:00.000"}, {Null: true}, {S: "pending"}},
		{{S: "2"}, {Null: true}, {S: "[]"}, {S: "{'k':'1'}"}, {S: "00000000-0000-0000-0000-000000000001"}, {S: "0.35"}, {S: "0.25"}, {S: "2026-01-01 00:00:01.000"}, {S: "false"}, {S: "running"}},
	}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("rows\n%v\nwant\n%v", r.Rows, want)
	}
	if r.Cols[1].Type != "Nullable(String)" || r.Cols[7].Type != "DateTime64(3, 'UTC')" {
		t.Errorf("types %v", r.Cols)
	}
	if r.Tag != "SELECT 2" {
		t.Errorf("tag %q", r.Tag)
	}
	// arguments go in as literals, a backslash and a quote of them kept
	r, err = c.Query(context.Background(), "select $1, $2, $1 = 'a\\\\b''c'", db.Val{S: `a\b'c`}, db.Val{Null: true})
	if err != nil || !reflect.DeepEqual(r.Rows[0], []db.Val{{S: `a\b'c`}, {Null: true}, {S: "1"}}) {
		t.Errorf("args: %v %v", r.Rows, err)
	}
}

// A statement the server refuses is its error, its name the code; one that
// fails once rows have gone is an error too, not rows (§8.9).
func TestErrors(t *testing.T) {
	c := connect(t, IntegrationDSN(t), false)
	ctx := context.Background()
	_, err := c.Query(ctx, "select nope from sqlmux.t_user")
	se := Engine{}.ErrorOf(err)
	if se.Code != "UNKNOWN_IDENTIFIER" || !strings.Contains(se.Message, "nope") || strings.Contains(se.Message, "version") {
		t.Errorf("error %+v", se)
	}
	_, err = c.Query(ctx, "select number, throwIf(number = 3, 'boom') from numbers(10) settings max_block_size = 1")
	if se := (Engine{}).ErrorOf(err); se.Code != "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO" || !strings.Contains(se.Message, "boom") {
		t.Errorf("an error after rows: %v %+v", err, se)
	}
	if (Engine{}).Broken(err) != db.NotLost {
		t.Error("a server's error is no lost connection")
	}
	if _, _, err := Connect(ctx, "clickhouse://localhost:1/x", "", false); (Engine{}).Broken(err) != db.LostUnsent {
		t.Errorf("a refused dial: %v", err)
	}
	if _, _, err := Connect(ctx, "clickhouse://localhost:9000/x", "", false); err == nil || !strings.Contains(err.Error(), "8123") {
		t.Errorf("the native port: %v", err)
	}
}

// Statements run one after the other, the first failure stopping the rest;
// USE moves the database every request after it names, as UseSchema does.
func TestExecAndUse(t *testing.T) {
	c := connect(t, IntegrationDSN(t), false)
	ctx := context.Background()
	rs, err := c.Exec(ctx, "select 1; use analytics; select currentDatabase(); select count() from events", 0)
	if err != nil || len(rs) != 4 || rs[2].Rows[0][0].S != "analytics" || rs[3].Rows[0][0].S != "3" {
		t.Fatalf("%v %v", rs, err)
	}
	rs, err = c.Exec(ctx, "select 1; select nope; select 2", 0)
	if err == nil || len(rs) != 1 {
		t.Errorf("stops at the failure: %d results, %v", len(rs), err)
	}
	if err := (Engine{}).UseSchema(ctx, c, "sqlmux", ""); err != nil {
		t.Fatal(err)
	}
	r, _ := c.Query(ctx, "select currentDatabase()")
	if r.Rows[0][0].S != "sqlmux" {
		t.Errorf("after UseSchema: %v", r.Rows)
	}
	rs, err = c.Exec(ctx, "select number from numbers(10)", 3)
	if err != nil || len(rs[0].Rows) != 3 || !rs[0].Truncated {
		t.Errorf("maxRows: %v %v", rs, err)
	}
	r2, err := c.Query(ctx, "select 1 as a format JSONEachRow")
	if err != nil || len(r2.Rows) != 1 || r2.Rows[0][0].S != `{"a":1}` || r2.Cols[0].Name != "jsoneachrow" {
		t.Errorf("a format of its own: %v %v", r2, err)
	}
}

// The tree's databases, tables and views, and what a table tab knows of a
// table: its key, and why a table or a column is not to edit (§8.4, §10.1).
func TestCatalog(t *testing.T) {
	c := connect(t, IntegrationDSN(t), true)
	ctx, e := context.Background(), Engine{}
	schemas, current, err := e.Schemas(ctx, c)
	if err != nil || !slices.Contains(schemas, "sqlmux") || !slices.Contains(schemas, "analytics") || slices.Contains(schemas, "system") || current != "sqlmux" {
		t.Errorf("schemas %v %q %v", schemas, current, err)
	}
	tables, err := e.Tables(ctx, c, []string{"sqlmux", "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, tb := range tables {
		kinds[tb.Schema+"."+tb.Name] = tb.Kind
		if strings.HasPrefix(tb.Name, ".") {
			t.Errorf("an inner table listed: %s", tb.Name)
		}
		if tb.Name == "t_order" && tb.Rows != 5200 {
			t.Errorf("t_order rows %v", tb.Rows)
		}
	}
	if kinds["sqlmux.t_user"] != "r" || kinds["sqlmux.v_active"] != "v" || kinds["analytics.mv_daily"] != "m" {
		t.Errorf("kinds %v", kinds)
	}
	cols, err := e.TableColumns(ctx, c, "sqlmux", "t_order")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cols.Key(), []string{"id"}) || cols.ReadOnly != "" {
		t.Errorf("t_order key %v, %q", cols.Key(), cols.ReadOnly)
	}
	by := map[string]db.Column{}
	for _, col := range cols.Cols {
		by[col.Name] = col
	}
	if by["id"].ReadOnly == "" || by["user_id"].ReadOnly == "" { // the sorting key's
		t.Errorf("key columns editable: %+v %+v", by["id"], by["user_id"])
	}
	if by["attrs"].ReadOnly == "" || by["tags"].ReadOnly != "" || by["note"].ReadOnly != "" {
		t.Errorf("map %+v, array %+v, note %+v", by["attrs"], by["tags"], by["note"])
	}
	if !reflect.DeepEqual(by["status"].Enum, []string{"pending", "running", "done", "failed"}) || by["note"].NotNull || !by["amount"].NotNull {
		t.Errorf("status %+v note %+v amount %+v", by["status"], by["note"], by["amount"])
	}
	if cols, _ := e.TableColumns(ctx, c, "sqlmux", "t_log"); cols.Key() != nil {
		t.Errorf("t_log, ORDER BY tuple(), has a key: %v", cols.Key())
	}
	if cols, _ := e.TableColumns(ctx, c, "sqlmux", "t_mem"); cols.ReadOnly == "" {
		t.Error("a Memory table is editable")
	}
	users, _ := e.TableColumns(ctx, c, "sqlmux", "t_user")
	if email := users.Cols[2]; email.Name != "email" || email.ReadOnly != "" || !email.AddOnly {
		t.Errorf("email %+v", email)
	}
	ddl, err := e.DDL(ctx, c, "sqlmux", "t_log")
	if err != nil || !strings.HasPrefix(ddl, "CREATE TABLE sqlmux.t_log\n(") || !strings.HasSuffix(ddl, ";") {
		t.Errorf("ddl %q %v", ddl, err)
	}
	keys, err := e.Keys(ctx, c, "sqlmux", "t_order")
	if err != nil || len(keys) != 1 || !reflect.DeepEqual(keys[0].Cols, []string{"id"}) {
		t.Errorf("keys %v %v", keys, err)
	}
}

// A page and a count of a table, as a table tab asks them (§8.5).
func TestPage(t *testing.T) {
	c := connect(t, IntegrationDSN(t), true)
	ctx, e := context.Background(), Engine{}
	src := db.SQLTable{E: e, Schema: "sqlmux", Name: "t_order", Meta: db.NewWorker(c)}
	cols, err := src.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := src.Page(ctx, db.PageQuery{Cols: cols, Filter: "status = 'done'", Order: []db.OrderCol{{Col: "amount", Desc: true}}, Limit: 3, Offset: 1})
	if err != nil || len(p.Rows) != 3 || !p.Next {
		t.Fatalf("page %v %v", p, err)
	}
	if n, err := src.Count(ctx, "status = 'done'"); err != nil || n != 1300 {
		t.Errorf("count %d %v", n, err)
	}
}

// A save on ClickHouse: each change a mutation the server waits for, a count
// first that says the row is there and holds what was read (§10.3).
func TestSave(t *testing.T) {
	dsn := OwnDB(t)
	main := connect(t, dsn, false)
	ctx, e := context.Background(), Engine{}
	w := db.NewWorker(main)
	src := db.SQLTable{E: e, Schema: main.database, Name: "t_order", Meta: w, Main: w}
	cols, err := src.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	src.Known = cols
	read := func(id string) []db.Val {
		r, err := main.Query(ctx, "select note, ratio, created_at, tags, status, amount, paid from t_order where id = $1", db.Val{S: id})
		if err != nil || len(r.Rows) != 1 {
			t.Fatalf("read %s: %v %v", id, r.Rows, err)
		}
		return r.Rows[0]
	}
	old := read("2")
	upd := db.Row{Key: []string{"2"}, Cols: []db.Change{
		{Name: "status", Val: db.Val{S: "failed"}, Old: old[4]},
		{Name: "amount", Val: db.Val{S: "12.50"}, Old: old[5]},
		{Name: "paid", Val: db.Val{Null: true}, Old: old[6]},
		{Name: "ratio", Val: db.Val{S: "0.1"}, Old: old[1]},
		{Name: "note", Val: db.Val{S: "it's a\\b\tc"}, Old: old[0]},
		{Name: "created_at", Val: db.Val{S: "2026-05-06 07:08:09.123"}, Old: old[2]},
		{Name: "tags", Val: db.Val{S: "['x','y']"}, Old: old[3]},
	}}
	if failed, tx, err := src.Save(ctx, db.Changes{Updates: []db.Row{upd}}, false); err != nil || failed != -1 || tx != 'I' {
		t.Fatalf("save: %d %c %v", failed, tx, err)
	}
	got := read("2")
	want := []db.Val{{S: "it's a\\b\tc"}, {S: "0.1"}, {S: "2026-05-06 07:08:09.123"}, {S: "['x','y']"}, {S: "failed"}, {S: "12.5"}, {Null: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the save\n%v\nwant\n%v", got, want)
	}
	// what was read is no longer there: stale, nothing written
	if failed, _, err := src.Save(ctx, db.Changes{Updates: []db.Row{upd}}, false); !errors.Is(err, db.ErrStale) || failed != 0 {
		t.Errorf("stale: %d %v", failed, err)
	}
	// each value back to what it was, from the text the save read
	back := db.Row{Key: []string{"2"}}
	for i, c := range upd.Cols {
		back.Cols = append(back.Cols, db.Change{Name: c.Name, Val: c.Old, Old: got[[]int{4, 5, 6, 1, 0, 2, 3}[i]]})
	}
	if _, _, err := src.Save(ctx, db.Changes{Updates: []db.Row{back}}, false); err != nil {
		t.Fatalf("back: %v", err)
	}
	if got := read("2"); !reflect.DeepEqual(got, old) {
		t.Errorf("not back\n%v\nwas\n%v", got, old)
	}
	// a delete, an insert, an insert of the DEFAULTs; then a delete of a row
	// gone after one that went: the first stays written, and the error says so
	ch := db.Changes{
		Deletes: [][]string{{"3"}},
		Inserts: [][]db.Change{{{Name: "id", Val: db.Val{S: "900"}}, {Name: "user_id", Val: db.Val{S: "1"}}, {Name: "status", Val: db.Val{S: "done"}}, {Name: "amount", Val: db.Val{S: "1.5"}},
			{Name: "ratio", Val: db.Val{S: "2"}}, {Name: "tags", Val: db.Val{S: "[]"}}, {Name: "attrs", Default: true}, {Name: "ref", Val: db.Val{S: "00000000-0000-0000-0000-000000000900"}},
			{Name: "region", Val: db.Val{S: "eu"}}, {Name: "created_at", Val: db.Val{S: "2026-01-01 00:00:00"}}}},
	}
	if failed, _, err := src.Save(ctx, ch, false); err != nil || failed != -1 {
		t.Fatalf("delete and insert: %d %v", failed, err)
	}
	r, _ := main.Query(ctx, "select count() from t_order where id in (3, 900)")
	if r.Rows[0][0].S != "1" {
		t.Errorf("after delete 3 and insert 900: %v", r.Rows)
	}
	users := db.SQLTable{E: e, Schema: main.database, Name: "t_user", Meta: w, Main: w}
	users.Known, _ = users.Columns(ctx)
	if _, _, err := users.Save(ctx, db.Changes{Inserts: [][]db.Change{nil}}, false); err != nil {
		t.Errorf("a row of DEFAULTs: %v", err)
	}
	failed, _, err := src.Save(ctx, db.Changes{Deletes: [][]string{{"4"}, {"3"}}}, false)
	var written db.Written
	if failed != 1 || !errors.Is(err, db.ErrGone) || !errors.As(err, &written) || written.N != 1 {
		t.Errorf("a delete after one that went: %d %v", failed, err)
	}
	r, _ = main.Query(ctx, "select count() from t_order where id = 4")
	if r.Rows[0][0].S != "0" {
		t.Error("the first delete was taken back")
	}
	// the preview is what is sent, the values in
	p := src.Preview(db.Changes{Updates: []db.Row{{Key: []string{"5"}, Cols: []db.Change{{Name: "note", Val: db.Val{S: "x"}, Old: db.Val{Null: true}}}}}})
	if len(p) != 1 || p[0] != "alter table `"+main.database+"`.`t_order` update `note` = CAST('x' AS Nullable(String)) where `id` = CAST('5' AS UInt64) and `note` is null" {
		t.Errorf("preview %q", p)
	}
}

// A read-only connection's writes the server refuses, as quick SQL's are
// (§13).
func TestReadOnly(t *testing.T) {
	ro := connect(t, OwnDB(t), true)
	ctx := context.Background()
	if _, err := ro.Query(ctx, "insert into t_user (id, name) values (99, 'x')"); (Engine{}).ErrorOf(err).Code != "READONLY" {
		t.Errorf("insert on read only: %v", err)
	}
	if _, err := ro.Query(ctx, "set max_threads = 2"); err != nil {
		t.Errorf("a setting on read only: %v", err)
	}
	w := db.NewWorker(ro)
	if _, err := (Engine{}).Quick(ctx, w, "alter table t_user delete where 1", "", 10); err == nil {
		t.Error("quick SQL wrote")
	}
	r, err := Engine{}.Quick(ctx, w, "select number from numbers(100)", "sqlmux", 10)
	if err != nil || len(r.Rows) != 10 || !r.Truncated {
		t.Errorf("quick read: %d %v %v", len(r.Rows), r.Truncated, err)
	}
}

// A cancel stops the statement on the server, not only the wait for it (§8.3).
func TestCancel(t *testing.T) {
	c := connect(t, IntegrationDSN(t), false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := c.Query(ctx, "select count(), 'sqlmux_cancel_marker' from numbers(1e12)")
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel did not stop the request")
	}
	other := connect(t, IntegrationDSN(t), true)
	for range 50 {
		r, err := other.Query(context.Background(), "select count() from system.processes where query like '%sqlmux_cancel_marker%' and query not like '%system.processes%'")
		if err == nil && r.Rows[0][0].S == "0" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("the statement still runs on the server")
}
