package sqlkit

import "strings"

// keywords are the words Scan takes as Keyword, in PG and MySQL alike:
// lazysql's sqlKeywords (components/sql_lexer.go) and the common words of
// the two besides, and the first word of every statement in PG 17's docs,
// Reference, SQL Commands, which IsWrite goes by (§12「写语句」). Not all
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
		abort checkpoint close cluster comment copy deallocate discard import listen load move notify
		prepare reassign refresh reindex reset security start unlisten vacuum`) {
		keywords[w] = true
	}
}

// Common are the keywords completion offers besides the names of tables
// and columns: the common ones of lazysql's builtinKeywords
// (components/sql_completer.go), a few functions and clauses of two words
// among them.
var Common = []string{
	"select", "from", "where", "and", "or", "not", "in", "is", "null", "like", "between", "exists",
	"as", "on", "join", "left join", "using", "group by", "order by", "having", "limit", "offset",
	"union", "all", "distinct", "case", "when", "then", "else", "end", "with", "values", "explain", "show",
	"count", "sum", "avg", "min", "max", "coalesce", "cast", "asc", "desc",
}
