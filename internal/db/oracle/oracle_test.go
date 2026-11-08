//go:build integration

package oracle

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

func connect(t *testing.T) *Conn {
	t.Helper()
	c, _, err := Connect(context.Background(), IntegrationDSN(t), "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// Each value as the grid shows it (§8.11): NUMBER's exact digits, a binary
// double's shortest text, a CLOB's, RAW in hex, a DATE to the second, a
// TIMESTAMP's fraction without its trailing zeros, a zone as +02:00.
func TestValues(t *testing.T) {
	c := connect(t)
	r, err := c.Query(context.Background(), `select id, amount, ratio, note, raw_bytes, created_at, due_at, status,
12345678901234567890123456789.123, (select created_at from t_user where id = 2), interval '1 02:03:04' day to second
from t_order where id in (1, 2) order by id`)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]db.Val{
		{{S: "1"}, {S: "0.25"}, {S: "0"}, {S: "tab\tand\nnewline's"}, {S: "00FF"}, {S: "2026-01-01 00:00:00"}, {S: "2026-02-01 08:00:00+02:00"}, {S: "pending"},
			{S: "12345678901234567890123456789.123"}, {S: "2026-01-02 10:30:00"}, {S: "+01 02:03:04.000000"}},
		{{S: "2"}, {S: "0.35"}, {S: "0.25"}, {Null: true}, {Null: true}, {S: "2026-01-01 00:00:01"}, {Null: true}, {S: "running"},
			{S: "12345678901234567890123456789.123"}, {S: "2026-01-02 10:30:00"}, {S: "+01 02:03:04.000000"}},
	}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("rows\n%v\nwant\n%v", r.Rows, want)
	}
	if r.Cols[2].Type != "BINARY_DOUBLE" || r.Cols[6].Type != "TIMESTAMP WITH TIME ZONE" || r.Cols[3].Type != "CLOB" || r.Tag != "SELECT 2" {
		t.Errorf("cols %v, tag %q", r.Cols, r.Tag)
	}
	r, err = c.Query(context.Background(), "select :1, :2, case when :3 = 'it''s' then 'y' end from dual", db.Val{S: "it's"}, db.Val{Null: true}, db.Val{S: "it's"})
	if err != nil || !reflect.DeepEqual(r.Rows[0], []db.Val{{S: "it's"}, {Null: true}, {S: "y"}}) {
		t.Errorf("args: %v %v", r.Rows, err)
	}
}

// An error is its ORA- code, the message without it, and the character of the
// statement it points at (§11).
func TestErrors(t *testing.T) {
	c := connect(t)
	_, err := c.Query(context.Background(), "select nope from dual")
	se := Engine{}.ErrorOf(err)
	if se.Code != "ORA-00904" || !strings.Contains(se.Message, `"NOPE"`) || strings.Contains(se.Message, "ORA-") || strings.Contains(se.Message, "position") || se.Position != 8 {
		t.Errorf("%+v", se)
	}
	if (Engine{}).Broken(err) != db.NotLost {
		t.Error("a server's error is no lost connection")
	}
	if _, _, err := Connect(context.Background(), "oracle://u:p@localhost:1/x", "", false); err == nil || (Engine{}).Broken(err) == db.NotLost {
		t.Errorf("a refused dial: %v", err)
	}
}

// Statements one after the other, a PL/SQL block as one to its /; a query's
// rows capped at maxRows.
func TestExec(t *testing.T) {
	c := connect(t)
	rs, err := c.Exec(context.Background(), "select 1 from dual;\nbegin\n  null;\nend;\n/\nselect count(*) from t_user\n/\nselect level from dual connect by level <= 10;", 3)
	if err != nil || len(rs) != 4 || rs[2].Rows[0][0].S != "5" || len(rs[3].Rows) != 3 || !rs[3].Truncated {
		t.Fatalf("%v %v", rs, err)
	}
}

// Oracle begins a transaction at its first write; the driver's is begun by SET
// TRANSACTION and ended by COMMIT, ROLLBACK or a DDL, which commits with it
// (§8.11). Out of one, a write commits on its own.
func TestTransactions(t *testing.T) {
	name := OwnTable(t, "create table %s (id number primary key, v varchar2(10))", "insert into %s values (1, 'a')")
	c, other := connect(t), connect(t)
	ctx := context.Background()
	read := func() string {
		r, err := other.Query(ctx, "select v from "+name+" where id = 1")
		if err != nil {
			t.Fatal(err)
		}
		return r.Rows[0][0].S
	}
	if _, err := c.Query(ctx, (&Conn{}).Begin()); err != nil || c.TxStatus() != 'T' {
		t.Fatalf("begin: %v %c", err, c.TxStatus())
	}
	if _, err := c.Query(ctx, "update "+name+" set v = 'b' where id = 1"); err != nil {
		t.Fatal(err)
	}
	if read() != "a" {
		t.Error("an uncommitted write is seen by another session")
	}
	if _, err := c.Query(ctx, "rollback"); err != nil || c.TxStatus() != 'I' || read() != "a" {
		t.Errorf("rollback: %v %c %s", err, c.TxStatus(), read())
	}
	// a savepoint takes back what came after it alone, as db.Guarded asks
	_, _ = c.Query(ctx, "set transaction read write")
	_, _ = c.Query(ctx, "update "+name+" set v = 'c' where id = 1")
	err := db.Guarded(ctx, c, "sqlmux_save", func() error {
		_, _ = c.Query(ctx, "update "+name+" set v = 'd' where id = 1")
		return errors.New("fails")
	})
	if r, _ := c.Query(ctx, "select v from "+name+" where id = 1"); err == nil || r.Rows[0][0].S != "c" {
		t.Errorf("after the savepoint: %v %v", r.Rows, err)
	}
	// a DDL commits what came before it, and ends the transaction
	if _, err := c.Query(ctx, "comment on table "+name+" is 'x'"); err != nil || c.TxStatus() != 'I' || read() != "c" {
		t.Errorf("ddl: %v %c %s", err, c.TxStatus(), read())
	}
	// out of a transaction a write is its own
	if _, err := c.Query(ctx, "update "+name+" set v = 'e' where id = 1"); err != nil || read() != "e" {
		t.Errorf("autocommit: %v %s", err, read())
	}
}

// The tree's schemas, tables and the rest; what a table tab knows of a table:
// its key, a unique key over NOT NULL columns, what not to edit (§8.4, §10.1).
func TestCatalog(t *testing.T) {
	c := connect(t)
	ctx, e := context.Background(), Engine{}
	schemas, current, err := e.Schemas(ctx, c)
	if err != nil || !slices.Contains(schemas, "SQLMUX") || !slices.Contains(schemas, "SQLMUX_OTHER") || slices.Contains(schemas, "SYS") || current != "SQLMUX" {
		t.Errorf("schemas %v %q %v", schemas, current, err)
	}
	tables, err := e.Tables(ctx, c, []string{"SQLMUX", "SQLMUX_OTHER"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, tb := range tables {
		kinds[tb.Schema+"."+tb.Name] = tb.Kind
	}
	if kinds["SQLMUX.T_ORDER"] != "r" || kinds["SQLMUX.V_ACTIVE"] != "v" || kinds["SQLMUX_OTHER.T_NOTE"] != "r" {
		t.Errorf("kinds %v", kinds)
	}
	cols, err := e.TableColumns(ctx, c, "SQLMUX", "T_ORDER")
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, col := range cols.Cols {
		types[col.Name] = col.Type
	}
	want := map[string]string{"ID": "NUMBER(10)", "AMOUNT": "NUMBER(10,2)", "RATIO": "BINARY_DOUBLE", "NOTE": "CLOB", "RAW_BYTES": "RAW(4)",
		"CREATED_AT": "TIMESTAMP(3)", "DUE_AT": "TIMESTAMP(3) WITH TIME ZONE", "STATUS": "VARCHAR2(10)", "USER_ID": "NUMBER(10)"}
	if !reflect.DeepEqual(types, want) || !reflect.DeepEqual(cols.Key(), []string{"ID"}) {
		t.Errorf("types %v, key %v", types, cols.Key())
	}
	if items, _ := e.TableColumns(ctx, c, "SQLMUX", "T_ORDER_ITEM"); !reflect.DeepEqual(items.Key(), []string{"ORDER_ID", "LINE_NO"}) {
		t.Errorf("composite key %v", items.Key())
	}
	if log, _ := e.TableColumns(ctx, c, "SQLMUX", "T_LOG"); log.Key() != nil {
		t.Errorf("t_log has a key %v", log.Key())
	}
	ddl, err := e.DDL(ctx, c, "SQLMUX", "T_USER")
	if err != nil || !strings.Contains(ddl, `CREATE TABLE "SQLMUX"."T_USER"`) || strings.Contains(ddl, "TABLESPACE") {
		t.Errorf("ddl %q %v", ddl, err)
	}
	if v, err := e.DDL(ctx, c, "SQLMUX", "V_ACTIVE"); err != nil || !strings.Contains(v, "VIEW") {
		t.Errorf("view ddl %q %v", v, err)
	}
	idx, err := e.Indexes(ctx, c, "SQLMUX", "T_ORDER_ITEM")
	if err != nil || !slices.ContainsFunc(idx, func(i db.Index) bool { return i.Name == "IX_ORDER_ITEM_QTY" && i.Cols[0] == `"QTY"` && !i.Unique }) {
		t.Errorf("indexes %+v %v", idx, err)
	}
	keys, err := e.Keys(ctx, c, "SQLMUX", "T_ORDER_ITEM")
	if err != nil || len(keys) != 2 || keys[1].Ref == nil || keys[1].Ref.Table != "T_ORDER" || keys[1].Ref.Cols[0] != "ID" {
		t.Errorf("keys %+v %v", keys, err)
	}
	fs, err := e.Functions(ctx, c, []string{"SQLMUX"})
	if err != nil || len(fs) != 2 {
		t.Fatalf("functions %v %v", fs, err)
	}
	if def, err := e.FunctionDef(ctx, c, fs[0]); err != nil || !strings.Contains(def, "FUNCTION") || !strings.Contains(def, "DOUBLE_IT") {
		t.Errorf("function def %q %v", def, err)
	}
	if seqs, err := e.Sequences(ctx, c, nil); err != nil || !slices.Contains(seqs, db.Sequence{Schema: "SQLMUX", Name: "SEQ_ORDER"}) {
		t.Errorf("sequences %v %v", seqs, err)
	}
}

// A page and a count, OFFSET … FETCH as Oracle has them (§8.5).
func TestPage(t *testing.T) {
	c := connect(t)
	ctx := context.Background()
	src := db.SQLTable{E: Engine{}, Schema: "SQLMUX", Name: "T_ORDER", Meta: db.NewWorker(c)}
	cols, err := src.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := src.Page(ctx, db.PageQuery{Cols: cols, Filter: "status = 'done'", Order: []db.OrderCol{{Col: "AMOUNT", Desc: true}}, Limit: 3, Offset: 1})
	if err != nil || len(p.Rows) != 3 || !p.Next {
		t.Fatalf("page %v %v", p, err)
	}
	if n, err := src.Count(ctx, "status = 'done'"); err != nil || n != 1300 {
		t.Errorf("count %d %v", n, err)
	}
}

// A save is one transaction, as on PG (§10.3): each UPDATE carries the values
// read, each typed to its column; one that finds its row changed rolls all of
// it back.
func TestSave(t *testing.T) {
	name := OwnTable(t, `create table %s (id number(10) primary key, amount number(10,2) not null, ratio binary_double, note clob,
raw_bytes raw(4), d date, ts timestamp(3), tz timestamp(3) with time zone, v varchar2(20) default 'dflt')`,
		"insert into %s select id, amount, ratio, note, raw_bytes, cast(created_at as date), created_at, due_at, status from t_order where id <= 10")
	c := connect(t)
	ctx, e := context.Background(), Engine{}
	w := db.NewWorker(c)
	src := db.SQLTable{E: e, Schema: "SQLMUX", Name: name, Meta: w, Main: w}
	cols, err := src.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	src.Known = cols
	read := func(id string) []db.Val {
		r, err := c.Query(ctx, "select amount, ratio, note, raw_bytes, d, ts, tz, v from "+name+" where id = :1", db.Val{S: id})
		if err != nil || len(r.Rows) != 1 {
			t.Fatalf("read %s: %v %v", id, r.Rows, err)
		}
		return r.Rows[0]
	}
	names := []string{"AMOUNT", "RATIO", "NOTE", "RAW_BYTES", "D", "TS", "TZ", "V"}
	vals := []string{"12.5", "0.1", "it's a\tclob", "0A0B", "2026-05-06 07:08:09", "2026-05-06 07:08:09.123", "2026-05-06 07:08:09.5-03:30", "new"}
	old := read("1")
	upd := db.Row{Key: []string{"1"}}
	for i, n := range names {
		upd.Cols = append(upd.Cols, db.Change{Name: n, Val: db.Val{S: vals[i]}, Old: old[i]})
	}
	if failed, tx, err := src.Save(ctx, db.Changes{Updates: []db.Row{upd}}, false); err != nil || failed != -1 || tx != 'I' {
		t.Fatalf("save: %d %c %v", failed, tx, err)
	}
	got := read("1")
	for i, v := range vals {
		if got[i].S != v {
			t.Errorf("%s: %q, want %q", names[i], got[i].S, v)
		}
	}
	if failed, _, err := src.Save(ctx, db.Changes{Updates: []db.Row{upd}}, false); !errors.Is(err, db.ErrStale) || failed != 0 {
		t.Errorf("stale: %d %v", failed, err)
	}
	back := db.Row{Key: []string{"1"}}
	for i, n := range names {
		back.Cols = append(back.Cols, db.Change{Name: n, Val: old[i], Old: got[i]})
	}
	if _, _, err := src.Save(ctx, db.Changes{Updates: []db.Row{back}}, false); err != nil {
		t.Fatalf("back: %v", err)
	}
	if got := read("1"); !reflect.DeepEqual(got, old) {
		t.Errorf("not back\n%v\nwas\n%v", got, old)
	}
	// a delete that goes, then one of a row gone: all of it rolled back
	failed, _, err := src.Save(ctx, db.Changes{Deletes: [][]string{{"2"}, {"999"}}}, false)
	var written db.Written
	if failed != 1 || !errors.Is(err, db.ErrGone) || errors.As(err, &written) {
		t.Errorf("a delete of a row gone: %d %v", failed, err)
	}
	if r, _ := c.Query(ctx, "select count(*) from "+name+" where id = 2"); r.Rows[0][0].S != "1" {
		t.Error("the first delete stayed")
	}
	// an insert, and a row of DEFAULTs but its key
	ins := db.Changes{Inserts: [][]db.Change{{{Name: "ID", Val: db.Val{S: "100"}}, {Name: "AMOUNT", Val: db.Val{S: "1"}}}}}
	if _, _, err := src.Save(ctx, ins, false); err != nil || read("100")[7].S != "dflt" {
		t.Errorf("insert: %v", err)
	}
	// the manual mode: the save stays in the transaction
	if _, tx, err := src.Save(ctx, db.Changes{Deletes: [][]string{{"3"}}}, true); err != nil || tx != 'T' {
		t.Errorf("manual: %c %v", tx, err)
	}
	if _, err := c.Query(ctx, "rollback"); err != nil {
		t.Fatal(err)
	}
	if r, _ := c.Query(ctx, "select count(*) from "+name+" where id = 3"); r.Rows[0][0].S != "1" {
		t.Error("the manual save was not rolled back")
	}
}

// Quick SQL reads in a read-only transaction; a write does not go (§12).
func TestQuick(t *testing.T) {
	c := connect(t)
	ctx, w := context.Background(), db.NewWorker(c)
	r, err := Engine{}.Quick(ctx, w, "select id from t_order order by id", "SQLMUX", 5)
	if err != nil || len(r.Rows) != 5 || !r.Truncated {
		t.Errorf("read: %d %v", len(r.Rows), err)
	}
	if _, err := (Engine{}).Quick(ctx, w, "delete from t_user where id = 1", "", 5); err == nil {
		t.Error("quick SQL wrote")
	}
	if c.TxStatus() != 'I' {
		t.Error("quick SQL left its transaction open")
	}
	if _, err := (Engine{}).Quick(ctx, w, "select * from sqlmux_other.t_note", "", 5); err != nil {
		t.Errorf("another schema: %v", err)
	}
}

// A cancel stops the statement on the server; the connection, or a new one,
// goes on (§8.3).
func TestCancel(t *testing.T) {
	c := connect(t)
	w := db.NewWorker(c)
	w.Redial = func(ctx context.Context) (db.Conn, error) {
		n, _, err := Connect(ctx, IntegrationDSN(t), "", false)
		return n, err
	}
	w.Broken = Engine{}.Broken
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := w.Query(ctx, "select count(*) from all_objects a, all_objects b, all_objects c")
		done <- err
	}()
	time.Sleep(1500 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("not cancelled")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the cancel did not stop the request")
	}
	if r, err := w.Query(context.Background(), "select 1 from dual"); err != nil || r.Rows[0][0].S != "1" {
		t.Errorf("after the cancel: %v %v", r.Rows, err)
	}
}
