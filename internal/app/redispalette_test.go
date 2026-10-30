package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// keyPalette is an App on a Redis connection whose db 0 has an index of key names
// and the tree's keys, one of them typed, with the palette open on typed.
func keyPalette(t *testing.T, typed string) (*App, *Conn) {
	t.Helper()
	a, _, conn := keyConsole(t, "p:employees:1", "p:employees:2", "i18n:other", "plain")
	conn.scan = &keyScan{keys: []redis.KeyInfo{{Name: "p:employees:1", Type: "hash"}, {Name: "tree:only", Type: "list"}}, total: 5, more: true}
	conn.DB = "0"
	feed(t, a, "<C-p>"+typed)
	return a, conn
}

func keyRows(a *App) (rows []paletteItem) {
	items, ms := a.paletteMatches()
	for _, m := range ms {
		if items[m.Index].kind == itemRedisKey {
			rows = append(rows, items[m.Index])
		}
	}
	return rows
}

// The palette's @ and the all scope list the keys of the dbs whose index of names or
// whose tree has read them, with the tables, as "Key" rows placed in their db, the
// icon of the type the tree knows; none for a db that has neither, and none while
// nothing is typed. ↵ opens the key as the tree does, and typing asks the server
// of nothing (§12, F20.3).
func TestPaletteRedisKeys(t *testing.T) {
	a, conn := keyPalette(t, "@employees")
	rows := keyRows(a)
	names := []string{}
	for _, r := range rows {
		names = append(names, r.name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"p:employees:1", "p:employees:2"}) {
		t.Fatalf("@employees: %v", names)
	}
	for _, r := range rows {
		want := a.icons.Key
		if r.name == "p:employees:1" { // the tree read its type
			want = a.icons.KeyType("hash")
		}
		if r.where != "doraemon/db0" || r.icon != want || r.id != "doraemon/db0/"+r.name || itemTags[r.kind] != "Key" {
			t.Errorf("row %+v, icon want %+v", r, want)
		}
	}
	feed(t, a, "<Esc>")

	for _, tc := range []struct{ typed, want string }{
		{"@tree", "tree:only"}, // what the tree read, no index of it
		{"i18n", "i18n:other"}, // in the all scope too
		{"@", ""},              // nothing typed: no keys to read
		{"$employees", ""},     // not in the sessions' scope
		{">employees", ""},
	} {
		feed(t, a, "<C-p>"+tc.typed)
		var got []string
		for _, r := range keyRows(a) {
			got = append(got, r.name)
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%q: %q, want %q", tc.typed, got, tc.want)
		}
		feed(t, a, "<Esc><Esc>")
	}

	feed(t, a, "<C-p>@employees")
	if len(keyRows(a)) == 0 {
		t.Fatal("@employees lists none")
	}
	a.palette.input.Text = "@" // what was matched is not what nothing typed lists
	if rows := keyRows(a); len(rows) != 0 {
		t.Errorf("nothing typed after a query: %d keys", len(rows))
	}
	feed(t, a, "<Esc><Esc>")

	// a db that was not read lists none; one that was is placed in its own
	other := conn.inDB("1")
	other.State = connReady
	feed(t, a, "<C-p>@employees")
	for _, r := range keyRows(a) {
		if r.where != "doraemon/db0" {
			t.Errorf("a db that has no keys read: %+v", r)
		}
	}
	other.scan = &keyScan{keys: []redis.KeyInfo{{Name: "p:employees:9", Type: "string"}}, total: 1}
	var in1 []paletteItem
	for _, r := range keyRows(a) {
		if r.where == "doraemon/db1" {
			in1 = append(in1, r)
		}
	}
	if len(in1) != 1 || in1[0].id != "doraemon/db1/p:employees:9" {
		t.Errorf("db 1's: %+v", in1)
	}
	feed(t, a, "<Esc><Esc>")
}

// ↵ on a key opens its key tab in the data pane, and on one that is open focuses it;
// it is kept among the recent picks; the preview is empty (§12, F20.3).
func TestPaletteRedisKeyEnter(t *testing.T) {
	a, conn := keyPalette(t, "@employees:2")
	if a.palettePreview() != nil {
		t.Error("a key has a preview")
	}
	pane := a.openTarget() // where the tree's ↵ opens a table
	_, ms := a.paletteMatches()
	items, _ := a.paletteMatches()
	i := slices.IndexFunc(ms, func(m ui.Match) bool { return items[m.Index].id == "doraemon/db0/p:employees:2" })
	if i < 0 {
		t.Fatalf("not listed: %v", ms)
	}
	a.paletteRun(i, false)
	d := dataOf(pane)
	if a.palette != nil || d == nil || d.table.Name != "p:employees:2" || d.table.Kind != "k" || d.conn != conn || a.focused() != pane {
		t.Fatalf("opened: palette %v, tab %+v, focus %v", a.palette != nil, d, a.focused())
	}
	if slices.ContainsFunc(a.state.Recent, func(r config.Recent) bool { return r.Kind == "key" }) {
		t.Errorf("a key is kept among the recent picks: %+v", a.state.Recent)
	}
	a.win().focus(2)
	feed(t, a, "<C-p>@employees:2")
	items, ms = a.paletteMatches()
	a.paletteRun(slices.IndexFunc(ms, func(m ui.Match) bool { return items[m.Index].kind == itemRedisKey }), false)
	keyTabs := 0
	for _, tb := range pane.Tabs {
		if tb.Data != nil && tb.Data.table.Name == "p:employees:2" {
			keyTabs++
		}
	}
	if keyTabs != 1 || a.focused() != pane || pane.tab().Data == nil { // the open one is focused, not opened twice
		t.Errorf("opened twice: %d key tabs of %d, focus %v", keyTabs, len(pane.Tabs), a.focused())
	}
}

// Typing in the palette sends nothing to the server and starts no index: the
// keys it lists are what was read already (§12, F20.3).
func TestPaletteRedisKeysAskNothing(t *testing.T) {
	a, conn := keyPalette(t, "")
	conn.index = nil
	conn.scan.more = true // the tree stopped at its budget, and no index was begun
	for _, k := range "@employees" {
		_, cmd := a.Update(teaKey(keymap.Key(string(k))))
		if cmd == nil {
			continue
		}
		if _, ok := cmd().(previewDue); !ok { // the preview of a table the list has, as always: nothing of the keys' db
			t.Fatalf("%q asks: %T", k, cmd())
		}
	}
	if conn.index != nil || a.palette.hint != nil {
		t.Errorf("index %+v, hint %v", conn.index, a.palette.hint)
	}
}

// The palette matches the keys once for what is typed, among what the query before it
// matched when it goes on from it, and finds what matching them all finds, in the same
// order; it keeps the best keyListed, and makes the list again when a db has read more
// (F20.3).
func TestPaletteRedisKeysNarrow(t *testing.T) {
	var names []string
	for i := range 3 * keyListed {
		names = append(names, fmt.Sprintf("app%d:user_%d:s%d", i%7, i%13, i))
	}
	a, conn := keyPalette(t, "@")
	conn.index.names = names
	conn.index.makeCands()
	conn.scan = nil
	labels := func(items []paletteItem, ms []ui.Match) (out []string) {
		for _, m := range ms {
			out = append(out, items[m.Index].name)
		}
		return out
	}
	for _, q := range []string{"u", "us", "user_1", "user_12", "user_2", "a", "app3:u", "User_1", "zzz"} {
		a.palette.input.Text = "@" + q
		got := labels(a.keyMatches(q))
		a.palette.keys = nil
		if want := labels(a.keyMatches(q)); !slices.Equal(got, want) {
			t.Errorf("%q: %d narrowed, %d matching all", q, len(got), len(want))
		}
	}
	items, ms := a.keyMatches("user_1")
	if len(ms) != keyListed || len(items) != keyListed {
		t.Errorf("%d kept of many that match", len(ms))
	}
	a.keyMatches("user_12")
	if all := len(a.palette.keys.items); len(a.palette.keys.cand) >= all {
		t.Error("what goes on from the last query looks at all of them")
	}
	before := a.palette.keys
	if a.keyMatches("user_12"); a.palette.keys != before {
		t.Error("the same query makes the list again")
	}
	conn.index.names = append(conn.index.names, "user_12:late")
	conn.index.cands = nil
	if items, ms := a.keyMatches("late"); len(ms) != 1 || items[ms[0].Index].name != "user_12:late" {
		t.Errorf("a name the index read after the list was made: %v", labels(items, ms))
	}
}

// BenchmarkPaletteRedisKeys is the keystrokes of "@user_123" over an index of
// index_keys (100000) names, the lists made again for each, as a frame does
// eleven times (F20.3): the first one makes the list of keys and matches it, the
// next ones go on from the last match. Past 30ms for the first, to say in the review.
func BenchmarkPaletteRedisKeys(b *testing.B) {
	var names []string
	for i := range 100000 {
		names = append(names, fmt.Sprintf("app%d:user_%d:session:%d", i%40, i%997, i))
	}
	a := sized(160, 45, "nerd")
	conn := a.sess.Conns[0]
	conn.Spec.Engine, conn.drv, conn.eng, conn.DB = "redis", kvDriver{}, nil, "0"
	conn.index = &keyIndex{names: names, done: true}
	a.openPalette("@")
	var first, slowest time.Duration
	for b.Loop() {
		a.palette.keys = nil
		for n := 1; n <= len("user_123"); n++ {
			a.palette.input.Text = "@" + "user_123"[:n]
			start := time.Now()
			for range 11 { // the calls of one keystroke and its frame
				a.paletteMatches()
			}
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
