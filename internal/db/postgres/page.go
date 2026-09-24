package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"sqlmux/internal/db"
)

// Page reads one page of a table (§8.5): every column, ordered by the row
// identity key so pages are stable (unordered without one), limit rows from
// offset. It asks for one row more to tell whether a next page exists, even
// before the count is in.
func Page(ctx context.Context, c db.Conn, schema, table string, key []string, limit, offset int) (r db.Result, next bool, err error) {
	sql := "select * from " + pgx.Identifier{schema, table}.Sanitize()
	if len(key) > 0 {
		cols := make([]string, len(key))
		for i, k := range key {
			cols[i] = pgx.Identifier{k}.Sanitize()
		}
		sql += " order by " + strings.Join(cols, ", ")
	}
	r, err = c.Query(ctx, sql+fmt.Sprintf(" limit %d offset %d", limit+1, offset))
	if next = len(r.Rows) > limit; next {
		r.Rows = r.Rows[:limit]
	}
	return r, next, err
}
