package app

import (
	"cmp"
	"errors"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// The working set as a file (§14「布局文件」, F6.5): layoutOf takes it from
// the sessions, Restore builds sessions from it.

// layoutOf is every session as a Layout: the windows with their split
// trees and panes, a table's request, a query's file, connection and
// schema. The result area, a landing tab, results and what is not saved are
// left out, and so is a session of no connection (F7.4).
func (a *App) layoutOf() *config.Layout {
	l := &config.Layout{Version: config.LayoutVersion}
	for _, s := range a.sessions {
		if s.start == nil {
			continue
		}
		if s == a.sess {
			l.Active = len(l.Sessions)
		}
		ls := config.LayoutSession{Name: s.Name, Connection: s.start.Spec.Name, Active: s.Active}
		for _, w := range s.Windows {
			ls.Windows = append(ls.Windows, windowLayout(w))
		}
		l.Sessions = append(l.Sessions, ls)
	}
	return l
}

func windowLayout(w *Window) config.LayoutWindow {
	lw := config.LayoutWindow{Name: w.Name, Focus: w.Focus, Zoom: w.Zoom, SidebarOpen: w.TreeOpen, SidebarWidth: w.TreeW}
	root, ratio := w.Root, w.resultRatio
	if w.Result != nil && !w.resultHidden { // the result area is the root's lower half
		root, ratio = w.Root.A, w.Root.Ratio
	}
	if ratio != 0 { // from the area as it is, or as it was when it was closed
		lw.ResultHeight = 1 - ratio
	}
	leaves := root.Leaves()
	if w.Result != nil && w.Focus == w.Result.ID { // it is not saved: the pane focused before it is
		lw.Focus = slices.MaxFunc(leaves, func(a, b *Pane) int { return cmp.Compare(w.focusedAt[a.ID], w.focusedAt[b.ID]) }).ID
	}
	if w.Result != nil && w.Zoom == w.Result.ID {
		lw.Zoom = 0
	}
	lw.Layout = nodeLayout(root)
	for _, p := range leaves {
		lw.Panes = append(lw.Panes, paneLayout(p))
	}
	return lw
}

func nodeLayout(n *Node) *config.LayoutNode {
	switch n.Split {
	case Horiz:
		return &config.LayoutNode{Split: "h", Ratio: n.Ratio, A: nodeLayout(n.A), B: nodeLayout(n.B)}
	case Vert:
		return &config.LayoutNode{Split: "v", Ratio: n.Ratio, A: nodeLayout(n.A), B: nodeLayout(n.B)}
	case Leaf: // below
	}
	return &config.LayoutNode{Pane: n.Pane.ID}
}

func paneLayout(p *Pane) config.LayoutPane {
	lp := config.LayoutPane{ID: p.ID}
	for i, tb := range p.Tabs {
		var lt config.LayoutTab
		switch {
		case tb.Data != nil:
			d := tb.Data
			lt = config.LayoutTab{Kind: "table", Connection: d.conn.Spec.Name, DB: d.conn.Spec.Database, Schema: d.table.Schema, Table: d.table.Name,
				Where: d.applied, Order: config.LayoutOrder(d.order), Desc: d.desc && len(d.order) == 0, Limit: d.limit, Page: d.pageNo, Transpose: d.transpose}
			for name, hidden := range d.hidden {
				if hidden {
					lt.Cols = append(lt.Cols, name)
				}
			}
			slices.Sort(lt.Cols)
			lt.OrderCols = slices.Clone(d.colOrder)
			lt.ColWidths = maps.Clone(d.colWidth)
			for rec, w := range d.transposeWidths {
				if lt.TransposeWidths == nil {
					lt.TransposeWidths = map[string]int{}
				}
				lt.TransposeWidths[strconv.Itoa(rec+1)] = w
			}
			if d.table.Kind == "k" { // its pages go by cursors, which a new start has none of: the first
				lt.Kind, lt.Key, lt.Table, lt.Page = "key", d.table.Name, "", 0
			}
		case tb.Query != nil:
			t := tb.Query
			c, schema := t.conn, t.schema
			if t.want != nil { // not there yet: it is where it is going that is kept
				c, schema = t.want, t.wantSchema
			}
			lt = config.LayoutTab{Kind: "query", Connection: c.Spec.Name, DB: c.Spec.Database, Schema: schema, Name: t.name}
		default: // a landing tab
			continue
		}
		if i == p.Cur {
			lp.Current = len(lp.Tabs)
		}
		lp.Tabs = append(lp.Tabs, lt)
	}
	return lp
}

// Restored is the sessions a layout made, and what was said making them.
type Restored struct {
	Sessions []*Session
	Active   int
	Warnings []string
}

// Restore builds the sessions of l over the connections of specs (§14),
// none of them connected: a table tab waits for its connection's catalog.
// What cannot be built is left out, said in Warnings: a connection that is
// not there, a query file that does not read.
func Restore(specs []config.Connection, l *config.Layout) *Restored {
	r := &Restored{Active: min(max(l.Active, 0), len(l.Sessions)-1)}
	used := map[string]bool{} // the queries taken, by lower-cased name: files of names differing in case are one on some file systems
	for _, ls := range l.Sessions {
		conns := make([]*Conn, len(specs))
		for j, spec := range specs {
			conns[j] = newConn(spec)
		}
		start := slices.IndexFunc(conns, func(c *Conn) bool { return c.Spec.Name == ls.Connection })
		if start < 0 {
			r.Warnings = append(r.Warnings, i18n.T("没有名为 %s 的连接，session %s 用 %s 开始", ls.Connection, ls.Name, conns[0].Spec.Name))
			start = 0
		}
		first := conns[start]
		groupOrder(conns)
		s := bareSession(ls.Name, conns, first)
		for _, lw := range ls.Windows {
			s.winID++
			w, warns := restoreWindow(s, lw, used)
			w.ID = s.winID
			s.Windows = append(s.Windows, w)
			r.Warnings = append(r.Warnings, warns...)
		}
		if len(s.Windows) == 0 { // a session with no window is one with the default
			name := freeQueryName(func(name string) bool { return used[name] })
			w, warning := s.newWindow(first, name)
			if warning != "" {
				r.Warnings = append(r.Warnings, warning)
			} else {
				used[name] = true
			}
			s.Windows = append(s.Windows, w)
		}
		s.Active = min(max(ls.Active, 0), len(s.Windows)-1)
		r.Sessions = append(r.Sessions, s)
	}
	return r
}

// restoreWindow is lw as a window of s: the split tree with the panes it
// names, their tabs, the focus and the zoom. A pane the tree has that no
// entry gives is an empty one; one the tree does not have gets no tab; a
// window with no tree is one pane, the first there is.
func restoreWindow(s *Session, lw config.LayoutWindow, used map[string]bool) (*Window, []string) {
	var warns []string
	panes := map[int]*Pane{} // the tree's
	w := &Window{Name: lw.Name, TreeOpen: lw.SidebarOpen, TreeW: lw.SidebarWidth, Tree: &Pane{ID: 0}}
	var build func(n *config.LayoutNode) *Node
	build = func(n *config.LayoutNode) *Node {
		if n == nil {
			return nil
		}
		if n.A != nil || n.B != nil { // two halves make a split; a leaf that has a split written too is still a leaf (§14)
			dir := Horiz // none written: h
			switch n.Split {
			case "", "h":
			case "v":
				dir = Vert
			default: // said once, and read as h: the panes and their tabs are not dropped for a slip
				if warn := i18n.T("不认识的 split %q，按 h", n.Split); !slices.Contains(warns, warn) {
					warns = append(warns, warn)
				}
			}
			a, b := build(n.A), build(n.B)
			switch {
			case a == nil:
				return b
			case b == nil:
				return a
			}
			ratio := n.Ratio
			if ratio <= 0 || ratio >= 1 {
				ratio = 0.5
			}
			return &Node{Split: dir, Ratio: ratio, A: a, B: b}
		}
		if n.Pane <= 0 || panes[n.Pane] != nil {
			return nil
		}
		panes[n.Pane] = &Pane{ID: n.Pane, Prev: -1}
		return leaf(panes[n.Pane])
	}
	if w.Root = build(lw.Layout); w.Root == nil {
		id := 1
		if i := slices.IndexFunc(lw.Panes, func(lp config.LayoutPane) bool { return lp.ID > 0 }); i >= 0 {
			id = lw.Panes[i].ID
		}
		panes[id] = &Pane{ID: id, Prev: -1}
		w.Root = leaf(panes[id])
	}
	filled := map[int]bool{}
	for _, lp := range lw.Panes {
		p := panes[lp.ID]
		if p == nil || filled[lp.ID] { // not in the tree, or a second entry: not read
			continue
		}
		filled[lp.ID] = true
		for i, lt := range lp.Tabs {
			tb, warn := s.restoreTab(lt, used)
			if warn != "" {
				warns = append(warns, warn)
				continue
			}
			if d := tb.Data; d != nil && p.tableTab(d.conn, d.table) >= 0 { // a table is one tab of a pane (F3.37): the first stays
				continue
			}
			if i == lp.Current {
				p.Cur = len(p.Tabs)
			}
			p.Tabs = append(p.Tabs, tb)
		}
	}
	for _, p := range w.Root.Leaves() {
		w.lastID = max(w.lastID, p.ID)
	}
	if lw.ResultHeight > 0 && lw.ResultHeight < 1 {
		w.resultRatio = 1 - lw.ResultHeight
	}
	w.focus(w.Root.Leaves()[0].ID)
	if (lw.Focus != 0 || lw.SidebarOpen) && w.pane(lw.Focus) != nil { // 0 is the sidebar, when there is one
		w.focus(lw.Focus)
	}
	if lw.Zoom != 0 && lw.Zoom == w.Focus { // a zoom of another pane than the focused one hides it
		w.Zoom = lw.Zoom
	}
	return w, warns
}

// restoreTab is a layout's tab: a table, pending its connection's catalog,
// or a query on its file; warn says why there is none. A tab with no
// connection is on the session's start (§14).
func (s *Session) restoreTab(lt config.LayoutTab, used map[string]bool) (tb Tab, warn string) {
	if lt.Kind != "table" && lt.Kind != "key" && lt.Kind != "query" {
		return tb, i18n.T("不认识的 tab")
	}
	name := cmp.Or(lt.Connection, s.start.Spec.Name)
	i := slices.IndexFunc(s.Conns, func(c *Conn) bool { return c.Spec.Name == name })
	if i < 0 {
		return tb, i18n.T("没有名为 %s 的连接，跳过 %s", name, cmp.Or(lt.Table, lt.Key, lt.QueryName()))
	}
	c := s.Conns[i].inDB(lt.DB)
	if lt.Kind != "query" { // a schema it does not name is the connection's home, known with the catalog
		d := newDataTab(c, db.Table{Schema: lt.Schema, Name: lt.Table})
		if lt.Kind == "key" {
			d.table = db.Table{Name: lt.Key, Kind: "k"}
		}
		r := d.request
		r.applied, r.order, r.desc, r.pageNo = lt.Where, lt.Order, lt.Desc, max(lt.Page, 0)
		if lt.Limit > 0 { // typed by hand it may be anything
			r.limit = min(lt.Limit, maxLimit)
		}
		d.setRequest(r)
		d.pending = true
		for _, name := range lt.Cols {
			d.hidden[name] = true
		}
		d.transpose = lt.Transpose
		d.colOrder = slices.Clone(lt.OrderCols)
		for no, w := range lt.TransposeWidths {
			if rec, err := strconv.Atoi(no); err == nil && rec > 0 && w > 0 { // typed by hand it may be anything
				if d.transposeWidths == nil {
					d.transposeWidths = map[int]int{}
				}
				d.transposeWidths[rec-1] = w
			}
		}
		for name, w := range lt.ColWidths {
			if w > 0 { // typed by hand it may be anything
				if d.colWidth == nil {
					d.colWidth = map[string]int{}
				}
				d.colWidth[name] = w
			}
		}
		return Tab{Name: c.tabName(d.table), Data: d}, ""
	}
	cname := lt.QueryName()
	if cname == "" || used[strings.ToLower(cname)] || config.CheckQueryName(cname) != nil { // none, taken or not a file's: the next free one, with its own file
		cname = freeQueryName(func(name string) bool { return used[name] })
	}
	t, err := openQuery(s.start, cname)
	if err != nil {
		return tb, err.Error()
	}
	used[strings.ToLower(cname)] = true
	if c == s.start {
		t.schema = lt.Schema
	} else { // the connection it was on goes over once it is there
		t.want, t.wantSchema = c, lt.Schema
	}
	return Tab{Name: cname, Query: t}, ""
}

// Adopt makes r the program's sessions, the one it names attached; the
// warnings are said once it runs. Before the program runs only.
func (a *App) Adopt(r *Restored) {
	a.sessions, a.sess = r.Sessions, r.Sessions[r.Active]
	a.warning = JoinNotes(append([]string{a.warning}, r.Warnings...)...)
	a.plainQueries()
	a.restored = true
}

// connectsNow are the connections the running program connects to at once:
// each session's start, a pending table tab's, the one a query waits for.
func (a *App) connectsNow() []tea.Cmd {
	var cmds []tea.Cmd
	catalog := map[*Conn]bool{}
	want := func(c *Conn) {
		if c.State == connReady && !catalog[c] {
			catalog[c] = true
			cmds = append(cmds, a.loadCatalog(c))
		} else if c.State != connReady {
			cmds = append(cmds, a.connect(c))
		}
	}
	for _, s := range a.sessions {
		if s.last != nil { // none at a start with no connection (F7.4)
			want(s.last)
		}
		for _, p := range s.panes() {
			for _, tb := range p.Tabs {
				switch {
				case tb.Data != nil && tb.Data.pending:
					want(tb.Data.conn)
				case tb.Query != nil && tb.Query.want != nil:
					want(tb.Query.want)
				}
			}
		}
	}
	return cmds
}

// startTable is the table (or the key) the command line asks to open, once
// its connection's catalog is in (F19.1).
type startTable struct {
	conn        *Conn
	name, where string
}

// OpenAtStart has the program open table name of the start's connection
// as it begins, with the condition where on it: a Redis connection's is a key's
// name, with no condition. Before the program runs only; the error is what is
// wrong with the asking.
func (a *App) OpenAtStart(name, where string) error {
	c := a.sess.start
	switch {
	case c == nil:
		return errors.New("--table needs a connection, and there is none in connections.toml")
	case !c.sql() && where != "":
		return errors.New("--where is for the tables of SQL connections, not for the keys of " + c.Spec.Name)
	}
	a.want = &startTable{c, name, where}
	return nil
}

// openWanted opens what the command line asked for in the first pane, the
// data pane of the default layout, as the tree's ↵ does: the table of the
// name in the connection's schema, or of schema.name; the key of a Redis
// connection is not looked up. No such table is said, the layout stays.
func (a *App) openWanted(w *startTable) tea.Cmd {
	c, t := w.conn, db.Table{Name: w.name, Kind: "k"}
	if c.sql() {
		i := slices.IndexFunc(c.Tables, func(t db.Table) bool {
			return t.Schema == c.Schema && t.Name == w.name || t.Schema+"."+t.Name == w.name
		})
		if i < 0 {
			return a.showToast(i18n.T("%s：没有表 %s", c.label(), w.name))
		}
		t = c.Tables[i]
	}
	r := newDataTab(c, t).request
	r.applied, r.desc = w.where, a.newDesc
	p := a.win().Root.Leaves()[0]
	open := a.openTableAs(p, c, t, &r, a.newTurned)
	if d := dataOf(p); w.where != "" && d != nil { // the pane is empty at a start: the table is a new tab
		return tea.Batch(open, a.rememberWhere(d)) // as if it was typed (§9.7)
	}
	return open
}

// resolvePending runs once c's catalog is in: a table tab waiting for it
// gets its table and fetches; one whose table is not in it is closed, said
// (§14「恢复」); so is the table of the command line opened.
func (a *App) resolvePending(c *Conn) []tea.Cmd {
	var cmds []tea.Cmd
	if w := a.want; w != nil && w.conn == c {
		a.want = nil
		cmds = append(cmds, a.openWanted(w))
	}
	for _, s := range a.sessions {
		for _, p := range s.panes() {
			for i := len(p.Tabs) - 1; i >= 0; i-- {
				d := p.Tabs[i].Data
				if d == nil || !d.pending || d.conn != c {
					continue
				}
				d.pending = false
				home := func(t db.Table) string { return cmp.Or(t.Schema, c.home) } // a hand's layout may leave it out
				if slices.ContainsFunc(p.Tabs[:i], func(o Tab) bool {
					return o.Data != nil && o.Data.conn == c && o.Data.table.Name == d.table.Name && home(o.Data.table) == home(d.table)
				}) { // the same table as one before it: a pane has it once (F3.37)
					dropTab(p, i)
					continue
				}
				if d.table.Kind == "k" { // not in a catalog: it reads once its connection is up
					cmds = append(cmds, a.fetch(d, true))
					continue
				}
				d.table.Schema = home(d.table)
				if j := slices.IndexFunc(c.Tables, func(t db.Table) bool { return idOf(t) == idOf(d.table) }); j >= 0 {
					d.table = c.Tables[j]
					p.Tabs[i].Name = c.tabName(d.table) // restored with no schema, it had none in its name
					cmds = append(cmds, a.fetch(d, true))
					continue
				}
				cmds = append(cmds, a.showToast(i18n.T("%s 里没有表 %s，跳过", c.label(), d.table.Schema+"."+d.table.Name)))
				dropTab(p, i)
			}
		}
	}
	return cmds
}

// dropPending closes the table tabs waiting on connection c, whose catalog
// is not to be had, without a word: the catalog's own toast says it (§14).
func (a *App) dropPending(c *Conn) {
	if a.want != nil && a.want.conn == c {
		a.want = nil
	}
	for _, s := range a.sessions {
		for _, p := range s.panes() {
			for i := len(p.Tabs) - 1; i >= 0; i-- {
				if d := p.Tabs[i].Data; d != nil && d.pending && d.conn == c {
					dropTab(p, i)
				}
			}
		}
	}
}

// verbOpen is what opening a layout asks about: it replaces every session
// (§14).
var verbOpen = lostVerb{i18n.N("打开"), i18n.N("打开布局会丢弃。"), i18n.N("打开布局会回滚。")}

// SaveLayoutOnExit has the program write its working set to path as it ends
// by its own quit paths (§14「保存」).
func (a *App) SaveLayoutOnExit(path string) { a.lastPath = path }

// saveLast writes the working set where SaveLayoutOnExit said, as the
// program quits. An error has no one to tell then.
// ponytail: a program killed does not write; the layout is the one of the
// last quit.
func (a *App) saveLast() {
	if l := a.layoutOf(); a.lastPath != "" && len(l.Sessions) > 0 {
		_ = config.SaveLayout(a.lastPath, l)
	}
}

// askLayoutName is layout.save: the name typed in the status bar, ↵ writes
// layouts/<name>.toml, esc drops it (§14「命令」).
func (a *App) askLayoutName() {
	a.askName = &nameAsk{label: i18n.T("布局名："), done: func(a *App, name string) (tea.Cmd, bool) {
		if err := config.CheckLayoutName(name); err != nil {
			return a.showToast(err.Error()), false
		}
		l := a.layoutOf()
		if len(l.Sessions) == 0 { // a start with no connection has nothing to save (F7.4)
			return a.noConnection(), false
		}
		if err := config.SaveLayout(config.LayoutPath(name), l); err != nil {
			return a.showToast(err.Error()), false
		}
		return a.showToast(i18n.T("已保存布局 %s", name)), false
	}}
}

// openLayoutDrop is layout.open: the layouts listed, last among them when
// there is one.
func (a *App) openLayoutDrop() {
	a.drop = &dropdown{kind: dropLayout, pane: a.focused(), sel: 0}
	a.dropMove(0)
}

// layoutNames are what layout.open offers: the layouts of layouts/, and
// last, where the program left off, when it is kept.
func layoutNames() []string {
	names := config.LayoutNames()
	if _, err := os.Stat(config.LastPath()); err == nil {
		names = append([]string{"last"}, slices.DeleteFunc(names, func(n string) bool { return n == "last" })...)
	}
	return names
}

// pickLayout is a layout.open pick: asked first when every session's
// unsaved changes and transactions would go (§10.5); then the queries are
// written, the layout read and every session replaced, all in this one
// Update (§14「命令」): between, what was typed would be lost or written
// over the new queries' files. The connections are dialed in the background,
// the first session's start among them.
func (a *App) pickLayout(name string) tea.Cmd {
	path := config.LayoutPath(name)
	if name == "last" {
		path = config.LastPath()
	}
	var ps []*Pane
	for _, s := range a.sessions {
		ps = append(ps, s.panes()...)
	}
	return a.unlessLost(unsaved(ps...), "", txConns(a.sessions...), verbOpen, func() tea.Cmd {
		if cmd, ok := a.flushAll(ps...); !ok { // the queries' text is not lost (§11)
			return cmd
		}
		l, err := config.LoadLayout(path)
		if err != nil {
			return a.showToast(err.Error())
		}
		specs := make([]config.Connection, len(a.sess.Conns))
		for i, c := range a.sess.Conns {
			specs[i] = c.Spec
		}
		r := Restore(specs, l)
		var ws []*db.Worker
		for _, s := range a.sessions {
			ws = append(ws, s.workers()...)
		}
		a.sessions = r.Sessions
		a.attach(r.Sessions[r.Active])
		cmds := append(a.connectsNow(), closeWorkers(ws))
		if len(r.Warnings) > 0 {
			cmds = append(cmds, a.showToast(JoinNotes(r.Warnings...)))
		}
		return tea.Batch(cmds...)
	})
}
