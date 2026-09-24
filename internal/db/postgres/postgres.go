// Package postgres is db.Conn over pgconn (tech-design §8.1): the simple
// protocol for Exec, the extended one for Query, every value as text.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgtype"

	"sqlmux/internal/db"
)

type Conn struct {
	pg   *pgconn.PgConn
	Addr string // user@host:port, for the status bar (§7.8)
	// SearchPath is search_path as the connection started, before any
	// console sets its own schema in front of it (§8.6).
	SearchPath string
}

// cancelGrace is how long a cancelled request may take to stop on the
// server before the connection is dropped instead (§8.3).
// ponytail: a dropped connection stays dropped; reconnect when that bites.
const cancelGrace = 5 * time.Second

// Connect opens one connection. dsn goes through pgconn.ParseConfig, which
// reads PG* variables and ~/.pgpass; password, when not "", wins over both.
// readOnly makes every transaction read only, as the Meta connection is (§8.2).
func Connect(ctx context.Context, dsn, password string, readOnly bool) (*Conn, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if password != "" {
		cfg.Password = password
	}
	if _, ok := cfg.RuntimeParams["application_name"]; !ok {
		cfg.RuntimeParams["application_name"] = "sqlmux"
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}
	// Startup parameters: set on every connection with no round trip (§8.1).
	cfg.RuntimeParams["DateStyle"] = "ISO, YMD" // time text the cell editor can parse (§10.2)
	if readOnly {
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
	}
	// pgconn's default handler closes the connection when a ctx is cancelled;
	// a session only has two, so ask the server to cancel instead (§8.3).
	cfg.BuildContextWatcherHandler = func(pg *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: pg, DeadlineDelay: cancelGrace}
	}
	pg, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c := &Conn{pg: pg, Addr: fmt.Sprintf("%s@%s:%d", cfg.User, cfg.Host, cfg.Port)}
	r, err := c.Query(ctx, "show search_path")
	if err != nil {
		c.Close()
		return nil, err
	}
	c.SearchPath = r.Rows[0][0].S
	return c, nil
}

func (c *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	var out []db.Result
	start := time.Now()
	mrr := c.pg.Exec(ctx, sql)
	for mrr.NextResult() {
		r, err := read(mrr.ResultReader(), maxRows, start)
		if err != nil {
			mrr.Close()
			return out, err
		}
		out = append(out, r)
		start = time.Now() // the next statement starts where this one ended
	}
	return out, mrr.Close()
}

func (c *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	params := make([][]byte, len(args))
	for i, a := range args {
		if !a.Null {
			params[i] = []byte(a.S)
		}
	}
	// nil OIDs and formats: the server infers each parameter's type, and
	// everything travels as text.
	start := time.Now()
	return read(c.pg.ExecParams(ctx, sql, params, nil, nil, nil), 0, start)
}

func (c *Conn) Close() error { return c.pg.Close(context.Background()) }

// types names the built-in OIDs; others (enums, domains) are left unnamed.
var types = pgtype.NewMap()

// read collects one result, keeping at most maxRows rows (0: all). The rest
// is read and dropped, not cancelled: cancelling would stop the statement,
// rolling back a write with RETURNING and skipping Exec's next statements.
func read(rr *pgconn.ResultReader, maxRows int, start time.Time) (db.Result, error) {
	var r db.Result
	for _, f := range rr.FieldDescriptions() {
		col := db.Col{Name: f.Name}
		if t, ok := types.TypeForOID(f.DataTypeOID); ok {
			col.Type = t.Name
		}
		r.Cols = append(r.Cols, col)
	}
	for rr.NextRow() {
		if maxRows > 0 && len(r.Rows) == maxRows {
			r.Truncated = true
			continue
		}
		row := make([]db.Val, len(r.Cols))
		for i, v := range rr.Values() {
			row[i] = db.Val{S: string(v), Null: v == nil}
		}
		r.Rows = append(r.Rows, row)
	}
	tag, err := rr.Close()
	r.Tag, r.Took = tag.String(), time.Since(start)
	return r, err
}
