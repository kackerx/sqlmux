package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// queryFile writes a query file with text in it.
func queryFile(t *testing.T, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(config.QueryPath(name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteQuery(config.QueryPath(name), text); err != nil {
		t.Fatal(err)
	}
}

// queryNode puts the tree cursor on the query file called name.
func queryNode(t *testing.T, a *App, name string) {
	t.Helper()
	ns, _ := a.treeNodes()
	i := slices.IndexFunc(ns, func(n node) bool { return n.id == "queries/"+name })
	if i < 0 {
		t.Fatalf("no query %s in\n%s", name, treeTexts(a))
	}
	a.win().focus(0)
	a.treeGo(i)
}

// queryNames is the names under the Queries root.
func queryNames(a *App) (names []string) {
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind == nodeQueryFile {
			names = append(names, n.query)
		}
	}
	return names
}

// A new query is the first number with no file and no open query: a
// closed one keeps its file, and its number, till the file is deleted; one
// open that has no file yet keeps its number too (F8.21).
func TestNewQueryNumbers(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	for _, n := range []string{"query_1", "query_2", "query_3"} {
		queryFile(t, n, n+"\n")
	}
	if got := a.freeQuery(); got != "query_4" {
		t.Errorf("with query_1 to _3: %s", got)
	}
	if err := os.Remove(config.QueryPath("query_2")); err != nil {
		t.Fatal(err)
	}
	if got := a.freeQuery(); got != "query_2" {
		t.Errorf("query_2 deleted: %s", got)
	}
	// query_1 is open, as it is at start, query_2 too and with no file
	a.run("query.new", 0)
	if got := queryOf(a.focused()).name; got != "query_2" {
		t.Fatalf("query.new: %s", got)
	}
	if got := a.freeQuery(); got != "query_4" {
		t.Errorf("query_2 open with no file: %s", got)
	}
	if text := strings.Join(queryOf(a.focused()).ed.Lines(), ""); text != "" {
		t.Errorf("the new query_2 is not new: %q", text)
	}
}

// The Queries root lists the files by number and the open ones with none
// yet, the connection last used at the right, those open in focus color; ↵
// goes to one open, and opens one not, on the connection it was on; a file
// is listed once it is written, and a name once it is changed (F8.21).
func TestQueriesNode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	queryFile(t, "query_10", "ten\n")
	queryFile(t, "query_2", "two\n")
	queryFile(t, "scratch", "select 1\n")
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "doraemon", Schema: "agentable"}}
	if got := queryNames(a); !slices.Equal(got, []string{"query_1", "query_2", "query_10", "scratch"}) {
		t.Fatalf("listed: %v", got)
	}
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind != nodeQueryFile {
			continue
		}
		if want := n.query == "query_1"; n.Current != want {
			t.Errorf("%s: open is %v", n.query, n.Current)
		}
		if want := map[string]string{"query_2": "doraemon"}[n.query]; n.Note != want {
			t.Errorf("%s: connection %q", n.query, n.Note)
		}
	}
	// ↵ on one that is not open: a tab of the data pane, on its connection and schema
	queryNode(t, a, "query_2")
	feed(t, a, "<CR>")
	c := queryOf(a.focused())
	if c == nil || c.name != "query_2" || text(c) != "two" || c.conn != a.sess.Conns[0] || c.schema != "agentable" {
		t.Fatalf("↵ on query_2: %+v", c)
	}
	if got := len(a.queries()); got != 2 {
		t.Errorf("%d queries open", got)
	}
	// ↵ on one open goes to it
	a.showPane(1)
	queryNode(t, a, "query_2")
	feed(t, a, "<CR>")
	if c2 := queryOf(a.focused()); c2 != c || len(a.queries()) != 2 {
		t.Errorf("↵ on an open query: %v, %d open", c2, len(a.queries()))
	}
	// a query with no file is listed while it is open, and once it is
	// written, closed: the directory is read again after its first write
	a.run("query.new", 0)
	n := queryOf(a.focused())
	if !slices.Contains(queryNames(a), n.name) || config.QueryExists(n.name) {
		t.Fatalf("%s open, with no file: %v", n.name, queryNames(a))
	}
	a.queryDid(n, n.ed.Load("select 2"))
	if err := n.flush(); err != nil {
		t.Fatal(err)
	}
	a.run("tab.close", 0)
	if len(a.queries()) != 2 || slices.ContainsFunc(a.queries(), func(c *queryTab) bool { return c.name == n.name }) {
		t.Fatalf("%s not closed: %d open", n.name, len(a.queries()))
	}
	if got := queryNames(a); !slices.Contains(got, n.name) {
		t.Errorf("after a write and a close: %v", got)
	}
}

// ↵ on a query not open opens it on the connection state.json says it was
// on; when that is no connection of the session, on the current one (F8.21).
func TestOpenQueryFileConnection(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	other := readyConn("other")
	a.sess.Conns = append(a.sess.Conns, other)
	queryFile(t, "query_2", "two\n")
	queryFile(t, "query_3", "three\n")
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "other"}, "query_3": {Conn: "gone"}}
	for _, c := range []struct { // in this order: the current connection is the one the focused tab is on
		name string
		want *Conn
	}{{"query_3", a.sess.Conns[0]}, {"query_2", other}} {
		queryNode(t, a, c.name)
		feed(t, a, "<CR>")
		if got := queryOf(a.focused()); got == nil || got.name != c.name || got.conn != c.want {
			t.Errorf("%s: opened on %v, want %s", c.name, got, c.want.Spec.Name)
		}
	}
}

// A file that cannot be deleted says so, and keeps its file and what
// state.json has of it (F8.21).
func TestDeleteQueryFileFails(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if os.Geteuid() == 0 {
		t.Skip("root deletes in a read-only directory")
	}
	a := sized(160, 45, "nerd")
	queryFile(t, "query_2", "two\n")
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "doraemon"}}
	queryNode(t, a, "query_2")
	blockDelete(t, config.QueryPath("query_2"))
	feed(t, a, "dd<CR>")
	if a.toast == "" || !config.QueryExists("query_2") || a.state.Queries["query_2"].Conn != "doraemon" {
		t.Errorf("toast %q, file %v, state %v", a.toast, config.QueryExists("query_2"), a.state.Queries)
	}
}

// tree.delete on a query file asks first, then deletes the file and what
// state.json kept of it, and the number is free again; on any other node it
// does nothing (F8.21).
func TestDeleteQueryFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	queryFile(t, "query_2", "two\n")
	queryFile(t, "query_3", "three\n")
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "doraemon"}, "query_3": {Conn: "doraemon"}}
	queryNode(t, a, "query_2")
	feed(t, a, "dd")
	if a.confirm == nil || a.confirm.text != "删除 query_2？" || !config.QueryExists("query_2") {
		t.Fatalf("asked: %v", a.confirm)
	}
	feed(t, a, "<Esc>")
	if a.confirm != nil || !config.QueryExists("query_2") {
		t.Fatalf("no: asked %v, file %v", a.confirm, config.QueryExists("query_2"))
	}
	feed(t, a, "dd<CR>")
	if config.QueryExists("query_2") || config.QueryExists("query_3") != true || slices.Contains(queryNames(a), "query_2") {
		t.Errorf("deleted: %v", queryNames(a))
	}
	if _, kept := a.state.Queries["query_2"]; kept || a.state.Queries["query_3"].Conn != "doraemon" {
		t.Errorf("state: %v", a.state.Queries)
	}
	if got := a.freeQuery(); got != "query_2" {
		t.Errorf("the number is free: %s", got)
	}
	// another node: nothing
	a.toast = ""
	treeTo(t, a, "public")
	if feed(t, a, "dd"); a.confirm != nil || a.toast != "" {
		t.Errorf("a schema: asked %v, toast %q", a.confirm, a.toast)
	}
}

// A query that is open is deleted all the same, after the question: the
// file, what state.json kept, and its tabs in every session, the pane they
// leave empty; text not written goes with it, and an autosave still to come
// writes the file no more; one with no file yet is deleted as well (F10.10).
func TestDeleteOpenQuery(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, report := withSessions()
	other := report.queries()[0]
	other.name = "Query_1" // the same file, open in the other session as well
	c := queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	feed(t, a, "iselect 1<Esc>")
	if c.name != "query_1" || c.ver == c.saved {
		t.Fatalf("typed in %s: ver %d saved %d", c.name, c.ver, c.saved)
	}
	queryFile(t, "query_1", "select 0\n")
	a.state.Queries = map[string]config.QueryState{"query_1": {Conn: "doraemon"}}
	queryNode(t, a, "query_1")
	feed(t, a, "dd")
	if a.confirm == nil || a.confirm.text != "删除 query_1？" || a.toast != "" {
		t.Fatalf("asked: %v, toast %q", a.confirm, a.toast)
	}
	feed(t, a, "<Esc>")
	if len(a.sess.queries()) != 1 || !config.QueryExists("query_1") {
		t.Fatal("no: it stays")
	}
	pane := a.win().Root.B.Pane
	feed(t, a, "dd<CR>")
	if len(a.sess.queries()) != 0 || len(report.queries()) != 0 || len(pane.Tabs) != 0 {
		t.Errorf("tabs left: %d here, %d there, %d in its pane", len(a.sess.queries()), len(report.queries()), len(pane.Tabs))
	}
	if config.QueryExists("query_1") || a.state.Queries["query_1"].Conn != "" {
		t.Errorf("file %v, state %v", config.QueryExists("query_1"), a.state.Queries)
	}
	a.Update(autosave{t: c, ver: c.ver}) // what was typed does not bring the file back
	if config.QueryExists("query_1") || a.toast != "" {
		t.Errorf("an autosave after: file %v, toast %q", config.QueryExists("query_1"), a.toast)
	}
	// open and with no file yet: nothing to remove, the tab goes
	a.openQueryFile("query_7")
	d := a.queries()[slices.IndexFunc(a.queries(), func(c *queryTab) bool { return c.name == "query_7" })]
	queryNode(t, a, "query_7")
	if feed(t, a, "dd<CR>"); a.toast != "" || slices.Contains(a.queries(), d) {
		t.Errorf("no file: toast %q, open %v", a.toast, slices.Contains(a.queries(), d))
	}
}

// The last query on a connection in a transaction, deleted, asks as a close
// does and rolls it back; one running is not deleted; a query closed and
// deleted is not reopened (F10.10).
func TestDeleteOpenQueryAsks(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, c := inQuery(t, "", "")
	main := &saveDB{}
	c.conn.Main, c.conn.tx = db.NewWorker(main), 'T'
	queryFile(t, "query_1", "x\n")
	queryNode(t, a, "query_1")
	feed(t, a, "dd<CR>")
	if a.confirm == nil || a.confirm.text != "doraemon 有未提交的事务，关闭会回滚。" || !config.QueryExists("query_1") || len(a.sess.queries()) != 1 {
		t.Fatalf("a transaction left: %+v", a.confirm)
	}
	if feed(t, a, "<Esc>"); !config.QueryExists("query_1") || len(a.sess.queries()) != 1 {
		t.Fatal("no: the file and the tab stay")
	}
	queryNode(t, a, "query_1")
	feed(t, a, "dd<CR>y")
	if config.QueryExists("query_1") || len(a.sess.queries()) != 0 || !slices.Contains(main.sqls, "rollback []") {
		t.Errorf("y: file %v, %d queries, sent %q", config.QueryExists("query_1"), len(a.sess.queries()), main.sqls)
	}

	a, c = inQuery(t, "", "")
	queryFile(t, "query_1", "x\n")
	c.running = &run{}
	queryNode(t, a, "query_1")
	feed(t, a, "dd<CR>")
	if a.toast != "正在执行，结束后再关闭" || !config.QueryExists("query_1") || len(a.sess.queries()) != 1 {
		t.Errorf("running: toast %q, file %v, %d queries", a.toast, config.QueryExists("query_1"), len(a.sess.queries()))
	}

	a, _ = inQuery(t, "", "")
	a.openQueryFile("query_5")
	n := slices.IndexFunc(a.queries(), func(c *queryTab) bool { return c.name == "query_5" })
	a.closeTab(a.win().Root.B.Pane, n) // closed: tab.reopen goes back to it
	queryFile(t, "query_5", "five\n")
	queryNode(t, a, "query_5")
	feed(t, a, "dd<CR>")
	a.run("tab.reopen", 0)
	if slices.ContainsFunc(a.queries(), func(c *queryTab) bool { return c.name == "query_5" }) || config.QueryExists("query_5") {
		t.Error("a deleted query is brought back by tab.reopen")
	}
}

// moreQuery opens query name in session s on its first query's connection.
func moreQuery(t *testing.T, s *Session, name string) *queryTab {
	t.Helper()
	cons, err := openQuery(s.queries()[0].conn, name)
	if err != nil {
		t.Fatal(err)
	}
	putTab(s.Windows[0].pane(2), Tab{Name: name, Query: cons}, false)
	return cons
}

// A session has connections of its own: deleting a query of another session
// asks about its transaction only when that was the connection's last query
// there, and rolls it back then, not while another query of that session is
// on it; a query of another session that is running, in any case of its
// name, keeps the delete from happening (F10.10).
func TestDeleteQueryOfAnotherSession(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a, report := withSessions()
	first := report.queries()[0]
	first.name = "query_77"
	main := &saveDB{}
	first.conn.Main, first.conn.tx = db.NewWorker(main), 'T'
	moreQuery(t, report, "query_88")
	queryNode(t, a, "query_77")
	feed(t, a, "dd<CR>")
	if a.confirm != nil || slices.ContainsFunc(report.queries(), func(c *queryTab) bool { return c.name == "query_77" }) || slices.Contains(main.sqls, "rollback []") {
		t.Fatalf("another query on the connection: asked %+v, rolled back %q", a.confirm, main.sqls)
	}
	queryNode(t, a, "query_88") // the last of them
	feed(t, a, "dd<CR>")
	if a.confirm == nil || !strings.Contains(a.confirm.text, "有未提交的事务") || len(report.queries()) != 1 {
		t.Fatalf("the last one asks: %+v", a.confirm)
	}
	feed(t, a, "y")
	if len(report.queries()) != 0 || !slices.Contains(main.sqls, "rollback []") {
		t.Errorf("y: %d queries, sent %q", len(report.queries()), main.sqls)
	}

	// running there, as Query_1: the file called query_1 stays
	a, report = withSessions()
	r := report.queries()[0]
	r.name, r.running = "Query_1", &run{}
	queryFile(t, "query_1", "x\n")
	queryNode(t, a, "query_1")
	feed(t, a, "dd<CR>")
	if a.toast != "正在执行，结束后再关闭" || !config.QueryExists("query_1") || len(report.queries()) != 1 {
		t.Errorf("running elsewhere: toast %q, file %v, %d queries", a.toast, config.QueryExists("query_1"), len(report.queries()))
	}
}

// A file that cannot be deleted keeps its tab, and the transaction of its
// query's connection is not rolled back for it (F10.10).
func TestDeleteQueryFailsKeepsTransaction(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if os.Geteuid() == 0 {
		t.Skip("root deletes in a read-only directory")
	}
	a, report := withSessions()
	first := report.queries()[0]
	first.name = "query_77"
	main := &saveDB{}
	first.conn.Main, first.conn.tx = db.NewWorker(main), 'T'
	queryFile(t, "query_77", "x\n")
	queryNode(t, a, "query_77")
	blockDelete(t, config.QueryPath("query_77"))
	feed(t, a, "dd<CR>y")
	if a.toast == "" || len(report.queries()) != 1 || slices.Contains(main.sqls, "rollback []") || !config.QueryExists("query_77") {
		t.Errorf("toast %q, %d queries, sent %q", a.toast, len(report.queries()), main.sqls)
	}
}

// The Queries node: one open, one closed, one renamed (F8.21).
func TestGoldenQueriesNode(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	queryFile(t, "query_2", "two\n")
	queryFile(t, "query_3", "three\n")
	queryFile(t, "report", "select 1\n")
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "doraemon"}, "report": {Conn: "doraemon"}}
	queryNode(t, a, "query_3")
	a.sess.open["workspace"] = false
	golden.RequireEqual(t, a.render().String())
}

// A data directory with only consoles/console_2.sql, as a start of before
// F10.11 left it: once migrated, queries/query_2.sql is there and the Queries
// node lists it (F10.11).
func TestQueriesAfterMigration(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(config.DataDir(), "consoles"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.DataDir(), "consoles", "console_2.sql"), []byte("select 2;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.MigrateConsoles(); err != nil {
		t.Fatal(err)
	}
	a := sized(160, 45, "nerd")
	if _, err := os.Stat(config.QueryPath("query_2")); err != nil {
		t.Error(err)
	}
	if got := queryNames(a); !slices.Contains(got, "query_2") {
		t.Errorf("the Queries node lists %v", got)
	}
}

// The palette's 表 and 所有 scopes list the queries of the Queries node after
// the tables, with the connection state.json has of them; ↵ opens one that is
// not open, as it does in the tree; the preview is the file's first rows, none
// for an open query that has no file (F12.10).
func TestPaletteQueries(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	queryFile(t, "query_2", "select 1 -- one\nfrom t\n")
	a.state.Queries = map[string]config.QueryState{"query_2": {Conn: "doraemon"}}
	feed(t, a, "<C-p>@que")
	rows := a.paletteView().Rows
	if len(rows) != 2 || rows[0].Name != "query_1" || rows[1].Name != "query_2" || rows[1].Where != "doraemon" || rows[1].Tag != "Query" {
		t.Fatalf("@que: %+v", rows)
	}
	if a.palettePreview() != nil {
		t.Error("query_1 has no file: no preview")
	}
	feed(t, a, "<C-n>")
	if pv := a.palettePreview(); pv == nil || pv.Text != "select 1 -- one\nfrom t" {
		t.Errorf("preview %+v", pv)
	}
	feed(t, a, "<CR>")
	if q := queryOf(a.focused()); a.palette != nil || q == nil || q.name != "query_2" {
		t.Errorf("↵: palette %v, focused query %v", a.palette != nil, q)
	}
	feed(t, a, "<C-p>query_1") // 所有 has them too, after the open tabs
	if rows := a.paletteView().Rows; !slices.ContainsFunc(rows, func(r ui.PaletteRow) bool { return r.Tag == "Query" && r.Name == "query_1" }) {
		t.Errorf("in 所有: %+v", rows)
	}
	feed(t, a, "<Esc><C-p>@query_1")
	feed(t, a, "<CR>") // open: gone to, not opened again
	if q := queryOf(a.focused()); q == nil || q.name != "query_1" || len(a.queries()) != 2 {
		t.Errorf("↵ on an open query: %v, %d open", q, len(a.queries()))
	}
}

// The preview of a query is its first 100 rows and a few KB of them, ends in no
// newline, is colored by the catalog of the connection the query opens on, not
// the current one's, scrolls, and is at the top again for another query; a
// table of the same score is before the query (F12.10).
func TestPaletteQueryPreview(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, seed := withConns()
	seed.Tables = append(seed.Tables, db.Table{Schema: "public", Name: "only_in_seed"})
	var long []string
	for i := range 300 {
		long = append(long, "select "+strconv.Itoa(i)+";")
	}
	queryFile(t, "q_long", strings.Join(long, "\n")+"\n")
	queryFile(t, "q_long2", strings.Join(long, "\n")+"\n")
	queryFile(t, "q_wide", strings.Repeat("中", 20000))
	queryFile(t, "q_seed", "select * from only_in_seed\n")
	queryFile(t, "agent", "select 1\n")
	a.state.Queries = map[string]config.QueryState{"q_seed": {Conn: "seed"}, "agent": {Conn: "doraemon"}}
	a.run("palette.open", 0)
	pick := func(name string) *ui.PalettePreview {
		t.Helper()
		a.palette.input.Text, a.palette.input.Pos = "@"+name, len(name)+1
		a.palette.sel = 0
		items, ms := a.paletteMatches()
		for i, m := range ms {
			if items[m.Index].kind == itemQuery && items[m.Index].id == name {
				a.palette.sel = i
				a.wantDDL()
				return a.palettePreview()
			}
		}
		t.Fatalf("no query %s", name)
		return nil
	}
	if pv := pick("q_long"); pv == nil || strings.Count(pv.Text, "\n")+1 != queryPreviewRows || strings.HasSuffix(pv.Text, "\n") {
		t.Errorf("the rows: %+v", pv)
	}
	if pv := pick("q_wide"); pv == nil || len(pv.Text) > queryPreviewBytes || !utf8.ValidString(pv.Text) || len(pv.Text) < queryPreviewBytes-3 {
		t.Errorf("one long line: %d bytes", len(pv.Text))
	}
	if pv := pick("q_seed"); pv == nil || pv.Names(14, "only_in_seed") != ui.TableName {
		t.Errorf("the catalog of the connection it opens on: %+v", pv)
	}
	// scrolled, then another query: the top
	pick("q_long")
	a.palette.pvTop = 5
	if pv := a.palettePreview(); pv == nil || pv.Top != 5 {
		t.Errorf("scrolled: %+v", pv)
	}
	pick("q_long2")
	if a.palette.pvTop != 0 {
		t.Errorf("another query: the preview is at row %d", a.palette.pvTop)
	}
	pick("q_long")
	if a.palette.pvTop != 0 || a.palettePreview().Top != 0 {
		t.Errorf("back: row %d", a.palette.pvTop)
	}
	// a table and a query of one name, one score: the table first
	a.palette.input.Text, a.palette.input.Pos = "@agent", 6
	var tags []string
	for _, r := range a.paletteView().Rows {
		if r.Name == "agent" {
			tags = append(tags, r.Tag)
		}
	}
	if !slices.Equal(tags, []string{"表", "Query"}) {
		t.Errorf("the same name: %v", tags)
	}
	// the enter hint and the icon
	a.palette.input.Text, a.palette.input.Pos = "@q_seed", 7
	a.palette.sel = 0
	if r := a.paletteView().Rows[0]; r.Icon != a.icons.Query || !strings.Contains(enterOf(a), "打开") {
		t.Errorf("row %+v, enter %q", r, enterOf(a))
	}
}

// The preview colors a query by the schema state.json says it was in on the
// connection it says it was on, by the current connection's when none is kept,
// and a connection that is gone keeps no schema (F12.10).
func TestPaletteQueryPreviewPlace(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, seed := withConns()
	other := db.Table{Schema: "other", Name: "o_tab"}
	seed.Tables = append(seed.Tables, other)
	seed.cols[idOf(other)] = db.Columns{Cols: []db.Column{{Name: "only_col"}}}
	queryFile(t, "q_other", "select only_col from o_tab\n")
	queryFile(t, "q_public", "select only_col from o_tab\n")
	queryFile(t, "q_none", "select * from agent\n")
	queryFile(t, "q_gone", "select 1\n")
	a.state.Queries = map[string]config.QueryState{
		"q_other":  {Conn: "seed", Schema: "other"},
		"q_public": {Conn: "seed", Schema: "public"},
		"q_gone":   {Conn: "gone", Schema: "other"},
	}
	a.run("palette.open", 0)
	pick := func(name string) *ui.PalettePreview {
		t.Helper()
		a.palette.input.Text, a.palette.input.Pos = "@"+name, len(name)+1
		items, ms := a.paletteMatches()
		for i, m := range ms {
			if items[m.Index].kind == itemQuery && items[m.Index].id == name {
				a.palette.sel = i
				return a.palettePreview()
			}
		}
		t.Fatalf("no query %s", name)
		return nil
	}
	if pv := pick("q_other"); pv == nil || pv.Names(7, "only_col") != ui.ColumnName {
		t.Errorf("in the schema it was in: %+v", pv)
	}
	if pv := pick("q_public"); pv == nil || pv.Names(7, "only_col") == ui.ColumnName {
		t.Errorf("in another schema: the column is not its table's: %+v", pv)
	}
	if pv := pick("q_none"); pv == nil || pv.Names(14, "agent") != ui.TableName {
		t.Errorf("no connection kept, the current one's: %+v", pv)
	}
	cur := a.sess.Conns[0]
	if c, schema := a.queryPlace("q_gone", cur); c != cur || schema != cur.Schema {
		t.Errorf("a connection that is gone: %v %q", c.Spec.Name, schema)
	}
	if c, schema := a.queryPlace("q_other", cur); c != seed || schema != "other" {
		t.Errorf("a connection kept: %v %q", c.Spec.Name, schema)
	}
	if c, schema := a.queryPlace("q_none", nil); c != nil || schema != "" {
		t.Errorf("no connection at all: %v %q", c, schema)
	}
}
