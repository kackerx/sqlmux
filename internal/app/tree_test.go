package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

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

// treeTexts is the nodes showing, by their text, one per line with their
// depth as indent and ▸ / ▾.
func treeTexts(a *App) string {
	ns, _ := a.treeNodes()
	var out []string
	for _, n := range ns {
		mark := "  "
		if n.Branch {
			mark = map[bool]string{false: "▸ ", true: "▾ "}[n.Open]
		}
		out = append(out, strings.Repeat("  ", n.Depth)+mark+n.Text)
	}
	return strings.Join(out, "\n")
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

// cursorText is the text of the node under the tree's cursor.
func cursorText(a *App) string {
	n, _ := a.treeNode()
	return n.Text
}

// treeTo puts the tree's cursor on the node with text s.
func treeTo(t *testing.T, a *App, s string) {
	t.Helper()
	ns, _ := a.treeNodes()
	i := slices.IndexFunc(ns, func(n node) bool { return n.Text == s })
	if i < 0 {
		t.Fatalf("no node %q in\n%s", s, treeTexts(a))
	}
	a.treeGo(i)
}

// The default tree (§7.8): the session, current_schema() and its Tables
// open, other schemas closed, Views listed even when empty, then the
// workspace down to its panes.
func TestTreeNodes(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.sess.Tables = append(a.sess.Tables, db.Table{Schema: "public", Name: "v_paid", Kind: "v", Rows: -1}, db.Table{Schema: "public", Name: "mv_sum", Kind: "m", Rows: -1})
	want := []string{
		"▾ doraemon", "  ▸ agentable", "  ▾ public", "    ▾ Tables (14)",
		"      ▸ agent", "      ▸ agent_version",
	}
	got := strings.Split(treeTexts(a), "\n")
	if !slices.Equal(got[:6], want) {
		t.Fatalf("top:\n%s", strings.Join(got[:6], "\n"))
	}
	if tail := strings.Join(got[18:], "|"); tail != "    ▸ Views (2)|▾ 工作区|  ▾ data|      pane-1|    ▾ pane-2|        console_1" { // a pane is pane-<n>, no type (§7.8)
		t.Errorf("tail %q", tail)
	}
	ns, _ := a.treeNodes()
	for _, n := range ns {
		switch n.Text {
		case "t_order":
			if n.Note != "1.2m" || !n.Branch || n.Open {
				t.Errorf("t_order: %+v", n.TreeNode)
			}
		case "v_paid", "mv_sum":
			t.Errorf("%s shows under a closed Views", n.Text)
		}
	}
	treeTo(t, a, "Views (2)")
	feed(t, a, "<C-h><CR>")
	ns, _ = a.treeNodes()
	for _, n := range ns {
		if n.Text == "v_paid" && n.Note != "" || n.Text == "mv_sum" && n.Note != "?" || n.Text == "v_paid" && n.Icon != ui.NerdIcons.View {
			t.Errorf("%s: %+v", n.Text, n.TreeNode) // a plain view has no rows to count
		}
	}
}

// j / k / gg / G move over the nodes showing, with counts (§7.8).
func TestTreeMoves(t *testing.T) {
	a := inTree(160, 18) // 11 rows show
	for _, c := range []struct{ keys, want string }{
		{"j", "agentable"}, {"3j", "agent"}, {"k", "Tables (14)"},
		{"G", "console_1"}, {"j", "console_1"}, {"gg", "doraemon"}, {"k", "doraemon"},
	} {
		feed(t, a, c.keys)
		if got := cursorText(a); got != c.want {
			t.Errorf("%s: cursor on %s, want %s", c.keys, got, c.want)
		}
	}
	feed(t, a, "G")
	if ns, _ := a.treeNodes(); a.win().tree.top != len(ns)-11 {
		t.Errorf("G: top %d, the cursor's row must show", a.win().tree.top)
	}
}

// l opens a node, then steps into it; h closes it, then steps out to the
// parent; on a leaf l does nothing. A table's columns come from Meta the
// first time it opens, the primary key's with the key icon (§7.8).
func TestTreeExpand(t *testing.T) {
	a := inTree(160, 45)
	treeTo(t, a, "agentable")
	feed(t, a, "l")
	if !strings.Contains(treeTexts(a), "  ▾ agentable\n    ▸ Tables (1)\n      Views (0)") {
		t.Fatalf("l on agentable:\n%s", treeTexts(a))
	}
	feed(t, a, "ll")
	if cursorText(a) != "Tables (1)" || !strings.Contains(treeTexts(a), "    ▾ Tables (1)\n      ▸ planner") {
		t.Fatalf("ll: on %s\n%s", cursorText(a), treeTexts(a))
	}
	feed(t, a, "j")
	_, cmd := a.Update(teaKey("l"))
	if cmd == nil || !strings.Contains(treeTexts(a), "▾ planner") {
		t.Fatal("opening planner fetches its columns")
	}
	planner := a.sess.Tables[0]
	a.Update(colsMsg{planner, db.Columns{PK: []string{"id"}, Cols: []db.Column{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}}}, nil})
	ns, _ := a.treeNodes()
	i := slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeColumn })
	if i < 0 || ns[i].Text != "id" || ns[i].Icon != ui.NerdIcons.Key || ns[i].Note != "integer" || ns[i+1].Icon != ui.NerdIcons.Column {
		t.Fatalf("columns:\n%s", treeTexts(a))
	}
	a.Update(teaKey("R")) // drops the columns; planner, still open, fetches them again
	_, cmd = a.Update(catalogMsg{schemas: a.sess.Schemas, current: a.sess.home, tables: a.sess.Tables})
	if cmd == nil {
		t.Fatal("R: the open planner's columns are not fetched again")
	}
	if m, ok := cmd().(colsMsg); !ok || m.table != planner {
		t.Fatalf("R: no fetch of the open planner's columns")
	}
	a.Update(colsMsg{planner, db.Columns{PK: []string{"id"}, Cols: []db.Column{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}}}, nil})
	for _, c := range []struct{ keys, want string }{
		{"l", "id"}, {"l", "id"}, {"h", "planner"}, {"h", "planner"}, {"h", "Tables (1)"}, {"k", "agentable"},
	} {
		feed(t, a, c.keys)
		if got := cursorText(a); got != c.want {
			t.Errorf("%s: on %s, want %s", c.keys, got, c.want)
		}
	}
	if strings.Contains(treeTexts(a), "▾ planner") {
		t.Error("h closed planner")
	}
	feed(t, a, "<CR>")
	if strings.Contains(treeTexts(a), "Tables (1)") {
		t.Error("↵ on a schema closes it")
	}
}

// ↵ and t open the cursor's table where ↵ would land and move focus there
// (§7.8); the tree marks it as open. A table in another schema opens as it
// is, and puts the tree's schema there.
func TestTreeOpens(t *testing.T) {
	a := inTree(160, 45)
	data := a.win().Root.Leaves()[0]
	treeTo(t, a, "goal")
	feed(t, a, "<CR>")
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
	a.win().focus(0)
	treeTo(t, a, "agentable")
	feed(t, a, "lllj")
	if cursorText(a) != "planner" || a.sess.Schema != "agentable" {
		t.Fatalf("on %s, the tree's schema %q", cursorText(a), a.sess.Schema)
	}
	treeTo(t, a, "工作区")
	if a.sess.Schema != "agentable" {
		t.Error("the workspace is under no schema: the last one stays")
	}
	treeTo(t, a, "planner")
	if feed(t, a, "<CR>"); dataOf(data).table.Schema != "agentable" {
		t.Errorf("opened %+v", dataOf(data).table)
	}
}

// A column's ↵ opens its table on that column: once the page is in for a
// new tab, at once for one already open, not when COLS hides it (§7.8).
func TestTreeColumnOpens(t *testing.T) {
	a := inTree(160, 45)
	table, cols, page := ordersTable(3)
	for i, tb := range a.sess.Tables {
		if tb.Name == "t_order" {
			a.sess.Tables[i] = table
		}
	}
	a.sess.cols[idOf(table)] = cols
	treeTo(t, a, "t_order")
	feed(t, a, "l")
	treeTo(t, a, "amount")
	feed(t, a, "<CR>")
	tab := dataOf(a.focused())
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if tab.col != 2 || tab.wantCol != "" {
		t.Fatalf("new tab: col %d, want amount's 2", tab.col)
	}
	a.win().focus(0)
	treeTo(t, a, "note")
	if feed(t, a, "<CR>"); tab.col != 5 {
		t.Errorf("open tab: col %d, want note's 5", tab.col)
	}
	tab.hidden["status"] = true
	a.win().focus(0)
	treeTo(t, a, "status")
	if feed(t, a, "<CR>"); tab.col != 5 {
		t.Errorf("hidden: col %d, want it to stay", tab.col)
	}
	delete(tab.hidden, "status")
	a.win().focus(0)
	treeTo(t, a, "t_order")
	feed(t, a, "t") // a second tab of it: a column's ↵ now goes through the pick
	a.win().focus(0)
	treeTo(t, a, "amount")
	if feed(t, a, "<CR>"); a.palette == nil || a.palette.pick == nil {
		t.Fatal("two tabs of t_order: no pick")
	}
	if feed(t, a, "<CR>"); a.palette != nil || dataOf(a.focused()) != tab || tab.col != 2 {
		t.Errorf("picked the first: col %d, want amount's 2", tab.col)
	}
}

// The workspace lists this window's panes and their tabs, the one ↵ on a
// table lands on in focus color; ↵ on a pane opens or closes it, on a tab
// switches to it (§7.8).
func TestTreeWorkspace(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.focused()
	feed(t, a, "<C-p>@t_user<CR><C-p>@t_sku<C-t>")
	dataOf(data).shown.applied = "id > 1"
	a.win().focus(0)
	workspace := func() string { return treeTexts(a)[strings.Index(treeTexts(a), "▾ 工作区"):] }
	if ws := workspace(); ws != "▾ 工作区\n  ▾ data\n    ▾ pane-1\n        t_user\n        t_sku\n    ▾ pane-2\n        console_1" {
		t.Fatalf("workspace:\n%s", ws)
	}
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind == nodeTab && (n.Current != (n.Text == "t_sku") || n.Text == "t_sku" && n.Aside != "id > 1") {
			t.Errorf("%s: current %v aside %q", n.Text, n.Current, n.Aside)
		}
	}
	treeTo(t, a, "pane-1")
	if feed(t, a, "<CR>"); a.win().Focus != 0 || !strings.Contains(workspace(), "▸ pane-1\n    ▾ pane-2") {
		t.Fatalf("↵ on a pane closes it, the focus stays: %d\n%s", a.win().Focus, workspace())
	}
	pane := slices.IndexFunc(func() []node { ns, _ := a.treeNodes(); return ns }(), func(n node) bool { return n.Text == "pane-1" })
	if click(a, find(t, a, ui.Target{Kind: ui.KindNode, Pane: 0, I: pane}).Min); a.win().Focus != 0 || !strings.Contains(workspace(), "▾ pane-1") {
		t.Fatalf("a click on it opens it again, the focus stays: %d", a.win().Focus)
	}
	feed(t, a, "j<CR>")
	if a.win().Focus != data.ID || data.Cur != 0 {
		t.Fatalf("↵ on t_user: focus %d cur %d", a.win().Focus, data.Cur)
	}
	feed(t, a, "x")
	if strings.Contains(workspace(), "t_user") {
		t.Error("a closed tab stays in the workspace")
	}
}

// From the tree a table opens in the data pane focused last, not the first
// (§12); the open table and the workspace's current tab both point there
// (§7.8).
func TestTreeOpensInLastFocused(t *testing.T) {
	a := sized(160, 45, "nerd")
	top := a.focused()
	feed(t, a, `<C-p>@t_user<CR><Space>"`) // ② below ①, focused
	below := a.focused()
	feed(t, a, "<C-h>")
	if a.win().Focus != 0 {
		t.Fatalf("C-h from ②: focus %d", a.win().Focus)
	}
	treeTo(t, a, "t_sku")
	if feed(t, a, "<CR>"); tabNames(below) != "t_sku" || tabNames(top) != "t_user" || a.win().Focus != below.ID {
		t.Fatalf("↵: ① %v ② %v, focus %d", tabNames(top), tabNames(below), a.win().Focus)
	}
	feed(t, a, "<C-h>")
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if (n.kind == nodeTable || n.kind == nodeTab) && n.Current != (n.Text == "t_sku") {
			t.Errorf("%s (%v): current %v", n.Text, n.kind, n.Current)
		}
	}
}

// Clicks: a row is its ↵, ▸ / ▾ opens and closes, a middle click is t
// (§7.8).
func TestTreeMouse(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.win().Root.Leaves()[0]
	goal := slices.IndexFunc(func() []node { ns, _ := a.treeNodes(); return ns }(), func(n node) bool { return n.Text == "goal" })
	r := find(t, a, ui.Target{Kind: ui.KindNode, Pane: 0, I: goal})
	click(a, r.Min)
	if tabNames(data) != "goal" || a.win().Focus != data.ID {
		t.Fatalf("click: tabs %v, focus %d", tabNames(data), a.win().Focus)
	}
	a.View()
	a.Update(tea.MouseClickMsg{X: r.Min.X, Y: r.Min.Y + 1, Button: tea.MouseMiddle})
	if tabNames(data) != "goal mt_task" {
		t.Fatalf("middle click: tabs %v", tabNames(data))
	}
	fold := find(t, a, ui.Target{Kind: ui.KindFold, Pane: 0, I: 1}) // agentable's ▸
	click(a, fold.Min)
	if !strings.Contains(treeTexts(a), "▾ agentable") || a.win().Focus != 0 {
		t.Fatalf("clicking ▸ opens agentable:\n%s", treeTexts(a))
	}
	a.Update(tea.MouseMotionMsg{X: r.Min.X + 3, Y: r.Min.Y + 5})
	if st := a.render().Buf.CellAt(r.Min.X+3, r.Min.Y+5).Style; st.Bg != a.theme.Row {
		t.Errorf("hover: bg %v, want row", st.Bg)
	}
}

// C-w, M-BS and C-u in every input (§7.9): the tree's filter, a WHERE,
// the palette, a cell's edit, where all of it goes while all selected.
func TestInputDeleteWord(t *testing.T) {
	a := inTree(160, 45)
	in := &a.win().tree.filter
	for _, c := range []struct{ keys, want string }{
		{"/t_order foo<C-w>", "t_order "}, {"<M-BS>", ""}, {"ab cd<Left><Left><C-u>", "cd"},
	} {
		if feed(t, a, c.keys); in.Text != c.want {
			t.Errorf("filter %s: %q, want %q", c.keys, in.Text, c.want)
		}
	}
	a, tab, _ := withRecorder(t, 160, 45)
	if feed(t, a, "/id > 5<C-w><M-BS>"); tab.where.Text != "id " {
		t.Errorf("WHERE: %q", tab.where.Text)
	}
	if feed(t, a, "<Esc><C-p>@t_ord<C-w>"); a.palette.input.Text != "@" {
		t.Errorf("palette: %q", a.palette.input.Text)
	}
	if feed(t, a, "<Esc>i<C-w>"); tab.cell == nil || tab.cell.in.Text != "" {
		t.Errorf("a cell's edit, all selected: %+v", tab.cell)
	}
}

// The live filter (§7.8): INSERT while typing; the tables and views it
// matches, across schemas, with the nodes above them opened, and nothing
// else; ↵ keeps it for the list, esc clears it, the cursor staying on its
// node.
func TestTreeFilter(t *testing.T) {
	a := inTree(160, 45)
	feed(t, a, "/tor")
	if a.mode() != keymap.Insert || treeTexts(a) != "▾ doraemon\n  ▾ public\n    ▾ Tables (14)\n      ▸ t_order\n      ▸ t_order_item" {
		t.Fatalf("typing: mode %v\n%s", a.mode(), treeTexts(a))
	}
	f := a.render()
	if row := strings.Split(f.String(), "\n")[1]; !strings.Contains(row, " tor") || !strings.Contains(row, " 2/15 │") {
		t.Errorf("filter row %q", row)
	}
	if f.Cursor == nil || f.Cursor.Y != 1 {
		t.Errorf("the terminal cursor sits in the filter: %v", f.Cursor)
	}
	if st := styleOf(t, f, "order "); st.Fg != a.theme.Match || st.Bg == a.theme.Match { // "t_" didn't match, "or" did
		t.Errorf("match highlight: %+v", st)
	}
	feed(t, a, "<CR>j")
	if a.mode() != keymap.Normal || cursorText(a) != "t_order_item" {
		t.Fatalf("after ↵: mode %v, cursor %s", a.mode(), cursorText(a))
	}
	feed(t, a, "/<Esc>")
	if a.win().tree.filter.Text != "" || cursorText(a) != "t_order_item" || !strings.Contains(treeTexts(a), "工作区") {
		t.Fatalf("after esc: filter %q, cursor %s", a.win().tree.filter.Text, cursorText(a))
	}
	feed(t, a, "/plan")
	if !strings.Contains(treeTexts(a), "▾ agentable\n    ▾ Tables (1)\n      ▸ planner") || strings.Contains(treeTexts(a), "public") {
		t.Errorf("across schemas:\n%s", treeTexts(a))
	}
	feed(t, a, "<Esc>/x<C-l>")
	if a.mode() != keymap.Insert {
		t.Error("NORMAL keys are text in the filter")
	}
	click(a, uv.Pos(100, 10)) // another pane takes focus, and the filter row the keys
	if a.win().tree.filtering || a.win().tree.filter.Text != "x" {
		t.Errorf("leaving the tree: filtering %v, filter %q", a.win().tree.filtering, a.win().tree.filter.Text)
	}
}

// The first load opens current_schema(), else the first; the tree's schema
// starts there; a failed load says why in a toast (§7.8).
func TestCatalogLoad(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.sess.Schema = ""
	a.Update(catalogMsg{schemas: []string{"a", "b"}, current: "b", tables: []db.Table{{Schema: "b", Name: "x"}}})
	if a.sess.Schema != "b" || a.sess.home != "b" || !strings.Contains(treeTexts(a), "▾ b\n    ▾ Tables (1)") {
		t.Fatalf("schema %q\n%s", a.sess.Schema, treeTexts(a))
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

// t_order opened to its columns, the key's with its icon, types on the
// right (§7.8).
func TestGoldenTreeColumns160x45(t *testing.T) {
	a := inTree(160, 45)
	table, cols, _ := ordersTable(0)
	a.sess.Tables[slices.IndexFunc(a.sess.Tables, func(t db.Table) bool { return t.Name == "t_order" })] = table
	a.sess.cols[idOf(table)] = cols
	treeTo(t, a, "t_order")
	feed(t, a, "l")
	golden.RequireEqual(t, a.render().String())
}

// A filter across schemas: the matches and what is above them (§7.8).
func TestGoldenTreeFilter160x45(t *testing.T) {
	a := inTree(160, 45)
	feed(t, a, "/an")
	golden.RequireEqual(t, a.render().String())
}
