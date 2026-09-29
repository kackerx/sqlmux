package sqlkit

import "testing"

func TestWhereContext(t *testing.T) {
	for _, c := range []struct {
		in          string // | is the cursor
		prefix, col string
		ok          bool
	}{
		{"sta|", "sta", "", true},
		{"id > 5 and st|", "st", "", true},
		{"|", "", "", true},
		{"status = |", "", "status", true},
		{"status = 'do|", "'do", "status", true},
		{"status <> d|", "d", "status", true},
		{"paid = tr|", "tr", "paid", true},
		{"status in (|", "", "status", true},
		{"status IN ('done', 'fa|", "'fa", "status", true},
		{"status in ('done', |", "", "status", true},
		{"status in ('don''t', |", "", "status", true},
		{"note like 'a|", "", "", false}, // a string, but not a column's value
		{"x = 1 -- sta|", "", "", false}, // in a comment
		{`"weird col|`, "", "", false},   // a quoted identifier
		{"a = 1 and b = |", "", "b", true},
		{"f(x) = |", "", "", true}, // not a plain column
		{"id >= |", "", "", true},
		{"名前 = |", "", "名前", true},
	} {
		pos := len(c.in) - len("|")
		for i := range c.in {
			if c.in[i] == '|' {
				pos = i
			}
		}
		s := c.in[:pos] + c.in[pos+1:]
		w := WhereContext(s, pos)
		if w.Prefix != c.prefix || w.Column != c.col || w.OK != c.ok || w.OK && w.Start != pos-len(c.prefix) {
			t.Errorf("%q: %+v, want prefix %q column %q ok %v", c.in, w, c.prefix, c.col, c.ok)
		}
	}
}

func TestSelectLike(t *testing.T) {
	for s, want := range map[string]bool{
		"select 1": true, "  SELECT 1": true, "values (1)": true, "table t_order": true,
		"with x as (select 1) select * from x":                 true,
		"-- note\n/* why */ ((select 1) union all (select 2))": true,
		"show search_path": false, "explain select 1": false, "delete from t": false,
		"selectx 1": false, `"select"`: false, "'select'": false, "": false, "(": false,
	} {
		if got := SelectLike(s); got != want {
			t.Errorf("SelectLike(%q) = %v", s, got)
		}
	}
}
