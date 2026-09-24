package app

import (
	"fmt"
	"slices"

	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// colsMenu is the open COLS list (Q-04, §7.8): which of a table's columns
// the grid shows. The keys are the list's until / gives them to the filter.
type colsMenu struct {
	tab      *dataTab
	pane     *Pane
	filter   ui.Input
	typing   bool // the filter has the keys
	sel, top int
}

func (a *App) openCols() {
	p := a.focused()
	if t := dataOf(p); t != nil && len(t.page.Cols) > 0 {
		a.cols = &colsMenu{tab: t, pane: p}
	}
}

// colsMatches is the columns that pass the filter, in the table's order.
func (a *App) colsMatches() []ui.Match {
	var names []string
	for _, c := range a.cols.tab.page.Cols {
		names = append(names, c.Name)
	}
	ms := ui.Filter(a.cols.filter.Text, names)
	slices.SortFunc(ms, func(x, y ui.Match) int { return x.Index - y.Index })
	return ms
}

// colsBox is where v, the list as drawn, opens: under the COLS chip.
func (a *App) colsBox(v ui.Dropdown) (uv.Rectangle, int) {
	m := a.cols
	w := 30
	for _, c := range m.tab.page.Cols {
		w = max(w, ui.Width(c.Name+"  "+m.tab.typeOf(c.Name))+10) // box, key, borders, padding
	}
	return v.Box(a.window(), a.chipRect(m.pane, m.tab, "grid.cols"), w)
}

func (a *App) colsMove(d int) {
	m := a.cols
	n := len(a.colsMatches())
	_, rows := a.colsBox(a.colsView())
	m.sel = max(min(m.sel+d, n-1), 0)
	m.top = max(min(m.top, m.sel), m.sel-rows+1)
}

// colsToggle shows or hides the column at row i of the list.
func (a *App) colsToggle(i int) {
	if ms := a.colsMatches(); i < len(ms) {
		name := a.cols.tab.page.Cols[ms[i].Index].Name
		a.colsSet(func(n string) bool { return n == name }, a.cols.tab.hidden[name])
	}
}

// colsSetAll shows or hides every column the filter lets through (a / A).
func (a *App) colsSetAll(show bool) { a.colsSet(func(string) bool { return true }, show) }

// colsSet shows or hides the listed columns which, and moves the cursor off
// a column it hides.
func (a *App) colsSet(which func(name string) bool, show bool) {
	t := a.cols.tab
	at := t.fieldAt(t.col)
	for _, m := range a.colsMatches() {
		if name := t.page.Cols[m.Index].Name; which(name) {
			t.hidden[name] = !show
		}
	}
	t.col = t.nearestShown(at)
}

// colsEsc clears the filter, and when there is none, closes the list (Q-04).
func (a *App) colsEsc() {
	if a.cols.filter.Text != "" {
		a.cols.filter, a.cols.sel, a.cols.top = ui.Input{}, 0, 0
		return
	}
	a.cols = nil
}

// colsFilterKey edits the filter; ↵ and esc give the keys back to the list,
// the text kept (§7.8).
func (a *App) colsFilterKey(k keymap.Key) {
	m := a.cols
	switch k {
	case "<CR>", keymap.Esc:
		m.typing = false
	default:
		if editInput(&m.filter, k) {
			m.sel, m.top = 0, 0
		}
	}
}

func (a *App) colsView() ui.Dropdown {
	m := a.cols
	t := m.tab
	ms := a.colsMatches()
	v := ui.Dropdown{
		Search: a.icons.Filter, Input: m.filter, Typing: m.typing, Mark: -1, Sel: m.sel, Top: m.top,
		Pane:  m.pane.ID,
		Count: fmt.Sprintf("%d/%d", len(ms), len(t.page.Cols)),
		Hints: bound(
			ui.Hint{Key: a.keys.Hint("cols.all", "cols"), Label: "全选", Action: "cols.all"},
			ui.Hint{Key: a.keys.Hint("cols.none", "cols"), Label: "全不选", Action: "cols.none"},
		),
	}
	for _, mt := range ms {
		name := t.page.Cols[mt.Index].Name
		note := t.typeOf(name)
		if slices.Contains(t.cols.PK, name) {
			note += " " + a.icons.Key.Text
		}
		v.Items, v.Pos = append(v.Items, name), append(v.Pos, mt.Pos)
		v.Checks, v.Notes = append(v.Checks, !t.hidden[name]), append(v.Notes, note)
	}
	return v
}
