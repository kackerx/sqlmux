package mssql

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Catalog queries (§8.4), over sys.* as F18.6 has them: the system schemas
// and the four system databases out, the row counts of the partition stats,
// the keys of the indexes.

// isTrue is a bit as the driver's text has it: the bool it decodes to, or
// the server's "1" for whatever wrote the query a different way.
func isTrue(v db.Val) bool { return v.S == "1" || v.S == "true" }

// userSchema keeps the schemas a user browses.
const userSchema = `s.name not in ('sys', 'INFORMATION_SCHEMA', 'guest') and s.name not like 'db[_]%'`

// Schemas lists the user's schemas by name, and the one unqualified names
// go to, the user's default schema (§8.6).
func (Engine) Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, `select s.name, coalesce(schema_name(), '') from sys.schemas s where `+userSchema+` order by 1`)
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas, current = append(schemas, row[0].S), row[1].S
	}
	return schemas, current, nil
}

// Databases lists the databases the user may connect to by name: those it
// has access to, not the system's four (§8.4).
func (Engine) Databases(ctx context.Context, c db.Conn) ([]string, error) {
	r, err := c.Query(ctx, `select name from sys.databases where has_dbaccess(name) = 1 and name not in ('master', 'tempdb', 'model', 'msdb') order by 1`)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(r.Rows))
	for i, row := range r.Rows {
		names[i] = row[0].S
	}
	return names, nil
}

// Tables lists every schema's tables and views, by schema and name, the
// rows the partition stats estimate, a view none of.
func (Engine) Tables(ctx context.Context, c db.Conn, only []string) ([]db.Table, error) {
	r, err := c.Query(ctx, `
select s.name, o.name, coalesce(sum(ps.row_count), -1), rtrim(o.type)
from sys.objects o
join sys.schemas s on s.schema_id = o.schema_id
left join sys.dm_db_partition_stats ps on ps.object_id = o.object_id and ps.index_id in (0, 1)
where o.type in ('U', 'V') and `+db.Scope(userSchema, "s.name", only, sqlkit.MSSQL)+`
group by s.name, o.name, o.type
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
		if row[3].S == "V" {
			kind = "v"
		}
		ts[i] = db.Table{Schema: row[0].S, Name: row[1].S, Rows: rows, Kind: kind}
	}
	return ts, nil
}

// TableColumns reads one table's columns, primary key and the unique
// indexes that can stand in for it (§10.1): all their columns not null,
// none with a filter. An identity column's default is identity(seed,
// increment), and the server makes its value; a computed column's too; a
// row identity of bytes is no value to find a row by, so the table is read
// only, as MySQL's with a binary one is (§10.1).
func (Engine) TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	var out db.Columns
	r, err := c.Query(ctx, `
select c.name, ty.name, c.max_length, c.precision, c.scale, c.is_nullable, coalesce(dc.definition, cc.definition),
       c.is_identity, ic.seed_value, ic.increment_value, c.is_computed
from sys.columns c
join sys.types ty on ty.user_type_id = c.user_type_id
join sys.objects o on o.object_id = c.object_id
join sys.schemas s on s.schema_id = o.schema_id
left join sys.identity_columns ic on ic.object_id = c.object_id and ic.column_id = c.column_id
left join sys.default_constraints dc on dc.object_id = c.default_object_id
left join sys.computed_columns cc on cc.object_id = c.object_id and cc.column_id = c.column_id
where s.name = @p1 and o.name = @p2
order by c.column_id`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	notNull := map[string]bool{}
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: typeText(row[1].S, row[2].S, row[3].S, row[4].S), NotNull: !isTrue(row[5])}
		notNull[col.Name] = col.NotNull
		switch {
		case isTrue(row[7]): // an identity: the server makes the value
			col.Generated = true
			col.Default = "identity(" + numText(row[8]) + ", " + numText(row[9]) + ")"
		case isTrue(row[10]): // computed
			col.Generated = true
		case !row[6].Null:
			col.Default = row[6].S
		}
		out.Cols = append(out.Cols, col)
	}

	r, err = c.Query(ctx, `
select i.name, i.is_primary_key, c.name
from sys.indexes i
join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id and ic.key_ordinal > 0
join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
	where i.object_id = (select o.object_id from sys.objects o join sys.schemas s on s.schema_id = o.schema_id where s.name = @p1 and o.name = @p2)
	  and (i.is_primary_key = 1 or i.is_unique = 1 and i.has_filter = 0)
	order by i.is_primary_key desc, i.name, ic.key_ordinal`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	for i := 0; i < len(r.Rows); {
		name, cols, ok := r.Rows[i][0].S, []string(nil), true
		for ; i < len(r.Rows) && r.Rows[i][0].S == name; i++ {
			cols = append(cols, r.Rows[i][2].S)
			ok = ok && notNull[r.Rows[i][2].S]
		}
		switch {
		case isTrue(r.Rows[i-1][1]):
			out.PK = cols
		case ok:
			out.Unique = append(out.Unique, cols)
		}
	}
	for _, k := range out.Key() {
		for _, col := range out.Cols {
			if t := col.Type; col.Name == k && (strings.Contains(t, "binary") || strings.HasPrefix(t, "rowversion") || strings.HasPrefix(t, "timestamp")) {
				out.ReadOnly = i18n.T("行标识列 %s 是 %s", k, t)
			}
		}
	}
	return out, nil
}

// typeText is a column's type as the server formats it (§8.4): the lengths
// of the strings and the bytes, the precision and scale the column has, the
// scale of the datetime family that takes one, max where it is max.
func typeText(name, length, precision, scale string) string {
	switch name {
	case "char", "varchar", "binary", "varbinary":
		if length == "-1" {
			return name + "(max)"
		}
		return name + "(" + length + ")"
	case "nchar", "nvarchar":
		if length == "-1" {
			return name + "(max)"
		}
		n, _ := strconv.Atoi(length)
		return name + "(" + strconv.Itoa(n/2) + ")"
	case "decimal", "numeric":
		return name + "(" + precision + ", " + scale + ")"
	case "datetime2", "datetimeoffset", "time":
		return name + "(" + scale + ")"
	}
	return name
}

// numText is a number the driver gave as its text — an identity's seed or
// increment, a float in the server's text — as a plain number: 1.0 as 1.
func numText(v db.Val) string {
	if v.Null {
		return "1"
	}
	if f, err := strconv.ParseFloat(v.S, 64); err == nil {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return v.S
}

// DDL is schema.name's definition, as the palette previews it (§12「预览」,
// F4.2). A view's is the text the server keeps of it. A table's is put
// together from the catalog: its columns, their types, what may be null,
// the defaults, the primary key — SQL Server keeps no statement of a table
// as PG's pg_dump or MySQL's SHOW CREATE have it.
// ponytail: no indexes, no foreign keys, no checks, no collations, no
// identity's seed, no computed column's expression; add them when the
// preview is read as a script
func (e Engine) DDL(ctx context.Context, c db.Conn, schema, name string) (string, error) {
	r, err := c.Query(ctx, `
select rtrim(o.type), object_definition(o.object_id)
from sys.objects o join sys.schemas s on s.schema_id = o.schema_id
where s.name = @p1 and o.name = @p2`, db.Val{S: schema}, db.Val{S: name})
	if err != nil {
		return "", err
	}
	if len(r.Rows) == 0 {
		return "", errors.New(i18n.T("表已不存在，按 R 刷新"))
	}
	if r.Rows[0][0].S == "V" {
		if r.Rows[0][1].Null {
			return "", errors.New(i18n.T("没有读视图定义的权限"))
		}
		return r.Rows[0][1].S + ";", nil
	}
	cols, err := e.TableColumns(ctx, c, schema, name) // the columns once: the catalog's types, defaults and keys as the grid has them (F18.6)
	if err != nil {
		return "", err
	}
	pk, err := c.Query(ctx, `
select c.name
from sys.indexes i
join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id and ic.key_ordinal > 0
join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
where i.object_id = (select o.object_id from sys.objects o join sys.schemas s on s.schema_id = o.schema_id where s.name = @p1 and o.name = @p2)
  and i.is_primary_key = 1
order by ic.key_ordinal`, db.Val{S: schema}, db.Val{S: name})
	if err != nil {
		return "", err
	}
	var lines []string
	for _, col := range cols.Cols {
		l := "  " + e.Quote(col.Name) + " " + col.Type
		if col.NotNull {
			l += " not null"
		}
		switch {
		case col.Default != "" && col.Generated && strings.HasPrefix(col.Default, "identity"):
			l += " identity"
		case col.Generated:
			// its expression the preview does not repeat
		case col.Default != "":
			l += " default " + col.Default
		}
		lines = append(lines, l)
	}
	if len(pk.Rows) > 0 {
		var names []string
		for _, row := range pk.Rows {
			names = append(names, e.Quote(row[0].S))
		}
		lines = append(lines, "  primary key ("+strings.Join(names, ", ")+")")
	}
	body := "\n" + strings.Join(lines, ",\n") + "\n"
	if len(lines) == 0 {
		body = ""
	}
	return "create table " + e.Quote(schema, name) + " (" + body + ");", nil
}

// Indexes lists a table's indexes by name, the primary key's too, each as
// the statement that makes it, built — SQL Server keeps none (F14.3).
func (e Engine) Indexes(ctx context.Context, c db.Conn, schema, table string) ([]db.Index, error) {
	r, err := c.Query(ctx, `
select i.name, i.is_unique, i.type, c.name, ic.is_descending_key
from sys.indexes i
join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id and ic.key_ordinal > 0
join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
where i.object_id = (select o.object_id from sys.objects o join sys.schemas s on s.schema_id = o.schema_id where s.name = @p1 and o.name = @p2)
order by i.name, ic.key_ordinal`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	var out []db.Index
	for _, row := range r.Rows {
		if n := len(out); n == 0 || out[n-1].Name != row[0].S {
			unique := isTrue(row[1])
			clustered := ""
			if row[2].S == "1" {
				clustered = " clustered"
			} else if !unique {
				clustered = " nonclustered"
			}
			out = append(out, db.Index{Name: row[0].S, Unique: unique})
			ix := &out[len(out)-1]
			ix.Def = "create" + map[bool]string{true: " unique", false: ""}[unique] + clustered + " index " + e.Quote(ix.Name) +
				" on " + e.Quote(schema, table) + " ("
		}
		ix := &out[len(out)-1]
		if n := len(ix.Cols); n > 0 {
			ix.Def += ", "
		}
		part := e.Quote(row[3].S)
		if isTrue(row[4]) {
			part += " desc"
		}
		ix.Def += part
		ix.Cols = append(ix.Cols, row[3].S)
	}
	for i := range out {
		out[i].Def += ");"
	}
	return out, nil
}

// Keys lists a table's primary key, then its foreign keys by name, each with
// the table and columns it points at (F14.3).
func (Engine) Keys(ctx context.Context, c db.Conn, schema, table string) ([]db.Key, error) {
	var out []db.Key
	r, err := c.Query(ctx, `
select c.name
from sys.indexes i
join sys.index_columns ic on ic.object_id = i.object_id and ic.index_id = i.index_id and ic.key_ordinal > 0
join sys.columns c on c.object_id = ic.object_id and c.column_id = ic.column_id
where i.object_id = (select o.object_id from sys.objects o join sys.schemas s on s.schema_id = o.schema_id where s.name = @p1 and o.name = @p2)
  and i.is_primary_key = 1
order by ic.key_ordinal`, db.Val{S: schema}, db.Val{S: table})
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
select fk.name, c.name, rs.name, rt.name, rc.name
from sys.foreign_keys fk
join sys.foreign_key_columns ic on ic.constraint_object_id = fk.object_id
join sys.columns c on c.object_id = ic.parent_object_id and c.column_id = ic.parent_column_id
join sys.tables rt on rt.object_id = fk.referenced_object_id
join sys.schemas rs on rs.schema_id = rt.schema_id
join sys.columns rc on rc.object_id = ic.referenced_object_id and rc.column_id = ic.referenced_column_id
	where fk.parent_object_id = (select o.object_id from sys.objects o join sys.schemas s on s.schema_id = o.schema_id where s.name = @p1 and o.name = @p2)
order by fk.name, ic.constraint_column_id`, db.Val{S: schema}, db.Val{S: table})
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

// Functions lists every schema's functions, by schema, name and arguments,
// the scalar (FN), the inline (IF) and the multistatement (TF) ones; the ID
// is the object_id, which OBJECT_DEFINITION takes (F14.3).
func (Engine) Functions(ctx context.Context, c db.Conn, only []string) ([]db.Function, error) {
	r, err := c.Query(ctx, `
select s.name, o.name, convert(nvarchar(max), o.object_id),
       coalesce((select string_agg(concat(case when p.name is null then '' else stuff(p.name, 1, 1, '') + ' ' end, type_name(p.user_type_id)), ', ')
                 within group (order by p.parameter_id)
                 from sys.parameters p
                 where p.object_id = o.object_id and p.parameter_id > 0), '')
from sys.objects o
join sys.schemas s on s.schema_id = o.schema_id
where o.type in ('FN', 'IF', 'TF') and `+db.Scope(userSchema, "s.name", only, sqlkit.MSSQL)+`
order by 1, 2`)
	if err != nil {
		return nil, err
	}
	out := make([]db.Function, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Function{Schema: row[0].S, Name: row[1].S, Args: row[3].S, ID: row[2].S}
	}
	return out, nil
}

// Sequences lists every schema's sequences (§8.4).
func (Engine) Sequences(ctx context.Context, c db.Conn, only []string) ([]db.Sequence, error) {
	r, err := c.Query(ctx, `
select s.name, q.name
from sys.sequences q
join sys.schemas s on s.schema_id = q.schema_id
where `+db.Scope(userSchema, "s.name", only, sqlkit.MSSQL)+`
order by 1, 2`)
	if err != nil {
		return nil, err
	}
	out := make([]db.Sequence, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Sequence{Schema: row[0].S, Name: row[1].S}
	}
	return out, nil
}

// FunctionDef is f's definition as the server keeps it (F14.3).
func (Engine) FunctionDef(ctx context.Context, c db.Conn, f db.Function) (string, error) {
	r, err := c.Query(ctx, "select object_definition(convert(int, @p1))", db.Val{S: f.ID})
	if err != nil {
		return "", err
	}
	if len(r.Rows) == 0 || r.Rows[0][0].Null { // a function dropped since the catalog was read
		return "", errors.New(i18n.T("函数已不存在，按 R 刷新"))
	}
	return r.Rows[0][0].S + ";", nil
}

// SystemSchemas are SQL Server's own schemas: sys, INFORMATION_SCHEMA, guest
// and the fixed roles' db_*.
func (Engine) SystemSchemas(ctx context.Context, c db.Conn) ([]string, error) {
	return db.Names(ctx, c, `select s.name from sys.schemas s where not (`+userSchema+`) order by 1`)
}
