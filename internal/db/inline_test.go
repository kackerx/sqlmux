package db

import (
	"testing"

	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// $n goes in as a literal where it is code, not in a string, an identifier or a comment.
func TestInline(t *testing.T) {
	for _, c := range []struct {
		sql, want string
		args      []Val
	}{
		{"select $1, $2::text", "select 'a', NULL::text", []Val{{S: "a"}, {Null: true}}},
		{"select '$1', \"c$1\" -- $1\n, $1 /* $2 */", "select '$1', \"c$1\" -- $1\n, 'a' /* $2 */", []Val{{S: "a"}}},
		{"select 'it''s $1', $1", "select 'it''s $1', 'x'", []Val{{S: "x"}}},
		{"select $10, $1", "select 'j', 'a'", []Val{{S: "a"}, {}, {}, {}, {}, {}, {}, {}, {}, {S: "j"}}},
		{"select $$x$$, $1", "select $$x$$, 'a'", []Val{{S: "a"}}}, // a $$ is no parameter's
		{"select $q$ $1 $q$, $$ $1 $$, $1", "select $q$ $1 $q$, $$ $1 $$, 'a'", []Val{{S: "a"}}},
		{`select E'it\'s $1', $1`, `select E'it\'s $1', 'a'`, []Val{{S: "a"}}},
		{"select $ 1, $1", "select $ 1, 'a'", []Val{{S: "a"}}},                               // a $ and a number apart are two things
		{"select /* a /* $1 */ $1 */ $1", "select /* a /* $1 */ $1 */ 'a'", []Val{{S: "a"}}}, // PG's comments nest
	} {
		got, err := Inline(c.sql, c.args, sqlkit.PG)
		if err != nil || got != c.want {
			t.Errorf("%q: %q %v, want %q", c.sql, got, err, c.want)
		}
	}
	for _, sql := range []string{"select $2", "select $0"} {
		if _, err := Inline(sql, []Val{{S: "a"}}, sqlkit.PG); err == nil {
			t.Errorf("%s: a parameter with no argument", sql)
		}
	}
}
