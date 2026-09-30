package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"sqlmux/internal/db"
	"sqlmux/internal/sqlkit"
)

// Quick runs sql, one statement IsRead takes, for the palette's quick SQL
// (§12): in a read-only transaction that is always rolled back, with
// schema first on the search path. A query (sqlkit.IsQuery) goes through a
// cursor, so the server stops at maxRows+1 rows instead of working out all
// of them, as PG 16 psql's FETCH_COUNT did; the rest run as they are. At
// most maxRows rows come back, Truncated when there were more.
// ponytail: what is no query runs whole, as §12's known limit has it:
// explain analyze of a big join works it all out
func Quick(ctx context.Context, w *db.Worker, sql, schema string, maxRows int) (r db.Result, err error) {
	err = w.Run(ctx, func(ctx context.Context, c db.Conn) error {
		if _, err := c.Query(ctx, "begin read only"); err != nil {
			return err
		}
		// not on ctx: a cancelled request still ends its transaction
		defer c.Query(context.Background(), "rollback")
		// SET LOCAL search_path TO <schema>, <as connected> (§8.6): Meta
		// never sets it for good, so the current path is the one it connected with.
		path := "select set_config('search_path', $1 || ', ' || current_setting('search_path'), true)"
		if _, err := c.Query(ctx, path, db.Val{S: pgx.Identifier{schema}.Sanitize()}); err != nil {
			return err
		}
		start := time.Now()
		if sqlkit.IsQuery(sql, sqlkit.PG) {
			if _, err := c.Query(ctx, "declare sqlmux_quick no scroll cursor for "+sql); err != nil {
				return err
			}
			sql = fmt.Sprintf("fetch forward %d from sqlmux_quick", maxRows+1)
		}
		r, err = c.Query(ctx, sql)
		if len(r.Rows) > maxRows {
			r.Rows, r.Truncated = r.Rows[:maxRows], true
		}
		r.Took = time.Since(start)
		return err
	})
	return r, err
}
