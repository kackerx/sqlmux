package app

import (
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// dropKind is what an open dropdown picks: the tree's schema (§8.6), or a
// table's ORDER or LIMIT (§7.8「查询条」).
type dropKind int

const (
	dropSchema dropKind = iota
	dropOrder
	dropLimit
)

// dropdown is the open one-pick dropdown; tab and pane are the table an
// ORDER or LIMIT one acts on.
type dropdown struct {
	kind     dropKind
	tab      *dataTab
	pane     *Pane
	input    ui.Input
	sel, top int
}

var limits = []int{100, 500, 1000} // §8.5

// openDrop opens a dropdown of kind k, for the focused table unless it
// picks a schema; its selection starts on the current value.
func (a *App) openDrop(k dropKind) {
	d := &dropdown{kind: k}
	if k != dropSchema {
		p := a.focused()
		if d.tab = dataOf(p); d.tab == nil || len(d.tab.page.Cols) == 0 {
			return
		}
		d.pane = p
	}
	a.drop = d
	_, d.sel = a.dropItems()
	a.dropMove(0)
}

// dropItems is what the dropdown offers, and which of them is current.
func (a *App) dropItems() (items []string, current int) {
	d := a.drop
	switch d.kind {
	case dropOrder: // first the row identity, "默认"; then the columns
		items = []string{"默认"}
		current = 0
		for i, c := range d.tab.page.Cols {
			items = append(items, c.Name)
			if c.Name == d.tab.order {
				current = i + 1
			}
		}
		return items, current
	case dropLimit:
		for _, n := range limits {
			items = append(items, strconv.Itoa(n))
		}
		return items, slices.Index(limits, d.tab.limit)
	}
	return a.sess.Schemas, slices.Index(a.sess.Schemas, a.sess.Schema)
}

// dropMatches is the items that pass the filter, best first.
func (a *App) dropMatches() []ui.Match {
	items, _ := a.dropItems()
	return ui.Filter(a.drop.input.Text, items)
}

// dropBox is where v, the dropdown as drawn, opens (§8.6, §7.8): under its
// entry, left aligned with it, as wide as its longest item, and for the
// schema at least to the sidebar's right edge.
func (a *App) dropBox(v ui.Dropdown) (uv.Rectangle, int) {
	d := a.drop
	var entry uv.Rectangle
	w := 16
	switch d.kind {
	case dropSchema:
		side := a.sidebarRect()
		entry = uv.Rect(side.Min.X+2, side.Min.Y, 1, 1) // where Block draws the title
		w = side.Max.X - entry.Min.X                    // right border on the sidebar's
	case dropOrder:
		entry = a.chipRect(d.pane, d.tab, "grid.order")
	case dropLimit:
		entry = a.chipRect(d.pane, d.tab, "grid.limit")
	}
	items, _ := a.dropItems()
	for _, s := range items {
		w = max(w, ui.Width(s)+4) // border and padding on both sides
	}
	return v.Box(a.window(), entry, w)
}

func (a *App) dropMove(d int) {
	m := a.drop
	n := len(a.dropMatches())
	_, rows := a.dropBox(a.dropView())
	m.sel = max(min(m.sel+d, n-1), 0)
	m.top = max(min(m.top, m.sel), m.sel-rows+1)
}

// dropPick takes the item at i and closes the dropdown. A schema starts the
// tree over on its first table; ORDER and LIMIT refetch from the first page.
func (a *App) dropPick(i int) tea.Cmd {
	d, ms := a.drop, a.dropMatches()
	items, _ := a.dropItems()
	if a.drop = nil; i >= len(ms) {
		return nil
	}
	at := ms[i].Index
	item := items[at]
	switch t := d.tab; d.kind {
	case dropSchema:
		a.sess.Schema = item
		a.win().tree = treeState{}
		return nil
	case dropOrder:
		switch {
		case at == 0: // 默认: by the row identity
			t.order, t.desc = "", false
		case item == t.order: // the current column again: the other way
			t.desc = !t.desc
		case t.order == "" && slices.Equal(t.cols.Key(), []string{item}): // the key the chip shows is the current column
			t.order, t.desc = item, true
		default:
			t.order, t.desc = item, false
		}
	case dropLimit:
		t.limit, _ = strconv.Atoi(item)
	}
	d.tab.pageNo = 0
	return a.fetch(d.tab, false)
}

func (a *App) dropKey(k keymap.Key) {
	if editInput(&a.drop.input, k) {
		a.drop.sel, a.drop.top = 0, 0
	}
}

func (a *App) dropView() ui.Dropdown {
	v := ui.Dropdown{Search: a.icons.Search, Input: a.drop.input, Typing: true, Mark: -1, Sel: a.drop.sel, Top: a.drop.top}
	items, current := a.dropItems()
	for i, m := range a.dropMatches() {
		if m.Index == current {
			v.Mark = i
		}
		v.Items, v.Pos = append(v.Items, items[m.Index]), append(v.Pos, m.Pos)
	}
	return v
}
