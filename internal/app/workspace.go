package app

import (
	"fmt"
	"slices"
	"strconv"

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
	Tabs      []string
	Cur, Prev int        // tab bar * and - (T-01)
	Lines     []string   // M0 placeholder content
	Rows      [][]string // data pane: the placeholder table under Lines
	Scroll    int        // first placeholder line (or row) shown; the mouse wheel moves it
}

// Object is the title's "· name" part: the current tab.
func (p *Pane) Object() string {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur]
	}
	return ""
}

type Window struct {
	Name     string
	TreeOpen bool  // the ⟨0⟩ sidebar is open, not folded to its thin bar
	Tree     *Pane // ⟨0⟩ sidebar, not part of the split tree (D-04)
	Root     *Node
	Focus    int // pane ID
	Zoom     int // zoomed pane ID; 0 = none (P-03)
	lastID   int // highest pane ID handed out

	focusTick int
	focusedAt map[int]int // pane ID → focusTick when it last got focus
}

// focus moves focus to pane id, remembering when: moving by direction
// prefers the neighbour focused most recently (§5).
func (w *Window) focus(id int) {
	if w.focusedAt == nil {
		w.focusedAt = map[int]int{}
	}
	w.focusTick++
	w.Focus, w.focusedAt[id] = id, w.focusTick
}

// Session is one connection (tech-design §5).
type Session struct {
	Name, Engine string
	Addr         string // shown in the status bar, e.g. pg@localhost:5432
	Windows      []*Window
	Active       int
}

func (a *App) win() *Window { return a.sess.Windows[a.sess.Active] }

// fakeSession is M0's stand-in workspace: no database behind it.
func fakeSession() *Session {
	data := &Pane{ID: 1, Kind: KindData, Tabs: []string{"t_order", "t_user"}, Prev: 1,
		Lines: []string{"WHERE deleted_at is null"}, Rows: fakeGrid()}
	cons := &Pane{ID: 2, Kind: KindConsole, Tabs: []string{"console_1"}, Prev: -1, Lines: fakeSQL}
	main := &Window{
		Name:     "data",
		TreeOpen: true,
		Tree:     &Pane{ID: 0, Kind: KindSchema},
		Root:     &Node{Split: Horiz, Ratio: 5.0 / 9, A: leaf(data), B: leaf(cons)}, // data : console = 5 : 4 (§7.8)
		Focus:    1,
		lastID:   2,
	}
	// ponytail: the second window only shows in the status bar's window list;
	// switching windows is M5.
	return &Session{Name: "doraemon", Engine: "postgres", Addr: "pg@localhost:5432",
		Windows: []*Window{main, {Name: "report"}}}
}

type fakeTable struct{ name, rows string }

var fakeTables = []fakeTable{
	{"agent", "124"}, {"agent_version", "530"}, {"goal", "57"},
	{"mt_task", "812"}, {"mt_task_log", "96k"}, {"schema_migrations", "88"},
	{"t_order", "1.2M"}, {"t_order_item", "3.4M"}, {"t_payment", "410k"},
	{"t_refund", "12k"}, {"t_sku", "8.1k"}, {"t_user", "38k"},
	{"t_user_address", "52k"}, {"t_user_profile", "38k"},
}

// fakeCols and fakeGrid are the data pane's M0 stand-in table.
var fakeCols = []ui.GridCol{
	{Name: "id", PK: true, Numeric: true}, {Name: "biz_type"}, {Name: "status"}, {Name: "created_at"},
}

func fakeGrid() [][]string {
	status := []string{"running", "done", "failed", "pending"}
	biz := []string{"goal", "task", "report"}
	var rows [][]string
	for i := range 60 {
		rows = append(rows, []string{fmt.Sprint(689 + i), biz[i%3], status[i%4], fmt.Sprintf("2026-09-21 10:%02d:00", i)})
	}
	return rows
}

var fakeSQL = []string{
	"select * from mt_task",
	"where status = 'running';",
	"",
	"select id, biz_type, status",
	"from t_order",
	"where deleted_at is null",
	"order by created_at desc;",
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
	order := map[int]int{}
	for n, p := range a.panesByNumber() {
		order[p.ID] = n
	}
	// most recently focused first; never focused, then the one first in ⟨n⟩ order (up / left)
	prefer := func(x, y int) bool {
		if win.focusedAt[x] != win.focusedAt[y] {
			return win.focusedAt[x] > win.focusedAt[y]
		}
		return order[x] < order[y]
	}
	if id, ok := neighbor(rects, win.Focus, side, prefer); ok {
		win.focus(id)
	}
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

// focusPane gives focus to pane id if it is on screen (a click).
func (a *App) focusPane(id int) {
	if _, ok := a.layout()[id]; ok && (id != a.win().Tree.ID || a.win().TreeOpen) {
		a.win().focus(id)
	}
}

// wheelStep is how many placeholder lines one wheel notch scrolls.
const wheelStep = 3

// scrollPane scrolls pane id by notches (negative: up), within its content.
func (a *App) scrollPane(id, notches int) {
	for _, p := range a.panesByNumber() {
		if p.ID != id {
			continue
		}
		n := len(p.Lines)
		switch p.Kind {
		case KindSchema:
			n = len(fakeTables)
		case KindData:
			n = len(p.Rows)
		}
		p.Scroll = min(max(p.Scroll+notches*wheelStep, 0), max(n-1, 0))
	}
}
