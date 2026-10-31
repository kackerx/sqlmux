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
			batches++
			m := cmd().(indexMsg)
			cmd = a.gotIndex(m)
		}
		return batches
	}
	n := build()
	ix := c.index
	if n < 20 || !ix.done || ix.capped || int64(len(ix.names)) != size || ix.total != size {
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
	build := func(cmd tea.Cmd) {
		for cmd != nil {
			cmd = a.gotIndex(cmd().(indexMsg))
		}
	}
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
	if cmd := a.wrote(c, "FLUSHDB"); cmd != nil || len(c.index.names) != 0 {
		t.Errorf("FLUSHDB: %v", c.index.names)
	}
}
