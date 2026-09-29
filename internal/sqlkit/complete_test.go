package sqlkit

import (
	"reflect"
	"strings"
	"testing"
)

// | is the cursor. Cases after lazysql's components/sql_context_test.go.
func TestCompletionContext(t *testing.T) {
	for _, c := range []struct {
		sql          string
		ok           bool
		kind         CompKind
		prefix, qual string
		depth        int
		tables       []TableRef
		ctes         []string
	}{
		{"select * from t_o|", true, CompTables, "t_o", "", 0, []TableRef{{Name: "t_o"}}, nil},
		{"select * from public.t_o|", true, CompColumns, "t_o", "public", 0, []TableRef{{Schema: "public", Name: "t_o"}}, nil},
		{"select o.| from t_order o", true, CompColumns, "", "o", 0, []TableRef{{Name: "t_order", Alias: "o"}}, nil},
		{"select * from t_order as o join t_user u on u.na|", true, CompColumns, "na", "u", 0, []TableRef{{Name: "t_order", Alias: "o"}, {Name: "t_user", Alias: "u"}}, nil},
		{"select * from a, b|", true, CompTables, "b", "", 0, []TableRef{{Name: "a"}, {Name: "b"}}, nil},
		{"select a, b|", true, CompAny, "b", "", 0, nil, nil},
		{"select * from t where st|", true, CompAny, "st", "", 0, []TableRef{{Name: "t"}}, nil},
		{"update t_o| set a = 1", true, CompTables, "t_o", "", 0, []TableRef{{Name: "t_o"}}, nil},
		{"insert into t (a) values (|", true, CompAny, "", "", 1, []TableRef{{Name: "t"}}, nil},
		{"with recent as (select * from t_order), b as (select 1) select * from rec|", true, CompTables, "rec", "", 0, []TableRef{{Name: "t_order", Depth: 1}, {Name: "rec"}}, []string{"recent", "b"}},
		{"select * from t1 where id in (select id from t2 where x|)", true, CompAny, "x", "", 1, []TableRef{{Name: "t1"}, {Name: "t2", Depth: 1}}, nil},
		{"select 1 from a;\nselect * from b where |", true, CompAny, "", "", 0, []TableRef{{Name: "b"}}, nil},
		{`select "Odd".| from "Odd"`, true, CompColumns, "", "Odd", 0, []TableRef{{Name: "Odd"}}, nil},
		{"select 'ab|c'", false, 0, "", "", 0, nil, nil},
		{"select 1 -- ab|", false, 0, "", "", 0, nil, nil},
		{"select /* a| */ 1", false, 0, "", "", 0, nil, nil},
	} {
		pos := strings.Index(c.sql, "|")
		sql := c.sql[:pos] + c.sql[pos+1:]
		got := CompletionContext(sql, pos, PG)
		if !c.ok {
			if got.OK {
				t.Errorf("%s: completes", c.sql)
			}
			continue
		}
		want := Completion{OK: true, Kind: c.kind, Prefix: c.prefix, Start: pos - len(c.prefix), Qualifier: c.qual, Depth: c.depth, Tables: c.tables, CTEs: c.ctes}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.sql, got, want)
		}
	}
}
