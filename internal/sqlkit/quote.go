package sqlkit

import "strings"

// reserved are the words each dialect's grammar does not take as a column's or
// a table's name, as what its own catalog says: PG's pg_get_keywords()'s
// categories R and T (PG 17), MySQL 8.4's information_schema.KEYWORDS with
// RESERVED = 1, SQLite's keyword table (sqlite.org/lang_keywords.html), all of
// which it takes quoted. Not sqlkit's keyword table: that one is what
// highlights, and lacks some of these (MySQL's rank, SQLite's raise) and holds
// others that are names (MySQL's comment, first).
var reserved = map[Dialect]map[string]bool{
	PG: wordSet(`
		all analyse analyze and any array as asc asymmetric both case cast check collate column constraint
		create current_catalog current_date current_role current_time current_timestamp current_user default
		deferrable desc distinct do else end except false fetch for foreign from grant group having in
		initially intersect into lateral leading limit localtime localtimestamp not null offset on only or
		order placing primary references returning select session_user some symmetric system_user table then
		to trailing true union unique user using variadic when where window with authorization binary
		collation concurrently cross current_schema freeze full ilike inner is isnull join left like natural
		notnull outer overlaps right similar tablesample verbose`),
	MySQL: wordSet(`
		accessible add all alter analyze and as asc asensitive before between bigint binary blob both by call
		cascade case change char character check collate column condition constraint continue convert create
		cross cube cume_dist current_date current_time current_timestamp current_user cursor database
		databases day_hour day_microsecond day_minute day_second dec decimal declare default delayed delete
		dense_rank desc describe deterministic distinct distinctrow div double drop dual each else elseif
		empty enclosed escaped except exists exit explain false fetch first_value float float4 float8 for
		force foreign from fulltext function generated get grant group grouping groups having high_priority
		hour_microsecond hour_minute hour_second if ignore in index infile inner inout insensitive insert int
		int1 int2 int3 int4 int8 integer intersect interval into io_after_gtids io_before_gtids is iterate
		join json_table key keys kill lag last_value lateral lead leading leave left like limit linear lines
		load localtime localtimestamp lock long longblob longtext loop low_priority match maxvalue mediumblob
		mediumint mediumtext middleint minute_microsecond minute_second mod modifies natural
		no_write_to_binlog not nth_value ntile null numeric of on optimize optimizer_costs option optionally
		or order out outer outfile over partition percent_rank precision primary procedure purge qualify range
		rank read read_write reads real recursive references regexp release rename repeat replace require
		resignal restrict return revoke right rlike row row_number rows schema schemas second_microsecond
		select sensitive separator set show signal smallint spatial specific sql sql_big_result
		sql_calc_found_rows sql_small_result sqlexception sqlstate sqlwarning ssl starting stored
		straight_join system table tablesample terminated then tinyblob tinyint tinytext to trailing trigger
		true undo union unique unlock unsigned update usage use using utc_date utc_time utc_timestamp values
		varbinary varchar varcharacter varying virtual when where while window with write xor year_month
		zerofill`),
	SQLite: wordSet(`
		abort action add after all alter always analyze and as asc attach autoincrement before begin between
		by cascade case cast check collate column commit conflict constraint create cross current current_date
		current_time current_timestamp database default deferrable deferred delete desc detach distinct do
		drop each else end escape except exclude exclusive exists explain fail filter first following for
		foreign from full generated glob group groups having if ignore immediate in index indexed initially
		inner insert instead intersect into is isnull join key last left like limit match materialized natural
		no not nothing notnull null nulls of offset on or order others outer over partition plan pragma
		preceding primary query raise range recursive references regexp reindex release rename replace
		restrict returning right rollback row rows savepoint select set table temp temporary then ties to
		transaction trigger unbounded union unique update using vacuum values view virtual when where window
		with without`),
	MSSQL: wordSet(`
		add all alter and any as asc authorization backup begin between break browse bulk by cascade case
		check checkpoint close clustered coalesce collate column commit compute constraint contains
		containstable continue convert create cross current current_date current_time current_timestamp
		current_user cursor database dbcc deallocate declare default delete deny desc disk distinct
		distributed double drop dump else end errlvl escape except exec execute exists exit external fetch
		file fillfactor for foreign freetext freetexttable from full function goto grant group having
		holdlock identity identitycol identity_insert if in index inner insert intersect into is join key
		kill left like lineno load merge national nocheck nonclustered not null nullif of off offsets on
		open opendatasource openquery openrowset openxml option or order outer over percent pivot plan
		precision primary print proc procedure public raiserror read readtext reconfigure references
		replication restore restrict return revert revoke right rollback rowcount rowguidcol rule save
		schema securityaudit select semantickeyphrasetable semanticsimilaritydetailstable
		semanticsimilaritytable session_user set setuser shutdown some statistics system_user table
		tablesample textsize then to top tran transaction trigger truncate try_convert tsequal union
		unique unpivot update updatetext use user values varying view waitfor when where while with
		within writetext`),
}

// ClickHouse reserves little: a name may be almost any word, but these
// read as the clause they start where a name would go, so they are quoted.
func init() {
	reserved[ClickHouse] = wordSet(`
		all and any array as asc between by case cross desc distinct else end except exists final
		format from full global group having in inner intersect interval into is join left like
		limit not null offset on or order outer prewhere right sample select settings table then
		union using when where with`)
	// Trino's reserved keywords (trino.io/docs/current/language/reserved.html, 478)
	reserved[Trino] = wordSet(`
		alter and as between by case cast constraint create cross cube current_catalog current_date
		current_path current_role current_schema current_time current_timestamp current_user
		deallocate delete describe distinct drop else end escape except execute exists extract false
		for from full group grouping having in inner insert intersect into is join json_array
		json_exists json_object json_query json_table json_value left like listagg localtime
		localtimestamp natural normalize not null on or order outer prepare recursive right rollup
		select skip table then trim true uescape union unnest using values when where with`)
}

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// NeedsQuote is whether the name has to be quoted to be read as itself in d
// (§9.7, F15.11): one that is not a plain word, one PG folds to lower case, a
// word the dialect reserves.
func (d Dialect) NeedsQuote(name string) bool {
	if name == "" || !identStart(rune(name[0])) {
		return true
	}
	for i := range len(name) {
		if !IdentByte(name[i]) || d == PG && name[i] >= 'A' && name[i] <= 'Z' {
			return true
		}
	}
	return reserved[d][strings.ToLower(name)]
}
