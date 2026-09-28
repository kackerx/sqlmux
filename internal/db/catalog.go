package db

// Table is one relation the schema tree lists (§8.4): a table, a view,
// a materialized view, a foreign table, or a partitioned table's parent.
type Table struct {
	Schema, Name string
	Rows         float64 // the server's estimate; < 0 when it has none
	Kind         string  // pg_class.relkind: r, p, f are tables; v, m views
}

// View reports whether t is listed under Views, not Tables (§7.8).
func (t Table) View() bool { return t.Kind == "v" || t.Kind == "m" }

// Column is one column's attributes, for the grid and the cell editor
// (§8.4, §10.2).
type Column struct {
	Name    string
	Type    string // as the server formats it: "bigint", "numeric(10,2)", "order_status"
	NotNull bool
	Default string   // the default's expression; "" when there is none
	Enum    []string // the labels in their sort order, when the type is an enum
}

// Columns is what the catalog knows of one table's columns.
type Columns struct {
	Cols   []Column
	PK     []string   // the primary key, in key order
	Unique [][]string // unique indexes over not-null columns, in key order (§10.1)
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
