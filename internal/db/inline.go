package db

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Inline is sql with each $n, where it is code and not in a string, an identifier or
// a comment (sqlkit.Scan tells), written as the literal of args[n-1]: a string in the
// dialect's quotes, NULL for a null.
func Inline(sql string, args []Val, d sqlkit.Dialect) (string, error) {
	var b strings.Builder
	toks, last := sqlkit.Scan(sql, d), 0
	for i, t := range toks { // $ is an operator of its own, the number right after it (no blank is a token between) the parameter's
		if t.Kind != sqlkit.Op || sql[t.Start:t.End] != "$" || i+1 == len(toks) || toks[i+1].Kind != sqlkit.Number {
			continue
		}
		num := toks[i+1]
		n, err := strconv.Atoi(sql[num.Start:num.End])
		if err != nil || n < 1 || n > len(args) {
			return "", fmt.Errorf(i18n.T("$%d 没有对应的参数"), n)
		}
		b.WriteString(sql[last:t.Start])
		if a := args[n-1]; a.Null {
			b.WriteString("NULL")
		} else {
			b.WriteString(sqlkit.Literal(a.S, d))
		}
		last = num.End
	}
	b.WriteString(sql[last:])
	return b.String(), nil
}
