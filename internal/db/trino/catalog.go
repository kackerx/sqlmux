package trino

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Catalog queries (§8.4) over each catalog's information_schema: a
// connection's catalog is its database (§5), its schemas the level below.

// Schemas lists the catalog's schemas, information_schema aside, and the one
// the session is in.
func (Engine) Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, `select schema_name from information_schema.schemata where schema_name <> 'information_schema' order by 1`)
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas = append(schemas, row[0].S)
	}
	if own, ok := connOf(c); ok {
		current = own.schema
	}
	if current == "" && len(schemas) > 0 {
		current = schemas[0]
	}
	return schemas, current, nil
}

// Databases lists the server's catalogs (§8.4): each one a child of the
// connection in the tree, as PG's databases are.
func (Engine) Databases(ctx context.Context, c db.Conn) ([]string, error) {
	r, err := c.Query(ctx, "show catalogs")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, row := range r.Rows {
		names = append(names, row[0].S)
	}
	return names, nil
}

// Tables lists the catalog's tables and views by schema and name; Trino keeps
// no row count a query can ask cheaply, so there is none (-1).
func (Engine) Tables(ctx context.Context, c db.Conn, only []string) ([]db.Table, error) {
	r, err := c.Query(ctx, `
select table_schema, table_name, table_type from information_schema.tables
where table_schema <> 'information_schema'`+db.SchemaIn("table_schema", only, sqlkit.Trino)+`
order by 1, 2`)
	if err != nil {
		return nil, err
	}
	ts := make([]db.Table, len(r.Rows))
	for i, row := range r.Rows {
		kind := "r"
		if row[2].S == "VIEW" {
			kind = "v"
		}
		ts[i] = db.Table{Schema: row[0].S, Name: row[1].S, Rows: -1, Kind: kind}
	}
	return ts, nil
}

// TableColumns reads a table's columns. Trino tells no connector's keys, so a
// row's identity is its columns that are NOT NULL and of a type that compares
// (§10.1): a save counts the rows each change would hit and writes only where
// that is one (§10.3). A table with none of those, or a view, is not to edit;
// a varbinary, which the grid shows by its size, and an array, a map or a row,
// whose JSON no text casts back to, are not to edit either.
func (e Engine) TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	r, err := c.Query(ctx, `
select c.column_name, c.data_type, c.is_nullable, t.table_type
from information_schema.columns c join information_schema.tables t on t.table_schema = c.table_schema and t.table_name = c.table_name
where c.table_schema = $1 and c.table_name = $2 order by c.ordinal_position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return db.Columns{}, err
	}
	if len(r.Rows) == 0 {
		return db.Columns{}, errors.New(i18n.T("找不到表 %s.%s", schema, table))
	}
	var cols db.Columns
	var key []string
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: row[1].S, NotNull: row[2].S == "NO", AddOnly: true}
		if !editable(col.Type) {
			col.ReadOnly = i18n.T("%s 类型的列不能在表格里改", col.Type)
		}
		if col.NotNull && col.ReadOnly == "" {
			key = append(key, col.Name)
		}
		cols.Cols = append(cols.Cols, col)
	}
	switch {
	case r.Rows[0][3].S == "VIEW":
	case len(key) == 0:
		cols.ReadOnly = i18n.T("Trino 不提供主键：没有 NOT NULL 的列可以认出一行")
	default:
		cols.PK = key
	}
	return cols, nil
}

// editable is whether a column of Trino type typ takes its text back: not a
// varbinary, an array, a map or a row.
func editable(typ string) bool {
	for _, no := range []string{"varbinary", "array(", "map(", "row(", "hyperloglog", "qdigest", "tdigest", "setdigest"} {
		if strings.HasPrefix(typ, no) {
			return false
		}
	}
	return true
}

// DDL is the statement that makes the table or the view, as SHOW CREATE writes it.
func (e Engine) DDL(ctx context.Context, c db.Conn, schema, name string) (string, error) {
	r, err := c.Query(ctx, `select table_type from information_schema.tables where table_schema = $1 and table_name = $2`, db.Val{S: schema}, db.Val{S: name})
	if err != nil {
		return "", err
	}
	object := "table"
	if len(r.Rows) > 0 && r.Rows[0][0].S == "VIEW" {
		object = "view"
	}
	r, err = c.Query(ctx, "show create "+object+" "+e.Quote(schema, name))
	if err != nil || len(r.Rows) == 0 {
		return "", err
	}
	return r.Rows[0][0].S + ";", nil
}

// Indexes, Keys, Functions, Sequences and FunctionDef are none: Trino tells
// no connector's.
func (Engine) Indexes(context.Context, db.Conn, string, string) ([]db.Index, error) { return nil, nil }
func (Engine) Keys(context.Context, db.Conn, string, string) ([]db.Key, error)      { return nil, nil }
func (Engine) Functions(context.Context, db.Conn, []string) ([]db.Function, error)  { return nil, nil }
func (Engine) Sequences(context.Context, db.Conn, []string) ([]db.Sequence, error)  { return nil, nil }
func (Engine) FunctionDef(context.Context, db.Conn, db.Function) (string, error)    { return "", nil }

// Quick runs sql, one statement, for the palette's quick SQL (§12): in a
// read-only transaction, always rolled back, so a write is the server's to
// refuse; the words keep one from going at all. In a transaction of the
// user's it runs there. schema is where unqualified names go. The read gets
// the LIMIT §9.4 gives a query's, a row past maxRows.
func (e Engine) Quick(ctx context.Context, w *db.Worker, sql, schema string, maxRows int) (r db.Result, err error) {
	if sqlkit.IsWrite(sql, sqlkit.Trino) {
		return db.Result{}, errors.New(i18n.T("快速 SQL 是只读的：这条是写语句"))
	}
	err = w.RunSafe(ctx, func(ctx context.Context, c db.Conn) error {
		if schema != "" {
			if err := db.Setup(c, func() error { return e.UseSchema(ctx, c, schema, "") }); err != nil {
				return err
			}
		}
		if db.TxStatus(c) == 'I' {
			if _, err := c.Query(ctx, "start transaction read only"); err != nil {
				return err
			}
			defer func() { _, _ = db.End(ctx, c, "rollback") }()
		}
		text, _ := sqlkit.AutoLimit(sql, sqlkit.Trino, maxRows+1, 0)
		start := time.Now()
		r, err = c.Query(ctx, text)
		if len(r.Rows) > maxRows {
			r.Rows, r.Truncated = r.Rows[:maxRows], true
		}
		r.Took = time.Since(start)
		return err
	})
	return r, err
}

var (
	_ db.Engine   = Engine{}
	_ db.Typer    = Engine{}
	_ db.Unatomic = Engine{}
)
