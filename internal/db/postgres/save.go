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

// ErrStale is a row that no longer holds what was loaded, or is gone; ErrGone
// one to delete that is gone.
var (
	ErrStale = errors.New("行数据已变化或行不存在")
	ErrGone  = errors.New("行不存在")
)

// Changes is what a save writes (§10.3, §10.6): the rows to delete, by
// their row identity's values; the rows to update; the rows to insert,
// each the columns set in it, Val or Default, the rest their DEFAULT.
type Changes struct {
	Deletes [][]string
	Updates []Row
	Inserts [][]Change
}

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

// Save writes ch to schema.table in one transaction on w (§10.3, §10.6):
// first the DELETEs, then the UPDATEs, then the INSERTs, each hitting
// exactly one row. A row is found by keyCols with =; an UPDATE's also by
// each changed column still holding what was loaded. On any failure it
// rolls back; failed is the index of the statement at fault in that
// order, deletes, updates, inserts, -1 for none. Cancelling ctx stops the
// statements only.
// Reference: lazysql drivers/utils.go queriesInTransaction and its
// ExecutePendingChanges, which run the changes as listed and check
// neither the rows hit nor the loaded values; its INSERT leaves a DEFAULT
// column out, where ours writes DEFAULT.
func Save(ctx context.Context, w *db.Worker, schema, table string, keyCols []string, ch Changes) (failed int, err error) {
	type stmt struct {
		sql  string
		args []db.Val
		tag  string
		miss error // when the tag isn't
	}
	var stmts []stmt
	for _, k := range ch.Deletes {
		sql, args := remove(schema, table, keyCols, k)
		stmts = append(stmts, stmt{sql, args, "DELETE 1", ErrGone})
	}
	for _, r := range ch.Updates {
		sql, args := update(schema, table, keyCols, r)
		stmts = append(stmts, stmt{sql, args, "UPDATE 1", ErrStale})
	}
	for _, r := range ch.Inserts {
		sql, args := insert(schema, table, r)
		stmts = append(stmts, stmt{sql, args, "INSERT 0 1", ErrStale})
	}
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
		for i, s := range stmts {
			res, err := c.Query(ctx, s.sql, s.args...)
			if err == nil && res.Tag != s.tag {
				err = s.miss
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

// keyWhere is what finds a row by its row identity's values, arg taking
// each value in.
func keyWhere(keyCols, key []string, arg func(db.Val) string) []string {
	var where []string
	for i, k := range keyCols {
		where = append(where, pgx.Identifier{k}.Sanitize()+" = "+arg(db.Val{S: key[i]}))
	}
	return where
}

// argsOf is a statement's arguments, and what numbers each one in.
func argsOf() (*[]db.Val, func(db.Val) string) {
	var args []db.Val
	return &args, func(v db.Val) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
}

// remove is the DELETE of the row with key, and its arguments.
func remove(schema, table string, keyCols, key []string) (string, []db.Val) {
	args, arg := argsOf()
	return "delete from " + pgx.Identifier{schema, table}.Sanitize() + " where " + strings.Join(keyWhere(keyCols, key, arg), " and "), *args
}

// insert is the INSERT of a row with cols set, and its arguments.
func insert(schema, table string, cols []Change) (string, []db.Val) {
	into := "insert into " + pgx.Identifier{schema, table}.Sanitize()
	if len(cols) == 0 {
		return into + " default values", nil
	}
	args, arg := argsOf()
	var names, vals []string
	for _, c := range cols {
		v := "DEFAULT"
		if !c.Default {
			v = arg(c.Val)
		}
		names, vals = append(names, pgx.Identifier{c.Name}.Sanitize()), append(vals, v)
	}
	return into + " (" + strings.Join(names, ", ") + ") values (" + strings.Join(vals, ", ") + ")", *args
}

// update is r's UPDATE and its arguments, all text.
func update(schema, table string, keyCols []string, r Row) (string, []db.Val) {
	var sets []string
	args, arg := argsOf()
	for _, c := range r.Cols {
		v := "DEFAULT"
		if !c.Default {
			v = arg(c.Val)
		}
		sets = append(sets, pgx.Identifier{c.Name}.Sanitize()+" = "+v)
	}
	where := keyWhere(keyCols, r.Key, arg)
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
		" where " + strings.Join(where, " and "), *args
}
