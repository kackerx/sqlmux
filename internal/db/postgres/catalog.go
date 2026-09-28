package postgres

import (
	"context"
	"encoding/json"
	"strconv"

	"sqlmux/internal/db"
)

// Catalog queries (§8.4), checked against lazysql's and pgtui's: pg_catalog
// over information_schema, which hides columns the user can't read and
// names arrays and enums only ARRAY and USER-DEFINED.

// userSchema keeps the schemas a user browses. `!~ '^pg_'`, not `NOT LIKE
// 'pg_%'`: `_` is a wildcard there and would drop pgx_data as well.
const userSchema = `n.nspname !~ '^pg_' and n.nspname <> 'information_schema'`

// Schemas lists the user's schemas by name, and the one unqualified names
// resolve to first ("" when search_path names none that exists).
func Schemas(ctx context.Context, c db.Conn) (schemas []string, current string, err error) {
	r, err := c.Query(ctx, `select n.nspname, coalesce(current_schema(), '') from pg_namespace n where `+userSchema+` order by 1`)
	if err != nil {
		return nil, "", err
	}
	for _, row := range r.Rows {
		schemas, current = append(schemas, row[0].S), row[1].S
	}
	return schemas, current, nil
}

// Tables lists every schema's tables, views, materialized views, foreign
// tables and partitioned tables, by schema and name. Partitions are left
// out: their parent stands for them.
func Tables(ctx context.Context, c db.Conn) ([]db.Table, error) {
	r, err := c.Query(ctx, `
select n.nspname, c.relname, c.reltuples, c.relkind
from pg_class c join pg_namespace n on n.oid = c.relnamespace
where c.relkind in ('r', 'p', 'v', 'm', 'f') and not c.relispartition and `+userSchema+`
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

// TableColumns reads one table's columns, primary key and the unique
// indexes that can stand in for it (§10.1).
func TableColumns(ctx context.Context, c db.Conn, schema, table string) (db.Columns, error) {
	var out db.Columns
	// json_agg, not array_agg: JSON is a text form the standard library parses.
	r, err := c.Query(ctx, `
select a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
       coalesce(pg_get_expr(d.adbin, d.adrelid), ''),
       (select json_agg(e.enumlabel order by e.enumsortorder) from pg_enum e where e.enumtypid = a.atttypid)
from pg_attribute a
join pg_class c on c.oid = a.attrelid
join pg_namespace n on n.oid = c.relnamespace
left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
where n.nspname = $1 and c.relname = $2 and a.attnum > 0 and not a.attisdropped
order by a.attnum`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	for _, row := range r.Rows {
		col := db.Column{Name: row[0].S, Type: row[1].S, NotNull: row[2].S == "t", Default: row[3].S}
		if !row[4].Null {
			if err := json.Unmarshal([]byte(row[4].S), &col.Enum); err != nil {
				return out, err
			}
		}
		out.Cols = append(out.Cols, col)
	}

	// Each key as a JSON array of column names in key order: the primary key
	// by conkey, then, oldest first, the unique indexes whose key columns
	// (INCLUDE ones don't count) are all not null and plain columns (0 is an
	// expression), with no predicate.
	r, err = c.Query(ctx, `
select con.contype, (
    select json_agg(a.attname order by k.n)
    from unnest(con.conkey) with ordinality k(attnum, n)
    join pg_attribute a on a.attrelid = con.conrelid and a.attnum = k.attnum), con.oid
from pg_constraint con
join pg_class c on c.oid = con.conrelid
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = $1 and c.relname = $2 and con.contype = 'p'
union all
select 'u', (
    select json_agg(a.attname order by k.n)
    from unnest(i.indkey) with ordinality k(attnum, n)
    join pg_attribute a on a.attrelid = i.indrelid and a.attnum = k.attnum
    where k.n <= i.indnkeyatts), i.indexrelid
from pg_index i
join pg_class c on c.oid = i.indrelid
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = $1 and c.relname = $2
  and i.indisunique and not i.indisprimary and i.indisvalid and i.indpred is null and 0 <> all(i.indkey)
  and not exists (
    select from unnest(i.indkey) with ordinality k(attnum, n)
    join pg_attribute a on a.attrelid = i.indrelid and a.attnum = k.attnum
    where k.n <= i.indnkeyatts and not a.attnotnull)
order by 1, 3`, db.Val{S: schema}, db.Val{S: table})
	if err != nil {
		return out, err
	}
	for _, row := range r.Rows {
		var cols []string
		if err := json.Unmarshal([]byte(row[1].S), &cols); err != nil {
			return out, err
		}
		if row[0].S == "p" {
			out.PK = cols
		} else {
			out.Unique = append(out.Unique, cols)
		}
	}
	return out, nil
}
