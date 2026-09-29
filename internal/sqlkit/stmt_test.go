package sqlkit

import (
	"strings"
	"testing"
)

func TestStatements(t *testing.T) {
	for _, c := range []struct {
		d    Dialect
		in   string
		want []string
	}{
		{PG, "select 1; select 2;", []string{"select 1", "select 2"}},
		{PG, "-- lead\n  select 1 -- tail\n;\n\n/* x */ select 2", []string{"select 1", "select 2"}},
		{PG, "select ';', \"a;b\", $$;$$ /* ; */ -- ;\n; ;; select (1;2)", []string{`select ';', "a;b", $$;$$`, "select (1;2)"}},
		{PG, "  \n-- only a comment\n", nil},
		{MySQL, "select '\\';' # ;\n; select `a;`", []string{`select '\';'`, "select `a;`"}},
	} {
		var got []string
		for _, st := range Statements(c.in, c.d) {
			got = append(got, c.in[st.Start:st.End])
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStmtAt(t *testing.T) {
	s := "\n\nselect 1;\n\nselect 2;\n-- note\n"
	stmts := Statements(s, PG)
	for pos, want := range map[int]int{
		0:                              0, // before the first
		strings.Index(s, "1"):          0,
		strings.Index(s, ";\n\ns") + 2: 0, // the blank line after it
		strings.Index(s, "select 2"):   1,
		len(s) - 1:                     1,
	} {
		if got := StmtAt(stmts, pos); got != want {
			t.Errorf("at %d: %d, want %d", pos, got, want)
		}
	}
	if StmtAt(nil, 0) != -1 {
		t.Error("no statements")
	}
}

func TestIsRead(t *testing.T) {
	for s, want := range map[string]bool{
		"select 1": true, "  SELECT * from t": true, "((select 1) union (select 2))": true,
		"show search_path": true, "table t": true, "values (1)": true, "desc t": true,
		"explain select 1": true, "explain delete from t": true, "explain verbose update t set a = 1": true,
		"explain analyze select 1": true, "explain analyze delete from t": false,
		"EXPLAIN (ANALYZE, BUFFERS) update t set a = 1": false, "explain (analyze false) delete from t": true,
		"explain (analyze off, costs) delete from t": true, "explain (format json) delete from t": true,
		"explain (analyze) select 1":                                                               true,
		"with x as (select 1) select * from x":                                                     true,
		"with x as (delete from t returning *) select * from x":                                    false,
		"with x as (select 1) insert into t select * from x":                                       false,
		"with recursive x as (select 1) merge into t using x on true when matched then do nothing": false,
		"select a into t2 from t":                                                                  false,
		"select * from t where id in (select 1)":                                                   true,
		"select * from t for update":                                                               false,
		"select * from t for no key update nowait":                                                 false,
		"select * from (select * from t for share) s":                                              false,
		"select * from t for key share":                                                            false,
		"select substring('abc' from 1 for 2)":                                                     true,
		"select 'delete', \"update\" -- insert":                                                    true,
		"delete from t":                                                                            false, "update t set a = 1": false, "insert into t values (1)": false,
		"set x = 1": false, "begin": false, "": false, "create table t (a int)": false,
	} {
		if got := IsRead(s, PG); got != want {
			t.Errorf("IsRead(%q) = %v", s, got)
		}
	}
}

func TestAutoLimit(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"select 1", "select 1\nLIMIT 101"},
		{"select 1;", "select 1\nLIMIT 101"},
		{"select 1; -- done", "select 1 -- done\nLIMIT 101"},
		{"select * from t -- why", "select * from t -- why\nLIMIT 101"},
		{"with x as (select 1) select * from x", "with x as (select 1) select * from x\nLIMIT 101"},
		{"table t", "table t\nLIMIT 101"},
		{"values (1), (2)", "values (1), (2)\nLIMIT 101"},
		{"(select 1 limit 5)", "(select 1 limit 5)\nLIMIT 101"},
		{"select * from t where id in (select id from u limit 3)", "select * from t where id in (select id from u limit 3)\nLIMIT 101"},
		{"select * from t limit 5", "select * from t limit 5"},
		{"select * from t LIMIT 5, 10", "select * from t LIMIT 5, 10"},
		{"select * from t offset 5 fetch first 3 rows only", "select * from t offset 5 fetch first 3 rows only"},
		{"select * from t for update", "select * from t for update"},
		{"select a into t2 from t", "select a into t2 from t"},
		{"show search_path", "show search_path"},
		{"explain select 1", "explain select 1"},
		{"delete from t", "delete from t"},
		{"with x as (delete from t returning *) select * from x", "with x as (delete from t returning *) select * from x"},
		{"select 'limit'", "select 'limit'\nLIMIT 101"},
	} {
		if got := AutoLimit(c.in, PG, 101); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHasSemicolon(t *testing.T) {
	for s, want := range map[string]bool{
		"1=1; drop table t": true, "(a = 1;)": true, "a = ';'": false, `"a;b" = 1`: false,
		"a = 1 -- ;": false, "a = 1 /* ; */": false, "a = $$;$$": false,
	} {
		if got := HasSemicolon(s, PG); got != want {
			t.Errorf("HasSemicolon(%q) = %v", s, got)
		}
	}
	if HasSemicolon("a = '\\';'", MySQL) || !HasSemicolon("a = 1 # x\n;", MySQL) {
		t.Error("MySQL strings and # comments")
	}
}
