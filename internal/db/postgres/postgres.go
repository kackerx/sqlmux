// Package postgres is db.Conn over pgconn (tech-design §8.1): the simple
// protocol for Exec, the extended one for Query, every value as text.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

type Conn struct {
	pg *pgconn.PgConn
}

// cancelGrace is how long a cancelled request may take to stop on the
// server before the connection is dropped instead (§8.3); the Worker dials
// again on the next request.
const cancelGrace = 5 * time.Second

// Connect opens one connection, and says where it is: the address for the
// status bar (§7.8), the database for query titles (§8.6), and search_path as
// it started, before any query sets its own schema in front of it (§8.6). dsn
// goes through pgconn.ParseConfig, which reads PG* variables and ~/.pgpass;
// password, when not "", wins over both. readOnly makes every transaction read
// only, as the Meta connection is (§8.2).
func Connect(ctx context.Context, dsn, password string, readOnly bool) (*Conn, db.Info, error) {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return nil, db.Info{}, err
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
	if d, ok := db.DialerOf(ctx); ok { // through ssh: the name is the other end's to look up (F15.2)
		cfg.DialFunc = pgconn.DialFunc(d.Dial)
		cfg.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil }
	}
	pg, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil && readOnly && unsupportedStartup(err) {
		// A pooler (PgBouncer, Odyssey, …) takes only the startup parameters
		// it tracks. Dial again without it: a SET instead would stay on a
		// server connection the pool hands to others, so read only here is
		// sqlmux's own guard alone (§13).
		delete(cfg.RuntimeParams, "default_transaction_read_only")
		pg, err = pgconn.ConnectConfig(ctx, cfg)
	}
	if err != nil {
		return nil, db.Info{}, err
	}
	c := &Conn{pg: pg}
	r, err := c.Query(ctx, "select current_setting('search_path'), current_database()") // the DSN may name no database
	if err != nil {
		c.Close()
		return nil, db.Info{}, err
	}
	return c, db.Info{Addr: fmt.Sprintf("%s@%s:%d", cfg.User, cfg.Host, cfg.Port), Database: r.Rows[0][1].S, Path: r.Rows[0][0].S}, nil
}

// unsupportedStartup is whether err is a server, or a pooler before it,
// refusing a startup parameter it does not know.
func unsupportedStartup(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "08P01" && strings.Contains(pe.Message, "unsupported startup parameter")
}

// WithDatabase is dsn naming database name instead of the one it names: a
// URL's path, its query's dbname and database dropped, or, for the keywords
// form, a dbname the later of the two wins with (M15 F15.1).
func WithDatabase(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Del("dbname") // the query's are read after the path, and win
		q.Del("database")
		u.Path, u.RawQuery = "/"+name, q.Encode()
		return u.String(), nil
	}
	return dsn + " dbname='" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name) + "'", nil
}

// UseSchema puts schema first on c's search_path, then started, the path
// it connected with, so extensions there are still found (§8.6). By
// set_config with parameters: started may be "" or $user,public, which
// SET … TO written out cannot take.
func (Engine) UseSchema(ctx context.Context, c db.Conn, schema, started string) error {
	_, err := c.Query(ctx, "select set_config('search_path', concat_ws(', ', $1::text, nullif($2::text, '')), false)", db.Val{S: pgx.Identifier{schema}.Sanitize()}, db.Val{S: started})
	return err
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

// Stream reads sql's rows as they come, one at a time, the extended protocol
// as Query has it (F12.4). The statement is cancelled when row or head fail.
// Reference: lazysql drivers/query_stream.go reads database/sql rows in a
// goroutine, 100 a batch, and cancels the query's context when the consumer
// fails; here the Worker owns the connection, so each row goes to the caller
// as it comes, on pgconn's reader, and the cancel is the server's.
func (c *Conn) Stream(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := each(c.pg.ExecParams(ctx, sql, nil, nil, nil, nil), cancel, head, func() bool { return true }, row)
	return err
}

func (c *Conn) Close() error { return c.pg.Close(context.Background()) }

// Closed is whether pgconn has closed the connection: the server dropped
// it, or a cancel took too long (§8.3).
func (c *Conn) Closed() bool { return c.pg.IsClosed() }

// TxStatus is where the last request left the connection, as the
// server's ReadyForQuery said: 'I' out of a transaction, 'T' in one, 'E'
// in one that failed (§8.6).
func (c *Conn) TxStatus() byte {
	if c.pg.IsClosed() { // the server rolled it back as it went; pgconn's status stays as it was
		return 'I'
	}
	return c.pg.TxStatus()
}

// types names the built-in OIDs; others (enums, domains) are left unnamed.
var types = pgtype.NewMap()

// read collects one result, keeping at most maxRows rows (0: all). The rest
// is read and dropped, not cancelled: cancelling would stop the statement,
// rolling back a write with RETURNING and skipping Exec's next statements.
func read(rr *pgconn.ResultReader, maxRows int, start time.Time) (db.Result, error) {
	var r db.Result
	tag, err := each(rr, nil, func(cols []db.Col) error { r.Cols = cols; return nil }, db.Capped(&r, maxRows),
		func(vals []db.Val) error { r.Rows = append(r.Rows, vals); return nil })
	r.Tag, r.Affected, r.Took = tag.String(), tag.RowsAffected(), time.Since(start)
	return r, err
}

// each reads the result in rr: head with its columns, then row with each of
// the rows want says to keep, the others read and dropped. Closed, it is the
// command's tag and the server's error; head or row failing, it is that, the
// rest not read: stop, when there is one, cancels the statement first, as the
// rest would be dropped.
func each(rr *pgconn.ResultReader, stop func(), head func([]db.Col) error, want func() bool, row func([]db.Val) error) (pgconn.CommandTag, error) {
	var cols []db.Col
	for _, f := range rr.FieldDescriptions() {
		col := db.Col{Name: f.Name}
		if t, ok := types.TypeForOID(f.DataTypeOID); ok {
			col.Type = t.Name
		}
		cols = append(cols, col)
	}
	err := head(cols)
	if err != nil && len(cols) == 0 { // a statement that failed described nothing: its error is the news, not what head made of that
		if _, cerr := rr.Close(); cerr != nil {
			return pgconn.CommandTag{}, cerr
		}
	}
	for err == nil && rr.NextRow() {
		if !want() {
			continue
		}
		vals := make([]db.Val, len(cols))
		for i, v := range rr.Values() {
			vals[i] = db.Val{S: string(v), Null: v == nil}
		}
		err = row(vals)
	}
	if err != nil {
		if stop != nil {
			stop()
		}
		_, _ = rr.Close()
		return pgconn.CommandTag{}, err
	}
	return rr.Close()
}

// Engine is PG's way with the catalog, SQL and errors (§8.1).
type Engine struct{}

func (Engine) Connect(ctx context.Context, dsn, password string, readOnly bool) (db.Conn, db.Info, error) {
	c, info, err := Connect(ctx, dsn, password, readOnly)
	if err != nil {
		return nil, db.Info{}, err
	}
	return c, info, nil
}

func (Engine) Eq(col, arg string) string { return col + " = " + arg }

func (Engine) Dialect() sqlkit.Dialect      { return sqlkit.PG }
func (Engine) Quote(names ...string) string { return pgx.Identifier(names).Sanitize() }
func (Engine) Arg(n int) string             { return fmt.Sprintf("$%d", n) }

// Same is format's text, the type's output function's as what was read
// is: json and point have no =, and ::text isn't always the output (false
// for f, inet with its mask, char(n) trimmed).
func (Engine) Same(col, arg string) string { return "format('%s', " + col + ") = " + arg }
