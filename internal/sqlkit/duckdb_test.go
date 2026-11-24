package sqlkit

import (
	"strings"
	"testing"
)

// DuckDB reads as PG does, and has its own words for a query that reads: FROM
// first, SUMMARIZE (F24.2).
func TestDuckDB(t *testing.T) {
	for s, want := range map[string]bool{
		"from t": true, "from t select a, b": true, "(from t)": true, "summarize t": true, "describe t": true, "show tables": true,
		"select 1": true, "with x as (select 1) from x": true, "from t where a = 1": true,
		"copy t to 'x.csv'": false, "insert into t from u": false, "delete from t": false, "attach 'x.duckdb'": false,
		"pragma table_info('t')": false, "install httpfs": false, "from t select into x": false,
	} {
		if got := IsRead(s, DuckDB); got != want {
			t.Errorf("IsRead(%q) = %v", s, got)
		}
	}
	// what a read-only connection keeps from the database: what writes a file or loads code as well
	for s, want := range map[string]bool{
		"from t": false, "set threads = 1": false, "use main": false, "begin": false, "rollback": false,
		"copy t to 'x.csv'": true, "attach 'x.duckdb'": true, "install httpfs": true, "load httpfs": true, "checkpoint": true,
		"export database 'dir'": true, "insert into t values (1)": true, "begin read write": true,
	} {
		if got := BreaksReadOnly(s, DuckDB); got != want {
			t.Errorf("BreaksReadOnly(%q) = %v", s, got)
		}
	}
	if got, ok := AutoLimit("from t -- c", DuckDB, 11, 0); !ok || !strings.HasSuffix(strings.ToLower(got), "limit 11") {
		t.Errorf("AutoLimit: %q %v", got, ok)
	}
	if got, ok := AutoLimit("from t limit 5", DuckDB, 11, 0); ok || got != "from t limit 5" {
		t.Errorf("a LIMIT of its own: %q %v", got, ok)
	}
	if n := len(Statements("create macro m() as $$ select 1; $$; from t; select ';'", DuckDB)); n != 3 {
		t.Errorf("%d statements", n)
	}
	if !Dangerous("drop table t", DuckDB) || !Dangerous("delete from t", DuckDB) || !Dangerous("alter table t drop column c", DuckDB) || Dangerous("alter table t drop constraint c", DuckDB) {
		t.Error("dangerous")
	}
	if EndsTx("end", DuckDB) != "commit" || EndsTx("rollback", DuckDB) != "rollback" || EndsTx("select 1", DuckDB) != "" {
		t.Error("EndsTx")
	}
}

func TestDuckDBExplainAnalyzeRuns(t *testing.T) {
	if !Dangerous("explain analyze delete from t", DuckDB) || Dangerous("explain delete from t", DuckDB) || Dangerous("explain analyze select 1", DuckDB) {
		t.Error("EXPLAIN ANALYZE runs the statement it explains")
	}
}
