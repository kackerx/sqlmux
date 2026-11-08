package db

import (
	"context"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// Source is the rows behind a table tab (§5「接口缝」): one SQL table or view,
// or one Redis key. It is bound to its connection's Workers and to the table
// or the key, so the tab never builds a statement.
type Source interface {
	// Columns is what the tab knows of the rows' columns: their names, the
	// row identity, why a column or the table is not to edit, which columns
	// sort. A tab asks it with every fetch.
	Columns(ctx context.Context) (Columns, error)
	Page(ctx context.Context, q PageQuery) (Paged, error)
	// Count is how many rows the filter keeps; -1 when the source cannot say.
	Count(ctx context.Context, filter string) (int64, error)
	// Each gives the rows q keeps one at a time, head first, for an export;
	// with q.Limit it is that page.
	Each(ctx context.Context, q PageQuery, head func([]Col) error, row func([]Val) error) error
	// Save writes ch as one unit: failed is the change at fault in the order
	// deletes, updates, inserts, -1 for none; tx is where the connection's
	// transaction is as it ends, 0 when it never got the connection.
	Save(ctx context.Context, ch Changes, manual bool) (failed int, tx byte, err error)
	// Preview is what Save would send for ch, a text each, with the values in:
	// shown before it goes (§13, F17.10). None when the source cannot say.
	Preview(ch Changes) []string
}

// PageQuery is a page as the tab asks for it: the columns Columns gave, a
// filter the source reads as it does (SQL's WHERE, Redis's MATCH), the
// sorting, and the place: an offset, or the Cursor the page before gave.
type PageQuery struct {
	Cols          Columns
	Filter        string
	Order         []OrderCol
	Limit, Offset int
	Cursor        string
	// Live asks for what the connection has written and not committed: the
	// reads of the manual transaction mode, or of one the user began (§8.2).
	Live bool
}

// Paged is one page and what the next one needs. A Cursor that is not empty
// pages by cursor: only the next page is there, and the tab keeps the cursor
// of each page it has seen. Info is the line a key tab shows under its title
// (§8.7); a SQL table has none.
type Paged struct {
	Result
	Next   bool
	Cursor string
	Info   []Attr
}

// Attr is one name and value of a Paged's Info line.
type Attr struct{ Name, Value string }

// ErrReadOnly is a save on a read-only connection (§13); ErrTruncated an Each
// that stopped at the source's budget, what it gave a whole file of what it
// read, which an export says so (§8.7).
var (
	ErrReadOnly  = msgError(i18n.N("只读连接"))
	ErrTruncated = msgError(i18n.N("超过扫描预算，只导出了前面的一部分"))
)

// SQLTable is a Source that is a table or a view of a SQL engine: the
// statements of Page, Count, Select and Save, each on the Worker it is read
// or written on. Known is the catalog's columns when the tab has them; with
// none, Columns reads them.
type SQLTable struct {
	E            Engine
	Schema, Name string
	Meta, Main   *Worker
	Known        Columns
	ReadOnly     bool
}

func (s SQLTable) Columns(ctx context.Context) (Columns, error) {
	if s.Known.Cols != nil {
		return s.Known, nil
	}
	return s.E.TableColumns(ctx, s.Meta, s.Schema, s.Name)
}

// query is q as the statements take it.
func (s SQLTable) query(q PageQuery) Query {
	return Query{Schema: s.Schema, Table: s.Name, Where: q.Filter, Order: q.Order, Key: q.Cols.Key(), Lead: q.Cols.Lead, Limit: q.Limit, Offset: q.Offset}
}

// reads is the Worker a read runs on: Meta, or Main to see what it wrote.
func (s SQLTable) reads(q PageQuery) *Worker {
	if q.Live {
		return s.Main
	}
	return s.Meta
}

// Page reads in a savepoint in a transaction: a WHERE that fails fails it alone.
func (s SQLTable) Page(ctx context.Context, q PageQuery) (p Paged, err error) {
	err = s.reads(q).RunSafe(ctx, func(ctx context.Context, c Conn) error {
		return Guarded(ctx, c, "sqlmux_page", func() (err error) { p.Result, p.Next, err = Page(ctx, c, s.E, s.query(q)); return err })
	})
	return p, err
}

// Count counts on Meta whatever the mode: rows added or deleted and not
// committed are not counted, the pages going by Next all the same.
func (s SQLTable) Count(ctx context.Context, filter string) (int64, error) {
	return Count(ctx, s.Meta, s.E, Query{Schema: s.Schema, Table: s.Name, Where: filter})
}

func (s SQLTable) Each(ctx context.Context, q PageQuery, head func([]Col) error, row func([]Val) error) error {
	sql := Select(s.E, s.query(q))
	if q.Limit > 0 {
		sql += placeOf(s.E, s.query(q), q.Limit, q.Offset)
	}
	return s.reads(q).Run(ctx, func(ctx context.Context, c Conn) error {
		// in a transaction of the user's a failed read would fail it all (§10.3)
		return Guarded(ctx, c, "sqlmux_export", func() error { return Each(ctx, c, sql, head, row) })
	})
}

// Preview is the statements Save sends, from the columns the tab knows; none
// without them.
func (s SQLTable) Preview(ch Changes) []string {
	if s.Known.Cols == nil {
		return nil
	}
	return Preview(s.E, s.Schema, s.Name, s.Known.Key(), withTypes(ch, s.Known))
}

// withTypes is ch with the columns' types, which a Typer's arguments take.
func withTypes(ch Changes, cols Columns) Changes {
	if ch.Types == nil {
		ch.Types = map[string]string{}
		for _, c := range cols.Cols {
			ch.Types[c.Name] = c.Type
		}
	}
	return ch
}

func (s SQLTable) Save(ctx context.Context, ch Changes, manual bool) (int, byte, error) {
	if s.ReadOnly {
		return -1, 0, ErrReadOnly
	}
	cols, err := s.Columns(ctx)
	if err != nil {
		return -1, 0, err
	}
	return Save(ctx, s.Main, s.E, s.Schema, s.Name, cols.Key(), withTypes(ch, cols), manual)
}
