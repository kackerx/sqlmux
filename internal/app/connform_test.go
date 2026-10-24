package app

import (
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// keychain puts a fake security and secret-tool first on PATH: what they
// are given they keep in $FAKE, the last password in $FAKE/pw, each item in
// its own file (kcItem), every call in $FAKE/log; or they refuse with
// "locked". PATH holds nothing else a script needs, and is bare with none:
// the tests never reach the real keychain.
func keychain(t *testing.T, mode string) (pw string) {
	t.Helper()
	bin, notes := t.TempDir(), t.TempDir()
	if mode != "none" { // the fakes, and the sh -c a password_cmd runs in
		for _, tool := range []string{"cat", "rm", "sh"} {
			if path, err := exec.LookPath(tool); err == nil {
				if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("FAKE", notes)
	if mode != "none" {
		scripts := map[string]string{
			"security": `cmd=$1; svc=; pw=
while [ $# -gt 0 ]; do case $1 in -s) svc=$2 ;; -w) pw=$2 ;; esac; shift; done
case $cmd in
add-generic-password) printf %s "$pw" >"$FAKE/svc-$svc"; printf %s "$pw" >"$FAKE/pw" ;;
find-generic-password) [ -f "$FAKE/svc-$svc" ] || exit 44; cat "$FAKE/svc-$svc" ;;
delete-generic-password) [ -f "$FAKE/svc-$svc" ] || exit 44; rm -f "$FAKE/svc-$svc" ;;
esac
`,
			"secret-tool": `cmd=$1; for a; do last=$a; done
case $cmd in
store) cat >"$FAKE/svc-$last"; cat "$FAKE/svc-$last" >"$FAKE/pw" ;;
lookup) [ -f "$FAKE/svc-$last" ] || exit 1; cat "$FAKE/svc-$last" ;;
clear) rm -f "$FAKE/svc-$last" ;;
esac
`}
		for name, script := range scripts {
			if mode == "locked" {
				script = "echo locked >&2; exit 1\n"
			}
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/bash\necho \"$*\" >>\"$FAKE/log\"\n"+script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return filepath.Join(notes, "pw")
}

// kcItem is the password the fake keychain keeps for connection name, and
// whether it keeps one; the item's own name is the tool's of this system.
func kcItem(name string) (string, bool) {
	file := "svc-" + name
	if runtime.GOOS == "darwin" {
		file = "svc-sqlmux-" + name
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("FAKE"), file))
	return string(b), err == nil
}

// kcLog is what the fake keychain was asked, a call a line.
func kcLog() []string {
	b, _ := os.ReadFile(filepath.Join(os.Getenv("FAKE"), "log"))
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func connections() string {
	data, _ := os.ReadFile(filepath.Join(config.Dir(), "connections.toml"))
	return string(data)
}

// The password field draws a * for each character and the password goes
// into the keychain: connections.toml holds the command that reads it, not
// the password (§13, F7.3).
func TestConnFormPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the keychain fakes are /bin/sh scripts")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pw := keychain(t, "ok")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab>secret")
	if view := a.render().String(); strings.Contains(view, "secret") || !strings.Contains(view, "******") {
		t.Errorf("the password is on screen:\n%s", view)
	}
	golden.RequireEqual(t, a.render().String())
	feed(t, a, "<Tab>d<CR>")
	got, _ := os.ReadFile(pw)
	file := connections()
	// quoted: the command, secret-tool, has the word in it
	if a.form != nil || string(got) != "secret" || strings.Contains(file, `"secret"`) || !strings.Contains(file, "password_cmd = ") || len(a.sess.Conns) != 2 {
		t.Errorf("saved: form %v, keychain %q, conns %d, file:\n%s", a.form, got, len(a.sess.Conns), file)
	}
}

// With no keychain tool the password goes into connections.toml, 0600, and
// the toast says so; one that fails keeps the form, its complaint on the
// error line, nothing saved; a password beside a password_cmd is asked
// about; no password stores nothing.
func TestConnFormPasswordElsewhere(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab>secret<Tab>d<CR>")
	info, err := os.Stat(filepath.Join(config.Dir(), "connections.toml"))
	if err != nil || a.form != nil || !strings.Contains(connections(), `password = "secret"`) || (info.Mode().Perm() != 0o600 && runtime.GOOS != "windows") || a.toast != "没有钥匙串工具，密码以明文存进了 connections.toml" { // windows has no group/other bits
		t.Errorf("no tool: form %v, err %v, toast %q, file:\n%s", a.form, err, a.toast, connections())
	}

	if runtime.GOOS == "windows" { // the keychain fakes from here on are /bin/sh scripts
		t.Skip("the keychain flows")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "locked")
	a, _ = withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab>secret<Tab>d<CR>")
	if a.form == nil || !strings.HasSuffix(a.form.err, ": locked") || connections() != "" || len(a.sess.Conns) != 1 {
		t.Errorf("a keychain that fails: form %v, file:\n%s", a.form, connections())
	}
	feed(t, a, "<C-u><CR>") // cleared: no password, nothing to store
	if a.form != nil || strings.Contains(connections(), "password") || len(a.sess.Conns) != 2 {
		t.Errorf("cleared: form %v, file:\n%s", a.form, connections())
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "ok")
	a, _ = withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab>secret<Tab>d<Tab><Tab><Tab> <Tab><Tab>pass show x<CR>")
	if a.form == nil || a.form.sel != fieldPassword || a.form.err != "password 和 password_cmd 填一个就够了" || connections() != "" {
		t.Errorf("two sources: form %+v, file:\n%s", a.form, connections())
	}
}

// The engine sets the port while it is not typed in; the fields host, user
// and database are asked for, the port is a number; the form says which one
// and gives it the keys (§7.8).
func TestConnFormFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	port := func() string { return a.form.fields[fieldPort].Input.Text }
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Space>")
	if port() != "3306" {
		t.Errorf("mysql: port %q", port())
	}
	feed(t, a, "<Left>")
	if port() != "5432" {
		t.Errorf("postgres again: port %q", port())
	}
	feed(t, a, "<Tab><Tab>1<S-Tab><S-Tab><Right>") // a typed port stays: host, port ("54321"), back to the engine
	if a.form.sel != fieldEngine || port() != "54321" {
		t.Errorf("typed: on %d, port %q", a.form.sel, port())
	}
	for _, c := range []struct {
		keys, err string
		sel       int
	}{
		{"<CR>", "缺少 host", fieldHost},
		{"h<CR>", "缺少 user", fieldUser},
		{"u<CR>", "缺少 database", fieldDatabase},
	} {
		feed(t, a, c.keys)
		if a.form == nil || a.form.err != c.err || a.form.sel != c.sel || !a.form.fields[c.sel].Bad {
			t.Fatalf("%q: %+v", c.keys, a.form)
		}
	}
	feed(t, a, "d<S-Tab><S-Tab><S-Tab><C-u>99999<CR>")
	if a.form.err != "port 要是 1–65535 的数字" || a.form.sel != fieldPort {
		t.Fatalf("a port too big: %+v", a.form)
	}
}

// The 高级 fold holds dsn, password_cmd and ssh: closed, they are out of the
// way; open, a dsn typed there is the connection and host, port, user and
// database are dim and passed by Tab; password_cmd is its password.
func TestConnFormAdvanced(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<S-Tab>") // round to the last row that shows: the fold
	if a.form.sel != fieldAdvanced {
		t.Fatalf("closed: S-Tab from name is on %d", a.form.sel)
	}
	feed(t, a, "<Space><Tab>postgres://u@h/d<Tab>printf pw")
	if !a.form.dsnGiven() || !a.form.skipped(fieldHost) {
		t.Fatal("host is not dim with a dsn")
	}
	feed(t, a, "<Tab>")
	if a.form.sel != fieldSSH {
		t.Errorf("past password_cmd: on %d", a.form.sel)
	}
	feed(t, a, "<Tab>") // round to name
	if a.form.sel != fieldName {
		t.Errorf("Tab round the end: on %d", a.form.sel)
	}
	feed(t, a, "<Tab>")
	if a.form.sel != fieldGroup {
		t.Errorf("name, then the group: on %d", a.form.sel)
	}
	feed(t, a, "<Tab>")
	if a.form.sel != fieldEngine {
		t.Errorf("then the engine: on %d", a.form.sel)
	}
	feed(t, a, "<Tab>")
	if a.form.sel != fieldPassword {
		t.Errorf("past the dim host, port and user: on %d", a.form.sel)
	}
	feed(t, a, "<S-Tab><S-Tab>")
	golden.RequireEqual(t, a.render().String())
	feed(t, a, "<CR>")
	if a.form != nil || connections() != "[[connection]]\nname = \"play\"\nengine = \"postgres\"\ndsn = \"postgres://u@h/d\"\npassword_cmd = \"printf pw\"\n" {
		t.Errorf("saved: form %v, file:\n%s", a.form, connections())
	}
}

// C-t checks the form as saving does, then connects once with what it says
// and shows how it went on the row above the buttons; nothing is saved, and
// typing or a newer test makes a result that comes late no one's (F12.5).
func TestConnFormTest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>far")
	_, cmd := a.Update(teaKey("<C-t>"))
	if cmd != nil || !strings.Contains(a.form.err, "host") || a.form.sel != fieldHost {
		t.Fatalf("a form that does not check is not tested: %v, %q on %d", cmd != nil, a.form.err, a.form.sel)
	}
	a.form.sel = fieldAdvanced
	feed(t, a, "<Space><Tab>postgres://u@127.0.0.1:1/d?connect_timeout=1")
	_, cmd = a.Update(teaKey("<C-t>"))
	if cmd == nil || a.form.testing == 0 || !strings.Contains(a.render().String(), "测试中…") || !strings.Contains(a.render().String(), "[ 测试 ]") {
		t.Fatalf("testing: %v, %d", cmd != nil, a.form.testing)
	}
	a.Update(cmd())
	if a.form.resultOK || a.form.testing != 0 || !strings.Contains(a.form.result, "127.0.0.1:1") || a.form.err != "" {
		t.Errorf("refused: result %q, ok %v, err %q", a.form.result, a.form.resultOK, a.form.err)
	}
	if connections() != "" {
		t.Errorf("a test saved: %s", connections())
	}
	feed(t, a, "<Tab>") // moving between fields leaves it
	if !strings.Contains(a.form.result, "127.0.0.1:1") {
		t.Errorf("Tab cleared a failed test: %q", a.form.result)
	}
	feed(t, a, "x")
	if a.form.result != "" {
		t.Errorf("typing clears the result: %q", a.form.result)
	}
	// a result of a test the fields have left behind is dropped, one that is the form's own shown
	_, cmd = a.Update(teaKey("<C-t>"))
	late := cmd()
	feed(t, a, "x")
	a.Update(late)
	if a.form.result != "" || a.form.testing != 0 {
		t.Errorf("a late result: %q", a.form.result)
	}
	a.form.testing = 7
	a.Update(connTested{seq: 7, note: "已连接 · PostgreSQL 17.7 · 120ms"})
	if a.form.result != "已连接 · PostgreSQL 17.7 · 120ms" || a.form.err != "" || !a.form.resultOK {
		t.Errorf("connected: result %q, err %q", a.form.result, a.form.err)
	}
	golden.RequireEqual(t, a.render().String())
	a.form.err = "save failed" // an error of a save is no result: not drawn as one
	if v := a.formView(); v.OK || v.Err != "save failed" {
		t.Errorf("an error over a result: %+v", v)
	}
	a.form.err = ""
	a.form = nil
	a.Update(connTested{seq: 7, note: "late"}) // no form: nothing to show it on
}

// What the fields say changing, by typing, by the engine, by a group taken
// from the list, by going to another connection, drops a test's result and
// the test still out (F12.5).
func TestConnFormUntest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := withSessions()
	a.sess.Conns[0].Spec.Group = "work"
	a.run("conn.new", 0)
	f := a.form
	shown := func() { f.result, f.resultOK, f.testing = "已连接", true, 3 }
	for name, change := range map[string]func(){
		"typing":  func() { feed(t, a, "x") },
		"engine":  func() { a.formSegment(fieldEngine, 1) },
		"group":   func() { f.sel = fieldGroup; feed(t, a, "w"); shown(); a.groupAccept() },
		"another": func() { a.selectConn(0) },
	} {
		f = a.form
		shown()
		change()
		if f = a.form; f.result != "" || f.resultOK || f.testing != 0 {
			t.Errorf("%s: result %q, ok %v, testing %d", name, f.result, f.resultOK, f.testing)
		}
		a.selectConn(len(a.sess.Conns))
	}
}

func TestVersionText(t *testing.T) {
	for _, c := range []struct{ engine, v, want string }{
		{"postgres", "PostgreSQL 17.11 (Debian 17.11-1.pgdg13+2) on aarch64-unknown-linux-gnu, compiled by gcc", "PostgreSQL 17.11"},
		{"postgres", "PostgreSQL 16.2 on x86_64-pc-linux-gnu", "PostgreSQL 16.2"},
		{"mysql", "8.4.0", "MySQL 8.4.0"},
	} {
		if got := versionText(c.engine, c.v); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.engine, c.v, got, c.want)
		}
	}
}

// An empty dsn field shows the form of a dsn, the engine's; a typed one hides it (F12.5).
func TestDSNExample(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>x<S-Tab><Space><Tab>")
	if !strings.Contains(a.render().String(), dsnExample["postgres"]) {
		t.Error("no example under postgres")
	}
	a.formSegment(fieldEngine, 1)
	a.form.sel = fieldDSN
	if s := a.render().String(); !strings.Contains(s, dsnExample["mysql"]) || strings.Contains(s, dsnExample["postgres"]) {
		t.Error("the example does not follow the engine")
	}
	feed(t, a, "m")
	if strings.Contains(a.render().String(), dsnExample["mysql"]) {
		t.Error("the example stays under what is typed")
	}
}

func TestComposeDSN(t *testing.T) {
	for _, c := range []struct{ engine, host, port, user, db, want string }{
		{"postgres", "localhost", "5432", "u", "d", "postgres://u@localhost:5432/d?sslmode=prefer"},
		{"mysql", "db.example", "3306", "u", "d", "mysql://u@db.example:3306/d"},
		{"postgres", "::1", "5432", "p@t:x", "d b", "postgres://p%40t%3Ax@[::1]:5432/d%20b?sslmode=prefer"},
	} {
		if got := composeDSN(c.engine, c.host, c.port, c.user, c.db); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

// A host with a port in it is told so, an IPv6 one may have its brackets;
// without a keychain tool a password holding ${ is not saved, dial would take
// it for a variable (§13).
func TestConnFormHostAndPlainPassword(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>db.internal:5433<Tab><Tab>u<Tab>pa${ss}<Tab>d<CR>")
	if a.form == nil || a.form.sel != fieldHost || a.form.err != "host 里不要带端口，端口在下面" {
		t.Fatalf("host with a port: %+v", a.form)
	}
	feed(t, a, "<C-u>[::1]<CR>")
	if a.form == nil || a.form.sel != fieldPassword || connections() != "" {
		t.Fatalf("a ${ in a plain password: %+v\n%s", a.form, connections())
	}
	feed(t, a, "<C-u>plain<CR>")
	if a.form != nil || !strings.Contains(connections(), `dsn = "postgres://u@[::1]:5432/d?sslmode=prefer"`) {
		t.Errorf("saved: %v\n%s", a.form, connections())
	}
}

// The first connection gives its query to the window the program started
// with; the windows made before it are one empty pane each, as any new one
// (F7.4, F8.18).
func TestNoConnectionFirstQueries(t *testing.T) {
	keychain(t, "none")
	a := emptyApp(t)
	a.form = nil
	feed(t, a, "<Space>c<Space>c") // two more windows
	a.run("conn.new", 0)
	feed(t, a, "play<Tab><Tab><Tab>h<Tab><Tab>u<Tab><Tab>d<CR>")
	if len(a.sess.Windows) != 3 {
		t.Fatalf("%d windows", len(a.sess.Windows))
	}
	if c := queryOf(a.sess.Windows[0].pane(2)); c == nil || c.conn != a.sess.start {
		t.Errorf("the first window without its query: %+v", c)
	}
	for _, w := range a.sess.Windows[1:] {
		if len(w.Root.Leaves()) != 1 || len(queriesIn(w.Root.Leaves()...)) != 0 {
			t.Errorf("a window made before has %d panes and %d queries", len(w.Root.Leaves()), len(queriesIn(w.Root.Leaves()...)))
		}
	}
}

// The form stays out of the keychain when the file would not take the
// connection: a name the file has since got is refused before its password
// is stored over the other's (F7.3).
func TestConnFormKeychainAfterCheck(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	pw := keychain(t, "ok")
	if err := os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o700); err != nil {
		t.Fatal(err)
	}
	theirs := "[[connection]]\nname = \"play\"\nengine = \"postgres\"\ndsn = \"postgres://x@h/d\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", "connections.toml"), []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _ := withSessions() // the tree never had play
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab>secret<Tab>d<CR>")
	if _, err := os.Stat(pw); err == nil || a.form == nil || !strings.Contains(a.form.err, "play") || connections() != theirs {
		t.Errorf("keychain written (%v) or file changed, form %v:\n%s", err, a.form, connections())
	}
}

// A plain password changes no permissions of a file the user has: 0644 stays
// (§13); a file the form makes is 0600.
func TestConnFormPlainPasswordMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	keychain(t, "none")
	path := filepath.Join(dir, "sqlmux", "connections.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[[connection]]\nname = \"doraemon\"\nengine = \"postgres\"\ndsn = \"postgres://x@h/d\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab>secret<Tab>d<CR>")
	if info, err := os.Stat(path); err != nil || a.form != nil || (info.Mode().Perm() != 0o644 && runtime.GOOS != "windows") { // windows has no group/other bits
		t.Errorf("mode %v, %v, form %v", info.Mode(), err, a.form)
	}
}

// The port is the engine's default while it is empty or still that: a port
// typed in stays, one put back or cleared follows again; +5432 is saved as
// 5432.
func TestConnFormPortFollows(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	port := func() string { return a.form.fields[fieldPort].Input.Text }
	feed(t, a, "<C-p>conn.new<CR><Tab><Tab>") // on the engine
	for _, c := range []struct{ keys, want, what string }{
		{"<Space>", "3306", "mysql's"},
		{"<Tab><Tab><C-u>3307<S-Tab><S-Tab><Left>", "3307", "a typed port stays"},
		{"<Tab><Tab><C-u>5432<S-Tab><S-Tab><Space>", "3306", "postgres's put back follows"},
		{"<Tab><Tab><C-u><S-Tab><S-Tab><Left>", "5432", "a cleared one follows"},
	} {
		if feed(t, a, c.keys); port() != c.want {
			t.Fatalf("%s: %q", c.what, port())
		}
	}
	feed(t, a, "<Tab>h<Tab><C-u>+5432<Tab>u<Tab><Tab>d<S-Tab><S-Tab><S-Tab><S-Tab><S-Tab><S-Tab><S-Tab>play<CR>")
	if a.form != nil || !strings.Contains(connections(), `dsn = "postgres://u@h:5432/d?sslmode=prefer"`) {
		t.Errorf("form %v, file:\n%s", a.form, connections())
	}
}

// layout.open on a start with no connection says so, and does not open the
// list of layouts whose connections there are none of (F7.4).
func TestNoConnectionLayoutOpen(t *testing.T) {
	a := emptyApp(t)
	a.form = nil
	if err := os.MkdirAll(config.StateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LastPath(), []byte("version = 1\n[[session]]\nname = \"s\"\nconnection = \"c\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	feed(t, a, "<C-p>layout.open<CR><CR>")
	if a.drop != nil || a.toast != "还没有连接，先新建一个" {
		t.Errorf("drop %v, toast %q", a.drop, a.toast)
	}
}

// colAt is the column of text in the screen row y.
func colAt(t *testing.T, f *ui.Frame, y int, text string) int {
	t.Helper()
	row := strings.Split(f.String(), "\n")[y]
	i := strings.Index(row, text)
	if i < 0 {
		t.Fatalf("no %q in row %d: %q", text, y, row)
	}
	return ui.Width(row[:i])
}

// The form's colors: the name of the field with the keys is fg, the others
// dim, the one that is wrong error; a field that takes no part has dim text;
// the segment that is on has the select background, the other the sep one.
func TestConnFormColors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab><Tab>")
	th := a.theme
	row := func(field int) int { return find(t, a, ui.Target{Kind: ui.KindRow, I: field}).Min.Y }
	f := a.render()
	name := func(field int, label string) color.Color {
		return f.Buf.CellAt(colAt(t, f, row(field), label), row(field)).Style.Fg
	}
	if name(fieldName, "name") != th.Dim || name(fieldPort, "port") != th.Fg || name(fieldHost, "host") != th.Dim {
		t.Errorf("names: name %v, port %v, host %v", name(fieldName, "name"), name(fieldPort, "port"), name(fieldHost, "host"))
	}
	seg := func(text string) color.Color {
		return f.Buf.CellAt(colAt(t, f, row(fieldEngine), text), row(fieldEngine)).Style.Bg
	}
	if seg("postgres") != th.Select || seg("mysql") != th.Sep {
		t.Errorf("segments: postgres %v, mysql %v", seg("postgres"), seg("mysql"))
	}
	feed(t, a, "<CR>") // host is empty: wrong
	f = a.render()
	if name(fieldHost, "host") != th.Error {
		t.Errorf("a field that is wrong: %v", name(fieldHost, "host"))
	}
	feed(t, a, "h<S-Tab><S-Tab><S-Tab><Space><Tab>postgres://u@h/d") // host typed, then round to the fold, open, a dsn
	f = a.render()
	if fg := f.Buf.CellAt(colAt(t, f, row(fieldHost), "h"), row(fieldHost)).Style.Fg; fg != th.Dim {
		t.Errorf("host with a dsn given: its text is %v, not dim", fg)
	}
}

// Clicks: a row that takes no part is not given the keys, a check and the
// fold turn over, a segment is picked by its own place, the buttons save and
// cancel, and the ＋ of a tree with connections opens the form (§7.8).
func TestConnFormClicks(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	sb := a.sidebarRect()
	click(a, uv.Pos(sb.Max.X-3, sb.Min.Y+1)) // the ＋, by its place
	if a.form == nil {
		t.Fatal("the ＋ opens no form")
	}
	row := func(field int) uv.Position {
		return find(t, a, ui.Target{Kind: ui.KindRow, I: field}).Min.Add(uv.Pos(2, 0))
	}
	click(a, row(fieldReadOnly))
	if a.form.fields[fieldReadOnly].On != 1 || a.form.sel != fieldReadOnly {
		t.Errorf("read_only: %+v", a.form.fields[fieldReadOnly])
	}
	click(a, row(fieldAdvanced))
	if !a.form.advanced() {
		t.Error("the fold does not open")
	}
	feed(t, a, "<Tab>postgres://u@h/d")
	a.form.sel = fieldName
	click(a, row(fieldHost)) // dim now
	if a.form.sel != fieldName {
		t.Errorf("a dim row took the keys: %d", a.form.sel)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindSegment, Pane: fieldEngine, I: 1}).Min)
	if a.form.fields[fieldEngine].On != 1 || a.form.fields[fieldPort].Input.Text != "3306" {
		t.Errorf("mysql by its segment: %+v", a.form.fields[fieldEngine])
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindSegment, Pane: fieldEngine, I: 1}).Min)
	if a.form.fields[fieldEngine].On != 1 {
		t.Error("a click on the segment that is on changes it")
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "form.save"}).Min)
	if a.form == nil || a.form.err != "缺少 name" {
		t.Errorf("save: %+v", a.form)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "form.cancel"}).Min)
	if a.form != nil {
		t.Error("cancel leaves the form open")
	}
}

// What is typed in the fold is not taken when the fold is closed again.
func TestConnFormClosedFoldIgnored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab><Tab>d<Tab><Tab> ")
	feed(t, a, "<Tab>mysql://x@y/z<Tab>pass show x<S-Tab><S-Tab> ") // typed, then closed again
	if a.form.advanced() {
		t.Fatal("the fold did not close")
	}
	feed(t, a, "<CR>")
	if file := connections(); a.form != nil || strings.Contains(file, "mysql://x@y/z") || strings.Contains(file, "password_cmd") || !strings.Contains(file, "postgres://u@h:5432/d") {
		t.Errorf("form %v, file:\n%s", a.form, file)
	}
}

// A connections.toml that cannot be appended to is found out before the
// password goes into the keychain: the form says why, the keychain is not
// asked, the file is as it was (F8.16).
func TestConnFormUnwritableFile(t *testing.T) {
	a := managed(t)
	path := filepath.Join(config.Dir(), "connections.toml")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	a.form = nil
	feed(t, a, "<C-p>conn.new<CR>ro1<Tab><Tab><Tab>h<Tab><Tab>u<Tab>pw-ro<Tab>d<CR>")
	if a.form == nil || a.form.err == "" || connections() != managerFile || len(kcCalled("add-generic-password", "store")) != 0 {
		t.Errorf("form %v, file changed %v, keychain asked %q", a.form != nil, connections() != managerFile, kcLog())
	}
}

// The form's buttons and the keys under it show whole at 80×24, in either
// language, for a new connection and for one to edit, which has a button
// more: the keys beside the buttons when they fit, on the row under them,
// right-aligned, when they do not (F7.4, F8.16).
func TestConnFormHints(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		for _, w := range []int{160, 80} {
			for _, edit := range []bool{false, true} {
				i18n.Set(lang)
				a := managed(t)
				a.Update(tea.WindowSizeMsg{Width: w, Height: 24})
				a.run("conn.new", 0)
				if edit {
					a.run("conn.manage", 0)
				}
				var hints []string
				for _, h := range a.formView().Hints {
					hints = append(hints, h.Key+" "+h.Label)
				}
				keys := strings.Join(hints, " · ")
				lines := strings.Split(a.render().String(), "\n")
				at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "[ "+a.formView().Save+" ]") })
				i18n.Set("zh")
				beside := w == 160 && !(edit && lang == "en") // four buttons in English leave the keys 29 of the form's 71
				switch {
				case at < 0:
					t.Errorf("%s %d edit %v: no buttons", lang, w, edit)
				case beside && (!strings.Contains(lines[at], keys) || strings.Contains(lines[at+1], keys)):
					t.Errorf("%s %d edit %v: the keys are not beside the buttons: %q", lang, w, edit, lines[at])
				case !beside && (strings.Contains(lines[at], keys) || !strings.Contains(lines[at+1], keys)):
					t.Errorf("%s %d edit %v: the keys are not under the buttons: %q / %q", lang, w, edit, lines[at], lines[at+1])
				case !beside && !strings.HasPrefix(lines[at+1][strings.Index(lines[at+1], keys)+len(keys):], " │"):
					t.Errorf("%s %d edit %v: the keys are not right-aligned: %q", lang, w, edit, lines[at+1])
				}
			}
		}
	}
}

// Every width from 70 to 110 (from 75 in English, whose four buttons of an
// edit take 40 of the form's width less 33, F12.5), zh and en, new / list / edit: the buttons
// whole, the keys whole on the buttons' row or the one under it and on
// one of them only, the box's bottom border right after (F8.16).
func TestConnFormHintsAtEveryWidth(t *testing.T) {
	defer i18n.Set("zh")
	for _, lang := range []string{"zh", "en"} {
		for w := map[string]int{"zh": 70, "en": 75}[lang]; w <= 110; w++ {
			i18n.Set(lang)
			a := managed(t)
			a.Update(tea.WindowSizeMsg{Width: w, Height: 24})
			check := func(state string, edit bool) {
				fv := a.formView()
				var hs []string
				for _, h := range fv.Hints {
					hs = append(hs, h.Key+" "+h.Label)
				}
				keys := strings.Join(hs, " · ")
				lines := strings.Split(a.render().String(), "\n")
				at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "[ "+fv.Save+" ]") })
				if at < 0 {
					t.Errorf("%s %d %s: no buttons", lang, w, state)
					return
				}
				bs := []string{fv.Save, fv.Test, fv.Cancel}
				if edit {
					bs = append(bs, fv.Delete)
				}
				for _, b := range bs {
					if !strings.Contains(lines[at], "[ "+b+" ]") {
						t.Errorf("%s %d %s: button %q broken: %q", lang, w, state, b, lines[at])
					}
				}
				on := at
				if !strings.Contains(lines[at], keys) {
					on = at + 1
				}
				if !strings.Contains(lines[on], keys) {
					t.Errorf("%s %d %s: keys not whole: %q / %q", lang, w, state, lines[at], lines[at+1])
				}
				if !strings.Contains(lines[on+1], "└─") {
					t.Errorf("%s %d %s: not the bottom border after the keys: %q", lang, w, state, lines[on+1])
				}
			}
			a.run("conn.new", 0)
			check("new", false)
			a.form.testing = 1 // a test is out: the button is the same, the row says
			check("new, testing", false)
			a.form = nil
			a.run("conn.manage", 0)
			check("list", true)
			feed(t, a, "<Tab>")
			check("edit", true)
			a.form.testing = 1
			check("edit, testing", true)
		}
	}
}

// A click on a row of the manager's list gives the list the keys, as the
// form's field had them (F8.16, F13.13).
func TestConnListClickHasTheKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.openConnForm()
	if a.form.list {
		t.Fatal("the new form has the keys")
	}
	a.View()
	var at *uv.Rectangle
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindConn {
			r := h.Rect
			at = &r
			break
		}
	}
	if at == nil {
		t.Fatal("no row in the list")
	}
	click(a, at.Min)
	if !a.form.list {
		t.Error("a click on a row of the list leaves the keys to the form")
	}
}

// SQLite's form is its file and no more: host, port, user and password are
// not there, Tab goes from the engine to the file, the dsn is the file (F14.1).
func TestConnFormSQLite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>lite<Tab><Tab><Right><Right>")
	v := a.formView()
	hidden := func(i int) bool { return v.Fields[i].Hidden }
	if !hidden(fieldHost) || !hidden(fieldPort) || !hidden(fieldUser) || !hidden(fieldPassword) || hidden(fieldDatabase) || v.Fields[fieldDatabase].Label != "file" {
		t.Fatalf("the form of a file: host %v port %v user %v password %v, file %v %q", hidden(fieldHost), hidden(fieldPort), hidden(fieldUser), hidden(fieldPassword), hidden(fieldDatabase), v.Fields[fieldDatabase].Label)
	}
	if view := a.render().String(); regexp.MustCompile(`\b(host|port|user|password)\b`).MatchString(view) {
		t.Fatalf("the form of a file has the others' rows:\n%s", view)
	}
	feed(t, a, "<Tab>~/data.db?mode=ro<CR>")
	if got := connections(); !strings.Contains(got, `engine = "sqlite"`) || !strings.Contains(got, `dsn = "~/data.db?mode=ro"`) {
		t.Fatalf("connections.toml:\n%s", got)
	}
	// edited, the file is in its field
	if _, _, _, db := splitDSN("sqlite", "/a/b.db?mode=ro"); db != "/a/b.db?mode=ro" {
		t.Errorf("file %q", db)
	}
	if got := versionText("sqlite", "3.53.4"); got != "SQLite 3.53.4" {
		t.Errorf("version %q", got)
	}
	// no file, no connection
	b, _ := withSessions()
	feed(t, b, "<C-p>conn.new<CR>nofile<Tab><Tab><Right><Right><CR>")
	if b.form == nil || b.form.err == "" {
		t.Errorf("a file asked for: %+v", b.form)
	}
}

// The password a form kept from another engine is no password of a file: SQLite's
// form saves none, to the keychain or to the file (F14.1).
func TestConnFormSQLiteKeepsNoPassword(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "ok")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>")
	set := func(field int, text string) { a.form.fields[field].Input = ui.Input{Text: text, Pos: len(text)} }
	set(fieldName, "lite")
	set(fieldPassword, "secret") // typed under postgres
	a.formSegment(fieldEngine, 2)
	set(fieldDatabase, "/tmp/x.db")
	a.saveConnForm()
	got := connections()
	if !strings.Contains(got, `engine = "sqlite"`) || strings.Contains(got, "password") || strings.Contains(got, "secret") {
		t.Errorf("connections.toml:\n%s", got)
	}
	if _, ok := kcItem("lite"); ok || len(kcLog()) > 1 {
		t.Errorf("the keychain: %v", kcLog())
	}
}

// A connection of the command line is no row of the manager, to edit or delete
// or open as; the connections of the file are, from the first (F14.1).
func TestManagerHidesTempConnection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, _ := withSessions()
	temp := newConn(config.Connection{Name: "x", Engine: "sqlite", DSN: "/tmp/x.db", Temp: true})
	a.sess.Conns = append([]*Conn{temp}, a.sess.Conns...)
	a.openConnManager()
	m := a.managerView()
	for _, r := range m.Rows {
		if r.Name == "x" {
			t.Fatalf("the manager lists the temporary connection: %+v", m.Rows)
		}
	}
	if len(m.Rows) != len(a.sess.Conns) { // the ones of the file and the row that makes a new one
		t.Errorf("rows %d for %d connections", len(m.Rows), len(a.sess.Conns))
	}
	if a.form.cur != 0 || a.form.was == nil || a.form.was.Name == "x" {
		t.Errorf("the form is on %+v", a.form.was)
	}
	for range 5 {
		a.connsMove(1)
	}
	if a.form.cur != len(a.managed()) {
		t.Errorf("the last row is %d, not past the temporary one", a.form.cur)
	}
}

// Redis has the default user and db 0, so its form needs a host alone: the port
// follows the engine, the db row is called db and is a number, and what is
// typed makes the URL (F16.1).
func TestConnFormRedis(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	engine := "<C-p>conn.new<CR>cache<Tab><Tab><Right><Right><Right>" // postgres, mysql, sqlite, redis
	a, _ := withSessions()
	feed(t, a, engine)
	if v := a.formView(); v.Fields[fieldDatabase].Label != "db" || a.form.fields[fieldPort].Input.Text != "6379" || v.Fields[fieldHost].Hidden || v.Fields[fieldUser].Hidden {
		t.Fatalf("the form of redis: label %q port %q", v.Fields[fieldDatabase].Label, a.form.fields[fieldPort].Input.Text)
	}
	feed(t, a, "<Tab>h<CR>")
	if got := connections(); a.form != nil || !strings.Contains(got, `engine = "redis"`) || !strings.Contains(got, `dsn = "redis://h:6379/0"`) {
		t.Fatalf("a host alone: form %v, file:\n%s", a.form, got)
	}
	b, _ := withSessions()
	feed(t, b, strings.Replace(engine, "cache", "cache2", 1)+"<Tab>h<Tab><Tab>u<Tab><Tab>3<CR>")
	if got := connections(); !strings.Contains(got, `dsn = "redis://u@h:6379/3"`) {
		t.Fatalf("a user and a db:\n%s", got)
	}
	c, _ := withSessions()
	feed(t, c, strings.Replace(engine, "cache", "cache3", 1)+"<Tab>h<Tab><Tab><Tab><Tab>x<CR>")
	if c.form == nil || c.form.err == "" || !c.form.fields[fieldDatabase].Bad {
		t.Errorf("a db that is no number: %+v", c.form)
	}
	feed(t, c, "<C-u>-1<CR>")
	if c.form == nil || !c.form.fields[fieldDatabase].Bad {
		t.Errorf("a db below 0: %+v", c.form)
	}
	// a URL is split back into the rows, and written with no user when there is none
	if host, port, user, database := splitDSN("redis", "redis://u:p@h:6380/3"); host != "h" || port != "6380" || user != "u" || database != "3" {
		t.Errorf("split: %q %q %q %q", host, port, user, database)
	}
	if got := composeDSN("redis", "h", "6379", "", "0"); got != "redis://h:6379/0" {
		t.Errorf("no user: %q", got)
	}
	if got := versionText("redis", "# Server\r\nredis_version:7.4.0\r\nredis_mode:standalone\r\n"); got != "Redis 7.4.0" {
		t.Errorf("version %q", got)
	}
	if dsnExample["redis"] == "" || defaultPort["redis"] != "6379" {
		t.Error("no example or port of redis")
	}
}

// The form has a switch for production: ticked, read only is ticked with it, and
// can be unticked; the connection is written so that it reads back as it was
// made, a writable one saying read_only = false (F17.9, §13).
func TestConnFormProduction(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	keychain(t, "none")
	a, _ := withSessions()
	feed(t, a, "<C-p>conn.new<CR>play<Tab><Tab><Tab>h<Tab><Tab>u<Tab><Tab>d<Tab><Tab><Space>")
	if f := a.form; f.sel != fieldProduction || f.fields[fieldProduction].On != 1 || f.fields[fieldReadOnly].On != 1 {
		t.Fatalf("ticked: sel %d, production %d, read only %d", f.sel, f.fields[fieldProduction].On, f.fields[fieldReadOnly].On)
	}
	feed(t, a, "<CR>")
	if got := connections(); a.form != nil || !strings.Contains(got, "production = true\n") || !strings.Contains(got, "read_only = true\n") {
		t.Fatalf("saved: form %v, file:\n%s", a.form, got)
	}
	if c := a.sess.Conns[len(a.sess.Conns)-1]; c.Spec.Name != "play" || !c.Spec.Production || !c.Spec.ReadOnly {
		t.Errorf("the connection: %+v", c.Spec)
	}
	// writable, in the manager: read_only = false is written
	feed(t, a, "<C-p>conn.manage<CR>")
	for range len(a.managed()) - 1 {
		feed(t, a, "j")
	}
	feed(t, a, "<CR>") // into the form of play
	if f := a.form; f.list || f.fields[fieldProduction].On != 1 || f.fields[fieldReadOnly].On != 1 {
		t.Fatalf("the form of play: %+v", f)
	}
	a.form.sel = fieldReadOnly
	feed(t, a, "<Space><CR>")
	got := connections()
	if !strings.Contains(got, "production = true\nread_only = false\n") && !strings.Contains(got, "read_only = false\nproduction = true\n") {
		t.Errorf("writable:\n%s", got)
	}
	if c, err := config.ReadConnection("play"); err != nil || !c.Production || c.ReadOnly {
		t.Errorf("read back: %+v %v", c, err)
	}
	// the switch of a connection that is made: off and on, each saved (the form stays open)
	for _, on := range []bool{false, true} {
		a.form.sel = fieldProduction
		feed(t, a, "<Space><CR>")
		if c, err := config.ReadConnection("play"); err != nil || c.Production != on {
			t.Errorf("production switched to %v: %+v %v", on, c, err)
		}
	}
}
