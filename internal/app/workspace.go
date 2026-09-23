package app

import (
	"fmt"
	"slices"
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
	Cur, Prev int      // tab bar * and - (T-01)
	Lines     []string // M0 placeholder content
}

// Object is the title's "· name" part: the current tab.
func (p *Pane) Object() string {
	if p.Cur < len(p.Tabs) {
		return p.Tabs[p.Cur]
	}
	return ""
}

type Window struct {
	Tree  *Pane // ⟨0⟩ sidebar, not part of the split tree (D-04)
	Root  *Node
	Focus int // pane ID
}

// fakeWindow is M0's stand-in workspace: no database behind it.
func fakeWindow() *Window {
	data := &Pane{ID: 1, Kind: KindData, Tabs: []string{"t_order", "t_user"}, Prev: 1, Lines: fakeRows()}
	cons := &Pane{ID: 2, Kind: KindConsole, Tabs: []string{"console_1"}, Prev: -1, Lines: fakeSQL}
	return &Window{
		Tree:  &Pane{ID: 0, Kind: KindSchema},
		Root:  &Node{Split: Horiz, Ratio: 5.0 / 9, A: leaf(data), B: leaf(cons)}, // data : console = 5 : 4 (§7.8)
		Focus: 1,
	}
}

type fakeTable struct{ name, rows string }

var fakeTables = []fakeTable{
	{"agent", "124"}, {"agent_version", "530"}, {"goal", "57"},
	{"mt_task", "812"}, {"mt_task_log", "96k"}, {"schema_migrations", "88"},
	{"t_order", "1.2M"}, {"t_order_item", "3.4M"}, {"t_payment", "410k"},
	{"t_refund", "12k"}, {"t_sku", "8.1k"}, {"t_user", "38k"},
	{"t_user_address", "52k"}, {"t_user_profile", "38k"},
}

func fakeRows() []string {
	rows := []string{"WHERE deleted_at is null", "id    biz_type   status     created_at"}
	status := []string{"running", "done", "failed", "pending"}
	biz := []string{"goal", "task", "report"}
	for i := range 60 {
		rows = append(rows, fmt.Sprintf("%-5d %-10s %-10s 2026-09-21 10:%02d", 689+i, biz[i%3], status[i%4], i))
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
	if len(p.Tabs) > 0 {
		return
	}
	if root, heir := a.win.Root.remove(p.ID); root != nil {
		a.win.Root, a.win.Focus = root, heir.ID
	}
}
