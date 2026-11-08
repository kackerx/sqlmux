package sqlkit

import (
	"sort"
	"strconv"
	"strings"
)

// Stmt is a statement, s[Start:End]: from its first token that is not a
// blank or a comment to its last, without the ; that ends it. From Lead
// the cursor is in it (§9.2): the comment lines right above it count.
type Stmt struct{ Start, End, Lead int }

// Statements splits s at each ; outside parentheses (§9.2), the way usql's
// stmt and psql's lexer do; empty statements are dropped. A MSSQL text is
// cut at a GO of its own line first (§9.2, F18.6): the GO, its count with
// it, goes to no statement, none of it to the server.
// A SQLite trigger's body is one statement, its ;s not cutting it: CREATE
// [TEMP] TRIGGER … BEGIN, the statements, each ended by a ;, and END, as
// SQLite's own tokenizer, complete.c's sqlite3_complete, tracks it (F14.1).
// ponytail: PG 14's BEGIN ATOMIC … END is cut at the ; in its body, as
// psql tracks and this does not; split such a function by hand. So is a
// MySQL procedure's, trigger's or event's BEGIN … END, a T-SQL procedure's
// AS BEGIN … END, and DELIMITER, the mysql client's, goes to the server as
// it is (§9.2).
func Statements(s string, d Dialect) []Stmt {
	all := Scan(s, d)
	ts, depth := meaningful(s, all)
	var out []Stmt
	start := -1
	var words []string                // the first words of the statement being read, and what a trigger's body is
	body, prev, skip := false, "", -1 // skip is the GO line still to be read past
	first := 3                        // how many first words are kept: a trigger's three, PL/SQL's five
	if d == Oracle {
		first = 5
	}
	for i, t := range ts {
		w := strings.ToLower(s[t.Start:t.End])
		if start < 0 {
			words, body = words[:0], false
		}
		if d == Oracle && w == "/" && depth[i] == 0 && slashLine(ts, i) { // SQL*Plus's end of a block, or of any statement (§8.11)
			if start >= 0 {
				out = append(out, Stmt{Start: start, End: ts[i-1].End})
			}
			start = -1
			continue
		}
		if d == MSSQL && w == "go" && depth[i] == 0 && goLine(ts, i) {
			if start >= 0 {
				out = append(out, Stmt{Start: start, End: ts[i-1].End})
			}
			start, skip = -1, t.Line
			continue
		}
		if t.Line <= skip {
			continue
		}
		if len(words) < first && (t.Kind == Keyword || t.Kind == Ident) {
			words = append(words, w)
			if d == Oracle && !body && plsql(words) { // its ;s are the block's, till the /
				body = true
			}
		}
		if d == SQLite && depth[i] == 0 {
			switch {
			case w == "begin" && isTrigger(words):
				body = true
			case w == "end" && body && prev == ";":
				body = false
			}
		}
		prev = w
		if t.Kind == Punct && s[t.Start] == ';' && depth[i] == 0 && !body {
			if start >= 0 {
				out = append(out, Stmt{Start: start, End: ts[i-1].End})
			}
			start = -1
			continue
		}
		if start < 0 {
			start = t.Start
		}
	}
	if start >= 0 {
		out = append(out, Stmt{Start: start, End: ts[len(ts)-1].End})
	}
	if d == MySQL {
		execMarks(s, all, out)
	}
	// the lines code is on, and those comments are on
	lines := []int{0}
	for i := range len(s) {
		if s[i] == '\n' {
			lines = append(lines, i+1)
		}
	}
	code, comment := make([]bool, len(lines)), make([]bool, len(lines))
	for _, t := range all {
		for l := t.Line; t.Kind != Space && l <= t.Line+strings.Count(s[t.Start:t.End], "\n"); l++ {
			code[l] = code[l] || t.Kind != Comment
			comment[l] = comment[l] || t.Kind == Comment
		}
	}
	for i := range out {
		st := &out[i]
		st.Lead = st.Start
		l := sort.SearchInts(lines, st.Start+1) - 1
		if strings.TrimSpace(s[lines[l]:st.Start]) != "" {
			continue
		}
		for l > 0 && comment[l-1] && !code[l-1] {
			l--
			st.Lead = lines[l]
		}
	}
	return out
}

// isTrigger is whether the first words of a statement say it makes a trigger:
// CREATE TRIGGER, CREATE TEMP TRIGGER, CREATE TEMPORARY TRIGGER.
func isTrigger(words []string) bool {
	if len(words) < 2 || words[0] != "create" {
		return false
	}
	return words[1] == "trigger" || len(words) == 3 && (words[1] == "temp" || words[1] == "temporary") && words[2] == "trigger"
}

// plsql is whether the first words of an Oracle statement start a PL/SQL
// block, whose ;s are its own: BEGIN, DECLARE, and CREATE [OR REPLACE]
// [EDITIONABLE | NONEDITIONABLE] of a procedure, function, package,
// trigger, type or library.
func plsql(words []string) bool {
	if words[0] == "begin" || words[0] == "declare" {
		return true
	}
	if words[0] != "create" {
		return false
	}
	for _, w := range words[1:] {
		switch w {
		case "or", "replace", "editionable", "noneditionable":
		case "procedure", "function", "package", "trigger", "type", "library":
			return true
		default:
			return false
		}
	}
	return false
}

// slashLine is whether the / at ts[i] is all of its line, as SQL*Plus's end of
// a block is.
func slashLine(ts []Token, i int) bool {
	return (i == 0 || ts[i-1].Line != ts[i].Line) && (i+1 >= len(ts) || ts[i+1].Line != ts[i].Line)
}

// goLine is whether the GO at ts[i] has a line of its own: no meaningful
// token before it on the line, and at most its count after (§9.2). The GO
// of `select 1 go`, on the code's line, stays where it is, a name or an
// error as the server has it.
func goLine(ts []Token, i int) bool {
	if i > 0 && ts[i-1].Line == ts[i].Line {
		return false
	}
	if i+1 < len(ts) && ts[i+1].Line == ts[i].Line {
		return ts[i+1].Kind == Number && (i+2 >= len(ts) || ts[i+2].Line != ts[i].Line)
	}
	return true
}

// execMarks widens stmts over the /*! before them and the */ after them,
// comments to Scan, blanks and other comments between: cut off, the other
// one of the pair left in would open or close a comment, and the server
// keeps a version in /*!.
func execMarks(s string, all []Token, stmts []Stmt) {
	for i := range stmts {
		st := &stmts[i]
		j := sort.Search(len(all), func(j int) bool { return all[j].Start >= st.Start })
		for j--; j >= 0 && (all[j].Kind == Space || all[j].Kind == Comment); j-- {
			if strings.HasPrefix(s[all[j].Start:all[j].End], "/*!") {
				st.Start = all[j].Start
			}
		}
		j = sort.Search(len(all), func(j int) bool { return all[j].Start >= st.End })
		for ; j < len(all) && (all[j].Kind == Space || all[j].Kind == Comment); j++ {
			if s[all[j].Start:all[j].End] == "*/" {
				st.End = all[j].End
			}
		}
	}
}

// StmtAt is the index of the statement the cursor at pos in s is in (§9.2):
// one that starts on its line, the last there that starts by pos; else the
// last one it is past the Lead of, so a blank line after a statement is in
// it; the first when pos is before them all. -1 when there are none.
func StmtAt(s string, stmts []Stmt, pos int) int {
	from := strings.LastIndexByte(s[:pos], '\n') + 1
	to := len(s)
	if i := strings.IndexByte(s[pos:], '\n'); i >= 0 {
		to = pos + i
	}
	at := -1
	for i, st := range stmts {
		if st.Start >= from && st.Start < to && (at < 0 || st.Start <= pos) {
			at = i
		}
	}
	if at >= 0 || len(stmts) == 0 {
		return at
	}
	i := 0
	for i+1 < len(stmts) && stmts[i+1].Lead <= pos {
		i++
	}
	return i
}

// HasSemicolon is whether s has a ; outside strings and comments: a WHERE
// condition may not end its statement (§9.6).
func HasSemicolon(s string, d Dialect) bool {
	for _, t := range Scan(s, d) {
		if t.Kind == Punct && s[t.Start] == ';' {
			return true
		}
	}
	return false
}

// lower is token i's text in lower case, "" past the ends.
func lower(s string, ts []Token, i int) string {
	if i < 0 || i >= len(ts) {
		return ""
	}
	return strings.ToLower(s[ts[i].Start:ts[i].End])
}

// FirstWord is statement s's first word past opening parentheses, lower
// case: what it does, select or create; "" when it has none.
func FirstWord(s string, d Dialect) string {
	ts, _ := words(s, d)
	return lower(s, ts, first(s, ts))
}

// first is the index of the first token past opening parentheses.
func first(s string, ts []Token) int {
	i := 0
	for lower(s, ts, i) == "(" {
		i++
	}
	return i
}

// IsRead is whether statement s only reads (§9.3): its first word, past
// opening parentheses, is select, show, explain, table, values, desc or
// describe, but a select … into makes a table, a row lock (for update,
// for share, lock in share mode) writes, a with that has insert, update,
// delete or merge anywhere in it writes, and so does explain analyze of a
// statement that writes. For hints only: the database guards the data
// (§13).
func IsRead(s string, d Dialect) bool {
	ts, depth := words(s, d)
	return isRead(s, ts, depth)
}

// IsWrite is whether s is a statement IsRead does not take whose first
// word, past opening parentheses, is a keyword — or, on SQL Server, a name
// where the EXEC would be (§9.2, F18.6): what quick SQL keeps from the
// database (§12「写语句」). A word mistyped, selec, is none: the database's
// read-only transaction answers it with the syntax error.
func IsWrite(s string, d Dialect) bool {
	ts, depth := words(s, d)
	i := first(s, ts)
	return i < len(ts) && (ts[i].Kind == Keyword || BeginsBareExec(s, d)) && !isRead(s, ts, depth)
}

// BeginsBareExec is whether statement s starts as a procedure call that
// goes without its EXEC does (§9.2, F18.6): a name first, where a keyword
// would be, which T-SQL takes for one. It may write, it may answer with
// rows, and the words treat it as the EXEC it is: a write, a dangerous
// one, and one the read branch runs. A save's first word is no keyword of
// ours, and its transaction is none to run a procedure.
func BeginsBareExec(s string, d Dialect) bool {
	if d != MSSQL {
		return false
	}
	ts, _ := words(s, d)
	i := first(s, ts)
	return i < len(ts) && ts[i].Kind == Ident && lower(s, ts, i) != "save"
}

// sessionWords start what IsWrite takes but a read-only connection still
// runs (§13): transaction control and session settings, which leave the
// database's read only standing, and MySQL's USE; set, begin and start
// BreaksReadOnly looks into. MySQL's RESET is the server's, a write.
var sessionWords = map[string]bool{
	"commit": true, "end": true, "rollback": true, "abort": true,
	"savepoint": true, "release": true, "reset": true, "discard": true, "use": true,
}

// scopes are the words before what a SET sets, PG's and MySQL's, and in
// MySQL's @@scope.name.
var scopes = map[string]bool{"session": true, "local": true, "global": true, "persist": true, "persist_only": true}

// BreaksReadOnly is whether a read-only connection keeps statement s from
// the database (§13): a write, transaction control and session settings
// aside, or one that turns read only off, as PG 17 and MySQL 8.4 do it
// (TestReadOnlyUnlocks of postgres and mysql): SET [scope] TRANSACTION,
// SET SESSION CHARACTERISTICS, SET [scope]
// default_transaction_read_only or transaction_read_only, MySQL's
// @@[scope.]transaction_read_only among those a SET of its sets, BEGIN or
// START TRANSACTION … READ WRITE. On MySQL so is whatever a SET sets for
// the whole server, GLOBAL, PERSIST, PERSIST_ONLY or @@global., as PG's
// ALTER SYSTEM.
// ponytail: select set_config('default_transaction_read_only', 'off',
// false) reads, and turns it off, SET U&"…" names the setting in escapes
// this does not read, and after NO_BACKSLASH_ESCAPES MySQL's strings end
// where Scan's do not; read only guards against slips, not users (§13)
func BreaksReadOnly(s string, d Dialect) bool {
	ts, depth := words(s, d)
	i := first(s, ts)
	if d == SQLite && lower(s, ts, i) == "pragma" { // one that gives query_only a value, the read only standing (F14.1)
		for j := i + 1; j+1 < len(ts); j++ {
			if next := lower(s, ts, j+1); unquote(lower(s, ts, j)) == "query_only" && (next == "=" || next == "(") {
				return true
			}
		}
		return false
	}
	if BeginsBareExec(s, d) { // the EXEC that is not written (§9.2, F18.6)
		return true
	}
	if i >= len(ts) || ts[i].Kind != Keyword || isRead(s, ts, depth) {
		return false
	}
	switch w := lower(s, ts, i); w {
	case "set":
		if d == MSSQL { // no read only of the server's to undo, and SET TRANSACTION ISOLATION LEVEL reads (F18.6)
			return false
		}
		for j := i + 1; j < len(ts); j++ { // MySQL's SET sets a list, by commas
			scope := ""
			for scopes[lower(s, ts, j)] { // PG takes SET SESSION SESSION …
				scope, j = lower(s, ts, j), j+1
			}
			if lower(s, ts, j) == "@@" {
				if j++; scopes[lower(s, ts, j)] && lower(s, ts, j+1) == "." {
					scope, j = lower(s, ts, j), j+2
				}
			}
			if d == MySQL && scope != "" && scope != "session" && scope != "local" {
				return true
			}
			switch unquote(lower(s, ts, j)) { // the names go by any case
			case "transaction", "characteristics", "default_transaction_read_only", "transaction_read_only":
				return true
			}
			if d != MySQL {
				return false
			}
			for j < len(ts) && !(lower(s, ts, j) == "," && depth[j] == depth[i]) {
				j++
			}
		}
		return false
	case "begin", "start":
		if d == Oracle && w == "begin" { // a PL/SQL block: whatever it does
			return true
		}
		for j := i + 1; j < len(ts); j++ {
			if lower(s, ts, j) == "read" && lower(s, ts, j+1) == "write" {
				return true
			}
		}
		return false
	default:
		return !sessionWords[w] || d == MySQL && w == "reset"
	}
}

func isRead(s string, ts []Token, depth []int) bool {
	i := first(s, ts)
	switch lower(s, ts, i) {
	case "show", "desc", "describe", "table", "values", "check": // MySQL's CHECK TABLE
		return true
	case "explain":
		return explainRead(s, ts[i+1:], depth[i+1:])
	case "select", "with":
	default:
		return false
	}
	with := lower(s, ts, i) == "with"
	for j := i; j < len(ts); j++ {
		switch lower(s, ts, j) {
		case "into": // wherever it is: PG takes it nowhere but in the first select
			return false
		case "insert", "update", "delete", "merge":
			if with {
				return false
			}
		case "for": // for update, for no key update, for share, for key share
			if k := lower(s, ts, j+1); k == "update" || k == "share" || k == "no" || k == "key" {
				return false
			}
		case "lock": // MySQL's lock in share mode
			if lower(s, ts, j+1) == "in" && lower(s, ts, j+2) == "share" && lower(s, ts, j+3) == "mode" {
				return false
			}
		}
	}
	return true
}

// explainRead is whether EXPLAIN reads, ts following it: ANALYZE runs the
// statement, so it reads as that does; in (ANALYZE false) and the like it
// is off.
func explainRead(s string, ts []Token, depth []int) bool {
	analyze, i := explainOptions(s, ts)
	if !analyze || i >= len(ts) {
		return true
	}
	return isRead(s, ts[i:], depth[i:])
}

// explainOptions is whether the options after EXPLAIN, ts following it,
// turn ANALYZE on, and where the statement explained begins.
func explainOptions(s string, ts []Token) (analyze bool, i int) {
	for {
		switch lower(s, ts, i) {
		case "analyze", "analyse":
			analyze, i = true, i+1
			continue
		case "verbose":
			i++
			continue
		case "(":
			for i++; i < len(ts) && lower(s, ts, i) != ")"; i++ {
				if w := lower(s, ts, i); w == "analyze" || w == "analyse" {
					v := lower(s, ts, i+1)
					analyze = v != "false" && v != "off" && v != "0"
				}
			}
			i++
			continue
		}
		return analyze, i
	}
}

// queryWords start a query: what AutoLimit limits and quick SQL declares a
// cursor for (§9.4, §12), as PG 16 psql's is_select_command (common.c, for
// FETCH_COUNT; PG 17 dropped it) took select and values; table and with
// are added, a WITH … SELECT over a big table being common.
var queryWords = map[string]bool{"select": true, "with": true, "table": true, "values": true}

// IsQuery is whether statement s starts as a query does (queryWords), past
// opening parentheses.
func IsQuery(s string, d Dialect) bool { return queryWords[FirstWord(s, d)] }

// AutoLimit is s with LIMIT n after it (§9.4), and OFFSET offset when that is
// above 0, when it is a query that reads, select, with, table or values,
// with no LIMIT or FETCH at its top level; a ; at its end goes. When the
// whole query is in parentheses, or the one after a WITH list is, its top
// level is in them. The LIMIT is on a line of its own, so a -- comment at
// the end does not take it in. pageable is whether the LIMIT was put there
// and an OFFSET of its own is not in the way: a statement that is, can be
// asked for the next page with another offset (F10.4).
func AutoLimit(s string, d Dialect, n, offset int) (out string, pageable bool) {
	ts, depth := words(s, d)
	if !isRead(s, ts, depth) {
		return s, false
	}
	if w := lower(s, ts, first(s, ts)); !queryWords[w] || d == SQLite && w == "values" { // SQLite's takes no LIMIT after VALUES
		return s, false
	}
	out = s
	if last := ts[len(ts)-1]; s[last.Start:last.End] == ";" {
		out, ts = s[:last.Start]+s[last.End:], ts[:len(ts)-1]
	}
	lo, hi, top := 0, len(ts)-1, 0
	for hi > lo && lower(s, ts, hi) == ")" {
		j := hi - 1
		for j >= lo && depth[j] != depth[hi] { // back to its (
			j--
		}
		if j < lo || lower(s, ts, j) != "(" ||
			j != lo && !(lower(s, ts, lo) == "with" && lower(s, ts, j-1) == ")" && depth[j-1] == depth[j]) {
			break
		}
		lo, hi, top = j+1, hi-1, depth[j]+1
	}
	pageable = true
	var orders bool          // a top-level ORDER BY, which a T-SQL OFFSET must have
	var distinct, setop bool // a top-level DISTINCT or set operator, whose select list an ORDER BY (SELECT NULL) is not in
	var topw, forout bool    // a TOP clause, a FOR JSON or XML, which an OFFSET cannot stand in
	tail := -1               // ClickHouse's top-level SETTINGS or FORMAT, which a LIMIT goes before (§8.9)
	for j := lo; j <= hi; j++ {
		switch w := lower(s, ts, j); {
		case depth[j] != top:
		case d == ClickHouse && tail < 0 && (w == "settings" || w == "format"):
			tail = j
		case w == "limit" || w == "fetch":
			return s, false
		case w == "offset":
			pageable = false
		case w == "order" && j < hi && lower(s, ts, j+1) == "by":
			orders = true
		case w == "top":
			topw = true
		case w == "for" && j < hi && (lower(s, ts, j+1) == "json" || lower(s, ts, j+1) == "xml"):
			forout = true
		case w == "distinct":
			distinct = true
		case w == "union" || w == "intersect" || w == "except":
			setop = true
		}
	}
	if d == MSSQL {
		// what an OFFSET cannot stand in, the statement runs as it is, the
		// rows capped (§9.4, F18.6): a TOP's (10741), a FOR JSON's or FOR
		// XML's (a syntax error); and a DISTINCT's or a set operation's
		// without an ORDER BY of its own (145 and 104, the select list's)
		if !pageable || topw || forout || (distinct || setop) && !orders {
			return s, false
		}
		suffix := "\nOFFSET " + strconv.Itoa(offset) + " ROWS FETCH NEXT " + strconv.Itoa(n) + " ROWS ONLY"
		if !orders {
			suffix = "\nORDER BY (SELECT NULL)" + suffix
		}
		return out + suffix, pageable
	}
	if d == Oracle { // 12c's row limiting, which takes no ORDER BY of its own (§8.11)
		if !pageable {
			return s, false
		}
		suffix := "\nFETCH FIRST " + strconv.Itoa(n) + " ROWS ONLY"
		if offset > 0 {
			suffix = "\nOFFSET " + strconv.Itoa(offset) + " ROWS FETCH NEXT " + strconv.Itoa(n) + " ROWS ONLY"
		}
		return out + suffix, pageable
	}
	limit := "\nLIMIT " + strconv.Itoa(n)
	switch {
	case d == Trino && offset > 0: // its OFFSET goes before the LIMIT (§8.10)
		limit = "\nOFFSET " + strconv.Itoa(offset) + " LIMIT " + strconv.Itoa(n)
	case offset > 0:
		limit += " OFFSET " + strconv.Itoa(offset)
	}
	if tail >= 0 { // out is s up to its ; still, which comes after the SETTINGS or FORMAT
		at := ts[tail].Start
		return out[:at] + strings.TrimLeft(limit, "\n") + "\n" + out[at:], pageable
	}
	return out + limit, pageable
}

// NoBegin is whether statement s goes without the BEGIN the manual
// transaction mode puts before one out of a transaction, as psql's
// command_no_begin (src/bin/psql/common.c, PG 17) has it: transaction
// control but savepoints, and what PG runs in no transaction block,
// VACUUM, CLUSTER alone, CREATE / DROP DATABASE and TABLESPACE, the
// CONCURRENTLY ones, ALTER SYSTEM, DISCARD ALL; SQLite's, PRAGMA, ATTACH and
// DETACH besides. MySQL's (§8.2):
// transaction control, what commits the transaction (commitsMySQL), and
// SET TRANSACTION with no scope, which a transaction refuses (1568).
func NoBegin(s string, d Dialect) bool {
	ts, _ := words(s, d)
	w := func(i int) string { return lower(s, ts, i) }
	if d == MySQL {
		switch w(0) {
		case "begin", "start", "commit", "rollback":
			return true
		}
		return w(0) == "set" && w(1) == "transaction" || commitsMySQL(s, ts)
	}
	if d == SQLite && (w(0) == "pragma" || w(0) == "attach" || w(0) == "detach") { // none works in a transaction, or means what it says there (foreign_keys, journal_mode)
		return true
	}
	switch w(0) {
	case "abort", "begin", "start", "commit", "end", "rollback", "vacuum":
		return true
	case "prepare":
		return w(1) == "transaction"
	case "cluster": // with no arguments
		return w(1) == "" || w(1) == ";"
	case "create":
		i := 1
		switch w(1) {
		case "database", "tablespace":
			return true
		case "unique":
			i++
		}
		return w(i) == "index" && w(i+1) == "concurrently"
	case "alter":
		return w(1) == "system"
	case "drop", "reindex":
		switch w(1) {
		case "database", "system", "tablespace":
			return true
		case "index", "table":
			return w(2) == "concurrently"
		}
	case "discard":
		return w(1) == "all"
	}
	return false
}

// MySQLTx is where a MySQL session's transaction is after statement s ran
// in it, tx before, the server telling no client (§8.2): 'T' after BEGIN,
// START TRANSACTION, and COMMIT or ROLLBACK … AND CHAIN; 'I' after COMMIT,
// ROLLBACK but to a savepoint, and what commits it (commitsMySQL); tx
// else.
// ponytail: a COMMIT in a procedure CALL runs, SET autocommit = 0 and XA
// go by unseen
func MySQLTx(s string, tx byte) byte {
	ts, _ := words(s, MySQL)
	w := func(i int) string { return lower(s, ts, i) }
	switch w(0) {
	case "begin":
		return 'T'
	case "start":
		if w(1) == "transaction" {
			return 'T'
		}
	case "commit", "rollback":
		switch _, to, chain := txEnding(s, ts); {
		case to:
			return tx
		case chain:
			return 'T'
		}
		return 'I'
	}
	if commitsMySQL(s, ts) {
		return 'I'
	}
	return tx
}

// MSSQLTx is where a SQL Server session's transaction is after statement s
// ran in it, tx before, the server telling no client (§8.2): 'T' after a
// BEGIN, 'I' after a COMMIT or a ROLLBACK of its own, one that names a
// savepoint or a nested transaction leaving it as it was.
// ponytail: SET IMPLICIT_TRANSACTIONS ON, and a COMMIT of a procedure's,
// go by unseen
func MSSQLTx(s string, tx byte) byte {
	ts, _ := words(s, MSSQL)
	w := func(i int) string { return lower(s, ts, i) }
	switch w(0) {
	case "begin":
		return 'T'
	case "commit", "rollback":
		i := 1
		if w(i) == "work" || w(i) == "tran" || w(i) == "transaction" {
			i++
		}
		if w(i) != "" && w(i) != ";" { // a savepoint's or a nested transaction's name
			return tx
		}
		return 'I'
	}
	return tx
}

// txEnding reads a COMMIT or a ROLLBACK at the start of s, END and ABORT
// too, PG's names for them: which one it is ("commit" or "rollback", "" for
// neither), and whether it goes to a savepoint (to), which leaves the
// transaction going, or chains a new one (chain).
func txEnding(s string, ts []Token) (kind string, to, chain bool) {
	w := func(i int) string { return lower(s, ts, i) }
	switch w(0) {
	case "commit", "end":
		kind = "commit"
	case "rollback", "abort":
		kind = "rollback"
	default:
		return "", false, false
	}
	i := 1
	if w(i) == "work" || w(i) == "transaction" {
		i++
	}
	return kind, w(i) == "to", w(i) == "and" && w(i+1) == "chain"
}

// EndsTx is "commit" or "rollback" for a statement that ends the
// transaction it runs in, as the user types it (COMMIT [WORK], END, ROLLBACK
// [WORK], ABORT), "" for any other: ROLLBACK TO a savepoint ends none.
func EndsTx(s string, d Dialect) string {
	ts, _ := words(s, d)
	if kind, to, _ := txEnding(s, ts); !to {
		return kind
	}
	return ""
}

// commitsMySQL is whether statement ts of s commits the transaction it
// runs in, as MySQL 8.4's manual lists them (Statements That Cause an
// Implicit Commit): DDL but CREATE / DROP TEMPORARY, those on accounts,
// BEGIN, START, LOCK / UNLOCK TABLES, SET PASSWORD, SET autocommit = 1,
// the administrative ANALYZE, CACHE INDEX, CHECK, FLUSH, LOAD INDEX,
// OPTIMIZE, REPAIR, RESET but RESET PERSIST, replication control.
// ponytail: ALTER / CREATE / DROP of whatever object counts; UNLOCK
// TABLES commits after LOCK TABLES alone, LOAD DATA for NDB only
func commitsMySQL(s string, ts []Token) bool {
	w := func(i int) string { return lower(s, ts, i) }
	switch w(0) {
	case "create", "drop":
		return w(1) != "temporary"
	case "alter", "rename", "truncate", "grant", "revoke", "install", "uninstall", "begin", "start",
		"analyze", "cache", "check", "flush", "optimize", "repair", "change", "stop":
		return true
	case "lock", "unlock":
		return w(1) == "tables" || w(1) == "table"
	case "load":
		return w(1) == "index"
	case "reset":
		return w(1) != "persist"
	case "set":
		if w(1) == "password" {
			return true
		}
		for j := range ts { // autocommit = 1 among what it sets
			if w(j) == "autocommit" && (w(j+1) == "=" || w(j+1) == ":=") {
				if v := w(j + 2); v == "1" || v == "on" || v == "true" {
					return true
				}
			}
		}
	}
	return false
}
