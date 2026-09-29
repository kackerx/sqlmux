package app

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// dropKind is what an open dropdown picks: a table's ORDER or LIMIT (§7.8
// 「查询条」), a console's schema (§8.6).
type dropKind int

const (
	dropOrder dropKind = iota
	dropLimit
	dropSchema
)

// dropdown is the open one-pick dropdown; tab or console, in pane, is what
// it acts on.
type dropdown struct {
	kind     dropKind
	tab      *dataTab
	console  *consoleTab
	pane     *Pane
	input    ui.Input
	sel, top int
}

var limits = []int{100, 500, 1000} // §8.5

// maxLimit caps a page size typed in: a page is all in memory (§7.8).
const maxLimit = 10000

// limitTyped is the page size typed in LIMIT's filter (§7.8): a positive
// whole number, capped at maxLimit; 0 when what is typed is not one.
func limitTyped(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return min(n, maxLimit)
}

// openDrop opens a dropdown of kind k for the focused table; its
// selection starts on the current value.
func (a *App) openDrop(k dropKind) {
	p := a.focused()
	d := &dropdown{kind: k, tab: dataOf(p), pane: p}
	if d.tab == nil || len(d.tab.page.Cols) == 0 {
		return
	}
	a.drop = d
	_, d.sel = a.dropItems()
	a.dropMove(0)
}

// openSchemaDrop opens the focused console's schema dropdown (gs, §8.6),
// its selection on the schema it has.
func (a *App) openSchemaDrop() {
	p := a.focused()
	if c := consoleOf(p); c != nil && len(a.sess.Schemas) > 0 {
		a.drop = &dropdown{kind: dropSchema, console: c, pane: p}
		_, a.drop.sel = a.dropItems()
		a.dropMove(0)
	}
}

// dropItems is what the dropdown offers, and which of them is current.
func (a *App) dropItems() (items []string, current int) {
	d := a.drop
	switch d.kind {
	case dropSchema:
		return a.sess.Schemas, slices.Index(a.sess.Schemas, d.console.schema)
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
	case dropLimit: // a number typed goes first
		if n := limitTyped(d.input.Text); n > 0 {
			items = append(items, strconv.Itoa(n))
		}
		for _, n := range limits {
			items = append(items, strconv.Itoa(n))
		}
		return items, slices.Index(items, strconv.Itoa(d.tab.limit))
	}
	return nil, -1
}

// dropMatches is the items that pass the filter, best first; a page size
// typed in is first whatever the filter makes of it, the same preset
// dropped (§7.8).
func (a *App) dropMatches() []ui.Match {
	items, _ := a.dropItems()
	d := a.drop
	if d.kind != dropLimit || limitTyped(d.input.Text) == 0 {
		return ui.Filter(d.input.Text, items)
	}
	ms := []ui.Match{{Index: 0}}
	for _, m := range ui.Filter(d.input.Text, items[1:]) {
		if m.Index++; items[m.Index] != items[0] {
			ms = append(ms, m)
		}
	}
	return ms
}

// dropBox is where v, the dropdown as drawn, opens (§8.6, §7.8): under its
// chip, left aligned with it, as wide as its longest item. A schema one
// opens under the console title's button, to the pane's right border at
// least; under the title's right end when the button was left out.
func (a *App) dropBox(v ui.Dropdown) (uv.Rectangle, int) {
	d := a.drop
	var entry uv.Rectangle
	w := 16
	switch d.kind {
	case dropSchema:
		r := a.layout()[d.pane.ID]
		entry = uv.Rect(r.Max.X-1, r.Min.Y, 1, 1)
		for _, h := range a.hits { // the button as the last frame drew it
			if h.Target == (ui.Target{Kind: ui.KindHint, Pane: d.pane.ID, Action: "console.schema"}) {
				entry = h.Rect
			}
		}
		w = max(w, r.Max.X-entry.Min.X)
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

// dropPick takes the item at i, closes the dropdown and refetches from the
// first page.
func (a *App) dropPick(i int) tea.Cmd {
	d, ms := a.drop, a.dropMatches()
	items, _ := a.dropItems()
	if a.drop = nil; i >= len(ms) {
		return nil
	}
	at := ms[i].Index
	item := items[at]
	if d.kind == dropSchema { // the next run sets it (§8.6)
		d.console.schema = item
		return nil
	}
	switch t := d.tab; d.kind {
	case dropOrder:
		switch {
		case at == 0: // 默认: by the row identity
			t.order, t.desc = "", false
		case item == t.order, t.order == "" && len(t.cols.Key()) > 0 && t.cols.Key()[0] == item: // the column the chip shows first: the other way (§7.8)
			return a.toggleOrder(t)
		default:
			t.order, t.desc = item, false
		}
	case dropLimit:
		t.limit, _ = strconv.Atoi(item)
	}
	d.tab.pageNo = 0
	return a.fetch(d.tab, false)
}

// toggleOrder turns t's ORDER the other way, from the first page (§7.8):
// sorted by default, its row identity's first column goes descending.
func (a *App) toggleOrder(t *dataTab) tea.Cmd {
	switch key := t.cols.Key(); {
	case t.order != "":
		t.desc = !t.desc
	case key != nil:
		t.order, t.desc = key[0], true
	default: // nothing to turn: the chip shows no direction
		return nil
	}
	t.pageNo = 0
	return a.fetch(t, false)
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
