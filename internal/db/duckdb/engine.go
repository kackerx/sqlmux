//go:build !linux || duckdb

package duckdb

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

func (Engine) Dialect() sqlkit.Dialect { return sqlkit.DuckDB }

// Quote writes names as DuckDB's identifiers, "a"."b", a " in one doubled.
func (Engine) Quote(names ...string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + strings.ReplaceAll(n, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ".")
}

func (Engine) Arg(n int) string { return "$" + strconv.Itoa(n) }

// Same is whether the column still holds what was read, arg its text, which
// the library converts to the column's type; NULL is the same as NULL (§10.3).
// In parentheses, for it goes after an AND (F18.6 j).
func (Engine) Same(col, arg string) string { return "(" + col + " is not distinct from " + arg + ")" }

// Eq is whether the column of a row's identity holds arg, the key as it was
// read (§10.1).
func (Engine) Eq(col, arg string) string { return "(" + col + " is not distinct from " + arg + ")" }

// UseSchema makes schema the first unqualified names go to: use "schema". A
// connection starts in main, which started therefore says (§8.1).
func (e Engine) UseSchema(ctx context.Context, c db.Conn, schema, _ string) error {
	if schema == "" {
		return nil
	}
	_, err := c.Query(ctx, "use "+e.Quote(schema))
	return err
}

// Quick runs sql, one statement IsRead takes, for the palette's quick SQL
// (§12), with schema first: on the Meta connection, which is read only, each
// statement a read only transaction of its own (Conn); at most maxRows rows
// come back, Truncated when there were more.
func (e Engine) Quick(ctx context.Context, w *db.Worker, sql, schema string, maxRows int) (r db.Result, err error) {
	err = w.RunSafe(ctx, func(ctx context.Context, c db.Conn) error {
		if err := e.UseSchema(ctx, c, schema, ""); err != nil {
			return err
		}
		start := time.Now()
		rs, err := c.Exec(ctx, sql, maxRows)
		if err != nil {
			return err
		}
		r = rs[0]
		r.Took = time.Since(start)
		return nil
	})
	return r, err
}

// Begin and Savepoint are the transaction's words (db.TxSaver): DuckDB has no
// savepoints, so one is set by no statement. A save that fails in a transaction
// fails the transaction (§8.1), where a stale row, which the library takes for
// no error, would leave the rows before it written: taking the save's
// savepoint back is a statement the library fails the transaction with (a cast,
// which is syntax, no macro of the user's can stand in for, as it can for a
// function). A read or an export that fails leaves its transaction as it is.
func (*Conn) Begin() string { return "begin" }

func (*Conn) Savepoint(name string) (set, rollback, release string) {
	if name != db.SaveSavepoint {
		return "select 1", "select 1", ""
	}
	return "select 1", "select cast('sqlmux: the save failed' as integer)", ""
}
