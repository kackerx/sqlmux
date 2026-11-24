package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/db/duckdb"
)

// No libduckdb to load is a question of downloading it (F24.3): no says why it
// did not connect, yes downloads it, C-c cancels that, and once it is there
// the connection is tried again.
func TestNoLibduckdbAsksToDownload(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	into := filepath.Join(t.TempDir(), "duckdb", duckdb.PinnedVersion, "libduckdb.so")
	notFound := &duckdb.NotFound{Tried: []string{"/opt/homebrew/lib/libduckdb.dylib"}, Into: into}
	if _, ok := duckdb.Offer(notFound); !ok {
		t.Skip("no archive of libduckdb for this platform")
	}
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	failed := func() {
		c.State = connConnecting
		a.Update(connected{conn: c, err: notFound})
	}
	failed()
	if a.confirm == nil || !strings.Contains(a.confirm.text, into) || !strings.Contains(a.confirm.text, "duckdb-shared-libs-") {
		t.Fatalf("no question: %+v", a.confirm)
	}
	if a.run("confirm.no", 0); a.confirm != nil || !strings.Contains(a.toast, "找不到 libduckdb") || !strings.Contains(a.toast, "[duckdb] library") {
		t.Errorf("no: toast %q", a.toast)
	}
	failed()
	a.toast = ""
	if click(a, uv.Pos(1, 1)); a.confirm != nil || !strings.Contains(a.toast, "找不到 libduckdb") { // outside the box is no
		t.Errorf("a click outside: toast %q, box %v", a.toast, a.confirm != nil)
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
	if a.Update(libFetched{err: context.Canceled}); a.libFetch != nil || !strings.Contains(a.toast, "已取消下载") {
		t.Errorf("cancelled: toast %q", a.toast)
	}

	failed()
	a.run("confirm.yes", 0)
	if failed(); a.confirm != nil || len(a.libFetch.again) != 2 { // failing during the download: it waits for it
		t.Errorf("during the download: %+v", a.confirm)
	}
	c.State = connFailed
	if a.Update(libFetched{}); a.libFetch != nil || c.State != connConnecting {
		t.Error("downloaded: the connection is not tried again")
	}

	c.State = connConnecting
	a.Update(connected{conn: c, err: context.DeadlineExceeded}) // any other error is said, as it was
	if a.confirm != nil || !strings.Contains(a.toast, "deadline") {
		t.Errorf("another error: %+v, toast %q", a.confirm, a.toast)
	}
}

// The form's test asks the same, and tests again once the library is there;
// with the form closed meanwhile nothing is.
func TestNoLibduckdbInFormTest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	notFound := &duckdb.NotFound{Into: filepath.Join(t.TempDir(), "libduckdb.so")}
	if _, ok := duckdb.Offer(notFound); !ok {
		t.Skip("no archive of libduckdb for this platform")
	}
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>far")
	a.form.sel = fieldAdvanced
	feed(t, a, "<Space><Tab>postgres://u@127.0.0.1:1/d")
	a.form.testing = 7
	a.Update(connTested{seq: 7, err: notFound})
	if a.confirm == nil || !strings.Contains(a.form.result, "找不到 libduckdb") {
		t.Fatalf("no question: result %q", a.form.result)
	}
	a.run("confirm.yes", 0)
	if cmd := a.libFetch.again[0](); cmd == nil || a.form.testing == 0 {
		t.Error("not tested again")
	}
	a.form = nil
	if cmd := a.libFetch.again[0](); cmd != nil {
		t.Error("tested with no form")
	}
}
