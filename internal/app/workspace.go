package app

import (
	"context"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/config"
	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// PaneKind is what a pane shows.
type PaneKind uint8

const (
	KindSchema PaneKind = iota // the ⟨0⟩ sidebar
	KindData
	KindConsole
)

func (k PaneKind) String() string {
	return [...]string{"schema", "data", "console"}[k]
}

type Pane struct {
	ID        int // stable; the sidebar is 0
	Kind      PaneKind
	Tabs      []Tab
	Cur, Prev int // tab bar * and - (T-01)
}

// Object is the title's "· name" part: the current tab.
func (p *Pane) Object() string {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur].Name
	}
	return ""
}

type Window struct {
	Name     string
	TreeOpen bool  // the ⟨0⟩ sidebar is open, not folded to its thin bar
	TreeW    int   // the sidebar's width once dragged (§7.8); 0 is the default
	Tree     *Pane // ⟨0⟩ sidebar, not part of the split tree (D-04)
	tree     treeState
	Root     *Node
	Focus    int // pane ID
	Zoom     int // zoomed pane ID; 0 = none (P-03)
	lastID   int // highest pane ID handed out
	newTabIn int // the pane whose + was clicked: the table picked next opens in a new tab there; 0 for none

	focusTick int
	focusedAt map[int]int // pane ID → focusTick when it last got focus
}

// focus moves focus to pane id, remembering when: moving by direction
// prefers the neighbour focused most recently (§5). Leaving a pane leaves
// its input too: the tree's filter row, a table's query bar. Leaving the
// tree drops what a + asked of it.
func (w *Window) focus(id int) {
	if w.focusedAt == nil {
		w.focusedAt = map[int]int{}
	}
	w.focusTick++
	w.Focus, w.focusedAt[id] = id, w.focusTick
	if id != w.Tree.ID {
		w.tree.filtering, w.newTabIn = false, 0
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
	Windows    []*Window
	Active     int
}

func (a *App) win() *Window { return a.sess.Windows[a.sess.Active] }

// newSession is a session's default workspace (§5): one window, data, with
// the sidebar and an empty data pane.
func newSession(name, addr string, main, meta *db.Worker) *Session {
	w := &Window{
		Name:     "data",
		TreeOpen: true,
		Tree:     &Pane{ID: 0, Kind: KindSchema},
		Root:     leaf(&Pane{ID: 1, Kind: KindData, Prev: -1}),
		lastID:   1,
	}
	w.focus(1)
	return &Session{Name: name, Addr: addr, Main: main, Meta: meta, cols: map[tableID]db.Columns{}, Windows: []*Window{w}}
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
	return newSession(c.Name, main.Addr, db.NewWorker(main), db.NewWorker(meta)), nil
}

func (s *Session) Close() {
	s.Main.Close()
	s.Meta.Close()
}

// openTable shows table t and focuses where it shows (§7.8「打开已有的表」,
// §12). A new tab (C-t, the tree's t, the table picked after a +) opens in
// openTarget's pane. Else a tab of the window that has t is switched to,
// or, with several, picked from the palette; with none, t's first page is
// fetched in place of the target's current tab.
func (a *App) openTable(t db.Table, newTab bool) tea.Cmd {
	p := a.openTarget()
	if p == nil {
		return nil
	}
	if newTab = newTab || a.win().newTabIn == p.ID; !newTab {
		switch open := a.tabsOf(t); len(open) {
		case 0:
		case 1:
			a.showTab(open[0])
			return nil
		default:
			a.palette = &palette{pick: &t}
			return nil
		}
	}
	if cur := dataOf(p); cur != nil && len(cur.edits) > 0 { // never over changes not saved (§12)
		newTab = true
	}
	a.showPane(p.ID)
	tab := Tab{Name: t.Name, Data: newDataTab(t)}
	switch {
	case !newTab && len(p.Tabs) > 0:
		p.Tabs[p.Cur] = tab
	default:
		p.Prev = p.Cur
		if len(p.Tabs) == 0 { // an empty pane: there is no tab to go back to
			p.Prev = -1
		}
		p.Tabs = append(p.Tabs, tab)
		p.Cur = len(p.Tabs) - 1
	}
	return a.fetch(tab.Data, true)
}

// tabAt is a tab of the window: tab i of pane p, which is ⟨n⟩.
type tabAt struct {
	p    *Pane
	n, i int
}

// tabsOf is the window's tabs of table t, in ⟨n⟩ and then tab order.
func (a *App) tabsOf(t db.Table) []tabAt {
	var out []tabAt
	for n, p := range a.panesByNumber() {
		for i, tb := range p.Tabs {
			if tb.Data != nil && idOf(tb.Data.table) == idOf(t) {
				out = append(out, tabAt{p, n, i})
			}
		}
	}
	return out
}

// showTab focuses the tab's pane and switches to it.
func (a *App) showTab(at tabAt) {
	a.showPane(at.p.ID)
	selectTab(at.p, at.i)
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

// newTab is tab.new, a tab bar's +: the tree's filter takes the keys, and
// the table picked next opens in a new tab of the pane it is for (§7.8).
func (a *App) newTab() {
	p := a.openTarget()
	if p == nil {
		return
	}
	// a new pick: the last one's filter would take what is typed after it (§7.8)
	a.win().tree.filter = ui.Input{}
	a.treeFilter()
	if win := a.win(); win.Focus == win.Tree.ID {
		win.newTabIn = p.ID
	}
}

// closeTab closes the focused pane's current tab (:q). Closing the last tab
// closes the pane too, except the sidebar and the window's only pane.
func (a *App) closeTab() {
	p := a.focused()
	if p.Kind == KindSchema || len(p.Tabs) == 0 {
		return
	}
	closed := p.Cur
	p.Tabs = slices.Delete(p.Tabs, closed, closed+1)
	switch {
	case p.Prev > closed: // back to the previous tab, like vim's alternate
		p.Cur = p.Prev - 1
	case p.Prev >= 0 && p.Prev != closed:
		p.Cur = p.Prev
	default:
		p.Cur = max(min(closed, len(p.Tabs)-1), 0)
	}
	p.Prev = -1
	if len(p.Tabs) == 0 {
		a.removePane(p.ID)
	}
}

// removePane takes pane id out of the tree: its sibling gets the space and
// the focus, and any zoom ends. The window's only pane stays.
func (a *App) removePane(id int) {
	win := a.win()
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
	if p.Kind == KindSchema {
		return
	}
	win.lastID++ // never reused, so pane IDs stay stable (§5)
	np := &Pane{ID: win.lastID, Kind: p.Kind, Prev: -1}
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

func (a *App) toggleTree() {
	win := a.win()
	win.TreeOpen = !win.TreeOpen
	if !win.TreeOpen && win.Focus == win.Tree.ID {
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
	case p != nil:
		a.scrollGrid(p, down*wheelStep, right)
	}
}
