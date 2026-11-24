//go:build linux && !duckdb

package duckdb

import (
	"context"
	"strings"
	"testing"
)

// Without the tag a DuckDB connection says the build has none, and what to do.
func TestNoDuckDBInThisBuild(t *testing.T) {
	_, _, err := (Engine{}).Connect(context.Background(), "x.duckdb", "", false)
	if err == nil || !strings.Contains(err.Error(), "-tags duckdb") || !strings.Contains(err.Error(), "_duckdb") {
		t.Errorf("%v", err)
	}
}
