package app

import (
	"context"
	"errors"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// redisTree is an App whose first connection is a Redis one with dbs 0 and 1, db
// 0 scanned: the keys of kinds, a namespace that is a key too, and the odd ones.
func redisTree(t *testing.T, scan *keyScan) (*App, *Conn) {
	t.Helper()
	a := wide(160, 45)
	c := a.sess.Conns[0]
	c.Spec = config.Connection{Name: "cache", Engine: "redis", DSN: "redis://localhost/0"}
	c.drv, c.eng = kvDriver{}, nil
	c.Tables, c.Schemas, c.home, c.Schema = nil, nil, "", ""
	c.DB, c.Databases, c.scan = "0", []string{"0", "1"}, scan
	return a, c
}

func someKeys() *keyScan {
	var ks []redis.KeyInfo
	for _, k := range [][2]string{
		{"str:plain", "string"}, {"hash:user", "hash"}, {"app:users:1", "string"}, {"app:users:2", "string"}, {"app:orders:100", "string"},
		{"app", "string"}, {"a::b", "list"}, {":lead", "set"}, {"trail:", "zset"}, {"nodelimiter", "stream"}, {"json:doc", "ReJSON-RL"},
	} {
		ks = append(ks, redis.KeyInfo{Name: k[0], Type: k[1]})
	}
	return &keyScan{keys: ks, total: int64(len(ks))}
}

// outline is the tree's nodes down to the workspace, a line each: its depth in
// blanks, its text, its note.
func outline(a *App) (out []string) {
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind == nodeWorkspace {
			break
		}
		line := strings.Repeat("  ", n.Depth) + n.Text
		if n.Note != "" {
			line += " " + n.Note
		}
		out = append(out, line)
	}
	return out
}

// openAll opens every node that has others under it.
func openAll(a *App) {
	for range 20 {
		ns, _ := a.treeNodes()
		changed := false
		for _, n := range ns {
			if n.Branch && !n.Open && (n.kind == nodeNamespace || n.kind == nodeDatabase) {
				a.sess.open[n.id], changed = true, true
			}
		}
		if !changed {
			return
		}
	}
}

// A Redis connection's tree is its dbs, db0 with its size, and under a db the
// namespaces of its keys by the delimiter with how many keys each holds, then
// the keys: a name with two delimiters in a row has a namespace with no name,
// one with a delimiter at its end a key with none, a key may be called as a
// namespace is, and the keys of a db that is not connected are not there (F16.2).
func TestRedisTree(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	empty := i18n.T("(空)")
	if got := outline(a); !slices.Equal(got[:2], []string{"cache", "  db0 11"}) || got[len(got)-1] != "  db1" {
		t.Fatalf("closed: %q", got)
	}
	openAll(a)
	want := []string{
		"cache", "  db0 11",
		"    " + empty + " 1", "      lead",
		"    a 1", "      " + empty + " 1", "        b",
		"    app 3", "      orders 1", "        100", "      users 2", "        1", "        2",
		"    hash 1", "      user",
		"    json 1", "      doc",
		"    str 1", "      plain",
		"    trail 1", "      " + empty,
		"    app", "    nodelimiter",
		"  db1",
	}
	if got := outline(a); !slices.Equal(got, want) {
		t.Errorf("tree:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// the icons are of the types, in the colors of §7.7
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind == nodeRedisKey {
			if want := a.icons.KeyType(n.redisKey.Type); n.Icon != want || n.IconFg != a.keyColor(n.redisKey.Type) {
				t.Errorf("%s: icon %+v, want %+v", n.redisKey.Name, n.Icon, want)
			}
		}
	}
	// ids: the db, the namespace by its path, the key by its name
	ids := map[string]bool{}
	for _, n := range ns {
		ids[n.id] = true
	}
	for _, id := range []string{"conn:cache/db:0", "conn:cache/db:0/ns:app:users", "conn:cache/db:0/key:app:users:1", "conn:cache/db:0/key:app", "conn:cache/db:0/ns:app"} {
		if !ids[id] {
			t.Errorf("no node %s", id)
		}
	}
}

// The delimiter is the connection's, else `[redis] delimiter`; the scan that
// stopped at its budget says so under the keys it read (F16.2).
func TestRedisTreeDelimiterAndBudget(t *testing.T) {
	a, c := redisTree(t, &keyScan{keys: []redis.KeyInfo{{Name: "a/b:c", Type: "string"}, {Name: "a/d", Type: "string"}}, total: 1_200_000, more: true})
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	openAll(a)
	note := i18n.T("已扫描 %d / %s，用 / 过滤", 2, "1.2m")
	if got := outline(a); !slices.Equal(got[:6], []string{"cache", "  db0 1.2m", "    a/b 1", "      c", "    a/d", "    " + note}) {
		t.Errorf("by the default's :\n%q", got)
	}
	a.redisDelim = "/"
	openAll(a)
	if got := outline(a); !slices.Equal(got[:6], []string{"cache", "  db0 1.2m", "    a 2", "      b:c", "      d", "    " + note}) {
		t.Errorf("by [redis] delimiter /:\n%q", got)
	}
	c.Spec.Delimiter = ":"
	openAll(a)
	if got := outline(a); !slices.Equal(got[2:4], []string{"    a/b 1", "      c"}) {
		t.Errorf("by the connection's:\n%q", got)
	}
}

// The filter asks the server, and the tree shows what it found with every
// namespace open and none of the rest; the pattern has a * at each end unless
// it has one (F16.2).
func TestRedisTreeFilter(t *testing.T) {
	for typed, want := range map[string]string{"": "", "users": "*users*", "app:*": "app:*", "*x": "*x", "a?c": "*a?c*"} {
		if got := filterPattern(typed); got != want {
			t.Errorf("filterPattern(%q) = %q, want %q", typed, got, want)
		}
	}
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	a.win().tree.filter = ui.Input{Text: "users", Pos: 5}
	if got := outline(a); len(got) != 0 { // nothing found yet: no connection kept
		t.Errorf("before: %q", got)
	}
	a.win().tree.found = map[*Conn]*keyScan{c: {keys: []redis.KeyInfo{{Name: "app:users:1", Type: "string"}, {Name: "app:users:2", Type: "string"}, {Name: "app:users:3", Type: "string"}}, total: 11}}
	if got, matches := outline(a), 0; !slices.Equal(got, []string{"cache", "  db0 11", "    app 3", "      users 3", "        1", "        2", "        3"}) {
		t.Errorf("filtered: %q %d", got, matches)
	}
	if _, matches := a.treeNodes(); matches != 3 {
		t.Errorf("matches %d", matches)
	}
	// what was found for another filter is not shown, nor asked of a db that is not connected
	a.win().tree.found = nil
	found := func() *keyScan { return a.win().tree.found[c] }
	a.gotFound(foundMsg{win: a.win(), conn: c, pattern: "*other*", scan: &keyScan{keys: []redis.KeyInfo{{Name: "x", Type: "string"}}}})
	if found() != nil {
		t.Error("a scan for a filter that is gone is shown")
	}
	a.gotFound(foundMsg{win: a.win(), conn: c, pattern: "*users*", scan: &keyScan{keys: []redis.KeyInfo{{Name: "x", Type: "string"}}}})
	if found() == nil {
		t.Error("a scan for the filter is not shown")
	}
	if cmd := a.findKeys(c.inDB("1")); cmd != nil {
		t.Error("a db that is not connected is asked")
	}
	// only the last change of the filter asks the servers
	a.filterSeq = 5
	if a.gotFilterDue(filterDue{3}) != nil {
		t.Error("an old change asks")
	}
	if a.gotFilterDue(filterDue{5}) == nil {
		t.Error("the last change does not ask")
	}
}

// The db the DSN names is listed with the dbs the server has keys in, in its place,
// though INFO says none of it; typing in the filter asks the servers, and the
// cursor goes to the first key it kept; a delete of keys goes by batches.
func TestRedisTreeDetails(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	c.DB = "5"
	a.gotKeys(keysMsg{conn: c, dbs: []string{"1", "3"}, scan: &keyScan{}})
	if !slices.Equal(c.Databases, []string{"1", "3", "5"}) {
		t.Errorf("databases %v", c.Databases)
	}
	c.DB = "0"
	a.sess.open[dbNode(c, "0")] = true
	t.Run("db order", func(t *testing.T) { // the DSN's db is not first, but open (F16.10)
		c.DB, c.Databases = "2", nil
		a.gotKeys(keysMsg{conn: c, dbs: []string{"0", "1", "3"}, scan: &keyScan{}})
		var got []string
		ns, _ := a.treeNodes()
		for _, n := range ns {
			if n.kind == nodeDatabase {
				got = append(got, n.Text+map[bool]string{true: "▾", false: ""}[n.Open])
			}
		}
		if want := []string{"db0", "db1", "db2▾", "db3"}; !slices.Equal(got, want) {
			t.Errorf("dbs %v, want %v", got, want)
		}
		c.DB = "0"
	})
	t.Run("filter", func(t *testing.T) {
		a.win().tree.filtering = true
		a.win().tree.found = map[*Conn]*keyScan{c: {keys: []redis.KeyInfo{{Name: "app:users:1", Type: "string"}}}}
		cmd := a.filterKey("u")
		if cmd == nil {
			t.Error("typing in the filter does not ask")
		}
		if n, ok := a.treeNode(); !ok || n.kind != nodeRedisKey || n.redisKey.Name != "app:users:1" {
			t.Errorf("the cursor is on %+v", n)
		}
	})
	var sizes []int
	n, err := unlinkAll(make([]string, 1200), func(b []string) (int64, error) { sizes = append(sizes, len(b)); return int64(len(b)), nil })
	if n != 1200 || err != nil || !slices.Equal(sizes, []int{500, 500, 200}) {
		t.Errorf("batches %v: %d %v", sizes, n, err)
	}
	sizes = nil
	errBoom := errors.New("boom")
	n, err = unlinkAll(make([]string, 1200), func(b []string) (int64, error) { sizes = append(sizes, len(b)); return 3, errBoom })
	if n != 3 || !errors.Is(err, errBoom) || len(sizes) != 1 {
		t.Errorf("an error stops it: %v %d %v", sizes, n, err)
	}
}

// What the server found for a filter is kept in the window whose filter it is: the
// filter of another window, and the keys it found, are not drawn in this one.
func TestFoundIsEachWindows(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	first := a.win()
	first.tree.filter = ui.Input{Text: "users", Pos: 5}
	a.addWindow()
	second := a.win()
	second.tree.filter = ui.Input{Text: "orders", Pos: 6}
	a.gotFound(foundMsg{win: first, conn: c, pattern: "*users*", scan: &keyScan{keys: []redis.KeyInfo{{Name: "app:users:1", Type: "string"}}}}) // answered while the other window is shown
	a.gotFound(foundMsg{win: second, conn: c, pattern: "*orders*", scan: &keyScan{keys: []redis.KeyInfo{{Name: "app:orders:100", Type: "string"}}}})
	a.selectWindow(0)
	if got := outline(a); !slices.Contains(got, "      users 1") || slices.Contains(got, "      orders 1") {
		t.Errorf("first window: %q", got)
	}
	a.selectWindow(1)
	if got := outline(a); !slices.Contains(got, "      orders 1") || slices.Contains(got, "      users 1") {
		t.Errorf("second window: %q", got)
	}
}

// The tree of a connected Redis connection with its db scanned (nerd icons).
func TestGoldenRedisTree(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	openAll(a)
	golden.RequireEqual(t, a.render().String())
}

// While the filter's scan is out, the connection and the db stay and the db's
// note says 搜索中… in dim, over what the scan before found; the answer brings
// the size back, and one with no keys takes the nodes away; a change of the
// filter, esc too, cancels the scan out, whose answer then is dropped, with no
// error; a scan that fails stops the searching (F18.11).
func TestRedisTreeSearching(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	a.win().tree.filter = ui.Input{Text: "users", Pos: 5}
	found := func() *keyScan { return a.win().tree.found[c] }
	noteFg := func() color.Color {
		t.Helper()
		ns, _ := a.treeNodes()
		for _, n := range ns {
			if n.kind == nodeDatabase {
				return n.NoteFg
			}
		}
		t.Fatal("no db node")
		return nil
	}
	searching := i18n.T("搜索中…")
	if cmd := a.findKeys(c); cmd == nil || !found().busy {
		t.Fatalf("asked: %v, found %+v", cmd != nil, found())
	}
	if got := outline(a); !slices.Equal(got, []string{"cache", "  db0 " + searching}) || noteFg() != a.theme.Dim {
		t.Errorf("searching: %q, note %v", got, noteFg())
	}
	keys := []redis.KeyInfo{{Name: "app:users:1", Type: "string"}}
	a.gotFound(foundMsg{win: a.win(), conn: c, pattern: "*users*", scan: &keyScan{keys: keys, total: 11}})
	if got := outline(a); !slices.Equal(got, []string{"cache", "  db0 11", "    app 1", "      users 1", "        1"}) || noteFg() != a.theme.Border {
		t.Errorf("found: %q, note %v", got, noteFg())
	}
	a.findKeys(c) // asked again: what it showed stays, searching
	if got := outline(a); len(got) < 3 || got[1] != "  db0 "+searching || got[len(got)-1] != "        1" {
		t.Errorf("searching again: %q", got)
	}
	ctx := a.win().tree.findContext()
	a.filterSeq = 0
	a.askFilter() // the filter changes
	if ctx.Err() == nil || a.win().tree.findContext().Err() != nil {
		t.Errorf("the change: the old scan %v, the new one %v", ctx.Err(), a.win().tree.findContext().Err())
	}
	if cmd := a.gotFound(foundMsg{win: a.win(), conn: c, pattern: "*users*", err: context.Canceled}); cmd != nil || !found().busy {
		t.Errorf("a cancelled scan's answer: toast %v, found %+v", cmd != nil, found())
	}
	if cmd := a.gotFound(foundMsg{win: a.win(), conn: c, pattern: "*users*", err: errors.New("x")}); cmd == nil || found() != nil {
		t.Errorf("a scan that failed: toast %v, found %+v", cmd != nil, found())
	}
	a.gotFound(foundMsg{win: a.win(), conn: c, pattern: "*users*", scan: &keyScan{total: 11}})
	if got := outline(a); len(got) != 0 {
		t.Errorf("nothing found: %q", got)
	}
	a.win().tree.filtering = true // esc clears the filter: its scan stops too
	ctx = a.win().tree.findContext()
	a.filterKey(keymap.Esc)
	if ctx.Err() == nil {
		t.Error("esc: the scan goes on")
	}
}

// A window's filter changing cancels its own scan and no other window's: the
// other window's answer would be dropped as cancelled and nothing would ask
// again (F18.11).
func TestRedisTreeSearchingPerWindow(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	b := a.win()
	b.tree.filter = ui.Input{Text: "users", Pos: 5}
	a.findKeys(c)
	ctxB := b.tree.findContext()
	a.run("window.new", 0)
	if a.win() == b {
		t.Fatal("no second window")
	}
	a.win().tree.filter = ui.Input{Text: "orders", Pos: 6}
	a.askFilter() // the other window's filter changes
	if ctxB.Err() != nil {
		t.Errorf("the first window's scan: %v", ctxB.Err())
	}
}

// A filter's hit is lit in the part of the name it is in, a namespace's or a key's,
// each place it occurs, and case counts as the server's MATCH has it; a glob is not
// lit, nor a hit that crosses the delimiter (F20.2).
func TestRedisTreeFilterLit(t *testing.T) {
	for _, tc := range []struct {
		s, lit string
		want   []int
	}{
		{"employees", "emp", []int{0, 1, 2}},
		{"kaixuan_kaixuan2", "kaixuan", []int{0, 1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 12, 13, 14}},
		{"banana", "an", []int{1, 2, 3, 4}},
		{"Employees", "emp", nil},
		{"日本語キー", "語キ", []int{2, 3}},
		{"employees", "", nil},
		{"p", "p:emp", nil},
		{"", "x", nil},
		{"\xff\xffemp", "emp", []int{1, 2, 3}}, // two bytes that are no text are one � on the node
	} {
		if got := substringPos(tc.s, tc.lit); !slices.Equal(got, tc.want) {
			t.Errorf("%q in %q: %v, want %v", tc.lit, tc.s, got, tc.want)
		}
	}
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	found := &keyScan{keys: []redis.KeyInfo{{Name: "p:employees:kaixuan", Type: "string"}, {Name: "p:emp", Type: "string"}, {Name: "pemp", Type: "string"}}, total: 3}
	lit := func(filter string) map[string][]int {
		a.win().tree.filter = ui.Input{Text: filter, Pos: len(filter)}
		a.win().tree.found = map[*Conn]*keyScan{c: found}
		out := map[string][]int{}
		ns, _ := a.treeNodes()
		for _, n := range ns {
			if n.kind == nodeNamespace || n.kind == nodeRedisKey {
				out[n.Text] = n.Pos
			}
		}
		return out
	}
	if got := lit("emp"); !slices.Equal(got["employees"], []int{0, 1, 2}) || !slices.Equal(got["emp"], []int{0, 1, 2}) || !slices.Equal(got["pemp"], []int{1, 2, 3}) || got["p"] != nil || got["kaixuan"] != nil {
		t.Errorf("emp: %v", got)
	}
	if got := lit("kaixuan"); !slices.Equal(got["kaixuan"], []int{0, 1, 2, 3, 4, 5, 6}) || got["employees"] != nil {
		t.Errorf("kaixuan: %v", got)
	}
	if got := lit("p:emp"); len(got["p"])+len(got["employees"])+len(got["emp"]) != 0 { // across the delimiter: lit in none
		t.Errorf("p:emp: %v", got)
	}
	found.keys = append(found.keys, redis.KeyInfo{Name: "a*b", Type: "string"}, redis.KeyInfo{Name: "a?b", Type: "string"}, redis.KeyInfo{Name: "x[e]mp", Type: "string"})
	for _, glob := range []string{"a*b", "a?b", "emp*", "e?p", "[e]mp"} {
		for text, pos := range lit(glob) {
			if pos != nil {
				t.Errorf("%q lit %v of %q", glob, pos, text)
			}
		}
	}
	for text, pos := range lit("") {
		if pos != nil {
			t.Errorf("no filter lit %v of %q", pos, text)
		}
	}
}

// The tree of a filter on a Redis db shows the hit in the match color on the screen,
// and what is not the hit in the node's own; golden: the tree of kaixuan (F20.2).
func TestGoldenRedisTreeFilterLit(t *testing.T) {
	a, c := redisTree(t, someKeys())
	c.State = connReady
	a.sess.open[dbNode(c, "0")] = true
	var keys []redis.KeyInfo
	for _, k := range []string{"user:kaixuan:session", "user:kaixuan:profile", "kaixuan:token", "team:kaixuan_dev:1", "team:kaixuan_dev:2", "cache:kaixuan", "app:kaixuan:x", "app:kaixuan:y"} {
		keys = append(keys, redis.KeyInfo{Name: k, Type: "string"})
	}
	a.win().tree.filter = ui.Input{Text: "kaixuan", Pos: 7}
	a.win().tree.found = map[*Conn]*keyScan{c: {keys: keys, total: 61072}}
	f := a.render()
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind == nodeNamespace && n.Text == "kaixuan" && len(n.Pos) != 7 {
			t.Errorf("the namespace is lit %v", n.Pos)
		}
	}
	if st := styleOf(t, f, "kaixuan_dev"); st.Fg != a.theme.Match { // the hit at the start of a name
		t.Errorf("hit: %+v", st)
	}
	if st := styleOf(t, f, "xuan_dev"); st.Fg != a.theme.Match { // in the middle of a name
		t.Errorf("hit in team's: %+v", st)
	}
	if st := styleOf(t, f, "_dev"); st.Fg == a.theme.Match {
		t.Errorf("what is not the hit: %+v", st)
	}
	golden.RequireEqual(t, treeTexts(a)+"\n")
}

// The tree's keys come a batch at a time, each a request of its own: a batch that is not the last asks for
// the next and the tree has nothing yet; the last, or one that failed, gives the tree what was read (F21.7).
func TestKeysLoadInSteps(t *testing.T) {
	a, _, _ := keyConsole(t)
	c := a.sess.Conns[0]
	c.scan = nil
	sc := redis.NewScanning("*", 10, redis.ScanCount)
	sc.Keys, sc.Total = []redis.KeyInfo{{Name: "a", Type: "string"}}, 5
	if cmd := a.gotKeysStep(keysStep{conn: c, scan: sc}); cmd == nil || c.scan != nil {
		t.Errorf("a batch that is not the last: next %v, tree %+v", cmd != nil, c.scan)
	}
	a.gotKeysStep(keysStep{conn: c, scan: sc, done: true})
	if c.scan == nil || len(c.scan.keys) != 1 || c.scan.total != 5 {
		t.Errorf("the last: %+v", c.scan)
	}
	c.scan = nil
	a.gotKeysStep(keysStep{conn: c, scan: sc, err: errBoom})
	if c.scan != nil || !strings.Contains(a.toast, "boom") {
		t.Errorf("a failed batch: tree %+v, toast %q", c.scan, a.toast)
	}
}

// The tree's batches wait for the first index of the db that is made, which the completion has no
// names without; one read from its file is names enough, and the tree goes on (F21.7).
func TestKeysYieldToTheFirstIndex(t *testing.T) {
	defer func(d time.Duration) { keysYield = d }(keysYield)
	keysYield = time.Millisecond
	a, _, _ := keyConsole(t)
	c := a.sess.Conns[0]
	step := func() (yielded bool) { // the yield gives the step back as it was: first still set
		defer func() { _ = recover() }() // a step that goes on asks a server this db has none of
		m, ok := a.nextKeysStep(keysStep{conn: c, scan: redis.NewScanning("*", 10, redis.ScanCount), first: true})().(keysStep)
		return ok && m.first
	}
	c.index, c.building = nil, &keyIndex{}
	if !step() {
		t.Error("the tree goes on while the first index is made")
	}
	c.index = &keyIndex{names: []string{"old"}, done: true, stale: true}
	c.index.makeCands()
	if step() {
		t.Error("the tree waits while an index read from its file is made again")
	}
	c.index, c.building = &keyIndex{done: true}, nil
	if step() {
		t.Error("the tree waits with no index being made")
	}
}
