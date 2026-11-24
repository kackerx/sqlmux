//go:build integration

package duckdb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
)

// open is a connection to a new file, mode=rwc.
func open(t testing.TB) (*Conn, string) {
	t.Helper()
	UseLibrary(t)
	path := filepath.Join(t.TempDir(), "t.duckdb")
	conn, _, err := Connect(context.Background(), path+"?mode=rwc", "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, path
}

func q(t testing.TB, c db.Conn, sql string, args ...db.Val) db.Result {
	t.Helper()
	r, err := c.Query(context.Background(), sql, args...)
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

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// The texts of the types are those of the CLI's (duckdb -list), 2.0.1.
func TestTypes(t *testing.T) {
	c, _ := open(t)
	q(t, c, `create table t(tags integer[], st struct(b varchar, a int), mp map(varchar,int), d decimal(18,3), ts timestamptz, u uuid, iv interval, h hugeint, f double, nested struct(x integer[], y struct(z int)), un union(i int, s varchar), tz timetz, tm time, bit bit, e enum('a','b'), fl float, s varchar, n varchar)`)
	q(t, c, `insert into t values ([1,2],{'b':'x','a':1},map {'k':1},12345.678,'2024-01-02 03:04:05+08','3077b41a-1111-2222-3333-444455556666',interval '1 day 2 hours',170141183460469231731687303715884105727,'NaN'::double,{'x':[1,2],'y':{'z':3}},union_value(s:='hi'),'12:00:00+02'::timetz,'03:04:05.123456','101'::bit,'a',1.1,'NULL',NULL)`)
	eq(t, cells(q(t, c, "select * from t")), []string{"[1, 2]", "{'b': x, 'a': 1}", "{k=1}", "12345.678", "2024-01-02 03:04:05+08",
		"3077b41a-1111-2222-3333-444455556666", "1 day 02:00:00", "170141183460469231731687303715884105727", "nan",
		"{'x': [1, 2], 'y': {'z': 3}}", "hi", "12:00:00+02", "03:04:05.123456", "101", "a", "1.1", "NULL", "NULL"})
	r := q(t, c, "select s, n from t")
	if r.Rows[0][0].Null || !r.Rows[0][1].Null {
		t.Errorf("the string 'NULL' and NULL are told apart: %+v", r.Rows[0])
	}
	q(t, c, "set TimeZone='America/New_York'")
	eq(t, cells(q(t, c, "select ts from t")), []string{"2024-01-01 14:04:05-05"})
	r = q(t, c, "select d, tags, ts from t")
	var types []string
	for _, col := range r.Cols {
		types = append(types, col.Type)
	}
	eq(t, types, []string{"decimal(18,3)", "integer[]", "timestamp with time zone"})
	eq(t, cells(q(t, c, "select [1.5, null], {'a': null}, 1e20::double, ['a b', 'c''d']")), []string{"[1.5, NULL]", "{'a': NULL}", "1e+20", "[a b, 'c\\'d']"})
	// the name a statement gives twice stays, where the wrapped one is a_1
	r = q(t, c, "select 1 as a, 2 as a, null::int as n")
	if r.Cols[0].Name != "a" || r.Cols[1].Name != "a" || r.Cols[2].Name != "n" {
		t.Errorf("columns %+v", r.Cols)
	}
	r = q(t, c, "select * from range(0) t(a)")
	if len(r.Rows) != 0 || len(r.Cols) != 1 {
		t.Errorf("an empty result keeps its columns: %+v", r)
	}
}

func TestBlob(t *testing.T) {
	c, _ := open(t)
	q(t, c, `create table b(x blob)`)
	q(t, c, `insert into b values ('\x5C'::blob), ('\x00\x41abc\x0A"'' \x7F'::blob), ('\xE4\xBD\xA0'::blob), (''::blob), (null)`)
	eq(t, cells(q(t, c, "select x from b")), []string{"<binary 1 bytes>", "<binary 10 bytes>", "<binary 3 bytes>", "<binary 0 bytes>", "NULL"})
	var got []string
	err := c.Stream(context.Background(), "select x from b limit 2", func([]db.Col) error { return nil }, func(v []db.Val) error { got = append(got, v[0].S); return nil })
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, []string{`'\x5C'::BLOB`, `'\x00Aabc\x0A\x22\x27 \x7F'::BLOB`})
	// a list of blobs is a list, not a blob
	q(t, c, `create table bl(x blob[])`)
	q(t, c, `insert into bl values (['\xAA'::blob, 'b'::blob])`)
	got = nil
	if err := c.Stream(context.Background(), "select x from bl", func([]db.Col) error { return nil }, func(v []db.Val) error { got = append(got, v[0].S); return nil }); err != nil || len(got) != 1 || strings.Contains(got[0], "BLOB") {
		t.Errorf("a list of blobs: %q %v", got, err)
	}
	got = []string{`'\x5C'::BLOB`, `'\x00Aabc\x0A\x22\x27 \x7F'::BLOB`}
	// the literal makes the bytes again
	q(t, c, "create table b2 as select "+got[1]+" as x")
	eq(t, cells(q(t, c, "select octet_length(x) from b2")), []string{"10"})
}

// What a statement answers with, by what it is (§8.1).
func TestShapes(t *testing.T) {
	c, _ := open(t)
	r := q(t, c, "create table t(id int primary key, v varchar, l int[])")
	if r.Tag != "CREATE" || len(r.Cols) != 0 {
		t.Errorf("ddl: %+v", r)
	}
	r = q(t, c, "insert into t values (1,'a',[1]),(2,'b',null),(3,'c',null)")
	if r.Tag != "INSERT 3" || r.Affected != 3 || len(r.Cols) != 0 {
		t.Errorf("insert: %+v", r)
	}
	r = q(t, c, "update t set v='x' where id > 1")
	if r.Tag != "UPDATE 2" || r.Affected != 2 {
		t.Errorf("update: %+v", r)
	}
	r = q(t, c, "delete from t where id = 99")
	if r.Tag != "DELETE 0" || r.Affected != 0 {
		t.Errorf("delete: %+v", r)
	}
	r = q(t, c, "update t set v = 'y' where id = 2 returning id, v")
	eq(t, cells(r), []string{"2", "y"})
	r = q(t, c, "update t set v = 'z' where id = 2 returning id, l, v")
	eq(t, cells(r), []string{"2", "<?>", "<?>"}) // the list has no text: the row's cells from it on are shown so
	for sql, want := range map[string][]string{
		"show tables":                   {"t"},
		"values (1, 'a')":               {"1", "a"},
		"from t where id = 1":           {"1", "a", "[1]"},
		"with x as (select 5 a) from x": {"5"},
		"describe select 1 a":           {"a", "INTEGER", "YES", "NULL", "NULL", "NULL"},
	} {
		eq(t, cells(q(t, c, sql)), want)
	}
	// pragma's rows are read as they are, not wrapped
	eq(t, cells(q(t, c, "pragma table_info('t')"))[:6], []string{"0", "id", "INTEGER", "true", "NULL", "true"})
	r = q(t, c, "set TimeZone='UTC'")
	if r.Tag != "SET" || len(r.Cols) != 0 {
		t.Errorf("set: %+v", r)
	}
	// more rows than maxRows: the first of them, and Truncated
	rs, err := c.Exec(context.Background(), "select i from range(10) t(i) order by i desc", 3)
	if err != nil || !rs[0].Truncated || len(rs[0].Rows) != 3 || rs[0].Rows[0][0].S != "9" {
		t.Errorf("maxRows: %+v %v", rs, err)
	}
	rs, err = c.Exec(context.Background(), "select i from range(3) t(i)", 3)
	if err != nil || rs[0].Truncated || len(rs[0].Rows) != 3 {
		t.Errorf("as many as maxRows: %+v %v", rs, err)
	}
	// a comment the statement ends in, a ; too
	eq(t, cells(q(t, c, "select 1 -- one")), []string{"1"})
	eq(t, cells(q(t, c, "select 2;")), []string{"2"})
	// arguments are text the library converts, NULL is NULL
	r = q(t, c, "update t set v = $1 where id is not distinct from $2", db.Val{S: "q"}, db.Val{S: "1"})
	if r.Affected != 1 {
		t.Errorf("update with arguments: %+v", r)
	}
	q(t, c, "update t set v = $1 where id = $2", db.Val{Null: true}, db.Val{S: "1"})
	eq(t, cells(q(t, c, "select v from t where id = $1", db.Val{S: "1"})), []string{"NULL"})
	if _, err := c.Query(context.Background(), "update t set id = $1 where id = 1", db.Val{S: "abc"}); err == nil || !strings.Contains(err.Error(), "Conversion Error") {
		t.Errorf("a text that is no integer: %v", err)
	}
	// ErrorOf
	_, err = c.Query(context.Background(), "select * from nope")
	if e := (Engine{}).ErrorOf(err); e.Code != "Catalog" || !strings.HasPrefix(e.Message, "Table with name nope does not exist") {
		t.Errorf("%+v", e)
	}
}

func TestCancel(t *testing.T) {
	c, _ := open(t)
	long := "select count(*) from range(100000000000) a, range(1000) b"
	cancelled := func(sql string, after time.Duration) time.Duration {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(after, cancel)
		start := time.Now()
		_, err := c.Query(ctx, sql)
		if err == nil || !strings.Contains(err.Error(), "INTERRUPT") {
			t.Fatalf("a cancelled statement: %v", err)
		}
		return time.Since(start) - after
	}
	// at once, before the statement has started, the interrupt is lost and sent again
	for _, after := range []time.Duration{0, 50 * time.Microsecond, 150 * time.Microsecond, 400 * time.Microsecond, time.Millisecond, 3 * time.Millisecond, 200 * time.Millisecond} {
		if d := cancelled(long, after); d > time.Second {
			t.Errorf("cancelled after %v, back %v later", after, d)
		}
		eq(t, cells(q(t, c, "select 42")), []string{"42"}) // the connection is the same
	}
	q(t, c, "create table u(id int primary key, v varchar)")
	q(t, c, "insert into u values (1, 'a')")
	q(t, c, "begin")
	q(t, c, "update u set v = 'dirty' where id = 1")
	if db.TxStatus(c) != 'T' {
		t.Fatalf("in a transaction: %c", db.TxStatus(c))
	}
	cancelled(long, 100*time.Millisecond)
	if db.TxStatus(c) != 'E' {
		t.Errorf("after a cancel: %c", db.TxStatus(c))
	}
	if _, err := c.Query(context.Background(), "select 1"); err == nil || !strings.Contains(err.Error(), "Current transaction is aborted") {
		t.Errorf("aborted: %v", err)
	}
	q(t, c, "rollback")
	if db.TxStatus(c) != 'I' {
		t.Errorf("after the rollback: %c", db.TxStatus(c))
	}
	eq(t, cells(q(t, c, "select v from u")), []string{"a"})
	// any error fails the transaction
	q(t, c, "begin")
	if _, err := c.Query(context.Background(), "insert into u values (1, 'dup')"); err == nil {
		t.Fatal("a duplicate key")
	}
	if db.TxStatus(c) != 'E' {
		t.Errorf("after an error: %c", db.TxStatus(c))
	}
	q(t, c, "commit") // a COMMIT of a failed transaction is its rollback
	if db.TxStatus(c) != 'I' {
		t.Errorf("after the commit: %c", db.TxStatus(c))
	}
}

// Meta and Main are two connections to the one instance: the one reads only,
// neither waits for the other.
func TestTwoConnections(t *testing.T) {
	main, path := open(t)
	meta, _, err := Connect(context.Background(), path, "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { meta.Close() })
	q(t, main, "create table t(id int primary key, v varchar)")
	q(t, main, "insert into t values (1, 'a')")
	if _, err := meta.Query(context.Background(), "insert into t values (2, 'b')"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("a write on Meta: %v", err)
	}
	if _, err := meta.Query(context.Background(), "create table z(i int)"); err == nil {
		t.Errorf("DDL on Meta")
	}
	// the transaction the error aborted is gone: the next statement runs
	if _, err := meta.Query(context.Background(), "select * from nope"); err == nil {
		t.Fatal("a table that is not there")
	}
	eq(t, cells(q(t, meta, "select count(*) from t")), []string{"1"})
	// a BEGIN of Meta's is a read only one, kept until the COMMIT
	q(t, meta, "begin")
	if db.TxStatus(meta) != 'T' {
		t.Errorf("Meta in a transaction: %c", db.TxStatus(meta))
	}
	if _, err := meta.Query(context.Background(), "delete from t"); err == nil {
		t.Errorf("a write in a begun transaction of Meta")
	}
	q(t, meta, "rollback")
	// a transaction of Main is not seen by Meta, and a long statement of one
	// does not stop the other, nor does the interrupt of one
	q(t, main, "begin")
	q(t, main, "update t set v = 'dirty'")
	eq(t, cells(q(t, meta, "select v from t")), []string{"a"})
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, _ = main.Query(ctx, "select count(*) from range(100000000000) a, range(1000) b")
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	eq(t, cells(q(t, meta, "select count(*) from range(30000000) a, range(3) b")), []string{"90000000"})
	t.Logf("Meta's statement took %v while Main's ran", time.Since(start))
	select {
	case <-done:
		t.Fatal("Main's statement ended with nothing asked")
	default:
	}
	cancel()
	<-done
	q(t, main, "rollback")
}

// The file is opened once and shared, and let go of with the last connection;
// another process has it only then.
func TestInstance(t *testing.T) {
	c1, path := open(t)
	c2, _, err := Connect(context.Background(), path, "", false)
	if err != nil {
		t.Fatal(err)
	}
	q(t, c1, "create table t(i int)")
	q(t, c1, "insert into t values (7)")
	eq(t, cells(q(t, c2, "select i from t")), []string{"7"})
	if _, _, err := Connect(context.Background(), path+"?memory_limit=1GB", "", false); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Errorf("the file with other settings: %v", err)
	}
	// a symlink to the file is the same instance
	link := filepath.Join(t.TempDir(), "link.duckdb")
	if err := os.Symlink(path, link); err == nil {
		c3, _, err := Connect(context.Background(), link, "", false)
		if err != nil {
			t.Fatalf("by a symlink: %v", err)
		}
		eq(t, cells(q(t, c3, "select i from t")), []string{"7"})
		c3.Close()
	}
	if out := other(t, path); !strings.Contains(out, "lock") || !strings.Contains(out, "PID") {
		t.Errorf("another process while it is open: %q", out)
	}
	c1.Close()
	if out := other(t, path); !strings.Contains(out, "lock") {
		t.Errorf("another process while one connection is left: %q", out)
	}
	c2.Close()
	if out := other(t, path); out != "7" {
		t.Errorf("another process once all are closed: %q", out)
	}
	// :memory: is one database for the process
	m1, _, _ := Connect(context.Background(), ":memory:", "", false)
	m2, _, _ := Connect(context.Background(), ":memory:", "", false)
	defer m1.Close()
	defer m2.Close()
	q(t, m1, "create table m(i int)")
	q(t, m2, "select * from m")
	// a file that is not there is no new one, but with mode=rwc
	if _, _, err := Connect(context.Background(), filepath.Join(t.TempDir(), "no.duckdb"), "", false); err == nil {
		t.Error("a file that is not there")
	}
	if _, _, err := Connect(context.Background(), path+"?nope_key=1", "", false); err == nil || !strings.Contains(err.Error(), "nope_key") {
		t.Errorf("a setting there is none of: %v", err)
	}
}

// other is what the other process, this test binary asked for TestOther,
// makes of the file at path: the first cell of the first table it has, or the
// error.
func other(t *testing.T, path string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestOther$")
	cmd.Env = append(os.Environ(), "DUCKDB_OTHER="+path)
	out, _ := cmd.CombinedOutput()
	s := string(out)
	if _, after, ok := strings.Cut(s, "result: "); ok {
		s, _, _ = strings.Cut(after, "\n")
	}
	return s
}

func TestOther(t *testing.T) {
	path := os.Getenv("DUCKDB_OTHER")
	if path == "" {
		t.Skip("run by TestInstance")
	}
	UseLibrary(t)
	c, _, err := Connect(context.Background(), path, "", false)
	if err != nil {
		t.Logf("result: %v", err)
		return
	}
	defer c.Close()
	r := q(t, c, "select i from t")
	t.Logf("result: %s", r.Rows[0][0].S)
}
