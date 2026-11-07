package sqlkit

import "strings"

// Literal is s as a string literal of dialect d: in single quotes, a
// quote doubled; MySQL and ClickHouse also take a backslash as an escape, so it is
// doubled there (§10.6, F8.7). ponytail: MySQL's NO_BACKSLASH_ESCAPES mode
// reads the doubled backslash as two; ask the connection for sql_mode if
// that comes up.
func Literal(s string, d Dialect) string {
	if d == MySQL || d == ClickHouse {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
