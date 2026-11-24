//go:build integration

package duckdb

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// UseLibrary points the package at the libduckdb of SQLMUX_TEST_DUCKDB, the
// environment these tests have of DuckDB (§8.1「测试」): they skip without it.
func UseLibrary(t testing.TB) {
	t.Helper()
	p := os.Getenv("SQLMUX_TEST_DUCKDB")
	if p == "" {
		t.Skip("SQLMUX_TEST_DUCKDB is not set")
	}
	Configure(p, "")
}

// OwnDB is a file of the test's own, seeded from testdata/seed/duckdb.sql, for
// the tests of other packages that run on every engine; it is gone when the
// test ends, and its path is the connection's DSN.
func OwnDB(t testing.TB) string {
	t.Helper()
	UseLibrary(t)
	_, here, _, _ := runtime.Caller(0) // the seed by this file's path: tests of other packages call it
	seed, err := os.ReadFile(filepath.Join(filepath.Dir(here), "../../../testdata/seed/duckdb.sql"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.duckdb")
	c, _, err := Connect(context.Background(), path+"?mode=rwc", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close() // the file is let go of, for the test to open
	for _, s := range sqlkit.Statements(string(seed), sqlkit.DuckDB) {
		if _, err := c.Exec(context.Background(), string(seed[s.Start:s.End]), 0); err != nil {
			t.Fatalf("%s: %v", seed[s.Start:s.End], err)
		}
	}
	return path
}
