//go:build integration

package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
)

// openKey is key name of c opened in a tab of the focused pane, its page in.
func openKey(t *testing.T, a *App, c *Conn, name string) *dataTab {
	t.Helper()
	settle(a, a.openTable(c, db.Table{Name: name, Kind: "k"}))
	d := dataOf(a.focused())
	if d == nil || d.table.Name != name || d.page.Cols == nil {
		t.Fatalf("%s: no page in %+v", name, d)
	}
	return d
}

// rowsOf are a key tab's rows as the cells say them, a row to a string.
func cellsOf(d *dataTab) (rows []string) {
	for _, r := range d.page.Rows {
		var cells []string
		for _, v := range r {
			cells = append(cells, v.S)
		}
		rows = append(rows, strings.Join(cells, "="))
	}
	return rows
}

// A key opens in a tab named for it, its rows read by its type, and says what the
// key is on the line under the title: a hash's fields, a list's places, a stream's
// entries, with the count of them after it (F16.3).
func TestKeyTabOpens(t *testing.T) {
	a, c := redisApp(t, redis.IntegrationDSN(t), 100000)
	for _, k := range []struct {
		name, tab string
		cols      []string
		rows      []string
		typ, n    string
	}{
		{"str:plain", "str:plain", []string{"value"}, []string{"hello"}, "string", "5"},
		{"hash:user", "hash:user", []string{"field", "value"}, []string{"age=30", "email=ann@example.com", "meta={\"vip\":true}", "name=Ann"}, "hash", "4"},
		{"list:queue", "list:queue", []string{"index", "value"}, []string{"0=first", "1=second", "2=third", "3=fourth", "4=fifth"}, "list", "5"},
		{"set:tags", "set:tags", []string{"member"}, []string{"blue", "green", "red"}, "set", "3"},
		{"zset:rank", "zset:rank", []string{"member", "score"}, []string{"alice=1", "bob=2.5", "carol=3", "dave=3", "erin=10"}, "zset", "5"},
		{"stream:events", "stream:events", []string{"id", "target", "type", "user"}, []string{"1700000000000-1==login=ann", "1700000000000-2=button=click=ann", "1700000000001-1==logout=ann"}, "stream", "3"},
	} {
		d := openKey(t, a, c, k.name)
		var cols []string
		for _, col := range d.page.Cols {
			cols = append(cols, col.Name)
		}
		rows := cellsOf(d)
		for i, r := range rows {
			rows[i] = strings.ReplaceAll(r, "\x00", "")
		}
		if !slices.Equal(cols, k.cols) || !slices.Equal(rows, k.rows) {
			t.Errorf("%s: %v %v, want %v %v", k.name, cols, rows, k.cols, k.rows)
		}
		got := map[string]string{}
		for _, at := range d.info {
			got[at.Name] = at.Value
		}
		if got["type"] != k.typ || got["length"] != k.n || got["ttl"] == "" {
			t.Errorf("%s info: %v", k.name, d.info)
		}
		if p := a.focused(); p.tab().Name != k.tab {
			t.Errorf("%s: tab named %q", k.name, p.tab().Name)
		}
	}
	// it is one tab of its pane, as a table is
	n := len(a.focused().Tabs)
	openKey(t, a, c, "hash:user")
	if len(a.focused().Tabs) != n {
		t.Errorf("a second tab of one key: %d tabs", len(a.focused().Tabs))
	}
}

// A change of a cell is written to the key when saved, the row it is as it was read:
// one that someone else changed meanwhile is refused (F16.3).
func TestKeyTabSaves(t *testing.T) {
	a, c := redisApp(t, redis.OwnDB(t), 1000)
	command(t, c, "HSET", "h", "a", "1", "b", "2")
	d := openKey(t, a, c, "h")
	d.edits = map[editKey]edit{{d.rowKey(0), "value"}: {val: db.Val{S: "10"}, orig: d.page.Rows[0][1]}}
	settle(a, a.save())
	if got := command(t, c, "HGET", "h", "a").Rows[0][0].S; got != "10" {
		t.Fatalf("written: %q", got)
	}
	if rows := cellsOf(d); !slices.Equal(rows, []string{"a=10", "b=2"}) || d.bar != nil {
		t.Errorf("read back: %v %v", rows, d.bar)
	}
	command(t, c, "HSET", "h", "b", "other")
	d.edits = map[editKey]edit{{d.rowKey(1), "value"}: {val: db.Val{S: "20"}, orig: d.page.Rows[1][1]}}
	settle(a, a.save())
	if got := command(t, c, "HGET", "h", "b").Rows[0][0].S; got != "other" {
		t.Errorf("a stale row was written: %q", got)
	}
	// the DDL is read again for each view: the key is not as it was
	ddl := func() string {
		if cmd := a.openDDL(false); cmd != nil {
			a.Update(cmd())
		}
		v, _ := a.ddlContent()
		a.ddl = nil
		return v.Text
	}
	if got := ddl(); !strings.Contains(got, " a 10 ") {
		t.Errorf("DDL %q", got)
	}
	command(t, c, "HSET", "h", "a", "11")
	if got := ddl(); !strings.Contains(got, " a 11 ") {
		t.Errorf("DDL after a change %q", got)
	}
	// a read-only connection refuses
	c.readOnly = true
	d.edits = map[editKey]edit{{d.rowKey(0), "value"}: {val: db.Val{S: "x"}, orig: db.Val{S: "11"}}}
	settle(a, a.save())
	if got := command(t, c, "HGET", "h", "a").Rows[0][0].S; got != "11" {
		t.Errorf("a read-only connection wrote: %q", got)
	}
}

// ↵ on a key in the tree opens it; the TTL, the rename and the delete the Info row
// has are done to the key of the tab, which reads again after each and goes on under
// the name it has been given (F16.3).
func TestKeyTabOperations(t *testing.T) {
	a, c := redisApp(t, redis.OwnDB(t), 1000)
	command(t, c, "SET", "k1", "v")
	command(t, c, "SET", "ns:k3", "v")
	settle(a, a.loadKeys(c))
	a.sess.open[dbNode(c, c.DB)] = true
	a.sess.open[dbNode(c, c.DB)+"/ns:ns"] = true
	ns, _ := a.treeNodes()
	i := slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeRedisKey && n.redisKey.Name == "ns:k3" })
	if i < 0 {
		t.Fatalf("no node of ns:k3 in %v", outline(a))
	}
	a.treeGo(i)
	settle(a, a.treeOpen(false))
	if d := dataOf(a.focused()); d == nil || d.table.Name != "ns:k3" || d.page.Cols == nil {
		t.Fatalf("↵ on a key: %+v", d)
	}
	d := openKey(t, a, c, "k1")
	info := func(name string) (v string) {
		for _, at := range d.info {
			if at.Name == name {
				v = at.Value
			}
		}
		return v
	}
	ask := func(text string) {
		t.Helper()
		if a.askName == nil {
			t.Fatal("nothing asked")
		}
		cmd, _ := a.askName.done(a, text)
		a.askName = nil
		settle(a, cmd)
	}
	settle(a, actions["key.ttl"].Run(a, Args{}))
	ask("90")
	if got := info("ttl"); got != "1m30s" && got != "1m29s" {
		t.Errorf("ttl %q, info %v", got, d.info)
	}
	settle(a, actions["key.rename"].Run(a, Args{}))
	ask("k2")
	if d.table.Name != "k2" || a.focused().tab().Name != "k2" || cellsOf(d)[0] != "v" || d.bar != nil {
		t.Errorf("renamed: %q %q %v %v", d.table.Name, a.focused().tab().Name, cellsOf(d), d.bar)
	}
	if got := command(t, c, "EXISTS", "k1", "k2").Rows[0][0].S; got != "1" {
		t.Errorf("EXISTS k1 k2 = %s", got)
	}
	settle(a, actions["key.delete"].Run(a, Args{}))
	if a.confirm == nil {
		t.Fatal("a delete is asked about")
	}
	settle(a, a.confirm.then())
	if got := command(t, c, "EXISTS", "k2").Rows[0][0].S; got != "0" || d.bar == nil {
		t.Errorf("deleted: EXISTS %s, bar %v", got, d.bar)
	}
}

// The JSON view of a key tab is the key's own value by its type, the DDL view the
// commands that make it again, and an export past the scan budget is a part of
// the key, said so (F16.3).
func TestKeyTabViews(t *testing.T) {
	dir := inDir(t)
	a, c := redisApp(t, redis.IntegrationDSN(t), 100)
	for name, want := range map[string]string{
		"str:plain":     `"hello"`,
		"hash:user":     `{"age":"30","email":"ann@example.com","meta":"{\"vip\":true}","name":"Ann"}`,
		"list:queue":    `["first","second","third","fourth","fifth"]`,
		"set:tags":      `["blue","green","red"]`,
		"zset:rank":     `[["alice",1],["bob",2.5],["carol",3],["dave",3],["erin",10]]`,
		"stream:events": `[{"id":"1700000000000-1","fields":{"type":"login","user":"ann"}},{"id":"1700000000000-2","fields":{"target":"button","type":"click","user":"ann"}},{"id":"1700000000001-1","fields":{"type":"logout","user":"ann"}}]`,
	} {
		d := openKey(t, a, c, name)
		a.viewJSON()
		got := strings.Join(strings.Fields(jsonViewOf(a.focused()).text), "")
		if got != strings.Join(strings.Fields(want), "") {
			t.Errorf("%s: %s", name, got)
		}
		a.viewJSON()
		if d.jv != nil {
			t.Fatal("the view stays")
		}
	}
	// the commands that make it again
	openKey(t, a, c, "hash:user")
	cmd := a.openDDL(false)
	msg, ok := cmd().(ddlMsg)
	if !ok {
		t.Fatalf("no DDL asked: %T", cmd())
	}
	a.Update(msg)
	if v, title := a.ddlContent(); title != "hash:user" || !strings.Contains(v.Text, "HSET hash:user") || !strings.Contains(v.Text, "Ann") {
		t.Errorf("DDL %q %q", title, v.Text)
	}
	a.ddl = nil
	if a.openDDL(true) != nil || a.ddl != nil {
		t.Error("a key has no column DDL")
	}
	// an export that reads more than the budget says it is a part; the
	// colon of hash:big is a windows-safe underscore in the file's name
	// (F18.2e)
	big := openKey(t, a, c, "hash:big")
	exportAt(t, a, exportCSVAll)
	if toast := a.toast; !strings.Contains(toast, db.ErrTruncated.Error()) {
		t.Errorf("toast %q", toast)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "hash_big*.csv"))
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.Count(string(b), "\n") < 100 || strings.Count(string(b), "\n") > 2000 || big.bar != nil {
		t.Errorf("a part of the key is in the file: %d lines", strings.Count(string(b), "\n"))
	}
}

// A stream's entries are not changed, only deleted, and a string or a stream has
// no row to add; a list's index is the place and is not changed (F16.3).
func TestKeyTabNoEdit(t *testing.T) {
	a, c := redisApp(t, redis.IntegrationDSN(t), 1000)
	d := openKey(t, a, c, "stream:events")
	if s, ok := d.cellAtCursor(); !ok || s.why != d.cols.NoUpdate || s.why == "" {
		t.Errorf("a stream's cell: %+v", s)
	}
	settle(a, a.addRow())
	if a.toast != d.cols.NoInsert || a.toast == "" {
		t.Errorf("a row of a stream: %q", a.toast)
	}
	d = openKey(t, a, c, "str:plain")
	settle(a, a.addRow())
	if a.toast != d.cols.NoInsert || a.toast == "" {
		t.Errorf("a row of a string: %q", a.toast)
	}
	d = openKey(t, a, c, "list:queue")
	if s, ok := d.cellAtCursor(); !ok || s.why == "" || d.col != 0 {
		t.Errorf("a list's index: %+v", s)
	}
}
