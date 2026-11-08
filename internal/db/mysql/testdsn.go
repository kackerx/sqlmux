//go:build integration

package mysql

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// IntegrationDSN is the seeded database from docker-compose.yml that the
// MySQL integration tests run against (SQLMUX_TEST_MYSQL); unset, the
// test skips.
func IntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMUX_TEST_MYSQL")
	if dsn == "" {
		t.Skip("SQLMUX_TEST_MYSQL is not set")
	}
	return dsn
}

// OwnDB is a database of the test's own on the test server, seeded as the
// shared one is but for agentable and the functions, and dropped when the test ends: for
// tests that write (AGENTS.md「集成测试环境」). It returns its DSN.
func OwnDB(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	admin, _, err := Connect(ctx, IntegrationDSN(t), "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := fmt.Sprintf("sqlmux_worker_%d_%s", os.Getpid(), strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_")))
	name = name[:min(len(name), 64)] // MySQL's limit; a six-digit pid (WSL) made a long test name pass it
	drop := func() { _, _ = admin.Query(ctx, "drop database if exists "+name) }
	drop()
	if _, err := admin.Query(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	cfg, err := Config(IntegrationDSN(t), "")
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	_, here, _, _ := runtime.Caller(0) // the seed by this file's path: tests of other packages call it
	seed, err := os.ReadFile(filepath.Join(filepath.Dir(here), "../../../testdata/seed/mysql.sql"))
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := Connect(ctx, cfg.FormatDSN(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, s := range strings.Split(string(seed), ";\n") {
		if strings.TrimSpace(s) == "" || strings.Contains(s, "agentable") || strings.Contains(s, "grant ") || strings.Contains(s, "create function") { // a function takes SUPER, which the shared database got from root
			continue
		}
		if _, err := c.Exec(ctx, s, 0); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}
	return cfg.FormatDSN()
}
