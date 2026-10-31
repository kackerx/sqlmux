package app

import (
	"image/color"
	"slices"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// kindsAt is the kind of each candidate sqlComplete gives for sql, by label.
func kindsAt(a *App, conn *Conn, sql string) map[string]candKind {
	c, _ := a.sqlComplete(conn, sql, len(sql), "public", map[tableID]bool{}, true)
	kinds := map[string]candKind{}
	if c != nil {
		for _, it := range c.items {
			kinds[it.label] = it.kind
		}
	}
	return kinds
}

// A candidate is what its kind says: the keyword, the table, the view, the
// column, the value, the snippet (F16.11).
func TestCandidateKinds(t *testing.T) {
	a, _ := inQuery(t, "", "")
	ordersCatalog(a)
	conn := a.sess.Conns[0]
	conn.Tables = append(conn.Tables, db.Table{Schema: "public", Name: "v_order", Kind: "v"})
	for _, tc := range []struct {
		sql, label string
		want       candKind
	}{
		{"sel", "select", kindKeyword},
		{"select * from t_o", "t_order", kindTable},
		{"select * from v_", "v_order", kindView},
		{"select * from t_order where stat", "status", kindColumn},
		{"select * from t_order where status = ", "pending", kindValue},
		{"update t_order set note = ", "null", kindKeyword},
		{"insert into t_order ", "(id, status, amount, pai…) values ()", kindSnippet},
	} {
		got, ok := kindsAt(a, conn, tc.sql)[tc.label]
		if !ok || got != tc.want {
			t.Errorf("%q: %s is kind %d (offered %v), want %d", tc.sql, tc.label, got, ok, tc.want)
		}
	}
}

// Each kind has its icon, in the color the code or the tree has for it, and a
// color a theme gives the icon wins; a kind of none has no icon (F16.11, §7.7).
func TestCandIcon(t *testing.T) {
	a := sized(160, 45, "nerd")
	th := a.theme
	for _, tc := range []struct {
		kind candKind
		icon string
		fg   color.Color
	}{
		{kindKeyword, "keyword", th.Keyword}, {kindValue, "value", th.String}, {kindSnippet, "snippet", th.Info},
		{kindTable, "table", th.Func}, {kindView, "view", th.Func}, {kindColumn, "column", th.Dim},
		{kindKey, "key", th.PK}, {kindCommand, "command", th.Func},
	} {
		for _, set := range []*ui.Icons{ui.NerdIcons, ui.ASCIIIcons} {
			a.icons = set
			got := a.candIcon(tc.kind)
			if want := set.Named(tc.icon); got.Text == "" || got.Text != want.Text || got.Fg != tc.fg {
				t.Errorf("kind %d: %+v, want %q in %v", tc.kind, got, want.Text, tc.fg)
			}
		}
	}
	if got := a.candIcon(kindNone); got.Text != "" {
		t.Errorf("none: %+v", got)
	}
	themed := ui.Icon{Text: "X", Fg: color.White}
	a.icons = &ui.Icons{Keyword: themed}
	if got := a.candIcon(kindKeyword); got != themed {
		t.Errorf("a theme's: %+v", got)
	}
}

// The completion box has the icon in front of the label, and is as wide as the
// longest row with it (F16.11).
func TestCompletionBoxHasIcons(t *testing.T) {
	a, c := inQuery(t, "", "")
	ordersCatalog(a)
	feed(t, a, "iselect * from t_o")
	if c.comp == nil {
		t.Fatal("no list")
	}
	v, box, _ := a.completeView(c.comp, uv.Pos(0, 0))
	for _, it := range v.Items {
		if it.Icon.Text == "" {
			t.Errorf("%q has no icon", it.Text)
		}
		if ui.Width(it.Icon.Text+" "+it.Text+"  "+it.Note)+4 > box.Dx() {
			t.Errorf("%q does not fit in %d", it.Text, box.Dx())
		}
	}
}

// A console's candidates are commands, keywords (the subcommands and the
// tokens), keys and fields (F16.11).
func TestRedisCandidateKinds(t *testing.T) {
	a, c := redisConsole(t, &execDB{}, someCommands(), "")
	conn := a.sess.Conns[0]
	conn.docs, conn.hints = someDocs(), map[string][]string{"keys\x00us": {"user:1"}, "fields\x00h": {"field1"}}
	conn.building = &keyIndex{} // being built: the completion goes on with the names it was told of
	defer func() {              // a server with no DOCS: the names COMMAND lists
		conn.docs = nil
		c.ed.Load("g")
		feed(t, a, "A")
		a.queryComplete(c, false)
		if c.comp == nil || c.comp.items[0].label != "GET" || c.comp.items[0].kind != kindCommand {
			t.Errorf("without DOCS: %+v", c.comp)
		}
		feed(t, a, "<Esc>")
	}()
	for _, tc := range []struct {
		typed, label string
		want         candKind
	}{
		{"se", "SET", kindCommand}, {"config s", "SET", kindKeyword}, {"set k v e", "EX", kindKeyword},
		{"get us", "user:1", kindKey}, {"hget h fi", "field1", kindColumn},
	} {
		c.ed.Load(tc.typed)
		feed(t, a, "A")
		a.queryComplete(c, false)
		i := -1
		if c.comp != nil {
			i = slices.IndexFunc(c.comp.items, func(it candidate) bool { return it.label == tc.label })
		}
		if i < 0 || c.comp.items[i].kind != tc.want {
			t.Errorf("%q: %s is not kind %d", tc.typed, tc.label, tc.want)
		}
		feed(t, a, "<Esc>")
	}
}

// The WHERE input's list: columns, keywords and a column's values (F16.11).
func TestWhereCandidateKinds(t *testing.T) {
	for _, tc := range []struct {
		typed, label string
		want         candKind
	}{
		{"stat", "status", kindColumn}, {"id > 5 an", "and", kindKeyword}, {"status = ", "pending", kindValue},
	} {
		a, tab, _ := withRecorder(t)
		feed(t, a, "/"+tc.typed)
		i := -1
		if tab.comp != nil {
			i = slices.IndexFunc(tab.comp.items, func(it candidate) bool { return it.label == tc.label })
		}
		if i < 0 || tab.comp.items[i].kind != tc.want {
			t.Errorf("%q: %s is not kind %d", tc.typed, tc.label, tc.want)
		}
	}
}

// What a value can be: now() and the time's words are values, NULL and DEFAULT
// keywords; a CTE is offered as a table (F16.11).
func TestValueAndCTECandidateKinds(t *testing.T) {
	col := db.Column{Name: "at", Type: "timestamp with time zone", Default: "now()"}
	for label, want := range map[string]candKind{"now()": kindValue, "current_timestamp": kindValue, "null": kindKeyword, "default": kindKeyword} {
		got := valueCandidates(col, sqlkit.ValueInsert, sqlkit.PG)
		i := slices.IndexFunc(got, func(cd candidate) bool { return cd.label == label })
		if i < 0 || got[i].kind != want {
			t.Errorf("%s: %v", label, got)
		}
	}
	a, _ := inQuery(t, "", "")
	ordersCatalog(a)
	if k := kindsAt(a, a.sess.Conns[0], "with cte as (select 1) select * from ct")["cte"]; k != kindTable {
		t.Errorf("a CTE is kind %d", k)
	}
}
