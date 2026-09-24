package db

// Table is one relation the schema tree lists (§8.4): a table, a view,
// a materialized view, a foreign table, or a partitioned table's parent.
type Table struct {
	Schema, Name string
	Rows         float64 // the server's estimate; < 0 when it has none
}

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
