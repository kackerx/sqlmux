//go:build integration && (!linux || duckdb)

package app

import (
	"testing"

	"github.com/kackerx/sqlmux/internal/db/duckdb"
)

// duckdbDSN is a seeded DuckDB file of the test's own, "" where this build has no DuckDB.
func duckdbDSN(t *testing.T) string { return duckdb.OwnDB(t) }
