package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// connectionsFile writes connections.toml under a fresh XDG_CONFIG_HOME.
func connectionsFile(t *testing.T, body string, perm os.FileMode) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(Dir(), "connections.toml")
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil { // past the umask
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

func TestLoadConnectionByName(t *testing.T) {
	connectionsFile(t, twoConns, 0o600)
	for name, want := range map[string]string{"": "a", "b": "b"} {
		c, warn, err := LoadConnection(name)
		if err != nil || c.Name != want || warn != "" {
			t.Errorf("LoadConnection(%q) = %q %q %v, want %q", name, c.Name, warn, err, want)
		}
	}
	_, _, err := LoadConnection("x")
	if err == nil || !strings.Contains(err.Error(), `没有名为 "x" 的连接，已有：a, b`) {
		t.Errorf("an unknown name: %v", err)
	}
}

// A plain password in a file group or others can read warns (§13).
func TestLoadConnectionWarnsOnReadablePassword(t *testing.T) {
	connectionsFile(t, twoConns, 0o644)
	if _, warn, _ := LoadConnection("a"); !strings.Contains(warn, "明文密码") {
		t.Errorf("0644 with a password: warning %q", warn)
	}
	connectionsFile(t, strings.ReplaceAll(twoConns, `password = "pw"`, ""), 0o644)
	if _, warn, _ := LoadConnection("a"); warn != "" {
		t.Errorf("0644 without a password: warning %q", warn)
	}
}

func TestLoadConnectionErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(Dir(), "connections.toml")
	if _, _, err := LoadConnection(""); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("no file: %v, want the path %s", err, path)
	}
	for body, want := range map[string]string{
		"":                               "里面没有 [[connection]]",
		"[[connection]]\nname = \"a\"\n": "第 1 个连接：缺少 engine",
		"[[connection]]\nengine = \"postgres\"\n":                         "第 1 个连接：缺少 name",
		"[[connection]]\nname = \"a\"\nengine = \"mysql\"\ndsn = \"x\"\n": `第 1 个连接：engine = "mysql"，目前只支持 postgres`,
	} {
		path := connectionsFile(t, body, 0o600)
		if _, _, err := LoadConnection(""); err == nil || !strings.Contains(err.Error(), path+": "+want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

// password_cmd wins over password_env, which wins over password (§13).
func TestSecret(t *testing.T) {
	t.Setenv("SQLMUX_TEST_SECRET", "from-env")
	for _, c := range []struct {
		conn Connection
		want string
	}{
		{Connection{PasswordCmd: "printf 'from-cmd\\n'", PasswordEnv: "SQLMUX_TEST_SECRET", Password: "plain"}, "from-cmd"},
		{Connection{PasswordEnv: "SQLMUX_TEST_SECRET", Password: "plain"}, "from-env"},
		{Connection{Password: "plain"}, "plain"},
		{Connection{}, ""},
	} {
		if got, err := c.conn.Secret(); got != c.want || err != nil {
			t.Errorf("%+v: %q %v, want %q", c.conn, got, err, c.want)
		}
	}
	if _, err := (Connection{PasswordCmd: "echo nope >&2; exit 3"}).Secret(); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("a failing password_cmd: %v, want its stderr", err)
	}
	if _, err := (Connection{PasswordEnv: "SQLMUX_TEST_UNSET"}).Secret(); err == nil {
		t.Error("an unset password_env: no error")
	}
}
