//go:build integration

package app

import (
	"context"
	"fmt"
	"testing"

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
	c.forgetNames()
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
