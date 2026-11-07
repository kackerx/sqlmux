package db

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// OrderCol is a column an ORDER sorts by and the way (§7.6, F12.1).
type OrderCol struct {
	Col  string `toml:"col" json:"col"`
	Desc bool   `toml:"desc,omitempty" json:"desc,omitempty"`
}

// Query is what a data tab asks of its table (§8.5, §9.6).
type Query struct {
	Schema, Table string
	Where         string     // a condition as typed; "" for none
	Order         []OrderCol // the columns to sort by, in order; none for Key alone
	Key           []string   // the row identity: the order, or Order's tiebreaker
	Lead          string     // listed before *, as Columns.Lead (F14.1)
	Limit, Offset int
}

// from is "FROM t" and the WHERE: the input as typed, in parentheses so its
// OR binds inside them, and on lines of its own so a trailing -- comment
// can't swallow what follows (§9.6).
func (q Query) from(e Engine) string {
	s := " from " + e.Quote(q.Schema, q.Table)
	if strings.TrimSpace(q.Where) != "" {
		s += " where (\n" + q.Where + "\n)"
	}
	return s
}

// Page reads one page of a table (§8.5): every column, sorted by Order and
// then the row identity, the columns of it Order has left out, so pages are
// stable (unordered with neither). It
// asks for one row more than Limit to tell whether a next page exists, even
// before the count is in. One statement only: PG's extended protocol and a
// MySQL connection without multiStatements refuse more, whatever Where
// holds.
func Page(ctx context.Context, c Conn, e Engine, q Query) (r Result, next bool, err error) {
	r, err = c.Query(ctx, Select(e, q)+placeOf(e, q, q.Limit+1, q.Offset))
	if next = len(r.Rows) > q.Limit; next {
		r.Rows = r.Rows[:q.Limit]
	}
	return r, next, err
}

// placeOf is what a page or an export appends for its place: LIMIT and
// OFFSET, or, an engine of SQL Server's, the OFFSET/FETCH its §8.5 has, and
// the ORDER BY that goes before it — (SELECT NULL) when neither Order nor
// Key sorts, a T-SQL OFFSET without one being an error.
func placeOf(e Engine, q Query, limit, offset int) string {
	if e.Dialect() == sqlkit.MSSQL {
		s := fmt.Sprintf(" offset %d rows fetch next %d rows only", offset, limit)
		if len(q.Order) == 0 && len(q.Key) == 0 {
			s = " order by (select null)" + s
		}
		return s
	}
	if e.Dialect() == sqlkit.Trino { // OFFSET before LIMIT, as its grammar has them (§8.10)
		return fmt.Sprintf(" offset %d limit %d", offset, limit)
	}
	return fmt.Sprintf(" limit %d offset %d", limit, offset)
}

// Select is the statement Page asks, with no LIMIT and no OFFSET: every row
// of the table the condition keeps, in the order q gives (F12.4).
func Select(e Engine, q Query) string {
	var order []string
	for _, o := range q.Order {
		dir := " asc"
		if o.Desc {
			dir = " desc"
		}
		order = append(order, e.Quote(o.Col)+dir)
	}
	for _, k := range q.Key {
		switch {
		case slices.ContainsFunc(q.Order, func(o OrderCol) bool { return o.Col == k }):
		default:
			order = append(order, e.Quote(k))
		}
	}
	sql := "select " + q.Lead + "*" + q.from(e)
	if len(order) > 0 {
		sql += " order by " + strings.Join(order, ", ")
	}
	return sql
}

// Count counts the rows q's condition keeps (§8.5); the caller bounds it
// with its ctx. SQL Server's is COUNT_BIG, its COUNT an int that overflows
// on a big table.
func Count(ctx context.Context, c Conn, e Engine, q Query) (int64, error) {
	count := "count(*)"
	if e.Dialect() == sqlkit.MSSQL {
		count = "count_big(*)"
	}
	r, err := c.Query(ctx, "select "+count+q.from(e))
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(r.Rows[0][0].S, 10, 64)
}
