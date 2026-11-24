//go:build linux && !duckdb

// Package duckdb is the DuckDB engine (§8.1, F24.1), which on Linux is built in
// only with the duckdb tag: purego makes the binary dynamic, linked to glibc, and
// the Linux build that is the default stays static. This is what stands in for it
// there: a connection of DuckDB says so.
package duckdb

import (
	"context"
	"errors"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// Configure is for the real engine's library search; there is none to look.
func Configure(string, string) {}

// Engine is a driver that cannot connect (§8.1「Linux 例外」), no SQL engine:
// there is nothing of DuckDB's to read, for no connection is made.
type Engine struct{}

func (Engine) Connect(context.Context, string, string, bool) (db.Conn, db.Info, error) {
	return nil, db.Info{}, errors.New(i18n.T("这个 sqlmux 不带 DuckDB：Linux 上请用 duckdb 版的包（sqlmux_<版本>_linux_<架构>_duckdb），或 go install -tags duckdb"))
}

func (Engine) Broken(error) db.Lost { return db.NotLost }

func (Engine) ErrorOf(err error) db.ServerError {
	return db.ServerError{Severity: "ERROR", Message: err.Error()}
}
