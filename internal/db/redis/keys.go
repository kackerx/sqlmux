package redis

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// KeyInfo is a key of a db and its type, as TYPE says it: string, hash, list,
// set, zset, stream or ReJSON-RL, the type of a RedisJSON value.
type KeyInfo struct{ Name, Type string }

// Scanned is what a scan of a db read: the keys, the db's size (DBSIZE), and
// whether the scan reached the end or stopped at its budget.
type Scanned struct {
	Keys     []KeyInfo
	Total    int64
	Complete bool
}

// ScanCount is the COUNT of each SCAN a db's keys are listed with: a hint of how
// many keys the server looks at, one round trip each, so the first keys come
// early; FilterCount is the COUNT of the SCANs that are as many round trips as keys
// over it: the filter's, which MATCH thins on the server, the prefix hints', and
// the index of names that is read whole (§8.7, §9.8, F18.11, F20.11).
const (
	ScanCount   = 1000
	FilterCount = 10000
)

// scanKeys gives take each batch of the keys of the db cl is on that match the
// glob match, SCAN by SCAN and never KEYS, until the cursor comes round
// (complete) or budget keys are given, 0 for no budget. A key SCAN gives twice is
// once. take stopping it is that error.
func scanKeys(ctx context.Context, cl *goredis.Client, match string, budget, count int, take func(keys []string) error) (complete bool, err error) {
	// ponytail: SCAN may give a key twice while the table grows; the keys seen are
	// kept to drop it, which costs memory as the db is big and has no test, a real
	// server giving it rarely
	seen, n := map[string]bool{}, 0
	for cursor := uint64(0); ; {
		batch, next, err := cl.Scan(ctx, cursor, match, int64(count)).Result()
		if err != nil {
			return false, err
		}
		batch = slices.DeleteFunc(batch, func(k string) bool { dup := seen[k]; seen[k] = true; return dup })
		if room := budget - n; budget > 0 && len(batch) > room {
			batch = batch[:room]
			next = 1 // the budget ends it
		}
		n += len(batch)
		if len(batch) > 0 {
			if err := take(batch); err != nil {
				return false, err
			}
		}
		if next == 0 {
			return true, nil
		}
		if budget > 0 && n >= budget {
			return false, nil
		}
		cursor = next
	}
}

// Scan reads the keys of the db c is on that match the glob match until the
// cursor comes round or budget keys are read; the type of each batch comes in
// one pipeline (§8.7「树」), not on a connection that is a command (§8.8). The
// keys are in the order they are read; count is the COUNT of each SCAN.
func Scan(ctx context.Context, c db.Conn, match string, budget, count int) (s Scanned, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) (err error) {
		if s.Total, err = cl.DBSize(ctx).Result(); err != nil {
			return err
		}
		s.Complete, err = scanKeys(ctx, cl, match, budget, count, func(batch []string) error {
			if Of(c).untyped { // KeyInfo.Type "": the generic key icon
				for _, k := range batch {
					s.Keys = append(s.Keys, KeyInfo{Name: k})
				}
				return nil
			}
			types, err := typesOf(ctx, cl, batch)
			for i, k := range batch[:len(types)] {
				s.Keys = append(s.Keys, KeyInfo{k, types[i]})
			}
			return err
		})
		return err
	})
	return s, err
}

// typesOf is the type of each of keys, one pipeline.
func typesOf(ctx context.Context, cl *goredis.Client, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	cmds, err := cl.Pipelined(ctx, func(p goredis.Pipeliner) error {
		for _, k := range keys {
			p.Type(ctx, k)
		}
		return nil
	})
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, err
	}
	types := make([]string, len(cmds))
	for i, cmd := range cmds {
		types[i] = cmd.(*goredis.StatusCmd).Val()
	}
	return types, nil
}

// Each gives each batch of the keys of the db c is on that match the glob match,
// every one, with no budget: what a delete of a namespace says it deletes
// (§8.7). each stopping it is that error.
func Each(ctx context.Context, c db.Conn, match string, each func(keys []string) error) error {
	return Use(ctx, c, func(cl *goredis.Client) error {
		_, err := scanKeys(ctx, cl, match, 0, ScanCount, each)
		return err
	})
}

// Glob is name as a glob that matches it alone: MATCH treats * ? [ ] and \ as
// its own.
func Glob(name string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`).Replace(name)
}

// Databases are the dbs of the server that hold keys, by number as INFO
// keyspace lists them, which is in order.
func Databases(ctx context.Context, c db.Conn) (dbs []string, err error) {
	var text string
	err = Use(ctx, c, func(cl *goredis.Client) (err error) { text, err = cl.Info(ctx, "keyspace").Result(); return err })
	for l := range strings.SplitSeq(text, "\n") {
		if name, _, ok := strings.Cut(strings.TrimSpace(l), ":"); ok && strings.HasPrefix(name, "db") {
			if n, err := strconv.Atoi(name[2:]); err == nil {
				dbs = append(dbs, strconv.Itoa(n))
			}
		}
	}
	return dbs, err
}

// Unlink deletes keys without blocking the server, and says how many were
// there.
func Unlink(ctx context.Context, c db.Conn, keys ...string) (n int64, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) (err error) { n, err = cl.Unlink(ctx, keys...).Result(); return err })
	return n, err
}

// Rename makes key to; to a key that is there is left alone and exists says it
// is, unless overwrite (§8.7).
func Rename(ctx context.Context, c db.Conn, key, to string, overwrite bool) (exists bool, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) error {
		if overwrite {
			return cl.Rename(ctx, key, to).Err()
		}
		ok, err := cl.RenameNX(ctx, key, to).Result()
		exists = !ok && err == nil
		return err
	})
	return exists, err
}

// SetTTL gives key a time to live, or none when ttl is 0 (PERSIST).
func SetTTL(ctx context.Context, c db.Conn, key string, ttl time.Duration) error {
	return Use(ctx, c, func(cl *goredis.Client) error {
		if ttl <= 0 {
			return cl.Persist(ctx, key).Err()
		}
		ok, err := cl.PExpire(ctx, key, ttl).Result()
		if err == nil && !ok {
			err = errors.New(i18n.T("键不存在"))
		}
		return err
	})
}

// ParseTTL is what the TTL of the form says: seconds (3600), a duration
// (90m, 1h30m), or nothing, 0 or -1 for none; ok is false for any other.
func ParseTTL(s string) (ttl time.Duration, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || s == "-1" {
		return 0, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, n > 0
	}
	d, err := time.ParseDuration(s)
	return d, err == nil && d > 0
}

// hintRounds is how many SCANs a completion of key names makes at most: it looks at
// a few thousand keys, not the whole of a db that has millions (§9.8).
const hintRounds = 10

// KeyHints are up to want keys of the db c is on that start with prefix, from SCAN
// MATCH, a round at a time till there are want of them or hintRounds are done; the
// answer is what it found, which for a big db and a rare prefix may not be all.
func KeyHints(ctx context.Context, c db.Conn, prefix string, want int) (keys []string, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) error {
		var cursor uint64
		for range hintRounds {
			batch, next, err := cl.Scan(ctx, cursor, Glob(prefix)+"*", FilterCount).Result()
			if err != nil {
				return err
			}
			for _, k := range batch {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
			if cursor = next; cursor == 0 || len(keys) >= want {
				break
			}
		}
		return nil
	})
	slices.Sort(keys)
	return keys[:min(len(keys), want)], err
}

// ScanBatch is one SCAN of the db c is on from cursor, with COUNT count: the
// names it gave, which may repeat what an earlier round gave, and the cursor to
// go on from, 0 at the end. The index of key names is built a batch at a time,
// each one a request of its own (§9.8).
func ScanBatch(ctx context.Context, c db.Conn, cursor uint64, count int) (names []string, next uint64, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) (err error) {
		names, next, err = cl.Scan(ctx, cursor, "*", int64(count)).Result()
		return err
	})
	return names, next, err
}

// Exists is whether each of keys is in the db c is on: one EXISTS for a key, and for
// more a pipeline of them, one round trip, as EXISTS with several keys counts and
// does not say which (§9.8, F20.8).
func Exists(ctx context.Context, c db.Conn, keys []string) (found []bool, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) error {
		cmds, err := cl.Pipelined(ctx, func(p goredis.Pipeliner) error {
			for _, k := range keys {
				p.Exists(ctx, k)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, cmd := range cmds {
			found = append(found, cmd.(*goredis.IntCmd).Val() > 0)
		}
		return nil
	})
	return found, err
}

// DBSize is how many keys the db c is on holds.
func DBSize(ctx context.Context, c db.Conn) (n int64, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) (err error) { n, err = cl.DBSize(ctx).Result(); return err })
	return n, err
}

// HashFields are the fields of the hash key when it has no more than limit of them
// (HKEYS reads them all); none for a longer one, a key that is not a hash, or none.
func HashFields(ctx context.Context, c db.Conn, key string, limit int) (fields []string, err error) {
	err = Use(ctx, c, func(cl *goredis.Client) error {
		n, err := cl.HLen(ctx, key).Result()
		if err != nil && strings.HasPrefix(err.Error(), "WRONGTYPE") || err == nil && n > int64(limit) {
			return nil
		}
		if err != nil {
			return err
		}
		fields, err = cl.HKeys(ctx, key).Result()
		return err
	})
	slices.Sort(fields)
	return fields, err
}
