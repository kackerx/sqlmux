// Package db is how the app talks to a database (tech-design §8): every
// value is text, and each connection runs one request at a time.
package db

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Val is one value as the database's text, or NULL (§8.1).
type Val struct {
	S    string
	Null bool
}

// BinaryShown is how a value that is no UTF-8 reads in the grid, by its size
// (§8.1): the bytes themselves are not kept. IsBinaryShown tells such a text.
func BinaryShown(n int) string { return fmt.Sprintf("<binary %d bytes>", n) }

var binaryShown = regexp.MustCompile(`^<binary \d+ bytes>$`)

func IsBinaryShown(s string) bool { return binaryShown.MatchString(s) }

// bytesTypes are MySQL's column types that hold bytes, not text, whatever
// the bytes of a value happen to be (PG's bytea is in hex already).
var bytesTypes = map[string]bool{"binary": true, "varbinary": true, "tinyblob": true, "blob": true, "mediumblob": true, "longblob": true}

// IsBytes is whether typ, a catalog's or a driver's (varbinary(16), VARBINARY),
// is one of them.
func IsBytes(typ string) bool {
	name, _, _ := strings.Cut(strings.ToLower(typ), "(")
	return bytesTypes[strings.TrimSpace(name)]
}

type Col struct {
	Name string
	Type string // the database's type name; "" when the engine can't tell (e.g. a PG enum)
}

type Result struct {
	Cols      []Col
	Rows      [][]Val
	Tag       string // e.g. "UPDATE 3"
	Affected  int64  // the rows an INSERT, UPDATE or DELETE hit
	Truncated bool   // there were more rows than the caller's maxRows, which it keeps
	Took      time.Duration
}

// Conn is one engine's connection (§8.1). It sits behind a Worker, which
// runs one request on it at a time; cancelling a request's ctx cancels it on
// the server.
type Conn interface {
	// Exec runs sql, PG's simple protocol taking several statements, a
	// MySQL connection one; each result keeps at most maxRows rows (0:
	// all).
	Exec(ctx context.Context, sql string, maxRows int) ([]Result, error)
	// Query runs one statement, args as text: PG's extended protocol,
	// MySQL's text one with the args in it.
	Query(ctx context.Context, sql string, args ...Val) (Result, error)
	Close() error
}

// Streamer is a Conn that hands the rows of a statement over as they come,
// not all at once (F12.4): head with the columns first, then row for each,
// the slice its own. An error of either stops it, the rest of the rows left
// unread and the statement cancelled.
type Streamer interface {
	Stream(ctx context.Context, sql string, head func([]Col) error, row func([]Val) error) error
}

// Each runs one statement on c and gives its rows to row one at a time, head
// first; a Conn that does not stream reads them all, then gives them.
func Each(ctx context.Context, c Conn, sql string, head func([]Col) error, row func([]Val) error) error {
	if s, ok := c.(Streamer); ok {
		return s.Stream(ctx, sql, head, row)
	}
	r, err := c.Query(ctx, sql)
	if err != nil {
		return err
	}
	if err := head(r.Cols); err != nil {
		return err
	}
	for _, v := range r.Rows {
		if err := row(v); err != nil {
			return err
		}
	}
	return nil
}

// TxStatus is where c's transaction is: 'I' out of one, 'T' in one, 'E'
// in one that failed (PG's alone), as c tells; taken as out of one when
// it can't.
func TxStatus(c Conn) byte {
	if c, ok := c.(interface{ TxStatus() byte }); ok {
		return c.TxStatus()
	}
	return 'I'
}

// RolledBack is an error after which the server took the whole
// transaction back, MySQL's deadlock: no longer in one (§8.2).
type RolledBack struct{ Err error }

func (e RolledBack) Error() string { return e.Err.Error() }
func (e RolledBack) Unwrap() error { return e.Err }

// Driver is what every kind of server does its own way under a Worker
// (§5「连接缝」): connecting, whether an error is a lost connection, and what
// an error says. A key-value server is a Driver and no Engine.
type Driver interface {
	// Connect opens one connection; readOnly makes its transactions read
	// only, as Meta's are (§8.2).
	Connect(ctx context.Context, dsn, password string, readOnly bool) (Conn, Info, error)
	// Broken says whether err is a lost connection (§8.2「断线重连」).
	Broken(err error) Lost
	ErrorOf(err error) ServerError
}

// Engine is what PG, MySQL and SQLite each do their own way above a Conn
// (§8.1): writing names and arguments into SQL, the catalog (§8.4), quick SQL
// (§12) and a query's schema (§8.6), as well as what a Driver does.
type Engine interface {
	Driver
	Dialect() sqlkit.Dialect
	Quote(names ...string) string // a name, or schema and name, quoted
	Arg(n int) string             // the nth argument, from 1
	// Same is the condition that column col still holds what was read,
	// arg its text (§10.3); Eq that a column of a row's identity holds arg,
	// the key as it was read (§10.1).
	Same(col, arg string) string
	Eq(col, arg string) string
	// Schemas lists the user's schemas (MySQL's databases), and the one
	// unqualified names go to first, "" for none.
	Schemas(ctx context.Context, c Conn) (schemas []string, current string, err error)
	// Databases lists the databases the server has that the user can
	// connect to; nil for an engine whose databases are its schemas or that
	// has none (§5, §8.4).
	Databases(ctx context.Context, c Conn) ([]string, error)
	// Tables lists every schema's tables and views by schema and name; only
	// names the schemas it asks of, nil for all (F17.13), as it does for
	// Functions and Sequences.
	Tables(ctx context.Context, c Conn, only []string) ([]Table, error)
	// TableColumns reads a table's columns, its primary key and the unique
	// indexes that can stand in for it (§10.1).
	TableColumns(ctx context.Context, c Conn, schema, table string) (Columns, error)
	// DDL is a table's or a view's definition (§12「预览」).
	DDL(ctx context.Context, c Conn, schema, name string) (string, error)
	// Indexes and Keys are a table's, Functions and Sequences every
	// schema's, FunctionDef a function's definition as a statement that
	// makes it; nil for what the engine has none of (§8.4, F14.3).
	Indexes(ctx context.Context, c Conn, schema, table string) ([]Index, error)
	Keys(ctx context.Context, c Conn, schema, table string) ([]Key, error)
	Functions(ctx context.Context, c Conn, only []string) ([]Function, error)
	Sequences(ctx context.Context, c Conn, only []string) ([]Sequence, error)
	FunctionDef(ctx context.Context, c Conn, f Function) (string, error)
	// Quick runs one statement that reads, in a read-only transaction on
	// w, schema first, keeping maxRows rows (§12).
	Quick(ctx context.Context, w *Worker, sql, schema string, maxRows int) (Result, error)
	// UseSchema makes schema the first unqualified names go to, then
	// started, the one c connected with (§8.6).
	UseSchema(ctx context.Context, c Conn, schema, started string) error
}

// Info is where a connection landed: user@host:port for the status bar,
// the database for query titles, the schema path it started with
// (§7.8, §8.6).
type Info struct {
	Addr, Database, Path string
}

// ServerError is err as the result area's log and an error bar show it
// (§11, §7.8「错误栏」): a server's Severity, SQLSTATE and Message without
// the driver's dressing, its DETAIL and HINT a line each (DETAIL's own
// lines apart), and Position, the statement's character it points at from
// 1, 0 for none. An error no server sent is ERROR and its text alone.
type ServerError struct {
	Severity, Code, Message string
	More                    []string
	Position                int
}

// Scope is the condition on column col, a schema's name, of a catalog query: the
// schemas picked, only, the server's own among them when picked (F17.13); with
// none picked, user, the engine's condition that keeps its own schemas out.
func Scope(user, col string, only []string, d sqlkit.Dialect) string {
	if only == nil {
		return user
	}
	return "1 = 1" + SchemaIn(col, only, d)
}

// Systemer is an Engine whose server has schemas of its own, which the tree
// leaves out till they are picked (F17.13): their names.
type Systemer interface {
	SystemSchemas(ctx context.Context, c Conn) ([]string, error)
}

// SchemaIn is the condition that column col, a schema's name, is one of only, written
// to follow a where's others: the catalog's queries ask for the schemas picked
// (F17.13); "" for nil, all of them.
func SchemaIn(col string, only []string, d sqlkit.Dialect) string {
	if only == nil {
		return ""
	}
	if len(only) == 0 { // none picked: nothing (1 = 0: Oracle has no false)
		return " and 1 = 0"
	}
	lits := make([]string, len(only))
	for i, n := range only {
		lits[i] = sqlkit.Literal(n, d)
	}
	return " and " + col + " in (" + strings.Join(lits, ", ") + ")"
}
