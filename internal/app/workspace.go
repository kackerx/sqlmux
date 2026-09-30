package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/config"
	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// Pane is the sidebar or one of the split tree's: tables and consoles,
// side by side in its tabs (§5).
type Pane struct {
	ID        int // stable; the sidebar is 0
	Tabs      []Tab
	Cur, Prev int // tab bar * and - (T-01)
}

// Object is the title's "· name" part: the current tab.
func (p *Pane) Object() string {
	if t := p.tab(); t != nil {
		return t.Name
	}
	return ""
}

// tab is the current tab; nil when the pane has none.
func (p *Pane) tab() *Tab {
	if p.Cur < len(p.Tabs) {
		return &p.Tabs[p.Cur]
	}
	return nil
}

// landing is what a new tab is until a table or a console takes its place
// (§5「引导页」).
func landing() Tab { return Tab{Name: "新 tab"} }

// landing reports whether t shows the landing page: a new tab, or no tab.
func (t *Tab) landing() bool { return t == nil || t.Data == nil && t.Console == nil && t.Result == nil }

// putTab puts tab in pane p: over the current tab, or after the last one,
// which it becomes, the current one the previous.
func putTab(p *Pane, tab Tab, over bool) {
	if over && len(p.Tabs) > 0 {
		p.Tabs[p.Cur] = tab
		return
	}
	p.Prev = p.Cur
	if len(p.Tabs) == 0 { // an empty pane: there is no tab to go back to
		p.Prev = -1
	}
	p.Tabs = append(p.Tabs, tab)
	p.Cur = len(p.Tabs) - 1
}

type Window struct {
	Name     string
	TreeOpen bool  // the ⟨0⟩ sidebar is open, not folded to its thin bar
	TreeW    int   // the sidebar's width once dragged (§7.8); 0 is the default
	Tree     *Pane // ⟨0⟩ sidebar, not part of the split tree (D-04)
	// Result is the result area, at the bottom from the first run until it
	// is closed; resultRatio is the share of the rest above it, kept then
	// (0: 1 − result_height), and log the runs', kept too (§11).
	Result      *Pane
	resultRatio float64
	log         []ui.LogLine
	tree        treeState
	Root        *Node
	Focus       int // pane ID
	Zoom        int // zoomed pane ID; 0 = none (P-03)
	lastID      int // highest pane ID handed out

	focusTick int
	focusedAt map[int]int // pane ID → focusTick when it last got focus

	resultHidden bool // SPC r took the result area out of the layout, its tabs kept (§11)
}

// focus moves focus to pane id, remembering when: moving by direction
// prefers the neighbour focused most recently (§5). Leaving a pane leaves
// its input too: the tree's filter row, a table's query bar.
func (w *Window) focus(id int) {
	if w.focusedAt == nil {
		w.focusedAt = map[int]int{}
	}
	w.focusTick++
	w.Focus, w.focusedAt[id] = id, w.focusTick
	if id != w.Tree.ID {
		w.tree.filtering = false
	}
	for _, p := range w.Root.Leaves() {
		if t := dataOf(p); t != nil && p.ID != id {
			t.stopTyping()
		}
	}
}

// pane is the window's pane id, the sidebar too; nil when there is none.
func (w *Window) pane(id int) *Pane {
	if id == w.Tree.ID {
		return w.Tree
	}
	for _, p := range w.Root.Leaves() {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Session is one connection (tech-design §5).
type Session struct {
	Name       string
	Addr       string          // shown in the status bar, e.g. ctw@localhost:5432
	Main, Meta *db.Worker      // §8.2
	Schema     string          // the tree's schema: the cursor's, or the last it was under (§7.8)
	Schemas    []string        // the catalog's (§8.4)
	home       string          // current_schema(), whose Tables the tree opens with
	open       map[string]bool // tree nodes opened or closed by hand, by node ID; the rest as §7.8 says
	Tables     []db.Table      // every schema's, by schema and name
	cols       map[tableID]db.Columns
	colsAsked  map[tableID]bool    // for names' colors (§7.3): asked once, till cols is dropped
	ddl        map[tableID]ddlText // the palette's previews (F4.2), dropped with cols
	Windows    []*Window
	Active     int
	RunSeq     int    // the last run's number, #42 on its result tabs (§11)
	warning    string // console_1's file could not be read: a toast on start (§11)
	// DB is the database, on console titles; startedPath Main's search_path
	// as it connected; mainPath the schema a console's run last put first
	// on it, "" when not known, touched in Main's requests only (§8.6).
	DB                    string
	startedPath, mainPath string
}

func (a *App) win() *Window { return a.sess.Windows[a.sess.Active] }

// dropCols forgets the columns fetched: the catalog loads anew (§7.8).
func (s *Session) dropCols() {
	clear(s.cols)
	clear(s.colsAsked)
	clear(s.ddl)
}

// newSession is a session's default workspace (§5): one window, data, with
// the sidebar, a pane with no tab and console_1 beside it at 5 : 4 (§7.8),
// which has what its file kept (§11). A file that cannot be read leaves
// that pane with no tab too: opened empty, the autosave would write over it.
func newSession(name, addr string, main, meta *db.Worker) *Session {
	s := &Session{Name: name, Addr: addr, Main: main, Meta: meta, open: map[string]bool{}, cols: map[tableID]db.Columns{}, colsAsked: map[tableID]bool{}, ddl: map[tableID]ddlText{}}
	data := &Pane{ID: 1, Prev: -1}
	console := &Pane{ID: 2, Prev: -1}
	if cons, err := openConsole(name, 1); err != nil {
		s.warning = err.Error()
	} else {
		console.Tabs = []Tab{{Name: "console_1", Console: cons}}
	}
	w := &Window{
		Name:     "data",
		TreeOpen: true,
		Tree:     &Pane{ID: 0},
		Root:     &Node{Split: Horiz, Ratio: 5.0 / 9, A: leaf(data), B: leaf(console)},
		lastID:   2,
	}
	w.focus(1)
	s.Windows = []*Window{w}
	return s
}

// Open connects a session's Main and then its Meta (§8.2).
func Open(ctx context.Context, c config.Connection) (*Session, error) {
	pw, err := c.Secret()
	if err != nil {
		return nil, err
	}
	main, err := postgres.Connect(ctx, c.DSN, pw, false)
	if err != nil {
		return nil, err
	}
	meta, err := postgres.Connect(ctx, c.DSN, pw, true)
	if err != nil {
		main.Close()
		return nil, err
	}
	s := newSession(c.Name, main.Addr, db.NewWorker(main), db.NewWorker(meta))
	s.DB, s.startedPath = main.Database, main.SearchPath
	return s, nil
}

func (s *Session) Close() {
	s.Main.Close()
	s.Meta.Close()
}

// openTable shows table t in openTarget's pane and focuses it (§7.8
// 「打开已有的表」, §12).
func (a *App) openTable(t db.Table) tea.Cmd { return a.openTableIn(a.openTarget(), t) }

// openTableIn shows table t in pane p and focuses it: p's tab of t, a pane
// having one at most (F3.37), else a new one. A landing tab current gives
// way to it, replaced, or closed for the tab there (§5「引导页」).
func (a *App) openTableIn(p *Pane, t db.Table) tea.Cmd {
	a.showPane(p.ID)
	landing := p.tab().landing()
	if i := slices.IndexFunc(p.Tabs, func(tb Tab) bool { return tb.Data != nil && idOf(tb.Data.table) == idOf(t) }); i >= 0 {
		if closed := p.Cur; landing {
			if a.closeTab(p, closed); closed < i {
				i--
			}
		}
		selectTab(p, i)
		return nil
	}
	tab := Tab{Name: t.Name, Data: newDataTab(t)}
	putTab(p, tab, landing)
	return a.fetch(tab.Data, true)
}

// showTab focuses pane p and switches to its tab i.
func (a *App) showTab(p *Pane, i int) {
	a.showPane(p.ID)
	selectTab(p, i)
}

// selectTab makes tab i pane p's current one; the one it was becomes the
// previous, the - (T-01). What was being typed in it goes, as when its pane
// loses focus.
func selectTab(p *Pane, i int) {
	if i == p.Cur || i < 0 || i >= len(p.Tabs) {
		return
	}
	if t := dataOf(p); t != nil {
		t.stopTyping()
	}
	p.Prev, p.Cur = p.Cur, i
}

// cycleTab is gt (d 1) and gT (d -1) on the focused pane, as in vim: the
// next or previous tab, around the ends; with a count gt goes to tab count
// and gT back count tabs.
func (a *App) cycleTab(d, count int) {
	p := a.focused()
	if n := len(p.Tabs); d > 0 && count > 0 {
		selectTab(p, count-1)
	} else if n > 0 {
		selectTab(p, ((p.Cur+d*max(count, 1))%n+n)%n)
	}
}

// newTab is tab.new, a tab bar's +: a landing tab after the last one,
// which it becomes, in the focused pane (§5「引导页」).
func (a *App) newTab() {
	p := a.openTarget()
	a.showPane(p.ID)
	putTab(p, landing(), false)
}

// newConsole is console.new (§5, §11): console_n, n the least not open in
// the session, with what its file keeps, in place of a landing tab or in a
// new tab of openTarget's pane.
func (a *App) newConsole() tea.Cmd {
	p := a.openTarget()
	n := 1
	for a.consoleOpen(config.ConsolePath(a.sess.Name, n)) {
		n++
	}
	c, err := openConsole(a.sess.Name, n)
	if err != nil {
		return a.showToast(err.Error(), toastTTL)
	}
	c.schema = a.sess.Schema // then its own (§8.6)
	a.showPane(p.ID)
	putTab(p, Tab{Name: fmt.Sprintf("console_%d", n), Console: c}, p.tab().landing())
	return nil
}

// consoleOpen is whether a console of the session is open on path.
func (a *App) consoleOpen(path string) bool {
	return slices.ContainsFunc(a.sess.consoles(), func(c *consoleTab) bool { return c.path == path })
}

// consoles is the session's consoles, in every window.
func (s *Session) consoles() []*consoleTab {
	var out []*consoleTab
	for _, w := range s.Windows {
		for _, p := range w.Root.Leaves() {
			for _, t := range p.Tabs {
				if t.Console != nil {
					out = append(out, t.Console)
				}
			}
		}
	}
	return out
}

// paneShowing is the pane of the window whose current tab is t; nil when
// t is not on screen.
func (a *App) paneShowing(t *dataTab) *Pane {
	for _, p := range a.win().Root.Leaves() {
		if dataOf(p) == t {
			return p
		}
	}
	return nil
}

// closeTab closes pane p's tab i: the current one (:q) gives way to the
// previous, as vim's alternate; another leaves the current and previous
// tabs as they were. Closing the last tab closes the pane too, except the
// sidebar and the window's only pane; the result area's log stays (§11).
func (a *App) closeTab(p *Pane, i int) {
	if p == a.win().Tree || i < 0 || i >= len(p.Tabs) || p.Tabs[i].Result != nil && p.Tabs[i].Result.run == nil {
		return
	}
	p.Tabs = slices.Delete(p.Tabs, i, i+1)
	switch {
	case i != p.Cur:
		if p.Cur > i {
			p.Cur--
		}
		if p.Prev == i {
			p.Prev = -1
		} else if p.Prev > i {
			p.Prev--
		}
	case p.Prev > i:
		p.Cur, p.Prev = p.Prev-1, -1
	case p.Prev >= 0 && p.Prev != i:
		p.Cur, p.Prev = p.Prev, -1
	default:
		p.Cur, p.Prev = max(min(i, len(p.Tabs)-1), 0), -1
	}
	if len(p.Tabs) == 0 {
		a.removePane(p.ID)
	}
}

// closeTabAsking is x on pane p's tab i (F3.35): a console's is written
// first, not asked about (§11); a table's with changes asks first.
func (a *App) closeTabAsking(p *Pane, i int) tea.Cmd {
	if i < 0 || i >= len(p.Tabs) { // tab.close.at's i may come from a user's map
		return nil
	}
	if t := p.Tabs[i].Console; t != nil {
		if err := t.flush(); err != nil {
			return a.saveFailed(err)
		}
		a.closeTab(p, i)
		return nil
	}
	n, name := 0, ""
	if t := p.Tabs[i].Data; t != nil {
		n, name = t.changes(), t.table.Name+" "
	}
	return a.unlessUnsaved(n, name, "关闭", func() tea.Cmd { a.closeTab(p, i); return nil })
}

// removePane takes pane id out of the tree: its sibling gets the space and
// the focus, and any zoom ends. The window's last pane besides the result
// area stays. The result area goes with its tabs, pinned or not, its
// height kept for the next time (§11).
func (a *App) removePane(id int) {
	win := a.win()
	if res := win.Result; res != nil && id != res.ID && len(win.Root.Leaves()) == 2 && !win.resultHidden {
		return
	}
	if res := win.Result; res != nil && id == res.ID {
		for _, tb := range res.Tabs { // what a run going on took the place of goes too
			if r := tb.Result.run; r != nil {
				r.prev = nil
			}
		}
		win.resultRatio, win.Result = win.Root.Ratio, nil // it is the root's lower half
	}
	if root, heir := win.Root.remove(id); root != nil {
		win.Root, win.Zoom = root, 0
		win.focus(heir.ID)
	}
}

// panesByNumber is ⟨n⟩ order: the sidebar is 0, then the tree's leaves.
func (a *App) panesByNumber() []*Pane {
	return append([]*Pane{a.win().Tree}, a.win().Root.Leaves()...)
}

// focusSide moves focus to the neighbouring pane on side, from the rects the
// last frame was drawn with (tech-design §5). A folded sidebar takes no focus.
func (a *App) focusSide(side string) {
	win := a.win()
	rects := a.layout()
	if !win.TreeOpen {
		delete(rects, win.Tree.ID)
	}
	if id, ok := neighbor(rects, win.Focus, side, a.recent); ok {
		win.focus(id)
	}
}

// recent reports whether pane x beats y as the one focused most recently:
// moving by direction (§5) and opening a table from the tree (§12) pick by
// it. Of two never focused, the one first in ⟨n⟩ order (up / left) wins.
func (a *App) recent(x, y int) bool {
	win := a.win()
	if win.focusedAt[x] != win.focusedAt[y] {
		return win.focusedAt[x] > win.focusedAt[y]
	}
	ps := a.panesByNumber()
	at := func(id int) int { return slices.IndexFunc(ps, func(p *Pane) bool { return p.ID == id }) }
	return at(x) < at(y)
}

// splitPane divides the focused pane along d; the new, empty pane gets focus.
func (a *App) splitPane(d Dir) {
	win := a.win()
	p := a.focused()
	if p == win.Tree || p == win.Result { // one result area a window, at the bottom (§11)
		return
	}
	win.lastID++ // never reused, so pane IDs stay stable (§5)
	np := &Pane{ID: win.lastID, Prev: -1}
	win.Root, win.Zoom = win.Root.split(p.ID, d, np), 0
	win.focus(np.ID)
}

// closePane closes the focused pane; its sibling takes the space. The
// sidebar and the window's only pane stay.
func (a *App) closePane() {
	win := a.win()
	if win.Focus != win.Tree.ID {
		a.removePane(win.Focus)
	}
}

// resizeStep is how far one SPC H/J/K/L moves a border.
const resizeStep = 0.05

func (a *App) resizePane(d Dir, sign float64, count int) {
	win := a.win()
	if win.Focus == win.Tree.ID {
		return
	}
	win.Root = win.Root.resize(win.Focus, d, sign*resizeStep*float64(max(count, 1)))
}

func (a *App) toggleZoom() {
	win := a.win()
	switch {
	case win.Zoom != 0:
		win.Zoom = 0
	case win.Focus != win.Tree.ID:
		win.Zoom = win.Focus
	}
}

// toggleTree is SPC b: the sidebar folded, or open with the focus on it,
// its cursor on the node of the tab focused before (§7.8, F3.29).
func (a *App) toggleTree() {
	win := a.win()
	switch win.TreeOpen = !win.TreeOpen; {
	case win.TreeOpen:
		a.revealTab(a.focused())
		win.focus(win.Tree.ID)
	case win.Focus == win.Tree.ID:
		win.focus(win.Root.Leaves()[0].ID)
	}
}

// jumpToPane is SPC q's second key: a digit picks ⟨n⟩, anything else just
// closes the numbers.
func (a *App) jumpToPane(k keymap.Key) {
	a.paneNumbers = false
	n, err := strconv.Atoi(string(k))
	ps := a.panesByNumber()
	if err != nil || n < 0 || n >= len(ps) || n == 0 && !a.win().TreeOpen {
		return
	}
	a.win().Zoom = 0
	a.win().focus(ps[n].ID)
}

// showPane focuses pane id where the palette sends the user (§12). A zoom on
// another pane ends first, as a jump by number ends it (§5): the pane is on
// screen once it has focus.
func (a *App) showPane(id int) {
	if a.win().Zoom != id {
		a.win().Zoom = 0
	}
	a.focusPane(id)
}

// focusPane gives focus to pane id if it is on screen (a click).
func (a *App) focusPane(id int) {
	if _, ok := a.layout()[id]; ok && (id != a.win().Tree.ID || a.win().TreeOpen) {
		a.win().focus(id)
	}
}

// wheelStep is how many rows one wheel notch scrolls (§7.4).
const wheelStep = 3

// scrollPane scrolls pane id by down notches and right notches (negative:
// up, left), within its content.
func (a *App) scrollPane(id, down, right int) {
	switch p := a.win().pane(id); {
	case p == a.win().Tree:
		a.scrollTree(down)
	case p != nil && resultOf(p) != nil && resultOf(p).run == nil: // the log
		a.logMove(p, func(r, c, _, _ int) (int, int) { return r + down*wheelStep, c })
	case p != nil && consoleOf(p) != nil: // not sideways: nowrap scrolls with the cursor (§11)
		c := consoleOf(p)
		a.consoleView(p, c)
		c.ed.Scroll(down * wheelStep)
		c.comp = nil
	case p != nil:
		a.scrollGrid(p, down*wheelStep, right)
	}
}
