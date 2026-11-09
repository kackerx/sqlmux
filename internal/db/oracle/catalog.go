package oracle

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

// Catalog queries (§8.4) over the ALL_* views, what the user can see without
// a DBA's privileges: a schema is a user, the one level under a connection
// (its PDB is the connection's); the users Oracle maintains are left out.

// userSchemas is the condition that the column before it is a schema of a
// user's own, not Oracle's.
const userSchemas = ` in (select username from all_users where oracle_maintained = 'N')`

// Schemas lists the schemas, and the one unqualified names go to.
func (Engine) Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, `select username, sys_context('USERENV', 'CURRENT_SCHEMA') from all_users where oracle_maintained = 'N' order by 1`)
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas, current = append(schemas, row[0].S), row[1].S
	}
	return schemas, current, nil
}

// SystemSchemas are the users Oracle maintains: SYS, SYSTEM, DVSYS and the rest.
func (Engine) SystemSchemas(ctx context.Context, c db.Conn) ([]string, error) {
	return db.Names(ctx, c, `select username from all_users where oracle_maintained = 'Y' order by 1`)
}

// Databases is none: a connection is to one database, its PDB (§8.4).
func (Engine) Databases(context.Context, db.Conn) ([]string, error) { return nil, nil }

// Tables lists the tables, views and materialized views by schema and name,
// the rows the statistics last counted (-1 for none); a dropped table in the
// recycle bin, an IOT's overflow and a materialized view's own table are out.
// The materialized views are left out of the tables here, not in the query:
// a NOT EXISTS over ALL_MVIEWS there takes the dictionary a second.
func (Engine) Tables(ctx context.Context, c db.Conn, only []string) ([]db.Table, error) {
	in := func(col string) string { return db.Scope(col+userSchemas, col, only, sqlkit.Oracle) }
	r, err := c.Query(ctx, `
select owner, table_name, nvl(num_rows, -1), 'r' from all_tables
where `+in("owner")+` and dropped = 'NO' and iot_type is null and secondary = 'N' and nested = 'NO'
union all select owner, view_name, -1, 'v' from all_views where `+in("owner")+`
union all select owner, mview_name, -1, 'm' from all_mviews where `+in("owner")+`
order by 1, 2, 4`)
	if err != nil {
		return nil, err
	}
	sizes := segments(ctx, c, only)
	mviews := map[[2]string]bool{}
	for _, row := range r.Rows {
		if row[3].S == "m" {
			mviews[[2]string{row[0].S, row[1].S}] = true
		}
	}
	var ts []db.Table
	for _, row := range r.Rows {
		if row[3].S == "r" && mviews[[2]string{row[0].S, row[1].S}] {
			continue
		}
		rows, err := strconv.ParseFloat(row[2].S, 64)
		if err != nil {
			return nil, err
		}
		ts = append(ts, db.Table{Schema: row[0].S, Name: row[1].S, Rows: rows, Kind: row[3].S, Bytes: sizes[[2]string{row[0].S, row[1].S}]})
	}
	return ts, nil
}

// segmentsWait is how long the tree waits on DBA_SEGMENTS: on a database of
// tens of thousands of schemas it takes minutes, and a size is no reason to
// keep the tables from showing.
var segmentsWait = 5 * time.Second

// segments is the bytes each table takes, by owner and name: the schemas of
// only's (all for nil) from DBA_SEGMENTS when the user may read it within
// segmentsWait, else the user's own from USER_SEGMENTS; a table of another
// schema then has none (§8.11).
func segments(ctx context.Context, c db.Conn, only []string) map[[2]string]int64 {
	out := map[[2]string]int64{}
	dctx, cancel := context.WithTimeout(ctx, segmentsWait)
	r, err := c.Query(dctx, `select owner, segment_name, sum(bytes) from dba_segments where segment_type like 'TABLE%'`+
		db.SchemaIn("owner", only, sqlkit.Oracle)+` group by owner, segment_name`)
	cancel()
	if err != nil {
		if r, err = c.Query(ctx, `select user, segment_name, sum(bytes) from user_segments where segment_type like 'TABLE%' group by segment_name`); err != nil {
			return out
		}
	}
	for _, row := range r.Rows {
		out[[2]string{row[0].S, row[1].S}] = db.BytesOf(row[2])
	}
	return out
}

// TableColumns reads a table's columns, its primary key, and its unique keys
// over NOT NULL columns, which can stand in for one (§10.1). A virtual column
// is the server's to make; an identity column ALWAYS generated takes no value
// in an INSERT; a LONG, a BLOB, a BFILE or an object of a type is not to edit
// in the grid. DEFAULT is for a row added: an UPDATE takes none here.
func (Engine) TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	r, err := c.Query(ctx, `
select c.column_name, c.data_type, c.data_precision, c.data_scale, c.char_length, c.char_used, c.data_length,
       c.nullable, c.virtual_column, nvl(i.generation_type, ''), c.data_default
from all_tab_cols c
left join all_tab_identity_cols i on i.owner = c.owner and i.table_name = c.table_name and i.column_name = c.column_name
where c.owner = :1 and c.table_name = :2 and c.hidden_column = 'NO'
order by c.column_id`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return db.Columns{}, err
	}
	if len(r.Rows) == 0 {
		return db.Columns{}, errors.New(i18n.T("找不到表 %s.%s", schema, table))
	}
	var cols db.Columns
	notNull := map[string]bool{}
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: typeOf(row[1:7]), NotNull: row[7].S == "N", AddOnly: true, Default: strings.TrimSpace(row[10].S)}
		notNull[col.Name] = col.NotNull
		switch {
		case row[8].S == "YES":
			col.Generated, col.ReadOnly = true, i18n.T("虚拟列由服务器生成")
		case row[9].S == "ALWAYS":
			col.Generated, col.ReadOnly = true, i18n.T("%s 列由服务器生成", "IDENTITY")
		case !editable(col.Type):
			col.ReadOnly = i18n.T("%s 类型的列不能在表格里改", col.Type)
		}
		cols.Cols = append(cols.Cols, col)
	}
	k, err := c.Query(ctx, `
select k.constraint_type, k.constraint_name, cc.column_name
from all_constraints k join all_cons_columns cc on cc.owner = k.owner and cc.constraint_name = k.constraint_name
where k.owner = :1 and k.table_name = :2 and k.constraint_type in ('P', 'U') and k.status = 'ENABLED'
order by k.constraint_type, k.constraint_name, cc.position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return db.Columns{}, err
	}
	var unique []string
	name := ""
	flush := func() {
		if len(unique) > 0 && !anyNullable(unique, notNull) {
			cols.Unique = append(cols.Unique, unique)
		}
		unique = nil
	}
	for _, row := range k.Rows {
		switch row[0].S {
		case "P":
			cols.PK = append(cols.PK, row[2].S)
		case "U":
			if row[1].S != name {
				flush()
				name = row[1].S
			}
			unique = append(unique, row[2].S)
		}
	}
	flush()
	return cols, nil
}

// anyNullable is whether one of the columns names may be NULL.
func anyNullable(names []string, notNull map[string]bool) bool {
	for _, n := range names {
		if !notNull[n] {
			return true
		}
	}
	return false
}

// typeOf is a column's type as Oracle writes it in a CREATE TABLE: NUMBER(10,2),
// VARCHAR2(100), RAW(4), TIMESTAMP(3) as it comes, from data_type, precision,
// scale, char_length, char_used and data_length.
func typeOf(f []db.Val) string {
	t := f[0].S
	switch t {
	case "NUMBER":
		switch {
		case f[1].Null && f[2].S == "0":
			return "INTEGER"
		case f[1].Null:
			return "NUMBER"
		case f[2].Null || f[2].S == "0":
			return "NUMBER(" + f[1].S + ")"
		}
		return "NUMBER(" + f[1].S + "," + f[2].S + ")"
	case "VARCHAR2", "NVARCHAR2", "CHAR", "NCHAR":
		unit := ""
		if f[4].S == "C" && (t == "VARCHAR2" || t == "CHAR") {
			unit = " CHAR"
		}
		return t + "(" + f[3].S + unit + ")"
	case "RAW":
		return "RAW(" + f[5].S + ")"
	}
	return t
}

// editable is whether a column of type typ takes its text back in the grid.
func editable(typ string) bool {
	switch {
	case typ == "LONG", typ == "LONG RAW", typ == "BLOB", typ == "BFILE", typ == "ROWID", typ == "UROWID",
		strings.Contains(typ, "."), typ == "XMLTYPE", typ == "SDO_GEOMETRY", typ == "ANYDATA":
		return false
	}
	return true
}

// ddl is the statement that makes object name of schema, as DBMS_METADATA
// writes it, without storage and with its ;.
func ddl(ctx context.Context, c db.Conn, kind, schema, name string) (string, error) {
	if _, err := c.Exec(ctx, `begin
  dbms_metadata.set_transform_param(dbms_metadata.session_transform, 'SEGMENT_ATTRIBUTES', false);
  dbms_metadata.set_transform_param(dbms_metadata.session_transform, 'SQLTERMINATOR', true);
end;`, 0); err != nil {
		return "", err
	}
	r, err := c.Query(ctx, `select dbms_metadata.get_ddl(:1, :2, :3) from dual`, db.Val{S: kind}, db.Val{S: name}, db.Val{S: schema})
	if err != nil || len(r.Rows) == 0 {
		return "", err
	}
	return strings.TrimSpace(r.Rows[0][0].S), nil
}

// DDL is a table's or a view's definition (§12「预览」).
func (Engine) DDL(ctx context.Context, c db.Conn, schema, name string) (string, error) {
	r, err := c.Query(ctx, `select object_type from all_objects where owner = :1 and object_name = :2 and object_type in ('TABLE', 'VIEW', 'MATERIALIZED VIEW') order by decode(object_type, 'MATERIALIZED VIEW', 0, 1)`,
		db.Val{S: schema}, db.Val{S: name})
	if err != nil {
		return "", err
	}
	kind := "TABLE"
	if len(r.Rows) > 0 {
		kind = strings.ReplaceAll(r.Rows[0][0].S, " ", "_")
	}
	return ddl(ctx, c, kind, schema, name)
}

// Indexes are a table's, each its columns (an expression's text for a
// function-based one) and the CREATE INDEX that makes it.
func (e Engine) Indexes(ctx context.Context, c db.Conn, schema, table string) ([]db.Index, error) {
	r, err := c.Query(ctx, `
select i.index_name, i.uniqueness, ic.column_name, nvl(ie.column_expression, '')
from all_indexes i
join all_ind_columns ic on ic.index_owner = i.owner and ic.index_name = i.index_name
left join all_ind_expressions ie on ie.index_owner = ic.index_owner and ie.index_name = ic.index_name and ie.column_position = ic.column_position
where i.table_owner = :1 and i.table_name = :2 and i.index_type <> 'LOB'
order by i.index_name, ic.column_position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	var out []db.Index
	for _, row := range r.Rows {
		col := row[2].S
		if row[3].S != "" {
			col = row[3].S
		} else {
			col = e.Quote(col)
		}
		if n := len(out); n == 0 || out[n-1].Name != row[0].S {
			out = append(out, db.Index{Name: row[0].S, Unique: row[1].S == "UNIQUE"})
		}
		out[len(out)-1].Cols = append(out[len(out)-1].Cols, col)
	}
	for i, ix := range out {
		unique := ""
		if ix.Unique {
			unique = "unique "
		}
		out[i].Def = "create " + unique + "index " + e.Quote(schema, ix.Name) + " on " + e.Quote(schema, table) + " (" + strings.Join(ix.Cols, ", ") + ");"
	}
	return out, nil
}

// Keys are a table's primary key and its foreign keys, each with the table and
// columns it points at: those read by the constraint pointed at, a second
// query, as joining ALL_CONSTRAINTS to itself takes the dictionary seconds.
func (Engine) Keys(ctx context.Context, c db.Conn, schema, table string) ([]db.Key, error) {
	r, err := c.Query(ctx, `
select k.constraint_name, k.constraint_type, cc.column_name, nvl(k.r_owner, ''), nvl(k.r_constraint_name, '')
from all_constraints k join all_cons_columns cc on cc.owner = k.owner and cc.constraint_name = k.constraint_name
where k.owner = :1 and k.table_name = :2 and k.constraint_type in ('P', 'R')
order by decode(k.constraint_type, 'P', 0, 1), k.constraint_name, cc.position`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return nil, err
	}
	var out []db.Key
	refs := map[string][2]string{} // a foreign key's name: the owner and name of the constraint it points at
	for _, row := range r.Rows {
		if n := len(out); n == 0 || out[n-1].Name != row[0].S {
			out = append(out, db.Key{Name: row[0].S})
			if row[1].S == "R" {
				refs[row[0].S] = [2]string{row[3].S, row[4].S}
			}
		}
		out[len(out)-1].Cols = append(out[len(out)-1].Cols, row[2].S)
	}
	for i, k := range out {
		ref, ok := refs[k.Name]
		if !ok {
			continue
		}
		rc, err := c.Query(ctx, `select table_name, column_name from all_cons_columns where owner = :1 and constraint_name = :2 order by position`,
			db.Val{S: ref[0]}, db.Val{S: ref[1]})
		if err != nil {
			return nil, err
		}
		out[i].Ref = &db.Ref{Schema: ref[0]}
		for _, row := range rc.Rows {
			out[i].Ref.Table, out[i].Ref.Cols = row[0].S, append(out[i].Ref.Cols, row[1].S)
		}
	}
	return out, nil
}

// Functions lists the schemas' functions, procedures and packages; ID is the
// object's type, which FunctionDef asks DBMS_METADATA by.
func (Engine) Functions(ctx context.Context, c db.Conn, only []string) ([]db.Function, error) {
	r, err := c.Query(ctx, `select owner, object_name, object_type from all_objects
where object_type in ('FUNCTION', 'PROCEDURE', 'PACKAGE') and `+db.Scope("owner"+userSchemas, "owner", only, sqlkit.Oracle)+` order by 1, 2`)
	if err != nil {
		return nil, err
	}
	out := make([]db.Function, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Function{Schema: row[0].S, Name: row[1].S, Args: strings.ToLower(row[2].S), ID: row[2].S}
	}
	return out, nil
}

func (Engine) FunctionDef(ctx context.Context, c db.Conn, f db.Function) (string, error) {
	return ddl(ctx, c, f.ID, f.Schema, f.Name)
}

// Sequences lists the schemas' sequences.
func (Engine) Sequences(ctx context.Context, c db.Conn, only []string) ([]db.Sequence, error) {
	r, err := c.Query(ctx, `select sequence_owner, sequence_name from all_sequences where `+db.Scope("sequence_owner"+userSchemas, "sequence_owner", only, sqlkit.Oracle)+` order by 1, 2`)
	if err != nil {
		return nil, err
	}
	out := make([]db.Sequence, len(r.Rows))
	for i, row := range r.Rows {
		out[i] = db.Sequence{Schema: row[0].S, Name: row[1].S}
	}
	return out, nil
}

// Quick runs sql, one statement, for the palette's quick SQL (§12): out of a
// transaction of the user's, in a READ ONLY one the server holds it to, always
// rolled back; the words keep a write from going at all. schema is where
// unqualified names go. The read gets the row limit §9.4 gives a query's, a
// row past maxRows.
func (e Engine) Quick(ctx context.Context, w *db.Worker, sql, schema string, maxRows int) (r db.Result, err error) {
	if sqlkit.IsWrite(sql, sqlkit.Oracle) {
		return db.Result{}, errors.New(i18n.T("快速 SQL 是只读的：这条是写语句"))
	}
	err = w.RunSafe(ctx, func(ctx context.Context, c db.Conn) error {
		if schema != "" {
			if err := db.Setup(c, func() error { return e.UseSchema(ctx, c, schema, "") }); err != nil {
				return err
			}
		}
		if db.TxStatus(c) == 'I' {
			if _, err := c.Query(ctx, "set transaction read only"); err != nil {
				return err
			}
			defer func() { _, _ = db.End(ctx, c, "rollback") }()
		}
		text, _ := sqlkit.AutoLimit(sql, sqlkit.Oracle, maxRows+1, 0)
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
	_ db.TxSaver  = (*Conn)(nil)
	_ db.Systemer = Engine{}
)
