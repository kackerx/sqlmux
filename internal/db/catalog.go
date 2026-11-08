package db

import (
	"context"
	"slices"
	"strconv"
)

// Table is one relation the schema tree lists (§8.4): a table, a view,
// a materialized view, a foreign table, or a partitioned table's parent.
type Table struct {
	Schema, Name string
	Rows         float64 // the server's estimate; < 0 when it has none
	Kind         string  // pg_class.relkind: r, p, f are tables; v, m views
	// Bytes is what it takes on disk, its indexes with it where the server
	// says them together; 0 when it does not say
	Bytes int64
}

// bytesOf is a catalog's size text as Bytes: 0 for none.
func bytesOf(s string) int64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return 0
	}
	return int64(n)
}

// BytesOf is a size a catalog query read as text, 0 when it has none (NULL, -1).
func BytesOf(v Val) int64 {
	if v.Null {
		return 0
	}
	return bytesOf(v.S)
}

// View reports whether t is listed under Views, not Tables (§7.8).
func (t Table) View() bool { return t.Kind == "v" || t.Kind == "m" }

// Column is one column's attributes, for the grid and the cell editor
// (§8.4, §10.2).
type Column struct {
	Name      string
	Type      string // as the server formats it: "bigint", "numeric(10,2)", "order_status"
	NotNull   bool
	Default   string   // the default's expression, an identity column's how it is generated; "" when there is none
	Enum      []string // the labels in their sort order, when the type is an enum
	Generated bool     // the server makes the value: an identity column that is always generated, a generated column; INSERT may not name it
	Fixed     bool     // not to edit: MySQL's BIT, shown as a number it does not take back (§8.1)
	AddOnly   bool     // DEFAULT is for a row added: SQLite has none in an UPDATE (F14.1)
	ReadOnly  string   // why the column takes no edit, "" when it does: SQLite's rowid, a generated column
}

// Columns is what the catalog knows of one table's columns.
type Columns struct {
	Cols   []Column
	PK     []string   // the primary key, in key order
	Unique [][]string // unique indexes over not-null columns, in key order (§10.1)
	// ReadOnly is why a table with a row identity is not to edit, "" when
	// it is: MySQL's with a binary one or an engine with no transactions
	// (§10.1).
	ReadOnly string
	// Lead is what select lists before *, the key select * does not give:
	// SQLite's "rowid, " for a table with no primary key (F14.1).
	Lead string
	// Sortable is the columns the source can sort by: nil for every one, which
	// a SQL table can; an empty slice for none (§5「接口缝」).
	Sortable []string
	// NoInsert is why a row cannot be added, "" when one can: a Redis stream's
	// entries are made with XADD, not a row.
	NoInsert string
	// NoUpdate is why a cell of a row that is there cannot be changed, "" when
	// it can: a Redis stream's entries are deleted, not changed.
	NoUpdate string
}

// CanSort is whether the source can sort by column name.
func (c Columns) CanSort(name string) bool {
	return c.Sortable == nil || slices.Contains(c.Sortable, name)
}

// Key is the columns that identify a row (§10.1): the primary key, else the
// first unique index over not-null columns; nil when there is neither.
func (c Columns) Key() []string {
	if c.PK != nil {
		return c.PK
	}
	if len(c.Unique) > 0 {
		return c.Unique[0]
	}
	return nil
}

// Index is one index of a table (§8.4, F14.3): its key columns (an expression
// as its text), whether it is unique, and the statement that makes it.
type Index struct {
	Name   string
	Cols   []string
	Unique bool
	Def    string
}

// Key is a table's primary key, or one of its foreign keys, which has Ref:
// the columns it points at (§8.4, F14.3). A name is "" for a foreign key SQLite
// does not name.
type Key struct {
	Name string
	Cols []string
	Ref  *Ref
}

// Ref is the table and columns a foreign key points at.
type Ref struct {
	Schema, Table string
	Cols          []string
}

// Function is one function of a schema, Args its arguments as the engine
// writes them in a signature; ID is what FunctionDef asks by (F14.3).
type Function struct {
	Schema, Name, Args, ID string
}

// Sequence is one sequence of a schema (§8.4).
type Sequence struct{ Schema, Name string }

// Names is the first column of what sql reads on c, as text: a catalog's list
// of names.
func Names(ctx context.Context, c Conn, sql string) ([]string, error) {
	r, err := c.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = row[0].S
	}
	return out, nil
}
