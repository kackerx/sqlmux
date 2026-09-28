package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"sqlmux/internal/db"
)

// Row is one row's changes to save (§10.3): its row identity's values as
// loaded, and the changed columns in table order.
type Row struct {
	Key  []string
	Cols []Change
}

// Change is one changed column of a Row.
type Change struct {
	Name    string
	Val     db.Val // the new value; unused when Default
	Default bool   // the DEFAULT keyword
	Old     db.Val // as loaded: the row must still hold it
}

// ErrStale is a row that no longer holds what was loaded, or is gone.
var ErrStale = errors.New("行数据已变化或行不存在")

// endTimeout bounds COMMIT and ROLLBACK, which a cancel must not reach: a
// COMMIT the server has done would come back as an error (§10.3).
const endTimeout = 10 * time.Second

// Save writes rows to schema.table in one transaction on w (§10.3): an
// UPDATE a row, found by keyCols with = and each changed column's loaded
// value compared as text (json and point have no =), each hitting exactly
// one row. On any failure it rolls back; failed is the index of the row at
// fault, -1 for none. Cancelling ctx stops the UPDATEs only.
// Reference: lazysql drivers/utils.go queriesInTransaction, which checks
// neither the rows hit nor the loaded values.
func Save(ctx context.Context, w *db.Worker, schema, table string, keyCols []string, rows []Row) (failed int, err error) {
	failed = -1
	err = w.Run(ctx, func(ctx context.Context, c db.Conn) error {
		end := func(sql string) error {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), endTimeout)
			defer cancel()
			_, err := c.Query(ctx, sql)
			return err
		}
		if _, err := c.Query(ctx, "begin"); err != nil {
			end("rollback") // a cancel may land once the server has begun
			return err
		}
		for i, r := range rows {
			sql, args := update(schema, table, keyCols, r)
			res, err := c.Query(ctx, sql, args...)
			if err == nil && res.Tag != "UPDATE 1" {
				err = ErrStale
			}
			if err != nil {
				failed = i
				end("rollback")
				return err
			}
		}
		return end("commit")
	})
	return failed, err
}

// update is r's UPDATE and its arguments, all text.
func update(schema, table string, keyCols []string, r Row) (string, []db.Val) {
	var sets, where []string
	var args []db.Val
	arg := func(v db.Val) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	for _, c := range r.Cols {
		v := "DEFAULT"
		if !c.Default {
			v = arg(c.Val)
		}
		sets = append(sets, pgx.Identifier{c.Name}.Sanitize()+" = "+v)
	}
	for i, k := range keyCols {
		where = append(where, pgx.Identifier{k}.Sanitize()+" = "+arg(db.Val{S: r.Key[i]}))
	}
	for _, c := range r.Cols {
		where = append(where, pgx.Identifier{c.Name}.Sanitize()+"::text is not distinct from "+arg(c.Old))
	}
	return "update " + pgx.Identifier{schema, table}.Sanitize() + " set " + strings.Join(sets, ", ") +
		" where " + strings.Join(where, " and "), args
}
