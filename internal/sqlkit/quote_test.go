package sqlkit

import "testing"

// A name is quoted when read bare it would be another: not a word, folded by
// PG, reserved.
func TestNeedsQuote(t *testing.T) {
	for _, c := range []struct {
		name string
		d    Dialect
		want bool
	}{
		{"status", PG, false}, {"t_order2", PG, false}, {"a$b", PG, false}, {"_x", PG, false}, {"中文", PG, false}, {"名前x", PG, false},
		{"Status", PG, true}, {"createdAt", PG, true}, {"a b", PG, true}, {"a-b", PG, true}, {"1a", PG, true}, {"", PG, true}, {"a.b", PG, true}, {`a"b`, PG, true},
		{"user", PG, true}, {"order", PG, true}, {"ORDER", PG, true}, {"left", PG, true}, {"verbose", PG, true}, {"system_user", PG, true},
		{"name", PG, false}, {"type", PG, false}, {"comment", PG, false}, {"key", PG, false}, {"first", PG, false}, {"index", PG, false},
		{"Status", MySQL, false}, {"createdAt", SQLite, false}, {"a b", MySQL, true}, {"1a", SQLite, true},
		{"order", MySQL, true}, {"key", MySQL, true}, {"Order", SQLite, true}, {"status", MySQL, false},
		// each dialect's own reserved words: what only its list has is quoted, what only the keyword table has is not
		{"rank", MySQL, true}, {"condition", MySQL, true}, {"option", MySQL, true}, {"int", MySQL, true}, {"read", MySQL, true},
		{"comment", MySQL, false}, {"first", MySQL, false}, {"view", MySQL, false},
		{"autoincrement", SQLite, true}, {"isnull", SQLite, true}, {"notnull", SQLite, true}, {"raise", SQLite, true}, {"deferrable", SQLite, true},
		{"comment", SQLite, false}, {"rank", SQLite, false}, {"rank", PG, false},
		// DuckDB: PG's words, and names that go by any case need no quotes for a capital
		{"Status", DuckDB, false}, {"createdAt", DuckDB, false}, {"order", DuckDB, true}, {"a b", DuckDB, true}, {"1a", DuckDB, true},
	} {
		if got := c.d.NeedsQuote(c.name); got != c.want {
			t.Errorf("%q in %v: %v", c.name, c.d, got)
		}
	}
}
