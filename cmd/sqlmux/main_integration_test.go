//go:build integration

package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testDB is SQLMUX_TEST_PG split into a DSN without its password, and the
// password; unset, the tests skip.
func testDB(t *testing.T) (dsn, password string) {
	t.Helper()
	u, err := url.Parse(os.Getenv("SQLMUX_TEST_PG"))
	if err != nil || u.User == nil {
		t.Skip("SQLMUX_TEST_PG is not set to a postgres:// URL with a password")
	}
	password, _ = u.User.Password()
	u.User = url.User(u.User.Username())
	return u.String(), password
}

func writeConnection(t *testing.T, dsn, extra string) {
	t.Helper()
	writeConfig(t, "")
	body := "[[connection]]\nname = \"test\"\nengine = \"postgres\"\ndsn = \"" + dsn + "\"\n" + extra
	if err := os.WriteFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sqlmux", "connections.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// password_cmd, password_env and ~/.pgpass all connect (§13).
func TestOpenPasswordSources(t *testing.T) {
	dsn, pw := testDB(t)
	u, _ := url.Parse(dsn)
	pgpass := filepath.Join(t.TempDir(), "pgpass")
	os.WriteFile(pgpass, []byte(strings.Join([]string{u.Hostname(), "*", "*", u.User.Username(), pw}, ":")+"\n"), 0o600)
	for name, c := range map[string]struct{ extra, env, val string }{
		"password_cmd": {extra: "password_cmd = \"printf '%s\\\\n' '" + pw + "'\"\n"},
		"password_env": {extra: "password_env = \"SQLMUX_TEST_PW\"\n", env: "SQLMUX_TEST_PW", val: pw},
		"~/.pgpass":    {env: "PGPASSFILE", val: pgpass},
	} {
		t.Run(name, func(t *testing.T) {
			writeConnection(t, dsn, c.extra)
			if c.env != "" {
				t.Setenv(c.env, c.val)
			}
			sess, _, err := open("test")
			if err != nil {
				t.Fatal(err)
			}
			sess.Close()
		})
	}
}

// A wrong password prints the server's error, no UI (§14).
func TestOpenWrongPassword(t *testing.T) {
	dsn, _ := testDB(t)
	writeConnection(t, dsn, "password = \"wrong\"\n")
	_, _, err := open("")
	if err == nil || !strings.HasPrefix(err.Error(), "test: ") || !strings.Contains(err.Error(), "password authentication failed") {
		t.Fatalf("wrong password: %v", err)
	}
}
