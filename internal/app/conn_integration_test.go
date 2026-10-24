//go:build integration

package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/mysql"
	"github.com/kackerx/sqlmux/internal/db/postgres"
	"github.com/kackerx/sqlmux/internal/db/sqlite"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// settle delivers what connecting, the catalog, a run, a page, a save and a
// commit or a rollback bring back, and what those bring in turn; the rest
// is dropped.
func settle(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			settle(a, c)
		}
	case connected, catalogMsg, keysMsg, cmdsMsg, foundMsg, filterDue, keyDone, keyExists, namespaceFound, sessionDialed, runDone, pageDone, txDone, pageMsg, countMsg, saveMsg, dropped:
		_, next := a.Update(m)
		settle(a, next)
	}
}

// engineDSNs is each engine's seeded database the tests run against, as
// SQLMUX_TEST_PG and SQLMUX_TEST_MYSQL name them, and a file SQLite's, which
// needs no server; a test with only that one runs on it alone.
func engineDSNs(t *testing.T) map[string]string {
	dsns := map[string]string{"sqlite": sqlite.OwnDB(t)}
	if os.Getenv("SQLMUX_TEST_PG") != "" {
		dsns["postgres"] = postgres.IntegrationDSN(t) // the user's PG* variables out of the way
	}
	if os.Getenv("SQLMUX_TEST_MYSQL") != "" {
		dsns["mysql"] = mysql.IntegrationDSN(t)
	}
	return dsns
}

// eachEngine runs test on each engine's seeded database (engineDSNs).
func eachEngine(t *testing.T, test func(t *testing.T, eng, dsn string)) {
	for eng, dsn := range engineDSNs(t) {
		t.Run(eng, func(t *testing.T) { test(t, eng, dsn) })
	}
}

// eachServer is eachEngine but for SQLite, which is a file: no server to lose,
// no other database, no session of another's.
func eachServer(t *testing.T, test func(t *testing.T, eng, dsn string)) {
	eachEngine(t, func(t *testing.T, eng, dsn string) {
		if eng == "sqlite" {
			t.Skip("a file has no server")
		}
		test(t, eng, dsn)
	})
}

// ownDB is a database of the test's own on eng's server, for tests that
// write (AGENTS.md「集成测试环境」).
func ownDB(t *testing.T, eng string) string {
	switch eng {
	case "mysql":
		return mysql.OwnDB(t)
	case "sqlite":
		return sqlite.OwnDB(t)
	}
	return postgres.OwnDB(t)
}

// Only the connection started on connects; another connects once opened
// in the tree, open on its current_schema(), MySQL's database(); one that
// cannot is marked failed, named in a toast, the others as they were
// (§7.8「连接节点」).
func TestConnectOnOpen(t *testing.T) { eachEngine(t, connectOnOpen) }

func connectOnOpen(t *testing.T, eng, dsn string) {
	sess := openUp(t, []config.Connection{
		{Name: "play", Engine: eng, DSN: dsn},
		{Name: "seed", Engine: eng, DSN: dsn},
		{Name: "broken", Engine: "postgres", DSN: "postgres://nobody@127.0.0.1:1/x?connect_timeout=2"},
	})
	defer sess.Close()
	cfg := config.Default()
	keys, _ := keymap.New(cfg.Leader, cfg.Vim, cfg.Timeoutlen, cfg.Bindings)
	a := newApp(cfg, keys, sess, &config.State{}, "")
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	settle(a, a.loadCatalog(sess.Conns[0]))
	play, seed, broken := sess.Conns[0], sess.Conns[1], sess.Conns[2]
	if play.State != connReady || len(play.Tables) == 0 || seed.State != connIdle || broken.State != connIdle {
		t.Fatalf("at start: %v %v %v", play.State, seed.State, broken.State)
	}
	a.win().focus(0)
	for _, c := range []*Conn{broken, seed} {
		treeTo(t, a, c.Spec.Name)
		_, cmd := a.Update(teaKey("l"))
		settle(a, cmd)
	}
	if broken.State != connFailed || !strings.HasPrefix(a.toast, "broken：") {
		t.Errorf("broken: %v, toast %q", broken.State, a.toast)
	}
	ns, _ := a.treeNodes()
	home := slices.IndexFunc(ns, func(n node) bool { return n.id == schemaNode(seed, seed.home) })
	if seed.State != connReady || len(seed.Tables) == 0 || eng != "sqlite" && (home < 0 || !ns[home].Open) { // a file has no schema node
		t.Errorf("seed: %v, %d tables, %s at %d\n%s", seed.State, len(seed.Tables), seed.home, home, treeTexts(a))
	}
	if play.State != connReady || a.conn() != seed {
		t.Errorf("play %v; current %s", play.State, a.conn().Spec.Name)
	}
}

// The connection started on is the one dialed, the order of the groups
// moving it in the session (F10.6).
func TestOpenGroupedStart(t *testing.T) { eachEngine(t, openGroupedStart) }

func openGroupedStart(t *testing.T, eng, dsn string) {
	sess := openUp(t, []config.Connection{
		{Name: "grouped", Group: "g", Engine: eng, DSN: dsn},
		{Name: "plain", Engine: eng, DSN: dsn},
	})
	defer sess.Close()
	if s, p := sess.Conns[1], sess.Conns[0]; sess.start != s || s.Spec.Name != "grouped" || s.State != connReady || p.State != connIdle {
		t.Errorf("start %s; %s %v, %s %v", sess.start.Spec.Name, s.Spec.Name, s.State, p.Spec.Name, p.State)
	}
}

// password_cmd runs once for a connection however often it is dialed,
// again only after a dial failed (§13).
func TestSecretCached(t *testing.T) {
	u, err := url.Parse(postgres.IntegrationDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	u.User = url.User(u.User.Username())
	runs := filepath.Join(t.TempDir(), "runs")
	spec := func(pw string) config.Connection {
		cmd := "echo >> " + runs + "; printf " + pw // sh's echo counts a run, printf gives the password bare
		if runtime.GOOS == "windows" {              // no sh: cmd's echo, set /p without the newline, the run log quoted
			cmd = `echo.>>"` + runs + `"& <nul set /p=` + pw + `& exit 0`
		}
		return config.Connection{Name: "play", Engine: "postgres", DSN: u.String(), PasswordCmd: cmd}
	}
	count := func() int { b, _ := os.ReadFile(runs); return strings.Count(string(b), "\n") }
	secrets.Clear() // another test's play
	for range 2 {
		d, err := dial(context.Background(), spec(pw))
		if err != nil {
			t.Fatal(err)
		}
		d.main.Close()
		d.meta.Close()
	}
	if count() != 1 {
		t.Errorf("two dials ran password_cmd %d times", count())
	}
	secrets.Clear() // a password source written anew: a run's are by name (§13)
	for range 2 {
		if _, err := dial(context.Background(), spec("wrong")); err == nil {
			t.Fatal("a wrong password connects")
		}
	}
	if count() != 3 {
		t.Errorf("a failed dial kept the password: %d runs", count())
	}
}

// Two queries on one connection share its Main, its transaction too: one
// begins, the other is in it and stays on the connection till it ends;
// then it goes to another once that connects (§5, F5.11).
func TestQueriesShareMain(t *testing.T) { eachEngine(t, queriesShareMain) }

func queriesShareMain(t *testing.T, eng, dsn string) {
	sess := openUp(t, []config.Connection{
		{Name: "play", Engine: eng, DSN: dsn},
		{Name: "seed", Engine: eng, DSN: dsn},
	})
	defer sess.Close()
	cfg := config.Default()
	keys, _ := keymap.New(cfg.Leader, cfg.Vim, cfg.Timeoutlen, cfg.Bindings)
	a := newApp(cfg, keys, sess, &config.State{}, "")
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	settle(a, a.loadCatalog(sess.Conns[0]))
	play, seed := sess.Conns[0], sess.Conns[1]
	c1 := queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	a.run("query.new", 0)
	c2 := queryOf(a.focused())
	run := func(c *queryTab, sql string) {
		t.Helper()
		deliver(a, a.runSQL(c, c.conn, c.schema, "query", sql, 0, c.ver))
	}
	if run(c1, "begin"); play.tx != 'T' {
		t.Fatalf("begin: %c", play.tx)
	}
	if run(c2, "select 1"); play.tx != 'T' || c2.conn != play {
		t.Fatalf("the other query's run: %c", play.tx)
	}
	if a.pickConn(c2, seed); c2.conn != play || c2.want != nil {
		t.Error("left its connection in a transaction")
	}
	run(c2, "rollback")
	if settle(a, a.pickConn(c2, seed)); play.tx != 'I' || c2.conn != seed || seed.State != connReady {
		t.Errorf("rolled back: %c, on %s, seed %v", play.tx, c2.conn.Spec.Name, seed.State)
	}
}

// n makes a session of its own connections, the current one connected for
// it first, then attached: named 1, query_n the next free, the other
// connections not connected (§7.8「新建 session」).
func TestNewSession(t *testing.T) { eachEngine(t, newSessionOn) }

func newSessionOn(t *testing.T, eng, dsn string) {
	sess := openUp(t, []config.Connection{
		{Name: "play", Engine: eng, DSN: dsn},
		{Name: "seed", Engine: eng, DSN: dsn},
	})
	cfg := config.Default()
	keys, _ := keymap.New(cfg.Leader, cfg.Vim, cfg.Timeoutlen, cfg.Bindings)
	a := newApp(cfg, keys, sess, &config.State{}, "")
	defer a.Close()
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	settle(a, a.loadCatalog(sess.Conns[0]))
	a.Update(teaKey("<Space>"))
	a.Update(teaKey("s"))
	a.Update(teaKey("n"))
	_, cmd := a.Update(teaKey("<CR>"))
	settle(a, cmd)
	s := a.sess
	if len(a.sessions) != 2 || s == sess || s.Name != "1" || s.Conns[0] == sess.Conns[0] || s.Conns[0].State != connReady || len(s.Conns[0].Tables) == 0 || s.Conns[1].State != connIdle {
		t.Fatalf("sessions %d, attached %s, %v / %v", len(a.sessions), s.Name, s.Conns[0].State, s.Conns[1].State)
	}
	if ps := s.Windows[0].Root.Leaves(); len(ps) != 1 || len(s.queries()) != 0 {
		t.Errorf("one empty pane, no query (F8.18): %d panes, %d queries", len(ps), len(s.queries()))
	}
}

// opened is an App over a session of specs, the first connected and its
// catalog in, on 160 × 45.
func opened(t *testing.T, specs ...config.Connection) *App {
	t.Helper()
	sess := openUp(t, specs)
	cfg := config.Default()
	keys, _ := keymap.New(cfg.Leader, cfg.Vim, cfg.Timeoutlen, cfg.Bindings)
	a := newApp(cfg, keys, sess, &config.State{}, "")
	t.Cleanup(a.Close)
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	settle(a, a.loadCatalog(sess.Conns[0]))
	return a
}

// lastLog is the last line of the window's log, its text after the time.
func lastLog(a *App) string {
	l := a.win().log[len(a.win().log)-1]
	return l.Head[10:] + l.Tail
}

// In the manual mode a statement out of a transaction begins one, which
// stays till a rollback takes it back, as psql's AUTOCOMMIT off: after a
// commit in a run the next statement begins anew. On PG a commit loads the
// catalog again, a DDL's table there, VACUUM begins none, and a failed
// transaction stays so, a commit of it rolling back (F5.3); MySQL's DDL
// commits on its own (F5.4).
func TestManualTx(t *testing.T) { eachEngine(t, manualTx) }

func manualTx(t *testing.T, eng, _ string) {
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: ownDB(t, eng)})
	play, c := a.sess.Conns[0], queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	run := func(sql string) { // the whole text
		t.Helper()
		c.ed.Load(sql)
		settle(a, a.runSQL(c, c.conn, c.schema, "query_1", sql, 0, c.ver))
	}
	count := func(sql string) string {
		t.Helper()
		r, err := play.Meta.Query(context.Background(), sql) // another session
		if err != nil {
			return err.Error()
		}
		return r.Rows[0][0].S
	}
	run("create table probe (a int)")
	play.Manual = true
	if run("insert into probe values (1)"); play.tx != 'T' || count("select count(*) from probe") != "0" {
		t.Fatalf("a run: %c", play.tx)
	}
	if settle(a, a.endTx(play, "rollback")); play.tx != 'I' {
		t.Fatalf("rolled back: %c", play.tx)
	}
	if run("insert into probe values (1);\ncommit;\ninsert into probe values (2)"); play.tx != 'T' || count("select count(*) from probe") != "1" {
		t.Fatalf("after a commit in the run: %c, %s", play.tx, count("select count(*) from probe"))
	}
	if settle(a, a.endTx(play, "commit")); play.tx != 'I' || count("select count(*) from probe") != "2" {
		t.Fatalf("committed: %c, %s", play.tx, count("select count(*) from probe"))
	}
	if eng != "postgres" {
		return
	}
	if run("create table probe2 (a int)"); play.tx != 'T' || count("select count(*) from pg_class where relname = 'probe2'") != "0" {
		t.Fatalf("a DDL: %c", play.tx)
	}
	if settle(a, a.endTx(play, "commit")); !slices.ContainsFunc(play.Tables, func(tb db.Table) bool { return tb.Name == "probe2" }) {
		t.Errorf("committed: probe2 not in the tree")
	}
	if run("vacuum probe"); play.tx != 'I' || strings.Contains(lastLog(a), "ERROR") {
		t.Errorf("vacuum: %c, %s", play.tx, lastLog(a))
	}
	if run("select 1/0"); play.tx != 'E' {
		t.Fatalf("failed: %c", play.tx)
	}
	if settle(a, a.endTx(play, "commit")); play.tx != 'I' || a.toast != "事务已出错，提交改为了回滚" {
		t.Errorf("committed a failed one: %c, toast %q", play.tx, a.toast)
	}
}

// A read-only connection's Main is read only too: a write that reaches it
// fails in the database; what BreaksReadOnly takes does not reach it (§13).
func TestReadOnlyMain(t *testing.T) { eachEngine(t, readOnlyMain) }

func readOnlyMain(t *testing.T, eng, dsn string) {
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: dsn, ReadOnly: true})
	c := queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	err := c.conn.Main.Run(context.Background(), func(ctx context.Context, conn db.Conn) error {
		_, err := conn.Query(ctx, "update t_user set name = name where id = 1")
		return err
	})
	want := map[string]string{"sqlite": "8"}[eng] // SQLITE_READONLY
	if want == "" {
		want = "25006"
	}
	if e := c.conn.eng.ErrorOf(err); e.Code != want { // read_only_sql_transaction, MySQL's 1792 too
		t.Errorf("a write on Main: %+v", e)
	}
	c.ed.Load("start transaction read write")
	if settle(a, a.queryRun("")); lastLog(a) != "query_1  start transaction read write  play 是只读连接，整次都没有执行" || c.conn.tx == 'T' {
		t.Errorf("start transaction read write: %s, %c", lastLog(a), c.conn.tx)
	}
}

// A run that sets where unqualified names go, PG's SET or MySQL's USE,
// leaves the query's schema to be set again before the next (§8.6);
// MySQL's /*! … */ runs as code, its statements split around it (F5.4).
func TestRunSetsSchemaAgain(t *testing.T) { eachServer(t, runSetsSchemaAgain) }

func runSetsSchemaAgain(t *testing.T, eng, dsn string) {
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: dsn})
	c := queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	run := func(sql string) {
		t.Helper()
		c.ed.Load(sql)
		settle(a, a.runSQL(c, c.conn, c.schema, "query_1", sql, 0, c.ver))
	}
	away := map[string]string{"postgres": "set search_path to agentable", "mysql": "use agentable"}[eng]
	if run(away + ";\nselect count(*) from agent"); strings.Contains(lastLog(a), "ERROR") {
		t.Fatalf("agentable's agent: %s", lastLog(a))
	}
	if run("select count(*) from t_user"); strings.Contains(lastLog(a), "ERROR") {
		t.Errorf("back on %s: %s", c.schema, lastLog(a))
	}
	if eng != "mysql" {
		return
	}
	run("/*!40101 SET NAMES utf8mb4 */;\nSELECT 5 /*! +1 */")
	log := a.win().log
	if rt := a.win().Result.Tabs[1].Result; rt == nil || rt.page.Rows[0][0].S != "6" || !strings.Contains(log[len(log)-2].Head, "  /*!40101 SET NAMES utf8mb4 */") {
		t.Errorf("/*! … */: %+v, log %v", rt, log)
	}
}

// In the manual mode a table reads on Main, in a savepoint while in a
// transaction: a change saved and not committed shows, 「（未提交）」 says
// so, a WHERE that fails leaves the transaction going; a rollback takes
// the change back on screen too. So does one the user began in the auto
// mode (§8.2, §10.3, F5.3).
func TestManualTxTable(t *testing.T) { eachEngine(t, manualTxTable) }

func manualTxTable(t *testing.T, eng, _ string) {
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: ownDB(t, eng)})
	play := a.sess.Conns[0]
	play.Manual = true
	i := slices.IndexFunc(play.Tables, func(tb db.Table) bool { return tb.Schema == play.Schema && tb.Name == "t_order" })
	settle(a, a.openTable(play, play.Tables[i]))
	tab := dataOf(a.focused())
	note := slices.IndexFunc(tab.page.Cols, func(c db.Col) bool { return c.Name == "note" })
	if tab == nil || note < 0 || !tab.page.Rows[0][note].Null {
		t.Fatalf("t_order: %+v", tab)
	}
	tab.edits = map[editKey]edit{{tab.rowKey(0), "note"}: {val: db.Val{S: "mine"}, orig: tab.page.Rows[0][note]}}
	settle(a, a.save())
	if play.tx != 'T' || !strings.Contains(tab.note.Head, "（未提交）") || tab.page.Rows[0][note].S != "mine" {
		t.Fatalf("saved: %c, %q, read back %+v", play.tx, tab.note.Head, tab.page.Rows[0][note])
	}
	tab.applied = "no_such = 1"
	settle(a, a.fetch(tab, false))
	var tx byte
	_ = play.Main.Run(context.Background(), func(_ context.Context, c db.Conn) error { tx = db.TxStatus(c); return nil })
	if tab.bar == nil || tx != 'T' {
		t.Fatalf("a WHERE failing: bar %v, %c", tab.bar, tx)
	}
	tab.applied = ""
	if settle(a, a.endTx(play, "rollback")); play.tx != 'I' || !tab.page.Rows[0][note].Null {
		t.Errorf("rolled back: %c, %+v", play.tx, tab.page.Rows[0][note])
	}
	// the auto mode, a transaction the user began: read on Main as well
	play.Manual = false
	c, data := a.sess.queries()[0], a.paneShowing(tab)
	settle(a, a.runSQL(c, play, c.schema, "query_1", "begin", 0, c.ver))
	a.win().focus(data.ID)
	tab.edits = map[editKey]edit{{tab.rowKey(0), "note"}: {val: db.Val{S: "again"}, orig: tab.page.Rows[0][note]}}
	if settle(a, a.save()); play.tx != 'T' || tab.page.Rows[0][note].S != "again" {
		t.Errorf("saved in the user's: %c, read back %+v", play.tx, tab.page.Rows[0][note])
	}
	settle(a, a.endTx(play, "rollback"))
}

// backends is how many connections named app the server has.
func backends(t *testing.T, app string) int {
	t.Helper()
	c, _, err := postgres.Connect(context.Background(), postgres.IntegrationDSN(t), "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.Query(context.Background(), "select count(*) from pg_stat_activity where application_name = $1", db.Val{S: app})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(r.Rows[0][0].S)
	return n
}

// A session closed while one of its connections connects: the late answer
// closes them again, none left behind; a run going on in it says nothing
// in the session left, nor asks first (F5.1).
func TestKillSessionUnderWay(t *testing.T) {
	dsn := postgres.IntegrationDSN(t)
	app := fmt.Sprintf("sqlmux_kill_%d", os.Getpid())
	a := opened(t, config.Connection{Name: "play", Engine: "postgres", DSN: dsn},
		config.Connection{Name: "seed", Engine: "postgres", DSN: dsn + "&application_name=" + app})
	feed(t, a, "<Space>s")
	a.Update(teaKey("n"))
	_, cmd := a.Update(teaKey("<CR>"))
	settle(a, cmd)
	seed := a.sess.Conns[1]
	dial := a.connect(seed) // seed opened in session 1's tree
	a.run("query.new", 0)   // a new session has none
	con := a.sess.queries()[0]
	run := a.runSQL(con, con.conn, con.schema, con.name, "select pg_sleep(3)", 0, con.ver)
	ran := make(chan tea.Msg, 1)
	go func() { ran <- run().(tea.BatchMsg)[0]() }()
	time.Sleep(300 * time.Millisecond)
	feed(t, a, "<Space>s")
	_, kill := a.Update(teaKey("x"))
	if len(a.sessions) != 1 || a.confirm != nil {
		t.Fatalf("closed: %d sessions, asked %v", len(a.sessions), a.confirm)
	}
	settle(a, kill)
	if a.Update(<-ran); a.toast == "查询已取消" {
		t.Error("the closed session's run cancelled, said in the one left")
	}
	settle(a, dial)
	a.Close()
	if n := backends(t, app); n != 0 {
		t.Errorf("%d connections of the closed session left", n)
	}
}

// A connection the form adds while n connects is the new session's too
// (§7.8).
func TestNewSessionTakesNewConn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := opened(t, config.Connection{Name: "play", Engine: "postgres", DSN: postgres.IntegrationDSN(t)})
	feed(t, a, "<Space>s")
	a.Update(teaKey("n"))
	_, cmd := a.Update(teaKey("<CR>"))
	a.run("conn.new", 0)
	feed(t, a, "extra<Tab><Tab><Tab>localhost<Tab><Tab>u<Tab><Tab>x<CR>")
	if settle(a, cmd); a.sess.Name != "1" || len(a.sess.Conns) != 2 || a.sess.Conns[1].Spec.Name != "extra" {
		t.Errorf("session %s: %d connections", a.sess.Name, len(a.sess.Conns))
	}
}

// killBackends ends c's Main and Meta on the server, as a proxy dropping
// them would.
func killBackends(t *testing.T, eng, dsn string, c *Conn) {
	t.Helper()
	ctx := context.Background()
	idSQL, killSQL := "select pg_backend_pid()", "select pg_terminate_backend(%s)"
	if eng == "mysql" {
		idSQL, killSQL = "select connection_id()", "kill %s"
	}
	admin, _, err := drivers[eng].Connect(ctx, dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	for _, w := range []*db.Worker{c.Main, c.Meta} {
		r, err := w.Query(ctx, idSQL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Query(ctx, fmt.Sprintf(killSQL, r.Rows[0][0].S)); err != nil {
			t.Fatal(err)
		}
	}
}

// A connection the server dropped comes back by itself on the next
// request: the catalog without a word; a query run, which may write, says
// the connection is back and to run it again, its error the first, and the
// run after finds its schema as the query set it. A transaction is lost
// with the connection: a write in it is not run, a commit says it is not
// known to have taken; the manual mode stays (§8.2「断线重连」, F8.19).
func TestReconnect(t *testing.T) { eachServer(t, reconnect) }

func reconnect(t *testing.T, eng, _ string) {
	dsn := ownDB(t, eng)
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: dsn})
	was := Send
	Send = func(m tea.Msg) { a.Update(m) } // as the program's Send, in the order the events came in
	t.Cleanup(func() { Send = was })
	play, c := a.sess.Conns[0], queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	run := func(sql string) {
		t.Helper()
		c.ed.Load(sql)
		settle(a, a.runSQL(c, c.conn, c.schema, "query_1", sql, 0, c.ver))
	}
	count := func() string {
		t.Helper()
		r, err := play.Meta.Query(context.Background(), "select count(*) from probe")
		if err != nil {
			return err.Error()
		}
		return r.Rows[0][0].S
	}
	failed := func() bool { return strings.Contains(lastLog(a), "ERROR") || strings.Contains(lastLog(a), "FATAL") }
	run("create table probe (a int)")
	home := c.schema
	c.schema = "agentable"
	if run("select count(*) from agent"); failed() {
		t.Fatalf("agentable's agent: %s", lastLog(a))
	}

	killBackends(t, eng, dsn, play)
	a.toast = ""
	settle(a, a.loadCatalog(play))
	if len(play.Tables) == 0 || play.down() != nil || a.toast != "" {
		t.Fatalf("the catalog: %d tables, %v, toast %q", len(play.Tables), play.down(), a.toast)
	}
	if code := writeCode(play, play.Meta); code != "25006" { // Meta is read only after the redial too
		t.Fatalf("a write on Meta: %q", code)
	}
	if run("select count(*) from agent"); !failed() || a.toast != "play：已重连，请重新执行" {
		t.Fatalf("a run after the drop: %s, toast %q", lastLog(a), a.toast)
	}
	if run("select count(*) from agent"); failed() {
		t.Fatalf("the run again, in the query's schema: %s", lastLog(a))
	}
	c.schema = home

	killBackends(t, eng, dsn, play)
	if run("insert into probe values (1)"); !failed() || a.toast != "play：已重连，请重新执行" || count() != "0" {
		t.Fatalf("a write after the drop: %s, toast %q, %s rows", lastLog(a), a.toast, count())
	}
	if run("insert into probe values (2)"); failed() || count() != "1" {
		t.Fatalf("the connection is back: %s", lastLog(a))
	}

	play.Manual = true
	if run("insert into probe values (3)"); play.tx != 'T' {
		t.Fatalf("a transaction: %c", play.tx)
	}
	killBackends(t, eng, dsn, play)
	a.toast = ""
	run("insert into probe values (4)")
	if !strings.HasSuffix(lastLog(a), "连接断开，事务已丢失，这条没有执行") || a.toast != "play：连接断开，事务已丢失" || play.tx != 'I' || !play.Manual || count() != "1" {
		t.Fatalf("a write in the lost transaction: %s, toast %q, status %c, manual %v, %s rows", lastLog(a), a.toast, play.tx, play.Manual, count())
	}
	if run("insert into probe values (5)"); failed() || play.tx != 'T' {
		t.Fatalf("a new transaction: %s, %c", lastLog(a), play.tx)
	}
	killBackends(t, eng, dsn, play)
	settle(a, a.endTx(play, "commit"))
	if a.toast != "连接断开，提交结果未知：请先查一下数据" || play.tx != 'I' || count() != "1" {
		t.Errorf("a commit as the connection drops: toast %q, status %c, %s rows", a.toast, play.tx, count())
	}
}

// writeCode is the SQLSTATE a write on w is refused with, "no error" when
// it is not refused (the test's database is its own).
func writeCode(c *Conn, w *db.Worker) string {
	_, err := w.Query(context.Background(), "create table zz_ro (a int)")
	if err == nil {
		return "no error"
	}
	return c.eng.ErrorOf(err).Code
}

// What was dialed read only is read only after the redial too: a read-only
// connection's Main, and every connection's Meta (F8.19).
func TestReconnectReadOnly(t *testing.T) { eachServer(t, reconnectReadOnly) }

func reconnectReadOnly(t *testing.T, eng, _ string) {
	dsn := ownDB(t, eng) // a write that gets through must not reach the shared one
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: dsn, ReadOnly: true})
	was := Send
	Send = func(m tea.Msg) { a.Update(m) }
	t.Cleanup(func() { Send = was })
	play := a.sess.Conns[0]
	killBackends(t, eng, dsn, play)
	if code := writeCode(play, play.Main); code != "25006" { // read_only_sql_transaction, MySQL's 1792 too
		t.Errorf("Main after the redial: %q", code)
	}
	if code := writeCode(play, play.Meta); code != "25006" {
		t.Errorf("Meta after the redial: %q", code)
	}
}

// A table's page and the palette's DDL preview come back after the drop
// with no error (F8.19).
func TestReconnectTable(t *testing.T) { eachServer(t, reconnectTable) }

func reconnectTable(t *testing.T, eng, dsn string) {
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: dsn})
	play := a.sess.Conns[0]
	i := slices.IndexFunc(play.Tables, func(tb db.Table) bool { return tb.Schema == play.Schema && tb.Name == "t_order" })
	settle(a, a.openTable(play, play.Tables[i]))
	tab := dataOf(a.focused())
	killBackends(t, eng, dsn, play)
	a.toast = ""
	settle(a, a.fetch(tab, true))
	if tab.bar != nil || len(tab.page.Rows) == 0 || a.toast != "" {
		t.Errorf("after the drop: bar %v, %d rows, toast %q", tab.bar, len(tab.page.Rows), a.toast)
	}
	// and the palette's DDL preview
	killBackends(t, eng, dsn, play)
	feed(t, a, "<C-p>@t_order")
	cmd := a.fetchDDL(previewDue{a.palette, a.palette.previewSeq})
	if cmd == nil {
		t.Fatal("no DDL asked for")
	}
	if m, ok := cmd().(ddlMsg); !ok || m.err != nil || m.text == "" {
		t.Errorf("the DDL after the drop: %+v", m)
	}
}

// The manager's setConfig, in Update, and a Worker's redial reading the
// connection as it is now, in a request's goroutine (F8.19); the data race is
// -race's to find.
func TestRedialReadsConfigWithoutARace(*testing.T) {
	spec := config.Connection{Name: "race", Engine: "postgres", DSN: "postgres://u@127.0.0.1:1/x?connect_timeout=1"}
	c := newConn(spec)
	c.take(dialed{main: noDB{}, meta: noDB{}, spec: spec})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			_, _ = c.Main.Redial(context.Background())
			_, _ = c.Meta.Redial(context.Background())
		}
	}()
	for i := range 200 {
		edited := spec
		edited.DSN = fmt.Sprintf("postgres://u@127.0.0.1:%d/x?connect_timeout=1", 1+i%2)
		c.setConfig(edited)
	}
	<-done
}

// dd on a table or a view in the tree (TestDropTable) drops it on Main: its tabs close, the
// catalog loads again without it; a drop the server refuses is its message
// in a toast; a read-only connection is not asked (F8.23).
func TestDropTableOnServer(t *testing.T) { eachEngine(t, dropTableOn) }

func dropTableOn(t *testing.T, eng, dsn string) {
	a := opened(t, config.Connection{Name: "play", Engine: eng, DSN: ownDB(t, eng)})
	play, c := a.sess.Conns[0], queryOf(a.win().Root.B.Pane)
	a.win().focus(a.win().Root.B.Pane.ID)
	sql := "create table probe (a int);\ncreate view v_probe as select a from probe;"
	c.ed.Load(sql)
	settle(a, a.runSQL(c, c.conn, c.schema, "query_1", sql, 0, c.ver))
	settle(a, a.loadCatalog(play))
	has := func(name string) bool {
		return slices.ContainsFunc(play.Tables, func(tb db.Table) bool { return tb.Name == name })
	}
	if !has("probe") || !has("v_probe") {
		t.Fatalf("tables %v", play.Tables)
	}
	data := a.openTarget() // where the table opens
	drop := func(name string) {
		t.Helper()
		a.win().focus(0)
		treeTo(t, a, name)
		if feed(t, a, "dd"); a.confirm == nil || !strings.Contains(a.confirm.text, "DROP") {
			t.Fatalf("%s: asked %+v", name, a.confirm)
		}
		_, cmd := a.Update(teaKey("<CR>"))
		settle(a, cmd)
	}
	var probe db.Table
	for _, tb := range play.Tables {
		if tb.Name == "probe" {
			probe = tb
		}
	}
	a.sess.open[groupNode(play, probe.Schema, true)] = true // the Views
	settle(a, a.openTable(play, probe))
	if !strings.Contains(tabNames(data), "probe") {
		t.Fatalf("tabs %q", tabNames(data))
	}
	drop("v_probe")
	if has("v_probe") || !has("probe") || a.toast != "已删除 "+probe.Schema+".v_probe" {
		t.Errorf("view: tables %v, toast %q", play.Tables, a.toast)
	}
	drop("probe")
	if has("probe") || strings.Contains(tabNames(data), "probe") || !strings.HasPrefix(a.toast, "已删除 ") {
		t.Errorf("table: tables %v, tabs %q, toast %q", play.Tables, tabNames(data), a.toast)
	}
	// the node is stale, the table gone already: the server's answer
	play.Tables = append(play.Tables, probe)
	busy := play.busy
	drop("probe")
	if want := map[string]string{"postgres": `table "probe" does not exist`, "mysql": "Unknown table"}[eng]; !strings.Contains(a.toast, want) || play.busy != busy {
		t.Errorf("a table that is gone: toast %q, busy %d (was %d)", a.toast, play.busy, busy)
	}
	if has("probe") { // a failure reads the catalog again too: the stale node goes
		t.Errorf("a table that is gone is still listed: %v", play.Tables)
	}
	// read only: nothing is asked
	b := opened(t, config.Connection{Name: "ro", Engine: eng, DSN: dsn, ReadOnly: true})
	b.win().focus(0)
	treeTo(t, b, "t_user")
	if feed(t, b, "dd"); b.confirm != nil || b.toast != "ro 是只读连接" {
		t.Errorf("read only: asked %v, toast %q", b.confirm != nil, b.toast)
	}
}

// openUp is Open on the first of specs with it connected, as the program has it
// once its first dial is in.
func openUp(t *testing.T, specs []config.Connection) *Session {
	t.Helper()
	sess := Open(specs, 0)
	d, err := dial(context.Background(), specs[0])
	if err != nil {
		t.Fatal(err)
	}
	sess.start.take(d)
	return sess
}

// C-t on a form whose dsn is a database's connects and says the server's
// version and how long it took, on either engine; the connection is not
// kept (F12.5).
func TestConnFormTestConnects(t *testing.T) { eachEngine(t, connFormTestConnects) }

func connFormTestConnects(t *testing.T, eng, dsn string) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := withSessions()
	a.openConnForm()
	f := a.form
	f.fields[fieldName].Input = ui.Input{Text: "tried", Pos: 5}
	f.fields[fieldEngine].On = slices.Index(f.fields[fieldEngine].Choices, eng)
	f.fields[fieldAdvanced].On = 1
	f.fields[fieldDSN].Input = ui.Input{Text: dsn, Pos: len(dsn)}
	_, cmd := a.Update(teaKey("<C-t>"))
	if cmd == nil {
		t.Fatalf("not tested: %q", f.err)
	}
	a.Update(cmd())
	want := map[string]string{"postgres": "已连接 · PostgreSQL ", "mysql": "已连接 · MySQL "}[eng]
	if !strings.HasPrefix(f.result, want) || !strings.HasSuffix(f.result, "s") || f.err != "" || strings.Contains(f.result, " on ") || strings.Contains(f.result, " (") {
		t.Errorf("result %q, err %q", f.result, f.err)
	}
	if len(a.sess.Conns) != 1 {
		t.Errorf("the tested connection is in the tree: %d", len(a.sess.Conns))
	}
}

// password_cmd, password_env and ~/.pgpass all connect (§13); with none of
// them, nothing else supplies the password; a wrong one is the server's error.
func TestDialPasswordSources(t *testing.T) {
	u, err := url.Parse(postgres.IntegrationDSN(t))
	if err != nil || u.User == nil {
		t.Fatal("SQLMUX_TEST_PG must be a postgres:// URL with a password")
	}
	pw, _ := u.User.Password()
	u.User = url.User(u.User.Username())
	dsn := u.String() // without its password
	pgpass := filepath.Join(t.TempDir(), "pgpass")
	if err := os.WriteFile(pgpass, []byte(strings.Join([]string{u.Hostname(), "*", "*", u.User.Username(), pw}, ":")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		spec     config.Connection
		env, val string
		wrong    bool
		fails    bool
	}{
		"password_cmd": {spec: config.Connection{PasswordCmd: "printf '%s\\n' '" + pw + "'"}},
		"password_env": {spec: config.Connection{PasswordEnv: "SQLMUX_TEST_PW"}, env: "SQLMUX_TEST_PW", val: pw},
		"pgpass":       {env: "PGPASSFILE", val: pgpass},
		"none":         {fails: true},
		"wrong":        {spec: config.Connection{Password: "wrong"}, wrong: true, fails: true},
	} {
		t.Run(name, func(t *testing.T) {
			if c.env != "" {
				t.Setenv(c.env, c.val)
			}
			spec := c.spec
			spec.Name, spec.Engine, spec.DSN = "pw-"+name, "postgres", dsn // a run caches passwords by name (§13): one each
			d, err := dial(context.Background(), spec)
			if (err != nil) != c.fails || c.wrong && (err == nil || !strings.Contains(err.Error(), "password authentication failed")) {
				t.Fatalf("error %v, want one: %v", err, c.fails)
			}
			if err == nil {
				d.discard()()
			}
		})
	}
}

// A PG connection has the server's databases with its catalog, the one it
// is dialed to among them; one that dials another has none of its own, and
// MySQL and SQLite no databases at all: theirs are schemas, or the file
// (F15.1).
func TestDatabasesWithCatalog(t *testing.T) { eachEngine(t, databasesWithCatalog) }

func databasesWithCatalog(t *testing.T, eng, dsn string) {
	sess := openUp(t, []config.Connection{{Name: "play", Engine: eng, DSN: dsn}})
	defer sess.Close()
	cfg := config.Default()
	keys, _ := keymap.New(cfg.Leader, cfg.Vim, cfg.Timeoutlen, cfg.Bindings)
	a := newApp(cfg, keys, sess, &config.State{}, "")
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	play := sess.Conns[0]
	settle(a, a.loadCatalog(play))
	if eng != "postgres" {
		if play.Databases != nil {
			t.Fatalf("databases %v", play.Databases)
		}
		return
	}
	if !slices.Contains(play.Databases, play.DB) || slices.Contains(play.Databases, "template0") {
		t.Fatalf("databases %v of %s", play.Databases, play.DB)
	}
	other := "sqlmux_f151_" + strconv.Itoa(os.Getpid())
	admin, _, err := postgres.Connect(context.Background(), dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	_, _ = admin.Query(context.Background(), "drop database if exists "+other)
	if _, err := admin.Query(context.Background(), "create database "+other); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Query(context.Background(), "drop database if exists "+other+" with (force)") }()
	child := play.inDB(other)
	if child.Spec.Database != other || child.Spec.Name != "play" || child == play || play.inDB(other) != child {
		t.Fatalf("child %+v", child.Spec)
	}
	d, err := dial(context.Background(), child.Spec)
	if err != nil {
		t.Fatal(err)
	}
	child.take(d)
	defer d.discard()()
	m := a.loadCatalog(child)().(catalogMsg)
	if child.DB != other || m.err != nil || m.dbs != nil || len(m.schemas) == 0 {
		t.Fatalf("child in %q: catalog %v, databases %v, error %v", child.DB, m.schemas, m.dbs, m.err)
	}
	if play.inDB(play.DB) != play || play.inDB("") != play {
		t.Error("the DSN's database is the connection itself")
	}
}
