//go:build integration

package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sqlmux/internal/db/postgres"
)

// testDB is the integration database split into a DSN without its
// password, and the password.
func testDB(t *testing.T) (dsn, password string) {
	t.Helper()
	u, err := url.Parse(postgres.IntegrationDSN(t))
	if err != nil || u.User == nil {
		t.Fatal("SQLMUX_TEST_PG must be a postgres:// URL with a password")
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

// password_cmd, password_env and ~/.pgpass all connect (§13); with none of
// them, nothing else supplies the password.
func TestOpenPasswordSources(t *testing.T) {
	dsn, pw := testDB(t)
	u, _ := url.Parse(dsn)
	pgpass := filepath.Join(t.TempDir(), "pgpass")
	os.WriteFile(pgpass, []byte(strings.Join([]string{u.Hostname(), "*", "*", u.User.Username(), pw}, ":")+"\n"), 0o600)
	for name, c := range map[string]struct {
		extra, env, val string
		fails           bool
	}{
		"password_cmd": {extra: "password_cmd = \"printf '%s\\\\n' '" + pw + "'\"\n"},
		"password_env": {extra: "password_env = \"SQLMUX_TEST_PW\"\n", env: "SQLMUX_TEST_PW", val: pw},
		"~/.pgpass":    {env: "PGPASSFILE", val: pgpass},
		"none":         {fails: true},
	} {
		t.Run(name, func(t *testing.T) {
			writeConnection(t, dsn, c.extra)
			if c.env != "" {
				t.Setenv(c.env, c.val)
			}
			sess, _, err := open("test")
			if (err != nil) != c.fails {
				t.Fatalf("error %v, want one: %v", err, c.fails)
			}
			if err == nil {
				sess.Close()
			}
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
