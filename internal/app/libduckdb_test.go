package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db/duckdb"
)

// notFound is a NotFound that is offered a download on this platform; the
// test skips on one with no archive.
func notFound(t *testing.T) *duckdb.NotFound {
	t.Helper()
	nf := &duckdb.NotFound{Tried: []string{"/opt/homebrew/lib/libduckdb.dylib"}, Into: filepath.Join(t.TempDir(), "duckdb", duckdb.PinnedVersion, "libduckdb.so")}
	if _, ok := duckdb.Offer(nf); !ok {
		t.Skip("no archive of libduckdb for this platform")
	}
	return nf
}

// No libduckdb to load is a question of downloading it (F24.3): no, or a
// click outside, says why it did not connect, and where the manual way is;
// yes downloads it, C-c cancels that, and once it is there the connection is
// dialed again. The places looked in are in the log.
func TestNoLibduckdbAsksToDownload(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	nf := notFound(t)
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	failed := func() {
		c.State = connConnecting
		a.Update(connected{conn: c, err: nf, spec: c.Spec})
	}
	failed()
	if a.confirm == nil || !strings.Contains(a.confirm.text, nf.Into) || !strings.Contains(a.confirm.text, "duckdb-shared-libs-") || c.State != connConnecting {
		t.Fatalf("no question: %+v, state %v", a.confirm, c.State)
	}
	if l := a.win().log; len(l) == 0 || !strings.Contains(l[len(l)-1].Tail, "/opt/homebrew/lib/libduckdb.dylib") {
		t.Errorf("the places looked in are not logged: %+v", l)
	}
	if a.run("confirm.no", 0); a.confirm != nil || c.State != connFailed || !strings.Contains(a.toast, "找不到 libduckdb") || !strings.Contains(a.toast, "sqlmux#duckdb") {
		t.Errorf("no: toast %q, state %v", a.toast, c.State)
	}
	failed()
	a.toast = ""
	if click(a, uv.Pos(1, 1)); a.confirm != nil || !strings.Contains(a.toast, "找不到 libduckdb") { // outside the box is no
		t.Errorf("a click outside: toast %q", a.toast)
	}

	failed()
	a.run("confirm.yes", 0) // its Cmd, which would download, is not run
	if a.libFetch == nil {
		t.Fatal("yes: no download")
	}
	a.showToast("保存失败")
	if a.Update(libTick{a.libFetch}); a.toast != "保存失败" {
		t.Errorf("the download's toast over another: %q", a.toast)
	}
	if _, cmd := a.Update(libTick{&libFetch{}}); cmd != nil {
		t.Error("a tick of a download that is over goes on")
	}
	cancelled := false
	a.libFetch.cancel = func() { cancelled = true }
	if a.run("cancel", 0); !cancelled {
		t.Error("C-c does not cancel the download")
	}
	if a.Update(libFetched{err: context.Canceled}); a.libFetch != nil || c.State != connFailed || !strings.Contains(a.toast, "已取消下载") {
		t.Errorf("cancelled: toast %q, state %v", a.toast, c.State)
	}

	failed()
	a.run("confirm.yes", 0)
	if a.Update(libFetched{}); a.libFetch != nil || c.State != connConnecting {
		t.Error("downloaded: the connection is not dialed again")
	}

	c.State = connConnecting
	a.Update(connected{conn: c, err: context.DeadlineExceeded}) // any other error is said, as it was
	if a.confirm != nil || !strings.Contains(a.toast, "deadline") {
		t.Errorf("another error: %+v, toast %q", a.confirm, a.toast)
	}
}

// What waits for the download dials what it was (§8.1): the spec it was
// dialed with, an edit meanwhile the next dial's; the query tab that picked
// the connection still has it; one that fails meanwhile waits too, and a
// failed download fails them all, the command line's SQL for one dropped.
func TestLibduckdbWaitersKeepWhatTheyWere(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	nf := notFound(t)
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	q := a.queries()[0]
	first := c.Spec
	first.Engine, first.DSN = "sqlite", filepath.Join(t.TempDir(), "first.db")
	c.setSpec(first)
	c.State, q.want = connConnecting, c
	a.want = &startTable{conn: c, name: "insert into t values (1)", query: q} // --sql
	a.Update(connected{conn: c, err: nf, spec: first})
	a.run("confirm.yes", 0)
	second := first
	second.DSN = filepath.Join(t.TempDir(), "second.db")
	c.setConfig(second) // saveEditedConn's, for one connecting

	if q.want != c {
		t.Error("the query tab lost the connection it picked")
	}
	other := newConn(config.Connection{Name: "other", Engine: "duckdb", DSN: "y.duckdb"})
	a.sess.Conns = append(a.sess.Conns, other)
	other.State = connConnecting
	a.Update(connected{conn: other, err: nf, spec: other.Spec})
	if a.confirm != nil || len(a.libFetch.wants) != 2 {
		t.Fatalf("one failing during the download does not wait: %+v", a.confirm)
	}
	f := a.libFetch
	cmd := f.wants[0].again()
	if cmd == nil {
		t.Fatal("not dialed again")
	}
	if m, ok := cmd().(connected); !ok || m.spec.DSN != first.DSN {
		t.Errorf("dialed %+v, not the spec it was", m.spec)
	}

	a.Update(libFetched{err: context.DeadlineExceeded})
	if c.State != connFailed || other.State != connFailed || q.want != nil || a.want != nil || !strings.Contains(a.toast, "下载 libduckdb 失败") {
		t.Errorf("a failed download: %v %v, toast %q", c.State, other.State, a.toast)
	}
}

// A session dialed for a connection deleted meanwhile is not made, and does
// not crash.
func TestSessionDialedOfAConnectionGone(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	n := len(a.sessions)
	gone := config.Connection{Name: "gone", Engine: "duckdb", DSN: "x.duckdb"}
	a.Update(sessionDialed{name: "9", spec: gone, d: dialed{main: noDB{}, meta: noDB{}, spec: gone}})
	if len(a.sessions) != n {
		t.Error("a session of a connection that is not there")
	}
}

// The form's test asks the same, and tests again once the library is there:
// not when the form closed or its fields changed meanwhile. C-c then cancels
// the download, not the form.
func TestNoLibduckdbInFormTest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	nf := notFound(t)
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>far")
	a.form.sel = fieldAdvanced
	feed(t, a, "<Space><Tab>postgres://u@127.0.0.1:1/d")
	a.form.testing = 7
	a.Update(connTested{seq: 7, err: nf})
	if a.confirm == nil || !strings.Contains(a.form.result, "找不到 libduckdb") {
		t.Fatalf("no question: result %q", a.form.result)
	}
	a.run("confirm.yes", 0)
	again := a.libFetch.wants[0].again
	cancelled := false
	a.libFetch.cancel = func() { cancelled = true }
	if a.run("cancel", 0); !cancelled || a.form == nil {
		t.Errorf("C-c: cancelled %v, form %v", cancelled, a.form != nil)
	}
	if cmd := again(); cmd == nil || a.form.testing == 0 {
		t.Error("not tested again")
	}
	a.form.testing = 0
	a.form.result = toastLine(nf.Error())
	feed(t, a, "x") // its fields changed
	if cmd := again(); cmd != nil {
		t.Error("tested what the fields are now")
	}
	a.form = nil
	if cmd := again(); cmd != nil {
		t.Error("tested with no form")
	}
}
