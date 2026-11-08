package mysql

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Catalog queries (§8.4), over information_schema as lazysql's
// drivers/mysql.go reads it. A schema is a database.

// userSchema keeps the databases a user browses.
const userSchema = "not in ('information_schema', 'mysql', 'performance_schema', 'sys')"

// Schemas lists the user's databases by name, and the current one, ""
// when the DSN names none.
func (Engine) Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, "select schema_name, coalesce(database(), '') from information_schema.schemata where schema_name "+userSchema+" order by 1")
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas, current = append(schemas, row[0].S), row[1].S
	}
	return schemas, current, nil
}

// Tables lists every database's tables and views, by database and name; a
// view has no row count. Partitions are no tables of their own.
// ponytail: TABLE_ROWS is cached for information_schema_stats_expiry, a
// day by default, so counts lag behind; ANALYZE TABLE refreshes them
func (Engine) Tables(ctx context.Context, c db.Conn, only []string) ([]db.Table, error) {
	r, err := c.Query(ctx, `
select table_schema, table_name, coalesce(table_rows, -1), table_type, coalesce(data_length + index_length, 0)
from information_schema.tables
where `+db.Scope("table_schema "+userSchema, "table_schema", only, sqlkit.MySQL)+`
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
		kind := "r" // pg_class's relkind, which db.Table goes by
		if row[3].S == "VIEW" {
			kind = "v"
		}
		ts[i] = db.Table{Schema: row[0].S, Name: row[1].S, Rows: rows, Kind: kind, Bytes: db.BytesOf(row[4])}
	}
	return ts, nil
}

// TableColumns reads one table's columns, primary key and the unique
// indexes that can stand in for it (§10.1): all their columns not null,
// none an expression. An auto_increment column's default is
// auto_increment; an empty literal one is a pair of quotes, so it shows;
// a BIT is Fixed. The table is ReadOnly with a row identity whose text is
// no value to find a row by, a binary, a blob or a BIT, or on an engine
// with no transactions, a save no longer all or nothing (§10.1).
func (Engine) TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	var out db.Columns
	r, err := c.Query(ctx, `
select column_name, column_type, is_nullable = 'NO', column_default, extra
from information_schema.columns
where table_schema = ? and table_name = ?
order by ordinal_position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: row[1].S, NotNull: row[2].S == "1", Default: row[3].S, Enum: enumOf(row[1].S), Generated: generatedExtra(row[4].S), Fixed: strings.HasPrefix(row[1].S, "bit")}
		switch {
		case strings.Contains(row[4].S, "auto_increment"):
			col.Default = "auto_increment"
		case !row[3].Null && row[3].S == "":
			col.Default = "''"
		}
		out.Cols = append(out.Cols, col)
	}
	// PRIMARY first, then the unique indexes by name: STATISTICS knows no
	// order they were made in. An expression's column_name is NULL.
	r, err = c.Query(ctx, `
select s.index_name, s.column_name, coalesce(c.is_nullable, 'YES')
from information_schema.statistics s
left join information_schema.columns c
  on c.table_schema = s.table_schema and c.table_name = s.table_name and c.column_name = s.column_name
where s.table_schema = ? and s.table_name = ? and s.non_unique = 0
order by s.index_name <> 'PRIMARY', s.index_name, s.seq_in_index`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	for i := 0; i < len(r.Rows); {
		name, cols, ok := r.Rows[i][0].S, []string(nil), true
		for ; i < len(r.Rows) && r.Rows[i][0].S == name; i++ {
			cols, ok = append(cols, r.Rows[i][1].S), ok && !r.Rows[i][1].Null && r.Rows[i][2].S == "NO"
		}
		switch {
		case name == "PRIMARY":
			out.PK = cols
		case ok:
			out.Unique = append(out.Unique, cols)
		}
	}
	for _, k := range out.Key() {
		for _, col := range out.Cols {
			if t := col.Type; col.Name == k && (strings.Contains(t, "binary") || strings.Contains(t, "blob") || col.Fixed) {
				out.ReadOnly = i18n.T("行标识列 %s 是 %s", k, t)
			}
		}
	}
	r, err = c.Query(ctx, `
select t.engine, coalesce(e.transactions, 'YES')
from information_schema.tables t
left join information_schema.engines e on e.engine = t.engine
where t.table_schema = ? and t.table_name = ?`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	if len(r.Rows) > 0 && r.Rows[0][1].S != "YES" {
		out.ReadOnly = i18n.T("引擎 %s 不支持事务", r.Rows[0][0].S)
	}
	return out, nil
}

// enumOf is an enum's labels from its COLUMN_TYPE, enum('a','b'), a quote
// in one doubled, a backslash too; nil for any other type.
func enumOf(typ string) []string {
	rest, ok := strings.CutPrefix(typ, "enum(")
	if !ok {
		return nil
	}
	var labels []string
	for len(rest) > 1 && rest[0] == '\'' {
		var b strings.Builder
		i := 1
		for ; i < len(rest); i++ {
			if rest[i] == '\'' || rest[i] == '\\' {
				if i+1 < len(rest) && rest[i+1] == rest[i] {
					b.WriteByte(rest[i])
					i++
					continue
				}
				if rest[i] == '\'' {
					break
				}
			}
			b.WriteByte(rest[i])
		}
		labels = append(labels, b.String())
		rest = strings.TrimPrefix(rest[min(i+1, len(rest)):], ",")
	}
	return labels
}

// DDL is a table's or a view's definition as SHOW CREATE TABLE gives it
// (§8.4).
func (e Engine) DDL(ctx context.Context, c db.Conn, schema, name string) (string, error) {
	r, err := c.Query(ctx, "show create table "+e.Quote(schema, name))
	if err != nil {
		return "", err
	}
	return r.Rows[0][1].S + ";", nil
}

// generatedExtra reports whether information_schema's EXTRA says the server
// makes the column's value: VIRTUAL GENERATED or STORED GENERATED, not
// DEFAULT_GENERATED, a default that is an expression.
func generatedExtra(extra string) bool {
	e := strings.ToUpper(extra)
	return strings.Contains(e, "VIRTUAL GENERATED") || strings.Contains(e, "STORED GENERATED")
}

// Indexes lists a table's indexes by name, PRIMARY first; an expression is
// its text. MySQL keeps no statement of an index: Def is built from what
// information_schema.statistics says, as CREATE INDEX writes it (a prefix
// length, DESC, an expression in double parentheses, FULLTEXT, SPATIAL,
// INVISIBLE), a primary key as the ALTER that adds it (F14.3).
func (e Engine) Indexes(ctx context.Context, c db.Conn, schema, table string) ([]db.Index, error) {
	r, err := c.Query(ctx, `
select index_name, non_unique, column_name, expression, collation, sub_part, index_type, is_visible
from information_schema.statistics
where table_schema = ? and table_name = ?
order by index_name <> 'PRIMARY', index_name, seq_in_index`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	var out []db.Index
	var parts []string         // the index's key parts, as written
	var kind, invisible string // its FULLTEXT / SPATIAL, its INVISIBLE
	finish := func() {
		if len(out) == 0 {
			return
		}
		ix := &out[len(out)-1]
		list := " (" + strings.Join(parts, ", ") + ")" + invisible + ";"
		switch {
		case ix.Name == "PRIMARY":
			ix.Def = "ALTER TABLE " + e.Quote(schema, table) + " ADD PRIMARY KEY" + list
		case ix.Unique:
			ix.Def = "CREATE UNIQUE INDEX " + e.Quote(ix.Name) + " ON " + e.Quote(schema, table) + list
		default: // a FULLTEXT or SPATIAL one is never unique
			ix.Def = "CREATE " + kind + "INDEX " + e.Quote(ix.Name) + " ON " + e.Quote(schema, table) + list
		}
	}
	for _, row := range r.Rows {
		if n := len(out); n == 0 || out[n-1].Name != row[0].S {
			finish()
			out = append(out, db.Index{Name: row[0].S, Unique: row[1].S == "0"})
			parts, kind, invisible = nil, "", ""
			if t := row[6].S; t == "FULLTEXT" || t == "SPATIAL" {
				kind = t + " "
			}
			if row[7].S == "NO" {
				invisible = " INVISIBLE"
			}
		}
		ix := &out[len(out)-1]
		var part string
		if row[2].Null { // an expression has no column
			ix.Cols = append(ix.Cols, row[3].S)
			part = "((" + row[3].S + "))"
		} else {
			ix.Cols = append(ix.Cols, row[2].S)
			part = e.Quote(row[2].S)
			if !row[5].Null && kind == "" { // a prefix length; a spatial index reports 32 for one and takes none
				part += "(" + row[5].S + ")"
			}
		}
		if row[4].S == "D" {
			part += " DESC"
		}
		parts = append(parts, part)
	}
	finish()
	return out, nil
}

// Keys lists a table's primary key, then its foreign keys by name, each with
// the table and columns it points at (F14.3).
func (Engine) Keys(ctx context.Context, c db.Conn, schema, table string) ([]db.Key, error) {
	var out []db.Key
	r, err := c.Query(ctx, `
select column_name from information_schema.statistics
where table_schema = ? and table_name = ? and index_name = 'PRIMARY' order by seq_in_index`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	if len(r.Rows) > 0 {
		k := db.Key{Name: "PRIMARY"}
		for _, row := range r.Rows {
			k.Cols = append(k.Cols, row[0].S)
		}
		out = append(out, k)
	}
	r, err = c.Query(ctx, `
select constraint_name, column_name, referenced_table_schema, referenced_table_name, referenced_column_name
from information_schema.key_column_usage
where table_schema = ? and table_name = ? and referenced_table_name is not null
order by constraint_name, ordinal_position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	for _, row := range r.Rows {
		if n := len(out); n == 0 || out[n-1].Ref == nil || out[n-1].Name != row[0].S {
			out = append(out, db.Key{Name: row[0].S, Ref: &db.Ref{Schema: row[2].S, Table: row[3].S}})
		}
		k := &out[len(out)-1]
		k.Cols, k.Ref.Cols = append(k.Cols, row[1].S), append(k.Ref.Cols, row[4].S)
	}
	return out, nil
}

// Functions lists every database's functions by database and name, with the
// parameters as `name type, …`; not the procedures (F14.3). A function is
// its database and name, the ID is its name.
func (Engine) Functions(ctx context.Context, c db.Conn, only []string) ([]db.Function, error) {
	r, err := c.Query(ctx, `
select r.routine_schema, r.routine_name,
       coalesce((select group_concat(concat(p.parameter_name, ' ', p.dtd_identifier) order by p.ordinal_position separator ', ')
                 from information_schema.parameters p
                 where p.specific_schema = r.routine_schema and p.specific_name = r.specific_name and p.ordinal_position > 0), '')
from information_schema.routines r
where r.routine_type = 'FUNCTION' and `+db.Scope("r.routine_schema "+userSchema, "r.routine_schema", only, sqlkit.MySQL)+`
order by 1, 2`)
	if err != nil {
		return nil, err
	}
	out := make([]db.Function, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Function{Schema: row[0].S, Name: row[1].S, Args: row[2].S, ID: row[1].S}
	}
	return out, nil
}

// Databases is none: MySQL's databases are its schemas (§5).
func (Engine) Databases(context.Context, db.Conn) ([]string, error) { return nil, nil }

// Sequences is none: MySQL has no sequences.
func (Engine) Sequences(context.Context, db.Conn, []string) ([]db.Sequence, error) { return nil, nil }

// FunctionDef is f's CREATE FUNCTION as SHOW CREATE FUNCTION gives it.
func (e Engine) FunctionDef(ctx context.Context, c db.Conn, f db.Function) (string, error) {
	r, err := c.Query(ctx, "show create function "+e.Quote(f.Schema, f.ID))
	if err != nil || len(r.Rows) == 0 {
		return "", err
	}
	if r.Rows[0][2].Null { // the server shows the statement to the definer and to SHOW_ROUTINE, not to ALL on a database
		return "", errors.New(i18n.T("没有读函数定义的权限：要 SHOW_ROUTINE，或是它的 DEFINER"))
	}
	return r.Rows[0][2].S + ";", nil
}

// SystemSchemas are MySQL's own databases: mysql, sys and the two schemas of
// metadata.
func (Engine) SystemSchemas(ctx context.Context, c db.Conn) ([]string, error) {
	return db.Names(ctx, c, "select schema_name from information_schema.schemata where not (schema_name "+userSchema+") order by 1")
}
