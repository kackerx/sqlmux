//go:build !linux || duckdb

package duckdb

import (
	"context"
	"regexp"

	"github.com/kackerx/sqlmux/internal/db"
)

// Engine is DuckDB's way with errors and connections (§8.1); the rest of an
// engine is F24.2's.
type Engine struct{}

func (Engine) Connect(ctx context.Context, dsn, password string, readOnly bool) (db.Conn, db.Info, error) {
	conn, info, err := Connect(ctx, dsn, password, readOnly)
	if err != nil {
		return nil, info, err
	}
	return conn, info, nil
}

// Broken is none: a database in this process is no connection to lose.
func (Engine) Broken(error) db.Lost { return db.NotLost }

// category is the start of an error of the library's: <category> Error: then the
// message (Binder, Catalog, Constraint, INTERRUPT, Invalid Input, Out of Memory).
var category = regexp.MustCompile(`(?s)^([^\n]+?) Error: (.*)$`)

// ErrorOf is err as the log and an error bar show it (§11): the category is
// the code, the rest the message; there is no SQLSTATE.
func (Engine) ErrorOf(err error) db.ServerError {
	e := db.ServerError{Severity: "ERROR", Message: err.Error()}
	if m := category.FindStringSubmatch(e.Message); m != nil {
		e.Code, e.Message = m[1], m[2]
	}
	return e
}
