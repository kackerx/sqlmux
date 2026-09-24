package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// inTree is sized with the sidebar focused.
func inTree(w, h int) *App {
	a := sized(w, h, "nerd")
	a.win().focus(0)
	return a
}

// cursorTable is the table under the tree's cursor.
func cursorTable(a *App) string {
	ts, ms := a.treeTables()
	return ts[ms[a.win().tree.cursor].Index].Name
}

func TestTreeMoves(t *testing.T) {
	a := inTree(160, 18) // 11 of the 14 tables show
	for _, c := range []struct{ keys, want string }{
		{"j", "agent_version"}, {"3j", "mt_task_log"}, {"k", "mt_task"},
		{"G", "t_user_profile"}, {"j", "t_user_profile"}, {"gg", "agent"}, {"k", "agent"},
	} {
		feed(t, a, c.keys)
		if got := cursorTable(a); got != c.want {
			t.Errorf("%s: cursor on %s, want %s", c.keys, got, c.want)
		}
	}
	feed(t, a, "G")
	if top := a.win().tree.top; top != 14-11 {
		t.Errorf("G: top %d, the cursor's row must show", top)
	}
}

// ↵ and t open the cursor's table where ↵ would land and move focus there
// (§7.8); the tree marks it as open.
func TestTreeOpens(t *testing.T) {
	a := inTree(160, 45)
	data := a.win().Root.Leaves()[0]
	feed(t, a, "jj<CR>")
	if tabNames(data) != "goal" || a.win().Focus != data.ID {
		t.Fatalf("↵: tabs %v, focus %d", tabNames(data), a.win().Focus)
	}
	feed(t, a, "<C-h>jt")
	if tabNames(data) != "goal mt_task" || data.Cur != 1 {
		t.Fatalf("t: tabs %v cur %d", tabNames(data), data.Cur)
	}
	a.win().focus(0)
	f := a.render()
	th := f.Theme
	for _, c := range []struct {
		name string
		fg   uv.Style
	}{{"mt_task  ", uv.Style{Fg: th.Focus}}, {"goal  ", uv.Style{Fg: th.Fg}}} { // two spaces: not the data pane's title
		if st := styleOf(t, f, c.name); st.Fg != c.fg.Fg {
			t.Errorf("%s: fg %v, want %v", c.name, st.Fg, c.fg.Fg)
		}
	}
	if st := styleOf(t, f, "mt_task  "); st.Bg != th.Select {
		t.Errorf("the cursor row, focused: bg %v, want select", st.Bg)
	}
	a.win().focus(data.ID)
	if st := styleOf(t, a.render(), "mt_task  "); st.Bg != th.Row {
		t.Errorf("the cursor row, blurred: bg %v, want row", st.Bg)
	}
}

// styleOf is the style of the first cell of s on the screen.
func styleOf(t *testing.T, f *ui.Frame, s string) uv.Style {
	t.Helper()
	for y, line := range strings.Split(f.String(), "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return f.Buf.CellAt(ui.Width(line[:i]), y).Style
		}
	}
	t.Fatalf("no %q on screen", s)
	return uv.Style{}
}

// A click opens the table in the current tab, a middle click in a new one.
func TestTreeMouse(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.win().Root.Leaves()[0]
	r := find(t, a, ui.Target{Kind: ui.KindTable, Pane: 0, I: 2})
	click(a, r.Min)
	if tabNames(data) != "goal" || a.win().Focus != data.ID {
		t.Fatalf("click: tabs %v, focus %d", tabNames(data), a.win().Focus)
	}
	a.View()
	a.Update(tea.MouseClickMsg{X: r.Min.X, Y: r.Min.Y + 1, Button: tea.MouseMiddle})
	if tabNames(data) != "goal mt_task" {
		t.Fatalf("middle click: tabs %v", tabNames(data))
	}
	a.Update(tea.MouseMotionMsg{X: r.Min.X + 3, Y: r.Min.Y + 2})
	if st := styleOf(t, a.render(), "mt_task_log "); st.Bg != a.theme.Row {
		t.Errorf("hover: bg %v, want row", st.Bg)
	}
}

// The live filter (§7.8): INSERT while typing, matches kept in name order
// and lit, ↵ keeps it for the list, esc clears it where the cursor was.
func TestTreeFilter(t *testing.T) {
	a := inTree(160, 45)
	feed(t, a, "/tor")
	names := func() string {
		ts, ms := a.treeTables()
		var out []string
		for _, m := range ms {
			out = append(out, ts[m.Index].Name)
		}
		return strings.Join(out, " ")
	}
	if a.mode() != keymap.Insert || names() != "t_order t_order_item" {
		t.Fatalf("typing: mode %v, list %s", a.mode(), names())
	}
	f := a.render()
	if row := strings.Split(f.String(), "\n")[1]; !strings.Contains(row, " tor") || !strings.Contains(row, " 2/14 │") {
		t.Errorf("filter row %q", row)
	}
	if f.Cursor == nil || f.Cursor.Y != 1 {
		t.Errorf("the terminal cursor sits in the filter: %v", f.Cursor)
	}
	if st := styleOf(t, f, "order "); st.Bg != a.theme.Warn { // "t_" didn't match, "or" did
		t.Errorf("match highlight: %v", st.Bg)
	}
	feed(t, a, "<CR>j")
	if a.mode() != keymap.Normal || names() != "t_order t_order_item" || cursorTable(a) != "t_order_item" {
		t.Fatalf("after ↵: mode %v, list %s, cursor %s", a.mode(), names(), cursorTable(a))
	}
	feed(t, a, "/<Esc>")
	if a.win().tree.filter.Text != "" || a.mode() != keymap.Normal || cursorTable(a) != "t_order_item" {
		t.Fatalf("after esc: filter %q, mode %v, cursor %s", a.win().tree.filter.Text, a.mode(), cursorTable(a))
	}
	feed(t, a, "/x<C-l>")
	if a.mode() != keymap.Insert {
		t.Error("NORMAL keys are text in the filter")
	}
	click(a, uv.Pos(100, 10)) // another pane takes focus, and the filter row the keys
	if a.win().tree.filtering || a.win().tree.filter.Text != "x" {
		t.Errorf("leaving the tree: filtering %v, filter %q", a.win().tree.filtering, a.win().tree.filter.Text)
	}
}

// The schema dropdown (§8.6): typing filters, C-n / C-p move, ↵ switches
// the tree to its first table with no filter, esc and outside clicks close.
func TestSchemaMenu(t *testing.T) {
	a := inTree(160, 45)
	feed(t, a, "3j/ord<CR>gs")
	if a.drop == nil || a.mode() != keymap.Command || a.dropView().Items[a.drop.sel] != "public" {
		t.Fatalf("gs: menu %+v mode %v", a.drop, a.mode())
	}
	f := a.render()
	if st := styleOf(t, f, "public   "); st.Fg != a.theme.PK || st.Bg != a.theme.Select {
		t.Errorf("the current schema: %+v", st)
	}
	feed(t, a, "<C-p><CR>")
	top := strings.Split(a.render().String(), "\n")[0]
	if a.drop != nil || a.sess.Schema != "agentable" || !strings.Contains(top, " agentable ▾") {
		t.Fatalf("↵: schema %q, title %q", a.sess.Schema, top)
	}
	if tr := a.win().tree; tr.cursor != 0 || tr.filter.Text != "" || cursorTable(a) != "planner" {
		t.Errorf("the tree starts over: %+v", tr)
	}

	feed(t, a, "gspub")
	if items := a.dropView().Items; len(items) != 1 || items[0] != "public" {
		t.Fatalf("filtered: %v", items)
	}
	feed(t, a, "<Esc>")
	if a.drop != nil || a.sess.Schema != "agentable" {
		t.Fatal("esc closes, nothing picked")
	}

	r := find(t, a, ui.Target{Kind: ui.KindHint, Action: "tree.schema"})
	click(a, r.Min)
	if a.drop == nil {
		t.Fatal("clicking the title opens the dropdown")
	}
	row := find(t, a, ui.Target{Kind: ui.KindRow, I: 1})
	if box, _ := a.dropBox(a.dropView()); row.Min.Y != box.Min.Y+4 || box.Min.X != r.Min.X || box.Max.X != a.sidebarRect().Max.X {
		t.Errorf("the dropdown opens under the title, left aligned, its right edge on the sidebar's: %v", box)
	}
	click(a, row.Min)
	if a.drop != nil || a.sess.Schema != "public" {
		t.Fatalf("click: schema %q", a.sess.Schema)
	}
	feed(t, a, "gs")
	click(a, uv.Pos(100, 30))
	if a.drop != nil {
		t.Error("a click outside closes it")
	}
}

// The first load picks current_schema(), else the first; a failed one says
// why in a toast (§7.8).
func TestCatalogLoad(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.sess.Schema = ""
	a.Update(catalogMsg{schemas: []string{"a", "b"}, current: "b", tables: []db.Table{{Schema: "b", Name: "x"}}})
	if a.sess.Schema != "b" || len(a.sess.Tables) != 1 {
		t.Fatalf("schema %q tables %v", a.sess.Schema, a.sess.Tables)
	}
	a.Update(catalogMsg{schemas: []string{"a"}})
	if a.sess.Schema != "a" {
		t.Errorf("gone from the catalog: schema %q, want the first", a.sess.Schema)
	}
	a.Update(catalogMsg{err: errors.New("permission denied")})
	if a.toast != "permission denied" || a.sess.Schema != "a" {
		t.Errorf("a failed load: toast %q", a.toast)
	}
}

// The palette lists every schema's tables, the tree's first (§12).
func TestPaletteTablesFromCatalog(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>@")
	names := namesOf(a)
	if len(names) != 15 || names[0] != "表:agent" || names[14] != "表:planner" {
		t.Fatalf("tables: %v", names)
	}
	if p := a.paletteView().Rows[14]; p.Where != "doraemon.agentable" {
		t.Errorf("where %q", p.Where)
	}
	feed(t, a, "plan<CR>")
	if data := a.win().Root.Leaves()[0]; tabNames(data) != "planner" {
		t.Errorf("opened %v", tabNames(data))
	}
}
