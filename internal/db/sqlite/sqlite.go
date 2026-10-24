// Package sqlite is the SQLite engine (§8.1, F14.1): a file, one connection
// of modernc.org/sqlite, which is Go all through (CGO_ENABLED=0).
// Reference: lazysql drivers/sqlite.go, which reads the columns and the
// primary key with pragma table_info; SQLite's own complete.c, which the
// statements of a trigger follow (sqlkit).
package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Conn is one connection to the file. The file is its own server: nothing
// to kill (a cancelled ctx interrupts the statement and the connection
// stays), and nothing to lose.
type Conn struct {
	pool *sql.DB
	c    *sql.Conn
	tx   byte // 'I' or 'T', as the statements run tell (txAfter)
}

// Path is the file spec names: ~ at its start is the home directory, a
// relative one is relative to where sqlmux started; ?mode=ro and the like are
// the driver's, which Config passes on. Both separators are taken on windows.
func Path(spec string) (path, query string, err error) {
	path, query, _ = strings.Cut(spec, "?")
	if rest, ok := strings.CutPrefix(path, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == '\\' && runtime.GOOS == "windows") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		path = home + rest
	}
	path, err = filepath.Abs(path)
	return path, query, err
}

// slashPath is the windows drive path in the shape a file: URL takes: C:\x.db
// is the path /C:/x.db there. Unix paths already are that shape.
func slashPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	return "/" + strings.TrimPrefix(filepath.ToSlash(p), "/")
}

// DSN is spec as the driver takes it: the file with its parameters, a busy
// timeout, as Main writes while Meta reads the same file, unless a _pragma
// sets one, and mode=rw unless another mode is written.
func DSN(spec string) (path, dsn string, err error) {
	path, query, err := Path(spec)
	if err != nil {
		return "", "", err
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return "", "", err
	}
	if !slices.ContainsFunc(q["_pragma"], func(p string) bool { return strings.HasPrefix(strings.ToLower(p), "busy_timeout") }) {
		q.Add("_pragma", "busy_timeout(5000)")
	}
	if !q.Has("mode") { // a file there is not is no new database: a path mistyped; mode=rwc says it
		q.Set("mode", "rw")
	}
	u := url.URL{Scheme: "file", Path: slashPath(path), RawQuery: q.Encode()}
	return path, u.String(), nil
}

// Connect opens the file spec names, which has to be there unless the mode is
// rwc: SQLite would make an empty one. readOnly
// makes every transaction read only, as the Meta connection is (§8.2). The
// password is of no use here.
func Connect(ctx context.Context, spec, _ string, readOnly bool) (*Conn, db.Info, error) {
	path, dsn, err := DSN(spec)
	if err != nil {
		return nil, db.Info{}, err
	}
	u, _ := url.Parse(dsn) // DSN built it
	if _, err := os.Stat(path); err != nil && u.Query().Get("mode") != "rwc" {
		return nil, db.Info{}, err
	}
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, db.Info{}, err
	}
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(ctx)
	if err != nil {
		pool.Close()
		return nil, db.Info{}, err
	}
	c := &Conn{pool: pool, c: conn, tx: 'I'}
	// SQLite takes a transaction back by itself, on an error of ON CONFLICT
	// ROLLBACK, a RAISE(ROLLBACK) in a trigger, a statement interrupted: the
	// statement fails, and txAfter does not run
	err = conn.Raw(func(dc any) error {
		if hr, ok := dc.(sqlite.HookRegisterer); ok {
			hr.RegisterRollbackHook(func() { c.tx = 'I' })
		}
		return nil
	})
	if err == nil {
		_, err = c.Query(ctx, "select 1") // the file is opened and read here: one that is no database fails now
	}
	if err == nil && readOnly {
		_, err = c.Query(ctx, "pragma query_only = on")
	}
	if err != nil {
		c.Close()
		return nil, db.Info{}, err
	}
	return c, db.Info{Addr: path, Path: "main"}, nil // no database before main on a query's title, as MySQL has none
}

func (c *Conn) Close() error {
	c.c.Close()
	return c.pool.Close()
}

// TxStatus is 'T' after a BEGIN and 'I' after a COMMIT, END or ROLLBACK: the
// driver tells no one.
// ponytail: a SAVEPOINT outside a transaction, which begins one, goes unseen
func (c *Conn) TxStatus() byte { return c.tx }

// Exec runs sql, one statement.
func (c *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	r, err := c.run(ctx, sql, nil, maxRows)
	if err != nil {
		return nil, err
	}
	return []db.Result{r}, nil
}

func (c *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	return c.run(ctx, sql, db.SQLArgs(args), 0)
}

// run runs query, keeping at most maxRows rows (0: all), the rest read and
// dropped. Every statement goes through Query, whether it answers with rows
// or not (a RETURNING one does): one that writes and has no columns has
// changes() asked afterwards.
func (c *Conn) run(ctx context.Context, query string, args []any, maxRows int) (r db.Result, err error) {
	start := time.Now()
	defer func() {
		if err == nil {
			c.tx = txAfter(query, c.tx)
		}
	}()
	rows, err := c.c.QueryContext(ctx, query, args...)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	err = db.EachRow(rows, cellOf(shown), func(cols []db.Col) error {
		if len(cols) > 0 { // none for a statement that answers with none
			r.Cols = cols
		}
		return nil
	}, db.Capped(&r, maxRows), func(vals []db.Val) error { r.Rows = append(r.Rows, vals); return nil })
	if err != nil {
		return r, err
	}
	rows.Close()
	word := sqlkit.FirstWord(query, sqlkit.SQLite)
	r.Tag = strings.ToUpper(word)
	switch {
	case len(r.Cols) > 0:
		r.Tag += fmt.Sprintf(" %d", len(r.Rows))
	case writes(word, query):
		if r.Affected, err = c.changes(ctx); err == nil {
			r.Tag += fmt.Sprintf(" %d", r.Affected)
		}
	}
	r.Took = time.Since(start)
	return r, err
}

// writes is whether statement query, which starts with word, is one that hits
// rows: an INSERT, UPDATE, DELETE or REPLACE, a WITH that has one in it.
func writes(word, query string) bool {
	switch word {
	case "insert", "update", "delete", "replace":
		return true
	case "with":
		return !sqlkit.IsRead(query, sqlkit.SQLite)
	}
	return false
}

// changes is the rows the last INSERT, UPDATE or DELETE hit, as found,
// whether or not they changed.
func (c *Conn) changes(ctx context.Context) (n int64, err error) {
	err = c.c.QueryRowContext(ctx, "select changes()").Scan(&n)
	return n, err
}

// txAfter is where a transaction is after statement s ran in it, tx before.
func txAfter(s string, tx byte) byte {
	switch {
	case sqlkit.FirstWord(s, sqlkit.SQLite) == "begin":
		return 'T'
	case sqlkit.EndsTx(s, sqlkit.SQLite) != "":
		return 'I'
	}
	return tx
}

// Stream reads the rows of a statement as they come, one at a time (F12.4),
// a stop of either callback leaving the rest unread.
func (c *Conn) Stream(ctx context.Context, query string, head func([]db.Col) error, row func([]db.Val) error) error {
	rows, err := c.c.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	return db.EachRow(rows, cellOf(hexed), head, func() bool { return true }, row)
}

// cellOf is the text of the value the driver read, in a form: an integer, a
// REAL (realText), a time as SQLite writes one, a text or a blob as text tells,
// told by whether the driver read it as TEXT or as a BLOB.
func cellOf(text func(v []byte, blob bool) string) func(v any, typ string) db.Val {
	return func(v any, _ string) db.Val {
		switch v := v.(type) {
		case nil:
			return db.Val{Null: true}
		case int64:
			return db.Val{S: strconv.FormatInt(v, 10)}
		case float64:
			return db.Val{S: realText(v)}
		case string:
			return db.Val{S: text([]byte(v), false)}
		case []byte:
			return db.Val{S: text(v, true)}
		case time.Time:
			return db.Val{S: timeText(v)}
		}
		return db.Val{S: fmt.Sprint(v)} // none the driver gives
	}
}

// timeText is a time the driver parsed, of a DATE, DATETIME or TIMESTAMP column,
// as SQLite's own text has it: YYYY-MM-DD HH:MM:SS, and .SSS when there is a
// fraction, in UTC, no zone.
// ponytail: the text it was parsed from is not kept: a date alone, a T between,
// a .000 come back as the standard; modernc has no switch for it (§8.1)
func timeText(t time.Time) string {
	layout := "2006-01-02 15:04:05"
	if t.Nanosecond() != 0 {
		layout += ".000"
	}
	return t.UTC().Format(layout)
}

// realText is f as SQLite writes a REAL as text: the digits that read back as
// f, a point always (3.0), an exponent from 1e17 and under 1e-4 (1.0e+22).
func realText(f float64) string {
	switch {
	case f == 0:
		return "0.0"
	case math.IsInf(f, 0):
		return strings.Replace(strconv.FormatFloat(f, 'g', -1, 64), "+", "", 1)
	}
	form := byte('f')
	if a := math.Abs(f); a < 1e-4 || a >= 1e17 {
		form = 'e'
	}
	s := strconv.FormatFloat(f, form, -1, 64)
	mantissa, exp, _ := strings.Cut(s, "e")
	if !strings.Contains(mantissa, ".") {
		mantissa += ".0"
	}
	if exp != "" {
		exp = "e" + exp
	}
	return mantissa + exp
}

// shown is a value as the grid shows it: a blob, or a text that is no UTF-8, by
// its size (§8.1).
func shown(v []byte, blob bool) string {
	if blob || !utf8.Valid(v) {
		return db.BinaryShown(len(v))
	}
	return string(v)
}

// hexed is a value as a file has it: a blob, or a text that is no UTF-8, as the
// blob literal X'…', not the grid's placeholder, which is no data (F12.4).
func hexed(v []byte, blob bool) string {
	if blob || !utf8.Valid(v) {
		return "X'" + strings.ToUpper(hex.EncodeToString(v)) + "'"
	}
	return string(v)
}

// Engine is SQLite's way with the catalog, SQL and errors (§8.1).
type Engine struct{}

func (Engine) Connect(ctx context.Context, dsn, password string, readOnly bool) (db.Conn, db.Info, error) {
	c, info, err := Connect(ctx, dsn, password, readOnly)
	if err != nil {
		return nil, info, err
	}
	return c, info, nil
}

func (Engine) Dialect() sqlkit.Dialect { return sqlkit.SQLite }
func (Engine) Arg(n int) string        { return "?" + strconv.Itoa(n) } // numbered, so a statement can use one twice (Same, Eq)

func (Engine) Quote(names ...string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + strings.ReplaceAll(n, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ".")
}

// Same is whether the column still holds what was read, arg its text, and is
// ?N: as text, which holds for an integer, a text, a blob's; a REAL by number,
// which the text it was shown as, the shortest that reads back, is not always
// the digits SQLite casts it to; a DATE, DATETIME or TIMESTAMP, which the
// driver reads as a time and so shows as the standard text, by julianday, which
// is NULL, never equal, for the text of anything else.
func (Engine) Same(col, arg string) string {
	return "(cast(" + col + " as text) = " + arg + " or typeof(" + col + ") = 'real' and " + col + " = cast(" + arg + " as real) or julianday(" + col + ") = julianday(" + arg + "))"
}

// Eq is whether the column of a row's identity holds arg, ?N, as the key it
// was read as: = is no match of an integer to the text '1' where the column has
// no declared type, so the integer the text is is one the column may hold, if
// the text is the integer's own, not 'abc', which casts to 0 (F14.1). The
// second term is one expression of arg alone, NULL for any other text, so each
// term searches an index; an AND on the column would make a scan.
func (Engine) Eq(col, arg string) string {
	return "(" + col + " = " + arg + " or " + col + " = iif(cast(cast(" + arg + " as integer) as text) = " + arg + ", cast(" + arg + " as integer), null))"
}

// UseSchema does nothing: the file is the one schema, main.
func (Engine) UseSchema(context.Context, db.Conn, string, string) error { return nil }

// Broken is none: a file is no connection to lose.
func (Engine) Broken(error) db.Lost { return db.NotLost }

// ErrorOf is err as the log and an error bar show it (§11): the library's
// message.
func (Engine) ErrorOf(err error) db.ServerError {
	e := db.ServerError{Severity: "ERROR", Message: err.Error()}
	var se *sqlite.Error
	if errors.As(err, &se) {
		e.Code = strconv.Itoa(se.Code())
	}
	return e
}
