package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// connections_test.go and keychain_test.go read the Chinese texts: the
// language they run in is not the default, English (F7.6).
func init() { i18n.Set("zh") }

// connectionsFile writes connections.toml under a fresh XDG_CONFIG_HOME.
func connectionsFile(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(Dir(), "connections.toml")
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const twoConns = `
[[connection]]
name = "a"
engine = "postgres"
dsn = "postgres://localhost/a"

[[connection]]
name = "b"
engine = "postgres"
dsn = "postgres://localhost/b"
password = "pw"
`

// Every connection comes, the one named the start, else the first (§5).
func TestLoadConnectionsByName(t *testing.T) {
	connectionsFile(t, twoConns)
	for name, want := range map[string]int{"": 0, "b": 1} {
		cs, start, err := LoadConnections(name)
		if err != nil || len(cs) != 2 || cs[1].Name != "b" || start != want {
			t.Errorf("LoadConnections(%q) = %d of %d %v, want %d", name, start, len(cs), err, want)
		}
	}
	_, _, err := LoadConnections("x")
	if err == nil || !strings.Contains(err.Error(), `没有名为 "x" 的连接，已有：a, b`) {
		t.Errorf("an unknown name: %v", err)
	}
}

func TestLoadConnectionErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(Dir(), "connections.toml")
	if _, _, err := LoadConnections(""); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("no file: %v, want the path %s", err, path)
	}
	for body, want := range map[string]string{
		"":                               "里面没有 [[connection]]",
		"[[connection]]\nname = \"a\"\n": "第 1 个连接：缺少 engine",
		"[[connection]]\nengine = \"postgres\"\n":                                       "第 1 个连接：缺少 name",
		"[[connection]]\nname = \"a\"\nengine = \"oracle\"\ndsn = \"x\"\n":              `第 1 个连接：engine = "oracle"：只能是 postgres、mysql、sqlite、redis 或 exec`,
		"[[connection]]\nname = \"a/b\"\nengine = \"postgres\"\ndsn = \"x\"\n":          `第 1 个连接：name = "a/b"：不能含 / 或 \，也不能以 . 开头`,
		"[[connection]]\nname = 'a\\b'\nengine = \"postgres\"\ndsn = \"x\"\n":           `第 1 个连接：name = "a\\b"：不能含 / 或 \，也不能以 . 开头`,
		"[[connection]]\nname = \"..\"\nengine = \"postgres\"\ndsn = \"x\"\n":           `第 1 个连接：name = ".."：不能含 / 或 \，也不能以 . 开头`,
		twoConns + "[[connection]]\nname = \"a\"\nengine = \"postgres\"\ndsn = \"x\"\n": `第 3 个连接：name = "a" 重复了`,
	} {
		path := connectionsFile(t, body)
		if _, _, err := LoadConnections(""); err == nil || !strings.Contains(err.Error(), path+": "+want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

// password_cmd wins over password_env, which wins over password (§13).
func TestSecret(t *testing.T) {
	t.Setenv("SQLMUX_TEST_SECRET", "from-env")
	cmd, fail := "printf 'from-cmd\\n'", "echo nope >&2; exit 3"
	if runtime.GOOS == "windows" { // no sh: cmd's echo ends its lines with \r\n, set /p does not (and leaves errorlevel 1)
		cmd, fail = "<nul set /p=from-cmd&exit 0", "echo nope 1>&2&exit 3"
	}
	for _, c := range []struct {
		conn Connection
		want string
	}{
		{Connection{PasswordCmd: cmd, PasswordEnv: "SQLMUX_TEST_SECRET", Password: "plain"}, "from-cmd"},
		{Connection{PasswordEnv: "SQLMUX_TEST_SECRET", Password: "plain"}, "from-env"},
		{Connection{Password: "plain"}, "plain"},
		{Connection{}, ""},
	} {
		if got, err := c.conn.Secret(); got != c.want || err != nil {
			t.Errorf("%+v: %q %v, want %q", c.conn, got, err, c.want)
		}
	}
	if _, err := (Connection{PasswordCmd: fail}).Secret(); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("a failing password_cmd: %v, want its stderr", err)
	}
	if _, err := (Connection{PasswordEnv: "SQLMUX_TEST_UNSET"}).Secret(); err == nil {
		t.Error("an unset password_env: no error")
	}
}

// A connection made in the app goes at the end of connections.toml, as
// text after the user's, comments and all; none there, the file is made
// the user's only (§14).
func TestAppendConnection(t *testing.T) {
	path := connectionsFile(t, "# mine\n[[connection]]\nname = \"a\" # the first\nengine = \"postgres\"\ndsn = \"x\"")
	if err := AppendConnection(Connection{Name: "b", Engine: "postgres", DSN: "postgres://localhost/b", ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "# mine\n[[connection]]\nname = \"a\" # the first\nengine = \"postgres\"\ndsn = \"x\"\n\n[[connection]]\nname = \"b\"\nengine = \"postgres\"\ndsn = \"postgres://localhost/b\"\nread_only = true\n"
	if string(data) != want {
		t.Errorf("file:\n%s", data)
	}
	if cs, _, err := LoadConnections("b"); err != nil || len(cs) != 2 || !cs[1].ReadOnly {
		t.Errorf("loaded back: %+v %v", cs, err)
	}
	os.Remove(path)
	if err := AppendConnection(Connection{Name: "c", Engine: "postgres", DSN: "y"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || (info.Mode().Perm() != 0o600 && runtime.GOOS != "windows") { // windows has no group/other bits to strip
		t.Errorf("made: %v %v", info, err)
	}
	// connections in an inline array: a [[connection]] after it would break
	// the file, the next start exiting; it stays as it is
	inline := "connection = [{ name = \"a\", engine = \"postgres\", dsn = \"x\" }]\n"
	path = connectionsFile(t, inline)
	if err := AppendConnection(Connection{Name: "b", Engine: "postgres", DSN: "y"}); err == nil {
		t.Error("appended to an inline array")
	}
	if data, _ := os.ReadFile(path); string(data) != inline {
		t.Errorf("written: %s", data)
	}
}

// No file and no connection in it are the one case the program starts
// without a connection (F7.4); a file that does not read is an error.
func TestNoConnections(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, file := range []struct {
		text string
		none bool
	}{{"", true}, {"# empty\n", true}, {"[[connection]\n", false}, {"[[connection]]\nname = \"x\"\n", false}} {
		if file.text != "" {
			if err := os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "sqlmux", "connections.toml"), []byte(file.text), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := LoadConnections("")
		if err == nil || NoConnections(err) != file.none {
			t.Errorf("%q: %v", file.text, err)
		}
	}
}

// A file that cannot be appended to is the error of CheckAppend, one that is
// not there yet is not (F8.16).
func TestCheckAppendUnwritable(t *testing.T) {
	c := Connection{Name: "n", Engine: "postgres", DSN: "postgres://h/n"}
	path := connectionsFile(t, "[[connection]]\nname = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\n")
	if err := CheckAppend(c); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && CheckAppend(c) == nil {
		t.Error("a read-only file")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := CheckAppend(c); err != nil {
		t.Errorf("no file yet: %v", err)
	}
	if os.Geteuid() == 0 {
		return
	}
	if runtime.GOOS == "windows" { // chmod on a directory blocks nothing there; a file where it is does
		if err := os.RemoveAll(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Dir(path), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if CheckAppend(c) == nil {
			t.Error("a directory no file can be made in")
		}
		return
	}
	if err := os.Chmod(filepath.Dir(path), 0o500); err != nil { // no file yet, and none can be made
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o700) })
	if CheckAppend(c) == nil {
		t.Error("a directory no file can be made in")
	}
}

// An ssh is a destination for the local ssh: one it would take for an option
// or for two words is no connection (F15.2).
func TestCheckSSH(t *testing.T) {
	for ssh, bad := range map[string]bool{"me@jump": false, "jump": false, "": false, "-oProxyCommand=x": true, "-J x": true, "me@jump x": true, "me@jump\tx": true} {
		c := Connection{Name: "a", Engine: "postgres", DSN: "postgres://h/d", SSH: ssh}
		if field, _ := c.Check(nil); (field == "ssh") != bad || field != "" && field != "ssh" {
			t.Errorf("%q: field %q", ssh, field)
		}
	}
}

// A redis connection is a connection, and its delimiter is a key that is
// known; a delimiter on any other is a problem of its own (F16.1).
func TestRedisConnection(t *testing.T) {
	ok := Connection{Name: "r", Engine: "redis", DSN: "redis://localhost:6379/0", Delimiter: "/"}
	if field, problem := ok.Check(nil); field != "" || problem != "" {
		t.Errorf("redis: %q %q", field, problem)
	}
	bad := Connection{Name: "p", Engine: "postgres", DSN: "postgres://h/d", Delimiter: ":"}
	if field, _ := bad.Check(nil); field != "delimiter" {
		t.Errorf("a delimiter on postgres: field %q", field)
	}
	connectionsFile(t, "[[connection]]\nname = \"r\"\nengine = \"redis\"\ndsn = \"redis://localhost/0\"\ndelimiter = \"/\"\n")
	conns, _, err := LoadConnections("")
	if err != nil || conns[0].Delimiter != "/" {
		t.Fatalf("read: %+v %v", conns, err)
	}
	if lines := UnknownKeys(&Config{}); len(lines) != 0 {
		t.Errorf("delimiter is known: %v", lines)
	}
	was := conns[0]
	to := was
	to.Delimiter = ":"
	if _, err := UpdateConnection("r", was, to); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, connectionsPath()); !strings.Contains(got, "delimiter = \":\"\n") || strings.Contains(got, `"/"`) {
		t.Errorf("rewritten:\n%s", got)
	}
}

// An exec connection is a command and the dialect it speaks, with no dsn; it is read
// only whatever is written; the keys are for it alone (F17.7, §8.8).
func TestExecConnectionConfig(t *testing.T) {
	good := "[[connection]]\nname = \"prod\"\nengine = \"exec\"\ndialect = \"postgres\"\ncommand = [\"doraemonctl\", \"exec\", \"db\", \"--sql={sql}\"]\n"
	connectionsFile(t, good+"read_only = false\n")
	cs, _, err := LoadConnections("")
	if err != nil || len(cs) != 1 || !cs[0].ReadOnly || cs[0].Dialect != "postgres" || len(cs[0].Command) != 4 || cs[0].Command[3] != "--sql={sql}" || cs[0].DSN != "" {
		t.Fatalf("%+v %v", cs, err)
	}
	connectionsFile(t, "[[connection]]\nname = \"r\"\nengine = \"exec\"\ndialect = \"redis\"\ncommand = [\"doraemonctl\", \"exec\", \"redis\", \"--\", \"{cmd}\", \"{args...}\"]\n")
	if cs, _, err := LoadConnections(""); err != nil || len(cs) != 1 || cs[0].Kind() != "redis" || !cs[0].ReadOnly {
		t.Fatalf("redis dialect: %+v %v", cs, err)
	}
	for body, want := range map[string]string{
		"[[connection]]\nname = \"p\"\nengine = \"exec\"\ncommand = [\"x\", \"{sql}\"]\n":                                "dialect",
		"[[connection]]\nname = \"p\"\nengine = \"exec\"\ndialect = \"mysql\"\ncommand = [\"x\", \"{sql}\"]\n":           "dialect",
		"[[connection]]\nname = \"p\"\nengine = \"exec\"\ndialect = \"postgres\"\ncommand = [\"x\"]\n":                   "command",
		"[[connection]]\nname = \"p\"\nengine = \"exec\"\ndialect = \"postgres\"\n":                                      "command",
		"[[connection]]\nname = \"p\"\nengine = \"exec\"\ndialect = \"redis\"\ncommand = [\"x\", \"{cmd}\"]\n":           "{args...}",
		"[[connection]]\nname = \"p\"\nengine = \"exec\"\ndialect = \"redis\"\ncommand = [\"x\", \"{cmd} {args...}\"]\n": "{args...}",
		"[[connection]]\nname = \"p\"\nengine = \"postgres\"\ndsn = \"postgres://h/d\"\ncommand = [\"x\", \"{sql}\"]\n":  "command",
	} {
		connectionsFile(t, body)
		if _, _, err := LoadConnections(""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want a word of %s", body, err, want)
		}
	}
}
