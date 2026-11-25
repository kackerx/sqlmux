package sqlkit

// Dangerous is whether statement s is one to confirm before it runs (§13
// 「危险语句先确认」): a DELETE or UPDATE with no WHERE at its own level, a
// parenthesised one included (a CTE body, COPY (…)), a TRUNCATE, a DROP of
// anything, an ALTER TABLE that drops a column or a partition; on PG an
// EXPLAIN ANALYZE of one of them runs it. Only what the words say: what a
// function or a DO block does, or a WHERE that matches every row, is not
// seen. A guard against slips, not a permission (§13).
// ponytail: an ALTER TABLE … DROP that is not in keeps counts as a column's;
// a new clause of the database's that is none wants its word there.
func Dangerous(s string, d Dialect) bool {
	ts, depth := words(s, d)
	return dangerous(s, ts, depth, d)
}

func dangerous(s string, ts []Token, depth []int, d Dialect) bool {
	i := first(s, ts)
	if BeginsBareExec(s, d) { // the EXEC that is not written: T-SQL takes the name for it (§9.2, F18.6)
		return true
	}
	main := -1 // the DELETE or UPDATE that is the statement, when it is one
	switch lower(s, ts, i) {
	case "truncate", "drop":
		return true
	case "exec", "execute": // T-SQL's runs whatever its procedure says (F18.6)
		return d == MSSQL
	case "delete", "update":
		main = i
	case "alter":
		return lower(s, ts, i+1) == "table" && dropsData(s, ts, i, d)
	case "explain": // MySQL's only plans; PG's and DuckDB's ANALYZE runs it
		analyze, j := explainOptions(s, ts[i+1:])
		j += i + 1
		return (d == PG || d == DuckDB) && analyze && j < len(ts) && dangerous(s, ts[j:], depth[j:], d)
	case "with": // the statement after the CTEs, whose bodies are in parentheses
		main = withMain(s, ts, depth, i)
	}
	for j := range ts {
		if w := lower(s, ts, j); (w == "delete" || w == "update") && (j == main || lower(s, ts, j-1) == "(") && noWhere(s, ts, depth, j) {
			return true
		}
	}
	return false
}

// withMain is the index of the DELETE or UPDATE after the CTEs of the WITH at
// ts[i], -1 when the statement is another.
func withMain(s string, ts []Token, depth []int, i int) int {
	for j := i + 1; j < len(ts); j++ {
		if depth[j] != depth[i] {
			continue
		}
		switch lower(s, ts, j) {
		case "delete", "update":
			return j
		case "select", "insert", "merge", "values", "table":
			return -1
		}
	}
	return -1
}

// noWhere is whether the DELETE or UPDATE at ts[i] has no WHERE of its own:
// one in a subquery is deeper, and one past the closing parenthesis it is in
// is another's.
func noWhere(s string, ts []Token, depth []int, i int) bool {
	for j := i + 1; j < len(ts) && depth[j] >= depth[i]; j++ {
		if depth[j] == depth[i] && lower(s, ts, j) == "where" {
			return false
		}
	}
	return true
}

// keeps are what follows a clause's DROP in an ALTER TABLE that takes no
// column. PG's other words are no keywords there: `drop key` drops a column
// named key (§13, tried on PG 17); MySQL reserves these.
var keeps = map[Dialect]map[string]bool{
	PG:     {"constraint": true},
	MySQL:  {"constraint": true, "index": true, "key": true, "primary": true, "foreign": true, "check": true},
	MSSQL:  {"constraint": true},
	DuckDB: {"constraint": true},
}

// dropsData is whether the ALTER TABLE at ts[i] drops a column, or on MySQL
// a partition's rows. A DROP right after ALTER [COLUMN] <name> (default,
// not null, identity, expression) changes the column, nothing goes.
func dropsData(s string, ts []Token, i int, d Dialect) bool {
	for j := i + 2; j < len(ts); j++ {
		switch lower(s, ts, j) {
		case "truncate": // MySQL's TRUNCATE PARTITION
			if d == MySQL && lower(s, ts, j+1) == "partition" {
				return true
			}
		case "drop":
			if lower(s, ts, j-2) == "alter" || lower(s, ts, j-3) == "alter" && lower(s, ts, j-2) == "column" {
				continue
			}
			if next := lower(s, ts, j+1); next != "" && !keeps[d][next] {
				return true
			}
		}
	}
	return false
}
