package app

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// keyConsole is a console of a Redis connection whose db has names, indexed.
func keyConsole(t *testing.T, names ...string) (*App, *queryTab, *Conn) {
	t.Helper()
	a, c := redisConsole(t, &execDB{}, someCommands(), "")
	conn := a.sess.Conns[0]
	conn.docs = someDocs()
	conn.index = &keyIndex{names: names, total: int64(len(names)), done: true}
	conn.index.makeCands()
	return a, c, conn
}

// The index of key names is matched as the tables' are: a word starts after
// _ . - $ and :, so expert finds i18n_agent:expert:x and fntfp the five words of a
// name; what is typed is lit where it matched (§9.8, F20.1).
func TestKeyIndexMatches(t *testing.T) {
	a, c, _ := keyConsole(t, "i18n:a", "i18n_agent:expert:x", "i18n_agent:other", "feature:notify:team:flag:prod", "user:1", "xexpert")
	for _, tc := range []struct{ typed, want string }{
		{"get expert", "i18n_agent:expert:x"},
		{"get i18n_", "i18n_agent:expert:x i18n_agent:other"}, // best first: the order is the matcher's
		{"get fntfp", "feature:notify:team:flag:prod"},
		{"get us", "user:1"},
		{"get zzz", ""},
	} {
		got := typedAt(t, a, c, tc.typed, false)
		slices.Sort(got)
		if got := strings.Join(got, " "); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.typed, got, tc.want)
		}
	}
	typedAt(t, a, c, "get expert", false)
	c.ed.Load("get expert")
	a.win().focus(2)
	a.queryComplete(c, false)
	if c.comp != nil {
		t.Fatal("not in INSERT: no list")
	}
	feed(t, a, "A")
	a.queryComplete(c, false)
	if c.comp == nil || !slices.Equal(c.comp.items[0].pos, []int{11, 12, 13, 14, 15, 16}) { // "i18n_agent:" is 11 columns
		t.Errorf("lit: %+v", c.comp)
	}
	if tierOf("user", "App:USER_1") != 2 || tierOf("user", "USER_1") != 1 || tierOf("user", "xuser") != 3 {
		t.Error("the case of a name does not count, a word start does")
	}
	if tierOf("expert", "i18n:expert") != 2 || tierOf("expert", "expert:i18n") != 1 {
		t.Error(": is not a word start")
	}
}

// The index is made a SCAN at a time the first time key names are asked for,
// the names of one batch twice once, till the cursor comes round; with the db
// it asks the size; a batch of one that was let go is nothing, and a batch that
// failed drops the one being made, the index there is staying (§9.8, F20.8).
func TestKeyIndexBuilds(t *testing.T) {
	a, c, conn := keyConsole(t)
	conn.index = nil
	if _, _, cmds := a.keyCandidates(c, "zz"); conn.building == nil || len(cmds) != 2 || !conn.building.started { // the index's first batch, and the prefix scan till it is
		t.Fatalf("not started: %+v, %d commands", conn.building, len(cmds))
	}
	ix := conn.building
	if _, _, cmds := a.keyCandidates(c, "zz"); len(cmds) != 1 { // only the prefix's, the index being on its way
		t.Errorf("started again: %d commands", len(cmds))
	}
	if cmd := a.gotIndex(indexMsg{conn: conn, ix: ix, names: []string{"a", "b", "a"}, next: 7, total: 5}); cmd == nil || len(ix.names) != 2 || ix.done || ix.total != 5 || ix.cursor != 7 || conn.index != nil {
		t.Fatalf("a batch: %+v", ix)
	}
	a.gotIndex(indexMsg{conn: conn, ix: ix, names: []string{"b", "c"}})
	if !ix.done || ix.capped || !slices.Equal(ix.names, []string{"a", "b", "c"}) || len(ix.cands) != 3 || conn.indexed() != ix || conn.building != nil {
		t.Fatalf("done: %+v", ix)
	}
	if _, foot, cmds := a.keyCandidates(c, "zz"); foot != "" || len(cmds) != 0 {
		t.Errorf("an index that has it all: foot %q, %d commands", foot, len(cmds))
	}

	// one made again: the index there is used meanwhile, one let go is nothing, a
	// batch that failed leaves the index and drops the one being made
	a.rebuildIndex(conn)
	old := conn.building
	a.rebuildIndex(conn)
	if a.gotIndex(indexMsg{conn: conn, ix: old, names: []string{"x"}, next: 0}); len(old.names) != 0 || conn.index != ix {
		t.Error("a batch of an index that was let go is taken")
	}
	if conn.indexed() != ix || len(ix.names) != 3 {
		t.Error("the index is not used while another is made")
	}
	if a.gotIndex(indexMsg{conn: conn, ix: conn.building, err: errBoom}); conn.building != nil || conn.index != ix {
		t.Error("a failed batch takes the index with it")
	}
	a.rebuildIndex(conn)
	next := conn.building
	a.gotIndex(indexMsg{conn: conn, ix: next, names: []string{"n"}})
	if conn.index != next || conn.building != nil || !slices.Equal(next.names, []string{"n"}) {
		t.Errorf("the new index takes the place: %+v", conn.index)
	}
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }

// index_keys stops the index: the names past it are not kept, and the list says
// how many of the db it knows (§9.8).
func TestKeyIndexCapped(t *testing.T) {
	a, c, conn := keyConsole(t)
	conn.index = nil
	a.indexKeys = 3
	a.keyCandidates(c, "q")
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"a", "b", "c", "d", "e"}, next: 9, total: 12})
	ix := conn.index
	if !ix.done || !ix.capped || len(ix.names) != 3 {
		t.Fatalf("capped: %+v", ix)
	}
	if _, foot, _ := a.keyCandidates(c, "q"); foot != "已索引 3 / 共 12 键" {
		t.Errorf("foot %q", foot)
	}
	typedAt(t, a, c, "get ", true)
	feed(t, a, "A")
	a.queryComplete(c, true)
	if c.comp == nil || c.comp.foot != "已索引 3 / 共 12 键" {
		t.Errorf("the list's foot: %+v", c.comp)
	}
	conn.index.capped = false
	if a.queryComplete(c, true); c.comp.foot != "" {
		t.Errorf("a whole index has none: %q", c.comp.foot)
	}
}

// What a completion keeps of a big index is the best of what matches.
func TestKeyIndexShown(t *testing.T) {
	var names []string
	for i := range 5 * redisShown {
		names = append(names, "k:"+strings.Repeat("x", i%7)+string(rune('a'+i%26)))
	}
	a, c, _ := keyConsole(t, names...)
	feed(t, a, "A")
	c.ed.Load("get k")
	feed(t, a, "A")
	a.queryComplete(c, false)
	if c.comp == nil || len(c.comp.items) != redisShown {
		t.Errorf("%d candidates", len(c.comp.items))
	}
}

// The tree's keys are the index when it read the whole db, and again when R reads it
// so; when the budget stopped it, the index is built, and an R makes it again in the
// background, the index there is used till then (§9.8, F20.8).
func TestKeyIndexOfTheTree(t *testing.T) {
	a, c, conn := keyConsole(t)
	conn.index = nil
	conn.scan = &keyScan{keys: []redis.KeyInfo{{Name: "u:1", Type: "string"}, {Name: "u:2", Type: "hash"}}, total: 2}
	if _, _, cmds := a.keyCandidates(c, "u"); len(cmds) != 0 || conn.indexed() == nil || len(conn.index.names) != 2 {
		t.Fatalf("the tree's: %d commands, %+v", len(cmds), conn.index)
	}
	conn.Spec.Engine = "exec" // a command's tree is used all the same
	conn.index = nil
	if conn.indexed() == nil {
		t.Error("the tree of an exec connection is not used")
	}
	conn.index = nil
	conn.scan.more = true
	if conn.indexed() != nil {
		t.Error("a tree that stopped at its budget is no index")
	}
	if a.startIndex(conn) != nil || conn.building != nil { // and a connection that is a command builds none
		t.Error("an index of an exec connection is built")
	}
	conn.Spec.Engine = "redis"

	// R: a tree that read it all is the index at once; one that did not makes the
	// index there is again behind it; none, none
	conn.index = &keyIndex{done: true, names: []string{"old"}}
	conn.scan = nil
	a.gotKeys(keysMsg{conn: conn, scan: &keyScan{keys: []redis.KeyInfo{{Name: "u:1"}, {Name: "u:9"}}, total: 2}})
	if ix := conn.indexed(); ix == nil || !slices.Equal(ix.names, []string{"u:1", "u:9"}) || conn.building != nil {
		t.Errorf("R with a whole tree: %+v", conn.index)
	}
	old := conn.index
	if cmd := a.refreshIndex(conn); cmd != nil && conn.index == old {
		t.Error("a tree that read it all is not the index anew")
	}
	conn.index, conn.building = old, nil
	if cmd := a.gotKeys(keysMsg{conn: conn, scan: &keyScan{keys: []redis.KeyInfo{{Name: "x"}}, total: 9, more: true}}); cmd == nil || conn.index != old || conn.building == nil || conn.building == old {
		t.Errorf("R with a tree that stopped: index kept %v, building %v", conn.index == old, conn.building != nil)
	}
	conn.index, conn.building = nil, nil
	a.gotKeys(keysMsg{conn: conn, scan: &keyScan{total: 9, more: true}})
	if conn.index != nil || conn.building != nil {
		t.Errorf("R with no index to make again makes one: %+v %+v", conn.index, conn.building)
	}
	// a tree that read it all while an index is being made: the tree's keys are the
	// index and what was being made is let go
	conn.building = &keyIndex{seen: map[string]bool{}}
	let := conn.building
	a.gotKeys(keysMsg{conn: conn, scan: &keyScan{keys: []redis.KeyInfo{{Name: "u:1"}}, total: 1}})
	if conn.building != nil || conn.index == nil || len(conn.index.names) != 1 {
		t.Errorf("a whole tree while one is made: index %v, building %v", conn.index, conn.building)
	}
	if a.gotIndex(indexMsg{conn: conn, ix: let, names: []string{"x", "y"}}); len(conn.index.names) != 1 {
		t.Error("the batches of the one that was let go are taken")
	}
}

// writeDocs are the docs of some commands with keys, as COMMAND DOCS has them.
func writeDocs() redis.Docs {
	key := redis.Arg{Name: "key", Type: "key"}
	val := redis.Arg{Name: "value", Type: "string"}
	return redis.Docs{
		"set":     {Args: []redis.Arg{key, val, {Name: "condition", Type: "oneof", Optional: true, Args: []redis.Arg{{Name: "nx", Type: "pure-token", Token: "NX"}, {Name: "xx", Type: "pure-token", Token: "XX"}}}}},
		"del":     {Args: []redis.Arg{{Name: "key", Type: "key", Multiple: true}}},
		"rename":  {Args: []redis.Arg{{Name: "key", Type: "key"}, {Name: "newkey", Type: "key"}}},
		"expire":  {Args: []redis.Arg{key, {Name: "seconds", Type: "integer"}}},
		"mset":    {Args: []redis.Arg{{Name: "data", Type: "block", Multiple: true, Args: []redis.Arg{key, val}}}},
		"get":     {Args: []redis.Arg{key}},
		"eval":    {Args: []redis.Arg{{Name: "script", Type: "string"}, {Name: "numkeys", Type: "integer"}, {Name: "key", Type: "key", Optional: true, Multiple: true}}},
		"flushdb": {},
		"exec":    {},
		"select":  {Args: []redis.Arg{{Name: "index", Type: "integer"}}},
		"swapdb":  {Args: []redis.Arg{{Name: "index1", Type: "integer"}, {Name: "index2", Type: "integer"}}},
	}
}

// What the server's docs say are the keys of a command: the ones a write may make or
// take away; a command that counts its keys first, or is not in the docs, has none
// that can be told (§9.8, F20.8).
func TestWriteKeys(t *testing.T) {
	_, _, conn := keyConsole(t)
	conn.docs = writeDocs()
	for _, tc := range []struct {
		line  string
		keys  []string
		known bool
	}{
		{"SET new 1", []string{"new"}, true},
		{"set k v NX", []string{"k"}, true},
		{`SET "a b" 1`, []string{"a b"}, true},
		{"DEL a b c", []string{"a", "b", "c"}, true},
		{"RENAME a b", []string{"a", "b"}, true},
		{"EXPIRE k 0", []string{"k"}, true},
		{"MSET a 1 b 2", []string{"a", "b"}, true},
		{"SELECT 3", nil, true},
		{"EVAL script 1 k", nil, false},
		{"EXEC", nil, false},
		{"NOSUCH k", nil, false},
	} {
		words, _ := redis.SplitLine(tc.line)
		keys, known := conn.writeKeys(words)
		if !slices.Equal(keys, tc.keys) || known != tc.known {
			t.Errorf("%s: %q %v, want %q %v", tc.line, keys, known, tc.keys, tc.known)
		}
	}
}

// A write that ran changes the index by what the server says of its keys, in one
// ask; FLUSHDB empties the db's and FLUSHALL every db's of the connection; a
// command whose keys are not known makes the index again with the old one used
// meanwhile; an answer for an index that was replaced is nothing, and one that
// failed makes the index again. The slices an update leaves are new (§9.8, F20.8).
func TestKeyIndexWrites(t *testing.T) {
	cmds := someCommands()
	for _, name := range []string{"del", "rename", "expire", "mset", "eval", "flushdb", "flushall", "swapdb"} {
		cmds[name] = redis.Command{Flags: []string{"write"}}
	}
	a, _, conn := keyConsole(t, "a", "b", "k")
	conn.docs, conn.cmds = writeDocs(), cmds
	ix := conn.index
	names := func() string { return strings.Join(conn.index.names, " ") }
	step := func(line string, keys []string, found ...bool) {
		t.Helper()
		before, beforeC := slices.Clone(ix.names), slices.Clone(ix.cands)
		snapNames, snapCands := ix.names, ix.cands
		cmd := a.wrote(conn, line)
		if cmd == nil {
			t.Fatalf("%s asks nothing", line)
		}
		if got := a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch, keys: keys, found: found}); got != nil && a.palette == nil {
			_ = got
		}
		if !slices.Equal(snapNames, before) || !slices.EqualFunc(snapCands, beforeC, func(x, y candidate) bool { return x.label == y.label }) {
			t.Errorf("%s changed the slices a match may be going on over", line)
		}
		for i, n := range ix.names {
			if ix.cands[i].label != n || ix.cands[i].insert != redis.Quote(n) {
				t.Errorf("%s: name %q has candidate %q", line, n, ix.cands[i].label)
			}
		}
	}
	gen := ix.gen
	step("SET new 1", []string{"new"}, true)
	if names() != "a b k new" || ix.total != 4 || ix.gen != gen+1 {
		t.Errorf("SET: %s, total %d, gen %d", names(), ix.total, ix.gen)
	}
	step("DEL a", []string{"a"}, false)
	step("RENAME b z", []string{"b", "z"}, false, true)
	if names() != "k new z" {
		t.Errorf("DEL, RENAME: %s", names())
	}
	step("SET k v NX", []string{"k"}, true) // was there: not twice
	step("EXPIRE new 0", []string{"new"}, false)
	if names() != "k z" || ix.total != 2 {
		t.Errorf("NX, EXPIRE 0: %s, total %d", names(), ix.total)
	}
	step("MSET m1 1 m2 2", []string{"m1", "m2"}, true, true)
	if names() != "k m1 m2 z" {
		t.Errorf("MSET: %s", names())
	}
	if cmd := a.wrote(conn, "GET k"); cmd != nil { // a read changes none
		t.Error("a read asks")
	}
	if cmd := a.wrote(conn, "SELECT 2"); cmd != nil { // no key in it
		t.Error("a command without a key asks")
	}

	// the index of a db the tree read whole is made first, so that the new key is in
	conn.index, conn.scan = nil, &keyScan{keys: []redis.KeyInfo{{Name: "t1"}}, total: 1}
	if cmd := a.wrote(conn, "SET t2 1"); cmd == nil || conn.index == nil || len(conn.index.names) != 1 {
		t.Errorf("a write over the tree's keys: %v", conn.index)
	}
	conn.index, conn.scan = nil, nil
	if cmd := a.wrote(conn, "SET t2 1"); cmd != nil { // no index, none to keep: it will be made from the server
		t.Error("a write with no index asks")
	}

	// an answer that is no use: a FLUSH came since it was asked; or it failed
	ix = &keyIndex{done: true, names: []string{"x"}}
	ix.makeCands()
	conn.index = ix
	if a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch - 1, keys: []string{"s"}, found: []bool{true}}); len(ix.names) != 1 {
		t.Error("an answer from before a FLUSH is taken")
	}
	if cmd := a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch, keys: []string{"s"}, err: errBoom}); cmd == nil || conn.building == nil || conn.index != ix {
		t.Errorf("a failed answer: building %v", conn.building)
	}

	// what is not known: the index is made again, and used meanwhile
	conn.building = nil
	if cmd := a.wrote(conn, "EVAL script 1 k"); cmd == nil || conn.building == nil || conn.indexed() != ix || len(ix.names) != 1 {
		t.Errorf("EVAL: building %v", conn.building)
	}
	conn.building = nil
	if cmd := a.wrote(conn, "EXEC"); cmd == nil || conn.building == nil {
		t.Error("EXEC does not make the index again")
	}
	conn.building = nil
	if cmd := a.wrote(conn, "NOSUCH k"); cmd == nil || conn.building == nil { // not in the docs, and taken for a write
		t.Error("a command the server does not list does not make the index again")
	}

	// SWAPDB has no key in it, and the keys of the db are other ones
	conn.building = nil
	if cmd := a.wrote(conn, "SWAPDB 0 1"); cmd == nil || conn.building == nil || conn.indexed() != ix {
		t.Errorf("SWAPDB: building %v", conn.building)
	}

	// FLUSHDB and FLUSHALL
	conn.Spec.Engine = "redis"
	other := conn.inDB("1")
	other.State, other.Spec.Engine = connReady, "redis"
	other.index = &keyIndex{done: true, names: []string{"o"}}
	other.index.makeCands()
	if cmd := a.wrote(conn, "FLUSHDB"); cmd != nil || len(conn.index.names) != 0 || !conn.index.done || len(other.index.names) != 1 {
		t.Errorf("FLUSHDB: %v, other %v", conn.index.names, other.index.names)
	}
	asked := conn.epoch // a write asked of before the FLUSH is answered after it: the key is not there
	conn.index.names = []string{"again"}
	conn.building = &keyIndex{}
	if cmd := a.wrote(conn, "FLUSHALL"); cmd != nil || len(conn.index.names) != 0 || len(other.index.names) != 0 || conn.building != nil {
		t.Errorf("FLUSHALL: %v, other %v", conn.index.names, other.index.names)
	}
	if a.gotDelta(indexDelta{conn: conn, epoch: asked, keys: []string{"ghost"}, found: []bool{true}}); len(conn.index.names) != 0 {
		t.Errorf("an answer from before a FLUSH: %v", conn.index.names)
	}
}

// BenchmarkKeyIndexComplete is the keystrokes of "user_123" over an index of
// index_keys (100000) names, each a completion that goes on from what the one
// before matched: the slowest is the first, which looks at all of them; past
// 30ms the matching wants more than that (§9.8).
func BenchmarkKeyIndexComplete(b *testing.B) {
	was := matchInline
	matchInline = 1 << 30 // in place: it is the match that is timed, which the program does in the background
	defer func() { matchInline = was }()
	var names []string
	for i := range 100000 {
		names = append(names, fmt.Sprintf("app%d:user_%d:session:%d", i%40, i%997, i))
	}
	conn := newConn(config.Connection{Name: "r", Engine: "redis"})
	conn.docs = someDocs()
	conn.index = &keyIndex{names: names, done: true}
	conn.index.makeCands()
	a := &App{}
	t := &queryTab{conn: conn}
	var first, slowest time.Duration
	for b.Loop() {
		conn.index.last.pattern = ""
		for n := 1; n <= len("user_123"); n++ {
			typed := "get " + "user_123"[:n]
			start := time.Now()
			a.redisComplete(t, typed, len(typed), false)
			d := time.Since(start)
			if n == 1 {
				first = d
			}
			slowest = max(slowest, d)
		}
	}
	b.ReportMetric(float64(first.Milliseconds()), "first-ms")
	b.ReportMetric(float64(slowest.Milliseconds()), "slowest-ms")
}

// A key typed after another matches among what the one before matched, and finds
// what matching them all finds, in the same order; a pattern that is not an
// extension of it starts over (§9.8).
func TestKeyIndexNarrows(t *testing.T) {
	var names []string
	for i := range 300 {
		names = append(names, fmt.Sprintf("app%d:user_%d:s%d", i%7, i%13, i))
	}
	a, c, conn := keyConsole(t, names...)
	typed := func(text string) []string { return typedAt(t, a, c, text, false) }
	for _, text := range []string{"get u", "get us", "get user_1", "get user_12", "get user_2", "get a", "get app3:u", "get User_1"} {
		got := typed(text)
		conn.index.last.pattern = ""
		if want := typed(text); !slices.Equal(got, want) {
			t.Errorf("%q: %d names narrowed, %d matching all", text, len(got), len(want))
		}
	}
	typed("get user_1")
	if all := len(conn.index.cands); len(conn.index.narrowed("user_12")) >= all || len(conn.index.narrowed("USER_12")) >= all {
		t.Error("an extension of the last pattern looks at all of them")
	}
	if len(conn.index.narrowed("ser_1")) != len(conn.index.cands) || len(conn.index.narrowed("u")) != len(conn.index.cands) {
		t.Error("what does not extend it does not start from its matches")
	}
}

// Names that tie in the match (the same score and length) come in the order of the
// names, typed on from the last key or all at once (F20.1).
func TestKeyIndexNarrowKeepsTies(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	seen := map[string]bool{}
	var names []string
	for len(names) < 240 {
		n := make([]byte, 4+r.IntN(3))
		for i := range n {
			n[i] = "ab_"[r.IntN(3)]
		}
		if !seen[string(n)] {
			seen[string(n)] = true
			names = append(names, string(n))
		}
	}
	a, c, conn := keyConsole(t, names...)
	for _, seq := range [][]string{{"b", "ba"}, {"a", "ab", "ab_"}, {"_", "_a", "_ab"}, {"ba", "baa"}} {
		var got []string
		for _, text := range seq {
			got = typedAt(t, a, c, "get "+text, false)
		}
		conn.index.last.pattern = ""
		if want := typedAt(t, a, c, "get "+seq[len(seq)-1], false); !slices.Equal(got, want) {
			t.Errorf("%v: typed on %v, at once %v", seq, got, want)
		}
	}
}

// An index that is done while a key is being typed changes the open list to the
// matches in it, not from the next key (§9.8, F20.1).
func TestKeyIndexDoneRefreshesTheList(t *testing.T) {
	a, c, conn := keyConsole(t)
	conn.index, conn.building = nil, &keyIndex{started: true, seen: map[string]bool{}} // being built
	conn.hints = map[string][]string{"keys\x00exp": {"exp:prefix"}}
	c.ed.Load("get exp")
	feed(t, a, "A")
	a.queryComplete(c, false)
	if c.comp == nil || c.comp.items[0].label != "exp:prefix" {
		t.Fatalf("by the prefix till the index is done: %+v", c.comp)
	}
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"i18n:expert:1", "other"}, total: 2})
	var got []string
	for _, it := range c.comp.items {
		got = append(got, it.label)
	}
	if !slices.Equal(got, []string{"i18n:expert:1"}) {
		t.Errorf("the open list after the index: %v", got)
	}
}

// remember keeps the keys of a list and not the tokens that stood among them; an
// index of a db that is not connected is not started (§9.8, F20.1).
func TestKeyIndexRememberAndStart(t *testing.T) {
	_, c, conn := keyConsole(t, "NX", "a:1", "a:2") // a key named as a token
	ix := conn.index
	ix.remember("a", []candidate{{label: "NX", kind: kindKeyword}, {label: "a:2", kind: kindKey}}, ix.cands)
	if len(ix.last.cands) != 1 || ix.last.cands[0].label != "a:2" {
		t.Errorf("remembered %+v", ix.last.cands)
	}
	a := &App{indexKeys: 10}
	conn.index = nil
	conn.State = connConnecting
	if cmd := a.startIndex(conn); cmd != nil || conn.index != nil {
		t.Error("an index of a connection that is connecting is started")
	}
	_ = c
}

// The list says under it how many of the db the index has, in a row of the box of
// its own: the items are not one fewer for it (F20.1).
func TestKeyIndexFootHasItsRow(t *testing.T) {
	var names []string
	for i := range 20 {
		names = append(names, fmt.Sprintf("key:%02d", i))
	}
	a, c, conn := keyConsole(t, names...)
	conn.index.capped, conn.index.total = true, 99
	feed(t, a, "i")
	c.ed.Load("get ke")
	feed(t, a, "A")
	a.queryComplete(c, false)
	screen := a.render().String()
	rows := strings.Count(screen, "key:")
	if !strings.Contains(screen, "已索引 20 / 共 99 键") || rows != ui.CompleteRows {
		t.Errorf("%d key rows, foot shown %v:\n%s", rows, strings.Contains(screen, "已索引"), screen)
	}
}

// async makes every match of key names go in the background, for the test.
func async(t *testing.T) {
	t.Helper()
	was := matchInline
	matchInline = 0
	t.Cleanup(func() { matchInline = was })
}

// matched runs the commands of a completion and gives the matches among their
// answers, in the order they were asked.
func matched(cmd tea.Cmd) (out []matchMsg) {
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, c := range m {
				walk(c)
			}
		case matchMsg:
			out = append(out, m)
		}
	}
	walk(cmd)
	return out
}

// With many names the match goes in the background over the names as they were, and
// comes back with the number of its ask: the list stays the last one while it goes
// on, ↵ in the meantime puts nothing and takes the new list's selected one when it
// comes, and an answer that is not the last ask's, or
// whose place is gone (the text typed on, INSERT left), is dropped (§9.8, F20.8).
func TestKeyMatchInBackground(t *testing.T) {
	async(t)
	a, c, conn := keyConsole(t, "i18n:expert:1", "i18n:expert:2", "user:1", "user:2")
	c.ed.Load("get ")
	feed(t, a, "A")
	typed := func(keys ...string) (ms []matchMsg) { // as the program has them: a key, and what it asks for
		t.Helper()
		for _, k := range keys {
			_, cmd := a.Update(teaKey(keymap.Key(k)))
			ms = append(ms, matched(cmd)...)
		}
		return ms
	}
	ms := typed("e", "x", "p")
	if len(ms) != 3 || !c.matching || c.comp != nil {
		t.Fatalf("asked: %d matches, matching %v, list %v", len(ms), c.matching, c.comp)
	}
	a.Update(ms[0]) // an older ask's
	if !c.matching || c.comp != nil {
		t.Fatal("an answer that is not the last ask's is taken")
	}
	a.Update(ms[2])
	if c.matching || c.comp == nil || len(c.comp.items) != 2 || c.comp.items[0].label != "i18n:expert:1" || conn.index.last.pattern != "exp" {
		t.Fatalf("after the answer: matching %v, %+v, last %q", c.matching, c.comp, conn.index.last.pattern)
	}
	first := c.comp

	// the list stays the last one while the next goes on, and ↵ puts nothing in
	ms = typed("e")
	if !c.matching || c.comp != first {
		t.Fatalf("while it goes on: matching %v, the list kept %v", c.matching, c.comp == first)
	}
	before := strings.Join(c.ed.Lines(), "\n")
	if a.queryAccept(c); strings.Join(c.ed.Lines(), "\n") != before || c.comp != first {
		t.Error("a candidate of the list before is accepted while the next is made")
	}
	a.Update(ms[0]) // the ↵ that waited takes the first of the list that came, not of the one before
	if c.matching || c.comp != nil || strings.Join(c.ed.Lines(), "\n") != "get i18n:expert:1 " {
		t.Errorf("the answer: %+v, %q", c.comp, c.ed.Lines())
	}

	// the text changed before it came: it is of no use
	c.ed.Load("get expe")
	feed(t, a, "A")
	ms = typed("r")
	c.ed.Load("get userx")
	c.comp = first
	a.Update(ms[0])
	if c.comp != first {
		t.Errorf("an answer for text that is gone is taken: %+v", c.comp)
	}
	c.ed.Load("get exp") // the same cursor and mode, an edit between (the version of the text)
	feed(t, a, "A")
	ms = typed("e")
	c.ver++
	c.comp = first
	a.Update(ms[0])
	if c.comp != first {
		t.Error("an answer for an older version of the text is taken")
	}
	// INSERT was left
	c.ed.Load("get exp")
	feed(t, a, "A")
	ms = typed("e")
	feed(t, a, "<Esc>")
	c.matchFor.cur = c.ed.Cursor() // where it was is where the cursor is: it is the mode alone that is not as it was
	c.matching, c.comp = true, nil
	a.Update(ms[0])
	if c.comp != nil {
		t.Error("an answer after INSERT was left opens a list")
	}
}

// An update of the index is new slices: what a match going on in the background
// has is the index as it was, and the next ask sees the update (F20.8).
func TestKeyMatchSnapshot(t *testing.T) {
	async(t)
	a, c, conn := keyConsole(t, "k:a", "k:b")
	c.ed.Load("get k:")
	feed(t, a, "A")
	ms := matched(a.queryComplete(c, false))
	conn.index.update([]string{"k:c"}, nil) // after it was asked, before it ran
	a.Update(ms[0])
	var got []string
	for _, it := range c.comp.items {
		got = append(got, it.label)
	}
	if !slices.Equal(got, []string{"k:a", "k:b"}) {
		t.Errorf("a match over the names as they were: %v", got)
	}
	ms = matched(a.queryComplete(c, false))
	a.Update(ms[0])
	if len(c.comp.items) != 3 {
		t.Errorf("the next ask: %d candidates", len(c.comp.items))
	}
	if conn.index.last.pattern != "k:" || len(conn.index.last.cands) != 3 { // and what the stale answer matched was not kept to look among
		t.Errorf("what is kept to look among: %q, %d", conn.index.last.pattern, len(conn.index.last.cands))
	}
}

// The palette's keys with many names are made and matched in the background, over
// a copy of the last list when the dbs are as they were, and the list is the
// last one till the answer comes; an answer to an older ask is dropped (F20.8).
func TestPaletteKeysInBackground(t *testing.T) {
	async(t)
	a, conn := keyPalette(t, "@employees")
	rows := func() (names []string) {
		for _, r := range keyRows(a) {
			names = append(names, r.name)
		}
		slices.Sort(names)
		return names
	}
	if len(rows()) != 0 {
		t.Fatalf("matched before the background: %v", rows())
	}
	a.palette.keyAsked = "" // what the keys typed asked for went nowhere, as feed drops the commands
	ask := func() keyListMsg {
		t.Helper()
		cmd := a.keyListCmd()
		if cmd == nil {
			t.Fatal("nothing asked")
		}
		return cmd().(keyListMsg)
	}
	first := ask()
	if again := a.keyListCmd(); again != nil {
		t.Error("the same query is asked again while it is on its way")
	}
	a.Update(first)
	if got := rows(); !slices.Equal(got, []string{"p:employees:1", "p:employees:2"}) {
		t.Fatalf("after the answer: %v", got)
	}
	if a.keyListCmd() != nil {
		t.Error("asked for a query that is matched")
	}

	// the next query goes on from the last one's matches, and the list stays as it was meanwhile
	a.palette.input.Text = "@employees:2"
	list := a.palette.keys
	second := ask()
	if a.palette.keys != list || len(rows()) != 2 {
		t.Error("the list changed before the answer")
	}
	a.palette.input.Text = "@employees:1"
	third := ask() // a later ask: the second's answer is no use
	a.Update(second)
	if a.palette.keys != list {
		t.Error("an answer to an older ask is taken")
	}
	a.Update(third)
	if got := rows(); !slices.Equal(got, []string{"p:employees:1"}) {
		t.Errorf("the last answer: %v", got)
	}
	if len(third.list.cand) != 1 || len(third.list.items) != len(list.items) {
		t.Errorf("not over the same names: %d matched of %d, %d", len(third.list.cand), len(third.list.items), len(list.items))
	}

	// the dbs read more: made again, not over the old list; the old goes on till it is
	conn.index.update([]string{"p:employees:7"}, nil)
	a.palette.input.Text = "@employees"
	again := ask()
	if len(again.list.items) != len(list.items)+1 {
		t.Errorf("the list after the update has %d keys, was %d", len(again.list.items), len(list.items))
	}
	if &third.list.items[0] != &list.items[0] { // a copy of the last list, over the same keys, as the dbs were as they were
		t.Error("the list was made anew, not from a copy of the last one")
	}
	// the palette is closed meanwhile
	closed, before := a.palette, a.palette.keys
	feed(t, a, "<Esc><Esc>")
	a.Update(again)
	if a.palette != nil || closed.keys != before {
		t.Error("an answer is taken after the palette is closed")
	}
}

// BenchmarkKeyIndexKeystroke is what Update spends on a key typed in a console, over
// an index of index_keys (100000) names: with the match in the background it is the
// asking only (F20.8).
func BenchmarkKeyIndexKeystroke(b *testing.B) {
	var names []string
	for i := range 100000 {
		names = append(names, fmt.Sprintf("app%d:user_%d:session:%d", i%40, i%997, i))
	}
	for _, inline := range []int{matchInline, 1 << 30} {
		b.Run(fmt.Sprintf("inline=%d", inline), func(b *testing.B) {
			was := matchInline
			matchInline = inline
			defer func() { matchInline = was }()
			t := &testing.T{}
			a, c, _ := keyConsole(t, names...)
			c.ed.Load("get ")
			feed(t, a, "A")
			b.ResetTimer()
			for b.Loop() {
				c.ed.Load("get user_")
				feed(t, a, "A")
				a.Update(teaKey("1"))
				a.Update(teaKey("<BS>"))
			}
		})
	}
}

// What a write adds to the index is found by the next completion, though the one
// before had matched the names as they were: what was kept to look among goes with
// the update (F20.8).
func TestKeyIndexUpdateForgetsWhatWasMatched(t *testing.T) {
	a, c, conn := keyConsole(t, "k:a", "k:b")
	if got := typedAt(t, a, c, "get k:", false); len(got) != 2 || conn.index.last.pattern != "k:" {
		t.Fatalf("before: %v", got)
	}
	conn.index.update([]string{"k:c"}, nil)
	if got := typedAt(t, a, c, "get k:", false); len(got) != 3 {
		t.Errorf("after the update: %v", got)
	}
}

// The quick SQL's key completion goes in the background too: its list is the last
// one while it goes on, and the answer is its list, moved past the ;, when what was
// typed is as it was, else nothing (F20.8).
func TestQuickKeyMatchInBackground(t *testing.T) {
	async(t)
	a, _ := keyPalette(t, ";get emp")
	p := a.palette
	ms := matched(a.completeSQL())
	if len(ms) != 1 || !p.hint.matching || p.comp != nil {
		t.Fatalf("asked: %d, matching %v, list %v", len(ms), p.hint.matching, p.comp)
	}
	a.Update(ms[0])
	if p.comp == nil || p.comp.items[0].label != "p:employees:1" || p.comp.start != 5 { // ";get " is 5 columns
		t.Fatalf("answer: %+v", p.comp)
	}
	first := p.comp
	ms = matched(a.completeSQL())
	if p.comp != first {
		t.Error("the list is not the last one while the next is made")
	}
	p.input.Text += "x" // typed on, no completion asked for it yet
	a.Update(ms[0])
	if p.comp != first {
		t.Error("an answer for an input that is gone is taken")
	}
}

// A query goes on from the last one's matches only when that is sure to hold: not
// once the last has a ! or a $ term (F20.3).
func TestNarrowsTo(t *testing.T) {
	for _, tc := range []struct {
		last, query string
		want        bool
	}{
		{"pl", "pla", true}, {"pl", "pl x", true}, {"^pl", "^pla", true}, {"'pl", "'pla", true}, {"pl", "pl$", true}, {"pl", "pl !x", true},
		{"", "pl", false}, {"pl", "p", false}, {"pl", "xpl", false},
		{"!pl", "!pla", false}, {"a !pl", "a !pla", false}, {"a$", "a$b", false}, {"a$", "a$ ", false},
	} {
		if got := narrowsTo(tc.last, tc.query); got != tc.want {
			t.Errorf("narrowsTo(%q, %q) = %v, want %v", tc.last, tc.query, got, tc.want)
		}
	}
}

// The writes that come while an index is made go on over it when it is done, in their
// order: the first one's, which has no index till then, as well as the one that takes
// the place of another, which has them at once; a SCAN that was past the key does not
// tell (§9.8, F20.8).
func TestKeyIndexWritesWhileBuilding(t *testing.T) {
	cmds := someCommands()
	for _, name := range []string{"set", "del"} {
		cmds[name] = redis.Command{Flags: []string{"write"}}
	}
	a, _, conn := keyConsole(t)
	conn.docs, conn.cmds = writeDocs(), cmds
	conn.index = nil
	conn.building = &keyIndex{started: true, seen: map[string]bool{}}
	ix := conn.building
	write := func(line string, keys []string, found ...bool) {
		t.Helper()
		if a.wrote(conn, line) == nil {
			t.Fatalf("%s asks nothing while the index is made", line)
		}
		a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch, keys: keys, found: found})
	}
	write("SET x 1", []string{"x"}, true)
	write("SET y 1", []string{"y"}, true)
	write("DEL x", []string{"x"}, false)
	if conn.index != nil || len(ix.deltas) != 3 {
		t.Fatalf("no index yet: %v, %d deltas", conn.index, len(ix.deltas))
	}
	a.gotIndex(indexMsg{conn: conn, ix: ix, names: []string{"a", "b"}, total: 2})
	if got := strings.Join(conn.index.names, " "); got != "a b y" || len(conn.index.cands) != 3 || conn.index.deltas != nil {
		t.Errorf("done: %q", got)
	}

	// one made again: the index there has the write now, the new one when it is done
	old := conn.index
	a.rebuildIndex(conn)
	write("SET n 1", []string{"n"}, true)
	write("DEL a", []string{"a"}, false)
	if got := strings.Join(old.names, " "); got != "b n y" {
		t.Errorf("the index used meanwhile: %q", got)
	}
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"a", "b", "y"}, total: 3})
	if got := strings.Join(conn.index.names, " "); conn.index == old || got != "b n y" || conn.index.total != 3 {
		t.Errorf("the new one: %q, total %d", got, conn.index.total)
	}
}

// A write whose answer comes sets the open list again: the new key is in it at once
// (§9.8, F20.8).
func TestKeyIndexWriteRefreshesTheList(t *testing.T) {
	a, c, conn := keyConsole(t, "k:a")
	conn.docs = writeDocs()
	c.ed.Load("get k:")
	feed(t, a, "A")
	a.queryComplete(c, false)
	if c.comp == nil || len(c.comp.items) != 1 {
		t.Fatalf("before: %+v", c.comp)
	}
	a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch, keys: []string{"k:new"}, found: []bool{true}})
	if c.comp == nil || len(c.comp.items) != 2 {
		t.Errorf("the open list after the write: %+v", c.comp)
	}
}

// Of a capped index, a key that is there and was not read is not a new one: the count
// of the db goes by what a delete takes away of the names it has (§9.8, F20.8).
func TestKeyIndexTotalOfACappedOne(t *testing.T) {
	ix := &keyIndex{names: []string{"a", "b"}, total: 100, done: true, capped: true}
	ix.makeCands()
	ix.update([]string{"c"}, []string{"a", "zz"})
	if got := strings.Join(ix.names, " "); got != "b c" || ix.total != 99 {
		t.Errorf("capped: %q, total %d", got, ix.total)
	}
	ix = &keyIndex{names: []string{"a", "b"}, total: 2, done: true}
	ix.makeCands()
	ix.update([]string{"c"}, []string{"a"})
	if ix.total != 2 {
		t.Errorf("whole: total %d", ix.total)
	}
}

// ↵ while the match of the word is on takes nothing of the list before, which is of
// the word before, and is not lost: the list that comes has its selected one taken, if
// the text is as it was; typed on, or changed otherwise, it is dropped (§9.8, F20.8).
func TestKeyEnterWaitsForTheList(t *testing.T) {
	async(t)
	setup := func() (*App, *queryTab, func(...string) []matchMsg) {
		a, c, _ := keyConsole(t, "i18n:expert:1", "i18n:expert:2", "user:1")
		c.ed.Load("get ")
		feed(t, a, "A")
		typed := func(keys ...string) (ms []matchMsg) {
			t.Helper()
			for _, k := range keys {
				_, cmd := a.Update(teaKey(keymap.Key(k)))
				ms = append(ms, matched(cmd)...)
			}
			return ms
		}
		ms := typed("e", "x")
		a.Update(ms[len(ms)-1]) // the list of "ex"
		if c.comp == nil {
			t.Fatal("no list")
		}
		return a, c, typed
	}
	text := func(c *queryTab) string { return strings.Join(c.ed.Lines(), "\n") }

	a, c, typed := setup()
	ms := typed("p", "<CR>")
	if text(c) != "get exp" || !c.matching {
		t.Fatalf("↵ while it goes on: %q", text(c))
	}
	a.Update(ms[0])
	if text(c) != "get i18n:expert:1 " || c.comp != nil {
		t.Errorf("the list that came: %q, %+v", text(c), c.comp)
	}

	a, c, typed = setup() // a click on a row of the list before is the same
	ms = typed("p")
	c.comp.sel = 1
	a.acceptCompletion()
	a.Update(ms[0])
	if text(c) != "get i18n:expert:1 " {
		t.Errorf("a click: %q", text(c))
	}

	a, c, typed = setup() // typed on
	typed("p", "<CR>")
	ms = typed("e")
	a.Update(ms[0])
	if text(c) != "get expe" || c.comp == nil {
		t.Errorf("typed on: %q, %+v", text(c), c.comp)
	}

	a, c, typed = setup() // a ↵ that nothing came of is not kept for the next list
	ms = typed("q", "<CR>")
	a.Update(ms[0])
	conn := a.sess.Conns[0]
	ms = matched(a.gotDelta(indexDelta{conn: conn, epoch: conn.epoch, keys: []string{"exq:1"}, found: []bool{true}})) // the list is made again, with a name that matches
	a.Update(ms[0])
	if text(c) != "get exq" || c.comp == nil {
		t.Errorf("a ↵ of before: %q, %+v", text(c), c.comp)
	}

	a, c, typed = setup() // an edit of the text by other means
	ms = typed("p", "<CR>")
	c.ver++
	a.Update(ms[0])
	if text(c) != "get exp" {
		t.Errorf("text changed: %q", text(c))
	}
}

// The quick SQL's ↵ is the same: while the match is on it takes nothing of the list
// before, which would put another key in the word, and takes the list that comes
// (§9.8, F20.8).
func TestQuickEnterWaitsForTheList(t *testing.T) {
	async(t)
	run := func() (*App, string, []matchMsg) {
		a, _, _ := keyConsole(t, "hash:binary", "hash:user", "user:1")
		feed(t, a, "<Esc>")
		feed(t, a, "<C-p>;get ")
		var ms []matchMsg
		typed := func(keys ...string) {
			for _, k := range keys {
				_, cmd := a.Update(teaKey(keymap.Key(k)))
				ms = append(ms, matched(cmd)...)
			}
		}
		typed("h")
		a.Update(ms[len(ms)-1])
		if a.palette.comp == nil {
			t.Fatal("no list for h")
		}
		ms = nil
		typed("a", "s", "h", ":", "b", "<CR>")
		return a, a.palette.input.Text, ms
	}
	a, input, ms := run()
	if input != ";get hash:b" || !a.palette.hint.matching {
		t.Fatalf("↵ while it goes on: %q", input)
	}
	a.Update(ms[len(ms)-1])
	if a.palette == nil || a.palette.input.Text != ";get hash:binary " {
		t.Errorf("the list that came: %q", a.palette.input.Text)
	}

	a, _, _ = run() // typed on after the ↵
	_, cmd := a.Update(teaKey("i"))
	ms = matched(cmd)
	a.Update(ms[len(ms)-1])
	if a.palette.input.Text != ";get hash:bi" {
		t.Errorf("typed on: %q", a.palette.input.Text)
	}
}
