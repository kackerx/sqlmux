//go:build integration && linux && !duckdb

package app

import "testing"

// duckdbDSN is "" in a Linux build without the duckdb tag, which has no DuckDB (§8.1).
func duckdbDSN(*testing.T) string { return "" }
