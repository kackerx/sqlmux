//go:build integration

package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/redis"
)

// The index of a db of 20000 keys is made a SCAN COUNT 1000 at a time, each batch
// a request of its own on Meta, to the last name; index_keys stops it, and the
// list says how many of the db it has (§9.8, F20.1).
func TestKeyIndexOfARedisDB(t *testing.T) {
	dsn := redis.OwnDB(t)
	opt, err := goredis.ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cl := goredis.NewClient(opt)
	defer cl.Close()
	ctx := context.Background()
	pipe := cl.Pipeline()
	for i := range 20000 {
		pipe.Set(ctx, fmt.Sprintf("ns%d:item:%d", i%50, i), 1, 0)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	size, err := cl.DBSize(ctx).Result() // the 20000 and the key that claims the db
	if err != nil || size < 20000 {
		t.Fatalf("db size %d %v", size, err)
	}
	a := opened(t, config.Connection{Name: "cache", Engine: "redis", DSN: dsn})
	c := a.sess.Conns[0]
	if c.scan == nil || !c.scan.more {
		t.Fatalf("the tree stops at its budget: %+v", c.scan)
	}
	build := func() (batches int) {
		for cmd := a.startIndex(c); cmd != nil; {
			m, ok := cmd().(indexMsg)
			if !ok { // the last batch's answer is the write of the file
				break
			}
			batches++
			cmd = a.gotIndex(m)
		}
		return batches
	}
	n := build()
	ix := c.index
	if n < 2 || !ix.done || ix.capped || int64(len(ix.names)) != size || ix.total != size {
		t.Fatalf("%d batches, done %v capped %v, %d names of %d", n, ix.done, ix.capped, len(ix.names), ix.total)
	}
	c.index, c.building = nil, nil
	a.indexKeys = 5000
	build()
	ix = c.index
	if !ix.done || !ix.capped || len(ix.names) != 5000 || ix.total != size {
		t.Fatalf("capped: %d names of %d", len(ix.names), ix.total)
	}
	if _, foot, _ := a.keyCandidates(&queryTab{conn: c}, "ns"); foot != fmt.Sprintf("已索引 5000 / 共 %d 键", size) {
		t.Errorf("foot %q", foot)
	}
}

// buildIndex runs the batches of the index cmd asks for, one answer after the other, to
// the last, whose answer is the write of its file.
func buildIndex(a *App, cmd tea.Cmd) {
	for cmd != nil {
		m, ok := cmd().(indexMsg)
		if !ok {
			return
		}
		cmd = a.gotIndex(m)
	}
}

// A db's index is in its file once it is built; the next start has its names for the
// first completion before the first SCAN is sent, and the index made after takes their
// place (F21.4).
func TestKeyIndexOfARedisDBFromTheFile(t *testing.T) {
	dsn := redis.OwnDB(t)
	opt, err := goredis.ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cl := goredis.NewClient(opt)
	defer cl.Close()
	ctx := context.Background()
	pipe := cl.Pipeline()
	for i := range 20000 {
		pipe.Set(ctx, fmt.Sprintf("ns%d:item:%d", i%50, i), 1, 0)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, c := redisApp(t, dsn, 1)
	buildIndex(a, a.startIndex(c))
	if c.index == nil || len(c.index.names) < 20000 {
		t.Fatalf("built: %+v", c.index)
	}
	a.saveIndex(c)() // the write of the last batch's answer
	built := len(c.index.names)

	// the next start: a new App, the same data directory
	a2, c2 := redisApp(t, dsn, 1)
	first := a2.startIndex(c2) // the first SCAN, not sent yet
	if first == nil || c2.index == nil || !c2.index.stale || len(c2.index.names) != built {
		t.Fatalf("started: %+v", c2.index)
	}
	if got, foot, _ := a2.keyCandidates(&queryTab{conn: c2}, "ns7:item:7"); len(got) == 0 || foot != "索引更新中" {
		t.Errorf("before the first SCAN: %d candidates, foot %q", len(got), foot)
	}
	buildIndex(a2, first)
	if c2.index.stale || len(c2.index.names) != built || c2.building != nil {
		t.Errorf("the index made takes its place: %d names, stale %v", len(c2.index.names), c2.index.stale)
	}
}

// On a real server a write is followed in the index by what EXISTS says of its keys:
// SET, DEL, RENAME, SET … NX that is not set, EXPIRE 0, FLUSHDB; and EVAL, whose
// keys are not known, makes the index again with the old one used meanwhile
// (§9.8, F20.8).
func TestKeyIndexFollowsWrites(t *testing.T) {
	dsn := redis.OwnDB(t)
	opt, err := goredis.ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cl := goredis.NewClient(opt)
	defer cl.Close()
	for _, k := range []string{"w:a", "w:b", "w:k"} {
		cl.Set(context.Background(), k, "x", 0)
	}
	a, c := redisApp(t, dsn, 1) // a budget of one key: the tree does not read it all, the index is built
	build := func(cmd tea.Cmd) { buildIndex(a, cmd) }
	if c.scan == nil || !c.scan.more {
		t.Fatalf("the tree reads it all: %+v", c.scan)
	}
	build(a.startIndex(c))
	names := func() string {
		var out []string
		for _, n := range c.index.names {
			if strings.HasPrefix(n, "w:") {
				out = append(out, n)
			}
		}
		return strings.Join(out, " ")
	}
	if names() != "w:a w:b w:k" {
		t.Fatalf("built: %q", names())
	}
	write := func(args ...string) {
		t.Helper()
		command(t, c, args...)
		cmd := a.wrote(c, strings.Join(args, " "))
		if cmd == nil {
			t.Fatalf("%v asks nothing", args)
		}
		if m, ok := cmd().(indexDelta); ok {
			a.gotDelta(m)
		} else {
			t.Fatalf("%v: not an ask of EXISTS", args)
		}
	}
	write("SET", "w:new", "1")
	write("DEL", "w:a")
	write("RENAME", "w:b", "w:z")
	if names() != "w:k w:new w:z" {
		t.Errorf("SET, DEL, RENAME: %q", names())
	}
	write("SET", "w:k", "v", "NX") // there: not set, not twice
	write("EXPIRE", "w:new", "0")
	if names() != "w:k w:z" {
		t.Errorf("NX, EXPIRE 0: %q", names())
	}

	// not known: made again, the index there used meanwhile
	command(t, c, "SET", "w:lua", "1")
	old := c.index
	cmd := a.wrote(c, "EVAL \"return 1\" 0")
	if cmd == nil || c.building == nil || c.indexed() != old || names() != "w:k w:z" {
		t.Fatalf("EVAL: building %v", c.building)
	}
	build(cmd)
	if c.index == old || c.building != nil || names() != "w:k w:lua w:z" {
		t.Errorf("made again: %q", names())
	}
	command(t, c, "FLUSHDB")
	if cmd := a.wrote(c, "FLUSHDB"); cmd == nil || len(c.index.names) != 0 { // its file is written empty
		t.Errorf("FLUSHDB: %v", c.index.names)
	}
}

// What the key tab and the tree's menu do to a key goes into the index as a write of the
// console does, by one EXISTS: a hash that lost its last field is gone from it, a key
// renamed is there by its new name and not the old; SWAPDB makes it again (§9.8, F20.8).
func TestKeyIndexFollowsKeyTab(t *testing.T) {
	dsn := redis.OwnDB(t)
	opt, err := goredis.ParseURL(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cl := goredis.NewClient(opt)
	defer cl.Close()
	cl.HSet(context.Background(), "kt:solo", "a", "1")
	cl.Set(context.Background(), "kt:plain", "x", 0)
	a, c := redisApp(t, dsn, 1) // the tree does not read it all: the index is built
	buildIndex(a, a.startIndex(c))
	names := func() string {
		var out []string
		for _, n := range c.index.names {
			if strings.HasPrefix(n, "kt:") {
				out = append(out, n)
			}
		}
		return strings.Join(out, " ")
	}
	if names() != "kt:plain kt:solo" {
		t.Fatalf("built: %q", names())
	}
	a.confirmDangerous = false
	d := openKey(t, a, c, "kt:solo")
	d.deleted = map[string]bool{d.rowKey(0): true}
	settle(a, a.save())
	if got := command(t, c, "EXISTS", "kt:solo").Rows[0][0].S; got != "0" || names() != "kt:plain" {
		t.Errorf("the last field deleted: exists %s, %q", got, names())
	}
	command(t, c, "RENAME", "kt:plain", "kt:moved")
	c.busy++
	settle(a, a.gotKeyDone(keyDone{conn: c, say: "renamed", key: "kt:plain", to: "kt:moved"}))
	if names() != "kt:moved" {
		t.Errorf("renamed: %q", names())
	}
}
