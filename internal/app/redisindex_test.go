package app

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/redis"
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
// it asks the size; one that came after the index was dropped is nothing, and a
// batch that failed drops it for the next completion to start again (§9.8).
func TestKeyIndexBuilds(t *testing.T) {
	a, c, conn := keyConsole(t)
	conn.index = nil
	if _, _, cmds := a.keyCandidates(c, "zz"); conn.index == nil || len(cmds) != 2 || !conn.index.started { // the index's first batch, and the prefix scan till it is
		t.Fatalf("not started: %+v, %d commands", conn.index, len(cmds))
	}
	ix := conn.index
	if _, _, cmds := a.keyCandidates(c, "zz"); len(cmds) != 1 { // only the prefix's, the index being on its way
		t.Errorf("started again: %d commands", len(cmds))
	}
	if cmd := a.gotIndex(indexMsg{conn: conn, ix: ix, names: []string{"a", "b", "a"}, next: 7, total: 5}); cmd == nil || len(ix.names) != 2 || ix.done || ix.total != 5 || ix.cursor != 7 {
		t.Fatalf("a batch: %+v", ix)
	}
	a.gotIndex(indexMsg{conn: conn, ix: ix, names: []string{"b", "c"}})
	if !ix.done || ix.capped || !slices.Equal(ix.names, []string{"a", "b", "c"}) || len(ix.cands) != 3 || conn.indexed() != ix {
		t.Fatalf("done: %+v", ix)
	}
	if _, foot, cmds := a.keyCandidates(c, "zz"); foot != "" || len(cmds) != 0 {
		t.Errorf("an index that has it all: foot %q, %d commands", foot, len(cmds))
	}

	conn.index = nil
	a.keyCandidates(c, "q")
	old := conn.index
	conn.forgetNames()
	a.keyCandidates(c, "q")
	if a.gotIndex(indexMsg{conn: conn, ix: old, names: []string{"x"}, next: 0}); len(old.names) != 0 || conn.index == old {
		t.Error("a batch of an index that was dropped is taken")
	}
	if a.gotIndex(indexMsg{conn: conn, ix: conn.index, err: errBoom}); conn.index != nil {
		t.Error("a failed batch leaves the index")
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
	a.gotIndex(indexMsg{conn: conn, ix: conn.index, names: []string{"a", "b", "c", "d", "e"}, next: 9, total: 12})
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

// The tree's keys are the index when it read the whole db; when the budget
// stopped it, the index is built. A write the console ran and an R on the db
// drop it (§9.8).
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
	if a.startIndex(conn) != nil || conn.index != nil { // and a connection that is a command builds none
		t.Error("an index of an exec connection is built")
	}
	conn.Spec.Engine = "redis"

	conn.index = &keyIndex{done: true}
	conn.forgetHints("GET u:1")
	if conn.index == nil {
		t.Error("a read drops the index")
	}
	conn.forgetHints("SET u:3 1")
	if conn.index != nil {
		t.Error("a write keeps the index")
	}
	conn.index = &keyIndex{done: true}
	a.gotKeys(keysMsg{conn: conn, scan: &keyScan{}})
	if conn.index != nil {
		t.Error("R on the db keeps the index")
	}

	// after a write the tree's keys are not what the db has: the index is built
	// again from the server, till R reads the tree again
	conn.scan = &keyScan{keys: []redis.KeyInfo{{Name: "u:1"}}, total: 1}
	conn.forgetHints("SET u:9 1")
	if conn.indexed() != nil {
		t.Error("the tree of before the write is the index")
	}
	if _, _, cmds := a.keyCandidates(c, "u"); conn.index == nil || len(cmds) != 2 {
		t.Errorf("no index is built after a write: %+v, %d commands", conn.index, len(cmds))
	}
	a.gotKeys(keysMsg{conn: conn, scan: &keyScan{keys: []redis.KeyInfo{{Name: "u:1"}, {Name: "u:9"}}, total: 2}})
	if ix := conn.indexed(); ix == nil || len(ix.names) != 2 {
		t.Errorf("the tree of after R: %+v", ix)
	}
}

// BenchmarkKeyIndexComplete is the keystrokes of "user_123" over an index of
// index_keys (100000) names, each a completion that goes on from what the one
// before matched: the slowest is the first, which looks at all of them; past
// 30ms the matching wants more than that (§9.8).
func BenchmarkKeyIndexComplete(b *testing.B) {
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
	conn.index = &keyIndex{started: true, seen: map[string]bool{}} // being built
	conn.hints = map[string][]string{"keys\x00exp": {"exp:prefix"}}
	c.ed.Load("get exp")
	feed(t, a, "A")
	a.queryComplete(c, false)
	if c.comp == nil || c.comp.items[0].label != "exp:prefix" {
		t.Fatalf("by the prefix till the index is done: %+v", c.comp)
	}
	a.gotIndex(indexMsg{conn: conn, ix: conn.index, names: []string{"i18n:expert:1", "other"}, total: 2})
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
	ix.remember("a", []candidate{{label: "NX", kind: kindKeyword}, {label: "a:2", kind: kindKey}})
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
