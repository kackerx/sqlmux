package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// inQuery is the default layout with focus on query_1 (⟨2⟩), whose
// file is in a directory of the test's own, with the user's config.toml
// conf and keymaps.toml keys.
func inQuery(t testing.TB, conf, keys string) (*App, *queryTab) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sizedWith(160, 45, userConfig(t, conf, keys))
	feed(t, a, "<C-l>")
	c := queryOf(a.focused())
	if c == nil {
		t.Fatal("C-l from ⟨1⟩ is not on the query")
	}
	return a, c
}

func text(c *queryTab) string { return strings.Join(c.ed.Lines(), "\n") }

// Keys the keymap leaves go to the editor; one waiting for more takes the
// next key itself, so f<Space>x is no SPC x (§6.4). : and ; are its vim's.
func TestQueryKeys(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "ione two three<CR>four<Esc>gg0f<Space>x")
	if text(c) != "onetwo three\nfour" || len(a.win().Root.Leaves()) != 2 {
		t.Fatalf("f<Space>x: %q, panes %d", text(c), len(a.win().Root.Leaves()))
	}
	feed(t, a, "0fe;")
	if c.ed.Cursor() != (editor.Pos{Line: 0, Col: 10}) {
		t.Errorf("; repeats f: %v", c.ed.Cursor())
	}
	feed(t, a, ":")
	if a.palette != nil || c.ed.Mode() != editor.Command {
		t.Fatalf(": opens the editor's command line: palette %v, mode %v", a.palette, c.ed.Mode())
	}
	feed(t, a, "2<CR>")
	if c.ed.Cursor().Line != 1 {
		t.Errorf(":2 goes to line 2: %v", c.ed.Cursor())
	}
	feed(t, a, "d<C-c>") // C-c is esc: no quit toast
	if c.ed.Pending() != "" || a.toast != "" {
		t.Errorf("d then C-c: pending %q, toast %q", c.ed.Pending(), a.toast)
	}
	feed(t, a, ":foo<CR>")
	if a.toast != "不支持的命令：foo" {
		t.Errorf("toast %q", a.toast)
	}
}

// The user's maps for the query apply there, over the editor's own keys;
// the right-hand side goes in as typed, meeting the editor as it then is
// (§6.6): after i, a space is typed, no leader.
func TestQueryMaps(t *testing.T) {
	a, c := inQuery(t, "tab_width = 4", "[map.query.normal]\nL = \"5l\"\nQ = \"iselect x<Esc>\"\nX = \"f<Space>\"\n[map.query.visual]\nH = \"2h\"\n")
	feed(t, a, "Q")
	if text(c) != "select x" || c.ed.Mode() != editor.Normal || len(a.win().Root.Leaves()) != 2 {
		t.Fatalf("Q = iselect x<Esc>: %q in %v, panes %d", text(c), c.ed.Mode(), len(a.win().Root.Leaves()))
	}
	feed(t, a, "0Xxx")
	if text(c) != "select" || len(a.win().Root.Leaves()) != 2 {
		t.Fatalf("X = f<Space>, x, x: %q, panes %d", text(c), len(a.win().Root.Leaves()))
	}
	feed(t, a, "dd")
	feed(t, a, "iabcdefgh<Esc>0L")
	if c.ed.Cursor().Col != 5 {
		t.Errorf("L = 5l: col %d", c.ed.Cursor().Col)
	}
	if feed(t, a, "vH"); c.ed.Cursor().Col != 3 || c.ed.Mode() != editor.Visual {
		t.Errorf("H = 2h in VISUAL: col %d in %v", c.ed.Cursor().Col, c.ed.Mode())
	}
	feed(t, a, "<Esc>")
	feed(t, a, "o<Tab>x<Esc>")
	if c.ed.Lines()[1] != "    x" {
		t.Errorf("tab_width 4: %q", c.ed.Lines()[1])
	}
}

// The file (§11): :w writes it, 0600; unchanged it is not written; :q
// writes and closes; a new session has it back. The autosave writes only
// for the latest change.
func TestQueryFile(t *testing.T) {
	a, c := inQuery(t, "", "")
	if c.path != config.QueryPath("query_1") {
		t.Fatalf("path %s", c.path)
	}
	feed(t, a, "iselect 1;<Esc>")
	a.Update(autosave{c, c.ver - 1})
	if _, err := os.Stat(c.path); err == nil {
		t.Fatal("a stale autosave wrote")
	}
	a.Update(autosave{c, c.ver})
	if data, _ := os.ReadFile(c.path); string(data) != "select 1;\n" {
		t.Fatalf("autosave: %q", data)
	}
	os.Remove(c.path)
	feed(t, a, ":w<CR>")
	if _, err := os.Stat(c.path); err == nil {
		t.Error("written with nothing changed")
	}
	feed(t, a, "A -- one<Esc>:w<CR>")
	info, err := os.Stat(c.path)
	if err != nil || (info.Mode().Perm() != 0o600 && runtime.GOOS != "windows") { // windows has no group/other bits
		t.Fatalf(":w: %v %v", info, err)
	}
	feed(t, a, "o2<Esc>:wq<CR>")
	if data, _ := os.ReadFile(c.path); string(data) != "select 1; -- one\n2\n" || len(a.win().Root.Leaves()) != 1 {
		t.Fatalf(":wq: %q, panes %d", data, len(a.win().Root.Leaves()))
	}
	if s := soloSession(); text(queryOf(s.Windows[0].Root.B.Pane)) != "select 1; -- one\n2" || s.warning != "" {
		t.Fatalf("a new session: %q", s.warning)
	}
}

// A query runs on the connection its title's dropdown picks (§7.8
// 「query 标题」): only that part lights under the pointer; a connected
// one at once, in its Schema, kept for the file (§11); not while its own
// is in a transaction; one not connected once it connects, the query
// staying meanwhile, and on, it failing.
func TestQueryConn(t *testing.T) {
	a, c := inQuery(t, "", "")
	seed := readyConn("seed")
	seed.Schema, seed.Schemas, seed.DB = "s1", []string{"s1", "s2"}, "seeddb"
	idle := newConn(config.Connection{Name: "idle", Engine: "postgres"})
	a.sess.Conns = append(a.sess.Conns, seed, idle)
	r := find(t, a, ui.Target{Kind: ui.KindHint, Pane: a.focused().ID, Action: "query.conn"})
	a.Update(tea.MouseMotionMsg{X: r.Min.X, Y: r.Min.Y})
	if f := a.render(); f.Buf.CellAt(r.Max.X-1, r.Min.Y).Style.Bg != a.theme.Select || f.Buf.CellAt(r.Max.X+1, r.Min.Y).Style.Bg == a.theme.Select {
		t.Error("the pointer on the connection lights it alone, not the ·")
	}
	click(a, r.Min)
	if v := a.dropView(); a.drop == nil || strings.Join(v.Items, " ") != "doraemon seed idle" || v.Mark != 0 {
		t.Fatalf("dropdown %+v", a.drop)
	}
	feed(t, a, "seed<CR>")
	if c.conn != seed || c.schema != "s1" || a.state.Queries["query_1"] != (config.QueryState{Conn: "seed", Schema: "s1"}) {
		t.Fatalf("seed: on %s.%s, state %+v", c.conn.Spec.Name, c.schema, a.state.Queries)
	}
	if top := strings.Split(a.render().String(), "\n")[r.Min.Y]; !strings.Contains(top, "seed · seeddb.s1 ▾") {
		t.Errorf("title %q", top)
	}
	feed(t, a, "gs<C-n><CR>")
	if c.schema != "s2" || a.state.Queries["query_1"].Schema != "s2" {
		t.Errorf("schema %s, kept %+v", c.schema, a.state.Queries)
	}
	seed.tx = 'T'
	a.run("query.conn", 0)
	if feed(t, a, "doraemon<CR>"); c.conn != seed || a.toast != "当前连接有未提交的事务，先提交或回滚" {
		t.Errorf("in a transaction: on %s, toast %q", c.conn.Spec.Name, a.toast)
	}
	seed.tx, c.running = 'I', &run{}
	a.run("query.conn", 0)
	if feed(t, a, "doraemon<CR>"); c.conn != seed || a.toast != "正在执行，结束后再换连接" {
		t.Errorf("running: on %s, toast %q", c.conn.Spec.Name, a.toast)
	}
	c.running = nil
	a.run("query.conn", 0)
	if feed(t, a, "idle<CR>"); c.conn != seed || c.want != idle || idle.State != connConnecting {
		t.Fatalf("idle: on %s, want %v, %v", c.conn.Spec.Name, c.want, idle.State)
	}
	if a.Update(connected{conn: idle, err: errors.New("no route")}); c.conn != seed || c.want != nil || a.toast != "idle：no route" {
		t.Errorf("idle failing: on %s, want %v, toast %q", c.conn.Spec.Name, c.want, a.toast)
	}
}

// query_1 of the layout started with opens where its file last ran:
// on a connection connected, now; on one not, once it connects; a schema
// gone, the connection's once its catalog is in. A new query goes on the
// current connection, in its Schema, kept for its file (§11, F5.11).
func TestQueryRestore(t *testing.T) {
	a, c1 := inQuery(t, "", "")
	seed, idle := readyConn("seed"), newConn(config.Connection{Name: "idle", Engine: "postgres"})
	seed.Schema = "s1"
	a.sess.Conns = append(a.sess.Conns, seed, idle)
	a.state.Queries = map[string]config.QueryState{"query_1": {Conn: "seed", Schema: "dropped"}, "query_2": {Conn: "idle", Schema: "x"}}
	if a.Init(); c1.conn != seed || c1.schema != "dropped" {
		t.Errorf("query_1 at start: on %s.%s", c1.conn.Spec.Name, c1.schema)
	}
	a.Update(catalogMsg{conn: seed, schemas: []string{"s1"}, current: "s1"})
	if c1.schema != "s1" || a.state.Queries["query_1"].Schema != "s1" {
		t.Errorf("its schema gone: %s, kept %+v", c1.schema, a.state.Queries["query_1"])
	}
	a.run("query.new", 0)
	c := queryOf(a.focused())
	if c.conn != seed || c.schema != "s1" || c.want != nil || idle.State != connIdle || a.state.Queries["query_2"] != (config.QueryState{Conn: "seed", Schema: "s1"}) {
		t.Errorf("query_2: on %s.%s, want %v, kept %+v", c.conn.Spec.Name, c.schema, c.want, a.state.Queries["query_2"])
	}
}

// soloSession is a new session over doraemon alone, not connected.
func soloSession() *Session {
	c := newConn(config.Connection{Name: "doraemon", Engine: "postgres"})
	return newSession("doraemon", []*Conn{c}, c, config.QueryName(1))
}

// A query file that cannot be read is not opened, empty or not: a toast
// says so and the rest starts (§11).
func TestQueryUnreadable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	// a directory where the file goes
	if err := os.MkdirAll(config.QueryPath("query_1"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := soloSession()
	if p := s.Windows[0].Root.B.Pane; len(p.Tabs) != 0 || !strings.HasPrefix(s.warning, "读取失败：") {
		t.Fatalf("tabs %v, warning %q", p.Tabs, s.warning)
	}
	c := config.Default()
	keys, _ := keymap.New(c.Leader, c.Vim, c.Timeoutlen, c.Bindings)
	if a := newApp(c, keys, s, &config.State{}, "careful"); a.warning != "careful；"+s.warning {
		t.Errorf("warning %q", a.warning)
	}
}

// A query that cannot be written says so, and neither closes nor quits.
func TestQuerySaveFails(t *testing.T) {
	a, c := inQuery(t, "", "")
	c.path = filepath.Join(os.DevNull, "query_1.sql")
	feed(t, a, "ix<Esc>:w<CR>")
	if !strings.HasPrefix(a.toast, "保存失败：") {
		t.Fatalf("toast %q", a.toast)
	}
	if feed(t, a, ":q<CR>"); queryOf(a.focused()) != c {
		t.Error(":q closed what was not written")
	}
	if feed(t, a, "<C-p>>qa<CR>") {
		t.Error("quit with the query not written")
	}
	c.path = filepath.Join(t.TempDir(), "query_1.sql")
	if !feed(t, a, "<C-p>>qa<CR>") {
		t.Fatal("quit")
	}
	if data, _ := os.ReadFile(c.path); string(data) != "x\n" {
		t.Errorf("quitting writes the query: %q", data)
	}
}

// A paste is text in every mode, never keys (§11); a yank into "+ goes to
// the clipboard, a plain one not; "+p asks the terminal for it and puts
// what comes back (F3.38).
func TestQueryPasteAndYank(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "iab<Esc>0")
	a.Update(tea.PasteMsg{Content: "dd\r\nx"})
	if text(c) != "add\nxb" || c.ed.Mode() != editor.Normal {
		t.Fatalf("pasted in NORMAL: %q %v", text(c), c.ed.Mode())
	}
	if clipped(keys(t, a, "yy"), "xb\n") {
		t.Error("yy reaches the clipboard")
	}
	if !clipped(keys(t, a, `"+yy`), "xb\n") {
		t.Error(`"+yy does not reach the clipboard`)
	}
	if cmd := keys(t, a, `"+p`); cmd == nil || text(c) != "add\nxb" {
		t.Fatalf(`"+p before the clipboard comes: %q`, text(c))
	}
	if a.Update(tea.ClipboardMsg{Content: "clip"}); text(c) != "add\nxclipb" {
		t.Errorf(`"+p: %q`, text(c))
	}
}

// The status bar: the editor's mode by name in VISUAL's or INSERT's color,
// the selection's size with the key that runs it, what the editor waits
// on (§7.8).
func TestQueryStatusBar(t *testing.T) {
	a, _ := inQuery(t, "", "")
	feed(t, a, "iabc<CR>def<Esc>gg")
	th := a.theme
	for _, c := range []struct {
		keys, mode, info string
		bg               any
	}{
		{"vl", "VISUAL", "2 字符 · ↵ run", th.Keyword},
		{"<Esc>Vj", "V-LINE", "2 行 · ↵ run", th.Keyword},
		{"<Esc>gg<C-v>jl", "V-BLOCK", "2 行 × 2 列 · ↵ run", th.Keyword},
		{"<Esc>R", "REPLACE", "", th.Warn},
		{"<Esc>:", "COMMAND", "", th.Info},
		{"<Esc>2d", "NORMAL", "", th.Focus},
	} {
		feed(t, a, c.keys)
		f := a.render()
		bar := strings.Split(f.String(), "\n")[44]
		if st := styleOf(t, f, " "+c.mode+" "); !strings.Contains(bar, " "+c.mode+" ") || st.Bg != c.bg {
			t.Errorf("%s: %q, bg %v", c.keys, bar, st.Bg)
		}
		if c.info != "" && !strings.Contains(bar, c.info) {
			t.Errorf("%s: no %q in %q", c.keys, c.info, bar)
		}
	}
	if bar := strings.Split(a.render().String(), "\n")[44]; !strings.Contains(bar, " 2d ") {
		t.Errorf("showcmd: %q", bar)
	}
}

// The terminal cursor is vim's: a block, a bar in INSERT, a line under in
// REPLACE (§11).
func TestQueryCursorShape(t *testing.T) {
	a, _ := inQuery(t, "", "")
	for _, c := range []struct {
		keys string
		want tea.CursorShape
	}{{"", tea.CursorBlock}, {"i", tea.CursorBar}, {"<Esc>R", tea.CursorUnderline}, {"<Esc>:", tea.CursorBlock}} {
		feed(t, a, c.keys)
		if v := a.View(); v.Cursor == nil || v.Cursor.Shape != c.want {
			t.Errorf("%q: cursor %+v", c.keys, v.Cursor)
		}
	}
}

// A click puts the cursor there, a drag selects, the wheel scrolls the
// query under the pointer without taking the focus (§11).
func TestQueryMouse(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "i"+strings.Repeat("select 1;<CR>", 60)+"<Esc>gg")
	feed(t, a, "<C-h>") // focus back on ⟨1⟩
	text := c.ed.Top()
	wheel := func() {
		r := a.layout()[2]
		a.Update(tea.MouseWheelMsg{X: r.Min.X + 10, Y: r.Min.Y + 5, Button: tea.MouseWheelDown})
	}
	if wheel(); c.ed.Top() != text+wheelStep || a.win().Focus != 1 {
		t.Fatalf("wheel: top %d, focus %d", c.ed.Top(), a.win().Focus)
	}
	view, body := a.queryView(a.win().pane(2), c)
	at := view.TextArea(body).Min
	click(a, uv.Pos(at.X+3, at.Y+1))
	if a.win().Focus != 2 || c.ed.Cursor() != (editor.Pos{Line: c.ed.Top() + 1, Col: 3}) {
		t.Fatalf("click: focus %d, cursor %v", a.win().Focus, c.ed.Cursor())
	}
	a.Update(tea.MouseClickMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: at.X + 2, Y: at.Y + 1, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: at.X + 2, Y: at.Y + 1, Button: tea.MouseLeft})
	from, to, ok := c.ed.Selection()
	if !ok || from != (editor.Pos{Line: c.ed.Top(), Col: 0}) || to != (editor.Pos{Line: c.ed.Top() + 1, Col: 2}) {
		t.Errorf("drag: %v %v %v", from, to, ok)
	}
	if a.Update(tea.MouseMotionMsg{X: at.X + 5, Y: at.Y + 1}); c.ed.Cursor().Col != 2 {
		t.Error("moving after the release selects")
	}
}

// $EDITOR edits the file, written first; what it holds after is one undo
// step (§11).
func TestQueryExternal(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "iselect 1<Esc>")
	if a.run("query.external", 0) == nil {
		t.Fatal("no editor run")
	}
	if data, _ := os.ReadFile(c.path); string(data) != "select 1\n" {
		t.Fatalf("not written first: %q", data)
	}
	if err := os.WriteFile(c.path, []byte("select 2;\nselect 3;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.Update(externalDone{c, nil})
	if text(c) != "select 2;\nselect 3;" || c.saved != c.ver {
		t.Fatalf("read back: %q, saved %d of %d", text(c), c.saved, c.ver)
	}
	if feed(t, a, "u"); text(c) != "select 1" {
		t.Errorf("u: %q", text(c))
	}
}

// A keyword taken in a query gets a space, and the DML starts are
// offered first (§9.7); a word typed out is the first, a table's name not
// taking its place, a keyword of two words not put in twice; one undo takes
// it back with the INSERT.
func TestQueryKeywordSpace(t *testing.T) {
	a, c := inQuery(t, "", "")
	for in, first := range map[string]string{"ins": "insert into", "upd": "update", "del": "delete from"} {
		feed(t, a, "<Esc>ggdGi"+in)
		if c.comp == nil || c.comp.items[0].label != first {
			t.Fatalf("%s: %+v", in, c.comp)
		}
	}
	for in, want := range map[string]string{
		"selec":                     "select ",
		"SELECT":                    "SELECT ",
		"update t_order set":        "update t_order set ",
		"alter table t rename to":   "alter table t rename to ",
		"insert into":               "insert into ",
		"select 1 from t union all": "select 1 from t union all ",
		"insert into t_order (id) values (1) on conflict (id) do update set": "insert into t_order (id) values (1) on conflict (id) do update set ",
	} {
		feed(t, a, "<Esc>ggdGi"+in+"<CR>")
		if text(c) != want || c.ed.Cursor().Col != len(want) {
			t.Errorf("%s ↵: %q at %v", in, text(c), c.ed.Cursor())
		}
	}
	feed(t, a, "<Esc>ggdGiinsert in")
	i := slices.IndexFunc(c.comp.items, func(cd candidate) bool { return cd.label == "insert into" })
	if i < 0 {
		t.Fatalf("insert in: %+v", c.comp.items)
	}
	c.comp.sel = i
	if feed(t, a, "<CR>"); text(c) != "insert into " {
		t.Errorf("insert in, insert into taken: %q", text(c))
	}
	feed(t, a, "<Esc>u")
	if text(c) != "" {
		t.Errorf("u: %q", text(c))
	}
}

// INSERT INTO t ( lists the columns the parentheses lack, all of them first,
// and a generated one never; taking all of them leaves the cursor before the
// ) (§9.7).
func TestQueryInsertColumns(t *testing.T) {
	a, c := inQuery(t, "", "")
	table, cols, _ := ordersTable(0)
	cols.Cols[2].Generated = true
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
	feed(t, a, "iinsert into t_order(")
	if c.comp == nil || len(c.comp.items) != 7 || c.comp.items[0].note != "全部 6 列" || !strings.HasPrefix(c.comp.items[0].label, "id, status, paid") {
		t.Fatalf("after (: %+v", c.comp)
	}
	feed(t, a, "<CR>")
	if text(c) != "insert into t_order(id, status, paid, meta, note, created_at)" || c.ed.Cursor().Col != len(text(c))-1 {
		t.Fatalf("all: %q at %v", text(c), c.ed.Cursor())
	}
	feed(t, a, "<Esc>ggdGiinsert into t_order(id")
	feed(t, a, ", ")
	if c.comp == nil || len(c.comp.items) != 6 || c.comp.items[0].label == "id" || c.comp.items[1].label != "status" {
		t.Fatalf("after a comma: %+v", c.comp)
	}
	feed(t, a, "s")
	if c.comp == nil || c.comp.items[0].label != "status" || strings.HasPrefix(c.comp.items[0].note, "全部") {
		t.Fatalf("a word typed: no all-columns item: %+v", c.comp)
	}
	feed(t, a, "<Esc>ggdGiinsert into t_order(id, status, paid, meta, note, ")
	if c.comp == nil || len(c.comp.items) != 1 || c.comp.items[0].label != "created_at" {
		t.Fatalf("one column left: %+v", c.comp)
	}
	cols.Cols = append(cols.Cols, db.Column{Name: "x"})
	a.sess.Conns[0].cols[idOf(table)] = cols
	feed(t, a, "<Esc>ggdGiinsert into t_order(")
	if all := c.comp.items[0]; c.comp == nil || all.note != "全部 7 列" || all.label != "id, status, paid, meta, note, created_a…" || all.insert != "id, status, paid, meta, note, created_at, x" {
		t.Fatalf("41+ characters: %q, inserts %q", all.label, all.insert)
	}
}

// With the candidates up ↵ only takes the selected one, though the text
// does not change; a word takes a space, a function or a value or an
// INSERT's column does not (§9.7, F8.1).
func TestQueryEnterOnlyAccepts(t *testing.T) {
	a, c := inQuery(t, "", "")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
	for _, c2 := range []struct{ keys, want string }{
		{"iselect * from t_order where id<CR>", "select * from t_order where id "}, // a column: the next word follows
		{"<Esc>ggdGiselect count<CR>", "select count"},                             // a function: a ( follows
		{"<Esc>ggdGiselect * from t_order where paid is not null<CR>", "select * from t_order where paid is not null"},
		{"<Esc>ggdGiinsert into t_order(id<CR>", "insert into t_order(id)"}, // a list's item: , or ) follows
	} {
		feed(t, a, c2.keys)
		if text(c) != c2.want || c.comp != nil {
			t.Errorf("%q: %q, list %v", c2.keys, text(c), c.comp != nil)
		}
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order where id<CR><CR>")
	if text(c) != "select * from t_order where id \n" {
		t.Errorf("a second ↵ is the newline: %q", text(c))
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order where<CR><CR>") // the first ↵ takes where and its space, a list opens with nothing selected: the second ↵ is the newline
	if text(c) != "select * from t_order where \n" || c.comp != nil {
		t.Errorf("where↵↵: %q", text(c))
	}
	feed(t, a, "<Esc>ggdGiwith recent as (select 1) select * from rec<CR>") // a CTE's name is a word too
	if text(c) != "with recent as (select 1) select * from recent " {
		t.Errorf("a CTE: %q", text(c))
	}
	// keys that type nothing open no list: A and i on a word that is one's, an arrow
	for _, k := range []struct{ text, keys, want string }{
		{"select * from t_order", "A<CR>", "select * from t_order\n"},
		{"select * from t_order", "0wwi<CR>", "select * \nfrom t_order"}, // i before a word
		{"select id,", "A<CR>", "select id,\n"},
		{"select id,", "o<CR>", "select id,\n\n"},
	} {
		feed(t, a, "<Esc>ggdGi"+k.text+"<Esc>"+k.keys)
		if text(c) != k.want || c.comp != nil {
			t.Errorf("%q then %s: %q, list %v", k.text, k.keys, text(c), c.comp != nil)
		}
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order where id = 1 and paid<Left><Left><Left><Left><CR>")
	if text(c) != "select * from t_order where id = 1 and \npaid" {
		t.Errorf("an arrow opens no list: %q", text(c))
	}
}

// What comes after the cursor is not given a space before it: ) , ; . are
// there already (§9.7, F8.1).
func TestQueryAcceptNoSpaceBeforePunct(t *testing.T) {
	a, c := inQuery(t, "autopairs = false", "")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
	for _, k := range []struct{ text, after, want string }{
		{"select * from t_order where (stat", ")", "select * from t_order where (status)"},
		{"select * from t_order where id = 1 and stat", ";", "select * from t_order where id = 1 and status;"},
		{"select stat", ", id from t_order", "select status, id from t_order"},
		{"select * from t_ord", ".x", "select * from t_order.x"},
		{"select * from t_ord", "", "select * from t_order "},
	} {
		feed(t, a, "<Esc>ggdGi"+k.after+"<Esc>0i"+k.text+"<CR>")
		if got := text(c); got != k.want {
			t.Errorf("%q|%q: %q, want %q", k.text, k.after, got, k.want)
		}
	}
}

// Where a column comes next the candidates open with nothing typed, once
// a blank is: the statement's tables' columns alone, none when the
// statement names no table yet; esc closes them and typing filters them
// (§9.7, F8.4).
func TestQueryColumnsAfterBlank(t *testing.T) {
	a, c := inQuery(t, "", "")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
	labels := func() (out []string) {
		if c.comp != nil {
			for _, cd := range c.comp.items {
				out = append(out, cd.label)
			}
		}
		return out
	}
	want := make([]string, len(cols.Cols))
	for i, col := range cols.Cols {
		want[i] = col.Name
	}
	for _, sql := range []string{
		"select * from t_order where ", "select * from t_order where id = 1 and ", "select * from t_order where id = 1 or ",
		"select * from t_order order by id, ", "update t_order set ", "select * from t_order group by status having ",
		"select * from t_order o join t_order p on ",
	} {
		feed(t, a, "<Esc>ggdGi"+sql)
		if got := labels(); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want the columns %v", sql, got, want)
		}
	}
	feed(t, a, "<Esc>ggdGiselect ")
	if got := labels(); got != nil {
		t.Errorf("no table yet, but a list: %v", got)
	}
	feed(t, a, "<Esc>ggdGi from t_order<Esc>0iselect ")
	if got := labels(); !slices.Equal(got, want) {
		t.Errorf("the FROM after the cursor: %v", got)
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order where a")
	if feed(t, a, "<BS> "); c.comp == nil || len(c.comp.items) != len(want) || c.comp.items[0].kind == kindKeyword {
		t.Errorf("after the BS and a blank: %v", labels())
	}
	if feed(t, a, "<Esc>"); c.comp != nil {
		t.Error("esc closes it")
	}
	feed(t, a, "ggdGiselect * from t_order where st")
	if got := labels(); len(got) == 0 || got[0] != "status" {
		t.Errorf("typing filters: %v", got)
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order where <Esc>a<C-n>") // a lists everything: the keywords too
	if got := labels(); !slices.Contains(got, "id") || !slices.Contains(got, "and") || len(got) <= len(want) {
		t.Errorf("C-n after where: %v", got)
	}
	feed(t, a, "<Esc>ggdGiselect <Esc>a<C-n>") // the FROM is not there yet: tables and keywords
	if got := labels(); !slices.Contains(got, "select") || !slices.Contains(got, "t_order") {
		t.Errorf("C-n after select: %v", got)
	}
	// the tables at the cursor's depth first, the outer ones after
	conn := a.sess.Conns[0]
	conn.cols[tableID{"public", "t_user"}] = db.Columns{Cols: []db.Column{{Name: "uname"}, {Name: "email"}}}
	feed(t, a, "<Esc>ggdGiselect * from t_order where id in (select from t_user"+strings.Repeat("<Left>", 11)+" ")
	if got := labels(); !slices.Equal(got, append([]string{"uname", "email"}, want...)) {
		t.Errorf("a subquery: %v", got)
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order where id = 1,")
	if c.comp != nil {
		t.Errorf("no blank, no list: %v", labels())
	}
	feed(t, a, "<Esc>ggdGiselect * from ")
	if got := labels(); got != nil {
		t.Errorf("after FROM nothing opens on its own: %v", got)
	}
	if feed(t, a, "<C-n>"); len(labels()) == 0 || slices.Contains(labels(), "id") {
		t.Errorf("C-n after FROM: the tables, no columns: %v", labels())
	}
}

// In a query the candidates follow the cursor's place (§9.7): tables
// after FROM; an alias's columns right after its ., fetched once and
// completed again as they come; CTE names among the tables; C-n with
// nothing typed. ↵ takes one as typing it would, part of the INSERT's undo
// step, and nothing else (TestQueryEnterOnlyAccepts); esc closes the list
// and leaves INSERT, as nvim-cmp; a character not of a name closes it.
func TestQueryCompletion(t *testing.T) {
	a, c := inQuery(t, "", "")
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	feed(t, a, "iselect * from t_o")
	if c.comp == nil || c.comp.items[0].label != "t_order" || a.context().Overlay != "complete" {
		t.Fatalf("after from: %+v, overlay %q", c.comp, a.context().Overlay)
	}
	feed(t, a, "<CR>")
	if text(c) != "select * from t_order " || c.comp != nil || c.ed.Mode() != editor.Insert {
		t.Fatalf("↵ takes t_order: %q in %v", text(c), c.ed.Mode())
	}
	feed(t, a, "o where o.")
	if !c.asked[idOf(table)] || c.comp != nil {
		t.Fatalf("o.: asked %v, list %+v", c.asked, c.comp)
	}
	a.Update(colsMsg{a.sess.Conns[0], table, cols, nil})
	if c.comp == nil || c.comp.items[0].label != "id" || c.comp.items[0].note != "bigint · t_order" {
		t.Fatalf("the columns in: %+v", c.comp)
	}
	feed(t, a, "st<CR>")
	if text(c) != "select * from t_order o where o.status " {
		t.Fatalf("o.st ↵: %q", text(c))
	}
	feed(t, a, "9<CR>")
	if text(c) != "select * from t_order o where o.status 9\n" {
		t.Fatalf("no list: ↵ is a newline: %q", text(c))
	}
	feed(t, a, "<Esc>u")
	if text(c) != "" {
		t.Fatalf("u takes back the INSERT, what was taken with it: %q", text(c))
	}
	feed(t, a, "iwith recent as (select 1) select * from ")
	if c.comp != nil {
		t.Fatal("a blank opens nothing")
	}
	feed(t, a, "<C-n>")
	if c.comp == nil || c.comp.items[0].label != "recent" || c.comp.items[0].note != "CTE" {
		t.Fatalf("C-n: %+v", c.comp)
	}
	feed(t, a, "<Esc>")
	if c.comp != nil || c.ed.Mode() != editor.Normal {
		t.Fatalf("esc: list %+v in %v", c.comp, c.ed.Mode())
	}
	feed(t, a, "At_o;")
	if c.comp != nil {
		t.Error("; closes it")
	}
}

// X. lists an alias's or a table's columns, the one nearest the cursor's
// depth; a schema's tables after a schema; nothing for a subquery's alias
// out of it (§9.7, lazysql's resolveAliases).
func TestSQLCompleteQualifier(t *testing.T) {
	a := sized(160, 45, "nerd")
	table, cols, _ := ordersTable(0)
	a.sess.Conns[0].cols[idOf(table)] = cols
	a.sess.Conns[0].cols[tableID{"public", "t_user"}] = db.Columns{Cols: []db.Column{{Name: "email", Type: "text"}}}
	a.sess.Conns[0].cols[tableID{"agentable", "planner"}] = db.Columns{Cols: []db.Column{{Name: "goal", Type: "text"}}}
	for _, c := range []struct{ sql, first string }{
		{"select * from agentable.|", "planner"},
		{"select * from t_order join agentable.| on", "planner"},
		{"select planner.| from agentable.planner", "goal"},
		{"select * from t_order o where exists (select 1 from t_user o where o.|", "email"},
		{"select * from t_order o where o.| in (select 1 from t_user o)", "id"},
		{"select a.| from (select * from t_user a) sub", ""},
	} {
		pos := strings.Index(c.sql, "|")
		got, _ := a.sqlComplete(a.sess.Conns[0], strings.Replace(c.sql, "|", "", 1), pos, "public", map[tableID]bool{}, true)
		first := ""
		if got != nil {
			first = got.items[0].label
		}
		if first != c.first {
			t.Errorf("%s: %q, want %q", c.sql, first, c.first)
		}
	}
}

// A query's schema (§8.6): on its title, gs or a click opens the
// dropdown there, its own schema marked, ↵ picks; a new query takes the
// tree's, then each keeps its own; completion lists its schema's tables.
func TestQuerySchema(t *testing.T) {
	a, c := inQuery(t, "", "")
	title := func() string { return strings.Split(a.render().String(), "\n")[0] }
	if !strings.Contains(title(), " doraemon.public ▾  ▶ run ") {
		t.Fatalf("title %q", title())
	}
	feed(t, a, "gs")
	if a.drop == nil || a.drop.kind != dropSchema || a.dropView().Mark != 1 {
		t.Fatalf("gs: %+v", a.drop)
	}
	feed(t, a, "age<CR>")
	if c.schema != "agentable" || a.drop != nil || !strings.Contains(title(), " doraemon.agentable ▾ ") {
		t.Fatalf("picked: %q, title %q", c.schema, title())
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: a.focused().ID, Action: "query.schema"}).Min)
	if a.drop == nil || a.drop.query != c {
		t.Fatal("a click opens it")
	}
	// under the button as this frame draws it, to the pane's right border;
	// at that border with the button left out
	for _, w := range []int{130, 160, 110} {
		a.Update(tea.WindowSizeMsg{Width: w, Height: 45})
		f, r := a.render(), a.layout()[a.focused().ID]
		x, y := r.Max.X-16, r.Min.Y+1 // 16: its least width, more than its items need
		for _, h := range f.Hits {
			if h.Target.Action == "query.schema" {
				x = h.Rect.Min.X
			}
		}
		if f.Buf.CellAt(x, y).Content != "┌" || f.Buf.CellAt(r.Max.X-1, y).Content != "┐" {
			t.Errorf("%d wide: the box's corners at %d, %d on row %d:\n%s", w, x, r.Max.X-1, y, f.String())
		}
	}
	feed(t, a, "<Esc>")
	a.run("query.new", 0)
	if c2 := queryOf(a.focused()); c2 == c || c2.schema != "public" {
		t.Fatalf("a new query: %q", c2.schema)
	}
	selectTab(a.focused(), 0)
	feed(t, a, "iselect * from pl")
	if c.comp == nil || c.comp.items[0].label != "planner" {
		t.Errorf("agentable's tables: %+v", c.comp)
	}
}

// A query with no schema yet, or one gone, takes the tree's when the
// catalog comes in (§8.6).
func TestQuerySchemaFromCatalog(t *testing.T) {
	a, c := inQuery(t, "", "")
	for _, was := range []string{"", "gone"} {
		c.schema = was
		a.Update(catalogMsg{conn: a.sess.Conns[0], schemas: []string{"agentable", "public"}, current: "public", tables: a.sess.Conns[0].Tables})
		if c.schema != "public" {
			t.Errorf("%q: %q", was, c.schema)
		}
	}
	c.schema = "agentable"
	if a.Update(catalogMsg{conn: a.sess.Conns[0], schemas: []string{"agentable", "public"}, current: "public", tables: a.sess.Conns[0].Tables}); c.schema != "agentable" {
		t.Errorf("its own: %q", c.schema)
	}
}

// The query's title as the window narrows: ▶ run stays longest, then
// the connection and the schema, together, then ↵ (§7.8); at 200 columns
// everything and the name.
func TestGoldenQueryTitle(t *testing.T) {
	var titles []string
	for _, w := range []int{200, 160, 130, 110, 100, 90, 80} {
		a, _ := inQuery(t, "", "")
		a.Update(tea.WindowSizeMsg{Width: w, Height: 20})
		r := a.layout()[a.focused().ID]
		titles = append(titles, ansi.Cut(strings.Split(a.render().String(), "\n")[r.Min.Y], r.Min.X, r.Max.X))
	}
	golden.RequireEqual(t, strings.Join(titles, "\n")+"\n")
}

// The query's candidates open under the word they complete (§9.7).
func TestGoldenQueryCompletion160x45(t *testing.T) {
	a, _ := inQuery(t, "", "")
	feed(t, a, "iselect 1;<CR>select * from t_o")
	golden.RequireEqual(t, a.render().String())
	a.icons = ui.ASCIIIcons // a letter each
	t.Run("ascii", func(t *testing.T) { golden.RequireEqual(t, a.render().String()) })
}

// What the query draws is the editor's state: the selection by kind,
// the command line.
func TestQueryView(t *testing.T) {
	a, c := inQuery(t, "", "")
	c.ed.Load("ab\nc\td")
	feed(t, a, "Gfd")
	view := func() ui.Query { v, _ := a.queryView(a.focused(), c); return v }
	if v := view(); v.Cursor != (ui.TextPos{Line: 1, Col: 2}) || v.CursorCol != 2 || !v.Normal || v.Sel.Mode != ui.SelNone || v.Prompt != "" {
		t.Errorf("NORMAL: %+v", v)
	}
	for _, s := range []struct {
		keys string
		want ui.Sel
	}{
		{"kv", ui.Sel{Mode: ui.SelChars, From: ui.TextPos{Line: 0, Col: 1}, To: ui.TextPos{Line: 0, Col: 1}}},
		{"<Esc>Vj", ui.Sel{Mode: ui.SelLines, From: ui.TextPos{Line: 0, Col: 1}, To: ui.TextPos{Line: 1, Col: 2}}},
		{"<Esc>gg<C-v>j", ui.Sel{Mode: ui.SelBlock, From: ui.TextPos{Line: 0, Col: 1}, To: ui.TextPos{Line: 1, Col: 2}, Left: 1, Right: 2}}, // b to d, past the tab
	} {
		feed(t, a, s.keys)
		if v := view(); v.Sel != s.want || v.Normal {
			t.Errorf("%s: %+v", s.keys, v.Sel)
		}
	}
	feed(t, a, "<Esc>:%s")
	if v := view(); v.Prompt != ":" || v.Text != "%s" || v.Pos != 2 {
		t.Errorf("command line: %+v", v)
	}
}

// query.rename names the focused query's file: it is written, then
// renamed, the tab's name, the state's key and the layout's `name` going
// with it; a name that is no file's, or one that another file or query
// has, is refused with the input left (§11, F8.11).
func TestQueryRename(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "iselect 1")
	if c.ver == c.saved {
		t.Fatal("nothing to write yet")
	}
	a.keepQuery(c)
	if _, ok := a.state.Queries["query_1"]; !ok {
		t.Fatal("the state knows query_1")
	}
	feed(t, a, "<Esc>")
	a.run("query.rename", 0)
	if a.askName == nil || a.askName.in.Text != "query_1" {
		t.Fatalf("not asking: %+v", a.askName)
	}
	if cmd, ok := a.newQuery(c.conn); !ok || cmd == nil { // query_2, taking the focus: back to the first
		t.Fatal("a second query")
	}
	a.askName.in = ui.Input{}
	feed(t, a, "query_2<CR>")
	if a.askName == nil || a.toast == "" || c.name != "query_1" {
		t.Errorf("another query open: asking %v, toast %q, %q", a.askName != nil, a.toast, c.name)
	}
	for _, bad := range []string{"a/b", ".hidden", `a\\b`} {
		a.askName.in = ui.Input{}
		if feed(t, a, bad+"<CR>"); a.askName == nil || c.name != "query_1" {
			t.Errorf("%q: asking %v, %q", bad, a.askName != nil, c.name)
		}
	}
	if err := config.WriteQuery(config.QueryPath("taken"), "x"); err != nil {
		t.Fatal(err)
	}
	a.askName.in = ui.Input{}
	if feed(t, a, "taken<CR>"); a.askName == nil || c.name != "query_1" {
		t.Errorf("a file of that name: asking %v, %q", a.askName != nil, c.name)
	}
	a.askName.in = ui.Input{}
	feed(t, a, "orders<CR>")
	if a.askName != nil || c.name != "orders" || c.path != config.QueryPath("orders") {
		t.Fatalf("renamed: asking %v, %q at %s", a.askName != nil, c.name, c.path)
	}
	if got, err := config.ReadQuery(config.QueryPath("orders")); err != nil || got != "select 1\n" {
		t.Errorf("the file went with it: %q %v", got, err)
	}
	if _, err := os.Stat(config.QueryPath("query_1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the old file is still there: %v", err)
	}
	if _, old := a.state.Queries["query_1"]; old || a.state.Queries["orders"].Conn == "" {
		t.Errorf("the state's key: %+v", a.state.Queries)
	}
	var tabs []string
	for _, p := range a.win().Root.Leaves() {
		for _, tb := range p.Tabs {
			if tb.Query == c {
				tabs = append(tabs, tb.Name)
			}
		}
	}
	if !slices.Equal(tabs, []string{"orders"}) {
		t.Errorf("the tab: %v", tabs)
	}
	l := a.layoutOf()
	var seen []string
	for _, w := range l.Sessions[0].Windows {
		for _, p := range w.Panes {
			for _, lt := range p.Tabs {
				if lt.Kind == "query" {
					seen = append(seen, lt.Name+"/"+strconv.Itoa(lt.N))
				}
			}
		}
	}
	if !slices.Contains(seen, "orders/0") {
		t.Errorf("the layout writes the name, no n: %v", seen)
	}
}

// Names that differ only in case are one file on some file systems (macOS,
// Windows): a query cannot take the name of another open, whether or not
// that one has a file yet; two new ones never get the same file; and its
// own file may change case (§11, F8.11).
func TestQueryRenameCase(t *testing.T) {
	ask := func(a *App, name string) {
		a.askName.in = ui.Input{}
		feed(t, a, name+"<CR>")
	}
	a, first := inQuery(t, "", "") // query_1: nothing typed, no file
	a.newQuery(first.conn)
	second := queryOf(a.focused())
	feed(t, a, "iselect 2<Esc>")
	if err := second.flush(); err != nil {
		t.Fatal(err)
	}
	a.run("query.rename", 0)
	if ask(a, "Query_1"); a.askName == nil || second.name != "query_2" {
		t.Errorf("another open query's name in other case: asking %v, %q", a.askName != nil, second.name)
	}
	if got, _ := config.ReadQuery(second.path); got != "select 2\n" {
		t.Errorf("its file: %q", got)
	}
	if ask(a, "Query_3"); a.askName != nil || second.name != "Query_3" {
		t.Fatalf("a free name: asking %v, %q", a.askName != nil, second.name)
	}
	for range 2 {
		if _, ok := a.newQuery(first.conn); !ok {
			t.Fatal("a new query")
		}
	}
	var names []string
	for _, c := range a.queries() {
		names = append(names, c.name)
	}
	slices.Sort(names)
	if want := []string{"Query_3", "query_1", "query_2", "query_4"}; !slices.Equal(names, want) {
		t.Errorf("new queries go round it: %v, want %v", names, want)
	}

	a, c := inQuery(t, "", "")
	feed(t, a, "iselect 1<Esc>")
	if err := c.flush(); err != nil {
		t.Fatal(err)
	}
	a.run("query.rename", 0)
	if ask(a, "Query_1"); a.askName != nil || c.name != "Query_1" {
		t.Fatalf("its own file's case: asking %v, %q", a.askName != nil, c.name)
	}
	var files []string
	es, _ := os.ReadDir(filepath.Dir(c.path))
	for _, e := range es {
		files = append(files, e.Name())
	}
	if !slices.Equal(files, []string{"Query_1.sql"}) {
		t.Errorf("files %v", files)
	}

	// a closed query reopens on the open one of its file, whatever the case
	a.newQuery(c.conn)
	a.run("tab.close", 0) // query_2, closed
	a.showPane(a.win().pane(2).ID)
	a.run("query.rename", 0)
	ask(a, "QUERY_2")
	n := len(a.queries())
	if a.run("tab.reopen", 0); len(a.queries()) != n {
		t.Errorf("reopened on a second query of the one file: %d queries, was %d", len(a.queries()), n)
	}
}

// A query's name is changed as it stands: its own name again is nothing,
// one never typed in has no file to move, and a file that cannot be moved
// leaves the name, the tab and the state as they were (F8.11).
func TestQueryRenameEdges(t *testing.T) {
	a, c := inQuery(t, "", "")
	a.run("query.rename", 0)
	a.toast = ""
	if feed(t, a, "<CR>"); a.askName != nil || a.toast != "" || c.name != "query_1" {
		t.Errorf("its own name: asking %v, toast %q, %q", a.askName != nil, a.toast, c.name)
	}
	a.keepQuery(c)
	a.run("query.rename", 0)
	a.askName.in = ui.Input{}
	if feed(t, a, "fresh<CR>"); a.askName != nil || c.name != "fresh" || a.state.Queries["fresh"].Conn == "" {
		t.Errorf("no file yet: asking %v, %q, state %+v", a.askName != nil, c.name, a.state.Queries)
	}
	if _, err := os.Stat(c.path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a file made out of nothing: %v", err)
	}

	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	feed(t, a, "iselect 1<Esc>")
	if err := c.flush(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(c.path)
	if runtime.GOOS == "windows" { // a readonly file at the target is what blocks a rename there
		if err := os.WriteFile(config.QueryPath("moved"), []byte("x"), 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(config.QueryPath("moved"), 0o600) })
	} else if err := os.Chmod(dir, 0o555); err != nil { // a file there can be written over, not moved
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // the temp dir goes with it
	a.run("query.rename", 0)
	a.askName.in = ui.Input{}
	a.toast = ""
	if feed(t, a, "moved<CR>"); a.askName == nil || a.toast == "" || c.name != "fresh" || c.path != config.QueryPath("fresh") {
		t.Errorf("the file stays: asking %v, toast %q, %q at %s", a.askName != nil, a.toast, c.name, c.path)
	}
	if _, ok := a.state.Queries["moved"]; ok || a.state.Queries["fresh"].Conn == "" || a.focused().tab().Name != "fresh" {
		t.Errorf("state %+v, tab %q", a.state.Queries, a.focused().tab().Name)
	}
}

// A click on a candidate takes it, with its space (§9.7, F8.1).
func TestQueryClickCandidate(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "iselect * from t_o")
	if c.comp == nil {
		t.Fatal("no list")
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindRow, I: 0}).Min)
	if text(c) != "select * from t_order " || c.comp != nil {
		t.Errorf("clicked: %q", text(c))
	}
}

// A list that comes up on its own with nothing typed has no item selected:
// ↵ is the editor's, Tab, C-n and ↓ select the first, S-Tab, C-p and ↑ the
// last; a letter typed selects the first as ever; a word accepted with its
// space has the next list come up at once; C-n's list and an INSERT's all
// columns are selected from the start (§9.7, F8.17).
func TestQueryNoSelectList(t *testing.T) {
	a, c := inQuery(t, "", "")
	cols := ordersCatalog(a)
	first, last := cols.Cols[0].Name, cols.Cols[len(cols.Cols)-1].Name
	const q = "<Esc>ggdGiselect * from t_order where"
	feed(t, a, q+"<CR>")
	if text(c) != "select * from t_order where " || c.comp == nil || c.comp.sel != -1 || len(c.comp.items) != len(cols.Cols) {
		t.Fatalf("where↵: %q, list %+v", text(c), c.comp)
	}
	if feed(t, a, "<CR>"); text(c) != "select * from t_order where \n" || c.comp != nil {
		t.Errorf("↵ with nothing selected: %q, list %v", text(c), c.comp != nil)
	}
	for keys, want := range map[string]string{
		"<Tab><CR>": first, "<C-n><CR>": first, "<Down><CR>": first,
		"<S-Tab><CR>": last, "<C-p><CR>": last, "<Up><CR>": last,
		"<Tab><Tab><CR>": cols.Cols[1].Name, "<S-Tab><S-Tab><CR>": cols.Cols[len(cols.Cols)-2].Name,
	} {
		feed(t, a, q+"<CR>"+keys)
		if text(c) != "select * from t_order where "+want+" " {
			t.Errorf("%s: %q, want %s", keys, text(c), want)
		}
	}
	feed(t, a, q+"<CR>p<CR>") // a letter selects the first, ↵ takes it
	if text(c) != "select * from t_order where paid " {
		t.Errorf("a letter: %q", text(c))
	}
	feed(t, a, "<Esc>ggdGiselect * from t_order <C-n>")
	if c.comp == nil || c.comp.sel != 0 {
		t.Errorf("C-n's list: %+v", c.comp)
	}
	feed(t, a, "<Esc>ggdGiinsert into t_order (")
	if c.comp == nil || c.comp.sel != 0 || len(c.comp.items) < 2 {
		t.Errorf("an INSERT's columns: %+v", c.comp)
	}
}

// The list with nothing selected has no row in the select color; Tab gives
// the first one (F8.17).
func TestQueryNoSelectListDraws(t *testing.T) {
	a, _ := inQuery(t, "", "")
	cols := ordersCatalog(a)
	feed(t, a, "iselect * from t_order where<CR>")
	selected := func() []int {
		f := a.render()
		var rows []int
		for i := range cols.Cols {
			r := find(t, a, ui.Target{Kind: ui.KindRow, I: i})
			if f.Buf.CellAt(r.Min.X+1, r.Min.Y).Style.Bg == a.theme.Select {
				rows = append(rows, i)
			}
		}
		return rows
	}
	if got := selected(); len(got) != 0 {
		t.Errorf("rows in the select color with nothing selected: %v", got)
	}
	feed(t, a, "<Tab>")
	if got := selected(); len(got) != 1 || got[0] != 0 {
		t.Errorf("after Tab: %v", got)
	}
}

// ordersCatalog gives the session's t_order the columns of ordersTable.
func ordersCatalog(a *App) db.Columns {
	table, cols, _ := ordersTable(0)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
	return cols
}

// The list with nothing selected, as it shows under the word (F8.17).
func TestGoldenQueryNoSelect160x45(t *testing.T) {
	a, _ := inQuery(t, "", "")
	ordersCatalog(a)
	feed(t, a, "iselect * from t_order where<CR>")
	golden.RequireEqual(t, a.render().String())
}

// A complete.accept the user bound, pressed with nothing selected, does
// nothing; once a row is selected it takes it (F8.17).
func TestAcceptKeyWithNothingSelected(t *testing.T) {
	a, c := inQuery(t, "", "[keys.complete]\n\"<C-y>\" = \"complete.accept\"\n")
	cols := ordersCatalog(a)
	feed(t, a, "iselect * from t_order where<CR><C-y>")
	if c.comp == nil || c.comp.sel != -1 || text(c) != "select * from t_order where " {
		t.Fatalf("nothing selected: %q, list %+v", text(c), c.comp)
	}
	if feed(t, a, "<Tab><C-y>"); text(c) != "select * from t_order where "+cols.Cols[0].Name+" " || c.comp != nil {
		t.Errorf("after Tab: %q", text(c))
	}
}

// What was picked stays picked when the columns of another table come in and
// the list is made again: ↵ still takes it (F8.17).
func TestAsyncColumnsKeepThePick(t *testing.T) {
	a, c := inQuery(t, "", "")
	ordersCatalog(a)
	var other db.Table
	for _, tb := range a.sess.Conns[0].Tables {
		if tb.Name != "t_order" && tb.Schema == "public" {
			other = tb
			break
		}
	}
	feed(t, a, "iselect * from t_order a join "+other.Name+" b on <Tab>")
	if c.comp == nil || c.comp.sel != 0 {
		t.Fatalf("the pick: %+v", c.comp)
	}
	pick := c.comp.items[0].label
	a.Update(colsMsg{a.sess.Conns[0], other, db.Columns{Cols: []db.Column{{Name: "zz_x", Type: "int"}}}, nil})
	if c.comp == nil || c.comp.sel < 0 || c.comp.items[c.comp.sel].label != pick {
		t.Fatalf("after the columns came: %+v", c.comp)
	}
	if feed(t, a, "<CR>"); !strings.HasSuffix(text(c), " on "+pick+" ") {
		t.Errorf("↵ took %q", text(c))
	}
}

// And the pick survives the columns of a second table coming after the first
// (F8.17).
func TestAsyncColumnsKeepThePickTwice(t *testing.T) {
	a, c := inQuery(t, "", "")
	ordersCatalog(a)
	var others []db.Table
	for _, tb := range a.sess.Conns[0].Tables {
		if tb.Name != "t_order" && tb.Schema == "public" {
			others = append(others, tb)
		}
	}
	feed(t, a, "iselect * from t_order a join "+others[0].Name+" b on b.x = a.id join "+others[1].Name+" c on <Tab><Tab>")
	if c.comp == nil || c.comp.sel != 1 {
		t.Fatalf("the pick: %+v", c.comp)
	}
	pick := c.comp.items[1].label
	for i, o := range others[:2] {
		a.Update(colsMsg{a.sess.Conns[0], o, db.Columns{Cols: []db.Column{{Name: "zz_" + strconv.Itoa(i), Type: "int"}}}, nil})
	}
	if c.comp == nil || c.comp.sel < 0 || c.comp.items[c.comp.sel].label != pick {
		t.Fatalf("the pick %q after two arrivals: %+v", pick, c.comp)
	}
	if feed(t, a, "<CR>"); !strings.HasSuffix(text(c), " on "+pick+" ") {
		t.Errorf("↵ took %q", text(c))
	}
}

// A candidate that starts with what is typed comes before one that has a word
// that does, and that before the fuzzy rest, whatever the group; at the start
// of a statement the keywords come before the tables and the columns, elsewhere
// the columns, the tables, the keywords stay in that order (§9.7, F12.6).
func TestSQLCompleteRank(t *testing.T) {
	a := sized(160, 45, "nerd")
	conn := a.sess.Conns[0]
	for _, name := range []string{"pap_secretary_problem_event", "oa_workflow_instance", "oa_workflow_instance_log", "selected_item"} {
		conn.Tables = append(conn.Tables, db.Table{Schema: "public", Name: name})
	}
	table, cols, _ := ordersTable(0)
	conn.cols[idOf(table)] = cols
	for _, c := range []struct{ sql, want string }{
		{"sele|", "select"},
		{"inse|", "insert into"},
		{"sec|", "pap_secretary_problem_event"}, // a word of it, before select's fuzzy s·e·c
		{"select t|", "表"},                      // not at the start: a table, before the keywords that start with t
		{"select * from t_order where sta|", "status"},
	} {
		pos := strings.Index(c.sql, "|")
		got, _ := a.sqlComplete(conn, strings.Replace(c.sql, "|", "", 1), pos, "public", map[tableID]bool{}, false)
		if got == nil || got.items[0].label != c.want && got.items[0].note != c.want {
			t.Errorf("%s: %+v, want %q first", c.sql, got.items[0], c.want)
		}
	}
	if got, want := []int{tierOf("sec", "pap_secretary_problem_event"), tierOf("sec", "secret"), tierOf("sec", "select"), tierOf("", "x")}, []int{2, 1, 3, 3}; !slices.Equal(got, want) {
		t.Errorf("tiers %v, want %v", got, want)
	}
}

// Two programs on one data directory both start on query_1: what the one
// wrote is not written over by the other without the other being told, once;
// its next save is the yes. An autosave never overwrites, and says it once
// (F13.2).
func TestQueryFileChangedOutside(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sizedWith(160, 45, config.Default())
	feed(t, a, "<C-l>")
	ca := queryOf(a.focused())
	b := sizedWith(160, 45, config.Default())
	feed(t, b, "<C-l>")
	cb := queryOf(b.focused())
	if ca.path != cb.path {
		t.Fatalf("not one file: %s %s", ca.path, cb.path)
	}
	feed(t, a, "iselect 'written in A';<Esc>:w<CR>")
	feed(t, b, "iselect 'written in B';<Esc>")
	// B's autosave: refused and said, the file as A left it; said once
	_, cmd := b.Update(autosave{cb, cb.ver})
	if cmd == nil || !strings.Contains(b.toast, "别处改过") {
		t.Errorf("autosave: toast %q", b.toast)
	}
	b.toast = ""
	if _, cmd := b.Update(autosave{cb, cb.ver}); cmd != nil || b.toast != "" {
		t.Errorf("an autosave says it again: toast %q", b.toast)
	}
	if data, _ := os.ReadFile(ca.path); string(data) != "select 'written in A';\n" {
		t.Fatalf("B's autosave wrote: %q", data)
	}
	feed(t, b, ":w<CR>") // the autosave asked: this is the yes
	if data, _ := os.ReadFile(ca.path); !strings.Contains(string(data), "select 'written in B';") {
		t.Fatalf("B's :w after the autosave said it: %q, toast %q", data, b.toast)
	}
	// a file changed again after it: an autosave still does not write, a :w is
	// refused once, the next is the yes
	if err := os.WriteFile(ca.path, []byte("a third\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	feed(t, b, "ox<Esc>")
	b.Update(autosave{cb, cb.ver})
	if data, _ := os.ReadFile(ca.path); string(data) != "a third\n" {
		t.Fatalf("an autosave wrote over it: %q", data)
	}
	if err := os.WriteFile(ca.path, []byte("a fourth\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.toast = ""
	feed(t, b, ":w<CR>")
	if data, _ := os.ReadFile(ca.path); string(data) != "a fourth\n" || !strings.Contains(b.toast, "再保存一次会覆盖") {
		t.Fatalf("B's :w: file %q, toast %q", data, b.toast)
	}
	feed(t, b, ":w<CR>") // the yes
	if data, _ := os.ReadFile(ca.path); !strings.Contains(string(data), "x") || strings.Contains(string(data), "fourth") {
		t.Errorf("B's second :w: %q", data)
	}
	feed(t, b, "dd") // back to what it was typing
	// and it is B's file again: a change in it is written as it goes
	feed(t, b, "ox<Esc>:w<CR>")
	if data, _ := os.ReadFile(ca.path); !strings.Contains(string(data), "x") {
		t.Errorf("after the yes: %q", data)
	}
	// the file is read again after an external editor: no refusal
	if err := os.WriteFile(ca.path, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.gotExternal(externalDone{t: ca})
	feed(t, a, "Ax<Esc>:w<CR>")
	if data, _ := os.ReadFile(ca.path); !strings.Contains(string(data), "outside") || !strings.Contains(string(data), "x") {
		t.Errorf("after an external edit: %q", data)
	}
}

// A file that was there before the start is written by the first :w, as the
// stamp was taken at the read (F13.2).
func TestQueryExistingFileSaves(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.WriteQuery(config.QueryPath("query_1"), "select 0;\n"); err != nil {
		t.Fatal(err)
	}
	a := sizedWith(160, 45, config.Default())
	feed(t, a, "<C-l>")
	c := queryOf(a.focused())
	if c == nil || text(c) != "select 0;" {
		t.Fatal("not on query_1 with its text")
	}
	feed(t, a, "oselect 1;<Esc>:w<CR>")
	if data, _ := os.ReadFile(c.path); string(data) != "select 0;\nselect 1;\n" {
		t.Errorf("the first :w of an existing file: %q, toast %q", data, a.toast)
	}
}

// A rename moves the file and keeps its stamp: a save after it is no
// refusal; a told autosave is told again after a write that the user said yes
// to; an autosave writes a file that is gone (F13.2).
func TestQueryFileStampKept(t *testing.T) {
	a, c := inQuery(t, "", "")
	feed(t, a, "iselect 1;<Esc>:w<CR>")
	a.run("query.rename", 0)
	a.askName.in.Text, a.askName.in.Pos = "", 0
	feed(t, a, "renamed<CR>")
	if c.name != "renamed" {
		t.Fatalf("not renamed: %q toast %q", c.name, a.toast)
	}
	feed(t, a, "oselect 2;<Esc>:w<CR>")
	if data, _ := os.ReadFile(c.path); string(data) != "select 1;\nselect 2;\n" {
		t.Errorf("after a rename: %q toast %q", data, a.toast)
	}
	// told, then written over, then changed outside again: told again
	_ = os.WriteFile(c.path, []byte("outside\n"), 0o600)
	feed(t, a, "ox<Esc>")
	a.Update(autosave{c, c.ver})
	feed(t, a, ":w<CR>")
	if data, _ := os.ReadFile(c.path); strings.Contains(string(data), "outside") {
		t.Fatalf("not overwritten: %q", data)
	}
	_ = os.WriteFile(c.path, []byte("outside again\n"), 0o600)
	feed(t, a, "oy<Esc>")
	a.toast = ""
	a.Update(autosave{c, c.ver})
	if !strings.Contains(a.toast, "别处改过") {
		t.Errorf("a second change outside is not said by the autosave: %q", a.toast)
	}
	// the file gone: written again
	_ = os.Remove(c.path)
	feed(t, a, "oz<Esc>")
	a.Update(autosave{c, c.ver})
	if data, err := os.ReadFile(c.path); err != nil || !strings.Contains(string(data), "z") {
		t.Errorf("a deleted file, autosaved: %q %v", data, err)
	}
}

// An external editor's return takes the file as it is: what an autosave said of
// a file changed outside is said again when it changes again (F13.2).
func TestExternalEditorResetsTold(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sizedWith(160, 45, config.Default())
	feed(t, a, "<C-l>")
	c := queryOf(a.focused())
	feed(t, a, "iselect 1;<Esc>:w<CR>")
	if err := os.WriteFile(c.path, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	feed(t, a, "ox<Esc>")
	a.Update(autosave{c, c.ver})
	if !strings.Contains(a.toast, "别处改过") {
		t.Fatalf("the autosave: %q", a.toast)
	}
	a.gotExternal(externalDone{t: c}) // the editor was open: its file is what the tab has now
	if err := os.WriteFile(c.path, []byte("outside again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	feed(t, a, "oy<Esc>")
	a.toast = ""
	a.Update(autosave{c, c.ver})
	if !strings.Contains(a.toast, "别处改过") {
		t.Errorf("said once, then the editor, then changed again: %q", a.toast)
	}
}
