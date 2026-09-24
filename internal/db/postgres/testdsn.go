//go:build integration

package postgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// IntegrationDSN is the seeded database from docker-compose.yml that every
// integration test runs against (SQLMUX_TEST_PG); unset, the test skips.
// It blanks the user's PG* variables and ~/.pgpass for the test, so none of
// them can connect in place of what the test sets up.
func IntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMUX_TEST_PG")
	if dsn == "" {
		t.Skip("SQLMUX_TEST_PG is not set")
	}
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "PG") {
			t.Setenv(k, "") // pgconn reads an empty variable as unset
		}
	}
	t.Setenv("PGPASSFILE", filepath.Join(t.TempDir(), "none")) // not ~/.pgpass
	return dsn
}
