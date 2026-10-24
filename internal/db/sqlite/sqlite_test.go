package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
)

// seeded is a file of testdata/seed/sqlite.sql's tables, and a connection to
// it; no server to start, so these tests need no environment.
func seeded(t testing.TB) (*Conn, string) {
	t.Helper()
	script, err := os.ReadFile("../../../testdata/seed/sqlite.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	d.Close()
	c, _, err := Connect(context.Background(), path, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, path
}

func q(t testing.TB, c db.Conn, sql string) db.Result {
	t.Helper()
	r, err := c.Query(context.Background(), sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return r
}

func cells(r db.Result) []string {
	var out []string
	for _, row := range r.Rows {
		for _, v := range row {
			if v.Null {
				v.S = "NULL"
			}
			out = append(out, v.S)
		}
	}
	return out
}

// A path is the file: ~ is home, a relative one the start directory's; the
// driver's parameters go on, and a busy timeout when none says another.
func TestDSN(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	home, root := "/home/u", "/" // what ~ and an absolute path start from
	wd, _ := os.Getwd()
	wd = filepath.ToSlash(wd)
	if runtime.GOOS == "windows" { // UserHomeDir is %USERPROFILE%, /x the current drive's
		t.Setenv("USERPROFILE", `C:\Users\u`)
		home, root, wd = "/C:/Users/u", "/"+filepath.VolumeName(wd)+"/", "/"+wd
	}
	const busy = "_pragma=busy_timeout%285000%29"
	want := map[string]string{
		"~/x.db":             "file://" + home + "/x.db?" + busy + "&mode=rw",
		"x.db?mode=ro":       "file://" + wd + "/x.db?" + busy + "&mode=ro",
		"/t.db?mode=rwc":     "file://" + root + "t.db?" + busy + "&mode=rwc",
		"/a b/c#d.db":        "file://" + root + "a%20b/c%23d.db?" + busy + "&mode=rw",
		"/t.db?_pragma=a(1)": "file://" + root + "t.db?_pragma=a%281%29&" + busy + "&mode=rw", // a pragma of the user's keeps the timeout
		"/t.db?_pragma=busy_timeout(1)&_pragma=foreign_keys(1)": "file://" + root + "t.db?_pragma=busy_timeout%281%29&_pragma=foreign_keys%281%29&mode=rw",
		"~other/x.db": "file://" + wd + "/~other/x.db?" + busy + "&mode=rw", // not a home
	}
	if runtime.GOOS == "windows" {
		want[`~\x.db`] = "file://" + home + "/x.db?" + busy + "&mode=rw" // the other separator is taken too
	}
	for spec, want := range want {
		if _, got, err := DSN(spec); err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", spec, got, err, want)
		}
	}
}

// A file there is not is an error, no new empty database; mode=rwc makes one.
func TestConnectNeedsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	if _, _, err := Connect(context.Background(), path, "", false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a path that is none: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("it made the file")
	}
	c, _, err := Connect(context.Background(), path+"?mode=rwc", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	q(t, c, "create table t (a)")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("mode=rwc makes it: %v", err)
	}
}

// The catalog: tables and views by name, none of SQLite's own; every table's
// key and what stands in for it.
func TestCatalog(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	q(t, c, "create table t_seq (id integer primary key autoincrement, x)") // makes sqlite_sequence
	ss, cur, err := e.Schemas(ctx, c)
	if err != nil || !slices.Equal(ss, []string{"main"}) || cur != "main" {
		t.Fatalf("schemas %v %q %v", ss, cur, err)
	}
	ts, err := e.Tables(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tb := range ts {
		got = append(got, tb.Kind+":"+tb.Name)
	}
	if want := "r:ft r:t_aud r:t_bk r:t_ck r:t_desc r:t_dt r:t_free r:t_free_wr r:t_gen r:t_int r:t_kv r:t_log r:t_misc r:t_note r:t_order r:t_order_item r:t_part r:t_real r:t_rowid r:t_seq r:t_sku r:t_user v:v_order_total"; strings.Join(got, " ") != want {
		t.Fatalf("tables %v", got)
	}
	for _, tc := range []struct {
		table, key, unique, lead string
	}{
		{"t_user", "id", "", ""},
		{"t_order_item", "order_id,line_no", "", ""},
		{"t_sku", "code", "code|", ""}, // the nullable and the expression indexes can't identify a row
		{"t_log", "rowid", "", "rowid, "},
		{"t_kv", "k", "", ""},
		{"t_misc", "id", "", ""},
		{"v_order_total", "", "", ""},
	} {
		cols, err := e.TableColumns(ctx, c, "main", tc.table)
		if err != nil {
			t.Fatalf("%s: %v", tc.table, err)
		}
		if got := strings.Join(cols.Key(), ","); got != tc.key || cols.Lead != tc.lead {
			t.Errorf("%s: key %q lead %q, want %q %q (%+v)", tc.table, got, cols.Lead, tc.key, tc.lead, cols)
		}
	}
	cols, _ := e.TableColumns(ctx, c, "main", "t_sku")
	if len(cols.PK) != 0 || len(cols.Unique) != 1 || !slices.Equal(cols.Unique[0], []string{"code"}) {
		t.Errorf("t_sku: pk %v unique %v", cols.PK, cols.Unique)
	}
	cols, _ = e.TableColumns(ctx, c, "main", "t_log")
	if cols.Cols[0].Name != "rowid" || !cols.Cols[0].Generated || cols.Cols[1].Name != "at" || len(cols.Cols) != 3 {
		t.Errorf("t_log: the rowid first, nothing to set: %+v", cols.Cols)
	}
	cols, _ = e.TableColumns(ctx, c, "main", "t_misc")
	byName := map[string]db.Column{}
	for _, col := range cols.Cols {
		byName[col.Name] = col
	}
	if !byName["area"].Generated || byName["w"].Generated || byName["label"].Default != "'none'" || !byName["label"].AddOnly || byName["free"].Type != "" || byName["id"].Default != "rowid" {
		t.Errorf("t_misc: %+v", byName)
	}
	if ddl, err := e.DDL(ctx, c, "main", "v_order_total"); err != nil || !strings.HasPrefix(ddl, "CREATE VIEW v_order_total") || !strings.HasSuffix(ddl, ";") {
		t.Errorf("ddl %q %v", ddl, err)
	}
}

// What a statement gives is text, a REAL as SQLite writes it, a blob by its
// size, a NULL a NULL; the column types are the declared ones; a write has its
// rows hit.
func TestValues(t *testing.T) {
	c, _ := seeded(t)
	r := q(t, c, "select id, active, name, email, created_at from t_user where id = 2")
	if got := strings.Join(cells(r), "|"); got != "2|1|bob|NULL|2026-01-01 00:00:00" {
		t.Errorf("row %q", got)
	}
	if got := []db.Col{{Name: "id", Type: "integer"}, {Name: "active", Type: "integer"}, {Name: "name", Type: "text"}, {Name: "email", Type: "text"}, {Name: "created_at", Type: "text"}}; !slices.Equal(r.Cols, got) {
		t.Errorf("cols %v", r.Cols)
	}
	if r := q(t, c, "select 1 + 1 as two, 'x' || 'y'"); r.Cols[0].Type != "" || cells(r)[0] != "2" {
		t.Errorf("an expression: %+v", r)
	}
	if got := cells(q(t, c, "select raw from t_order where id = 1")); got[0] != db.BinaryShown(6) {
		t.Errorf("a blob: %q", got)
	}
	if got := cells(q(t, c, "select data from t_misc order by id")); got[0] != db.BinaryShown(2) || got[1] != "plain text" {
		t.Errorf("a blob, a blob that is text: %q", got)
	}
	// every REAL as SQLite writes it, by its own cast
	for _, f := range []string{"3.0", "0.1", "0.30000000000000004", "1e15", "1e14", "123456789012345.0", "1e22", "-2.5", "1e-5", "1.5e300", "0.0001", "100.0", "9007199254740993.0", "2.5e-10"} {
		got := cells(q(t, c, "select "+f+", cast("+f+" as text)"))
		if got[0] != got[1] {
			t.Errorf("%s: %s, SQLite's cast: %s", f, got[0], got[1])
		}
	}
	rs, err := c.Exec(context.Background(), "update t_user set active = 0 where id < 3", 0)
	if err != nil || rs[0].Affected != 2 || rs[0].Tag != "UPDATE 2" || rs[0].Cols != nil {
		t.Errorf("update: %+v %v", rs, err)
	}
	if r, err := c.Exec(context.Background(), "select * from t_order", 10); err != nil || len(r[0].Rows) != 10 || !r[0].Truncated || r[0].Tag != "SELECT 10" {
		t.Errorf("max rows: %d rows, truncated %v, %q %v", len(r[0].Rows), r[0].Truncated, r[0].Tag, err)
	}
	if r := q(t, c, "insert into t_log values ('x', 'y') returning rowid, line"); strings.Join(cells(r), "|") != "4|y" {
		t.Errorf("returning: %+v", r)
	}
	if r := q(t, c, "pragma table_info(t_log)"); len(r.Rows) != 2 {
		t.Errorf("pragma: %+v", r)
	}
}

// A page of a table by its key, the rowid one with the rowid; the count.
func TestPage(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	r, next, err := db.Page(ctx, c, e, db.Query{Schema: "main", Table: "t_order", Key: []string{"id"}, Limit: 500, Offset: 700, Where: "status = 'done'"})
	if err != nil || len(r.Rows) != 500 || next || r.Rows[0][0].S == "" {
		t.Fatalf("page: %d rows, next %v, %v", len(r.Rows), next, err)
	}
	if n, err := db.Count(ctx, c, e, db.Query{Schema: "main", Table: "t_order", Where: "status = 'done'"}); err != nil || n != 1200 {
		t.Errorf("count %d %v", n, err)
	}
	cols, _ := e.TableColumns(ctx, c, "main", "t_log")
	r, _, err = db.Page(ctx, c, e, db.Query{Schema: "main", Table: "t_log", Key: cols.Key(), Lead: cols.Lead, Limit: 10, Order: []db.OrderCol{{Col: "rowid", Desc: true}}})
	if err != nil || r.Cols[0].Name != "rowid" || strings.Join(cells(r)[:3], "|") != "3|2026-01-01 00:00:03|stopped" {
		t.Fatalf("t_log: %v %v", r.Cols, err)
	}
	if _, _, err := db.Page(ctx, c, e, db.Query{Schema: "main", Table: "t_order", Key: []string{"id"}, Limit: 5, Where: "nope = 1"}); err == nil {
		t.Error("a WHERE that does not hold is an error")
	}
}

// Saves: an UPDATE only of a row that still holds what was read, by key or by
// rowid, an INSERT that leaves a DEFAULT out, a DELETE; a failure takes the
// whole save back.
func TestSave(t *testing.T) {
	c, _ := seeded(t)
	ctx, e, w := context.Background(), Engine{}, db.NewWorker(c)
	save := func(table string, key []string, ch db.Changes) (int, error) {
		failed, _, err := db.Save(ctx, w, e, "main", table, key, ch, false)
		return failed, err
	}
	up := db.Changes{Updates: []db.Row{{Key: []string{"2"}, Cols: []db.Change{{Name: "email", Val: db.Val{S: "bob@example.com"}, Old: db.Val{Null: true}}}}}}
	if _, err := save("t_user", []string{"id"}, up); err != nil {
		t.Fatal(err)
	}
	if got := cells(q(t, c, "select email from t_user where id = 2")); got[0] != "bob@example.com" {
		t.Errorf("updated: %v", got)
	}
	if failed, err := save("t_user", []string{"id"}, up); !errors.Is(err, db.ErrStale) || failed != 0 { // it holds another now
		t.Errorf("again: %d %v", failed, err)
	}
	// a REAL as it was read is what it still is
	f := db.Changes{Updates: []db.Row{{Key: []string{"7"}, Cols: []db.Change{{Name: "amount", Val: db.Val{S: "1"}, Old: db.Val{S: "10.5"}}}}}}
	if _, err := save("t_order", []string{"id"}, f); err != nil {
		t.Errorf("a real: %v", err)
	}
	// the rowid: a table with nothing else to find a row by
	cols, _ := e.TableColumns(ctx, c, "main", "t_log")
	r := db.Changes{Updates: []db.Row{{Key: []string{"2"}, Cols: []db.Change{{Name: "line", Val: db.Val{S: "restarted"}, Old: db.Val{S: "started"}}}}}, Deletes: [][]string{{"3"}}}
	if _, err := save("t_log", cols.Key(), r); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cells(q(t, c, "select rowid, line from t_log order by rowid")), "|"); got != "1|started|2|restarted" {
		t.Errorf("t_log: %s", got)
	}
	// WITHOUT ROWID, by its primary key; an INSERT with a column left to its DEFAULT
	if _, err := save("t_kv", []string{"k"}, db.Changes{Updates: []db.Row{{Key: []string{"b"}, Cols: []db.Change{{Name: "v", Val: db.Val{S: "2"}, Old: db.Val{Null: true}}}}}}); err != nil {
		t.Fatal(err)
	}
	ins := db.Changes{Inserts: [][]db.Change{{{Name: "w", Val: db.Val{S: "6"}}, {Name: "h", Val: db.Val{S: "7"}}, {Name: "label", Default: true}}, {{Name: "label", Default: true}, {Name: "w", Val: db.Val{S: "1"}}, {Name: "h", Val: db.Val{S: "1"}}}}}
	if _, err := save("t_misc", []string{"id"}, ins); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cells(q(t, c, "select id, area, label from t_misc where id > 2")), "|"); got != "3|42|none|4|1|none" {
		t.Errorf("inserted: %s", got)
	}
	// all DEFAULT
	if _, err := save("t_log", cols.Key(), db.Changes{Inserts: [][]db.Change{{}}}); err == nil {
		t.Error("a log line is NOT NULL: DEFAULT VALUES is no row of it")
	}
	// a failure takes back the whole save
	bad := db.Changes{Updates: []db.Row{
		{Key: []string{"1"}, Cols: []db.Change{{Name: "name", Val: db.Val{S: "ALICE"}, Old: db.Val{S: "alice"}}}},
		{Key: []string{"2"}, Cols: []db.Change{{Name: "name", Val: db.Val{Null: true}, Old: db.Val{S: "bob"}}}}, // NOT NULL
	}}
	if failed, err := save("t_user", []string{"id"}, bad); err == nil || failed != 1 {
		t.Fatalf("not null: %d %v", failed, err)
	}
	if got := cells(q(t, c, "select name from t_user where id = 1")); got[0] != "alice" || c.TxStatus() != 'I' {
		t.Errorf("rolled back: %v, tx %c", got, c.TxStatus())
	}
}

// BEGIN, COMMIT, END and ROLLBACK are seen; a Meta connection is read only.
func TestTransactionAndReadOnly(t *testing.T) {
	c, path := seeded(t)
	for _, s := range []struct {
		sql  string
		want byte
	}{{"begin", 'T'}, {"insert into t_log values ('a','b')", 'T'}, {"end", 'I'}, {"begin immediate", 'T'}, {"rollback", 'I'}, {"begin transaction", 'T'}, {"commit transaction", 'I'}} {
		q(t, c, s.sql)
		if got := c.TxStatus(); got != s.want {
			t.Fatalf("after %q: %c, want %c", s.sql, got, s.want)
		}
	}
	m, _, err := Connect(context.Background(), path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Query(context.Background(), "insert into t_log values ('a','b')"); err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Errorf("a write on a read-only connection: %v", err)
	}
	if r := q(t, m, "select count(*) from t_log"); r.Rows[0][0].S != "4" { // the one committed above, seen from the other connection
		t.Errorf("count %v", r.Rows)
	}
	ro, _, err := Connect(context.Background(), path+"?mode=ro", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	_, err = ro.Query(context.Background(), "delete from t_log")
	if err == nil {
		t.Fatal("mode=ro takes no write")
	}
	if se := (Engine{}).ErrorOf(err); se.Severity != "ERROR" || se.Message == "" || se.Code == "" {
		t.Errorf("error %+v", se)
	}
}

// A statement cancelled stops, and the connection goes on; one cancelled before it ran does not.
func TestCancel(t *testing.T) {
	c, _ := seeded(t)
	w := db.NewWorker(c)
	time.AfterFunc(200*time.Millisecond, w.Cancel)
	start := time.Now()
	_, err := w.Query(context.Background(), "with recursive n (i) as (select 1 union all select i + 1 from n) select count(*) from n")
	if !errors.Is(err, context.Canceled) || time.Since(start) > 3*time.Second {
		t.Fatalf("cancelled: %v after %v", err, time.Since(start))
	}
	if r, err := w.Query(context.Background(), "select count(*) from t_user"); err != nil || r.Rows[0][0].S != "3" {
		t.Errorf("after: %v %v", r.Rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Query(ctx, "insert into t_log values ('a', 'b')"); !errors.Is(err, context.Canceled) {
		t.Errorf("before: %v", err)
	}
	if r := q(t, c, "select count(*) from t_log"); r.Rows[0][0].S != "3" {
		t.Errorf("it did not run: %v", r.Rows)
	}
}

// The rows come as they are read, a blob as a file has it; a stop leaves the rest.
func TestStream(t *testing.T) {
	c, _ := seeded(t)
	var cols []db.Col
	var rows []string
	err := c.Stream(context.Background(), "select id, raw from t_order where id <= 2", func(c []db.Col) error { cols = c; return nil }, func(v []db.Val) error {
		rows = append(rows, v[0].S+":"+v[1].S+map[bool]string{true: "NULL"}[v[1].Null])
		return nil
	})
	if err != nil || len(cols) != 2 || strings.Join(rows, " ") != "1:X'DEADBEEF00FF' 2:NULL" {
		t.Errorf("stream: %v %q %v", cols, rows, err)
	}
	stop := errors.New("enough")
	n := 0
	if err := c.Stream(context.Background(), "select id from t_order", func([]db.Col) error { return nil }, func([]db.Val) error {
		if n++; n == 3 {
			return stop
		}
		return nil
	}); !errors.Is(err, stop) || n != 3 {
		t.Errorf("stop: %v after %d", err, n)
	}
}

// Quick SQL reads in a transaction it takes back, limited to what comes back.
func TestQuick(t *testing.T) {
	c, path := seeded(t)
	m, _, err := Connect(context.Background(), path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	w := db.NewWorker(m)
	defer w.Close()
	_ = c
	r, err := Engine{}.Quick(context.Background(), w, "select id from t_order order by id", "main", 20)
	if err != nil || len(r.Rows) != 20 || !r.Truncated || m.TxStatus() != 'I' {
		t.Fatalf("quick: %d rows, truncated %v, tx %c, %v", len(r.Rows), r.Truncated, m.TxStatus(), err)
	}
	if _, err := (Engine{}).Quick(context.Background(), w, "delete from t_log", "main", 20); err == nil {
		t.Error("quick SQL does not write")
	}
}

// A table's indexes (those of a PRIMARY KEY or UNIQUE of the table left out) and
// keys: the primary key, then the foreign keys, a column left out of one
// being the parent's primary key (F14.3).
func TestObjects(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	text := func(ixs []db.Index) (out []string) {
		for _, ix := range ixs {
			out = append(out, ix.Name+"("+strings.Join(ix.Cols, ",")+")"+map[bool]string{true: "U"}[ix.Unique])
		}
		return out
	}
	for table, want := range map[string]string{
		"t_sku":        "t_sku_barcode(barcode)U t_sku_code(code)U t_sku_lower_title(expr)U",
		"t_order":      "t_order_user_status(user_id,status)",
		"t_order_item": "", // its primary key is an autoindex
		"t_kv":         "",
	} {
		ixs, err := e.Indexes(ctx, c, "main", table)
		if err != nil || strings.Join(text(ixs), " ") != want {
			t.Errorf("%s: %v %v, want %s", table, text(ixs), err, want)
		}
	}
	ixs, _ := e.Indexes(ctx, c, "main", "t_order")
	if ixs[0].Def != "CREATE INDEX t_order_user_status on t_order (user_id, status);" {
		t.Errorf("def %q", ixs[0].Def)
	}
	keys := func(table string) (out []string) {
		ks, err := e.Keys(ctx, c, "main", table)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range ks {
			s := k.Name + "(" + strings.Join(k.Cols, ",") + ")"
			if k.Ref != nil {
				s += "->" + k.Ref.Schema + "." + k.Ref.Table + "(" + strings.Join(k.Ref.Cols, ",") + ")"
			}
			out = append(out, s)
		}
		return out
	}
	for table, want := range map[string]string{
		"t_order_item": "PRIMARY(order_id,line_no) (order_id)->main.t_order(id)",
		"t_note":       "PRIMARY(id) (user_id)->main.t_user(id)",
		"t_log":        "",
		"t_kv":         "PRIMARY(k)",
	} {
		if got := strings.Join(keys(table), " "); got != want {
			t.Errorf("%s: %q, want %q", table, got, want)
		}
	}
	if fs, err := e.Functions(ctx, c, nil); err != nil || fs != nil {
		t.Errorf("functions %v %v", fs, err)
	}
}

// A REAL is shown as the shortest digits that read back, a point always, an
// exponent from 1e17 and under 1e-4, Inf and a zero as SQLite writes them.
func TestRealText(t *testing.T) {
	tenth := 0.1 // not a constant sum, which is exactly 0.3
	for f, want := range map[float64]string{
		1.0 / 7: "0.14285714285714285", tenth + 0.2: "0.30000000000000004", 3: "3.0", 100: "100.0", -2.5: "-2.5",
		1e15: "1000000000000000.0", 1e16: "10000000000000000.0", 9.99e16: "99900000000000000.0", 1e17: "1.0e+17", 1.5e300: "1.5e+300",
		1e-4: "0.0001", 1e-5: "1.0e-05", 2.5e-10: "2.5e-10", 123456789.123456789: "123456789.12345679",
		0: "0.0", math.Copysign(0, -1): "0.0", math.Inf(1): "Inf", math.Inf(-1): "-Inf",
	} {
		if got := realText(f); got != want {
			t.Errorf("%v: %s, want %s", f, got, want)
		}
	}
}

// What the driver reads is what the grid shows: a time as the standard text, a
// blob by its size whatever its bytes, a text that is no UTF-8 too, a REAL in
// all its digits; and the file has them as literals (F14.1).
func TestValueKinds(t *testing.T) {
	c, _ := seeded(t)
	got := cells(q(t, c, "select at, d, ts from t_dt order by id"))
	if want := []string{"2026-01-02 03:04:05", "2026-01-02 00:00:00", "2026-01-02 03:04:05.250", "2026-01-02 03:04:05", "NULL", "NULL"}; !slices.Equal(got, want) {
		t.Errorf("times %q, want %q", got, want)
	}
	if got := cells(q(t, c, "select cast('hi' as blob), x'0001', cast(x'ff' as text), 'ok', x'', typeof(cast('hi' as blob))")); !slices.Equal(got, []string{db.BinaryShown(2), db.BinaryShown(2), db.BinaryShown(1), "ok", db.BinaryShown(0), "blob"}) {
		t.Errorf("blobs %q", got)
	}
	if got := cells(q(t, c, "select x from t_real order by id")); !slices.Equal(got, []string{"0.14285714285714285", "0.30000000000000004", "10000000000000000.0", "1.0e+17", "123456789.12345679", "1.0e-05", "100.0"}) {
		t.Errorf("reals %q", got)
	}
	var rows []string
	if err := c.Stream(context.Background(), "select cast('hi' as blob), cast(x'ff' as text), 'ok'", func([]db.Col) error { return nil }, func(v []db.Val) error {
		rows = append(rows, v[0].S, v[1].S, v[2].S)
		return nil
	}); err != nil || !slices.Equal(rows, []string{"X'6869'", "X'FF'", "ok"}) {
		t.Errorf("a file's: %q %v", rows, err)
	}
}

// Statements that write say how many rows, those that do not do not ask: a DDL
// hits none, though the INSERT before it hit some; a WITH that writes counts.
func TestAffected(t *testing.T) {
	c, _ := seeded(t)
	q(t, c, "insert into t_log values ('a', 'b'), ('c', 'd')")
	for _, tc := range []struct{ sql, want string }{
		{"create table t_x (a)", "CREATE|0"},
		{"insert into t_x values (1), (2), (3)", "INSERT 3|3"},
		{"with n as (select 1) insert into t_x select * from n", "WITH 1|1"},
		{"with n as (select 1) select * from n", "WITH 1|0"},
		{"update t_x set a = a", "UPDATE 4|4"},
		{"drop table t_x", "DROP|0"},
		{"pragma foreign_keys", "PRAGMA 1|0"},
	} {
		r := q(t, c, tc.sql)
		if got := fmt.Sprintf("%s|%d", r.Tag, r.Affected); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.sql, got, tc.want)
		}
	}
}

// The rows are found by the key as it was read: a key with no declared type
// holds an integer the text '1' is, a text, and 'abc' does not match the 0; the
// same in a WITHOUT ROWID table; a row of a REAL, a time, a changed value is
// the row it was, by number or by julianday, and a change under it is stale (F14.1).
func TestSaveKeysAndSame(t *testing.T) {
	c, _ := seeded(t)
	ctx, e, w := context.Background(), Engine{}, db.NewWorker(c)
	set := func(table, key, col, old, val string) error {
		_, _, err := db.Save(ctx, w, e, "main", table, []string{keyOf(table)}, db.Changes{Updates: []db.Row{{Key: []string{key}, Cols: []db.Change{{Name: col, Val: db.Val{S: val}, Old: db.Val{S: old}}}}}}, false)
		return err
	}
	for _, table := range []string{"t_free", "t_free_wr"} {
		if err := set(table, "1", "v", "one", "ONE"); err != nil {
			t.Errorf("%s: an integer key, as text: %v", table, err)
		}
		if err := set(table, "two", "v", "two", "TWO"); err != nil {
			t.Errorf("%s: a text key: %v", table, err)
		}
		if err := set(table, "0", "v", "zero", "ZERO"); err != nil {
			t.Errorf("%s: the key 0: %v", table, err)
		}
		if err := set(table, "abc", "v", "zero", "x"); !errors.Is(err, db.ErrStale) {
			t.Errorf("%s: 'abc' is no 0: %v", table, err)
		}
		if got := strings.Join(cells(q(t, c, "select v from "+table+" order by v")), " "); got != "ONE TWO ZERO" {
			t.Errorf("%s: %s", table, got)
		}
	}
	for id, x := range map[string]string{"1": "0.14285714285714285", "2": "0.30000000000000004", "3": "10000000000000000.0", "5": "123456789.12345679", "6": "1.0e-05"} {
		if err := set("t_real", id, "x", x, "2"); err != nil {
			t.Errorf("a real %s as shown: %v", x, err)
		}
	}
	if err := set("t_real", "4", "x", "1.0e+16", "2"); !errors.Is(err, db.ErrStale) {
		t.Errorf("a real that is another: %v", err)
	}
	for id, at := range map[string]string{"1": "2026-01-02 03:04:05", "2": "2026-01-02 03:04:05"} { // one written with a T and a Z
		if err := set("t_dt", id, "at", at, "2027-01-01 00:00:00"); err != nil {
			t.Errorf("a time %s as shown: %v", id, err)
		}
	}
	if err := set("t_dt", "1", "ts", "2026-01-02 03:04:05", "x"); !errors.Is(err, db.ErrStale) {
		t.Errorf("a time that is another: %v", err)
	}
}

// A REAL is the same row by number: of random ones, some the text SQLite casts
// to is not the shortest digits the grid shows, and all of them are found by
// their shown text (F14.1).
func TestSameRealsByNumber(t *testing.T) {
	c, _ := seeded(t)
	ctx, e, w := context.Background(), Engine{}, db.NewWorker(c)
	rng := rand.New(rand.NewPCG(1, 2))
	q(t, c, "create table t_rnd (id integer primary key, x real)")
	for i := range 300 {
		if _, err := c.Query(ctx, "insert into t_rnd (id, x) values (?1, ?2)", db.Val{S: strconv.Itoa(i)}, db.Val{S: strconv.FormatFloat(rng.Float64()*math.Pow(10, float64(rng.IntN(20))), 'g', -1, 64)}); err != nil {
			t.Fatal(err)
		}
	}
	differs := 0
	for i := range 300 {
		r := q(t, c, "select x, cast(x as text) from t_rnd where id = "+strconv.Itoa(i))
		shown, cast := r.Rows[0][0].S, r.Rows[0][1].S
		if shown != cast {
			differs++
		}
		_, _, err := db.Save(ctx, w, e, "main", "t_rnd", []string{"id"}, db.Changes{Updates: []db.Row{{Key: []string{strconv.Itoa(i)}, Cols: []db.Change{{Name: "x", Val: db.Val{S: "1"}, Old: db.Val{S: shown}}}}}}, false)
		if err != nil {
			t.Fatalf("%s as shown (SQLite's text %s): %v", shown, cast, err)
		}
	}
	if differs == 0 {
		t.Error("no value of which SQLite's text is not the shown one: the test has no teeth")
	}
}

func keyOf(table string) string {
	return map[string]string{"t_free": "k", "t_free_wr": "k", "t_real": "id", "t_dt": "id"}[table]
}

// The catalog of what SQLite does its own way: a partial unique index is no
// key; a blob key makes the table read only; a primary key by its place in the
// declaration; the rowid's alias only an INTEGER key with an index of none; a
// virtual table has no hidden columns and no shadow tables; generated columns
// and the rowid are not to edit (F14.1).
func TestCatalogSQLiteWays(t *testing.T) {
	c, _ := seeded(t)
	ctx, e := context.Background(), Engine{}
	cols := func(table string) db.Columns {
		t.Helper()
		cs, err := e.TableColumns(ctx, c, "main", table)
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	if cs := cols("t_part"); len(cs.Unique) != 0 || cs.Lead != "rowid, " {
		t.Errorf("t_part: a partial index is no key: %+v", cs)
	}
	if cs := cols("t_bk"); strings.Join(cs.Key(), ",") != "id" || !strings.Contains(cs.ReadOnly, "id") || cs.Cols[0].Default != "" {
		t.Errorf("t_bk: %+v", cs)
	}
	if cs := cols("t_ck"); strings.Join(cs.PK, ",") != "a,b" || cs.Cols[0].Name != "b" {
		t.Errorf("t_ck: the key in its own order: %+v", cs.PK)
	}
	// a rowid table's primary key that may be NULL finds no row by =: the rowid is the identity
	q(t, c, "create table t_nullpk (a text, b text not null, primary key (a, b))")
	q(t, c, "create table t_nullpk1 (a text primary key, b text)")
	q(t, c, "create table t_nullpk_wr (a text primary key, b text) without rowid")
	for table, want := range map[string]string{"t_nullpk": "rowid", "t_nullpk1": "rowid", "t_nullpk_wr": "a", "t_user": "id"} {
		if cs := cols(table); strings.Join(cs.Key(), ",") != want {
			t.Errorf("%s: the key is %v, not %s", table, cs.Key(), want)
		}
	}
	for table, alias := range map[string]bool{"t_user": true, "t_misc": true, "t_int": false, "t_desc": false, "t_kv": false, "t_free": false} {
		cs := cols(table)
		got := false
		for _, col := range cs.Cols {
			got = got || col.Default == "rowid"
		}
		if got != alias {
			t.Errorf("%s: an alias of the rowid: %v", table, got)
		}
	}
	if cs := cols("ft"); len(cs.Cols) != 1 || cs.Cols[0].Name != "body" { // no ft or rank, which are hidden
		t.Errorf("ft: %+v", cs.Cols)
	}
	gen := cols("t_gen")
	byName := map[string]db.Column{}
	for _, col := range gen.Cols {
		byName[col.Name] = col
	}
	if byName["b"].ReadOnly == "" || byName["c"].ReadOnly == "" || byName["a"].ReadOnly != "" || !byName["b"].Generated {
		t.Errorf("t_gen: %+v", byName)
	}
	if cs := cols("t_log"); cs.Cols[0].Name != "rowid" || cs.Cols[0].ReadOnly == "" {
		t.Errorf("the rowid is not to edit: %+v", cs.Cols[0])
	}
	if cs := cols("t_rowid"); cs.Lead != "" || cs.Key() != nil { // a column of that name hides the rowid
		t.Errorf("t_rowid: %+v", cs)
	}
}

// SQLite takes a transaction back by itself on some errors: TxStatus says so
// when it does, and not when a statement fails and the transaction goes on
// (F14.1).
func TestTxStatusFollowsSQLite(t *testing.T) {
	c, _ := seeded(t)
	ctx := context.Background()
	q(t, c, "begin")
	if _, err := c.Query(ctx, "insert into t_user (id, name) values (1, 'dup')"); err == nil || c.TxStatus() != 'T' {
		t.Fatalf("a constraint: %v, tx %c: the transaction goes on", err, c.TxStatus())
	}
	if _, err := c.Query(ctx, "insert or rollback into t_user (id, name) values (1, 'dup')"); err == nil || c.TxStatus() != 'I' {
		t.Errorf("or rollback: %v, tx %c", err, c.TxStatus())
	}
	q(t, c, "begin")
	q(t, c, "create trigger t_raise before insert on t_log begin select raise(rollback, 'no'); end")
	if _, err := c.Query(ctx, "insert into t_log values ('a', 'b')"); err == nil || c.TxStatus() != 'I' {
		t.Errorf("raise(rollback): %v, tx %c", err, c.TxStatus())
	}
	q(t, c, "begin")
	cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	q(t, c, "insert into t_user (id, name) values (100, 'x')")
	if _, err := c.Query(cctx, "with recursive n (i) as (select 1 union all select i + 1 from n) insert into t_log select 'a', i from n"); err == nil {
		t.Fatal("an endless insert ends")
	}
	if c.TxStatus() != 'I' { // an INSERT interrupted takes the whole transaction back
		t.Errorf("interrupted: tx %c", c.TxStatus())
	}
}

// A trigger's statement is one statement, executed whole; the VALUES of quick
// SQL is read as it is, with no LIMIT after it; the shadow tables of a virtual
// one are not listed (F14.1).
func TestTriggerValuesAndShadows(t *testing.T) {
	c, path := seeded(t)
	q(t, c, "insert into t_aud values (7)")
	if r := q(t, c, "select line from t_log where line like 'row %'"); strings.Join(cells(r), "") != "row 7" {
		t.Errorf("the trigger ran: %v", cells(r))
	}
	m, _, err := Connect(context.Background(), path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	w := db.NewWorker(m)
	if r, err := (Engine{}).Quick(context.Background(), w, "values (1), (2), (3)", "main", 2); err != nil || len(r.Rows) != 2 || !r.Truncated { // read whole, no LIMIT to syntax-error, and cut at the end
		t.Errorf("values: %d rows %v %v", len(r.Rows), r.Truncated, err)
	}
	ts, _ := (Engine{}).Tables(context.Background(), c, nil)
	for _, tb := range ts {
		if strings.HasPrefix(tb.Name, "ft_") {
			t.Errorf("a shadow table: %s", tb.Name)
		}
	}
}

// A row is found by its key through an index, not a scan of the table: on a
// TEXT key, an untyped unique key and the INTEGER PRIMARY KEY (F14.1).
func TestEqUsesTheIndex(t *testing.T) {
	c, _ := seeded(t)
	for _, ddl := range []string{
		"create table eq_text (k text primary key, v) without rowid",
		"create table eq_free (id integer primary key, k unique, v)",
		"create table eq_int (id integer primary key, v)",
	} {
		q(t, c, ddl)
	}
	for table, key := range map[string]string{"eq_text": "k", "eq_free": "k", "eq_int": "id"} {
		r, err := c.Query(context.Background(), "explain query plan select * from "+table+" where "+Engine{}.Eq(key, "?1"), db.Val{S: "7"})
		if err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(cells(r), " ")
		if !strings.Contains(plan, "SEARCH") || strings.Contains(plan, "SCAN") {
			t.Errorf("%s: %s", table, plan)
		}
	}
}

// A foreign key of two columns is one key, its columns in the key's order, not
// the table's; the primary key in its own order (F14.3).
func TestKeysOrder(t *testing.T) {
	c, _ := seeded(t)
	e := Engine{}
	q(t, c, "create table ord_p (x, y, primary key (y, x))")
	q(t, c, "create table ord_c (a, b, foreign key (b, a) references ord_p (y, x), foreign key (a) references ord_p)")
	ks, err := e.Keys(context.Background(), c, "main", "ord_c")
	if err != nil || len(ks) != 2 {
		t.Fatalf("keys %+v %v", ks, err)
	}
	for _, k := range ks {
		if strings.Join(k.Cols, ",") == "b,a" && strings.Join(k.Ref.Cols, ",") != "y,x" || k.Cols[0] == "a" && len(k.Cols) != 1 {
			t.Errorf("a key: %+v %+v", k, k.Ref)
		}
	}
	if ks, err = e.Keys(context.Background(), c, "main", "ord_p"); err != nil || len(ks) != 1 || strings.Join(ks[0].Cols, ",") != "y,x" {
		t.Errorf("primary key %+v %v", ks, err)
	}
}
