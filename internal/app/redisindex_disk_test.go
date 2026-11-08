package app

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/config"
)

// The timer of a change is not waited for by the tests that run every command to its end.
func init() { indexSaveDelay = time.Millisecond }

// diskConsole is keyConsole with the data directory of its own, and the db's index not
// made yet: what the file has is all there is.
func diskConsole(t *testing.T) (*App, *queryTab, *Conn) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, c, conn := keyConsole(t)
	conn.index, conn.State = nil, connReady
	return a, c, conn
}

func writeIndexFile(t *testing.T, conn *Conn, ix *keyIndex) {
	t.Helper()
	if err := config.WriteRedisIndex(indexFile(conn), encodeIndex(ix)); err != nil {
		t.Fatal(err)
	}
}

func fileNames(t *testing.T, conn *Conn) []string {
	t.Helper()
	ix := decodeIndex(config.ReadRedisIndex(indexFile(conn)), 1<<20)
	if ix == nil {
		t.Fatalf("no index in %s", indexFile(conn))
	}
	return ix.names
}

// The file has the names a line each, as redis-cli reads a word, so that one with a line
// break, a quote or a blank in it is a line; the size of the db and whether the
// index stops short of it are in the first line; the user alone reads it (F21.4).
func TestIndexFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	names := []string{"plain", "a b", "line\nbreak", "cr\r\n", `"q" 'p'`, "#hash", "", "ключ", "bin\xff\x00"}
	ix := &keyIndex{names: slices.Clone(names), total: 12345, capped: true}
	got := decodeIndex(encodeIndex(ix), 100)
	if got == nil || !got.capped || got.total != 12345 || !got.stale || !got.done || len(got.cands) != len(names) {
		t.Fatalf("decoded: %+v", got)
	}
	slices.Sort(names)
	if !slices.Equal(got.names, names) {
		t.Errorf("names %q, want %q", got.names, names)
	}
	if bytes.Count(encodeIndex(ix), []byte("\n")) != len(names)+1 {
		t.Error("a name with a line break in it is more than a line")
	}
	if uncapped := decodeIndex(encodeIndex(&keyIndex{names: []string{"a"}, total: 1}), 100); uncapped == nil || uncapped.capped {
		t.Errorf("not capped: %+v", uncapped)
	}
	if empty := decodeIndex(encodeIndex(&keyIndex{}), 100); empty == nil || len(empty.names) != 0 {
		t.Errorf("an empty db: %+v", empty)
	}

	path := filepath.Join(config.DataDir(), "redis-index", "c", "db0")
	if err := config.WriteRedisIndex(path, encodeIndex(ix)); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the file: %v %v", fi, err)
	}
	if want := filepath.Join(config.DataDir(), "redis-index", "a%2Fb", "db3"); config.RedisIndexPath("a/b", "3") != want {
		t.Errorf("path %q, want %q", config.RedisIndexPath("a/b", "3"), want)
	}

	for name, text := range map[string]string{
		"not ours":          "hello\nworld\n",
		"empty":             "",
		"another version":   "sqlmux-redis-index 2 1 0\na\n",
		"no total":          "sqlmux-redis-index 1\na\n",
		"a quote left open": "sqlmux-redis-index 1 1 0\n\"a\n",
		"two words":         "sqlmux-redis-index 1 1 0\na b\n",
		"too many":          "sqlmux-redis-index 1 3 0\na\nb\nc\n",
	} {
		if decodeIndex([]byte(text), 2) != nil {
			t.Errorf("%s: read", name)
		}
	}
}

// The first completion of a db with a file has its names to match at once, says they are
// being updated, and the index made meanwhile takes their place and is written; a batch
// that failed leaves them, and the next completion makes the index again (F21.4).
func TestKeyIndexFromDisk(t *testing.T) {
	a, c, conn := diskConsole(t)
	writeIndexFile(t, conn, &keyIndex{names: []string{"old:1", "old:2", "gone"}, total: 3})

	cmd := a.startIndex(conn)
	if cmd == nil || conn.building == nil || conn.index == nil || !conn.index.stale || len(conn.index.names) != 3 {
		t.Fatalf("started: building %v, index %+v", conn.building, conn.index)
	}
	if got, foot, _ := a.keyCandidates(c, "old"); len(got) != 3 || foot != "索引更新中" {
		t.Errorf("candidates %d, foot %q", len(got), foot)
	}
	if cmd := a.startIndex(conn); cmd != nil {
		t.Error("started again while it is made")
	}

	a.gotIndex(indexMsg{conn: conn, ix: conn.building, err: errBoom})
	if conn.index == nil || !conn.index.stale || conn.building != nil {
		t.Fatalf("a failed batch: %+v", conn.index)
	}
	if a.startIndex(conn) == nil || conn.building == nil {
		t.Fatal("the index is not made again after a failure while the file's is used")
	}

	save := a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"new:1", "old:1"}, total: 2})
	if conn.index.stale || !slices.Equal(conn.index.names, []string{"new:1", "old:1"}) || save == nil {
		t.Fatalf("done: %+v", conn.index)
	}
	a.saveIndex(conn)()
	if got := fileNames(t, conn); !slices.Equal(got, []string{"new:1", "old:1"}) {
		t.Errorf("the file after it: %q", got)
	}
	if _, foot, _ := a.keyCandidates(c, "old"); foot != "" {
		t.Errorf("foot %q", foot)
	}
}

// A file that does not read is no file: the index is made and written over it; with
// index_cache = false the file is neither read nor written, and stays; a connection that
// is a command has none (F21.4).
func TestKeyIndexDiskOff(t *testing.T) {
	a, _, conn := diskConsole(t)
	path := indexFile(conn)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil || os.WriteFile(path, []byte("garbage\n"), 0o600) != nil {
		t.Fatal(err)
	}
	a.startIndex(conn)
	if conn.index != nil || conn.building == nil {
		t.Fatalf("a bad file: index %+v", conn.index)
	}
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"k"}, total: 1})
	a.saveIndex(conn)()
	if got := fileNames(t, conn); !slices.Equal(got, []string{"k"}) {
		t.Errorf("written over: %q", got)
	}

	writeIndexFile(t, conn, &keyIndex{names: []string{"on:disk"}, total: 1})
	before, _ := os.ReadFile(path)
	a.indexCache, conn.index, conn.building = false, nil, nil
	a.startIndex(conn)
	if conn.index != nil {
		t.Error("read with index_cache = false")
	}
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"other"}, total: 1})
	if a.saveIndex(conn) != nil || a.indexChanged(conn) != nil {
		t.Error("something is written with index_cache = false")
	}
	a.flushIndexes()
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Errorf("written with index_cache = false: %q", after)
	}

	a.indexCache, conn.Spec.Engine, conn.index = true, "exec", &keyIndex{names: []string{"x"}, done: true}
	if a.saveIndex(conn) != nil {
		t.Error("an exec connection's index is written")
	}
	if strings.Contains(string(before), "other") {
		t.Error("the file changed")
	}
}

// What a write does to the index is written once in a few seconds, not by each, and
// when the program ends; a stale index is not written (F21.4).
func TestKeyIndexDiskDebounce(t *testing.T) {
	a, _, conn := diskConsole(t)
	conn.index = &keyIndex{names: []string{"a"}, total: 1, done: true}
	conn.index.makeCands()
	a.saveIndex(conn)()
	set := func(key string) {
		t.Helper()
		a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch, keys: []string{key}, found: []bool{true}})
	}
	set("b")
	if !conn.indexDirty || a.indexChanged(conn) != nil {
		t.Error("the first change starts the wait, the ones after it have it")
	}
	set("c")
	if got := fileNames(t, conn); !slices.Equal(got, []string{"a"}) {
		t.Errorf("written before the wait is over: %q", got)
	}
	a.gotIndexSave(indexSave{conn})()
	if got := fileNames(t, conn); !slices.Equal(got, []string{"a", "b", "c"}) || conn.indexDirty {
		t.Errorf("after the wait: %q", got)
	}
	if a.gotIndexSave(indexSave{conn}) != nil {
		t.Error("written again with nothing changed")
	}

	set("d") // the program ends before the wait is over
	a.flushIndexes()
	if got := fileNames(t, conn); !slices.Equal(got, []string{"a", "b", "c", "d"}) || conn.indexDirty {
		t.Errorf("at the end: %q", got)
	}

	conn.index.stale = true // one read from its file, the new one on its way: nothing of it is written
	if a.indexChanged(conn) != nil || a.saveIndex(conn) != nil {
		t.Error("a stale index is written")
	}
}
