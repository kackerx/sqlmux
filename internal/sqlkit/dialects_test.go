package sqlkit

import (
	"strings"
	"testing"
)

// ClickHouse and Trino as their own dialects (§8.9, §8.10): what each reads
// as a name, a string, a comment, and what the words around a statement make
// of it.
func TestScanClickHouseTrino(t *testing.T) {
	for _, c := range []struct {
		d     Dialect
		in    string
		kinds string
	}{
		{ClickHouse, "\"a\"\"b\" `c` 'it\\'s'", "q_q_s"},    // "…" is a name, not MySQL's string; \' stays in the string
		{ClickHouse, "a # c\nb #!x\nc", "i_c_i_c_i"},        // # before a blank or ! comments the line
		{ClickHouse, "a#b", "ioi"},                          // elsewhere it is no comment
		{ClickHouse, "system reload dictionaries", "k_i_i"}, // ClickHouse's words of its own
		{PG, "system", "i"},                                 // which stay names elsewhere
		{Trino, "\"a\" 'b''c' `d`", "q_s_oio"},              // Trino has no `quoted` names
		{Trino, "$1 $$x$$", "on_ooi"},                       // nor $tag$ strings ($ inside a name is the name's): $n is the argument Inline fills
		{Trino, "e'x'", "is"},                               // nor E'…'
	} {
		if got := kinds(c.in, c.d); got != c.kinds {
			t.Errorf("%d %q: %s, want %s", c.d, c.in, got, c.kinds)
		}
	}
}

func TestDialectRules(t *testing.T) {
	if got := Literal(`a\b'c`, ClickHouse); got != `'a\\b''c'` {
		t.Errorf("ClickHouse literal: %s", got)
	}
	if got := Literal(`a\b`, Trino); got != `'a\b'` {
		t.Errorf("Trino literal: %s", got)
	}
	if ClickHouse.CaseInsensitive() || !Trino.CaseInsensitive() {
		t.Error("ClickHouse's names go by case, Trino's by none")
	}
	for _, c := range []struct {
		d    Dialect
		name string
		want bool
	}{
		{ClickHouse, "Users", false}, // no folding: as it is
		{ClickHouse, "prewhere", true},
		{ClickHouse, "user", false},
		{Trino, "Users", false},
		{Trino, "describe", true},
		{Trino, "name", false},
	} {
		if got := c.d.NeedsQuote(c.name); got != c.want {
			t.Errorf("%d NeedsQuote(%q) = %v", c.d, c.name, got)
		}
	}
	for _, c := range []struct {
		d     Dialect
		s     string
		write bool
	}{
		{ClickHouse, "system drop dns cache", true},
		{ClickHouse, "optimize table t final", true},
		{ClickHouse, "alter table t delete where id = 1", true},
		{ClickHouse, "select * from t final", false},
		{ClickHouse, "show create table t", false},
		{Trino, "update t set a = 1 where b = 2", true},
		{Trino, "call system.flush_metadata_cache()", true},
		{Trino, "show catalogs", false},
	} {
		if got := IsWrite(c.s, c.d); got != c.write {
			t.Errorf("%d IsWrite(%q) = %v", c.d, c.s, got)
		}
	}
	if got := Common(ClickHouse); !contains(got, "prewhere") || contains(got, "returning") {
		t.Errorf("ClickHouse's keywords: %v", got)
	}
	if got := Common(Trino); contains(got, "on conflict") {
		t.Errorf("Trino's keywords: %v", got)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// sql-formatter has both languages: what it lays out keeps ClickHouse's own
// words and Trino's.
func TestFormatClickHouseTrino(t *testing.T) {
	out, err := Format("select a from t final prewhere b = 1 settings max_threads = 2", ClickHouse, Options{KeywordCase: "upper", TabWidth: 2})
	if err != nil || !strings.Contains(out, "PREWHERE") || !strings.Contains(out, "FINAL") {
		t.Errorf("ClickHouse: %q %v", out, err)
	}
	out, err = Format("select a from tpch.tiny.nation where n_regionkey = 1", Trino, Options{KeywordCase: "upper", TabWidth: 2})
	if err != nil || !strings.Contains(out, "tpch.tiny.nation") {
		t.Errorf("Trino: %q %v", out, err)
	}
}

// A query's LIMIT on Trino has its OFFSET before it; on ClickHouse it goes
// before a SETTINGS or a FORMAT at the end (§9.4).
func TestAutoLimitClickHouseTrino(t *testing.T) {
	for _, c := range []struct {
		d         Dialect
		in, out   string
		n, offset int
	}{
		{Trino, "select * from t;", "select * from t\nOFFSET 500 LIMIT 501", 501, 500},
		{Trino, "select * from t", "select * from t\nLIMIT 501", 501, 0},
		{ClickHouse, "select * from t settings max_threads = 1;", "select * from t LIMIT 501\nsettings max_threads = 1", 501, 0},
		{ClickHouse, "select * from t format JSON", "select * from t LIMIT 10 OFFSET 20\nformat JSON", 10, 20},
		{ClickHouse, "select (select 1 settings a = 1) x", "select (select 1 settings a = 1) x\nLIMIT 5", 5, 0},
	} {
		if got, _ := AutoLimit(c.in, c.d, c.n, c.offset); got != c.out {
			t.Errorf("%d %q: %q, want %q", c.d, c.in, got, c.out)
		}
	}
}
