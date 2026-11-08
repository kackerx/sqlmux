// Package oracle is db.Conn over go-ora (tech-design §8.11), pure Go with no
// Instant Client: one connection held out of a pool of one, every value as
// text. Oracle has no BEGIN: a transaction is the driver's, begun when a
// statement asks for one (SET TRANSACTION, db.Save's Begin) and ended by a
// COMMIT, a ROLLBACK or a DDL, which commits on its own.
package oracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	goora "github.com/sijms/go-ora/v2"
	"github.com/sijms/go-ora/v2/network"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// The session's formats, which the text of a date or a timestamp is read and
// written in (§8.1): what the grid shows, what a typed argument parses
// (Typed), what an implicit conversion in a WHERE takes.
const (
	dateFormat = "YYYY-MM-DD HH24:MI:SS"
	tsFormat   = "YYYY-MM-DD HH24:MI:SS.FF"
	tzFormat   = "YYYY-MM-DD HH24:MI:SS.FFTZH:TZM"
)

var session = []string{
	"alter session set nls_date_format = '" + dateFormat + "'",
	"alter session set nls_timestamp_format = '" + tsFormat + "'",
	"alter session set nls_timestamp_tz_format = '" + tzFormat + "'",
	"alter session set nls_numeric_characters = '.,'",
}

type Conn struct {
	pool *sql.DB
	c    *sql.Conn
	tx   *sql.Tx // the transaction going on, nil out of one
	dead bool    // a request found the connection lost: the Worker, told so by Closed, dials before the next
}

// dialer is the Worker's DialFunc as go-ora's dialer (F15.2).
type dialer db.DialFunc

func (d dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

// DSN is dsn with password in it when that is not "": the oracle:// URL
// go-ora takes, oracle://user:pass@host:1521/service?param=value.
func DSN(dsn, password string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if u.Scheme != "oracle" {
		return "", fmt.Errorf("oracle: the dsn is oracle://user:pass@host:1521/service, not %s://", u.Scheme)
	}
	if password != "" {
		u.User = url.UserPassword(u.User.Username(), password)
	}
	return u.String(), nil
}

// Connect opens one connection, its session's formats set, and says where it
// is: user@host:port, the container (PDB) it landed in (§7.8). readOnly sets
// nothing on the server: Oracle has no read-only session; a read-only
// connection is the app's interception (§13), quick SQL's a read-only
// transaction (Quick).
func Connect(ctx context.Context, dsn, password string, _ bool) (*Conn, db.Info, error) {
	full, err := DSN(dsn, password)
	if err != nil {
		return nil, db.Info{}, err
	}
	connector := goora.NewConnector(full).(*goora.OracleConnector)
	if d, ok := db.DialerOf(ctx); ok { // through ssh (F15.2)
		connector.Dialer(dialer(d.Dial))
	}
	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(1)
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := pool.Conn(dialCtx)
	if err != nil {
		pool.Close()
		return nil, db.Info{}, err
	}
	c := &Conn{pool: pool, c: conn}
	for _, s := range session {
		if _, err := conn.ExecContext(dialCtx, s); err != nil {
			c.Close()
			return nil, db.Info{}, err
		}
	}
	r, err := c.Query(dialCtx, "select sys_context('USERENV', 'CON_NAME'), user from dual")
	if err != nil {
		c.Close()
		return nil, db.Info{}, err
	}
	u, _ := url.Parse(dsn)
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "1521")
	}
	return c, db.Info{Addr: strings.ToLower(r.Rows[0][1].S) + "@" + host, Database: r.Rows[0][0].S}, nil
}

func (c *Conn) Close() error {
	if c.tx != nil {
		_ = c.tx.Rollback()
	}
	c.c.Close()
	return c.pool.Close()
}

// Closed is whether a request found the connection lost (Broken).
func (c *Conn) Closed() bool { return c.dead }

// TxStatus is 'T' while a transaction of the driver's is going on.
func (c *Conn) TxStatus() byte {
	if c.tx != nil {
		return 'T'
	}
	return 'I'
}

// Begin is the statement that starts a transaction: Oracle's own SET
// TRANSACTION, which the connection takes as the driver's begin (§10.3).
func (*Conn) Begin() string { return "set transaction read write" }

// Savepoint is name's: SAVEPOINT and ROLLBACK TO SAVEPOINT, and no release —
// a savepoint stands till the transaction ends.
func (*Conn) Savepoint(name string) (set, rollback, release string) {
	return "savepoint " + name, "rollback to savepoint " + name, ""
}

// querier is the transaction going on, or the connection.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (c *Conn) on() querier {
	if c.tx != nil {
		return c.tx
	}
	return c.c
}

// Exec runs the statements of sql one after the other, the driver taking one
// at a time, a PL/SQL block as one (§9.2); it stops at the first that fails.
func (c *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	var out []db.Result
	for _, st := range sqlkit.Statements(sql, sqlkit.Oracle) {
		r, err := c.run(ctx, sql[st.Start:st.End], nil, maxRows)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Query runs one statement, args as text: :1, :2 as Arg writes them.
func (c *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	return c.run(ctx, sql, db.SQLArgs(args), 0)
}

// rowWords start the statements that answer with rows.
var rowWords = map[string]bool{"select": true, "with": true}

// ddlWords start the statements that commit the transaction they run in, as
// Oracle's DDL does: what was done in it is committed with them.
var ddlWords = map[string]bool{"create": true, "alter": true, "drop": true, "truncate": true, "grant": true, "revoke": true,
	"rename": true, "comment": true, "analyze": true, "audit": true, "noaudit": true, "purge": true, "flashback": true}

// run runs one statement, keeping maxRows rows of its answer (0: all). The
// transaction's own words go to the driver's transaction: SET TRANSACTION
// begins one, COMMIT and ROLLBACK end it; a DDL in one ends it as the server
// does (§8.11).
func (c *Conn) run(ctx context.Context, query string, args []any, maxRows int) (r db.Result, err error) {
	if err := ctx.Err(); err != nil {
		return r, err
	}
	start := time.Now()
	word := sqlkit.FirstWord(query, sqlkit.Oracle)
	defer func() {
		if err != nil {
			err = c.noted(err)
		}
		r.Took = time.Since(start)
	}()
	switch end := sqlkit.EndsTx(query, sqlkit.Oracle); {
	case end != "" && c.tx != nil:
		tx := c.tx
		c.tx = nil
		if end == "commit" {
			return db.Result{Tag: "COMMIT"}, tx.Commit()
		}
		return db.Result{Tag: "ROLLBACK"}, tx.Rollback()
	case word == "set" && c.tx == nil && strings.HasPrefix(strings.ToLower(strings.Join(strings.Fields(query), " ")), "set transaction"):
		// database/sql rolls a transaction back as the ctx it began with ends, and the
		// Worker ends each request's: one of the manual mode outlives its request
		if c.tx, err = c.c.BeginTx(context.WithoutCancel(ctx), nil); err != nil {
			c.tx = nil
			return r, err
		}
	}
	if !rowWords[word] || !sqlkit.IsRead(query, sqlkit.Oracle) {
		res, err := c.on().ExecContext(ctx, query, args...)
		if err != nil {
			return r, err
		}
		r.Affected, _ = res.RowsAffected()
		r.Tag = strings.ToUpper(word)
		switch word {
		case "insert", "update", "delete", "merge":
			r.Tag += " " + strconv.FormatInt(r.Affected, 10)
		}
		if ddlWords[word] && c.tx != nil { // committed with it, on the server
			tx := c.tx
			c.tx = nil
			_ = tx.Commit()
		}
		return r, nil
	}
	rows, err := c.on().QueryContext(ctx, query, args...)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	err = db.EachRow(rows, cellOf, func(cols []db.Col) error { r.Cols = typed(cols); return nil },
		db.Capped(&r, maxRows), func(v []db.Val) error { r.Rows = append(r.Rows, v); return nil })
	r.Tag = fmt.Sprintf("%s %d", strings.ToUpper(word), len(r.Rows))
	return r, err
}

// noted is what an error does to c: a lost connection takes the transaction
// with it, and the Worker dials before the next request.
func (c *Conn) noted(err error) error {
	if (Engine{}).Broken(err) != db.NotLost {
		c.tx, c.dead = nil, true
	}
	return err
}

// Stream reads the rows of a statement as they come (F12.4).
func (c *Conn) Stream(ctx context.Context, query string, head func([]db.Col) error, row func([]db.Val) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rows, err := c.on().QueryContext(ctx, query)
	if err != nil {
		return c.noted(err)
	}
	defer rows.Close()
	err = db.EachRow(rows, cellOf, func(cols []db.Col) error { return head(typed(cols)) }, func() bool { return true }, row)
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	if err != nil {
		return c.noted(err)
	}
	return nil
}

// typeNames are go-ora's names of a result's column types, as Oracle writes
// them: what the grid classes a column by (§7.6).
var typeNames = map[string]string{
	"ibdouble": "BINARY_DOUBLE", "ibfloat": "BINARY_FLOAT", "longvarchar": "CLOB", "nchar": "VARCHAR2", "char": "CHAR",
	"timestampdty": "TIMESTAMP", "timestamptz_dty": "TIMESTAMP WITH TIME ZONE", "timestampltz_dty": "TIMESTAMP WITH LOCAL TIME ZONE",
	"intervalds_dty": "INTERVAL DAY TO SECOND", "intervalym_dty": "INTERVAL YEAR TO MONTH", "ocibloblocator": "BLOB", "ociclobblocator": "CLOB",
}

func typed(cols []db.Col) []db.Col {
	for i, c := range cols {
		if t, ok := typeNames[c.Type]; ok {
			cols[i].Type = t
		} else {
			cols[i].Type = strings.ToUpper(c.Type)
		}
	}
	return cols
}

// cellOf is a value as the grid shows it (§8.1): NUMBER as the driver's exact
// decimal text, a binary float as the shortest text that reads back, a date
// and a timestamp in the session's formats (the fraction's trailing zeros
// left out, a zone as +02:00), RAW in hex as Oracle writes it, a BLOB by its
// size.
func cellOf(v any, col *sql.ColumnType) db.Val {
	typ := strings.ToLower(col.DatabaseTypeName())
	switch x := v.(type) {
	case nil:
		return db.Val{Null: true}
	case string:
		return db.Val{S: x}
	case float64:
		if typ == "ibfloat" {
			return db.Val{S: strconv.FormatFloat(float64(float32(x)), 'g', -1, 32)}
		}
		return db.Val{S: strconv.FormatFloat(x, 'g', -1, 64)}
	case float32:
		return db.Val{S: strconv.FormatFloat(float64(x), 'g', -1, 32)}
	case int64:
		return db.Val{S: strconv.FormatInt(x, 10)}
	case time.Time:
		return db.Val{S: timeText(x, typ)}
	case []byte:
		if typ == "raw" {
			return db.Val{S: strings.ToUpper(hex.EncodeToString(x))}
		}
		if typ != "ocibloblocator" && typ != "blob" && utf8.Valid(x) {
			return db.Val{S: string(x)}
		}
		return db.Val{S: db.BinaryShown(len(x))}
	}
	return db.Val{S: fmt.Sprint(v)}
}

// timeText is t as the session's formats write it: a DATE to the second, a
// TIMESTAMP with its fraction, its trailing zeros out, one WITH TIME ZONE its
// offset after it, as the time picker reads it (§10.2).
func timeText(t time.Time, typ string) string {
	if typ == "date" {
		return t.Format("2006-01-02 15:04:05")
	}
	s := t.Format("2006-01-02 15:04:05.999999999")
	if strings.Contains(typ, "tz") && !strings.Contains(typ, "ltz") {
		s += t.Format("-07:00")
	}
	return s
}

// Engine is Oracle's way with the catalog, SQL and errors (§8.11).
type Engine struct{}

func (Engine) Connect(ctx context.Context, dsn, password string, readOnly bool) (db.Conn, db.Info, error) {
	c, info, err := Connect(ctx, dsn, password, readOnly)
	if err != nil {
		return nil, db.Info{}, err
	}
	return c, info, nil
}

// brokenCodes are the ORA- numbers of a session that is gone: end of file on
// the channel, not connected, lost contact, killed, not logged on.
var brokenCodes = map[int]bool{3113: true, 3114: true, 3135: true, 28: true, 1012: true, 12537: true, 12547: true}

// Broken says whether err is a lost connection (§8.2「断线重连」).
func (Engine) Broken(err error) db.Lost {
	var oe *network.OracleError
	switch {
	case errors.Is(err, driver.ErrBadConn):
		return db.LostUnsent
	case db.NetworkError(err):
		return db.LostSent
	case errors.As(err, &oe) && brokenCodes[oe.ErrCode]:
		return db.LostSent
	}
	return db.NotLost
}

// ErrorOf is err as the log and an error bar show it (§11): ORA-00904 the
// code, the message without it and without the driver's position text, the
// position as the statement's character it points at.
func (Engine) ErrorOf(err error) db.ServerError {
	var oe *network.OracleError
	if !errors.As(err, &oe) {
		return db.ServerError{Severity: "ERROR", Message: err.Error()}
	}
	code := fmt.Sprintf("ORA-%05d", oe.ErrCode)
	msg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(oe.ErrMsg), code+":"))
	e := db.ServerError{Severity: "ERROR", Code: code, Message: msg}
	if p := oe.ErrPos(); p > 0 {
		e.Position = p + 1
	}
	return e
}

func (Engine) Dialect() sqlkit.Dialect { return sqlkit.Oracle }

func (Engine) Quote(names ...string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `"` + strings.ReplaceAll(n, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ".")
}

func (Engine) Arg(n int) string { return ":" + strconv.Itoa(n) }

// Typed is arg as a value of the column's type (§10.3): a number's, a date's
// and a timestamp's text parsed in the session's formats, RAW from its hex, a
// CLOB as one; Oracle would convert most of these by itself, but this way an
// index on the column is used and nothing hangs on the session.
func (Engine) Typed(arg, typ string) string {
	t := strings.ToUpper(typ)
	switch {
	case strings.HasPrefix(t, "NUMBER"), strings.HasPrefix(t, "FLOAT"), t == "INTEGER":
		return "TO_NUMBER(" + arg + ")"
	case t == "BINARY_DOUBLE":
		return "TO_BINARY_DOUBLE(" + arg + ")"
	case t == "BINARY_FLOAT":
		return "TO_BINARY_FLOAT(" + arg + ")"
	case t == "DATE":
		return "TO_DATE(" + arg + ", '" + dateFormat + "')"
	case strings.HasPrefix(t, "TIMESTAMP") && strings.HasSuffix(t, "WITH TIME ZONE"):
		return "TO_TIMESTAMP_TZ(" + arg + ", '" + tzFormat + "')"
	case strings.HasPrefix(t, "TIMESTAMP"):
		return "TO_TIMESTAMP(" + arg + ", '" + tsFormat + "')"
	case strings.HasPrefix(t, "RAW"):
		return "HEXTORAW(" + arg + ")"
	case t == "CLOB" || t == "NCLOB":
		return "TO_CLOB(" + arg + ")"
	}
	return arg
}

func (Engine) Eq(col, arg string) string { return col + " = " + arg }

// Same is the column holding what was read, typed as Typed has it: a CLOB is
// compared by DBMS_LOB, = takes none.
func (Engine) Same(col, arg string) string {
	if strings.HasPrefix(arg, "TO_CLOB(") {
		return "dbms_lob.compare(" + col + ", " + arg + ") = 0"
	}
	return col + " = " + arg
}

// UseSchema makes schema the one unqualified names go to (§8.6).
func (e Engine) UseSchema(ctx context.Context, c db.Conn, schema, _ string) error {
	_, err := c.Query(ctx, "alter session set current_schema = "+e.Quote(schema))
	return err
}
