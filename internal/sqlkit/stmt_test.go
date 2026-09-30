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
	s := "\n\nselect 1;\n\n-- about\n/* the next */\n  select 2; select 3;\n-- note\n\nselect 4 /* x\ny */\n\n"
	stmts := Statements(s, PG)
	at := func(sub string, off int) int { return strings.Index(s, sub) + off }
	for _, c := range []struct{ pos, want int }{
		{0, 0},                   // before the first
		{at("1", 0), 0},          // in it
		{at(";\n\n-", 2), 0},     // the blank line after it
		{at("-- about", 3), 1},   // a comment right above the next
		{at("/* the", 0), 1},     // and another
		{at("  select 2", 0), 1}, // its line's indent
		{at("select 3", 0), 2},   // the second on a line
		{at("2; select", 1), 1},  // the ; of the first
		{at("-- note", 0), 2},    // a comment after it, a blank line below
		{at("y */", 0), 3},       // a comment in it
		{len(s) - 1, 3},          // the blank lines at the end
	} {
		if got := StmtAt(s, stmts, c.pos); got != c.want {
			t.Errorf("at %d (%q): %d, want %d", c.pos, s[c.pos:min(c.pos+8, len(s))], got, c.want)
		}
	}
	if StmtAt("", nil, 0) != -1 {
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
		"select * from t lock in share mode":                                                       false,
		"describe t":                                                                               true,
		"with x as (select 1 a) (select * into t2 from x)":                                         false,
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
		{"(select 1 limit 5)", "(select 1 limit 5)"},
		{"((select 1 limit 5));", "((select 1 limit 5));"},
		{"(select 1 fetch first 1 rows only)", "(select 1 fetch first 1 rows only)"},
		{"with x as (select 1) (select * from x limit 5)", "with x as (select 1) (select * from x limit 5)"},
		{"(select 1 limit 5) union select 2", "(select 1 limit 5) union select 2\nLIMIT 101"},
		{"(select 1 limit 5) union (select 2)", "(select 1 limit 5) union (select 2)\nLIMIT 101"},
		{"with x as (select 1) select * from (select 1 limit 5) s", "with x as (select 1) select * from (select 1 limit 5) s\nLIMIT 101"},
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

func TestFirstWord(t *testing.T) {
	for s, want := range map[string]string{
		"-- make it\nCREATE table t (a int)": "create",
		"(select 1)":                         "select",
		"/* x */ drop table t":               "drop",
		"  ":                                 "",
	} {
		if got := FirstWord(s, PG); got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
}

// IsQuery takes what starts as a query, past comments and parentheses.
func TestIsQuery(t *testing.T) {
	for s, want := range map[string]bool{
		"select 1": true, "  SELECT 1": true, "values (1)": true, "table t_order": true,
		"with x as (select 1) select * from x":                 true,
		"-- note\n/* why */ ((select 1) union all (select 2))": true,
		"show search_path": false, "explain select 1": false, "delete from t": false,
		"selectx 1": false, `"select"`: false, "'select'": false, "": false, "(": false,
	} {
		if got := IsQuery(s, PG); got != want {
			t.Errorf("IsQuery(%q) = %v", s, got)
		}
	}
}

// IsWrite takes what IsRead doesn't but for a first word no keyword: a
// typo goes to the database.
func TestIsWrite(t *testing.T) {
	for s, want := range map[string]bool{
		"delete from t": true, "with x as (delete from t returning *) select * from x": true, "(insert into t values (1))": true,
		"select 1": false, "selec 1": false, "t_order": false, "": false, "explain analyze delete from t": true,
	} {
		if got := IsWrite(s, PG); got != want {
			t.Errorf("IsWrite(%q) = %v", s, got)
		}
	}
}
