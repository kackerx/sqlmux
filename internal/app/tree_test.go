package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// inTree is sized with the sidebar focused.
func inTree(h int) *App {
	a := sized(160, h, "nerd")
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
		if n.Prefix != "" {
			mark += n.Prefix + " "
		}
		if n.kind == nodeSep {
			mark = strings.TrimSpace("── " + n.Text)
			out = append(out, mark)
			continue
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
	a.sess.Conns[0].Tables = append(a.sess.Conns[0].Tables, db.Table{Schema: "public", Name: "v_paid", Kind: "v", Rows: -1}, db.Table{Schema: "public", Name: "mv_sum", Kind: "m", Rows: -1})
	want := []string{
		"▾ doraemon", "  ▸ agentable", "  ▾ public", "    ▾ Tables (14)",
		"      ▸ agent", "      ▸ agent_version",
	}
	got := strings.Split(treeTexts(a), "\n")
	if !slices.Equal(got[:6], want) {
		t.Fatalf("top:\n%s", strings.Join(got[:6], "\n"))
	}
	if tail := strings.Join(got[18:], "|"); tail != "    ▸ Views (2)|▾ 工作区|  ▾ data|      ② query_1|▾ Queries|    query_1" { // a tab straight under its window, marked with its pane's number (F10.5)
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
		if n.Text == "v_paid" && n.Note != "" || n.Text == "mv_sum" && n.Note != "" || n.Text == "v_paid" && n.Icon != ui.NerdIcons.View {
			t.Errorf("%s: %+v", n.Text, n.TreeNode) // a plain view has no rows to count
		}
	}
}

// withConns is sized with seed, connected, its t_order like doraemon's,
// and broken, dialing and idle, a MySQL one, as far as connecting got,
// after doraemon (§7.8「连接节点」).
func withConns() (*App, *Conn) {
	a := sized(160, 45, "nerd")
	seed := readyConn("seed")
	seed.Schema, seed.Schemas, seed.home = "public", []string{"public"}, "public"
	seed.Tables = []db.Table{{Schema: "public", Name: "t_order", Rows: 7}}
	broken, dialing := newConn(config.Connection{Name: "broken", Engine: "postgres"}), newConn(config.Connection{Name: "dialing", Engine: "postgres", ReadOnly: true})
	broken.State, dialing.State = connFailed, connConnecting
	a.sess.Conns = append(a.sess.Conns, seed, broken, dialing, newConn(config.Connection{Name: "idle", Engine: "mysql", ReadOnly: true}))
	return a, seed
}

// Each connection is a root: connected, open on its current_schema();
// connecting, a dim …; failed, in error; not connected, closed; a
// read-only one with a dim lock; its engine's icon (§7.7, §7.8).
func TestGoldenConnNodes(t *testing.T) {
	a, seed := withConns()
	a.sess.open[connNode(a.sess.Conns[0])] = false
	ns, _ := a.treeNodes()
	var rows []string
	for i, n := range ns {
		if n.kind != nodeConn && n.conn != seed {
			continue
		}
		if n.Text == "broken" && n.TextFg != a.theme.Error {
			t.Errorf("broken: %v", n.TextFg)
		}
		r := find(t, a, ui.Target{Kind: ui.KindNode, Pane: 0, I: i})
		rows = append(rows, ansi.Cut(strings.Split(a.render().String(), "\n")[r.Min.Y], r.Min.X, r.Max.X))
	}
	golden.RequireEqual(t, strings.Join(rows, "\n")+"\n")
}

// A connection not connected connects as it opens: l, ↵, or R on it;
// connecting, they do nothing more; failed, a toast names it, and they
// try again (§7.8「连接节点」).
func TestConnectFromTree(t *testing.T) {
	a, _ := withConns()
	broken, idle := a.sess.Conns[2], a.sess.Conns[4]
	a.win().focus(0)
	treeTo(t, a, "idle")
	if _, cmd := a.Update(teaKey("l")); cmd == nil || idle.State != connConnecting {
		t.Fatalf("l: %v", idle.State)
	}
	for _, k := range []string{"<CR>", "R", "l"} {
		if _, cmd := a.Update(teaKey(keymap.Key(k))); idle.State != connConnecting || cmd != nil && cmd() != nil {
			t.Errorf("%s while connecting: %v", k, idle.State)
		}
	}
	a.Update(connected{conn: idle, err: errors.New("no route")})
	if idle.State != connFailed || a.toast != "idle：no route" {
		t.Errorf("failed: %v, toast %q", idle.State, a.toast)
	}
	treeTo(t, a, "broken")
	if _, cmd := a.Update(teaKey("R")); cmd == nil || broken.State != connConnecting {
		t.Errorf("R on a failed one: %v", broken.State)
	}
}

// The current connection (§5): the focused tab's, a result tab's where it
// ran; on the tree the cursor's once connected, else the last one; a
// landing tab's the last one too. The same table of two connections is
// two tabs, each caching its own columns.
func TestCurrentConn(t *testing.T) {
	a, seed := withConns()
	doraemon := a.sess.Conns[0]
	if a.conn() != doraemon || queryOf(a.win().Root.B.Pane).conn != doraemon {
		t.Fatal("at start: the connection it started on, query_1's too")
	}
	a.win().focus(0)
	for _, c := range []struct {
		node string
		want *Conn
	}{{"seed", seed}, {"broken", seed}, {"t_order", doraemon}, {"idle", doraemon}} {
		treeTo(t, a, c.node)
		if a.Update(nil); a.conn() != c.want {
			t.Errorf("tree on %s: %s, want %s", c.node, a.conn().Spec.Name, c.want.Spec.Name)
		}
	}
	data := a.win().Root.A.Pane
	a.openTableIn(data, seed, seed.Tables[0])
	order, _, page := ordersTable(3)
	a.openTableIn(data, doraemon, order)
	if len(data.Tabs) != 2 || data.Tabs[0].Data.conn != seed || dataOf(data).conn != doraemon || a.conn() != doraemon {
		t.Fatalf("tabs %v, current %s", tabNames(data), a.conn().Spec.Name)
	}
	_, cols, _ := ordersTable(0)
	a.Update(pageMsg{tab: data.Tabs[0].Data, seq: data.Tabs[0].Data.seq, cols: cols, page: page})
	if _, ok := doraemon.cols[idOf(order)]; ok || seed.cols[idOf(order)].Cols == nil {
		t.Error("seed's t_order columns went to doraemon's cache")
	}
	feed(t, a, "1gt")
	if a.conn() != seed {
		t.Errorf("seed's tab: %s", a.conn().Spec.Name)
	}
	if feed(t, a, "<C-p>tab.new<CR>"); !data.tab().landing() || a.conn() != seed {
		t.Errorf("a landing tab: %s, want the last, seed", a.conn().Spec.Name)
	}
	query := a.win().Root.B.Pane
	queryOf(query).conn = seed
	if a.win().focus(query.ID); a.conn() != seed {
		t.Errorf("a query on seed: %s", a.conn().Spec.Name)
	}
	res := a.resultPane(a.win())
	res.Tabs = append(res.Tabs, Tab{Name: "quick #1", Result: &resultTab{run: &run{conn: doraemon}}})
	a.win().focus(res.ID)
	if selectTab(res, 1); a.conn() != doraemon {
		t.Errorf("a result of doraemon's: %s", a.conn().Spec.Name)
	}
	a.Update(nil)
	if selectTab(res, 0); a.conn() != doraemon { // the log: the last one, from the result tab
		t.Errorf("the log: %s", a.conn().Spec.Name)
	}
}

// The palette's tables: the current connection's Schema first, its other
// schemas, then the other connections connected; ↵ on seed's opens seed's
// (§12, F5.10).
func TestPaletteTablesOfConns(t *testing.T) {
	a, seed := withConns()
	seed.Tables = append(seed.Tables, db.Table{Schema: "s2", Name: "planner"})
	a.sess.Conns[0].Tables = a.sess.Conns[0].Tables[:3] // planner, agent, agent_version
	feed(t, a, "<C-p>@")
	var wheres []string
	for _, r := range a.paletteView().Rows {
		wheres = append(wheres, r.Name+"@"+r.Where)
	}
	if got := strings.Join(wheres, " "); got != "agent@doraemon.public agent_version@doraemon.public planner@doraemon.agentable t_order@seed.public planner@seed.s2 query_1@" { // the open query after them (F12.10)
		t.Errorf("order: %s", got)
	}
	feed(t, a, "t_order<CR>")
	if tab := dataOf(a.focused()); tab == nil || tab.conn != seed || tab.table.Name != "t_order" {
		t.Errorf("↵ on seed's t_order: %+v", tab)
	}
}

// j / k / gg / G move over the nodes showing, with counts (§7.8).
func TestTreeMoves(t *testing.T) {
	a := inTree(18) // 11 rows show
	for _, c := range []struct{ keys, want string }{
		{"j", "agentable"}, {"3j", "agent"}, {"k", "Tables (14)"},
		{"G", "query_1"}, {"j", "query_1"}, {"gg", "doraemon"}, {"k", "doraemon"},
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
	a := inTree(45)
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
	planner := a.sess.Conns[0].Tables[0]
	a.Update(colsMsg{a.sess.Conns[0], planner, db.Columns{PK: []string{"id"}, Cols: []db.Column{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}}}, nil})
	ns, _ := a.treeNodes()
	i := slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeColumn })
	if i < 0 || ns[i].Text != "id" || ns[i].Icon != ui.NerdIcons.Key || ns[i].Note != "integer" || ns[i+1].Icon != ui.NerdIcons.Column {
		t.Fatalf("columns:\n%s", treeTexts(a))
	}
	a.Update(teaKey("R")) // drops the columns; planner, still open, fetches them again
	_, cmd = a.Update(catalogMsg{conn: a.sess.Conns[0], schemas: a.sess.Conns[0].Schemas, current: a.sess.Conns[0].home, tables: a.sess.Conns[0].Tables})
	if cmd == nil {
		t.Fatal("R: the open planner's columns are not fetched again")
	}
	var fetched []string // what comes of it: the columns, and the indexes and keys (F14.3)
	for _, m := range batchMsgs(cmd) {
		switch m := m.(type) {
		case colsMsg:
			fetched = append(fetched, "cols "+m.table.Name)
		case objectsMsg:
			fetched = append(fetched, "objects "+m.table.Name)
		}
	}
	if strings.Join(fetched, ",") != "cols planner,objects planner" {
		t.Fatalf("R: fetched %v for the open planner", fetched)
	}
	a.Update(colsMsg{a.sess.Conns[0], planner, db.Columns{PK: []string{"id"}, Cols: []db.Column{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}}}, nil})
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
	a := inTree(45)
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
	if cursorText(a) != "planner" || a.sess.Conns[0].Schema != "agentable" {
		t.Fatalf("on %s, the tree's schema %q", cursorText(a), a.sess.Conns[0].Schema)
	}
	treeTo(t, a, "工作区")
	if a.sess.Conns[0].Schema != "agentable" {
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
	a := inTree(45)
	table, cols, page := ordersTable(3)
	for i, tb := range a.sess.Conns[0].Tables {
		if tb.Name == "t_order" {
			a.sess.Conns[0].Tables[i] = table
		}
	}
	a.sess.Conns[0].cols[idOf(table)] = cols
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
}

// The workspace lists this window's tabs, by pane, each marked with its
// pane's number, the one ↵ on a table lands on in focus color; a pane with
// no tab is not there; ↵ on a tab switches to it (§7.8, F10.5).
func TestTreeWorkspace(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.focused()
	feed(t, a, "<C-p>@t_user<CR><C-p>@t_sku<C-t>")
	dataOf(data).shown.applied = "id > 1"
	a.win().focus(0)
	workspace := func() string {
		_, ws, ok := strings.Cut(treeTexts(a), "▾ 工作区")
		if !ok {
			t.Fatal("the tree has no workspace section")
		}
		return "▾ 工作区" + ws
	}
	if ws := workspace(); ws != "▾ 工作区\n  ▾ data\n      ① t_user\n      ① t_sku\n      ② query_1\n▾ Queries\n    query_1" {
		t.Fatalf("workspace:\n%s", ws)
	}
	ns, _ := a.treeNodes()
	for _, n := range ns {
		if n.kind == nodeTab && (n.Current != (n.Text == "t_sku") || n.Text == "t_sku" && n.Aside != "id > 1") {
			t.Errorf("%s: current %v aside %q", n.Text, n.Current, n.Aside)
		}
	}
	// the pane's number before a tab is dim
	f := a.render()
	found := false
	for y := 0; y < a.h && !found; y++ {
		for x := range a.sidebarRect().Dx() { // the sidebar's, not the panes' titles
			if cell := f.Buf.CellAt(x, y); cell != nil && cell.Content == "①" {
				found = true
				if cell.Style.Fg != a.theme.Dim {
					t.Errorf("a tab's pane number is %v, want dim %v", cell.Style.Fg, a.theme.Dim)
				}
				break
			}
		}
	}
	if !found {
		t.Error("no pane number in the sidebar")
	}
	// a pane of two tabs of its own, and another with none: ①①②②④, numbered by pane, the empty ③ left out
	feed(t, a, `<C-l><Space>"<C-p>@t_order<CR><C-p>@t_payment<C-t><Space>"<C-h>`)
	want := "▾ 工作区\n  ▾ data\n      ① t_user\n      ① t_sku\n      ② t_order\n      ② t_payment\n      ④ query_1\n▾ Queries\n    query_1"
	if ws := workspace(); ws != want {
		t.Fatalf("two panes of two tabs:\n%s", ws)
	}
	a.win().focus(0)
	ns, _ = a.treeNodes()
	a.treeGo(slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeTab && n.Text == "t_user" }))
	feed(t, a, "<CR>")
	if a.win().Focus != data.ID || data.Cur != 0 {
		t.Fatalf("↵ on t_user: focus %d cur %d", a.win().Focus, data.Cur)
	}
	feed(t, a, "x")
	if strings.Contains(workspace(), "t_user") {
		t.Error("a closed tab stays in the workspace")
	}
	// a window with no tab has nothing to open
	feed(t, a, "<Space>c")
	if ws := workspace(); !strings.Contains(ws, "\n    data-2\n") {
		t.Errorf("a window of one empty pane:\n%s", ws)
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
	a := inTree(45)
	in := &a.win().tree.filter
	for _, c := range []struct{ keys, want string }{
		{"/t_order foo<C-w>", "t_order "}, {"<M-BS>", ""}, {"ab cd<Left><Left><C-u>", "cd"},
	} {
		if feed(t, a, c.keys); in.Text != c.want {
			t.Errorf("filter %s: %q, want %q", c.keys, in.Text, c.want)
		}
	}
	a, tab, _ := withRecorder(t)
	if feed(t, a, "/id > 5<C-w><M-BS>"); tab.where.Text != "id " {
		t.Errorf("WHERE: %q", tab.where.Text)
	}
	if feed(t, a, "<Esc><Esc><C-p>@t_ord<C-w>"); a.palette.input.Text != "@" {
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
	a := inTree(45)
	feed(t, a, "/tor")
	if a.mode() != keymap.Insert || treeTexts(a) != "▾ doraemon\n  ▾ public\n    ▾ Tables (14)\n      ▸ t_order\n      ▸ t_order_item" {
		t.Fatalf("typing: mode %v\n%s", a.mode(), treeTexts(a))
	}
	f := a.render()
	if row := strings.Split(f.String(), "\n")[1]; !strings.Contains(row, " tor") || !strings.Contains(row, " 2/15 "+a.icons.RowAdd.Text+" │") {
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
	a.sess.Conns[0].Schema = ""
	a.Update(catalogMsg{conn: a.sess.Conns[0], schemas: []string{"a", "b"}, current: "b", tables: []db.Table{{Schema: "b", Name: "x"}}})
	if a.sess.Conns[0].Schema != "b" || a.sess.Conns[0].home != "b" || !strings.Contains(treeTexts(a), "▾ b\n    ▾ Tables (1)") {
		t.Fatalf("schema %q\n%s", a.sess.Conns[0].Schema, treeTexts(a))
	}
	a.Update(catalogMsg{conn: a.sess.Conns[0], schemas: []string{"a"}})
	if a.sess.Conns[0].Schema != "a" {
		t.Errorf("gone from the catalog: schema %q, want the first", a.sess.Conns[0].Schema)
	}
	a.Update(catalogMsg{conn: a.sess.Conns[0], err: errors.New("permission denied")})
	if a.toast != "doraemon：permission denied" || a.sess.Conns[0].Schema != "a" {
		t.Errorf("a failed load: toast %q", a.toast)
	}
}

// The palette lists every schema's tables, the tree's first (§12).
func TestPaletteTablesFromCatalog(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>@")
	names := namesOf(a)
	if len(names) != 16 || names[0] != "表:agent" || names[14] != "表:planner" || names[15] != "Query:query_1" { // the open query is the one after them (F12.10)
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
	a := inTree(45)
	table, cols, _ := ordersTable(0)
	a.sess.Conns[0].Tables[slices.IndexFunc(a.sess.Conns[0].Tables, func(t db.Table) bool { return t.Name == "t_order" })] = table
	a.sess.Conns[0].cols[idOf(table)] = cols
	treeTo(t, a, "t_order")
	feed(t, a, "l")
	golden.RequireEqual(t, a.render().String())
}

// A filter across schemas: the matches and what is above them (§7.8).
func TestGoldenTreeFilter160x45(t *testing.T) {
	a := inTree(45)
	feed(t, a, "/an")
	golden.RequireEqual(t, a.render().String())
}

// The schema the tree opens a connection on stays across catalog loads
// while it is there, whatever Meta's current one is now (a quick SQL's
// USE moves MySQL's); gone, the current one takes its place (§7.8).
func TestCatalogKeepsHome(t *testing.T) {
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	a.Update(catalogMsg{conn: c, schemas: []string{"agentable", "public"}, current: "agentable", tables: c.Tables})
	if c.home != "public" {
		t.Errorf("moved to %s", c.home)
	}
	a.Update(catalogMsg{conn: c, schemas: []string{"agentable"}, current: "agentable", tables: c.Tables})
	if c.home != "agentable" {
		t.Errorf("public gone: %s", c.home)
	}
}

// A SQLite file has no schema level: its Tables and Views hang from the
// connection, a table's columns two levels under them (F14.1).
func TestTreeSQLiteFlat(t *testing.T) {
	a := sized(160, 40, "nerd")
	c := a.sess.Conns[0]
	c.Spec.Engine = "sqlite"
	c.Schemas, c.home, c.Schema = []string{"main"}, "main", "main"
	c.Tables = []db.Table{{Schema: "main", Name: "t_user", Rows: -1, Kind: "r"}, {Schema: "main", Name: "v_user", Rows: -1, Kind: "v"}}
	c.cols[idOf(c.Tables[0])] = db.Columns{Cols: []db.Column{{Name: "id", Type: "integer"}}, PK: []string{"id"}}
	a.sess.open[tableNode(c, c.Tables[0])] = true
	c.idx[idOf(c.Tables[0])] = []db.Index{{Name: "i", Cols: []string{"id"}}}
	c.keys[idOf(c.Tables[0])] = []db.Key{{Name: "PRIMARY", Cols: []string{"id"}}, {Cols: []string{"u"}, Ref: &db.Ref{Schema: "main", Table: "t_x", Cols: []string{"id"}}}}
	a.sess.open["keys:"+tableNode(c, c.Tables[0])] = true
	if ns, _ := a.treeNodes(); slices.ContainsFunc(ns, func(n node) bool { return n.kind == nodeTable && n.Note != "" }) {
		t.Error("a file has no row count to show")
	}
	got := strings.Split(treeTexts(a), "\n")
	want := []string{"▾ doraemon", "  ▾ Tables (1)", "    ▾ t_user", "        id", "      ▸ Indexes (1)", "      ▾ Keys (2)", "          PRIMARY (id)", "          FOREIGN KEY (u) → t_x (id)", "  ▸ Views (1)"}
	if len(got) < len(want) || strings.Join(got[:len(want)], "|") != strings.Join(want, "|") {
		t.Fatalf("tree:\n%s", strings.Join(got, "\n"))
	}
}

// batchMsgs is what the Cmd, a tea.Batch of Cmds perhaps, brings.
func batchMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, batchMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{cmd()}
}

// With a filter kept, esc in the tree is /: back in the input, the text as it was and
// the cursor at its end; esc there clears it, as it always did, so two of them
// clear it; with no filter esc does what it did, nothing (F20.9).
func TestTreeEscBackToFilter(t *testing.T) {
	a := inTree(45)
	before := treeTexts(a)
	if feed(t, a, "<Esc>"); a.win().tree.filtering || a.mode() != keymap.Normal || treeTexts(a) != before {
		t.Fatal("esc with no filter does something")
	}
	feed(t, a, "/tor<CR>jj")
	kept := treeTexts(a)
	if feed(t, a, "<Esc>"); !a.win().tree.filtering || a.mode() != keymap.Insert || a.win().tree.filter.Text != "tor" || a.win().tree.filter.Pos != 3 || treeTexts(a) != kept {
		t.Fatalf("esc with a filter kept: filtering %v, mode %v, filter %+v", a.win().tree.filtering, a.mode(), a.win().tree.filter)
	}
	if feed(t, a, "<Esc>"); a.win().tree.filtering || a.win().tree.filter.Text != "" || a.mode() != keymap.Normal || treeTexts(a) == kept || !strings.Contains(treeTexts(a), "工作区") {
		t.Errorf("the second esc: filtering %v, filter %q\n%s", a.win().tree.filtering, a.win().tree.filter.Text, treeTexts(a))
	}
}

// The same in a Redis tree, whose filter asks the server (F20.9).
func TestRedisTreeEscBackToFilter(t *testing.T) {
	a, _ := redisTree(t, someKeys())
	a.win().focus(0)
	a.win().tree.filter = ui.Input{Text: "users", Pos: 5}
	if feed(t, a, "<Esc>"); !a.win().tree.filtering || a.win().tree.filter.Text != "users" || a.win().tree.filter.Pos != 5 {
		t.Fatalf("esc with a filter kept: %+v", a.win().tree)
	}
	if feed(t, a, "<Esc>"); a.win().tree.filtering || a.win().tree.filter.Text != "" {
		t.Errorf("the second esc: %+v", a.win().tree)
	}
}
