package sqlkit

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/dop251/goja"
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
		{SQLite, "select \"a b\", `c`, x -> 'k' from t where d = ?1 order by 1 limit 5 offset 10"},
		{DuckDB, "select [1, 2] l, {'k': 1} s, x::int from \"t\" where d is not distinct from $1 -- c\nqualify row_number() over () = 1 limit 5"},
	} {
		out, err := Format(c.sql, c.d, Options{KeywordCase: "lower", TabWidth: 2})
		if err != nil {
			t.Fatalf("%s: %v", c.sql, err)
		}
		b.WriteString("-- " + map[Dialect]string{PG: "pg", MySQL: "mysql", SQLite: "sqlite", DuckDB: "duckdb"}[c.d] + ": " + c.sql + "\n" + out + "\n\n")
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
	_, _ = Format("select 1", PG, o)
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
	_, _ = Format("select 1", PG, Options{KeywordCase: "preserve", TabWidth: 2}) // the VM made, not timed
	defer func(d time.Duration) { FormatTimeout = d }(FormatTimeout)
	FormatTimeout = time.Millisecond
	if _, err := Format("insert into t (a) values "+strings.Repeat("(1), ", 300)+"(1)", PG, Options{KeywordCase: "preserve", TabWidth: 2}); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err %v", err)
	}
	FormatTimeout = 5 * time.Second
	if out, err := Format("select a from t", PG, Options{KeywordCase: "preserve", TabWidth: 2}); err != nil || out != "select\n  a\nfrom\n  t" {
		t.Errorf("after: %q %v", out, err)
	}
}

// slowFormat gives a format all the time it needs, under -race as well.
func slowFormat(t *testing.T) {
	t.Helper()
	was := FormatTimeout
	FormatTimeout = time.Minute
	t.Cleanup(func() { FormatTimeout = was })
}

// Several statements formatted one by one are what one call lays out of them:
// the same text, whatever the comments, blanks and ;s between, in either
// keyword case; what the split has no account of goes in one call (F13.19).
func TestFormatAllIsFormat(t *testing.T) {
	slowFormat(t)
	st := "select a.id, b.name, count(*) from orders a join users b on a.uid = b.id where a.ts > now() - interval '1 day' group by 1, 2 having count(*) > 3;\n"
	for _, c := range []struct {
		d Dialect
		s string
	}{
		{PG, strings.Repeat(st, 12)},
		{PG, "select 1; select 2"},
		{PG, "select 1;\n\n\n-- note\nselect 2;\n"},
		{PG, "select 1; -- trailing\nselect 2; /* block */"},
		{PG, "select 1;;select 2;"},
		{PG, "-- head\nselect 1;\nselect 2;\n-- tail\n"},
		{PG, "select 1;\n\n-- a\n-- b\n\nselect 2;"},
		{PG, "select 1 /* c */ ; select 2;"},
		{PG, "select 1;\n\n-- a\n\n-- b\n\nselect 2;"},
		{PG, "select 1;\n  -- a\n  -- b\nselect 2;"},
		{PG, "select 1;\n/* a\n     b */\nselect 2;"},
		{PG, "-- h1\n\n-- h2\nselect 1; select 2;"},
		{PG, "select 1; select 2; -- t1\n\n-- t2\n"},
		{PG, "select 1; /* x */ select 2;"},
		{PG, "select 1; -- x\n-- y\nselect 2;"},
		{PG, "create function f() returns int begin atomic select 1; select 2; end; select 3;"},
		{PG, "select 1; select 2 -- tail no semi"},
		{PG, "select 1"},
		{PG, ";select 1; select 2"},
		{PG, "select 1; select 2; ;"},
		{MySQL, "/*!40101 SET a=1; SET b=2 */;\nselect 1;"},
		{MySQL, "select 1; # c\nselect 2;"},
	} {
		for _, kc := range []string{"preserve", "upper"} {
			o := Options{KeywordCase: kc, TabWidth: 2}
			want, werr := Format(c.s, c.d, o)
			got, err := FormatAll(c.s, c.d, o)
			if got != want || fmt.Sprint(err) != fmt.Sprint(werr) {
				t.Errorf("%s %q:\n got %q (%v)\nwant %q (%v)", kc, c.s, got, err, want, werr)
			}
		}
	}
}

// wrapFormat has f make the formatter's calls, orig being the real one: to
// count them, or to take the time a call would (F13.19).
func wrapFormat(t *testing.T, f func(orig goja.Callable, this goja.Value, args ...goja.Value) (goja.Value, error)) {
	t.Helper()
	formatter.Lock()
	loaded := formatter.vm != nil
	formatter.Unlock()
	if !loaded {
		if _, err := Format("select 1", PG, Options{KeywordCase: "upper", TabWidth: 2}); err != nil { // the VM is made
			t.Fatal(err)
		}
	}
	formatter.Lock()
	orig := formatter.format
	formatter.format = func(this goja.Value, args ...goja.Value) (goja.Value, error) { return f(orig, this, args...) }
	formatter.Unlock()
	t.Cleanup(func() {
		formatter.Lock()
		formatter.format = orig
		formatter.Unlock()
	})
}

// Several statements are a call each, twelve for twelve, and the lot has the
// one FormatTimeout: once it has passed no call is made, whatever the one
// before took (F13.19).
func TestFormatAllIsOneCallEachUnderOneDeadline(t *testing.T) {
	slowFormat(t)
	o := Options{KeywordCase: "upper", TabWidth: 2}
	s := strings.Repeat("select a.id, b.name from orders a join users b on a.uid = b.id;\n", 12)
	calls := 0
	wrapFormat(t, func(orig goja.Callable, this goja.Value, args ...goja.Value) (goja.Value, error) {
		calls++
		return orig(this, args...)
	})
	if _, err := FormatAll(s, PG, o); err != nil || calls != 12 {
		t.Fatalf("%d calls for 12 statements: %v", calls, err)
	}
	FormatTimeout = 50 * time.Millisecond
	wrapFormat(t, func(_ goja.Callable, _ goja.Value, _ ...goja.Value) (goja.Value, error) {
		calls++
		time.Sleep(100 * time.Millisecond) // the first takes the time there is
		return goja.Undefined(), nil
	})
	calls = 0
	if _, err := FormatAll(s, PG, o); !errors.Is(err, ErrTimeout) || calls != 1 {
		t.Errorf("past the one deadline: %d calls, %v", calls, err)
	}
}

// What a format said is not turned into a timeout by the second go at it, made
// for where it was, and the first statement is not gone at twice (F13.19).
func TestFormatAllErrorIsKept(t *testing.T) {
	o := Options{KeywordCase: "upper", TabWidth: 2}
	parse := errors.New("parse error")
	t.Run("first", func(t *testing.T) {
		slowFormat(t)
		calls := 0
		wrapFormat(t, func(_ goja.Callable, _ goja.Value, _ ...goja.Value) (goja.Value, error) {
			calls++
			return nil, parse
		})
		calls = 0
		if _, err := FormatAll("select 1; select 2;", PG, o); !errors.Is(err, parse) || calls != 1 {
			t.Errorf("the first statement fails: %d calls, %v", calls, err)
		}
	})
	t.Run("second", func(t *testing.T) {
		slowFormat(t)
		FormatTimeout = 200 * time.Millisecond
		calls := 0
		wrapFormat(t, func(orig goja.Callable, this goja.Value, args ...goja.Value) (goja.Value, error) {
			if calls++; calls == 1 {
				return orig(this, args...)
			}
			time.Sleep(300 * time.Millisecond) // the second fails, past the deadline
			return nil, parse
		})
		calls = 0
		if _, err := FormatAll("select 1; select 2;", PG, o); !errors.Is(err, parse) || calls != 2 {
			t.Errorf("the second statement fails, and the time is gone: %d calls, %v", calls, err)
		}
	})
}

// A syntax error is where it is in the text, not in the statement it is in
// (F13.19).
func TestFormatAllErrorPosition(t *testing.T) {
	slowFormat(t)
	o := Options{KeywordCase: "upper", TabWidth: 2}
	for _, s := range []string{"select 1;\nselect 2;\nselect 'a", "select 1; select 2; select 3 'x", "select '😀'; select 'x"} {
		_, want := Format(s, PG, o)
		_, got := FormatAll(s, PG, o)
		if want == nil || got == nil || got.Error() != want.Error() {
			t.Errorf("%q: got %v, want %v", s, got, want)
		}
	}
}

// Each statement is a call of its own: 48 of them take what a few cost, where
// one call over the lot took seconds (F13.19).
func BenchmarkFormatAll48(b *testing.B) {
	o := Options{KeywordCase: "upper", TabWidth: 2}
	s := strings.Repeat("select a.id, b.name, count(*) from orders a join users b on a.uid = b.id where a.ts > now() - interval '1 day' group by 1, 2 having count(*) > 3;\n", 48)
	for range b.N {
		if _, err := FormatAll(s, PG, o); err != nil {
			b.Fatal(err)
		}
	}
}
