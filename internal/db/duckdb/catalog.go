//go:build !linux || duckdb

package duckdb

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Catalog queries (§8.4), over duckdb_tables(), duckdb_columns() and the
// rest of the library's own functions. What they list is of the database the
// connection is in (current_database()): the system's schemas are another's,
// and the user's main is flagged internal.

const mine = "database_name = current_database()"

func (Engine) Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, "select schema_name, current_schema() from duckdb_schemas() where "+mine+" order by 1")
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas, current = append(schemas, row[0].S), row[1].S
	}
	return schemas, current, nil
}

// Databases is none: an attached database is none of ours (§8.1).
func (Engine) Databases(context.Context, db.Conn) ([]string, error) { return nil, nil }

// Tables lists the tables and views by schema and name; estimated_size is the
// row count the library keeps, a view has none.
func (Engine) Tables(ctx context.Context, c db.Conn, only []string) ([]db.Table, error) {
	in := db.SchemaIn("schema_name", only, sqlkit.DuckDB)
	r, err := c.Query(ctx, `
select schema_name, table_name, estimated_size, 'r' from duckdb_tables() where `+mine+in+`
union all
select schema_name, view_name, -1, 'v' from duckdb_views() where `+mine+in+`
order by 1, 2`)
	if err != nil {
		return nil, err
	}
	ts := make([]db.Table, len(r.Rows))
	for i, row := range r.Rows {
		rows, err := strconv.ParseFloat(row[2].S, 64)
		if err != nil {
			return nil, err
		}
		ts[i] = db.Table{Schema: row[0].S, Name: row[1].S, Rows: rows, Kind: row[3].S}
	}
	return ts, nil
}

// names is a list the library wrote as JSON, to_json(list).
func names(s string) (out []string, err error) {
	if s == "" {
		return nil, nil
	}
	err = json.Unmarshal([]byte(s), &out)
	return out, err
}

// TableColumns reads a table's columns, its primary key, and the unique
// constraints and indexes over not-null columns that can stand in for it
// (§10.1). A generated column is read only. The row identity is the key or a
// unique one, never the rowid, which DuckDB changes as it compacts a table
// (§8.1); a table with neither is read only, and so is one whose key is a
// BLOB, the bytes of which are no text to find a row by.
func (Engine) TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	var out db.Columns
	where := " where " + mine + " and schema_name = $1 and table_name = $2"
	args := []db.Val{{S: schema}, {S: table}}
	r, err := c.Query(ctx, "select column_name, data_type, is_nullable, column_default, is_generated from duckdb_columns()"+where+" order by column_index", args...)
	if err != nil {
		return out, err
	}
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: strings.ToLower(row[1].S), NotNull: row[2].S == "false", Default: row[3].S, Generated: row[4].S == "true"}
		if col.Generated {
			col.Default, col.ReadOnly = "", i18n.T("%s 是生成列，只读", col.Name)
		}
		out.Cols = append(out.Cols, col)
	}
	cons, err := c.Query(ctx, "select constraint_type, to_json(constraint_column_names) from duckdb_constraints()"+where+" and constraint_type in ('PRIMARY KEY', 'UNIQUE') order by constraint_index", args...)
	if err != nil {
		return out, err
	}
	notNull := func(cols []string) bool {
		return len(cols) > 0 && !slices.ContainsFunc(cols, func(n string) bool {
			i := slices.IndexFunc(out.Cols, func(col db.Column) bool { return col.Name == n })
			return i < 0 || !out.Cols[i].NotNull
		})
	}
	for _, row := range cons.Rows {
		cols, err := names(row[1].S)
		if err != nil {
			return out, err
		}
		switch {
		case row[0].S == "PRIMARY KEY":
			out.PK = cols
		case notNull(cols):
			out.Unique = append(out.Unique, cols)
		}
	}
	ixs, err := (Engine{}).Indexes(ctx, c, schema, table)
	if err != nil {
		return out, err
	}
	for _, ix := range ixs {
		if ix.Unique && notNull(ix.Cols) { // an expression is no column: notNull has none by that name
			out.Unique = append(out.Unique, ix.Cols)
		}
	}
	for _, k := range out.Key() {
		for _, col := range out.Cols {
			if col.Name == k && col.Type == "blob" {
				out.ReadOnly = i18n.T("行标识列 %s 是 %s", k, col.Type)
			}
		}
	}
	return out, nil
}

// DDL is a table's or a view's definition as the library keeps it (§8.4).
func (Engine) DDL(ctx context.Context, c db.Conn, schema, name string) (string, error) {
	r, err := c.Query(ctx, `
select sql from duckdb_tables() where `+mine+` and schema_name = $1 and table_name = $2
union all
select sql from duckdb_views() where `+mine+` and schema_name = $1 and view_name = $2`, db.Val{S: schema}, db.Val{S: name})
	if err != nil || len(r.Rows) == 0 {
		return "", err
	}
	return r.Rows[0][0].S, nil
}

// Indexes lists a table's indexes, those made with CREATE INDEX: what a
// PRIMARY KEY or UNIQUE makes is a constraint (Keys). Def is the library's
// statement; Cols are the keys, an expression as its text.
func (Engine) Indexes(ctx context.Context, c db.Conn, schema, table string) ([]db.Index, error) {
	r, err := c.Query(ctx, "select index_name, is_unique, expressions, sql from duckdb_indexes() where "+mine+" and schema_name = $1 and table_name = $2 order by index_name", db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	out := make([]db.Index, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Index{Name: row[0].S, Unique: row[1].S == "true", Cols: parseList(row[2].S), Def: row[3].S}
	}
	return out, nil
}

// parseList is a list of texts as the library writes one (duckdb_indexes'
// expressions in 2.0.1 is a VARCHAR of it): [a, 'b c', '"d"']. An item that is
// quoted is read as the text it holds, a "quoted" name as the name.
// shortcut: an item that has a ', ' of its own outside quotes is cut there.
func parseList(s string) []string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	var out []string
	for s != "" {
		var item string
		if s[0] == '\'' {
			end := 1
			for end < len(s) && s[end] != '\'' {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			item = strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(s[1:min(end, len(s))])
			s = s[min(end+1, len(s)):]
		} else {
			item, s, _ = strings.Cut(s, ", ")
		}
		s = strings.TrimPrefix(s, ", ")
		if len(item) > 1 && item[0] == '"' && item[len(item)-1] == '"' {
			item = strings.ReplaceAll(item[1:len(item)-1], `""`, `"`)
		}
		out = append(out, item)
	}
	return out
}

// Keys lists a table's primary key, which has no name in DuckDB, then its
// foreign keys, unnamed too, which point at a table of the same schema (it
// allows no other).
func (Engine) Keys(ctx context.Context, c db.Conn, schema, table string) ([]db.Key, error) {
	r, err := c.Query(ctx, `
select constraint_type, to_json(constraint_column_names), referenced_table, to_json(referenced_column_names)
from duckdb_constraints() where `+mine+` and schema_name = $1 and table_name = $2 and constraint_type in ('PRIMARY KEY', 'FOREIGN KEY')
order by constraint_type <> 'PRIMARY KEY', constraint_index`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	var out []db.Key
	for _, row := range r.Rows {
		cols, err := names(row[1].S)
		if err != nil {
			return nil, err
		}
		k := db.Key{Cols: cols}
		if row[0].S == "PRIMARY KEY" {
			k.Name = "PRIMARY"
		} else {
			k.Ref = &db.Ref{Schema: schema, Table: row[2].S}
			if k.Ref.Cols, err = names(row[3].S); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	return out, nil
}

// Functions lists the user's macros, the only functions a user makes in SQL;
// Args are their parameters, ID is what FunctionDef asks by.
func (Engine) Functions(ctx context.Context, c db.Conn, only []string) ([]db.Function, error) {
	r, err := c.Query(ctx, `
select schema_name, function_name, array_to_string(parameters, ', ') from duckdb_functions()
where `+mine+` and not internal and function_type in ('macro', 'table_macro')`+db.SchemaIn("schema_name", only, sqlkit.DuckDB)+`
order by 1, 2, 3`)
	if err != nil {
		return nil, err
	}
	out := make([]db.Function, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Function{Schema: row[0].S, Name: row[1].S, Args: row[2].S, ID: row[2].S}
	}
	return out, nil
}

func (Engine) Sequences(ctx context.Context, c db.Conn, only []string) ([]db.Sequence, error) {
	r, err := c.Query(ctx, "select schema_name, sequence_name from duckdb_sequences() where "+mine+db.SchemaIn("schema_name", only, sqlkit.DuckDB)+" order by 1, 2")
	if err != nil {
		return nil, err
	}
	out := make([]db.Sequence, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Sequence{Schema: row[0].S, Name: row[1].S}
	}
	return out, nil
}

// FunctionDef is the statement that makes a macro: its parameters and body as
// the library keeps them.
// shortcut: a parameter's default (b := 2) is not kept by the library's
// catalog, so it is not in the statement.
func (e Engine) FunctionDef(ctx context.Context, c db.Conn, f db.Function) (string, error) {
	r, err := c.Query(ctx, `
select function_type, macro_definition from duckdb_functions()
where `+mine+` and schema_name = $1 and function_name = $2 and array_to_string(parameters, ', ') = $3 and function_type in ('macro', 'table_macro')`,
		db.Val{S: f.Schema}, db.Val{S: f.Name}, db.Val{S: f.ID})
	if err != nil {
		return "", err
	}
	if len(r.Rows) == 0 { // a macro dropped since the catalog was read
		return "", errors.New(i18n.T("函数已不存在，按 R 刷新"))
	}
	body := r.Rows[0][1].S
	if r.Rows[0][0].S == "table_macro" {
		body = "table " + body
	}
	var params []string
	if f.Args != "" {
		params = strings.Split(f.Args, ", ")
	}
	for i, p := range params {
		params[i] = e.Quote(p)
	}
	return "create macro " + e.Quote(f.Schema, f.Name) + "(" + strings.Join(params, ", ") + ") as " + body + ";", nil
}
