package sqlkit

import (
	"slices"
	"strings"
)

// keywords are the words Scan takes as Keyword, in PG and MySQL alike:
// lazysql's sqlKeywords (components/sql_lexer.go) and the common words of
// the two besides, and the first word of every statement in PG 17's docs,
// Reference, SQL Commands, and of MySQL 8.4's that write, which IsWrite
// goes by (§12「写语句」, §13). Not all
// either one reserves: PG's unreserved keywords (name, type, user) are
// column names as often.
var keywords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		add all alter analyze and any array as asc begin between both by call cascade case cast check
		coalesce collate column commit conflict constraint convert create cross current database
		declare default delete desc describe distinct do drop duplicate each else elseif end escape
		except exec execute exists explain false fetch filter first following for foreign from full
		function grant group groups having if ifnull ignore ilike in index inner insert intersect
		interval into is join key last lateral leading left like limit lock loop materialized merge
		natural next not nothing nowait null nulls of offset on only or order outer over partition
		preceding primary procedure range recursive references release rename replace return
		returning revoke right rollback row rows savepoint schema select set share show similar skip
		some table temp temporary then ties to top trailing transaction trigger true truncate
		unbounded union unique update use using values view when where while window with within
		abort analyse checkpoint close cluster comment copy deallocate discard import listen load move notify
		prepare reassign refresh reindex reset security start unlisten vacuum
		binlog change clone flush handler install kill optimize purge repair restart shutdown stop uninstall
		unlock xa`) {
		keywords[w] = true
	}
}

// common are the keywords completion offers besides the names of tables
// and columns: the common ones of lazysql's builtinKeywords
// (components/sql_completer.go), a few functions and clauses of two words
// among them, and the starts and clauses of DML and DDL (§9.7).
var common = strings.Fields(`select from where and or not in is null like between exists
	as on join using having limit offset union all distinct case when then else end with values explain show
	count sum avg min max coalesce cast convert asc desc
	intersect except recursive set update truncate begin commit rollback call
	unique check constraint default references cascade`)

// commonPairs are the keywords of more than one word, which Fields would split.
var commonPairs = []string{
	"left join", "inner join", "right join", "cross join", "natural join",
	"group by", "order by", "insert into", "delete from",
	"create table", "create index", "create unique index", "create view", "alter table",
	"drop table", "drop index", "drop view", "add column", "drop column", "rename to",
	"primary key", "foreign key", "if exists", "if not exists",
}

// Common is the keywords completion offers on a connection of dialect d:
// the shared ones, and what only PG (full join, returning, on conflict),
// only MySQL (ifnull, replace into, on duplicate key update), only ClickHouse
// (array join, prewhere, final) or only Trino has; SQLite's are PG's.
func Common(d Dialect) []string {
	out := append(slices.Clone(common), commonPairs...)
	switch d {
	case MySQL:
		return append(out, "ifnull", "replace into", "on duplicate key update", "use", "describe")
	case ClickHouse:
		return append(out, "full join", "array join", "prewhere", "final", "settings", "use", "describe")
	case Trino:
		return append(out, "full join", "use", "describe", "show catalogs", "show schemas", "show tables")
	}
	return append(out, "full join", "returning", "on conflict", "do nothing", "do update set") // SQLite's alike (F14.1)
}

// noSpace are the candidates that stay as they are as one is taken: values,
// which are often what a condition ends with, and functions, which take a
// ( next. The other keywords get a space after (§9.7).
var noSpace = map[string]bool{
	"null": true, "true": true, "false": true, "is null": true, "is not null": true,
	"count": true, "sum": true, "avg": true, "min": true, "max": true,
	"coalesce": true, "ifnull": true, "cast": true, "convert": true,
}

// SpaceAfter reports whether taking keyword k adds a space after it.
func SpaceAfter(k string) bool { return !noSpace[k] }
