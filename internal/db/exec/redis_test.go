//go:build !windows

// The fake the engine is tested through is a /bin/sh script; the engine
// itself runs whatever argv it is given, on windows as anywhere.

package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
)

// fakeRedis is a command that answers a Redis command's name and words as the tool
// would: JSON in the shape of the reply; what it was given goes to log.
func fakeRedis(t *testing.T) (argv []string, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	script := `#!/bin/sh
[ "$1" = "--" ] && shift
echo "$*" >> "` + log + `"
cmd=$(echo "$1" | tr a-z A-Z)
case "$cmd" in
PING) echo '"PONG"' ;;
TYPE) echo '"hash"' ;;
HGETALL) echo '{"zeta":"1","alpha":"two words","n":3}' ;;
DBSIZE) echo 2 ;;
SCAN) echo '["0",["user:1","user:2"]]' ;;
GET) echo null ;;
STRLEN) echo 12 ;;
EXISTS) echo true ;;
HKEYS) echo '[]' ;;
HLEN) echo 3 ;;
TTL|PTTL) echo -1 ;;
OBJECT) echo '"listpack"' ;;
MEMORY) echo 120 ;;
HSCAN) echo '["0",["zeta","1","alpha","two words","n","3"]]' ;;
SLOW) sleep 5; echo '"late"' ;;
FAIL) echo "no permission for FAIL" >&2; exit 2 ;;
BADJSON) echo hello ;;
*) echo "\"$cmd done\"" ;;
esac
`
	path := filepath.Join(dir, "cmd.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"/bin/sh", path, "--", "{cmd}", "{args...}"}, log
}

// go-redis speaks to the pipe and the command answers it: the shapes of the replies
// are the ones the typed calls read (F17.8).
func TestRedisOverACommand(t *testing.T) {
	argv, log := fakeRedis(t)
	ctx := context.Background()
	c, info, err := RedisDriver{Command: argv}.Connect(ctx, "", "", true)
	if err != nil || info.Addr != "sh" || info.Database != "0" {
		t.Fatalf("connect: %+v %v", info, err)
	}
	defer c.Close()
	dbs, err := redis.Databases(ctx, c)
	if err != nil || !slices.Equal(dbs, []string{"0"}) {
		t.Errorf("databases %v %v", dbs, err)
	}
	err = redis.Use(ctx, c, func(cl *goredis.Client) error {
		if typ, err := cl.Type(ctx, "k").Result(); err != nil || typ != "hash" {
			t.Errorf("TYPE: %q %v", typ, err)
		}
		if got, err := cl.HGetAll(ctx, "k").Result(); err != nil || len(got) != 3 || got["alpha"] != "two words" || got["n"] != "3" {
			t.Errorf("HGETALL: %v %v", got, err)
		}
		if got, err := cl.Do(ctx, "HGETALL", "k").Slice(); err != nil || len(got) != 6 || got[0] != "zeta" || got[2] != "alpha" || got[4] != "n" { // in the order the command wrote them
			t.Errorf("HGETALL in order: %v %v", got, err)
		}
		keys, cursor, err := cl.Scan(ctx, 0, "user:*", 100).Result()
		if err != nil || cursor != 0 || !slices.Equal(keys, []string{"user:1", "user:2"}) {
			t.Errorf("SCAN: %v %d %v", keys, cursor, err)
		}
		if _, err := cl.Get(ctx, "k").Result(); !errors.Is(err, goredis.Nil) {
			t.Errorf("GET of null: %v", err)
		}
		if n, err := cl.StrLen(ctx, "k").Result(); err != nil || n != 12 {
			t.Errorf("STRLEN: %d %v", n, err)
		}
		if n, err := cl.Exists(ctx, "k").Result(); err != nil || n != 1 {
			t.Errorf("EXISTS of true: %d %v", n, err)
		}
		if keys, err := cl.HKeys(ctx, "k").Result(); err != nil || len(keys) != 0 {
			t.Errorf("HKEYS of []: %v %v", keys, err)
		}
		if err := cl.Do(ctx, "FAIL", "x").Err(); err == nil || !strings.Contains(err.Error(), "no permission for FAIL") {
			t.Errorf("a command that fails: %v", err)
		}
		if err := cl.Do(ctx, "BADJSON").Err(); err == nil || !strings.Contains(err.Error(), "JSON") {
			t.Errorf("not JSON: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// the console's lines: a reply is a result
	rs, err := c.Exec(ctx, "ZAP a b", 0)
	if err != nil || len(rs) != 1 || len(rs[0].Rows) == 0 || !strings.Contains(rs[0].Rows[0][0].S, "ZAP done") && !strings.Contains(rs[0].Rows[0][len(rs[0].Rows[0])-1].S, "ZAP done") {
		t.Errorf("exec: %+v %v", rs, err)
	}
	b, _ := os.ReadFile(log)
	if sent := strings.ToLower(string(b)); !strings.Contains(sent, "ping\n") || !strings.Contains(sent, "hgetall k\n") || !strings.Contains(sent, "zap a b\n") || strings.Contains(sent, "select") || strings.Contains(sent, "client") {
		t.Errorf("the command was run as:\n%s", sent)
	}
}

// What the connection says for itself is not run: SELECT 0, AUTH, CLIENT; any other db
// is an error (F17.8).
func TestRedisOverACommandLocalAnswers(t *testing.T) {
	argv, log := fakeRedis(t)
	ctx := context.Background()
	c, _, err := RedisDriver{Command: argv}.Connect(ctx, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = redis.Use(ctx, c, func(cl *goredis.Client) error {
		if err := cl.Do(ctx, "SELECT", "0").Err(); err != nil {
			t.Errorf("SELECT 0: %v", err)
		}
		if err := cl.Do(ctx, "SELECT", "3").Err(); err == nil || !strings.Contains(err.Error(), "db 0 only") {
			t.Errorf("SELECT 3: %v", err)
		}
		if err := cl.Do(ctx, "AUTH", "x").Err(); err != nil {
			t.Errorf("AUTH: %v", err)
		}
		return nil
	})
	if b, _ := os.ReadFile(log); strings.Contains(strings.ToLower(string(b)), "select") || strings.Contains(strings.ToLower(string(b)), "auth") {
		t.Errorf("run:\n%s", b)
	}
}

// Closing the connection while a command runs kills it (F17.8).
func TestRedisOverACommandCancel(t *testing.T) {
	argv, _ := fakeRedis(t)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	c, _, err := RedisDriver{Command: argv}.Connect(context.Background(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = redis.Use(ctx, c, func(cl *goredis.Client) error { return cl.Do(ctx, "SLOW").Err() })
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("%v after %v", err, time.Since(start))
	}
	if (RedisDriver{}).Broken(err) == db.LostSent {
		t.Errorf("a cancel is no connection lost with a command out: %v", (RedisDriver{}).Broken(err))
	}
}

// A key tab reads a hash through the command: its rows, in the order the command gave
// them or the tab's own, and its info (F17.8).
func TestKeyTabOverACommand(t *testing.T) {
	argv, _ := fakeRedis(t)
	ctx := context.Background()
	c, _, err := RedisDriver{Command: argv}.Connect(ctx, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	w := db.NewWorker(c)
	k := redis.Key{Meta: w, Main: w, Name: "k", LoadAll: 100, Type: "hash", ReadOnly: true}
	cols, err := k.Columns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := k.Page(ctx, db.PageQuery{Cols: cols, Limit: 50})
	if err != nil || len(p.Rows) != 3 {
		t.Fatalf("page: %v rows %v", err, p.Rows)
	}
	got := map[string]string{}
	for _, r := range p.Rows {
		got[r[0].S] = r[1].S
	}
	if got["alpha"] != "two words" || got["n"] != "3" || got["zeta"] != "1" {
		t.Errorf("rows %v", got)
	}
}

// The tree's scan over a command asks no TYPE: each would be a run, a thousand to a batch;
// the keys come with no type, and the tab of one asks its own when it opens (F17.8).
func TestScanOverACommandAsksNoType(t *testing.T) {
	argv, log := fakeRedis(t)
	ctx := context.Background()
	c, _, err := RedisDriver{Command: argv}.Connect(ctx, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := redis.Scan(ctx, c, "user:*", 100)
	if err != nil || len(s.Keys) != 2 || s.Keys[0] != (redis.KeyInfo{Name: "user:1"}) || s.Keys[1].Type != "" {
		t.Fatalf("scan: %+v %v", s, err)
	}
	if b, _ := os.ReadFile(log); strings.Contains(strings.ToLower(string(b)), "type") {
		t.Errorf("the scan ran TYPE:\n%s", b)
	}
	w := db.NewWorker(c)
	cols, err := (redis.Key{Meta: w, Main: w, Name: "user:1"}).Columns(ctx) // the tab: no type known yet
	if err != nil || len(cols.Cols) != 2 || cols.Cols[0].Name != "field" {
		t.Errorf("the tab's columns: %+v %v", cols, err)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(strings.ToLower(string(b)), "type user:1") {
		t.Errorf("the tab did not ask TYPE:\n%s", b)
	}
}
