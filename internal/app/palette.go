package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// palette is the open command palette (§12).
type palette struct {
	input    ui.Input
	sel, top int // selected candidate, first one shown
}

// itemKind is what a palette row stands for (K-03), in the order an empty
// input lists them.
type itemKind int

const (
	itemWindow itemKind = iota
	itemPane
	itemTable
	itemCommand
)

var itemTags = [...]string{itemWindow: "窗口", itemPane: "Pane", itemTable: "表", itemCommand: "命令"}

// scopes are the palette's tabs (K-02). The prefix typed in the input is the
// only scope state: Tab rewrites it, and `:` is `>` typed (§12).
var scopes = []struct {
	label, prefix string
	kinds         []itemKind // nil: everything
}{
	{"所有", "", nil},
	{"窗口·Pane", "%", []itemKind{itemWindow, itemPane}},
	{"表", "@", []itemKind{itemTable}},
	{"命令", ">", []itemKind{itemCommand}},
}

// paletteItem is one candidate. id tells it apart within its kind: the
// action, the table, the pane's ID or the window's number.
type paletteItem struct {
	kind        itemKind
	id          string
	icon        ui.Icon
	name, where string
}

type itemKey struct {
	kind itemKind
	id   string
}

func (it paletteItem) key() itemKey { return itemKey{it.kind, it.id} }

// exAliases rank their command first when typed exactly in the command
// scope, so :q↵ and :qa↵ work as they always have (§12).
var exAliases = map[string]string{"q": "tab.close", "qa": "quit", "w": "save"}

func (a *App) openPalette(text string) {
	a.palette = &palette{input: ui.Input{Text: text, Pos: len(text)}}
}

// paletteScope splits the input into its scope and the query after the prefix.
func (a *App) paletteScope() (scope int, query string) {
	for i, s := range scopes[1:] {
		if q, ok := strings.CutPrefix(a.palette.input.Text, s.prefix); ok {
			return i + 1, q
		}
	}
	return 0, a.palette.input.Text
}

// paletteItems is every candidate, recent ones first and the rest in kind
// order: windows, panes by ⟨n⟩, tables as the sidebar lists them, commands
// by action id (§12).
func (a *App) paletteItems() []paletteItem {
	var items []paletteItem
	for i, w := range a.sess.Windows {
		items = append(items, paletteItem{itemWindow, strconv.Itoa(i), a.icons.Window, fmt.Sprintf("%d: %s", i, w.Name), a.sess.Name})
	}
	win := fmt.Sprintf("%d: %s", a.sess.Active, a.win().Name)
	for n, p := range append([]*Pane{a.win().Tree}, a.win().Root.Leaves()...) {
		name := a.icons.Number(n) + " " + p.Kind.String()
		if p.Object() != "" {
			name += " · " + p.Object()
		}
		items = append(items, paletteItem{itemPane, strconv.Itoa(p.ID), a.kindIcon(p.Kind), name, win})
	}
	for _, t := range fakeTables { // ponytail: M0's one fake schema; M1 lists the catalog's
		items = append(items, paletteItem{itemTable, t.name, a.icons.Table, t.name, a.sess.Name + ".public"})
	}
	var ids []string
	for id, act := range actions {
		if act.Title != "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		items = append(items, paletteItem{itemCommand, id, a.icons.Command, actions[id].Title, id})
	}
	recent := func(it paletteItem) int {
		if i := slices.Index(a.recent, it.key()); i >= 0 {
			return i
		}
		return len(a.recent)
	}
	slices.SortStableFunc(items, func(x, y paletteItem) int { return recent(x) - recent(y) })
	return items
}

// paletteMatches ranks the candidates in scope for what is typed: fzf over
// "name where", every kind mixed by score (§12).
func (a *App) paletteMatches() (items []paletteItem, ms []ui.Match) {
	scope, query := a.paletteScope()
	for _, it := range a.paletteItems() {
		if kinds := scopes[scope].kinds; kinds == nil || slices.Contains(kinds, it.kind) {
			items = append(items, it)
		}
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.name + " " + it.where
	}
	ms = ui.Filter(query, texts)
	alias := itemKey{itemCommand, exAliases[strings.TrimSpace(query)]}
	if i := slices.IndexFunc(items, func(it paletteItem) bool { return it.key() == alias }); i >= 0 && scopes[scope].prefix == ">" {
		ms = slices.Insert(slices.DeleteFunc(ms, func(m ui.Match) bool { return m.Index == i }), 0, ui.Match{Index: i})
	}
	return items, ms
}

// paletteView is what the palette draws.
func (a *App) paletteView() ui.Palette {
	items, ms := a.paletteMatches()
	scope, _ := a.paletteScope()
	p := ui.Palette{Input: a.palette.input, Scope: scope, Sel: a.palette.sel, Top: a.palette.top}
	for _, s := range scopes {
		p.Scopes = append(p.Scopes, s.label)
	}
	for _, m := range ms {
		it := items[m.Index]
		right := ""
		if it.kind == itemCommand {
			right = a.keyFor(it.id)
			if on := actions[it.id].On; on != nil {
				right = map[bool]string{true: "ON", false: "OFF"}[on(a)]
			}
		}
		p.Rows = append(p.Rows, ui.PaletteRow{Icon: it.icon, Name: it.name, Where: it.where, Pos: m.Pos, Right: right, Tag: itemTags[it.kind]})
	}
	p.Footer = bound(
		ui.Hint{Key: a.hints("palette", "/", "palette.up", "palette.down"), Label: "移动"},
		ui.Hint{Key: a.hints("palette", "/", "palette.scope.next", "palette.scope.prev"), Label: "范围"},
		ui.Hint{Key: a.keys.Hint("palette.close", "palette"), Label: "关闭", Action: "palette.close"},
	)
	if a.palette.sel < len(ms) {
		it := items[ms[a.palette.sel].Index]
		enter := [...]string{itemWindow: "切换", itemPane: "聚焦", itemTable: "打开", itemCommand: "执行"}[it.kind]
		if it.kind == itemCommand && actions[it.id].On != nil {
			enter = "切换"
		}
		p.Enter = bound(ui.Hint{Key: a.keys.Hint("palette.run", "palette"), Label: enter, Action: "palette.run"})
		if it.kind == itemTable {
			p.Enter = append(p.Enter, bound(ui.Hint{Key: a.keys.Hint("palette.open.tab", "palette"), Label: "新 tab", Action: "palette.open.tab"})...)
		}
	}
	return p
}

// keyFor is the key an action has where the user is: the focused pane's
// scope, then NORMAL, then global.
func (a *App) keyFor(id string) string {
	for _, scope := range []string{a.paneScope(), "normal", "global"} {
		if k := a.keys.Hint(id, scope); k != "" {
			return k
		}
	}
	return ""
}

// paletteMove moves the selection by d, scrolling the list to keep it shown.
func (a *App) paletteMove(d int) {
	_, ms := a.paletteMatches()
	p := a.palette
	p.sel = max(min(p.sel+d, len(ms)-1), 0)
	_, rows := ui.PaletteBox(a.window(), len(ms))
	p.top = max(min(p.top, p.sel), p.sel-rows+1)
}

// paletteScopeTo switches to scope i, wrapping around, by rewriting the
// input's prefix; what was typed after it stays.
func (a *App) paletteScopeTo(i int) {
	scope, query := a.paletteScope()
	i = (i + len(scopes)) % len(scopes)
	pos := max(a.palette.input.Pos-len(scopes[scope].prefix), 0) + len(scopes[i].prefix)
	a.palette.input = ui.Input{Text: scopes[i].prefix + query, Pos: pos}
	a.palette.sel, a.palette.top = 0, 0
}

// paletteRun runs candidate i (K-04); newTab is C-t, which only tables take.
// A toggle leaves the palette open, so its ON / OFF can be seen to change
// (§12); a window only closes it until windows can switch (M5).
func (a *App) paletteRun(i int, newTab bool) tea.Cmd {
	items, ms := a.paletteMatches()
	if i >= len(ms) {
		return nil
	}
	it := items[ms[i].Index]
	if newTab && it.kind != itemTable {
		return nil
	}
	a.recent = slices.Insert(slices.DeleteFunc(a.recent, func(k itemKey) bool { return k == it.key() }), 0, it.key())
	if it.kind == itemCommand && actions[it.id].On != nil {
		cmd := a.run(it.id, 0)
		items, ms = a.paletteMatches() // it may have moved up among the recent ones
		a.palette.sel = slices.IndexFunc(ms, func(m ui.Match) bool { return items[m.Index].key() == it.key() })
		a.paletteMove(0) // and the list scrolls to it
		return cmd
	}
	a.palette = nil
	switch it.kind {
	case itemCommand:
		return a.run(it.id, 0)
	case itemTable:
		a.openTable(it.id, newTab)
	case itemPane:
		id, _ := strconv.Atoi(it.id)
		a.showPane(id)
	}
	return nil
}

// paletteKey edits the palette's input. Its own editing keys are not
// bindings, as with any input.
func (a *App) paletteKey(k keymap.Key) {
	in := &a.palette.input
	switch k {
	case "<Left>":
		in.Left()
		return
	case "<Right>":
		in.Right()
		return
	case "<BS>":
		in.Backspace()
	default:
		if keymap.Text(k) == "" {
			return
		}
		in.Insert(keymap.Text(k))
	}
	a.palette.sel, a.palette.top = 0, 0
}
