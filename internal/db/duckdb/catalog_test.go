//go:build integration

package duckdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
)

// seeded is a connection to a file of testdata/seed/duckdb.sql's tables.
func seeded(t testing.TB) (*Conn, string) {
	t.Helper()
	path := OwnDB(t)
	c, _, err := Connect(context.Background(), path, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, path
}

func TestCatalog(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	schemas, current, err := e.Schemas(ctx, c)
	if err != nil || strings.Join(schemas, ",") != "agentable,main" || current != "main" {
		t.Errorf("schemas %v %q %v", schemas, current, err)
	}
	if dbs, err := e.Databases(ctx, c); dbs != nil || err != nil {
		t.Errorf("databases %v %v", dbs, err)
	}
	ts, err := e.Tables(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tb := range ts {
		got = append(got, tb.Schema+"."+tb.Name+"/"+tb.Kind)
	}
	want := "agentable.agent/r agentable.agent_version/r agentable.goal/r main.t_log/r main.t_order/r main.t_order_item/r main.t_sku/r main.t_types/r main.t_user/r main.v_paid_order/v"
	if strings.Join(got, " ") != want {
		t.Errorf("tables:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	for _, tb := range ts {
		if tb.Name == "t_order" && tb.Rows < 5000 || tb.View() && tb.Rows >= 0 {
			t.Errorf("rows of %s: %v", tb.Name, tb.Rows)
		}
	}
	only, err := e.Tables(ctx, c, []string{"agentable"})
	if err != nil || len(only) != 3 {
		t.Errorf("one schema: %v %v", only, err)
	}
	if none, err := e.Tables(ctx, c, []string{}); err != nil || len(none) != 0 {
		t.Errorf("no schema: %v %v", none, err)
	}
	seqs, err := e.Sequences(ctx, c, nil)
	var sn []string
	for _, s := range seqs {
		sn = append(sn, s.Schema+"."+s.Name)
	}
	if err != nil || strings.Join(sn, " ") != "agentable.run_no main.invoice_no main.t_order_id main.t_user_id" {
		t.Errorf("sequences %v %v", sn, err)
	}
	fns, err := e.Functions(ctx, c, nil)
	var fn []string
	for _, f := range fns {
		fn = append(fn, f.Schema+"."+f.Name+"("+f.Args+")")
	}
	if err != nil || strings.Join(fn, " ") != "agentable.fn_agent_count() main.fn_greet(who) main.fn_order_total(order_id) main.fn_range(n)" {
		t.Errorf("functions %v %v", fn, err)
	}
	for _, f := range fns {
		def, err := e.FunctionDef(ctx, c, f)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "fn_range" && def != `create macro "main"."fn_range"("n") as table SELECT * FROM "range"(n);` {
			t.Errorf("def %q", def)
		}
		q(t, c, strings.Replace(def, "create macro", "create or replace macro", 1)) // it makes the macro again
	}
}

func TestTableColumns(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	cols := func(schema, table string) db.Columns {
		t.Helper()
		r, err := e.TableColumns(ctx, c, schema, table)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	u := cols("main", "t_user")
	if strings.Join(u.PK, ",") != "id" || len(u.Cols) != 5 || u.Cols[0].Name != "id" || !u.Cols[0].NotNull || u.Cols[0].Type != "bigint" ||
		u.Cols[1].Type != "varchar" || !u.Cols[1].NotNull || u.Cols[2].NotNull || u.Cols[3].Default != "true" || u.Cols[4].Type != "timestamp with time zone" || u.Cols[4].Default != "now()" {
		t.Errorf("t_user: %+v", u)
	}
	if k := cols("main", "t_order_item").Key(); strings.Join(k, ",") != "order_id,line_no" {
		t.Errorf("composite key %v", k)
	}
	sku := cols("main", "t_sku") // no primary key: the unique constraints and indexes over not-null columns, not the nullable barcode or an expression
	if sku.PK != nil || len(sku.Unique) != 2 || strings.Join(sku.Unique[0], ",") != "code" || strings.Join(sku.Unique[1], ",") != "title,code" {
		t.Errorf("t_sku: %+v", sku)
	}
	if lg := cols("main", "t_log"); lg.Key() != nil || lg.Lead != "" {
		t.Errorf("t_log has nothing to find a row by: %+v", lg)
	}
	ty := cols("main", "t_types")
	if a := ty.Cols[len(ty.Cols)-1]; a.Name != "area" || !a.Generated || a.ReadOnly == "" || a.Default != "" {
		t.Errorf("a generated column: %+v", a)
	}
	if ty.Cols[1].Type != "integer[]" || ty.Cols[2].Type != "struct(a integer, b varchar)" || ty.Cols[3].Type != "map(varchar, integer)" {
		t.Errorf("t_types: %+v", ty.Cols)
	}
	if a := cols("agentable", "agent"); strings.Join(a.PK, ",") != "id" || len(a.Cols) != 2 {
		t.Errorf("a table of another schema: %+v", a)
	}
	if v := cols("main", "v_paid_order"); len(v.Cols) != 3 || v.Key() != nil {
		t.Errorf("a view: %+v", v)
	}
	// a BLOB is no key to find a row by
	q(t, c, "create table bk(id blob primary key, v varchar)")
	if b := cols("main", "bk"); b.ReadOnly == "" || !strings.Contains(b.ReadOnly, "id") {
		t.Errorf("a blob key: %+v", b)
	}
	if n := cols("main", "nope"); len(n.Cols) != 0 {
		t.Errorf("no such table: %+v", n)
	}
}

func TestKeysIndexesDDL(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	ks, err := e.Keys(ctx, c, "main", "t_order_item")
	if err != nil || len(ks) != 2 || ks[0].Name != "PRIMARY" || strings.Join(ks[0].Cols, ",") != "order_id,line_no" ||
		ks[1].Ref == nil || ks[1].Ref.Table != "t_order" || ks[1].Ref.Schema != "main" || strings.Join(ks[1].Ref.Cols, ",") != "id" || strings.Join(ks[1].Cols, ",") != "order_id" {
		t.Errorf("keys %+v %v", ks, err)
	}
	ks, err = e.Keys(ctx, c, "agentable", "goal")
	if err != nil || len(ks) != 2 || ks[1].Ref.Schema != "agentable" || ks[1].Ref.Table != "agent" {
		t.Errorf("keys of the other schema %+v %v", ks, err)
	}
	ix, err := e.Indexes(ctx, c, "main", "t_sku")
	if err != nil || len(ix) != 2 || ix[0].Name != "t_sku_lower_title" || !ix[0].Unique || !strings.HasPrefix(ix[0].Def, "CREATE UNIQUE INDEX t_sku_lower_title") ||
		strings.Join(ix[0].Cols, "|") != "(lower(title))" || ix[1].Name != "t_sku_title_code" || strings.Join(ix[1].Cols, ",") != "title,code" {
		t.Errorf("indexes %+v %v", ix, err)
	}
	ix, err = e.Indexes(ctx, c, "main", "t_order")
	if err != nil || len(ix) != 1 || ix[0].Unique || strings.Join(ix[0].Cols, ",") != "user_id,created_at" && !strings.HasPrefix(strings.Join(ix[0].Cols, ","), "user_id") {
		t.Errorf("t_order indexes %+v %v", ix, err)
	}
	for _, name := range []string{"t_user", "v_paid_order"} {
		d, err := e.DDL(ctx, c, "main", name)
		if err != nil || !strings.Contains(d, name) || !strings.HasSuffix(d, ";") {
			t.Errorf("DDL of %s: %q %v", name, d, err)
		}
	}
	if d, err := e.DDL(ctx, c, "main", "nope"); d != "" || err != nil {
		t.Errorf("DDL of what is not there: %q %v", d, err)
	}
}

func TestParseList(t *testing.T) {
	for in, want := range map[string]string{
		`[]`:                "",
		`[a, b]`:            "a|b",
		`[x, '(lower(y))']`: "x|(lower(y))",
		`['"name"']`:        "name",
		`['"a, b"', c]`:     `a, b|c`,
		`['it\'s', 'a b']`:  "it's|a b",
		`["a ""q"" b"]`:     `a "q" b`,
	} {
		if got := strings.Join(parseList(in), "|"); got != want {
			t.Errorf("parseList(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestUseSchemaAndQuick(t *testing.T) {
	c, path := seeded(t)
	ctx, e := context.Background(), Engine{}
	if err := e.UseSchema(ctx, c, "agentable", ""); err != nil {
		t.Fatal(err)
	}
	eq(t, cells(q(t, c, "select count(*) from agent")), []string{"3"})
	eq(t, cells(q(t, c, "select count(*) from t_user")), []string{"50"}) // main's are found still
	if err := e.UseSchema(ctx, c, "main", "main"); err != nil {
		t.Fatal(err)
	}
	if err := e.UseSchema(ctx, c, "no such", ""); err == nil || e.ErrorOf(err).Code != "Catalog" {
		t.Errorf("a schema that is not there: %v", err)
	}
	if err := e.UseSchema(ctx, c, `we"ird`, ""); err == nil {
		t.Errorf("quoted")
	}
	// Quick runs in schema first, in a read only transaction
	meta, _, err := Connect(ctx, path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	w := db.NewWorker(meta)
	r, err := e.Quick(ctx, w, "select name from agent order by id", "agentable", 2)
	if err != nil || len(r.Rows) != 2 || !r.Truncated || r.Rows[0][0].S != "planner" {
		t.Errorf("quick: %+v %v", r, err)
	}
	r, err = e.Quick(ctx, w, "from agent limit 1", "agentable", 10)
	if err != nil || len(r.Rows) != 1 {
		t.Errorf("quick from-first: %+v %v", r, err)
	}
	if _, err := e.Quick(ctx, w, "delete from agent", "agentable", 10); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("a write by quick SQL: %v", err)
	}
	if r, err := e.Quick(ctx, w, "select count(*) from t_user", "main", 10); err != nil || r.Rows[0][0].S != "50" {
		t.Errorf("after an error: %+v %v", r, err)
	}
}

func TestPageAndCount(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	q1 := db.Query{Schema: "main", Table: "t_order", Key: []string{"id"}, Limit: 500, Offset: 700, Where: "status = 'done'"}
	r, next, err := db.Page(ctx, c, e, q1)
	if err != nil || len(r.Rows) != 500 || !next || r.Cols[0].Name != "id" {
		t.Fatalf("page: %d rows, next %v, %v", len(r.Rows), next, err)
	}
	if n, err := db.Count(ctx, c, e, db.Query{Schema: "main", Table: "t_order", Where: "status = 'done'"}); err != nil || n != 1500 {
		t.Errorf("count %d %v", n, err)
	}
	// a WHERE with a trailing comment, an ORDER, a page of another schema's table
	r, _, err = db.Page(ctx, c, e, db.Query{Schema: "main", Table: "t_user", Key: []string{"id"}, Limit: 3, Where: "id > 10 -- ten", Order: []db.OrderCol{{Col: "name", Desc: true}}})
	if err != nil || len(r.Rows) != 3 || r.Rows[0][1].S != "user_50" {
		t.Errorf("ordered page: %v %v", r.Rows, err)
	}
	r, _, err = db.Page(ctx, c, e, db.Query{Schema: "agentable", Table: "agent", Key: []string{"id"}, Limit: 10})
	if err != nil || len(r.Rows) != 3 {
		t.Errorf("agentable: %v %v", r, err)
	}
	if _, _, err := db.Page(ctx, c, e, db.Query{Schema: "main", Table: "t_order", Key: []string{"id"}, Limit: 5, Where: "nope = 1"}); err == nil {
		t.Error("a WHERE that does not hold is an error")
	}
	// the BLOB of a row, a column of every kind
	r = q(t, c, `select raw, meta, deleted_at, status, amount from t_order where id = 1`)
	if r.Rows[0][0].S != "<binary 6 bytes>" || r.Rows[0][1].S != `{"n":1,"tags":["a","b"]}` || !r.Rows[0][2].Null || r.Rows[0][3].S != "running" || r.Rows[0][4].S != "1.99" {
		t.Errorf("row: %+v", r.Rows[0])
	}
}

// A row is found by its key, and written only while it holds what was read.
func TestSave(t *testing.T) {
	c, _ := seeded(t)
	ctx, e, w := context.Background(), Engine{}, db.NewWorker(c)
	save := func(table string, key []string, ch db.Changes) (int, error) {
		failed, _, err := db.Save(ctx, w, e, "main", table, key, ch, false)
		return failed, err
	}
	set := func(id, col, old, val string) db.Changes {
		return db.Changes{Updates: []db.Row{{Key: []string{id}, Cols: []db.Change{{Name: col, Val: db.Val{S: val}, Old: db.Val{S: old}}}}}}
	}
	if _, err := save("t_user", []string{"id"}, set("5", "email", "", "five@example.com")); !errors.Is(err, db.ErrStale) { // user 5's email is NULL, not ''
		t.Errorf("NULL read as '': %v", err)
	}
	up := db.Changes{Updates: []db.Row{{Key: []string{"5"}, Cols: []db.Change{{Name: "email", Val: db.Val{S: "five@example.com"}, Old: db.Val{Null: true}}}}}}
	if _, err := save("t_user", []string{"id"}, up); err != nil {
		t.Fatal(err)
	}
	eq(t, cells(q(t, c, "select email from t_user where id = 5")), []string{"five@example.com"})
	if failed, err := save("t_user", []string{"id"}, up); !errors.Is(err, db.ErrStale) || failed != 0 { // it holds another now
		t.Errorf("again: %d %v", failed, err)
	}
	// the old value is in the table more than once: one row changes, the one of the key
	q(t, c, "update t_user set name = 'twin' where id in (6, 7)")
	if _, err := save("t_user", []string{"id"}, set("7", "name", "twin", "seven")); err != nil {
		t.Fatal(err)
	}
	eq(t, cells(q(t, c, "select id, name from t_user where id in (6, 7) order by id")), []string{"6", "twin", "7", "seven"})
	// a unique constraint is the row's identity
	sku := db.Changes{Updates: []db.Row{{Key: []string{"sku_3"}, Cols: []db.Change{{Name: "title", Val: db.Val{S: "Third"}, Old: db.Val{S: "SKU 3"}}}}}}
	if _, err := save("t_sku", []string{"code"}, sku); err != nil {
		t.Fatal(err)
	}
	// the types a cell is written as, from text: a timestamp, a decimal, JSON, a list; a text the type does not take
	for _, ch := range []struct{ id, col, old, val string }{
		{"8", "created_at", "2026-01-09 00:00:00+00", "2030-02-03 04:05:06+00"},
		{"8", "name", "user_8", "x"},
	} {
		if _, err := save("t_user", []string{"id"}, set(ch.id, ch.col, ch.old, ch.val)); err != nil {
			t.Errorf("%v: %v", ch, err)
		}
	}
	if _, err := save("t_order", []string{"id"}, set("2", "amount", "2.99", "abc")); err == nil || e.ErrorOf(err).Code != "Conversion" {
		t.Errorf("a text that is no decimal: %v", err)
	}
	if _, err := save("t_order", []string{"id"}, set("2", "meta", `{"n":2,"tags":["a","b"]}`, `{"n":3}`)); err != nil {
		t.Errorf("json: %v", err)
	}
	// an INSERT leaving a column to its DEFAULT, a DELETE
	ins := db.Changes{Inserts: [][]db.Change{{{Name: "name", Val: db.Val{S: "new"}}, {Name: "active", Default: true}}}}
	if _, err := save("t_user", []string{"id"}, ins); err != nil {
		t.Fatal(err)
	}
	eq(t, cells(q(t, c, "select id, active from t_user where name = 'new'")), []string{"51", "true"})
	if _, err := save("t_user", []string{"id"}, db.Changes{Deletes: [][]string{{"51"}}}); err != nil {
		t.Fatal(err)
	}
	// a failure takes back the whole save
	bad := db.Changes{Updates: []db.Row{
		{Key: []string{"1"}, Cols: []db.Change{{Name: "name", Val: db.Val{S: "ALICE"}, Old: db.Val{S: "user_1"}}}},
		{Key: []string{"2"}, Cols: []db.Change{{Name: "name", Val: db.Val{Null: true}, Old: db.Val{S: "user_2"}}}}, // NOT NULL
	}}
	if failed, err := save("t_user", []string{"id"}, bad); err == nil || failed != 1 || e.ErrorOf(err).Code != "Constraint" {
		t.Fatalf("not null: %d %v", failed, err)
	}
	eq(t, cells(q(t, c, "select name from t_user where id = 1")), []string{"user_1"})
	if db.TxStatus(c) != 'I' {
		t.Errorf("tx %c", db.TxStatus(c))
	}
	// in a transaction of the user's: committed with it, no savepoint to take a failure back
	q(t, c, "begin")
	if _, err := save("t_user", []string{"id"}, set("9", "name", "user_9", "nine")); err != nil || db.TxStatus(c) != 'T' {
		t.Fatalf("in a transaction: %v %c", err, db.TxStatus(c))
	}
	q(t, c, "rollback")
	eq(t, cells(q(t, c, "select name from t_user where id = 9")), []string{"user_9"})
}

// Every type's text, as the grid shows it, finds the row it came from: Same.
func TestSameRoundTrip(t *testing.T) {
	c, _ := open(t)
	ctx, e := context.Background(), Engine{}
	q(t, c, `create table r(i int, d double, f float, n decimal(18,3), ts timestamp, tz timestamptz, dt date, tm time, iv interval, u uuid, l int[], st struct(a int, b varchar), mp map(varchar, int), s varchar, b bool, h hugeint, j json, e enum('a','b'), bi bit)`)
	q(t, c, `insert into r values (1, 0.1, 1.1, 12345.678, '2024-01-02 03:04:05.123456', '2024-01-02 03:04:05+08', '2024-01-02', '03:04:05.5', interval '1 day 2 hours 3 seconds',
		'3077b41a-1111-2222-3333-444455556666', [1, 2], {'a': 1, 'b': 'x y'}, map {'k': 1}, 'a''b "c" \', true, 170141183460469231731687303715884105727, '{"a":[1,2]}', 'b', '1011')`)
	q(t, c, `insert into r(i) values (2)`)
	cols := q(t, c, "select * from r where i = 1").Cols
	row := q(t, c, "select * from r where i = 1").Rows[0]
	nulls := q(t, c, "select * from r where i = 2").Rows[0]
	for i, col := range cols {
		for _, v := range []db.Val{row[i], nulls[i]} {
			where := e.Same(`"`+col.Name+`"`, "$1")
			r, err := c.Query(ctx, "select count(*) from r where "+where, v)
			if err != nil {
				t.Errorf("%s of %q: %v", col.Name, v.S, err)
				continue
			}
			if r.Rows[0][0].S != "1" {
				t.Errorf("%s (%s) %q: %s rows", col.Name, col.Type, v.S, r.Rows[0][0].S)
			}
		}
	}
	// and Eq: a key is found by its text
	for _, c1 := range []string{"i"} {
		if r := q(t, c, "select count(*) from r where "+e.Eq(c1, "$1"), db.Val{S: "1"}); r.Rows[0][0].S != "1" {
			t.Errorf("Eq %v", r.Rows)
		}
	}
	// an arm of Same goes with AND of its own: the parentheses keep it whole
	if r := q(t, c, "select count(*) from r where i = 1 and "+e.Same("s", "$1")+" or false", db.Val{S: "nope"}); r.Rows[0][0].S != "0" {
		t.Errorf("Same in parentheses: %v", r.Rows)
	}
	if !slices.Contains([]string{"("}, e.Same("a", "$1")[:1]) {
		t.Error("parentheses")
	}
}

func TestReadOnlyConnection(t *testing.T) {
	_, path := seeded(t)
	ctx := context.Background()
	ro, _, err := Connect(ctx, path, "", true) // a read-only connection of the program: the same instance, read only transactions
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for _, sql := range []string{"delete from t_log", "update t_user set name = 'x'", "create table z(i int)", "insert into t_log values (now(), 'x')"} {
		if _, err := ro.Query(ctx, sql); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	eq(t, cells(q(t, ro, "select count(*) from t_log")), []string{"3"})
}

// A save in a manual transaction whose second row is stale writes nothing: the
// library takes a row that is not there for no error, so taking the save back
// is the transaction's failure (review of F24.2).
func TestManualSaveIsAllOrNothing(t *testing.T) {
	c, _ := seeded(t)
	ctx, e, w := context.Background(), Engine{}, db.NewWorker(c)
	ch := db.Changes{Updates: []db.Row{
		{Key: []string{"1"}, Cols: []db.Change{{Name: "name", Val: db.Val{S: "ALICE"}, Old: db.Val{S: "user_1"}}}},
		{Key: []string{"2"}, Cols: []db.Change{{Name: "name", Val: db.Val{S: "BOB"}, Old: db.Val{S: "changed since"}}}},
	}}
	if _, _, err := db.Save(ctx, w, e, "main", "t_user", []string{"id"}, ch, true); !errors.Is(err, db.ErrStale) {
		t.Fatalf("the second row is stale: %v", err)
	}
	if db.TxStatus(c) != 'E' {
		t.Errorf("the transaction is to be rolled back: %c", db.TxStatus(c))
	}
	if r, err := c.Query(ctx, "commit"); err != nil || r.Tag != "ROLLBACK" { // a COMMIT of a failed transaction is its rollback
		t.Errorf("commit: %+v %v", r, err)
	}
	eq(t, cells(q(t, c, "select name from t_user where id in (1, 2) order by id")), []string{"user_1", "user_2"})
	if db.TxStatus(c) != 'I' {
		t.Errorf("after: %c", db.TxStatus(c))
	}
}

// A USE that leaves the file's database is undone: the grid's two connections
// would go by different ones, and a write by one name reach another file.
func TestUseStaysInTheDatabase(t *testing.T) {
	c, _ := seeded(t)
	ctx := context.Background()
	q(t, c, "attach ':memory:' as other")
	q(t, c, "create table other.t_user(id bigint primary key, name varchar)")
	q(t, c, "begin")
	_, err := c.Query(ctx, "use other")
	if err == nil || !strings.Contains(err.Error(), "database.schema.table") {
		t.Fatalf("use other: %v", err)
	}
	if db.TxStatus(c) != 'T' {
		t.Errorf("the transaction is as it was: %c", db.TxStatus(c))
	}
	eq(t, cells(q(t, c, "select current_database() = (select database_name from duckdb_tables() where table_name = 't_log')")), []string{"true"})
	eq(t, cells(q(t, c, "select count(*) from t_user")), []string{"50"})
	q(t, c, "use agentable") // a schema of its own is as it was
	eq(t, cells(q(t, c, "select count(*) from agent")), []string{"3"})
	q(t, c, "rollback")
}

// The file made by ?mode=rwc through a symlinked directory is one instance
// for the two connections, as it is once it is there.
func TestCreateThroughASymlink(t *testing.T) {
	UseLibrary(t)
	target, link := t.TempDir(), filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	main, _, err := Connect(ctx, filepath.Join(link, "new.duckdb")+"?mode=rwc", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer main.Close()
	meta, _, err := Connect(ctx, filepath.Join(link, "new.duckdb")+"?mode=rwc", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	q(t, main, "create table t(i int)")
	q(t, main, "insert into t values (5)")
	eq(t, cells(q(t, meta, "select i from t")), []string{"5"})
}

// Cancelled before it starts, a statement is not run; a raw result keeps maxRows
// rows and says there were more; an export stops at its cancel.
func TestCancelledBeforeAndLongResults(t *testing.T) {
	c, _ := seeded(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Query(ctx, "delete from t_log"); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled statement: %v", err)
	}
	eq(t, cells(q(t, c, "select count(*) from t_log")), []string{"3"})
	rs, err := c.Exec(context.Background(), "update t_sku set price = price returning code", 5)
	if err != nil || len(rs) != 1 || len(rs[0].Rows) != 5 || !rs[0].Truncated {
		t.Errorf("a raw result of 20 rows, 5 kept: %d %v %v", len(rs[0].Rows), rs[0].Truncated, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	n := 0
	err = c.Stream(ctx, "select * from t_order", func([]db.Col) error { return nil }, func([]db.Val) error {
		if n++; n == 10 {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || n != 10 {
		t.Errorf("an export cancelled at its 10th row: %d %v", n, err)
	}
}

func TestTransactionControlThatIsNotTaken(t *testing.T) {
	c, _ := open(t)
	q(t, c, "begin")
	if _, err := c.Query(context.Background(), "commit and chain"); err == nil {
		t.Fatal("COMMIT AND CHAIN is no DuckDB statement")
	}
	if db.TxStatus(c) != 'E' {
		t.Errorf("it left the transaction to be rolled back: %c", db.TxStatus(c))
	}
	q(t, c, "rollback")
}
