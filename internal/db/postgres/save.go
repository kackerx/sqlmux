package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

// ServerError is err as the result area's log and an error bar show it
// (§11, §7.8「错误栏」): a server's Severity, SQLSTATE and Message without
// pgconn's dressing, its DETAIL and HINT a line each (DETAIL's own lines
// apart), and Position, the statement's character it points at from 1, 0
// for none. An error no server sent is ERROR and its text alone.
type ServerError struct {
	Severity, Code, Message string
	More                    []string
	Position                int
}

func ServerErrorOf(err error) ServerError {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return ServerError{Severity: "ERROR", Message: err.Error()}
	}
	e := ServerError{Severity: pe.Severity, Code: pe.Code, Message: pe.Message, Position: int(pe.Position)}
	for _, l := range [][2]string{{"DETAIL", pe.Detail}, {"HINT", pe.Hint}} {
		if l[1] != "" {
			e.More = append(e.More, strings.Split(l[0]+": "+l[1], "\n")...)
		}
	}
	return e
}

// ErrorLines is err as the result area's log shows it (§11): "ERROR:
// <Message>", then the DETAIL and HINT lines.
func ErrorLines(err error) []string {
	e := ServerErrorOf(err)
	return append([]string{e.Severity + ": " + e.Message}, e.More...)
}

// endTimeout bounds COMMIT and ROLLBACK, which a cancel must not reach: a
// COMMIT the server has done would come back as an error (§10.3).
const endTimeout = 10 * time.Second

// Save writes rows to schema.table in one transaction on w (§10.3): an
// UPDATE a row, found by keyCols with = and each changed column still
// holding what was loaded, each hitting exactly one row. On any failure it rolls back; failed is the index of the row at
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
	// What was loaded is the type's output function's text, and so is
	// format's; json and point have no =, and ::text isn't always the output
	// (false for f, inet with its mask, char(n) trimmed).
	for _, c := range r.Cols {
		col := pgx.Identifier{c.Name}.Sanitize()
		if c.Old.Null {
			where = append(where, col+" is null")
		} else {
			where = append(where, "format('%s', "+col+") = "+arg(c.Old))
		}
	}
	return "update " + pgx.Identifier{schema, table}.Sanitize() + " set " + strings.Join(sets, ", ") +
		" where " + strings.Join(where, " and "), args
}
