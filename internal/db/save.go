package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
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
	Val     Val  // the new value; unused when Default
	Default bool // the DEFAULT keyword
	Old     Val  // as loaded: the row must still hold it
}

// ErrStale is a row that no longer holds what was loaded, or is gone; ErrGone
// one to delete that is gone; ErrNotInserted one to insert that went in as
// none, a PG BEFORE trigger's NULL.
var (
	ErrStale       = msgError(i18n.N("行数据已变化或行不存在"))
	ErrGone        = msgError(i18n.N("行不存在"))
	ErrNotInserted = msgError(i18n.N("没有插入"))
)

// msgError is an error that says itself in the language in use, when it is
// asked (§14「界面语言」); the same text is the same error.
type msgError string

func (e msgError) Error() string { return i18n.T(string(e)) }

// Changes is what a save writes (§10.3, §10.6): the rows to delete, by
// their row identity's values; the rows to update; the rows to insert,
// each the columns set in it, Val or Default, the rest their DEFAULT.
type Changes struct {
	Deletes [][]string
	Updates []Row
	Inserts [][]Change
	// Types is each column's type, as the catalog has it, for an engine that
	// takes every argument as its column's type (Typer); none for the others.
	Types map[string]string
}

// Typer is an Engine that reads no text as a number or a date where a column
// of one is compared or set (§10.3): ClickHouse, Trino. Each argument of a
// save goes in through Typed, as its column's type.
type Typer interface {
	Typed(arg, typ string) string
}

// Unatomic is an Engine whose writes each stand on their own, with no
// transaction to hold a save's together (§10.3): ClickHouse, whose UPDATE and
// DELETE are mutations, and Trino, whose connectors write in autocommit only.
// Save counts the rows each UPDATE and DELETE would hit before it sends it,
// and what was written before a statement that fails stays written (Written).
type Unatomic interface {
	// Write is the statement that sets set in the rows of table where holds,
	// or, set "", deletes them, in the engine's words.
	Write(table, set, where string) string
}

// Written is the error of an Unatomic engine's save that failed at statement
// N, the N before it written and not to be taken back (§10.3).
type Written struct {
	N   int
	Err error
}

func (e Written) Error() string { return e.Err.Error() }
func (e Written) Unwrap() error { return e.Err }

// endTimeout bounds COMMIT and ROLLBACK, which a cancel must not reach: a
// COMMIT the server has done would come back as an error (§10.3).
const endTimeout = 10 * time.Second

// End runs sql, a COMMIT, a ROLLBACK or the like, on c out of ctx's cancel,
// bounded by endTimeout. It returns the command tag: a COMMIT of a failed
// transaction's is ROLLBACK.
func End(ctx context.Context, c Conn, sql string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), endTimeout)
	defer cancel()
	r, err := c.Query(ctx, sql)
	return r.Tag, err
}

// Guarded runs f on c, in savepoint name, a plain word, when c is in a
// transaction: f failing takes back what f did alone, where an error would
// leave the whole transaction failed, all the user did in it lost (§10.3).
func Guarded(ctx context.Context, c Conn, name string, f func() error) error {
	if TxStatus(c) == 'I' {
		return f()
	}
	set, to, release := "savepoint "+name, "rollback to savepoint "+name, "release savepoint "+name
	if t, ok := saverOf(c); ok {
		set, to, release = t.Savepoint(name)
	}
	if _, err := c.Query(ctx, set); err != nil {
		return err
	}
	if err := f(); err != nil {
		_, _ = End(ctx, c, to)
		if release != "" {
			_, _ = End(ctx, c, release)
		}
		return err
	}
	if release == "" {
		return nil
	}
	_, err := End(ctx, c, release)
	return err
}

// TxSaver is a Conn whose transaction wording differs from the default's
// (§10.3): SQL Server begins with BEGIN TRANSACTION, keeps a savepoint with
// SAVE TRANSACTION, takes what came after it back with ROLLBACK TRANSACTION,
// and has no release, its save names standing till the transaction ends
// (F18.6). The db package's words stand for the rest.
type TxSaver interface {
	// Begin is the statement that starts a transaction.
	Begin() string
	// Savepoint is name's three statements: to set it, to take back what
	// came after it, and to release it, "" for none.
	Savepoint(name string) (set, rollback, release string)
}

// saverOf is c as a TxSaver, through a counted's Unwrap (§5「连接缝」).
func saverOf(c Conn) (TxSaver, bool) {
	if t, ok := c.(TxSaver); ok {
		return t, true
	}
	u, ok := c.(interface{ Unwrap() Conn })
	if !ok {
		return nil, false
	}
	t, ok := u.Unwrap().(TxSaver)
	return t, ok
}

// beginOf is the statement that starts a transaction on c.
func beginOf(c Conn) string {
	if t, ok := saverOf(c); ok {
		return t.Begin()
	}
	return "begin"
}

// Save writes ch to schema.table on w, in e's SQL (§10.3, §10.6): first
// the DELETEs, then the UPDATEs, then the INSERTs, each hitting exactly
// one row. A row is found by keyCols with =; an UPDATE's also by each
// changed column still holding what was loaded. In its own transaction it
// commits, or on any failure rolls back. In the manual transaction mode
// (one begun first if need be), or in a transaction the user began, it
// goes in that one, not committed, a failure taking back this save alone.
// failed is the index of the statement at fault in that order, deletes,
// updates, inserts, -1 for none; tx is where the transaction is as it
// ends. Cancelling ctx stops the statements only.
// Reference: lazysql drivers/utils.go queriesInTransaction and its
// ExecutePendingChanges, which run the changes as listed and check
// neither the rows hit nor the loaded values; its INSERT leaves a DEFAULT
// column out, as ours does since F14.1.
func Save(ctx context.Context, w *Worker, e Engine, schema, table string, keyCols []string, ch Changes, manual bool) (failed int, tx byte, err error) {
	stmts := Statements(e, schema, table, keyCols, ch)
	if _, ok := e.(Unatomic); ok {
		return saveEach(ctx, w, stmts)
	}
	failed = -1
	committing := false // the COMMIT of the save's own transaction is out
	err = w.Run(ctx, func(ctx context.Context, c Conn) error {
		defer func() { tx = TxStatus(c) }()
		write := func() error {
			for i, s := range stmts {
				res, err := c.Query(ctx, s.SQL, s.Args...)
				if err == nil && res.Affected != 1 {
					err = s.miss
				}
				if err != nil {
					failed = i
					return err
				}
			}
			return nil
		}
		nested := manual || TxStatus(c) != 'I'
		if TxStatus(c) == 'I' {
			if _, err := c.Query(ctx, beginOf(c)); err != nil {
				_, _ = End(ctx, c, "rollback") // a cancel may land once the server has begun
				return err
			}
		}
		if nested {
			return Guarded(ctx, c, "sqlmux_save", write)
		}
		if err := write(); err != nil {
			_, _ = End(ctx, c, "rollback")
			return err
		}
		committing = true
		_, err := End(ctx, c, "commit")
		return err
	})
	var lost TxLost
	if errors.As(err, &lost) {
		switch {
		case committing: // the connection went with the COMMIT: it took, if it was sent (§8.2)
			err = ErrNotCommitted
			if lost.Sent {
				err = ErrCommitUnknown
			}
		case lost.Err != nil: // before it: the statement's own error, the transaction was this save's and is gone, as rolled back (§8.2)
			err = lost.Err
		}
	}
	return failed, tx, err
}

// Stmt is one statement of a save: its SQL with the placeholders e writes, its
// arguments, and what it is when it hits no row. Count and CountArgs, an
// UPDATE's or a DELETE's, count the rows it would hit: an Unatomic engine's
// save asks them first.
type Stmt struct {
	SQL       string
	Args      []Val
	miss      error
	Count     string
	CountArgs []Val
}

// arg puts a value of column col into a statement: as e writes an argument,
// or as Preview a literal; the column's type around it for a Typer.
type arg func(col string, v Val) string

// builder is one statement of a save, written by sql with the arguments put in
// as arg says, and what it is when it hits no row; where is the condition of an
// UPDATE's or a DELETE's rows, nil for an INSERT.
type builder struct {
	miss  error
	sql   func(arg arg) string
	where func(arg arg) string
}

// builders are the statements of a save for ch, in its order: the DELETEs, then
// the UPDATEs, then the INSERTs (§10.3). The one place that order is written:
// Statements numbers the arguments, Preview puts them in.
func builders(e Engine, schema, table string, keyCols []string, ch Changes) (bs []builder) {
	for _, k := range ch.Deletes {
		where := func(arg arg) string { return strings.Join(keyWhere(e, keyCols, k, arg), " and ") }
		bs = append(bs, builder{ErrGone, func(arg arg) string { return remove(e, schema, table, where(arg)) }, where})
	}
	for _, r := range ch.Updates {
		bs = append(bs, builder{ErrStale, func(arg arg) string { return update(e, schema, table, keyCols, r, arg) }, func(arg arg) string { return rowWhere(e, keyCols, r, arg) }})
	}
	for _, r := range ch.Inserts {
		bs = append(bs, builder{ErrNotInserted, func(arg arg) string { return insert(e, schema, table, keyCols, r, arg) }, nil})
	}
	return bs
}

// Statements is what Save sends for ch, in its order.
func Statements(e Engine, schema, table string, keyCols []string, ch Changes) (stmts []Stmt) {
	for _, b := range builders(e, schema, table, keyCols, ch) {
		args, arg := argsOf(e, ch.Types)
		st := Stmt{SQL: b.sql(arg), Args: *args, miss: b.miss}
		if b.where != nil {
			args, arg := argsOf(e, ch.Types)
			st.Count, st.CountArgs = "select count(*) from "+e.Quote(schema, table)+" where "+b.where(arg), *args
		}
		stmts = append(stmts, st)
	}
	return stmts
}

// Preview is the statements of Statements with each argument written in as a
// literal, one text each: what a save shows before it sends them (§13, F17.10).
func Preview(e Engine, schema, table string, keyCols []string, ch Changes) (out []string) {
	lit := typed(e, ch.Types, func(v Val) string {
		if v.Null {
			return "NULL"
		}
		return sqlkit.Literal(v.S, e.Dialect())
	})
	for _, b := range builders(e, schema, table, keyCols, ch) {
		out = append(out, b.sql(lit))
	}
	return out
}

// keyWhere is what finds a row by its row identity's values, arg taking
// each value in.
func keyWhere(e Engine, keyCols, key []string, arg arg) []string {
	var where []string
	for i, k := range keyCols {
		where = append(where, e.Eq(e.Quote(k), arg(k, Val{S: key[i]})))
	}
	return where
}

// argsOf is a statement's arguments, and what puts each one in as e
// writes them: Preview puts them in as literals.
func argsOf(e Engine, types map[string]string) (*[]Val, arg) {
	var args []Val
	return &args, typed(e, types, func(v Val) string {
		args = append(args, v)
		return e.Arg(len(args))
	})
}

// typed is put as an arg: the value as put writes it, as its column's type
// when e is a Typer and the type is known.
func typed(e Engine, types map[string]string, put func(Val) string) arg {
	t, ok := e.(Typer)
	return func(col string, v Val) string {
		if typ := types[col]; ok && typ != "" {
			return t.Typed(put(v), typ)
		}
		return put(v)
	}
}

// remove is the DELETE of the rows where holds: SQL's, or an Unatomic
// engine's own words.
func remove(e Engine, schema, table, where string) string {
	if u, ok := e.(Unatomic); ok {
		return u.Write(e.Quote(schema, table), "", where)
	}
	return "delete from " + e.Quote(schema, table) + " where " + where
}

// saveEach is Save on an Unatomic engine (§10.3): each statement on its own,
// an UPDATE or a DELETE only once a count says it hits exactly one row, an
// INSERT, or one whose Result says how many it hit, checked after; failed is
// the statement that failed, -1 for none, and the error says how many before
// it were written. Nothing is taken back: there is no transaction to do it.
func saveEach(ctx context.Context, w *Worker, stmts []Stmt) (failed int, tx byte, err error) {
	failed = -1
	err = w.Run(ctx, func(ctx context.Context, c Conn) error {
		defer func() { tx = TxStatus(c) }()
		for i, s := range stmts {
			failed = i
			if s.Count != "" {
				r, err := c.Query(ctx, s.Count, s.CountArgs...)
				if err != nil {
					return err
				}
				if len(r.Rows) != 1 || len(r.Rows[0]) != 1 || r.Rows[0][0].S != "1" {
					return s.miss
				}
			}
			res, err := c.Query(ctx, s.SQL, s.Args...)
			if err != nil {
				return err
			}
			if res.Affected >= 0 && res.Affected != 1 { // < 0: the engine cannot say (a mutation)
				return s.miss
			}
		}
		failed = -1
		return nil
	})
	if err != nil && failed > 0 {
		err = Written{N: failed, Err: err}
	}
	return failed, tx, err
}

// DefaultRow is the INSERT of a row of every column's DEFAULT, as PG's
// DEFAULT VALUES and MySQL's () VALUES () write it (§10.6); kw writes its
// keywords as asked for (identity for the save's own).
func DefaultRow(e Engine, schema, table string, kw func(string) string) string {
	if e.Dialect() == sqlkit.MySQL {
		return kw("insert into") + " " + e.Quote(schema, table) + " () " + kw("values") + " ()"
	}
	return kw("insert into") + " " + e.Quote(schema, table) + " " + kw("default values")
}

// insert is the INSERT of a row with cols set; with none set, every column's
// DEFAULT (§10.6): an Unatomic engine's and Oracle's (neither DEFAULT VALUES
// nor () VALUES () there) as its first key column's DEFAULT, which the others follow.
func insert(e Engine, schema, table string, keyCols []string, cols []Change, arg arg) string {
	var names, vals []string
	for _, c := range cols {
		if !c.Default { // left out, a column takes its DEFAULT: SQLite has no keyword for it in a VALUES
			names, vals = append(names, e.Quote(c.Name)), append(vals, arg(c.Name, c.Val))
		}
	}
	if _, ok := e.(Unatomic); (ok || e.Dialect() == sqlkit.Oracle) && len(names) == 0 && len(keyCols) > 0 {
		names, vals = []string{e.Quote(keyCols[0])}, []string{"DEFAULT"}
	}
	if len(names) == 0 {
		return DefaultRow(e, schema, table, func(s string) string { return s })
	}
	return "insert into " + e.Quote(schema, table) + " (" + strings.Join(names, ", ") + ") values (" + strings.Join(vals, ", ") + ")"
}

// update is r's UPDATE, its values all text: the changed columns must still
// hold what was loaded, as e's Same tells (§10.3).
func update(e Engine, schema, table string, keyCols []string, r Row, arg arg) string {
	var sets []string
	for _, c := range r.Cols {
		v := "DEFAULT"
		if !c.Default {
			v = arg(c.Name, c.Val)
		}
		sets = append(sets, e.Quote(c.Name)+" = "+v)
	}
	set, where := strings.Join(sets, ", "), rowWhere(e, keyCols, r, arg)
	if u, ok := e.(Unatomic); ok {
		return u.Write(e.Quote(schema, table), set, where)
	}
	return "update " + e.Quote(schema, table) + " set " + set + " where " + where
}

// rowWhere is what finds r's row as it was loaded: its row identity, and each
// changed column still holding what was read.
func rowWhere(e Engine, keyCols []string, r Row, arg arg) string {
	where := keyWhere(e, keyCols, r.Key, arg)
	for _, c := range r.Cols {
		if c.Old.Null {
			where = append(where, e.Quote(c.Name)+" is null")
		} else {
			where = append(where, e.Same(e.Quote(c.Name), arg(c.Name, c.Old)))
		}
	}
	return strings.Join(where, " and ")
}
