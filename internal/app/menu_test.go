package app

import (
	"context"
	"image/color"
	"io"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// menuApp is a tree with a view and a materialized view in public, query_1
// open: the nodes a right-click menu is for.
func menuApp() *App {
	a := sized(160, 45, "nerd")
	a.win().focus(0)
	c := a.sess.Conns[0]
	c.Tables = append(c.Tables, db.Table{Schema: "public", Name: "v_paid", Kind: "v", Rows: -1}, db.Table{Schema: "public", Name: "mv_sum", Kind: "m", Rows: -1})
	a.sess.open[groupNode(c, "public", true)] = true // the Views
	return a
}

// nodeRect is where the tree draws the node with this text.
func nodeRect(t *testing.T, a *App, text string) uv.Rectangle {
	t.Helper()
	ns, _ := a.treeNodes()
	i := slices.IndexFunc(ns, func(n node) bool { return n.Text == text || n.id == text })
	if i < 0 {
		t.Fatalf("no node %q in\n%s", text, treeTexts(a))
	}
	return find(t, a, ui.Target{Kind: ui.KindNode, Pane: 0, I: i})
}

// rightClickNode right-clicks the node with this text, drawn first.
func rightClickNode(t *testing.T, a *App, text string) {
	t.Helper()
	r := nodeRect(t, a, text)
	a.Update(tea.MouseClickMsg{X: r.Min.X + 3, Y: r.Min.Y, Button: tea.MouseRight})
}

func menuLabels(a *App) (labels []string) {
	if a.menu != nil {
		for _, it := range a.menu.items {
			labels = append(labels, it.label)
		}
	}
	return labels
}

// A right click on a table, a view, a query file opens a menu with what
// can be done to it, the tree's cursor on the node; on any other node
// nothing opens (F8.23).
func TestMenuItemsByNode(t *testing.T) {
	a := menuApp()
	for _, c := range []struct {
		node string
		want []string
	}{
		{"t_user", []string{"打开", "在新 tab 打开", "删除表…", "加列…", "加索引…", "改名…", "删除列…", "删除索引…"}}, // and the structure changes' (F14.4)
		{"v_paid", []string{"打开", "在新 tab 打开", "删除视图…", "改名…"}},
		{"mv_sum", []string{"打开", "在新 tab 打开", "删除视图…", "改名…"}},
		{"queries/query_1", []string{"打开", "重命名…", "删除…"}},
		{"public", nil},
		{"Tables (14)", nil},
		{"Queries", nil},
		{"data", nil},
	} {
		a.menu = nil
		a.View()
		rightClickNode(t, a, c.node)
		if got := menuLabels(a); !slices.Equal(got, c.want) || (a.menu == nil) != (c.want == nil) {
			t.Errorf("%s: %v (open: %v), want %v", c.node, got, a.menu != nil, c.want)
		}
		if n, _ := a.treeNode(); n.Text != c.node && n.id != c.node {
			t.Errorf("%s: the cursor is on %s", c.node, n.Text)
		}
	}
}

// The menu takes j / k / the arrows, ↵ and esc, and no other key: they are no
// text for the query behind it; a click on an item runs it, a click off
// the menu closes it and does no more (F8.23).
func TestMenuKeysAndClicks(t *testing.T) {
	a := menuApp()
	data := a.win().Root.Leaves()[0]
	a.View()
	rightClickNode(t, a, "t_user")
	feed(t, a, "j<Down>k<CR>")
	if a.menu != nil || tabNames(data) != "t_user" {
		t.Fatalf("↵ on the second item (open in a new tab): menu %v, tabs %q", a.menu != nil, tabNames(data))
	}
	a.View()
	rightClickNode(t, a, "t_order")
	if feed(t, a, "xyz"); a.menu == nil || a.menu.sel != 0 {
		t.Fatal("a key that is no menu's closed it or moved it")
	}
	feed(t, a, "<Esc>")
	if a.menu != nil || a.confirm != nil {
		t.Fatal("esc leaves the menu open")
	}
	a.View()
	rightClickNode(t, a, "t_order")
	a.View()
	click(a, find(t, a, ui.Target{Kind: ui.KindRow, I: 1}).Min)
	if a.menu != nil || tabNames(data) != "t_user t_order" {
		t.Errorf("a click on an item: menu %v, tabs %q", a.menu != nil, tabNames(data))
	}
	a.View()
	rightClickNode(t, a, "t_sku")
	a.View()
	click(a, uv.Pos(100, 20))
	if a.menu != nil || tabNames(data) != "t_user t_order" {
		t.Errorf("a click off the menu: menu %v, tabs %q", a.menu != nil, tabNames(data))
	}
	// another overlay is no place for one: the palette hides the tree, the
	// name asked in the status bar does not
	feed(t, a, "<C-p>")
	a.View()
	rightClickNode(t, a, "t_sku")
	if a.menu != nil {
		t.Error("a menu over the palette")
	}
	feed(t, a, "<Esc>")
	a.win().focus(a.win().Root.Leaves()[1].ID)
	a.run("query.rename", 0)
	if a.askName == nil {
		t.Fatal("no name asked")
	}
	a.View()
	rightClickNode(t, a, "t_sku")
	if a.menu != nil {
		t.Error("a menu while a name is typed")
	}
}

// The menu's rename is query.rename, the query opened first when it is
// not; its delete is tree.delete (F8.23).
func TestMenuRenameAndDelete(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := menuApp()
	queryFile(t, "query_2", "two\n")
	a.View()
	rightClickNode(t, a, "queries/query_2")
	feed(t, a, "jj<CR>")
	if a.confirm == nil || a.confirm.text != "删除 query_2？" {
		t.Fatalf("delete: %v", a.confirm)
	}
	feed(t, a, "<Esc>")
	a.View()
	rightClickNode(t, a, "queries/query_2")
	feed(t, a, "j<CR>")
	if a.askName == nil || queryOf(a.focused()) == nil || queryOf(a.focused()).name != "query_2" {
		t.Fatalf("rename: asked %v, on %v", a.askName != nil, queryOf(a.focused()))
	}
}

// dd on a table or a view asks to drop it, the DROP in the question, the title in
// the error color, whatever confirm_dangerous says; on a schema it does
// nothing; ↵ drops, and the tabs of it in every window go (F8.23).
func TestDropTable(t *testing.T) {
	a := menuApp()
	a.confirmDangerous = false
	data := a.win().Root.Leaves()[0]
	feed(t, a, "<C-p>@t_user<CR><C-p>@t_sku<CR>")
	if tabNames(data) != "t_user t_sku" {
		t.Fatalf("tabs %q", tabNames(data))
	}
	a.win().focus(0)
	treeTo(t, a, "public")
	if feed(t, a, "dd"); a.confirm != nil {
		t.Fatal("dd on a schema asks")
	}
	a.sess.Conns[0].Tables = append(a.sess.Conns[0].Tables, db.Table{Schema: "public", Name: "ft_remote", Kind: "f", Rows: -1})
	for node, want := range map[string]string{"t_user": `DROP TABLE "public"."t_user"`, "v_paid": `DROP VIEW "public"."v_paid"`, "mv_sum": `DROP MATERIALIZED VIEW "public"."mv_sum"`, "ft_remote": `DROP FOREIGN TABLE "public"."ft_remote"`} {
		treeTo(t, a, node)
		if feed(t, a, "dd"); a.confirm == nil || !a.confirm.danger || !strings.Contains(a.confirm.text, want) {
			t.Fatalf("%s: %+v", node, a.confirm)
		}
		feed(t, a, "<Esc>")
	}
	treeTo(t, a, "t_user")
	feed(t, a, "dd")
	golden.RequireEqual(t, a.render().String())
	busy := a.sess.Conns[0].busy // the pages of the two tabs
	_, cmd := a.Update(teaKey("<CR>"))
	if a.confirm != nil || cmd == nil || a.sess.Conns[0].busy != busy+1 {
		t.Fatalf("↵: confirm %v, cmd %v, busy %d, was %d", a.confirm != nil, cmd != nil, a.sess.Conns[0].busy, busy)
	}
	m, ok := cmd().(dropped)
	if !ok || m.err != nil || m.table.Name != "t_user" {
		t.Fatalf("answer %+v", m)
	}
	a.Update(m)
	if tabNames(data) != "t_sku" || a.sess.Conns[0].busy != busy || a.toast != "已删除 public.t_user" {
		t.Errorf("dropped: tabs %q, busy %d, toast %q", tabNames(data), a.sess.Conns[0].busy, a.toast)
	}
}

// A drop that the server refuses is its message in a toast, with what
// depends on the table, the tabs stay and the catalog is read again; a cancel
// is said so (F8.23).
func TestDropTableFails(t *testing.T) {
	a := menuApp()
	c := a.sess.Conns[0]
	boom := &pgconn.PgError{Severity: "ERROR", Code: "2BP01", Message: "cannot drop table t_user", Detail: "view v depends on table t_user"}
	c.Main = db.NewWorker(&execDB{fail: map[string]error{`DROP TABLE "public"."t_user"`: boom}})
	data := a.win().Root.Leaves()[0]
	feed(t, a, "<C-p>@t_user<CR>")
	a.win().focus(0)
	treeTo(t, a, "t_user")
	feed(t, a, "dd")
	busy := c.busy
	_, cmd := a.Update(teaKey("<CR>"))
	m, ok := cmd().(dropped)
	if !ok || m.err == nil || c.busy != busy+1 {
		t.Fatalf("answer %+v, busy %d (was %d)", m, c.busy, busy)
	}
	a.Update(m)
	if tabNames(data) != "t_user" || a.toast != "cannot drop table t_user · DETAIL: view v depends on table t_user" || c.busy != busy {
		t.Errorf("tabs %q, toast %q, busy %d (was %d)", tabNames(data), a.toast, c.busy, busy)
	}
	// a lost connection says so by its own event: no toast of the driver's here
	c.busy++
	a.toast = "kept"
	_, lost := a.Update(dropped{c, m.table, io.ErrUnexpectedEOF})
	if a.toast != "kept" || lost == nil { // the server may have dropped it after the connection went: the catalog is read, and that is all
		t.Errorf("a lost connection: toast %q, catalog read: %v", a.toast, lost != nil)
	}
	c.busy++
	if a.Update(dropped{c, m.table, context.Canceled}); a.toast != "查询已取消" {
		t.Errorf("cancelled: toast %q", a.toast)
	}
}

// Only the tabs of the table dropped close: not one of the same name in
// another schema, nor of another connection; in every session that has the
// connection by name, whatever its Conn; what was read of the table is gone
// (F8.23).
func TestDropTableClosesItsTabs(t *testing.T) {
	a, report := withSessions()
	first := a.sess
	c := first.Conns[0]
	c.Tables = append(c.Tables, db.Table{Schema: "agentable", Name: "t_user"})
	other := readyConn("other")
	other.Tables = []db.Table{{Schema: "public", Name: "t_user"}}
	first.Conns = append(first.Conns, other)
	user := db.Table{Schema: "public", Name: "t_user"}
	report.Conns[0].Tables = []db.Table{user}
	idle := newConn(config.Connection{Name: "doraemon", Engine: "postgres"}) // listed, not dialed: it has no Meta to read a catalog with
	third := newSession("third", []*Conn{idle}, idle, a.freeQuery())
	a.sessions = append(a.sessions, third)
	a.attach(report)
	a.openTable(report.Conns[0], user)
	a.attach(first)
	a.openTable(c, user)
	a.openTable(c, db.Table{Schema: "agentable", Name: "t_user"})
	a.openTable(other, user)
	tabs := func(s *Session) (names []string) {
		for _, p := range s.panes() {
			for _, tb := range p.Tabs {
				if d := tb.Data; d != nil {
					names = append(names, d.conn.Spec.Name+"."+d.table.Schema)
				}
			}
		}
		return names
	}
	if got := tabs(first); !slices.Equal(got, []string{"doraemon.public", "doraemon.agentable", "other.public"}) {
		t.Fatalf("tabs %v", got)
	}
	c.cols[idOf(user)] = db.Columns{}
	report.Conns[0].cols[idOf(user)] = db.Columns{}
	a.spinOn = true // no tick of the dots among the commands counted below
	_, cmd := a.Update(dropped{c, user, nil})
	if got := tabs(first); !slices.Equal(got, []string{"doraemon.agentable", "other.public"}) {
		t.Errorf("first session: %v", got)
	}
	if got := tabs(report); len(got) != 0 {
		t.Errorf("the other session's: %v", got)
	}
	if _, ok := c.cols[idOf(user)]; ok {
		t.Error("what was read of the table stays")
	}
	if _, ok := report.Conns[0].cols[idOf(user)]; ok {
		t.Error("what the other session read of it stays")
	}
	if batch, ok := cmd().(tea.BatchMsg); !ok || len(batch) != 3 { // the toast, and the catalog of each of the two connections that can read it
		t.Errorf("%d commands, want the toast and two catalogs: %T", len(batch), batch)
	}
}

// Begun by the user, a transaction is not dropped into: MySQL's DROP would
// commit what it has, PG's would join it and the tree not know (F8.23).
func TestDropTableInTransaction(t *testing.T) {
	a := menuApp()
	a.sess.Conns[0].tx = 'T'
	treeTo(t, a, "t_user")
	if feed(t, a, "dd"); a.confirm != nil || a.toast != "事务中不能删表：先提交或回滚" {
		t.Errorf("asked %v, toast %q", a.confirm != nil, a.toast)
	}
}

// The error color is the title's, in a drop's question and in the one before a
// delete with no WHERE, and not the statement's (F8.23, F6.7).
func TestDangerTitleIsErrorColored(t *testing.T) {
	// the foreground of the first cell of the first row that says text
	fgOf := func(a *App, text string) any {
		f := a.render()
		first, _ := utf8.DecodeRuneInString(text)
		for y, l := range strings.Split(f.String(), "\n") {
			if !strings.Contains(l, text) {
				continue
			}
			for x := range a.w {
				if c := f.Buf.CellAt(x, y); c.String() == string(first) {
					return c.Style.Fg
				}
			}
		}
		return nil
	}
	titleFg := func(a *App, title, line string) (any, any) { return fgOf(a, title), fgOf(a, line) }
	a := menuApp()
	treeTo(t, a, "t_user")
	feed(t, a, "dd")
	if tf, lf := titleFg(a, "删除表 public.t_user", "DROP TABLE"); tf != a.theme.Error || lf == a.theme.Error {
		t.Errorf("drop: title %v, statement %v, error is %v", tf, lf, a.theme.Error)
	}
	d := dangerScene(t)
	if tf, lf := titleFg(d, "这次执行有", "delete from t_log"); tf != d.theme.Error || lf == d.theme.Error {
		t.Errorf("delete: title %v, statement %v, error is %v", tf, lf, d.theme.Error)
	}
}

// The menu takes what is asked of the app while it is open: C-p, an action run
// by name, the wheel over the tree, a resize; only C-c, which is esc, closes it (F8.23).
func TestMenuIsAlone(t *testing.T) {
	a := menuApp()
	a.View()
	rightClickNode(t, a, "t_user")
	feed(t, a, "k") // not above the first item
	if a.menu.sel != 0 {
		t.Fatalf("k on the first: %d", a.menu.sel)
	}
	for _, act := range []string{"palette.open", "window.new", "session.list", "tab.next"} {
		if a.run(act, 0); a.menu == nil || a.palette != nil || a.sessList != nil {
			t.Fatalf("%s: menu %v, palette %v", act, a.menu != nil, a.palette != nil)
		}
	}
	feed(t, a, "<C-p>")
	if a.menu == nil || a.palette != nil {
		t.Fatalf("C-p: menu %v, palette %v", a.menu != nil, a.palette != nil)
	}
	feed(t, a, "<C-c>")
	if a.menu != nil {
		t.Error("C-c does not close it")
	}
	rightClickNode(t, a, "t_user")
	a.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	if a.menu != nil {
		t.Error("a menu through a resize")
	}
	// the tree scrolls here: the wheel over it would move the cursor, not with a menu open
	a.View()
	rightClickNode(t, a, "t_user")
	at, _ := a.treeNode()
	r := nodeRect(t, a, "t_user")
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 2, Y: r.Min.Y, Button: tea.MouseWheelUp})
	if n, _ := a.treeNode(); n.id != at.id || a.menu == nil {
		t.Errorf("the wheel moved the cursor to %s or closed the menu", n.Text)
	}
	a.menu = nil
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 2, Y: r.Min.Y, Button: tea.MouseWheelUp})
	if n, _ := a.treeNode(); n.id == at.id {
		t.Error("the wheel does not move the cursor with no menu: the case is not a case")
	}
	// a session attached, as one dialed in the background is, takes it down
	a.menu = &menu{items: []menuItem{{"x", "", nil}}}
	a.attach(a.sess)
	if a.menu != nil {
		t.Error("a menu through an attach")
	}
}

// The box starts on the row under the pointer, the node clicked in view; where it does not
// fit below it ends on the row above (F8.23).
func TestMenuPlacement(t *testing.T) {
	for _, h := range []int{45, 22} {
		a := menuApp()
		a.Update(tea.WindowSizeMsg{Width: 160, Height: h})
		a.View()
		var below, above bool
		ns, _ := a.treeNodes()
		for _, hit := range a.hits {
			if hit.Target.Kind != ui.KindNode || ns[hit.Target.I].kind != nodeTable {
				continue
			}
			a.menu = nil
			a.Update(tea.MouseClickMsg{X: hit.Rect.Min.X + 3, Y: hit.Rect.Min.Y, Button: tea.MouseRight})
			if a.menu == nil {
				t.Fatalf("h %d: no menu on %s", h, ns[hit.Target.I].Text)
			}
			a.View()
			first := find(t, a, ui.Target{Kind: ui.KindRow, I: 0}).Min.Y
			y := hit.Rect.Min.Y
			fits := y+1+len(a.menu.items)+2 <= a.window().Max.Y
			want := y - len(a.menu.items) - 1
			if fits {
				want = y + 2
			}
			if first != want {
				t.Fatalf("h %d, pointer row %d: first item on row %d, want %d (fits below: %v)", h, y, first, want, fits)
			}
			below, above = below || fits, above || !fits
			a.Update(tea.WindowSizeMsg{Width: 160, Height: h}) // closes it, for the next
			a.View()
		}
		if h == 45 && !below || h == 22 && !(below && above) {
			t.Errorf("h %d: below %v, above %v: the cases were not met", h, below, above)
		}
	}
}

// A right click with a menu open closes it, and goes on to the node under the
// pointer; on the menu itself it only closes it (F8.23).
func TestMenuRightClickWhileOpen(t *testing.T) {
	a := menuApp()
	a.View()
	rightClickNode(t, a, "t_user")
	a.View()
	rightClickNode(t, a, "t_order")
	if n, _ := a.treeNode(); a.menu == nil || n.Text != "t_order" || !slices.Equal(menuLabels(a)[:3], []string{"打开", "在新 tab 打开", "删除表…"}) {
		t.Fatalf("on another node: menu %v, cursor %q", a.menu != nil, n.Text)
	}
	a.View()
	r := find(t, a, ui.Target{Kind: ui.KindRow, I: 1})
	a.Update(tea.MouseClickMsg{X: r.Min.X + 2, Y: r.Min.Y, Button: tea.MouseRight})
	if n, _ := a.treeNode(); a.menu != nil || n.Text != "t_order" {
		t.Errorf("on the menu: menu %v, cursor %q", a.menu != nil, n.Text)
	}
	a.View()
	rightClickNode(t, a, "t_order")
	a.View()
	a.Update(tea.MouseClickMsg{X: 100, Y: 30, Button: tea.MouseRight})
	if a.menu != nil {
		t.Error("off the tree: a menu is open")
	}
}

// The right click goes to the tree whichever pane has the focus, and not at
// all while a cell's edit cannot end (F8.23).
func TestMenuRightClickFocusAndEdit(t *testing.T) {
	a := menuApp()
	a.win().focus(a.win().Root.Leaves()[1].ID) // the query
	a.View()
	rightClickNode(t, a, "t_user")
	if n, _ := a.treeNode(); a.menu == nil || a.win().Focus != 0 || n.Text != "t_user" {
		t.Errorf("from the query: menu %v, focus %d, cursor %q", a.menu != nil, a.win().Focus, n.Text)
	}
	b := wide(160, 45)
	loadOrders(t, b, 10)
	feed(t, b, "ix")
	if tab := b.typingTab(); tab == nil || tab.cell == nil || tab.cellHint() == "" {
		t.Fatal("no cell edit that cannot end")
	}
	b.View()
	rightClickNode(t, b, "t_user")
	if b.menu != nil {
		t.Error("a menu while a cell's edit cannot end")
	}
}

// Not dropped on a read-only connection, nor in the manual transaction mode
// (F8.23).
func TestDropTableRefused(t *testing.T) {
	for _, c := range []struct {
		set  func(c *Conn)
		want string
	}{
		{func(c *Conn) { c.readOnly = true }, "doraemon 是只读连接"},
		{func(c *Conn) { c.Manual = true }, "手动事务模式下不能删表：先提交或回滚，或切回自动"},
	} {
		a := menuApp()
		c.set(a.sess.Conns[0])
		treeTo(t, a, "t_user")
		a.win().focus(0)
		if feed(t, a, "dd"); a.confirm != nil || a.toast != c.want {
			t.Errorf("%q: asked %v, toast %q", c.want, a.confirm != nil, a.toast)
		}
	}
}

// The menu of a table, at the pointer, its first item selected (F8.23).
func TestGoldenTableMenu(t *testing.T) {
	a := menuApp()
	a.View()
	rightClickNode(t, a, "t_user")
	golden.RequireEqual(t, a.render().String())
	a.icons = ui.ASCIIIcons
	t.Run("ascii", func(t *testing.T) { golden.RequireEqual(t, a.render().String()) })
}

// The tree's filter has the keys while it is typed in, the menu opened over
// it takes them: what it has no key for is not typed into the filter (F8.23).
func TestMenuKeysAreNotTypedInTheFilter(t *testing.T) {
	a := menuApp()
	feed(t, a, "/")
	if !a.win().tree.filtering {
		t.Fatal("the filter is not typed in")
	}
	a.View()
	rightClickNode(t, a, "t_user")
	if a.menu == nil {
		t.Fatal("no menu")
	}
	feed(t, a, "xyz")
	if got := a.win().tree.filter.Text; got != "" {
		t.Errorf("the filter has %q", got)
	}
}

// The wheel over a modal overlay does not scroll the table under it: the COLS
// and ORDER lists, a dropdown, the palette, the sessions and the connection
// manager, a question; without one it does (F13.7).
func TestOverlayWheelIsModal(t *testing.T) {
	wheel := func(a *App) {
		r := a.layout()[a.focused().ID]
		a.Update(tea.MouseWheelMsg{X: r.Min.X + 3, Y: r.Min.Y + 8, Button: tea.MouseWheelDown})
		a.Update(tea.MouseWheelMsg{X: r.Min.X + 3, Y: r.Min.Y + 8, Button: tea.MouseWheelDown})
	}
	a, tab, _ := withRecorder(t)
	top := tab.top
	if wheel(a); tab.top == top {
		t.Fatal("the wheel scrolls no table with no overlay open")
	}
	for name, open := range map[string]func(a *App){
		"cols":     func(a *App) { a.run("grid.cols", 0) },
		"order":    func(a *App) { a.run("grid.order", 0) },
		"dropdown": func(a *App) { a.openDrop(dropLimit) },
		"palette":  func(a *App) { feed(t, a, "<C-p>") },
		"sessions": func(a *App) { a.run("session.list", 0) },
		"form":     func(a *App) { a.openConnForm() },
		"confirm":  func(a *App) { a.confirm = &confirmBox{text: "?", yes: "y", then: func() tea.Cmd { return nil }} },
	} {
		a, tab, _ := withRecorder(t)
		open(a)
		if a.topOverlay() == "" {
			t.Fatalf("%s: not open", name)
		}
		top := tab.top
		if wheel(a); tab.top != top {
			t.Errorf("%s: the wheel scrolled the table under it, %d -> %d", name, top, tab.top)
		}
	}
}

// The palette over a WHERE being typed takes what is pasted, not the WHERE
// under it (F13.13).
func TestPasteGoesToPaletteOverWhere(t *testing.T) {
	a, tab, _ := withRecorder(t)
	feed(t, a, "/id<Esc>") // its NORMAL: C-p is no candidate's up here
	feed(t, a, "<C-p>")
	if a.palette == nil || tab.typing != "where" {
		t.Fatalf("palette %v typing %q", a.palette != nil, tab.typing)
	}
	a.Update(tea.PasteMsg{Content: "zz"})
	if tab.where.Text != "id" || a.palette.input.Text != "zz" {
		t.Errorf("the paste went under the palette: WHERE %q, palette %q", tab.where.Text, a.palette.input.Text)
	}
}

// j and k stop at the ends of the menu, however many keys come (a table's has
// eight items since the structure templates).
func TestMenuMoveStopsAtTheEnds(t *testing.T) {
	a := menuApp()
	a.View()
	rightClickNode(t, a, "t_order")
	last := len(a.menu.items) - 1
	feed(t, a, strings.Repeat("j", last+5))
	if a.menu.sel != last {
		t.Errorf("j past the end: %d, want %d", a.menu.sel, last)
	}
	feed(t, a, strings.Repeat("k", last+5))
	if a.menu.sel != 0 {
		t.Errorf("k past the start: %d", a.menu.sel)
	}
}

// Every item of the menus has the icon §7.6 gives it, one a theme's [icon] can
// name; the tree's menus, a table's, a view's, a query's, a Redis key's, a
// namespace's and a db's, and the grid's, a cell's and a head's (F16.11).
func TestMenuItemsHaveIcons(t *testing.T) {
	want := map[string]string{
		"打开": "open", "在新 tab 打开": "open", "删除…": "trash", "重命名…": "rename", "删除表…": "trash", "删除视图…": "trash",
		"改名…": "rename", "设 TTL…": "auto_refresh", "刷新": "refresh",
		"加列…": "column", "加索引…": "index", "删除列…": "hide", "删除索引…": "trash",
		"在值编辑器中打开": "edit", "按这个值过滤": "filter", "按这一列排序 ↑": "sort_asc", "按这一列排序 ↓": "sort_desc", "加入排序": "sort_asc",
		"隐藏这一列": "hide", "复制这一格": "copy", "复制行为 JSON": "copy", "以 JSON 查看": "json", "导出…": "export",
	}
	a := menuApp()
	c := a.sess.Conns[0]
	ns, _ := a.treeNodes()
	var menus [][]menuItem
	for _, n := range ns {
		if n.kind == nodeTable || n.kind == nodeQueryFile {
			menus = append(menus, a.menuItems(n))
		}
	}
	redisConn := &Conn{Spec: config.Connection{Engine: "redis"}}
	for _, n := range []node{{kind: nodeRedisKey, conn: c}, {kind: nodeNamespace, conn: c}, {kind: nodeDatabase, conn: redisConn}} {
		menus = append(menus, a.menuItems(n))
	}
	a2, tab, _ := withRecorder(t)
	menus = append(menus, a2.columnItems(tab, true), a2.columnItems(tab, false))
	items := 0
	for _, m := range menus {
		for _, it := range m {
			items++
			if it.icon != want[it.label] || a.icons.Named(it.icon).Text == "" {
				t.Errorf("%s: icon %q, want %q", it.label, it.icon, want[it.label])
			}
		}
	}
	if items < 30 {
		t.Errorf("only %d items looked at", items)
	}
	if a.icons.Named("json") != a.icons.KeyType("ReJSON-RL") { // the menu's JSON is the key type's, and keeps its name
		t.Error("json is not the Redis key type's icon")
	}
}

// The icon of a menu item is dim, as is, and as the text on the row picked; the
// color a theme gives it is its own on either (F16.11, §7.7).
func TestMenuIconColors(t *testing.T) {
	glyph := func(a *App, g string) *uv.Cell {
		f := a.render()
		for y := range f.Bounds().Dy() {
			for x := range f.Bounds().Dx() {
				if c := f.Buf.CellAt(x, y); c != nil && c.Content == g {
					return c
				}
			}
		}
		t.Fatalf("no %q drawn", g)
		return nil
	}
	a := menuApp()
	rightClickNode(t, a, "t_user")
	if got := glyph(a, a.icons.Open.Text).Style.Fg; got != a.theme.Fg { // 打开, picked
		t.Errorf("picked: %v, want the text's %v", got, a.theme.Fg)
	}
	if got := glyph(a, a.icons.Trash.Text).Style.Fg; got != a.theme.Dim { // 删除表…, not
		t.Errorf("not picked: %v, want dim %v", got, a.theme.Dim)
	}
	themed := *a.icons
	themed.Open.Fg, themed.Trash.Fg = color.RGBA{R: 1, A: 255}, color.RGBA{G: 1, A: 255}
	a.icons = &themed
	if got := glyph(a, a.icons.Open.Text).Style.Fg; got != themed.Open.Fg {
		t.Errorf("picked, themed: %v", got)
	}
	if got := glyph(a, a.icons.Trash.Text).Style.Fg; got != themed.Trash.Fg {
		t.Errorf("not picked, themed: %v", got)
	}
}
