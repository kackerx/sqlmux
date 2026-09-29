package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// treeState is one window's schema tree (§7.8); what it shows is the
// session's.
type treeState struct {
	cursor, top int    // into the nodes showing
	at          string // the cursor's node: it stays on it as others come and go
	filter      ui.Input
	filtering   bool // the filter row has the keys (INSERT)
}

// catalogMsg is what loadCatalog read.
type catalogMsg struct {
	schemas []string
	current string
	tables  []db.Table
	err     error
}

// loadCatalog reads the schemas and every schema's tables on Meta; a
// table's columns wait until it is opened (§8.4).
func (a *App) loadCatalog() tea.Cmd {
	meta := a.sess.Meta
	return func() tea.Msg {
		ctx := context.Background()
		schemas, current, err := postgres.Schemas(ctx, meta)
		if err != nil {
			return catalogMsg{err: err}
		}
		tables, err := postgres.Tables(ctx, meta)
		return catalogMsg{schemas: schemas, current: current, tables: tables, err: err}
	}
}

func (a *App) gotCatalog(m catalogMsg) tea.Cmd {
	if m.err != nil {
		return a.showToast(m.err.Error(), toastTTL)
	}
	s := a.sess
	s.Schemas, s.Tables = m.schemas, m.tables
	if s.home = m.current; s.home == "" && len(s.Schemas) > 0 {
		s.home = s.Schemas[0]
	}
	if !slices.Contains(s.Schemas, s.Schema) { // the first load, or the schema is gone (§7.8)
		s.Schema = s.home
	}
	for _, w := range s.Windows { // the consoles opened before the tree had a schema (§8.6)
		for _, p := range w.Root.Leaves() {
			for _, t := range p.Tabs {
				if t.Console != nil && t.Console.schema == "" {
					t.Console.schema = s.Schema
				}
			}
		}
	}
	a.clampTree()
	var cmds []tea.Cmd
	for _, t := range s.Tables { // R dropped the columns; an open table keeps showing them (§7.8)
		if _, ok := s.cols[idOf(t)]; !ok && a.opened(tableNode(t), false) {
			cmds = append(cmds, a.fetchCols(t))
		}
	}
	return tea.Batch(cmds...)
}

// tableNode is table t's node ID.
func tableNode(t db.Table) string { return "table:" + t.Schema + "." + t.Name }

// nodeKind is what a tree node stands for (§7.8).
type nodeKind int

const (
	nodeSession nodeKind = iota
	nodeSchema
	nodeGroup // Tables or Views
	nodeTable // a table or a view
	nodeColumn
	nodeWorkspace
	nodeWindow
	nodePane
	nodeTab
)

// node is one of the tree's rows: what it stands for, and how it draws.
type node struct {
	ui.TreeNode
	id     string // stable while the node exists: expansion and the cursor go by it
	kind   nodeKind
	schema string   // the schema it is under; "" outside them
	table  db.Table // a table's, or a column's table
	column string
	win    int
	pane   *Pane
	tab    int
}

// opened is whether node id shows its children: as the user left it, else
// def, §7.8's default.
func (a *App) opened(id string, def bool) bool {
	if v, ok := a.sess.open[id]; ok {
		return v
	}
	return def
}

// treeNodes is what the tree shows, top to bottom (§7.8): the session's
// schemas, their Tables and Views, tables and, opened, their columns; then
// the workspace's windows, panes and tabs. A filter keeps the tables and
// views it matches with the nodes above them, opened; matches is how many.
//
// ponytail: rebuilt on every call, 4–5 times a key press. Measured at
// 160×45: 1.7ms a call at 10 schemas × 100 tables, 4ms at 50 × 100, 19ms at
// 20 × 1000 (46ms filtering), so 5 / 16 / 90ms a key. Build it once per
// Update, or group the tables by schema up front, if catalogs that big turn up.
func (a *App) treeNodes() (ns []node, matches int) {
	s, ic, th := a.sess, a.icons, a.theme
	filter := a.win().tree.filter.Text
	pos := map[tableID][]int{} // what the filter kept
	names := make([]string, len(s.Tables))
	for i, t := range s.Tables {
		names[i] = t.Name
	}
	for _, m := range ui.Filter(filter, names) {
		pos[idOf(s.Tables[m.Index])] = m.Pos
	}
	filtering := filter != ""
	open := func(id string, def bool) bool { return filtering || a.opened(id, def) }
	var current tableID // the table ↵ lands on, and its tab (§7.8)
	target := a.openTarget()
	if t := dataOf(target); t != nil {
		current = idOf(t.table)
	}
	add := func(n node) bool {
		ns = append(ns, n)
		return n.Branch && n.Open
	}

	if add(node{id: "session", kind: nodeSession, TreeNode: ui.TreeNode{Branch: true, Open: open("session", true), Icon: ic.Postgres, IconFg: th.Info, Text: s.Name}}) {
		for _, sc := range s.Schemas {
			var groups [2][]db.Table // Tables, Views
			for _, t := range s.Tables {
				if t.Schema == sc {
					groups[b2i(t.View())] = append(groups[b2i(t.View())], t)
				}
			}
			kept := func(ts []db.Table) (out []db.Table) {
				for _, t := range ts {
					if _, ok := pos[idOf(t)]; ok {
						out = append(out, t)
					}
				}
				return out
			}
			if len(kept(groups[0]))+len(kept(groups[1])) == 0 && filtering {
				continue
			}
			id := "schema:" + sc
			if !add(node{id: id, kind: nodeSchema, schema: sc, TreeNode: ui.TreeNode{Depth: 1, Branch: true, Open: open(id, sc == s.home), Icon: ic.Schema, IconFg: th.PK, Text: sc}}) {
				continue
			}
			for g, ts := range groups {
				label, icon, gid := "Tables", ic.Table, id+"/tables"
				if g == 1 {
					label, icon, gid = "Views", ic.View, id+"/views"
				}
				shown := kept(ts)
				if filtering && len(shown) == 0 {
					continue
				}
				gn := node{id: gid, kind: nodeGroup, schema: sc, TreeNode: ui.TreeNode{Depth: 2, Branch: len(ts) > 0, Open: open(gid, sc == s.home && g == 0), Icon: icon, IconFg: th.Func, Text: fmt.Sprintf("%s (%d)", label, len(ts))}}
				if !add(gn) {
					continue
				}
				for _, t := range shown {
					tid := tableNode(t)
					rows := ui.Magnitude(t.Rows)
					if t.Kind == "v" { // a plain view has no rows to count
						rows = ""
					}
					tn := node{id: tid, kind: nodeTable, schema: sc, table: t, TreeNode: ui.TreeNode{
						Depth: 3, Branch: true, Open: !filtering && a.opened(tid, false), Icon: icon, IconFg: th.Func, Text: t.Name, Pos: pos[idOf(t)],
						Note: rows, NoteFg: th.Border, Current: idOf(t) == current,
					}}
					if !add(tn) {
						continue
					}
					cols := s.cols[idOf(t)] // none until fetched: no children, no "loading" (§7.8)
					for _, c := range cols.Cols {
						cn := node{id: tid + "/" + c.Name, kind: nodeColumn, schema: sc, table: t, column: c.Name, TreeNode: ui.TreeNode{
							Depth: 4, Icon: ic.Column, IconFg: th.Dim, Text: c.Name, Note: c.Type, NoteFg: th.Dim,
						}}
						if slices.Contains(cols.PK, c.Name) {
							cn.Icon, cn.IconFg = ic.Key, th.PK
						}
						add(cn)
					}
				}
			}
		}
	}
	if filtering { // the workspace isn't what a filter looks for (§7.8)
		return ns, len(pos)
	}
	if !add(node{id: "workspace", kind: nodeWorkspace, TreeNode: ui.TreeNode{Branch: true, Open: a.opened("workspace", true), Icon: ic.Window, IconFg: th.Info, Text: "工作区"}}) {
		return ns, len(pos)
	}
	for wi, w := range s.Windows {
		var leaves []*Pane
		if w.Root != nil {
			leaves = w.Root.Leaves()
		}
		wid := fmt.Sprintf("window:%d", wi)
		if !add(node{id: wid, kind: nodeWindow, win: wi, TreeNode: ui.TreeNode{Depth: 1, Branch: len(leaves) > 0, Open: a.opened(wid, true), Icon: ic.Window, IconFg: th.Info, Text: w.Name}}) {
			continue
		}
		for n, p := range leaves { // numbered by ⟨n⟩: the sidebar is 0; no type, the tabs have theirs (§5)
			pid := fmt.Sprintf("%s/pane:%d", wid, p.ID)
			if !add(node{id: pid, kind: nodePane, win: wi, pane: p, TreeNode: ui.TreeNode{Depth: 2, Branch: len(p.Tabs) > 0, Open: a.opened(pid, true), Icon: ui.Icon{Text: ic.Number(n + 1)}}}) {
				continue
			}
			for i, tb := range p.Tabs {
				icon, _ := a.tabIcon(&p.Tabs[i])
				// a blank for a landing tab's: its name in line with the others'
				icon.Text = cmp.Or(icon.Text, " ")
				fg := th.Info // a console's
				if tb.Data != nil {
					fg = th.Func
				}
				tn := node{id: fmt.Sprintf("%s/tab:%d", pid, i), kind: nodeTab, win: wi, pane: p, tab: i, TreeNode: ui.TreeNode{
					Depth: 3, Icon: icon, IconFg: fg, Text: tb.Name,
					Current: wi == s.Active && p == target && i == p.Cur,
				}}
				if tb.Data != nil {
					tn.Aside = strings.Join(tabSummary(tb.Data), " · ")
				}
				add(tn)
			}
		}
	}
	return ns, len(pos)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// treeRows is how many nodes the sidebar shows.
func (a *App) treeRows() int { return ui.TreeRows(a.sidebarRect().Dy() - 2) }

// treeView is cursor and top kept on a list of n and in a view of rows.
func treeView(cursor, top, n, rows int) (int, int) {
	cursor = max(min(cursor, n-1), 0)
	return cursor, max(min(top, cursor, n-rows), cursor-rows+1, 0)
}

// treeAt is where the cursor is among ns, on its node while that shows,
// else on the row it was on; and the first row the view shows.
func (a *App) treeAt(ns []node) (cursor, top int) {
	t := a.win().tree
	if i := slices.IndexFunc(ns, func(n node) bool { return n.id == t.at }); i >= 0 {
		t.cursor = i
	}
	return treeView(t.cursor, t.top, len(ns), a.treeRows())
}

// clampTree settles the cursor on a node; its schema is the tree's
// schema from now on, until the cursor is under another (§7.8).
func (a *App) clampTree() {
	ns, _ := a.treeNodes()
	t := &a.win().tree
	t.cursor, t.top = a.treeAt(ns)
	if t.cursor < len(ns) {
		n := ns[t.cursor]
		if t.at = n.id; n.schema != "" {
			a.sess.Schema = n.schema
		}
	}
}

// treeGo puts the cursor on row i.
func (a *App) treeGo(i int) {
	t := &a.win().tree
	t.cursor, t.at = i, ""
	a.clampTree()
}

func (a *App) treeMove(d int) {
	ns, _ := a.treeNodes()
	cur, _ := a.treeAt(ns)
	a.treeGo(cur + d)
}

// treeNode is the node under the cursor.
func (a *App) treeNode() (node, bool) {
	ns, _ := a.treeNodes()
	if cur, _ := a.treeAt(ns); cur < len(ns) {
		return ns[cur], true
	}
	return node{}, false
}

// scrollTree moves the view, not the cursor, which is only pulled back into
// it, as in nvim (§7.8).
func (a *App) scrollTree(notches int) {
	ns, _ := a.treeNodes()
	t := &a.win().tree
	cur, _ := a.treeAt(ns)
	rows := a.treeRows()
	t.top = max(min(t.top+notches*wheelStep, len(ns)-rows), 0)
	a.treeGo(max(min(cur, t.top+rows-1), t.top))
}

// treeFold opens or closes n; a table opening fetches its columns unless
// the catalog has them (§8.4).
func (a *App) treeFold(n node, open bool) tea.Cmd {
	if !n.Branch {
		return nil
	}
	if a.sess.open == nil {
		a.sess.open = map[string]bool{}
	}
	a.sess.open[n.id] = open
	a.clampTree()
	if _, ok := a.sess.cols[idOf(n.table)]; open && n.kind == nodeTable && !ok {
		return a.fetchCols(n.table)
	}
	return nil
}

// treeExpand is l: a closed node opens, an open one hands the cursor to its
// first child; a leaf does nothing (§7.8).
func (a *App) treeExpand() tea.Cmd {
	n, ok := a.treeNode()
	switch {
	case !ok || !n.Branch:
	case !n.Open:
		return a.treeFold(n, true)
	default:
		ns, _ := a.treeNodes()
		if cur, _ := a.treeAt(ns); cur+1 < len(ns) && ns[cur+1].Depth > n.Depth {
			a.treeGo(cur + 1)
		}
	}
	return nil
}

// treeCollapse is h: an open node closes; a closed one or a leaf hands the
// cursor to its parent (§7.8).
func (a *App) treeCollapse() tea.Cmd {
	n, ok := a.treeNode()
	if !ok {
		return nil
	}
	if n.Branch && n.Open {
		return a.treeFold(n, false)
	}
	ns, _ := a.treeNodes()
	cur, _ := a.treeAt(ns)
	for i := cur - 1; i >= 0; i-- {
		if ns[i].Depth < n.Depth {
			a.treeGo(i)
			break
		}
	}
	return nil
}

// treeOpen is ↵ (and t, newTab) on the cursor's node (§7.8): a table opens,
// a column opens its table on that column, a pane or tab of this window is
// switched to; the rest open and close. t only opens tables.
func (a *App) treeOpen(newTab bool) tea.Cmd {
	n, ok := a.treeNode()
	switch {
	case !ok:
	case n.kind == nodeTable:
		return a.openTable(n.table, newTab)
	case newTab:
	case n.kind == nodeColumn:
		cmd := a.openTable(n.table, false)
		a.gotoColumn(n.table, n.column)
		return cmd
	case n.kind == nodePane && n.win == a.sess.Active:
		a.showPane(n.pane.ID)
	case n.kind == nodeTab && n.win == a.sess.Active:
		a.showTab(tabAt{p: n.pane, i: n.tab})
	case n.kind != nodePane && n.kind != nodeTab: // a window's switch waits for M5
		return a.treeFold(n, !n.Open)
	}
	return nil
}

// gotoColumn puts the cursor of the table just opened on column col: now,
// or once its page is in; with several tabs of it, once one is picked.
// Hidden by COLS, it stays where it is (§7.8).
func (a *App) gotoColumn(t db.Table, col string) {
	if a.palette != nil {
		a.palette.pickCol = col
		return
	}
	dt := dataOf(a.focused())
	if dt == nil || idOf(dt.table) != idOf(t) {
		return
	}
	dt.wantCol = col
	if dt.page.Cols != nil {
		dt.applyWantCol()
	}
}

// treeFilter puts the keys in the tree's filter row, showing and focusing
// the tree first.
func (a *App) treeFilter() {
	win := a.win()
	win.TreeOpen = true
	a.showPane(win.Tree.ID)
	if win.Focus == win.Tree.ID {
		win.tree.filtering = true
		win.tree.filter.Pos = len(win.tree.filter.Text)
	}
}

// filterKey edits the tree's filter, nvim-tree's live filter (§7.8): the
// cursor goes to the first match as it changes, and ↵ keeps it for the
// list; esc clears it, the cursor staying on its node.
func (a *App) filterKey(k keymap.Key) {
	t := &a.win().tree
	switch k {
	case "<CR>":
		t.filtering = false
		a.treeFirstMatch()
	case keymap.Esc:
		t.filter, t.filtering = ui.Input{}, false
		a.clampTree()
	default:
		if editInput(&t.filter, k) {
			a.treeFirstMatch()
		}
	}
}

// treeFirstMatch puts the cursor on the first table or view the filter
// kept, else the first node.
func (a *App) treeFirstMatch() {
	ns, _ := a.treeNodes()
	a.treeGo(max(slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeTable }), 0))
}

// editInput applies an input's own editing keys to in (they are no
// bindings) and reports whether its text changed.
func editInput(in *ui.Input, k keymap.Key) bool {
	switch k {
	case "<Left>":
		in.Left()
		return false
	case "<Right>":
		in.Right()
		return false
	case "<BS>":
		in.Backspace()
		return true
	}
	if keymap.Text(k) == "" {
		return false
	}
	in.Insert(keymap.Text(k))
	return true
}

// openTarget is the pane a table opens in (§5, §12): the focused one;
// with the tree or the result area focused, the one focused most recently
// whose current tab is no console, else the most recent of all (the
// first, if none ever was). Never the result area.
func (a *App) openTarget() *Pane {
	win := a.win()
	if p := a.focused(); p != win.Tree && p != win.Result {
		return p
	}
	var best, recent *Pane
	for _, p := range win.Root.Leaves() {
		if p == win.Result {
			continue
		}
		if recent == nil || a.recent(p.ID, recent.ID) {
			recent = p
		}
		if consoleOf(p) == nil && (best == nil || a.recent(p.ID, best.ID)) {
			best = p
		}
	}
	return cmp.Or(best, recent)
}
