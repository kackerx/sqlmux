package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
)

// fakeExec is a command that answers a few statements as a PG over a command would:
// where it landed, the schemas, the tables, and one row for anything else.
func fakeExec(t *testing.T) config.Connection {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
*current_setting*) echo '[{"columns":["a","b"],"rows":[["shop","public"]]}]' ;;
*"select current_database()"*) echo '[{"columns":["current_database"],"rows":[["shop"]]}]' ;;
*"from pg_namespace n where"*) echo '[{"columns":["nspname","cs"],"rows":[["public","public"]]}]' ;;
*"from pg_class c join pg_namespace"*) echo '[{"columns":["n","r","t","k"],"rows":[["public","orders",12,"r"]]}]' ;;
*"from pg_proc p"*|*"cl.relkind = 'S'"*) echo '[{"columns":[],"rows":[]}]' ;;
*pg_database*) echo '[{"columns":["datname"],"rows":[["shop"]]}]' ;;
*) echo '[{"columns":["x"],"rows":[["one"],["two"]]}]' ;;
esac
`
	path := filepath.Join(dir, "cmd.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return config.Connection{Name: "prod", Engine: "exec", Dialect: "postgres", Command: []string{"/bin/sh", path, "{sql}"}, ReadOnly: true}
}

// A connection that is a command dials, loads its catalog into the tree, runs a query,
// and takes no write: read only is all it is (F17.7, §8.8).
func TestExecConnection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake exec command is a /bin/sh script")
	}
	a, q := inQuery(t, "", "")
	c := a.sess.Conns[0]
	spec := fakeExec(t)
	c.setSpec(spec)
	d, err := dial(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	c.take(d)
	if slices.Contains(a.managed(), c) { // written in connections.toml, not listed in the manager (§8.8)
		t.Error("the manager lists an exec connection")
	}
	if c.Addr != "sh" || c.DB != "shop" || !c.sql() || !c.readOnly || c.redis() {
		t.Fatalf("dialed: addr %q db %q sql %v read only %v", c.Addr, c.DB, c.sql(), c.readOnly)
	}
	m, ok := a.loadCatalog(c)().(catalogMsg)
	if !ok || m.err != nil || len(m.tables) != 1 || m.tables[0].Name != "orders" {
		t.Fatalf("catalog: %+v", m)
	}
	if len(m.dbs) != 1 || m.dbs[0] != "shop" { // the database level, as any PG connection has (F17.12)
		t.Errorf("databases %v", m.dbs)
	}
	a.Update(m)
	if !strings.Contains(treeTexts(a), "orders") || !strings.Contains(treeTexts(a), "shop") {
		t.Errorf("the tree:\n%s", treeTexts(a))
	}
	q.ed.Load("select x from t")
	q.conn = c
	deliver(a, a.queryRun(""))
	if got := logText(a); !strings.Contains(got, "select x from t") || strings.Contains(got, "error") {
		t.Errorf("the log: %s", got)
	}
	q.ed.Load("update t set x = 1")
	a.queryRun("")
	if got := logText(a); !strings.Contains(got, "prod 是只读连接") {
		t.Errorf("a write: %s", got)
	}
	// a command that does not work is the dial's error, with what it said
	bad := spec
	bad.Command = []string{"/bin/sh", "-c", "echo not logged in >&2; exit 2", "{sql}"}
	if _, err := dial(context.Background(), bad); err == nil || err.Error() != "not logged in" {
		t.Errorf("a command that fails: %v", err)
	}
}

// fakeExecRedis is a command that answers as the tool of a Redis would: PING, the
// size, a SCAN, TYPE, and nothing of COMMAND.
func fakeExecRedis(t *testing.T) config.Connection {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cmd.sh")
	script := `#!/bin/sh
[ "$1" = "--" ] && shift
cmd=$(echo "$1" | tr a-z A-Z)
case "$cmd" in
PING) echo '"PONG"' ;;
DBSIZE) echo 2 ;;
SCAN) echo '["0",["user:1","user:2"]]' ;;
TYPE) echo '"hash"' ;;
COMMAND) echo "COMMAND is not allowed" >&2; exit 1 ;;
*) echo null ;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return config.Connection{Name: "prodredis", Engine: "exec", Dialect: "redis", Command: []string{"/bin/sh", path, "--", "{cmd}", "{args...}"}, ReadOnly: true}
}

// An exec connection of the redis dialect is a Redis connection to everything above
// the driver: its tree has the keys the command's SCAN gave, its icon is Redis's, it is
// read only, and what the command refused of COMMAND says every command writes (F17.8).
func TestExecRedisConnection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake exec command is a /bin/sh script")
	}
	a, _ := inQuery(t, "", "")
	c := a.sess.Conns[0]
	spec := fakeExecRedis(t)
	c.setSpec(spec)
	d, err := dial(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	c.take(d)
	if c.Addr != "sh" || !c.redis() || c.sql() || !c.readOnly || c.Spec.Kind() != "redis" {
		t.Fatalf("dialed: addr %q redis %v sql %v read only %v", c.Addr, c.redis(), c.sql(), c.readOnly)
	}
	var deliver func(tea.Cmd)
	deliver = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch m := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range m {
				deliver(c)
			}
		case keysMsg, cmdsMsg:
			_, next := a.Update(m)
			deliver(next)
		}
	}
	deliver(a.loadCatalog(c))
	if txt := treeTexts(a); !strings.Contains(txt, "user") || !strings.Contains(txt, "db0") {
		t.Errorf("the tree:\n%s", txt)
	}
	if !c.cmdsReady || !strings.Contains(a.toast, "读不到命令列表") || !c.lang().Writes("GET k") {
		t.Errorf("COMMAND refused: ready %v, toast %q, GET writes %v", c.cmdsReady, a.toast, c.lang().Writes("GET k"))
	}
}
