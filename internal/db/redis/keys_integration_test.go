//go:build integration

package redis

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/kackerx/sqlmux/internal/db"
)

func connectTo(t *testing.T, dsn string) *Conn {
	t.Helper()
	c, _, err := Connect(context.Background(), dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func names(ks []KeyInfo) (out []string) {
	for _, k := range ks {
		out = append(out, k.Name)
	}
	slices.Sort(out)
	return out
}

// A scan a batch at a time, each Step a request, reads what Scan reads: the same keys with
// the same types, to the budget, the size of the db from the first, more than one Step for
// a db of 12000 keys (F21.7).
func TestScanning(t *testing.T) {
	ctx := context.Background()
	c := connectTo(t, IntegrationDSN(t))
	all, err := Scan(ctx, c, "*", 100000, ScanCount)
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int{100000, 1500} {
		sc, steps := NewScanning("*", budget, ScanCount), 0
		for done := false; !done; steps++ {
			if done, err = sc.Step(ctx, c); err != nil {
				t.Fatal(err)
			}
		}
		want, err := Scan(ctx, c, "*", budget, ScanCount)
		if err != nil || steps < 2 || sc.Total != all.Total || sc.Complete != want.Complete || !slices.Equal(names(sc.Keys), names(want.Keys)) {
			t.Errorf("budget %d: %d steps, %d keys of %d, complete %v (%v), total %d, %v", budget, steps, len(sc.Keys), len(want.Keys), sc.Complete, want.Complete, sc.Total, err)
		}
		for _, k := range sc.Keys {
			if k.Name == "hash:user" && k.Type != "hash" {
				t.Errorf("type of hash:user: %q", k.Type)
			}
		}
	}
}

// A scan reads every key of a db once with its type, stops at its budget, takes a
// pattern, and sees the size of the db whatever it read (F16.2, seed db0).
func TestScan(t *testing.T) {
	ctx := context.Background()
	c := connectTo(t, IntegrationDSN(t))
	all, err := Scan(ctx, c, "*", 100000, ScanCount)
	if err != nil || !all.Complete || all.Total < 12000 || int64(len(all.Keys)) != all.Total {
		t.Fatalf("all: %d keys of %d, complete %v, %v", len(all.Keys), all.Total, all.Complete, err)
	}
	if len(names(all.Keys)) != len(slices.Compact(names(all.Keys))) {
		t.Error("a key twice")
	}
	types := map[string]string{}
	for _, k := range all.Keys {
		types[k.Name] = k.Type
	}
	for name, want := range map[string]string{"str:plain": "string", "hash:user": "hash", "list:queue": "list", "set:tags": "set", "zset:rank": "zset", "stream:events": "stream", "str:binary": "string", "bulk:777": "string"} {
		if types[name] != want {
			t.Errorf("%s is %q, want %q", name, types[name], want)
		}
	}
	cut, err := Scan(ctx, c, "*", 1500, ScanCount)
	if err != nil || cut.Complete || len(cut.Keys) != 1500 || cut.Total != all.Total {
		t.Errorf("budget: %d keys, complete %v, total %d, %v", len(cut.Keys), cut.Complete, cut.Total, err)
	}
	app, err := Scan(ctx, c, "app:*", 100, ScanCount)
	if err != nil || !app.Complete || !slices.Equal(names(app.Keys), []string{"app:config", "app:orders:100", "app:orders:101", "app:users:1", "app:users:2", "app:users:3"}) {
		t.Errorf("app:*: %v %v", names(app.Keys), err)
	}
	if one, err := Scan(ctx, c, "app:users:1", 100, ScanCount); err != nil || len(one.Keys) != 1 {
		t.Errorf("a pattern with no glob: %v %v", names(one.Keys), err)
	}
	if none, err := Scan(ctx, c, "nothing:*", 100, ScanCount); err != nil || !none.Complete || len(none.Keys) != 0 {
		t.Errorf("no match: %v %v", none, err)
	}
	// each: every key, no budget
	n := 0
	if err := Each(ctx, c, "bulk:*", func(ks []string) error { n += len(ks); return nil }); err != nil || n != 12000 {
		t.Errorf("each: %d %v", n, err)
	}
	stop := context.DeadlineExceeded
	if err := Each(ctx, c, "bulk:*", func([]string) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("each stopped by its function: %v", err)
	}
}

// Databases are those with keys, by number; Glob names a key that has the
// glob's characters in its name; delete, rename and TTL on a db of the test's
// own.
func TestKeyOperations(t *testing.T) {
	ctx := context.Background()
	if dbs, err := Databases(ctx, connectTo(t, IntegrationDSN(t))); err != nil || !slices.Contains(dbs, "0") || !slices.Contains(dbs, "1") || slices.Index(dbs, "0") > slices.Index(dbs, "1") {
		t.Errorf("databases: %v %v", dbs, err)
	}
	c := connectTo(t, OwnDB(t))
	for _, k := range []string{"we*ird", "we?ird", "we[i]rd", `we\ird`, "weXird", "a", "b"} {
		if _, err := c.Query(ctx, "SET", db.Val{S: k}, db.Val{S: "1"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []string{"we*ird", "we?ird", "we[i]rd", `we\ird`} {
		got, err := Scan(ctx, c, Glob(k), 100, ScanCount)
		if err != nil || !slices.Equal(names(got.Keys), []string{k}) {
			t.Errorf("Glob(%q): %v %v", k, names(got.Keys), err)
		}
	}
	// a budget under what one SCAN gives cuts the batch, and the scan is not complete (the 7 keys and the one that claims the db)
	if cut, err := Scan(ctx, c, "*", 3, ScanCount); err != nil || len(cut.Keys) != 3 || cut.Complete {
		t.Errorf("a budget under one batch: %d keys, complete %v, %v", len(cut.Keys), cut.Complete, err)
	}
	if all, err := Scan(ctx, c, "*", 8, ScanCount); err != nil || len(all.Keys) != 8 || !all.Complete {
		t.Errorf("a budget that is the size: %d keys, complete %v, %v", len(all.Keys), all.Complete, err)
	}
	if exists, err := Rename(ctx, c, "a", "b", false); err != nil || !exists {
		t.Errorf("rename onto a key: exists %v %v", exists, err)
	}
	if r, _ := c.Query(ctx, "EXISTS", db.Val{S: "a"}); r.Rows[0][0].S != "1" {
		t.Error("a key that was not renamed is gone")
	}
	if exists, err := Rename(ctx, c, "a", "b", true); err != nil || exists {
		t.Errorf("rename over: exists %v %v", exists, err)
	}
	if r, _ := c.Query(ctx, "EXISTS", db.Val{S: "a"}, db.Val{S: "b"}); r.Rows[0][0].S != "1" {
		t.Errorf("after the rename: %v", r.Rows)
	}
	if exists, err := Rename(ctx, c, "b", "free", false); err != nil || exists {
		t.Errorf("rename to a free name: exists %v %v", exists, err)
	}
	if _, err := Rename(ctx, c, "nosuch", "x", false); err == nil {
		t.Error("renaming a key that is not there")
	}
	ttl := func() string { r, _ := c.Query(ctx, "TTL", db.Val{S: "free"}); return r.Rows[0][0].S }
	if err := SetTTL(ctx, c, "free", 90*time.Second); err != nil || ttl() == "-1" {
		t.Errorf("a TTL: %s %v", ttl(), err)
	}
	if err := SetTTL(ctx, c, "free", 0); err != nil || ttl() != "-1" {
		t.Errorf("no TTL: %s %v", ttl(), err)
	}
	if err := SetTTL(ctx, c, "nosuch", time.Minute); err == nil {
		t.Error("a TTL of a key that is not there")
	}
	if n, err := Unlink(ctx, c, "free", "we*ird", "nosuch"); err != nil || n != 2 {
		t.Errorf("unlink: %d %v", n, err)
	}
}

// Completing key names reads a few rounds of SCAN, not the db; fields of a hash are
// its HKEYS, unless it is longer than the limit or is no hash (F16.5).
func TestKeyHints(t *testing.T) {
	ctx := context.Background()
	c := connectTo(t, IntegrationDSN(t)) // db0: 12000 bulk keys
	start := time.Now()
	// db0 has more keys than the rounds read, and which of them are seen is up to the hash seed of the
	// server: what it gives is some of the keys that match, in a db of the test's own all of them
	got, err := KeyHints(ctx, c, "app:users:", 50)
	if err != nil || len(got) > 3 || slices.IndexFunc(got, func(k string) bool { return !strings.HasPrefix(k, "app:users:") }) >= 0 {
		t.Errorf("app:users: %v %v", got, err)
	}
	own := connectTo(t, OwnDB(t))
	for _, k := range []string{"app:users:1", "app:users:2", "app:users:3", "other"} {
		if _, err := own.Query(ctx, "SET", db.Val{S: k}, db.Val{S: "1"}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := KeyHints(ctx, own, "app:users:", 50); err != nil || !slices.Equal(got, []string{"app:users:1", "app:users:2", "app:users:3"}) {
		t.Errorf("app:users: of a db of its own: %v %v", got, err)
	}
	if got, err := KeyHints(ctx, c, "bulk:1", 20); err != nil || len(got) != 20 || !strings.HasPrefix(got[0], "bulk:1") {
		t.Errorf("bulk:1: %d %v %v", len(got), got, err)
	}
	if got, err := KeyHints(ctx, c, "nothing:", 20); err != nil || len(got) != 0 {
		t.Errorf("nothing: %v %v", got, err)
	}
	for _, k := range []string{"a*b1", "axb2"} {
		if _, err := own.Query(ctx, "SET", db.Val{S: k}, db.Val{S: "1"}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := KeyHints(ctx, own, "a*b", 20); err != nil || !slices.Equal(got, []string{"a*b1"}) {
		t.Errorf("a glob character in the prefix is itself: %v %v", got, err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
	if f, err := HashFields(ctx, c, "hash:user", 100); err != nil || !slices.Equal(f, []string{"age", "email", "meta", "name"}) {
		t.Errorf("fields %v %v", f, err)
	}
	if f, err := HashFields(ctx, c, "hash:big", 100); err != nil || len(f) != 0 {
		t.Errorf("a long hash: %d %v", len(f), err)
	}
	if f, err := HashFields(ctx, c, "str:plain", 100); err != nil || len(f) != 0 {
		t.Errorf("a string: %v %v", f, err)
	}
	if f, err := HashFields(ctx, c, "no:such", 100); err != nil || len(f) != 0 {
		t.Errorf("no key: %v %v", f, err)
	}
}

// scanHook counts the SCANs a connection sends, and runs onScan after each.
type scanHook struct {
	n      atomic.Int32
	onScan func()
}

func (h *scanHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (h *scanHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == "scan" {
			h.n.Add(1)
			if h.onScan != nil {
				h.onScan()
			}
		}
		return err
	}
}
func (h *scanHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

// The filter's scan of a db of 60000 keys takes at most 8 round trips with
// FilterCount where ScanCount takes about 60, finds the same keys, and a cancel
// of its context stops it after the SCAN it is in (F18.11).
func TestScanFilterRoundTrips(t *testing.T) {
	ctx := context.Background()
	c := connectTo(t, OwnDB(t))
	if err := Use(ctx, c, func(cl *goredis.Client) error {
		_, err := cl.Pipelined(ctx, func(p goredis.Pipeliner) error {
			for i := range 60000 {
				k := "other:" + strconv.Itoa(i)
				if i%200 == 0 {
					k = "p:employees:" + strconv.Itoa(i) + "@example.com"
				}
				p.Set(ctx, k, "1", 0)
			}
			return nil
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h := &scanHook{}
	Of(c).c.AddHook(h)
	count := func(n int) int32 {
		h.n.Store(0)
		s, err := Scan(ctx, c, "*p:employees*", 100000, n)
		if err != nil || !s.Complete || len(s.Keys) != 300 {
			t.Fatalf("count %d: %d keys, complete %v, %v", n, len(s.Keys), s.Complete, err)
		}
		return h.n.Load()
	}
	if n := count(FilterCount); n > 8 {
		t.Errorf("FilterCount: %d SCANs", n)
	}
	if n := count(ScanCount); n <= 8 {
		t.Errorf("ScanCount: %d SCANs, the test says nothing", n)
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	h.n.Store(0)
	h.onScan = cancel
	if _, err := Scan(cctx, c, "*p:employees*", 100000, FilterCount); err == nil || h.n.Load() > 2 {
		t.Errorf("cancelled after the first SCAN: %d SCANs, %v", h.n.Load(), err)
	}
}
