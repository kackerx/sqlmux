package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// menu is the right-click menu over a tree node (§7.8「右键菜单」, F8.23): the
// node is under the tree's cursor, which is what the items act on; or over a
// grid's cell or head (§7.6, F12.3), the cursor on it.
type menu struct {
	at    uv.Position // the pointer
	items []menuItem
	sel   int
	top   int // the first row shown, held while the selection is in view (F13.10)
}

type menuItem struct {
	label string
	icon  string // its name in a theme's [icon] (§7.7)
	run   func() tea.Cmd
}

func inMenu(a *App) bool { return a.menu != nil }

// menuItems is what n offers; none for a node that has no menu.
func (a *App) menuItems(n node) []menuItem {
	switch n.kind {
	case nodeQueryFile:
		return []menuItem{
			{i18n.T("打开"), "open", func() tea.Cmd { return a.treeOpen(false) }},
			{i18n.T("重命名…"), "rename", a.renameQueryFile},
			{i18n.T("删除…"), "trash", a.deleteQuery},
		}
	case nodeTable:
		drop := i18n.T("删除表…")
		if n.table.View() {
			drop = i18n.T("删除视图…")
		}
		items := []menuItem{
			{i18n.T("打开"), "open", func() tea.Cmd { return a.treeOpen(false) }},
			{i18n.T("在新 tab 打开"), "open", func() tea.Cmd { return a.treeOpen(true) }},
			{i18n.T("属性"), "table", func() tea.Cmd { return a.openProps(n.conn, n.table) }},
			{drop, "trash", a.dropTable},
		}
		return append(items, a.alterItems(n)...)
	case nodeRedisKey:
		return []menuItem{
			{i18n.T("打开"), "open", func() tea.Cmd { return a.treeOpen(false) }},
			{i18n.T("删除…"), "trash", func() tea.Cmd { return a.deleteRedisKey(n) }},
			{i18n.T("改名…"), "rename", func() tea.Cmd { return a.renameRedisKey(n) }},
			{i18n.T("设 TTL…"), "auto_refresh", func() tea.Cmd { return a.ttlKey(n) }},
		}
	case nodeNamespace:
		return []menuItem{{i18n.T("删除…"), "trash", func() tea.Cmd { return a.deleteNamespace(n) }}}
	case nodeDatabase:
		if n.conn.redis() {
			return []menuItem{{i18n.T("刷新"), "refresh", func() tea.Cmd { return a.loadKeys(n.conn) }}}
		}
		return a.pickItem(n)
	case nodeConn:
		return append(a.pickItem(n), a.connItems(n.conn)...)
	case nodeSchema, nodeGroup, nodeColumn, nodeObjects, nodeIndex, nodeKey, nodeFunc, nodeSequence, nodeWorkspace, nodeWindow, nodeTab, nodeQueries, nodeSep, nodeNote: // no menu
	}
	return nil
}

// rightClick opens the menu of the tree node under the pointer, the cursor
// put on it. One open is closed first: a click on it is no more, one off it
// goes on to the node under the pointer, which its backdrop covers.
func (a *App) rightClick(p uv.Position) {
	hits := a.hits
	if a.menu != nil {
		a.menu = nil
		hits = slices.DeleteFunc(slices.Clone(a.hits), func(h ui.Hit) bool { return h.Target.Kind == ui.KindBackdrop })
	}
	t, _ := ui.HitAt(hits, p)
	if t.Kind != ui.KindNode && t.Kind != ui.KindFold && t.Kind != ui.KindCell && t.Kind != ui.KindHead || a.overlaid() || !a.endEdit() { // on the ▸ too
		return
	}
	if t.Kind == ui.KindCell || t.Kind == ui.KindHead { // a grid's (F12.3)
		a.gridMenu(t, p)
		return
	}
	a.focusPane(t.Pane)
	a.treeGo(t.I)
	if n, ok := a.treeNode(); ok {
		if items := a.menuItems(n); len(items) > 0 {
			a.menu = &menu{at: p, items: items}
		}
	}
}

// menuMove moves the selection by d, to the ends and no further.
func (a *App) menuMove(d int) {
	a.menu.sel = max(min(a.menu.sel+d, len(a.menu.items)-1), 0)
}

// menuRun closes the menu and runs its item i.
func (a *App) menuRun(i int) tea.Cmd {
	m := a.menu
	a.menu = nil
	return m.items[i].run()
}

// drawMenu paints the menu in a Complete's box, its left edge in the
// pointer's column, moved over when it does not fit; a click off it closes it.
func (a *App) drawMenu(f *ui.Frame) {
	m := a.menu
	v, w := ui.Complete{Sel: m.sel}, 0
	for i, it := range m.items {
		var icon ui.Icon
		lead := ""
		if !a.icons.Labeled { // ascii says it in words alone
			icon = a.icons.Named(it.icon)
			lead = icon.Text + " "
			if i == m.sel { // dim, as Complete draws an icon of no color, but the picked row's is as its text
				icon.Fg = cmp.Or(icon.Fg, a.theme.Fg)
			}
		}
		v.Items = append(v.Items, ui.CompleteItem{Icon: icon, Text: it.label})
		w = max(w, ui.Width(lead+it.label)+5) // a border, a blank and the two cells Complete leaves on the right
	}
	// from the row under the pointer; over it, ending on the row above, when it does not fit below: the node clicked is seen
	box, rows := ui.CompleteBox(a.window(), m.at, w, len(v.Items), 0, len(v.Items))
	_, m.top = ui.ListView(m.sel, m.top, len(v.Items), rows) // a window too short for all of them scrolls with the selection
	v.Top = m.top
	f.Region(f.Bounds(), ui.Target{Kind: ui.KindBackdrop})
	v.Draw(f, box, rows)
}

// renameQueryFile is the menu's rename on a query file: the query is
// gone to, opened first when it is not, and asked its name as query.rename
// does.
// ponytail: a file not open is opened to be renamed; rename the file alone if
// that bothers.
func (a *App) renameQueryFile() tea.Cmd {
	n, ok := a.treeNode()
	if !ok || n.kind != nodeQueryFile {
		return nil
	}
	cmd := a.openQueryFile(n.query)
	a.askQueryName()
	return cmd
}

// treeDelete is tree.delete (dd): a query file's deletion, a table's or a
// view's drop, a Redis key's or a namespace's delete, a connection's; on any
// other node it does nothing.
func (a *App) treeDelete() tea.Cmd {
	n, ok := a.treeNode()
	switch {
	case ok && n.kind == nodeTable:
		return a.dropTable()
	case ok && n.kind == nodeRedisKey:
		return a.deleteRedisKey(n)
	case ok && n.kind == nodeNamespace:
		return a.deleteNamespace(n)
	case ok && n.kind == nodeConn:
		return a.treeDeleteConn(n.conn)
	}
	return a.deleteQuery()
}

// connItems are the menu's entries on connection c: the manager on it, its
// deletion (F18.14). A connection of the command line is in no file: none;
// the manager does not list an exec one (F17.7).
func (a *App) connItems(c *Conn) []menuItem {
	var items []menuItem
	if !c.Spec.Temp && c.Spec.Engine != "exec" {
		items = append(items, menuItem{i18n.T("编辑…"), "edit", func() tea.Cmd { a.manage(c); return nil }})
	}
	if !c.Spec.Temp {
		items = append(items, menuItem{i18n.T("删除…"), "trash", func() tea.Cmd { return a.treeDeleteConn(c) }})
	}
	return items
}

// treeDeleteConn is the delete of connection c from the tree: the manager's,
// of the block connections.toml has now.
func (a *App) treeDeleteConn(c *Conn) tea.Cmd {
	if c.Spec.Temp {
		return nil
	}
	spec, err := config.ReadConnection(c.Spec.Name)
	if err != nil {
		return a.showToast(err.Error())
	}
	return a.deleteConnection(spec)
}

// dropSQL is the statement that drops t, as its kind is dropped (§7.8「右键菜单」).
func dropSQL(eng db.Engine, t db.Table) string {
	kind := "TABLE"
	switch t.Kind {
	case "v":
		kind = "VIEW"
	case "m":
		kind = "MATERIALIZED VIEW"
	case "f":
		kind = "FOREIGN TABLE"
	}
	return fmt.Sprintf("DROP %s %s", kind, eng.Quote(t.Schema, t.Name))
}

// dropped answers dropTable.
type dropped struct {
	conn  *Conn
	table db.Table
	err   error
}

// dropTable is tree.delete on a table or a view, and the menu's: after a
// question that shows the DROP, whatever confirm_dangerous says, it runs on
// the connection's Main. In the manual transaction mode it does not: a
// DROP would either join a transaction the catalog does not see, or, MySQL's,
// commit what is pending (F8.23).
func (a *App) dropTable() tea.Cmd {
	n, ok := a.treeNode()
	if !ok || n.kind != nodeTable {
		return nil
	}
	c, t := n.conn, n.table
	switch {
	case c.readOnly:
		return a.showToast(i18n.T("%s 是只读连接", c.Spec.Name))
	case c.Manual:
		return a.showToast(i18n.T("手动事务模式下不能删表：先提交或回滚，或切回自动"))
	case c.inTx(): // begun by the user in a query: MySQL's DROP would commit it, PG's join it
		return a.showToast(i18n.T("事务中不能删表：先提交或回滚"))
	}
	sql := dropSQL(c.eng, t)
	title := i18n.T("删除表 %s，不能撤销：", c.qualified(t))
	if t.View() {
		title = i18n.T("删除视图 %s，不能撤销：", c.qualified(t))
	}
	a.confirm = writeBox(c, title+"\n  "+sql, i18n.T("删除"), true, func() tea.Cmd {
		main := c.Main
		c.busy++
		return func() tea.Msg {
			err := main.Run(context.Background(), func(ctx context.Context, conn db.Conn) error {
				_, err := conn.Exec(ctx, sql, 0)
				return err
			})
			return dropped{c, t, err}
		}
	})
	return nil
}

// gotDropped closes the tabs of the table dropped, in every session, loads
// the catalog again and says so; a failure is the server's message, or the
// cancel's, and the catalog is read again then too: the server may have
// dropped it after all (the connection lost as it was sent).
func (a *App) gotDropped(m dropped) tea.Cmd {
	c := m.conn
	c.busy--
	if m.err != nil {
		if c.drv.Broken(m.err) != db.NotLost { // the connection event says it: reconnected, run it again
			return a.loadCatalog(c)
		}
		e := c.drv.ErrorOf(m.err)
		msg := e.Message
		if errors.Is(m.err, context.Canceled) {
			msg = i18n.T("查询已取消")
		}
		if len(e.More) > 0 && strings.HasPrefix(e.More[0], "DETAIL: ") { // what depends on it
			msg += " · " + e.More[0]
		}
		return tea.Batch(a.showToast(msg), a.loadCatalog(c))
	}
	cmds := []tea.Cmd{a.showToast(i18n.T("已删除 %s", c.qualified(m.table)))}
	for _, s := range a.sessions { // a session has its own Conn of a connection's name
		for _, o := range s.conns() {
			if o.Spec.Name != c.Spec.Name || o.Spec.Database != c.Spec.Database {
				continue
			}
			for _, p := range s.panes() {
				for i := len(p.Tabs) - 1; i >= 0; i-- {
					if d := p.Tabs[i].Data; d != nil && d.conn == o && idOf(d.table) == idOf(m.table) {
						dropTab(p, i)
					}
				}
			}
			o.dropCols()
			if o.State == connReady {
				cmds = append(cmds, a.loadCatalog(o))
			}
		}
	}
	return tea.Batch(cmds...)
}
