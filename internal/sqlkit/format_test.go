package sqlkit

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"
)

// Each long Unicode name is there as often as it was: a new bundle that
// has them elsewhere fails here, before goja fails on it (§9.5).
func TestUnicodeNames(t *testing.T) {
	for _, u := range unicodeNames {
		if n := strings.Count(formatterJS, u.from); n != u.n {
			t.Errorf("%s: %d in the bundle, want %d", u.from, n, u.n)
		}
	}
}

// What the 2026-09-23 spike checked (§9.5), each dialect's.
func TestGoldenFormat(t *testing.T) {
	var b strings.Builder
	for _, c := range []struct {
		d   Dialect
		sql string
	}{
		{PG, "create function f() returns int as $$ select 1 $$ language sql"},
		{PG, "select E'a\\'b', x::int, count(*) filter (where y > 0) from t where id = $1 -- the one\ngroup by x"},
		{PG, "/* two */ update t set a = 1 where b in (select b from u); delete from t"},
		{MySQL, "select `a b`, c from `t` # a comment\nwhere d = 'x' limit 5, 10"},
		{MySQL, "insert into t (a) values (1), (2) on duplicate key update a = values(a)"},
	} {
		out, err := Format(c.sql, c.d, Options{KeywordCase: "lower", TabWidth: 2})
		if err != nil {
			t.Fatalf("%s: %v", c.sql, err)
		}
		b.WriteString("-- " + map[Dialect]string{PG: "pg", MySQL: "mysql"}[c.d] + ": " + c.sql + "\n" + out + "\n\n")
	}
	up, _ := Format("select a, now() from t; create table t (a int)", PG, Options{KeywordCase: "upper", TabWidth: 4})
	b.WriteString("-- upper, types too, not functions; tab_width 4\n" + up + "\n")
	golden.RequireEqual(t, b.String())
}

func TestFormatError(t *testing.T) {
	if _, err := Format("select 'a", PG, Options{KeywordCase: "lower", TabWidth: 2}); err == nil || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "Parse error") {
		t.Errorf("an unclosed string: %v", err)
	}
}

// The VM is made once, by the first format, and kept (F3.9).
func TestFormatVMOnce(t *testing.T) {
	o := Options{KeywordCase: "lower", TabWidth: 2}
	Format("select 1", PG, o)
	vm := formatter.vm
	if _, err := Format("select 2", PG, o); err != nil || vm == nil || formatter.vm != vm {
		t.Errorf("a second VM, or none: %v", err)
	}
}

// The first format, the VM made then, takes less than 300ms (F3.9): a
// benchmark, as go test ./... on a busy machine takes longer.
func BenchmarkFirstFormat(b *testing.B) {
	for b.Loop() {
		vm, format, err := load()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := format(nil, vm.ToValue("select a, b from t where c = 1")); err != nil {
			b.Fatal(err)
		}
	}
	if per := b.Elapsed() / time.Duration(b.N); per > 300*time.Millisecond {
		b.Errorf("first format took %v", per)
	}
}

// A format past FormatTimeout is interrupted, the VM usable after (§9.5).
func TestFormatTimeout(t *testing.T) {
	Format("select 1", PG, Options{KeywordCase: "preserve", TabWidth: 2}) // the VM made, not timed
	defer func(d time.Duration) { FormatTimeout = d }(FormatTimeout)
	FormatTimeout = time.Millisecond
	if _, err := Format("insert into t (a) values "+strings.Repeat("(1), ", 300)+"(1)", PG, Options{KeywordCase: "preserve", TabWidth: 2}); err != ErrTimeout {
		t.Fatalf("err %v", err)
	}
	FormatTimeout = 5 * time.Second
	if out, err := Format("select a from t", PG, Options{KeywordCase: "preserve", TabWidth: 2}); err != nil || out != "select\n  a\nfrom\n  t" {
		t.Errorf("after: %q %v", out, err)
	}
}
