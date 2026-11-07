package clickhouse

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Catalog queries (§8.4) over system.*: a ClickHouse database is the schema
// level, as MySQL's is; there is no level above it.

// systemDBs are the server's own databases, which the tree leaves out.
const systemDBs = `name not in ('system', 'INFORMATION_SCHEMA', 'information_schema')`

// Schemas lists the databases, and the one the connection is in.
func (Engine) Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, `select name, currentDatabase() from system.databases where `+systemDBs+` order by name`)
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas, current = append(schemas, row[0].S), row[1].S
	}
	return schemas, current, nil
}

// Databases is none: the databases are the schemas (§8.4).
func (Engine) Databases(context.Context, db.Conn) ([]string, error) { return nil, nil }

// Tables lists every database's tables and views, the rows the server counts
// for a MergeTree's parts, none for a view; the inner tables of materialized
// views, whose names start with a dot, are left out.
func (Engine) Tables(ctx context.Context, c db.Conn, only []string) ([]db.Table, error) {
	r, err := c.Query(ctx, `
select database, name, ifNull(toInt64(total_rows), -1), engine
from system.tables
where not is_temporary and not startsWith(name, '.') and database `+systemIn+db.SchemaIn("database", only, sqlkit.ClickHouse)+`
order by database, name`)
	if err != nil {
		return nil, err
	}
	ts := make([]db.Table, len(r.Rows))
	for i, row := range r.Rows {
		rows, err := strconv.ParseFloat(row[2].S, 64)
		if err != nil {
			return nil, err
		}
		kind := "r"
		switch row[3].S {
		case "View", "LiveView", "WindowView":
			kind = "v"
		case "MaterializedView":
			kind = "m"
		}
		ts[i] = db.Table{Schema: row[0].S, Name: row[1].S, Rows: rows, Kind: kind}
	}
	return ts, nil
}

// systemIn is the condition that the column after it is no system database's.
const systemIn = `not in ('system', 'INFORMATION_SCHEMA', 'information_schema')`

// TableColumns reads a table's columns and its primary key, the row identity
// of a save (§10.1): ClickHouse does not hold a key's rows apart, so a save
// counts the rows each change would hit first (§10.3). A table that is not a
// MergeTree's, or whose key is no list of its columns, is not to edit; nor is
// a column of the sorting or partition key, which ALTER … UPDATE refuses, one
// the server makes (MATERIALIZED, ALIAS), or one of a type that no text casts
// to (Map, Tuple, JSON).
func (Engine) TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	t, err := c.Query(ctx, `select engine, primary_key from system.tables where database = $1 and name = $2`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return db.Columns{}, err
	}
	if len(t.Rows) == 0 {
		return db.Columns{}, errors.New(i18n.T("找不到表 %s.%s", schema, table))
	}
	engine, key := t.Rows[0][0].S, t.Rows[0][1].S
	r, err := c.Query(ctx, `
select name, type, default_kind, default_expression, is_in_sorting_key or is_in_partition_key or is_in_primary_key
from system.columns where database = $1 and table = $2 order by position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return db.Columns{}, err
	}
	var cols db.Columns
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: row[1].S, AddOnly: true, NotNull: !strings.HasPrefix(row[1].S, "Nullable(")}
		col.Enum = enumLabels(row[1].S)
		switch row[2].S {
		case "DEFAULT":
			col.Default = row[3].S
		case "MATERIALIZED", "ALIAS", "EPHEMERAL":
			col.Generated, col.ReadOnly = true, i18n.T("%s 列由服务器生成", row[2].S)
		}
		switch {
		case col.ReadOnly != "":
		case row[4].S == "1" || row[4].S == "true":
			col.ReadOnly = i18n.T("排序键、分区键的列 ClickHouse 不能改")
		case !castable(row[1].S):
			col.ReadOnly = i18n.T("%s 类型的列不能在表格里改", row[1].S)
		}
		cols.Cols = append(cols.Cols, col)
	}
	switch {
	case !strings.HasSuffix(engine, "MergeTree"):
		cols.ReadOnly = i18n.T("ClickHouse 的 %s 引擎不支持修改行", engine)
	default:
		cols.PK = keyColumns(key, cols.Cols)
	}
	return cols, nil
}

// keyColumns is the columns of a primary key's expression, "id, user_id"; nil
// when there is none, or a part of it is no column (toDate(at)).
func keyColumns(key string, cols []db.Column) []string {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	var names []string
	for _, part := range strings.Split(key, ",") {
		name := strings.Trim(strings.TrimSpace(part), "`")
		found := false
		for _, c := range cols {
			found = found || c.Name == name
		}
		if !found {
			return nil
		}
		names = append(names, name)
	}
	return names
}

// castable is whether a value of type typ can be set from its text: what
// CAST from a String takes. Map and Tuple it does not, nor JSON's kinds.
func castable(typ string) bool {
	t := Unwrap(typ)
	for _, no := range []string{"Map(", "Tuple(", "Nested(", "JSON", "Object(", "Variant(", "Dynamic", "AggregateFunction(", "SimpleAggregateFunction("} {
		if strings.HasPrefix(t, no) || strings.Contains(t, "("+no) || strings.Contains(t, " "+no) {
			return false
		}
	}
	return true
}

// Unwrap is typ without the Nullable and LowCardinality around it: what the
// values are, as the grid classes them (§7.6).
func Unwrap(typ string) string {
	for {
		inner, ok := strings.CutPrefix(typ, "Nullable(")
		if !ok {
			inner, ok = strings.CutPrefix(typ, "LowCardinality(")
		}
		if !ok {
			return typ
		}
		typ = strings.TrimSuffix(inner, ")")
	}
}

// enumLabels is the labels of an Enum8 or Enum16 in the order of their
// numbers, as it sorts them: Enum8('pending' = 1, 'running' = 2) (§10.2).
func enumLabels(typ string) []string {
	t := Unwrap(typ)
	if !strings.HasPrefix(t, "Enum8(") && !strings.HasPrefix(t, "Enum16(") {
		return nil
	}
	var labels []string
	for _, tok := range sqlkit.Scan(t, sqlkit.ClickHouse) {
		if tok.Kind == sqlkit.String {
			labels = append(labels, unescape(strings.ReplaceAll(t[tok.Start+1:tok.End-1], "''", "'")))
		}
	}
	return labels
}

// DDL is the statement that makes the table or view, as the server writes it.
func (e Engine) DDL(ctx context.Context, c db.Conn, schema, name string) (string, error) {
	r, err := c.Query(ctx, "show create table "+e.Quote(schema, name))
	if err != nil {
		return "", err
	}
	if len(r.Rows) == 0 {
		return "", nil
	}
	return r.Rows[0][0].S + ";", nil
}

// Indexes are a table's data-skipping indexes, each its expression and the
// INDEX clause that makes it; ClickHouse has no unique ones.
func (Engine) Indexes(ctx context.Context, c db.Conn, schema, table string) ([]db.Index, error) {
	r, err := c.Query(ctx, `select name, expr, type_full, granularity from system.data_skipping_indices where database = $1 and table = $2 order by name`,
		db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	var out []db.Index
	for _, row := range r.Rows {
		out = append(out, db.Index{Name: row[0].S, Cols: []string{row[1].S},
			Def: "INDEX " + row[0].S + " " + row[1].S + " TYPE " + row[2].S + " GRANULARITY " + row[3].S})
	}
	return out, nil
}

// Keys is the primary key, its expression's columns; ClickHouse has no
// foreign keys.
func (e Engine) Keys(ctx context.Context, c db.Conn, schema, table string) ([]db.Key, error) {
	r, err := c.Query(ctx, `select primary_key from system.tables where database = $1 and name = $2`, db.Val{S: schema}, db.Val{S: table})
	if err != nil || len(r.Rows) == 0 || r.Rows[0][0].S == "" {
		return nil, err
	}
	var cols []string
	for _, p := range strings.Split(r.Rows[0][0].S, ",") {
		cols = append(cols, strings.TrimSpace(p))
	}
	return []db.Key{{Name: "PRIMARY KEY", Cols: cols}}, nil
}

// Functions, Sequences and FunctionDef are none: a ClickHouse function the
// user made belongs to no database, and there are no sequences.
func (Engine) Functions(context.Context, db.Conn, []string) ([]db.Function, error) { return nil, nil }
func (Engine) Sequences(context.Context, db.Conn, []string) ([]db.Sequence, error) { return nil, nil }
func (Engine) FunctionDef(context.Context, db.Conn, db.Function) (string, error)   { return "", nil }

// Quick runs sql, one statement, for the palette's quick SQL (§12): on the
// read-only connection it is given, so a write is the server's to refuse;
// the words keep one from going at all, as SQL Server's do. schema is the
// database it runs in. The read gets the LIMIT §9.4 gives a query's, a row
// past maxRows.
func (e Engine) Quick(ctx context.Context, w *db.Worker, sql, schema string, maxRows int) (r db.Result, err error) {
	if sqlkit.IsWrite(sql, sqlkit.ClickHouse) {
		return db.Result{}, errors.New(i18n.T("快速 SQL 是只读的：这条是写语句"))
	}
	err = w.RunSafe(ctx, func(ctx context.Context, c db.Conn) error {
		if schema != "" {
			if err := db.Setup(c, func() error { return e.UseSchema(ctx, c, schema, "") }); err != nil {
				return err
			}
		}
		text, _ := sqlkit.AutoLimit(sql, sqlkit.ClickHouse, maxRows+1, 0)
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
