package app

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

var (
	errLost   = errors.New("broken pipe")  // lost, the request sent
	errUnsent = errors.New("write failed") // lost before anything was sent
)

// redialed makes a's Main dial again, to d2, and tells the tiers of the
// errors those two are.
func redialed(a *App, d2 db.Conn) {
	w := a.sess.Conns[0].Main
	w.Redial = func(context.Context) (db.Conn, error) { return d2, nil }
	w.Broken = func(err error) db.Lost {
		switch {
		case errors.Is(err, errLost):
			return db.LostSent
		case errors.Is(err, errUnsent):
			return db.LostUnsent
		}
		return db.NotLost
	}
	w.Notify = func(e db.Event) { Send(connEvent{a.sess.Conns[0], false, e}) }
}

// heard is what the Workers tell the program while the test runs (Send).
func heard(t *testing.T) *[]connEvent {
	t.Helper()
	var evs []connEvent
	was := Send
	Send = func(m tea.Msg) { evs = append(evs, m.(connEvent)) }
	t.Cleanup(func() { Send = was })
	return &evs
}

// A query run goes again on the connection dialed anew only if nothing of
// it was sent, a read as much as a write (a select may call a function that
// writes); the new connection has its own search_path, which the query's
// schema is set on before. What is not run again keeps its error, and a toast
// says to run it (F8.19).
func TestRunAfterLostConnection(t *testing.T) {
	evs := heard(t)
	run := func(sql string, failed map[string]error, key string) (*App, *execDB, *execDB) {
		d := &execDB{fail: failed}
		a, c := inRun(t, d, sql)
		d2 := &execDB{res: map[string]db.Result{"select 1": rows(1)}}
		redialed(a, d2)
		a.sess.Conns[0].startedPath = `"$user", public`
		c.schema = "agentable"
		*evs = nil
		press(t, a, key)
		return a, d, d2
	}
	// nothing sent: the run goes again, the schema set first
	a, _, d2 := run("select 1", map[string]error{"select 1": errUnsent}, "<CR>")
	if strings.Join(d2.ran, "; ") != `search_path "agentable" "$user", public; select 1`+"\nLIMIT 501" || strings.Contains(logText(a), "write failed") {
		t.Fatalf("on the new connection: %q, log %q", d2.ran, logText(a))
	}
	if len(*evs) != 1 || !(*evs)[0].Rerun || (*evs)[0].Err != nil {
		t.Fatalf("events %+v", *evs)
	}
	// sent, a read or a write alike: not run again, the error the first
	for _, sql := range []string{"select nextval('s')", "update t set a = 1"} {
		a, d, d2 := run(sql, map[string]error{sql: errLost}, "<CR>")
		if len(d2.ran) != 0 || !strings.Contains(logText(a), "broken pipe") || len(d.ran) == 0 {
			t.Fatalf("%s: ran again: %q, log %q", sql, d2.ran, logText(a))
		}
		last := (*evs)[len(*evs)-1]
		if last.Rerun {
			t.Fatalf("%s: events %+v", sql, *evs)
		}
		if a.Update(last); a.toast != "doraemon：已重连，请重新执行" {
			t.Errorf("%s: toast %q", sql, a.toast)
		}
	}
	// a statement done, a second unsent: the first would run twice
	a, d, d2 := run("insert 1;\ninsert 2", map[string]error{"insert 2": errUnsent}, "ggVG<CR>")
	if len(d2.ran) != 0 || len(d.ran) != 3 || !strings.Contains(logText(a), "write failed") {
		t.Fatalf("a run of two: ran %q on the new connection, %q before, log %q", d2.ran, d.ran, logText(a))
	}
}

// What a run says when the transaction is lost with the connection: its
// statement was not run, not the SET of the schema that went first, which is
// the one that found the connection lost (F8.19).
func TestRunInLostTransaction(t *testing.T) {
	evs := heard(t)
	set := `search_path "agentable" "$user", public`
	d := &execDB{tx: 'T', fail: map[string]error{"update t set a = 1": errLost, set: errLost}}
	a, c := inRun(t, d, "select 1")
	d2 := &execDB{}
	redialed(a, d2)
	a.sess.Conns[0].startedPath = `"$user", public`
	press(t, a, "<CR>") // Main is in a transaction as this run ends
	c.schema = "agentable"
	a.queryDid(c, c.ed.Load("update t set a = 1"))
	*evs = nil
	press(t, a, "<CR>")
	if len(d2.ran) != 0 || logText(a)[strings.LastIndex(logText(a), "\n")+1:] != "query_1  update t set a = 1  ERROR: 连接断开，事务已丢失，这条没有执行" {
		t.Fatalf("ran %q on the new connection, log %q", d2.ran, logText(a))
	}
	if len(*evs) != 1 || !(*evs)[0].TxLost {
		t.Fatalf("events %+v", *evs)
	}
}

// A COMMIT or a ROLLBACK the user types in the query says what the
// commit of the toolbar says: a commit not taken, or not known to have; a
// rollback is done (F8.19).
func TestQueryEndLost(t *testing.T) {
	heard(t)
	for _, tc := range []struct {
		sql, key string
		fail     error
		want     string // the last log line, past the statement
	}{
		{"commit", "commit", errLost, "ERROR: 连接断开，提交结果未知：请先查一下数据"},
		{"commit work", "commit work", errUnsent, "ERROR: 事务已丢失，没有提交"},
		{"end", "end", errLost, "ERROR: 连接断开，提交结果未知：请先查一下数据"},
		{"rollback", "rollback", errLost, "ROLLBACK"},
		{"abort", "abort", errLost, "ROLLBACK"},
		{"update t set a = 1", "update t set a = 1", errLost, "ERROR: 连接断开，事务已丢失，这条没有执行"},
		// back to a savepoint is no end: the transaction would go on, and it is not told so
		{"rollback to savepoint a", "rollback to savepoint a", errLost, "ERROR: 连接断开，事务已丢失，这条没有执行"},
		{"rollback work to a", "rollback work to a", errLost, "ERROR: 连接断开，事务已丢失，这条没有执行"},
		{"rollback to a", "rollback to a", errLost, "ERROR: 连接断开，事务已丢失，这条没有执行"},
	} {
		d := &execDB{tx: 'T', fail: map[string]error{tc.key: tc.fail}}
		a, c := inRun(t, d, "select 1")
		redialed(a, &execDB{})
		press(t, a, "<CR>") // Main is in a transaction as this run ends
		a.queryDid(c, c.ed.Load(tc.sql))
		press(t, a, "<CR>")
		log := logText(a)
		if got := log[strings.LastIndex(log, "\n")+1:]; !strings.HasPrefix(got, "query_1  "+tc.sql+"  "+tc.want) {
			t.Errorf("%s: %q", tc.sql, got)
		}
		// the commit that may have taken is no "transaction lost" in the toast, which the
		// connection's event, first, said (F13.1)
		if unknown := errors.Is(tc.fail, errLost) && (tc.sql == "commit" || tc.sql == "end"); unknown != (a.toast == "连接断开，提交结果未知：请先查一下数据") {
			t.Errorf("%s: toast %q", tc.sql, a.toast)
		}
	}
}

// What a lost rollback leaves unrun of a run that goes on past it is told
// as it is: the rollback was not run either (F8.19).
func TestQueryRollbackLostMidRun(t *testing.T) {
	heard(t)
	d := &execDB{tx: 'T', fail: map[string]error{"rollback": errLost}}
	a, c := inRun(t, d, "select 1")
	redialed(a, &execDB{})
	press(t, a, "<CR>")
	a.queryDid(c, c.ed.Load("rollback;\nselect 2"))
	press(t, a, "ggVG<CR>")
	if !strings.Contains(logText(a), "ERROR: 连接断开，事务已丢失，这条没有执行") {
		t.Errorf("log %q", logText(a))
	}
}

// A commit or a rollback after the connection lost its transaction (F8.19).
func TestEndTxLost(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		err  error
		want error // nil: the user hears nothing of it
	}{
		{"commit", db.TxLost{Err: errLost, Sent: true}, db.ErrCommitUnknown},
		{"commit", db.TxLost{Err: errUnsent}, db.ErrNotCommitted},
		{"commit", db.TxLost{}, db.ErrNotCommitted},
		{"rollback", db.TxLost{Err: errLost, Sent: true}, nil},
		{"commit", errors.New("other"), errors.New("other")},
	} {
		got := txLost(tc.sql, tc.err)
		if got == nil && tc.want != nil || got != nil && (tc.want == nil || got.Error() != tc.want.Error()) {
			t.Errorf("%s of %v: %v, want %v", tc.sql, tc.err, got, tc.want)
		}
	}
}

// queryDB is a connection whose Query fails with what fail says, and whose
// status is tx; a MySQL-like one, that does not set it itself.
type queryDB struct {
	noDB
	tx   byte
	fail map[string]error
}

func (d *queryDB) TxStatus() byte { return d.tx }
func (d *queryDB) Query(_ context.Context, sql string, _ ...db.Val) (db.Result, error) {
	err := d.fail[sql]
	if errors.Is(err, errLost) {
		d.tx = 'I' // as MySQL's does, the server took it
	}
	return db.Result{Tag: strings.ToUpper(sql)}, err
}

// A commit sent as the connection dropped is not run again, and its
// result is not known: that is what the toast says, after the one that
// says the transaction is lost; the status is out of a transaction (F8.19).
func TestCommitLost(t *testing.T) {
	evs := heard(t)
	a, _ := inRun(t, &execDB{}, "")
	c := a.sess.Conns[0]
	old := &queryDB{tx: 'T', fail: map[string]error{"commit": errLost}}
	c.Main = db.NewWorker(old)
	redialed(a, &queryDB{})
	if _, err := c.Main.Query(context.Background(), "select 1"); err != nil { // the Worker learns it is in a transaction
		t.Fatal(err)
	}
	c.tx = 'T'
	msg := a.endTx(c, "commit")()
	for _, e := range *evs { // the program gets the events first: Send ran inside the request
		a.Update(e)
	}
	if a.toast != "doraemon：连接断开，事务已丢失" || c.tx != 'I' {
		t.Fatalf("after the event: toast %q, status %c", a.toast, c.tx)
	}
	a.Update(msg)
	if a.toast != db.ErrCommitUnknown.Error() || c.tx != 'I' {
		t.Errorf("after the commit: toast %q, status %c", a.toast, c.tx)
	}
}

// A redial that fails turns the connection's node red, a toast naming why,
// till one goes through; the connection stays connected for that try
// (F8.19).
func TestRedialFailureShows(t *testing.T) {
	evs := heard(t)
	a, _ := withConns()
	c := a.sess.Conns[0]
	c.take(dialed{main: noDB{}, meta: noDB{}, spec: c.Spec}) // the wiring of the Workers: Main's and Meta's apart
	red := func() bool {
		ns, _ := a.treeNodes()
		for _, n := range ns {
			if n.kind == nodeConn && n.conn == c {
				return n.TextFg == a.theme.Error
			}
		}
		t.Fatal("no node")
		return false
	}
	tell := func(w *db.Worker, e db.Event) {
		t.Helper()
		*evs = nil
		w.Notify(e)
		if len(*evs) != 1 {
			t.Fatalf("events %+v", *evs)
		}
		a.Update((*evs)[0])
	}
	if red() {
		t.Fatal("red before")
	}
	tell(c.Meta, db.Event{Err: errors.New("connection refused")})
	if !red() || c.State != connReady || a.toast != "doraemon：connection refused" {
		t.Fatalf("failed: red %v, %v, toast %q", red(), c.State, a.toast)
	}
	tell(c.Main, db.Event{Rerun: true}) // Main's: Meta's is still down
	if !red() {
		t.Error("Main's redial cleared Meta's")
	}
	tell(c.Meta, db.Event{Rerun: true})
	if red() {
		t.Error("still red after a redial went through")
	}
}

// A transaction lost with the connection: the status goes back to out of
// one, the mode stays, the catalog and the tables load again, the user is
// told; a redial that ran the request again says nothing (F8.19).
func TestRedialTransactionLost(t *testing.T) {
	a, _ := withConns()
	c := a.sess.Conns[0]
	c.Manual, c.tx = true, 'T'
	c.cols[idOf(db.Table{Schema: "public", Name: "t"})] = db.Columns{}
	if _, cmd := a.Update(connEvent{c, false, db.Event{Rerun: true}}); cmd != nil && c.tx != 'T' || a.toast != "" {
		t.Fatalf("a quiet redial: %c, toast %q", c.tx, a.toast)
	}
	_, cmd := a.Update(connEvent{c, false, db.Event{TxLost: true, Rerun: true}})
	if c.tx != 'I' || !c.Manual || a.toast != "doraemon：连接断开，事务已丢失" {
		t.Errorf("lost: %c, manual %v, toast %q", c.tx, c.Manual, a.toast)
	}
	if len(c.cols) != 0 || cmd == nil {
		t.Errorf("the tables and the catalog stay: %d columns, cmd %v", len(c.cols), cmd)
	}
}

// What comes of a session closed meanwhile (F8.19).
func TestRedialEventOfAClosedSession(t *testing.T) {
	a, _ := withConns()
	gone := newConn(config.Connection{Name: "gone", Engine: "postgres"})
	a.spinOn = true // the dots' tick is not what the event asks for
	if _, cmd := a.Update(connEvent{gone, false, db.Event{Err: errors.New("x")}}); cmd != nil || a.toast != "" || gone.down() != nil {
		t.Errorf("toast %q, down %v", a.toast, gone.down())
	}
}

// A connection renamed in the manager takes its keychain item with it, and
// a Worker that dials again reads the new one, not the item of the old name,
// which is gone (F8.19).
func TestRedialAfterRenameInTheManager(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the keychain fakes are /bin/sh scripts")
	}
	a := managed(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	withOwnPassword(t, a)
	conn := a.sess.Conns[1]
	conn.take(dialed{main: noDB{}, meta: noDB{}, spec: conn.Spec})
	a.run("conn.manage", 0)
	feed(t, a, "j<Tab><C-u>third<C-s>")
	if conn.Spec.Name != "third" {
		t.Fatalf("renamed to %q", conn.Spec.Name)
	}
	_ = os.Remove(filepath.Join(os.Getenv("FAKE"), "log"))
	_, err := conn.Main.Redial(context.Background())
	if err == nil || strings.Contains(err.Error(), "password") {
		t.Errorf("the redial: %v", err) // a dial that fails, its password found
	}
	if calls := kcCalled("find-generic-password", "lookup"); len(calls) != 1 || !strings.Contains(calls[0], "third") {
		t.Errorf("the keychain read: %q", calls)
	}
}

// A redial dials the connection as it is now, not as it was dialed: a
// rename moved the keychain item, an edit may have changed the address; its
// engine and lock stay the dial's (F8.19).
func TestRedialDialsTheCurrentConfig(t *testing.T) {
	c := newConn(config.Connection{Name: "was", Engine: "postgres", DSN: "postgres://u@127.0.0.1:1/x?connect_timeout=1"})
	c.take(dialed{main: noDB{}, meta: noDB{}, spec: c.Spec})
	edited := c.Spec
	edited.Name, edited.DSN, edited.Engine = "now", "postgres://u@127.0.0.1:2/x?connect_timeout=1", "mysql"
	c.setConfig(edited)
	_, err := c.Main.Redial(context.Background())
	refused := "127.0.0.1:2: connect: connection refused" // windows says connectex
	if runtime.GOOS == "windows" {
		refused = "127.0.0.1:2: connectex"
	}
	if err == nil || !strings.Contains(err.Error(), refused) { // PG's dial of the new address
		t.Errorf("the address it dials: %v", err)
	}
}

// A redial asks for the password as the first dial did, not from what the
// first one cached: password_cmd may give a token that has changed, and a
// renamed connection's is by its new name (F8.19).
func TestRedialAsksForThePasswordAgain(t *testing.T) {
	runs := filepath.Join(t.TempDir(), "runs")
	spec := config.Connection{Name: "redial-secret", Engine: "postgres", DSN: "postgres://u@127.0.0.1:1/x?connect_timeout=1", PasswordCmd: "echo >> " + runs + "; printf pw"}
	secrets.Store(spec.Name, "cached")
	t.Cleanup(func() { secrets.Delete(spec.Name) })
	c := newConn(spec)
	w := c.worker(noDB{}, spec, false)
	if _, err := w.Redial(context.Background()); err == nil {
		t.Fatal("nothing listens on port 1")
	}
	if b, _ := os.ReadFile(runs); strings.Count(string(b), "\n") != 1 {
		t.Errorf("password_cmd ran %q", b)
	}
}

// lostSaveDB is Main of a save whose UPDATE loses the connection: BEGIN puts
// it in a transaction, the UPDATE fails with errLost.
type lostSaveDB struct {
	noDB
	tx byte
}

func (d *lostSaveDB) Query(_ context.Context, sql string, _ ...db.Val) (db.Result, error) {
	switch {
	case sql == "begin":
		d.tx = 'T'
	case strings.HasPrefix(sql, "update"):
		return db.Result{}, errLost
	}
	return db.Result{}, nil
}

func (d *lostSaveDB) TxStatus() byte { return cmp.Or(d.tx, 'I') }

// A save that loses the connection before its COMMIT shows the statement's
// own error as rolled back, as any failure of it, the changes still marked;
// and says the transaction is lost, as one of the user's (F13.1, §8.2).
func TestSaveLostBeforeCommit(t *testing.T) {
	evs := heard(t)
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	a.sess.Conns[0].Main = db.NewWorker(&lostSaveDB{})
	redialed(a, &lostSaveDB{})
	feed(t, a, "lix<Esc>")
	n := len(tab.edits)
	_, cmd := a.Update(teaKey("<C-s>"))
	m := cmd().(saveMsg)
	a.Update(m)
	if b := tab.bar; b == nil || b.First != (ui.Note{Head: "id = 1：", Mid: "broken pipe", Tail: "，已回滚"}) || len(tab.edits) != n || n == 0 {
		t.Fatalf("bar %+v, edits %d of %d", tab.bar, len(tab.edits), n)
	}
	if len(*evs) != 1 || !(*evs)[0].TxLost {
		t.Fatalf("events %+v", *evs)
	}
	if a.Update((*evs)[0]); a.toast != "doraemon：连接断开，事务已丢失" {
		t.Errorf("toast %q", a.toast)
	}
}
