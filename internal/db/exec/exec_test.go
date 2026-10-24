//go:build !windows

// The fake the engine is tested through is a /bin/sh script; the engine
// itself runs whatever argv it is given, on windows as anywhere.

package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/postgres"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// fake is a command that answers by the statement it is given, and keeps what it
// was given in log.
func fake(t *testing.T) (argv []string, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	script := `#!/bin/sh
sql="$1"
printf '%s\n' "$sql" >> "` + log + `"
case "$sql" in
*current_setting*) echo '[{"columns":["a","b"],"rows":[["shop","\"$user\", public"]]}]' ;;
*"select current_database()"*) echo '[{"columns":["current_database"],"rows":[["shop"]]}]' ;;
*"from pg_namespace n where"*) echo '[{"columns":["nspname","cs"],"rows":[["public","public"],["sales","public"]]}]' ;;
*"from pg_class c join pg_namespace"*) echo '[{"columns":["n","r","t","k"],"rows":[["public","orders",12,"r"],["public","v_orders",-1,"v"]]}]' ;;
*pg_database*) echo '[{"columns":["datname"],"rows":[["shop"],["other"]]}]' ;;
*seven*) off=0; case "$sql" in *"offset "*) off=${sql##*offset } ;; esac
  n=0; rows=; i=$off; while [ $i -lt 7 ] && [ $n -lt 3 ]; do i=$((i+1)); n=$((n+1)); rows="$rows${rows:+,}[$i]"; done
  cut=false; [ $i -lt 7 ] && cut=true
  echo "[{\"columns\":[\"x\"],\"rows\":[$rows],\"truncated\":$cut}]" ;;
*six*) off=0; case "$sql" in *"offset "*) off=${sql##*offset } ;; esac
  n=0; rows=; i=$off; while [ $i -lt 6 ] && [ $n -lt 3 ]; do i=$((i+1)); n=$((n+1)); rows="$rows${rows:+,}[$i]"; done
  cut=false; [ $off = 0 ] && cut=true # only the first says it is cut, as pgAdmin's wall does
  echo "[{\"columns\":[\"x\"],\"rows\":[$rows],\"truncated\":$cut}]" ;;
*shortcut*) case "$sql" in *offset*) echo '[{"columns":["x"],"rows":[[4]],"truncated":true}]' ;; *) echo '[{"columns":["x"],"rows":[[1],[2],[3]],"truncated":true}]' ;; esac ;;
*stuck*) case "$sql" in *offset*) echo '[{"columns":["x"],"rows":[],"truncated":true}]' ;; *) echo '[{"columns":["x"],"rows":[[1],[2],[3]],"truncated":true}]' ;; esac ;;
*endless*) echo '[{"columns":["x"],"rows":[[1],[2],[3]],"truncated":true}]' ;;
*tagged*) echo '[{"n":3}]' ;;
*cut*) echo '[{"columns":["x"],"rows":[[1],[2]],"truncated":true,"extra":{"a":1}}]' ;;
*bigerr*) head -c 10000 /dev/zero | tr '\000' a >&2; echo END >&2; exit 1 ;;
*stderr*) echo "the panel is not open" >&2; exit 3 ;;
*itemerr*) echo '[{"err":"relation does not exist"}]' ;;
*notjson*) echo 'hello' ;;
*slow*) sleep 5; echo '[]' ;;
*ragged*) echo '[{"columns":["a","b"],"rows":[[1],[1,2,3]]}]' ;;
*one*) echo '{"columns":["c"],"rows":[["only"]]}' ;;
*) echo '[{"columns":["x"],"rows":[[1],[null],[true],["it'"'"'s"],[{"a":1}],[2.50],[false]],"n":7}]' ;;
esac
`
	path := filepath.Join(dir, "cmd.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"/bin/sh", path, "{sql}"}, log
}

func ran(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// A run is the command with the statement in place of {sql}; the output's values are
// the text protocol's: a number as written, t and f, NULL, nested JSON as text (F17.7).
func TestQueryValues(t *testing.T) {
	argv, log := fake(t)
	c, err := New(argv, sqlkit.PG)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Query(context.Background(), "select x from t where a = $1 and b = $2", db.Val{S: "it's"}, db.Val{Null: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range r.Rows {
		s := row[0].S
		if row[0].Null {
			s = "NULL"
		}
		got = append(got, s)
	}
	if want := "1 NULL t it's {\"a\":1} 2.50 f"; strings.Join(got, " ") != want || r.Affected != 7 || len(r.Cols) != 1 || r.Cols[0].Name != "x" {
		t.Errorf("rows %q, affected %d, cols %v", got, r.Affected, r.Cols)
	}
	if sent := ran(t, log); len(sent) != 1 || sent[0] != "select x from t where a = 'it''s' and b = NULL" {
		t.Errorf("the command was given %q", sent)
	}
}

// $n goes in as a literal where it is code, not in a string, an identifier or a comment.
func TestInline(t *testing.T) {
	for _, c := range []struct {
		sql, want string
		args      []db.Val
	}{
		{"select $1, $2::text", "select 'a', NULL::text", []db.Val{{S: "a"}, {Null: true}}},
		{"select '$1', \"c$1\" -- $1\n, $1 /* $2 */", "select '$1', \"c$1\" -- $1\n, 'a' /* $2 */", []db.Val{{S: "a"}}},
		{"select 'it''s $1', $1", "select 'it''s $1', 'x'", []db.Val{{S: "x"}}},
		{"select $10, $1", "select 'j', 'a'", []db.Val{{S: "a"}, {}, {}, {}, {}, {}, {}, {}, {}, {S: "j"}}},
		{"select $$x$$, $1", "select $$x$$, 'a'", []db.Val{{S: "a"}}}, // a $$ is no parameter's
		{"select $q$ $1 $q$, $$ $1 $$, $1", "select $q$ $1 $q$, $$ $1 $$, 'a'", []db.Val{{S: "a"}}},
		{`select E'it\'s $1', $1`, `select E'it\'s $1', 'a'`, []db.Val{{S: "a"}}},
		{"select $ 1, $1", "select $ 1, 'a'", []db.Val{{S: "a"}}},                               // a $ and a number apart are two things
		{"select /* a /* $1 */ $1 */ $1", "select /* a /* $1 */ $1 */ 'a'", []db.Val{{S: "a"}}}, // PG's comments nest
	} {
		got, err := Inline(c.sql, c.args, sqlkit.PG)
		if err != nil || got != c.want {
			t.Errorf("%q: %q %v, want %q", c.sql, got, err, c.want)
		}
	}
	for _, sql := range []string{"select $2", "select $0"} {
		if _, err := Inline(sql, []db.Val{{S: "a"}}, sqlkit.PG); err == nil {
			t.Errorf("%s: a parameter with no argument", sql)
		}
	}
}

// Each statement is a run of its own, the results in order; the first that fails stops
// the rest and comes back with those before it (F17.7).
func TestExec(t *testing.T) {
	argv, log := fake(t)
	c, _ := New(argv, sqlkit.PG)
	rs, err := c.Exec(context.Background(), "select one; select 'a;b'; select 3", 3)
	if err != nil || len(rs) != 3 || rs[0].Rows[0][0].S != "only" || len(rs[1].Rows) != 3 || !rs[1].Truncated {
		t.Fatalf("%+v %v", rs, err)
	}
	if sent := ran(t, log); len(sent) != 3 || sent[1] != "select 'a;b';" && sent[1] != "select 'a;b'" {
		t.Errorf("runs %q", sent)
	}
	rs, err = c.Exec(context.Background(), "select one; select itemerr; select one", 0)
	var ce Error
	if len(rs) != 1 || !errors.As(err, &ce) || ce.Msg != "relation does not exist" {
		t.Errorf("a failing statement: %d results, %v", len(rs), err)
	}
	if _, err := c.Exec(context.Background(), "select stderr", 0); !errors.As(err, &ce) || ce.Msg != "the panel is not open" {
		t.Errorf("stderr: %v", err)
	}
	if _, err := c.Exec(context.Background(), "select notjson", 0); !errors.As(err, &ce) || !strings.Contains(ce.Msg, "JSON") {
		t.Errorf("not JSON: %v", err)
	}
	if _, err := New([]string{"x"}, sqlkit.PG); err == nil {
		t.Error("no {sql}")
	}
	if _, err := New([]string{"/no/such/command", "{sql}"}, sqlkit.PG); err != nil {
		t.Error("a command that is not there is the first run's error")
	}
}

// A statement that writes has no columns and says how many rows it hit; an item that
// says err is that error on a single query as it is on Exec; what a command says on
// stderr is its last 4 KB (F17.7).
func TestQueryTagErrAndStderrTail(t *testing.T) {
	argv, _ := fake(t)
	c, _ := New(argv, sqlkit.PG)
	ctx := context.Background()
	if r, err := c.Query(ctx, "update tagged"); err != nil || r.Tag != "3" || r.Affected != 3 || len(r.Cols) != 0 {
		t.Errorf("no columns: %+v %v", r, err)
	}
	var ce Error
	if _, err := c.Query(ctx, "select itemerr"); !errors.As(err, &ce) || ce.Msg != "relation does not exist" {
		t.Errorf("an item's err: %v", err)
	}
	if _, err := c.Query(ctx, "select bigerr"); !errors.As(err, &ce) || len(ce.Msg) > 4096 || !strings.HasSuffix(ce.Msg, "aaaEND") {
		t.Errorf("stderr: %d bytes, ends %q", len(ce.Msg), ce.Msg[max(0, len(ce.Msg)-8):])
	}
}

// An item the command says is truncated is a truncated result, whatever maxRows is; one
// that says nothing of it is not; a field it does not know is ignored (F17.11).
func TestCommandTruncated(t *testing.T) {
	argv, _ := fake(t)
	c, _ := New(argv, sqlkit.PG)
	ctx := context.Background()
	if rs, err := c.Exec(ctx, "select cut", 0); err != nil || len(rs) != 1 || !rs[0].Truncated || len(rs[0].Rows) != 2 {
		t.Errorf("truncated on Exec: %+v %v", rs, err)
	}
	if r, err := c.Query(ctx, "select plain"); err != nil || r.Truncated {
		t.Errorf("not truncated: %+v %v", r, err)
	}
}

// A statement sqlmux sends that the command cut is run on as pages of itself, the cut's
// size, from the rows read, until one is not cut, and the rows are one result; a user's
// statements (Exec) are not paged and say they are cut; at the cap it stays cut (F17.11).
func TestQueryPagesWhatIsCut(t *testing.T) {
	argv, log := fake(t)
	c, _ := New(argv, sqlkit.PG)
	ctx := context.Background()
	r, err := c.Query(ctx, "select x from seven;")
	var got []string
	for _, row := range r.Rows {
		got = append(got, row[0].S)
	}
	if err != nil || strings.Join(got, "") != "1234567" || r.Truncated {
		t.Fatalf("rows %v truncated %v: %v", got, r.Truncated, err)
	}
	sent := ran(t, log)
	if len(sent) != 3 || sent[1] != "select * from (select x from seven) as sqlmux_page limit 3 offset 3" || !strings.HasSuffix(sent[2], "limit 3 offset 6") {
		t.Errorf("sent %q", sent)
	}
	// a page as long as the cut says nothing of being cut: the end is a short page, or an empty one
	for _, tc := range []struct{ sql, want string }{{"select x from six", "123456"}} {
		r, err := c.Query(ctx, tc.sql)
		var got []string
		for _, row := range r.Rows {
			got = append(got, row[0].S)
		}
		if err != nil || strings.Join(got, "") != tc.want || r.Truncated {
			t.Errorf("%s: rows %v truncated %v: %v", tc.sql, got, r.Truncated, err)
		}
	}
	rs, err := c.Exec(ctx, "select x from seven", 0)
	if err != nil || len(rs) != 1 || len(rs[0].Rows) != 3 || !rs[0].Truncated || len(ran(t, log)) != 7 {
		t.Errorf("Exec: %+v %v, %d runs in all", rs, err, len(ran(t, log)))
	}
	if r, err := c.Query(ctx, "select stuck"); err != nil || len(r.Rows) != 3 || r.Truncated { // an empty page ends it, whatever it says
		t.Errorf("an empty page: %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
	if r, err := c.Query(ctx, "select shortcut"); err != nil || len(r.Rows) != 4 || r.Truncated { // a short page ends it, whatever it says
		t.Errorf("a short page: %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
	// a user's statement in the quick SQL is one run, cut and saying so
	e := Engine{Engine: postgres.Engine{}, Command: argv}
	before := len(ran(t, log))
	if r, err := e.Quick(ctx, db.NewWorker(c), "select x from seven", "", 100); err != nil || len(r.Rows) != 3 || !r.Truncated || len(ran(t, log)) != before+1 {
		t.Errorf("quick: %d rows, truncated %v, %d runs, %v", len(r.Rows), r.Truncated, len(ran(t, log))-before, err)
	}
	defer func(n int) { maxPaged = n }(maxPaged)
	maxPaged = 7
	if r, err := c.Query(ctx, "select endless"); err != nil || len(r.Rows) != 9 || !r.Truncated {
		t.Errorf("at the cap: %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
}

// Cancelling kills the run, as a request's cancel does (F17.7).
func TestCancelKillsTheRun(t *testing.T) {
	argv, _ := fake(t)
	c, _ := New(argv, sqlkit.PG)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Query(ctx, "select slow")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Errorf("%v after %v", err, time.Since(start))
	}
}

// The engine is PG's over the command: its catalog queries are the ones the tree makes,
// and where the connection landed is what the first run said (F17.7).
func TestEngineOverACommand(t *testing.T) {
	argv, log := fake(t)
	e := Engine{Engine: postgres.Engine{}, Command: argv}
	ctx := context.Background()
	c, info, err := e.Connect(ctx, "", "", false)
	if err != nil || info.Database != "shop" || info.Addr != "sh" || info.Path != `"$user", public` {
		t.Fatalf("connect: %+v %v", info, err)
	}
	schemas, current, err := e.Schemas(ctx, c)
	if err != nil || strings.Join(schemas, " ") != "public sales" || current != "public" {
		t.Errorf("schemas %v %q %v", schemas, current, err)
	}
	ts, err := e.Tables(ctx, c, nil)
	if err != nil || len(ts) != 2 || ts[0].Name != "orders" || ts[0].Rows != 12 || !ts[1].View() {
		t.Errorf("tables %+v %v", ts, err)
	}
	if _, err := e.Tables(ctx, c, []string{"sales", "it's"}); err != nil { // the picked schemas go into the query
		t.Errorf("picked tables: %v", err)
	}
	if sent := strings.Join(ran(t, log), "\n"); !strings.Contains(sent, `and n.nspname in ('sales', 'it''s')`) {
		t.Errorf("the statements: %q", sent)
	}
	if dbs, err := e.Databases(ctx, c); err != nil || len(dbs) != 1 || dbs[0] != "shop" { // the one it is on, not the server's list: the command reaches that one
		t.Errorf("databases %v %v", dbs, err)
	}
	if err := e.UseSchema(ctx, c, "sales", ""); err != nil {
		t.Errorf("UseSchema: %v", err)
	}
	if e.Broken(errors.New("x")) != db.NotLost {
		t.Error("a run that failed is not a lost connection")
	}
	if got := e.ErrorOf(Error{"boom"}); got.Message != "boom" || got.Severity != "ERROR" {
		t.Errorf("ErrorOf: %+v", got)
	}
	w := db.NewWorker(c)
	r, err := e.Quick(ctx, w, "select 1", "public", 3)
	if err != nil || len(r.Rows) != 3 || !r.Truncated {
		t.Errorf("quick: %d rows, truncated %v, %v", len(r.Rows), r.Truncated, err)
	}
	if _, _, err := (Engine{Engine: postgres.Engine{}, Command: []string{"/bin/sh", "-c", "echo down >&2; exit 1", "{sql}"}}).Connect(ctx, "", "", false); err == nil || err.Error() != "down" {
		t.Errorf("a command that does not work fails the connect: %v", err)
	}
}

// A row has as many cells as the columns, however the command wrote it: the code that
// reads a row by place does not read past its end.
func TestRaggedRows(t *testing.T) {
	argv, _ := fake(t)
	c, _ := New(argv, sqlkit.PG)
	r, err := c.Query(context.Background(), "select ragged")
	if err != nil || len(r.Rows) != 2 || len(r.Rows[0]) != 2 || !r.Rows[0][1].Null || len(r.Rows[1]) != 2 || r.Rows[1][1].S != "2" {
		t.Errorf("%+v %v", r.Rows, err)
	}
}
