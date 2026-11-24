package duckdb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

var errClosed = errors.New("duckdb: connection closed")

// duckdb_type and duckdb_statement_type of duckdb.h, the members that are used.
const (
	typeBlob       = 18
	stmtSelect     = 1
	stmtInsert     = 2
	stmtUpdate     = 3
	stmtDelete     = 5
	stmtCopy       = 11
	stmtMergeInto  = 30
	cancelInterval = 300 * time.Millisecond // an interrupt before the statement has started is lost: sent again
)

// readable are the column types duckdb_value_varchar gives the text of
// (§8.1): the others it gives "" for, and after the first of them all the
// row's cells go empty. bool, the integers, the floats, decimal, date, time,
// timestamp, interval, varchar and blob, and huge integers.
func readable(typ int32) bool { return typ >= 1 && typ <= 19 || typ == 32 }

// Conn is one connection to a database the process has open. The file is its
// own server: a cancel interrupts the statement and the connection stays,
// and nothing is lost.
type Conn struct {
	inst     *instance
	h        uintptr
	readOnly bool
	tx       byte // 'I', 'T', or 'E' for a transaction a statement failed in: any error does that (§8.1)
	closed   bool
}

// Connect opens the database spec names (§8.1「DSN」): a connection to the
// one instance of it the process has. readOnly makes every transaction of the
// connection read only, as the Meta connection's are (§8.2). The password is
// of no use here.
func Connect(ctx context.Context, spec, _ string, readOnly bool) (*Conn, db.Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, db.Info{}, err
	}
	if err := Load(); err != nil {
		return nil, db.Info{}, err
	}
	t, err := parse(spec)
	if err != nil {
		return nil, db.Info{}, err
	}
	inst, err := acquire(t)
	if err != nil {
		return nil, db.Info{}, err
	}
	conn := &Conn{inst: inst, readOnly: readOnly, tx: 'I'}
	if c.connect(inst.h, &conn.h) != 0 {
		inst.release()
		return nil, db.Info{}, errors.New("duckdb_connect failed")
	}
	return conn, db.Info{Addr: t.path, Path: "main"}, nil // no database before main on a query's title, as SQLite's
}

func (conn *Conn) Close() error {
	if !conn.closed {
		conn.closed = true
		c.disconnect(&conn.h)
		conn.inst.release()
	}
	return nil
}

// TxStatus is where the transaction is: 'E' once a statement of it failed.
func (conn *Conn) TxStatus() byte { return conn.tx }

// Exec runs sql, one statement.
func (conn *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	r, err := conn.run(ctx, sql, nil, maxRows, false)
	if err != nil {
		return nil, err
	}
	return []db.Result{r}, nil
}

func (conn *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	return conn.run(ctx, sql, args, 0, false)
}

// Stream hands the rows of a statement over (F12.4). They are all read first:
// the library has the whole result in memory, and so does this.
// shortcut: a big table's export is held whole; read the chunks of the result
// (duckdb_fetch_chunk) when someone exports one that does not fit.
func (conn *Conn) Stream(ctx context.Context, sql string, head func([]db.Col) error, row func([]db.Val) error) error {
	r, err := conn.run(ctx, sql, nil, 0, true)
	if err != nil {
		return err
	}
	if err := head(r.Cols); err != nil {
		return err
	}
	for _, vals := range r.Rows {
		if err := row(vals); err != nil {
			return err
		}
	}
	return nil
}

// run runs sql, one statement, keeping at most maxRows rows (0: all), the
// rest dropped; the bytes of a BLOB are the literal that makes them for a
// file (export), or the grid's size of them.
func (conn *Conn) run(ctx context.Context, sql string, args []db.Val, maxRows int, export bool) (r db.Result, err error) {
	if conn.closed {
		return r, errClosed
	}
	start := time.Now()
	word := sqlkit.FirstWord(sql, sqlkit.DuckDB)
	if conn.readOnly && conn.tx == 'I' {
		// a statement is a read only transaction of its own, a BEGIN a read only one that lasts
		if word == "begin" || word == "start" {
			sql = "begin transaction read only"
		} else {
			if err := conn.simple("begin transaction read only"); err != nil {
				return r, err
			}
			defer func() { _ = conn.simple("rollback") }() // an error aborts the transaction as well: it goes
		}
	}
	defer func() { conn.tx = txAfter(conn.tx, word, sql, err) }()
	st, err := conn.prepare(sql)
	if err != nil {
		return r, err
	}
	typ := c.stmtType(st)
	wrap := typ == stmtSelect && word != "pragma" // pragma's rows are no subquery
	var cols []db.Col
	if wrap {
		c.destroyPrep(&st)
		// shortcut: no type for the columns of a statement with arguments, describe takes none
		if len(args) == 0 && !slices.Contains([]string{"describe", "show", "summarize"}, word) {
			if cols, err = conn.describe(ctx, sql); err != nil {
				return r, err
			}
		}
		if st, err = conn.prepare(wrapSQL(sql, maxRows)); err != nil {
			return r, err
		}
	}
	defer c.destroyPrep(&st)
	res, err := conn.execute(ctx, st, args)
	if err != nil {
		return r, err
	}
	defer c.destroy(res)
	conn.read(res, &r, cols, wrap, typ, export)
	if maxRows > 0 && len(r.Rows) > maxRows {
		r.Rows, r.Truncated = r.Rows[:maxRows], true
	}
	if word == "" {
		word = "select"
	}
	r.Tag = strings.ToUpper(word)
	switch {
	case len(r.Cols) > 0:
		r.Tag += " " + strconv.Itoa(len(r.Rows))
	case isWrite(typ):
		r.Tag += " " + strconv.FormatInt(r.Affected, 10)
	}
	r.Took = time.Since(start)
	return r, nil
}

// isWrite is a statement that hits rows, which says how many.
func isWrite(typ int32) bool {
	return typ == stmtInsert || typ == stmtUpdate || typ == stmtDelete || typ == stmtCopy || typ == stmtMergeInto
}

// wrapSQL is a SELECT's statement made one that has every value as the
// library's own text, which is what the CLI shows too (§8.1), and keeps
// maxRows+1 rows, to tell a result that is longer. The newline ends a -- comment
// the statement ends in.
func wrapSQL(sql string, maxRows int) string {
	s := "select cast(columns(*) as varchar) from (\n" + strings.TrimRight(sql, "; \t\r\n") + "\n)"
	if maxRows > 0 {
		s += " limit " + strconv.Itoa(maxRows+1)
	}
	return s
}

// txAfter is where the transaction is after statement sql, whose first word is
// word, ran in it, tx before, err the error it ended with. Any error fails the
// transaction it was in, a cancel too (§8.1, tried on 2.0.1).
func txAfter(tx byte, word, sql string, err error) byte {
	switch {
	case err != nil:
		if tx == 'T' {
			return 'E'
		}
	case word == "begin" || word == "start":
		return 'T'
	case sqlkit.EndsTx(sql, sqlkit.DuckDB) != "":
		return 'I'
	}
	return tx
}

// prepare is the statement the library made of sql, or what it says of one it could not.
func (conn *Conn) prepare(sql string) (st uintptr, err error) {
	if c.prepare(conn.h, sql, &st) != 0 {
		err = errors.New(goString(c.prepareError(st)))
		c.destroyPrep(&st)
		return 0, err
	}
	return st, nil
}

// simple runs sql that nothing asks of, as the library's own, out of any cancel.
func (conn *Conn) simple(sql string) error {
	var res result
	defer c.destroy(&res)
	if c.query(conn.h, sql, &res) != 0 {
		return errors.New(goString(c.resultError(&res)))
	}
	return nil
}

// describe is the columns the statement answers with, names and types as the
// library says them, in lower case (DECIMAL(18,3) is decimal(18,3)); the names
// are the statement's, where the text of the statement made a copy of a name
// into a_1.
func (conn *Conn) describe(ctx context.Context, sql string) ([]db.Col, error) {
	var res result
	var state int32
	conn.interruptible(ctx, func() { state = c.query(conn.h, "describe "+strings.TrimRight(sql, "; \t\r\n"), &res) })
	defer c.destroy(&res)
	if state != 0 {
		return nil, errors.New(goString(c.resultError(&res)))
	}
	n := c.rowCount(&res)
	cols := make([]db.Col, n)
	for i := range n {
		cols[i] = db.Col{Name: cell(&res, 0, i), Type: strings.ToLower(cell(&res, 1, i))}
	}
	return cols, nil
}

// cell is the text of a cell of a result, "" for NULL.
func cell(res *result, col, row uint64) string {
	if c.isNull(res, col, row) {
		return ""
	}
	return fromC(c.valVarchar(res, col, row))
}

// execute binds args to st, as text the library converts to the type the
// statement wants, and runs it; a cancel of ctx interrupts it.
func (conn *Conn) execute(ctx context.Context, st uintptr, args []db.Val) (*result, error) {
	for i, a := range args {
		var state int32
		if a.Null {
			state = c.bindNull(st, uint64(i+1))
		} else {
			state = c.bindVarchar(st, uint64(i+1), a.S, uint64(len(a.S)))
		}
		if state != 0 {
			return nil, fmt.Errorf("duckdb: no argument %d in the statement", i+1)
		}
	}
	res := new(result)
	var state int32
	conn.interruptible(ctx, func() { state = c.execPrepared(st, res) })
	if state != 0 {
		defer c.destroy(res)
		return nil, errors.New(goString(c.resultError(res)))
	}
	return res, nil
}

// interruptible runs f, a call of the library, and interrupts the connection
// when ctx ends, again every cancelInterval until f is back: the interrupt of a
// statement that has not started is lost (§8.1). None goes out after f.
func (conn *Conn) interruptible(ctx context.Context, f func()) {
	if ctx.Done() == nil {
		f()
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
		case <-stop:
			return
		}
		t := time.NewTicker(cancelInterval)
		defer t.Stop()
		for {
			c.interrupt(conn.h)
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
	f()
	close(stop)
	<-done
}

// read puts the result's columns and rows in r. A statement of rows that was
// wrapped has them all as VARCHAR, cols the types describe told; any other
// is read as the library gives it, the cells of a type it has no text for
// shown as <?> from the first (§8.1). A result of one column, Count or Success,
// is the answer of a command (how many rows it hit): no columns.
// shortcut: duckdb_value_varchar is deprecated (still exported by 2.0.1) and
// reads a VARCHAR alone; read the vectors of duckdb_result_get_chunk when a
// library drops it, or when a statement's own rows need more types.
func (conn *Conn) read(res *result, r *db.Result, cols []db.Col, wrap bool, typ int32, export bool) {
	n := c.colCount(res)
	names := make([]string, n)
	types := make([]int32, n)
	for i := range n {
		names[i], types[i] = goString(c.colName(res, i)), c.colType(res, i)
	}
	if !wrap && n == 1 && (names[0] == "Count" || names[0] == "Success") {
		if names[0] == "Count" && isWrite(typ) {
			r.Affected = int64(c.rowsChanged(res))
		}
		return
	}
	blob := make([]bool, n)
	r.Cols = make([]db.Col, n)
	for i := range n {
		r.Cols[i] = db.Col{Name: names[i]}
		if len(cols) == int(n) {
			r.Cols[i] = cols[i]
		}
		blob[i] = types[i] == typeBlob && !wrap || strings.HasPrefix(r.Cols[i].Type, "blob") && wrap
	}
	unreadable := -1 // the first column of a type with no text
	for i := range n {
		if !wrap && !readable(types[i]) {
			unreadable = int(i)
			break
		}
	}
	rows := c.rowCount(res)
	r.Rows = make([][]db.Val, rows)
	for row := range rows {
		vals := make([]db.Val, n)
		for col := range n {
			switch {
			case unreadable >= 0 && int(col) >= unreadable:
				vals[col] = db.Val{S: "<?>"}
			case c.isNull(res, col, row):
				vals[col] = db.Val{Null: true}
			default:
				s := fromC(c.valVarchar(res, col, row))
				if blob[col] {
					s = blobText(s, export)
				}
				vals[col] = db.Val{S: s}
			}
		}
		r.Rows[row] = vals
	}
}

// blobText is a BLOB as the library writes it, \xAA for a byte that is no
// printable ASCII, and the byte of a backslash or a quote too: the grid's size
// of it, or, for a file, the literal that makes it (F12.4).
func blobText(s string, literal bool) string {
	if literal {
		return "'" + s + "'::BLOB"
	}
	n := 0
	for i := 0; i < len(s); n++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			i += 4
		} else {
			i++
		}
	}
	return db.BinaryShown(n)
}
