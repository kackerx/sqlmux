//go:build integration

package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// A Redis connection is tried by the form's button with PING and says the
// server's version; it dials as any connection does, Main and Meta each,
// through the driver table with no engine; a db of the server is a connection
// of its own by the URL's path; and a Main the server killed dials again
// (F16.1).
func TestRedisConnection(t *testing.T) {
	ctx := context.Background()
	dsn := redis.IntegrationDSN(t)
	spec := config.Connection{Name: "cache", Engine: "redis", DSN: dsn}
	note, err := testConn(spec, "")
	if err != nil || !strings.Contains(note, "Redis ") || !strings.Contains(note, strings.SplitN(i18n.T("已连接 · %s · %s", "", ""), " ", 2)[0]) {
		t.Errorf("the test button: %q %v", note, err)
	}
	if _, err := testConn(config.Connection{Name: "x", Engine: "redis", DSN: "redis://127.0.0.1:1/0"}, ""); err == nil {
		t.Error("a server that is not there is tried")
	}
	// it is PING that is sent first: a user that may do all but that is refused
	admin := opened(t, spec).sess.Conns[0]
	acl := func(args ...string) error {
		var vals []db.Val
		for _, a := range args[1:] {
			vals = append(vals, db.Val{S: a})
		}
		_, err := admin.Main.Query(ctx, args[0], vals...)
		return err
	}
	if err := acl("ACL", "SETUSER", "sqlmux_noping", "on", ">p", "~*", "+@all", "-ping"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = acl("ACL", "DELUSER", "sqlmux_noping") })
	noPing := config.Connection{Name: "x", Engine: "redis", DSN: strings.Replace(dsn, "redis://", "redis://sqlmux_noping:p@", 1)}
	if _, err := testConn(noPing, ""); err == nil || !strings.Contains(err.Error(), "NOPERM") {
		t.Errorf("the test button sends PING: %v", err)
	}
	a := opened(t, spec)
	c := a.sess.Conns[0]
	if c.State != connReady || c.eng != nil || c.drv == nil || c.DB != "0" || c.Addr == "" {
		t.Fatalf("connected: state %v eng %v drv %v db %q addr %q", c.State, c.eng, c.drv, c.DB, c.Addr)
	}
	for _, w := range []*db.Worker{c.Main, c.Meta} {
		if r, err := w.Query(ctx, "PING"); err != nil || r.Rows[0][0].S != "PONG" {
			t.Errorf("PING: %+v %v", r, err)
		}
	}
	// a db of its own
	child := spec
	child.Database = "3"
	d, err := dial(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	defer d.discard()()
	if r, err := d.meta.Query(ctx, "CLIENT", db.Val{S: "INFO"}); err != nil || !strings.Contains(r.Rows[0][0].S, "db=3") || d.info.Database != "3" {
		t.Errorf("db 3: %+v %v %+v", r.Rows, err, d.info)
	}
	// the server kills Main: the next request goes on a connection dialed again
	id, err := c.Main.Query(ctx, "CLIENT", db.Val{S: "ID"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Meta.Query(ctx, "CLIENT", db.Val{S: "KILL"}, db.Val{S: "ID"}, db.Val{S: id.Rows[0][0].S}); err != nil {
		t.Fatal(err)
	}
	if r, err := c.Main.Query(ctx, "PING"); err != nil || r.Rows[0][0].S != "PONG" {
		t.Errorf("after the kill: %+v %v", r, err)
	}
	// the error of a command is the server's
	_, err = c.Main.Query(ctx, "HGET", db.Val{S: "str:plain"}, db.Val{S: "f"})
	if e := c.drv.ErrorOf(err); e.Code != "WRONGTYPE" {
		t.Errorf("an error: %v %+v", err, e)
	}
}

// The console runs a command on the server and lays its reply out; one that blocks
// (BLPOP with no timeout) is cancelled by C-c, which closes the connection, and the
// next command goes on a connection dialed again (F16.4).
func TestRedisConsoleRunsAndCancels(t *testing.T) {
	a, c := redisApp(t, redis.OwnDB(t), 1000)
	a.run("query.new", 0)
	q := a.sess.queries()[0]
	q.conn = c
	runText := func(text string) tea.Cmd { return a.runSQL(q, c, "", q.name, text, 0, q.ver) }
	command(t, c, "RPUSH", "l", "a", "b", "c")
	settle(a, runText("LRANGE l 0 -1"))
	rt := resultOf(a.win().Result)
	if rt == nil || len(rt.page.Rows) != 3 || rt.page.Rows[0][0].S != "a" {
		t.Fatalf("LRANGE: %+v", rt)
	}
	blocked := runText("BLPOP nolist 0")
	done := make(chan tea.Msg, 1)
	go func() { done <- blocked() }()
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	c.cancel()
	select {
	case m := <-done:
		a.Update(m)
	case <-time.After(5 * time.Second):
		t.Fatal("C-c did not end the command")
	}
	if time.Since(start) > 3*time.Second || a.toast != i18n.T("查询已取消") {
		t.Errorf("cancel took %v, toast %q", time.Since(start), a.toast)
	}
	settle(a, runText("PING"))
	if rt := resultOf(a.win().Result); rt == nil || rt.page.Rows[0][0].S != "PONG" {
		t.Errorf("after the cancel: %+v", rt)
	}
}

// A console on a real server completes what COMMAND DOCS lists and what the db has:
// the names of commands, a key after GET, a field after HGET and the key, the list
// made again when the names come from the server (F16.5).
func TestRedisConsoleCompletes(t *testing.T) {
	a, c := redisApp(t, redis.OwnDB(t), 1000) // a db with few keys: the rounds of a scan read all of them
	if c.docs == nil || c.cmds == nil || !c.cmdsReady {
		t.Fatalf("docs %d, commands %d", len(c.docs), len(c.cmds))
	}
	for _, k := range []string{"app:users:1", "app:users:2", "app:users:3"} {
		command(t, c, "SET", k, "x")
	}
	command(t, c, "HSET", "hash:user", "name", "Ann", "age", "30")
	a.run("query.new", 0)
	q := a.sess.queries()[0]
	q.conn = c
	labels := func() (out []string) {
		if q.comp != nil {
			for _, it := range q.comp.items {
				out = append(out, it.label)
			}
		}
		return out
	}
	typed := func(text string) {
		t.Helper()
		q.ed.Load(text)
		feed(t, a, "A")
		cmd := a.queryComplete(q, false)
		for _, m := range collect(cmd) { // the ticks and the asks, as the program would deliver them
			_, next := a.Update(m)
			for _, m2 := range collect(next) {
				_, last := a.Update(m2)
				settle(a, last)
			}
		}
	}
	typed("hse")
	if got := labels(); len(got) == 0 || got[0] != "HSET" {
		t.Errorf("hse: %v", got)
	}
	feed(t, a, "<Esc>")
	typed("GET app:users:")
	if got := labels(); !slices.Equal(got, []string{"app:users:1", "app:users:2", "app:users:3"}) {
		t.Errorf("a key: %v", got)
	}
	feed(t, a, "<Esc>")
	typed("HGET hash:user na")
	if got := labels(); len(got) == 0 || got[0] != "name" {
		t.Errorf("a field: %v", got)
	}
	feed(t, a, "<Esc>")
}

// collect is the messages of cmd, a batch's each.
func collect(cmd tea.Cmd) (out []tea.Msg) {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			out = append(out, collect(c)...)
		}
	case nil:
	default:
		out = append(out, m)
	}
	return out
}
