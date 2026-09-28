package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/db"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// palette is the open command palette (§12).
type palette struct {
	input    ui.Input
	sel, top int       // selected candidate, first one shown
	pick     *db.Table // listing its tabs instead, to pick one: it is open in several (§7.8「打开已有的表」)

	// Quick SQL's (§12).
	comp  *completion
	quick *quickSQL
	asked map[tableID]bool // tables whose columns were fetched for completion
}

// itemKind is what a palette row stands for (K-03), in the order an empty
// input lists them.
type itemKind int

const (
	itemWindow itemKind = iota
	itemPane
	itemTable
	itemCommand
	itemTab // an open tab of the table being picked
	itemSQL // a quick SQL from the history
)

var itemTags = [...]string{itemWindow: "窗口", itemPane: "Pane", itemTable: "表", itemCommand: "命令", itemTab: "tab", itemSQL: "SQL"}

// scopes are the palette's tabs (K-02). The prefix typed in the input is the
// only scope state: Tab rewrites it, and `:` is `>` typed (§12).
var scopes = []struct {
	label, prefix string
	kinds         []itemKind
}{
	{"所有", "", []itemKind{itemWindow, itemPane, itemTable, itemCommand}}, // not the SQL history (§12)
	{"窗口·Pane", "%", []itemKind{itemWindow, itemPane}},
	{"表", "@", []itemKind{itemTable}},
	{"命令", ">", []itemKind{itemCommand}},
	{"SQL", ";", []itemKind{itemSQL}}, // what follows the ; is SQL, the history while there is none
}

// sqlScope is where the input is quick SQL (§12).
const sqlScope = 4

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

// recent is how state.json keeps it (§14).
func (it paletteItem) recent() config.Recent {
	kind := [...]string{itemWindow: "window", itemPane: "pane", itemTable: "table", itemCommand: "command", itemTab: "tab", itemSQL: "sql"}[it.kind]
	return config.Recent{Kind: kind, ID: it.id}
}

// recentRows is how many palette picks state.json keeps.
const recentRows = 50

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
// order: windows, panes by ⟨n⟩, tables, commands by action id (§12).
func (a *App) paletteItems() []paletteItem {
	var items []paletteItem
	for i, w := range a.sess.Windows {
		items = append(items, paletteItem{itemWindow, strconv.Itoa(i), a.icons.Window, fmt.Sprintf("%d: %s", i, w.Name), a.sess.Name})
	}
	win := fmt.Sprintf("%d: %s", a.sess.Active, a.win().Name)
	for n, p := range a.panesByNumber() {
		name := a.icons.Number(n) + " " + p.Kind.String()
		if p.Object() != "" {
			name += " · " + p.Object()
		}
		items = append(items, paletteItem{itemPane, strconv.Itoa(p.ID), a.kindIcon(p.Kind), name, win})
	}
	// the tree's schema first, as the tree lists it, then the others (§12)
	for _, here := range []bool{true, false} {
		for _, t := range a.sess.Tables {
			if (t.Schema == a.sess.Schema) == here {
				items = append(items, paletteItem{itemTable, t.Schema + "." + t.Name, a.icons.Table, t.Name, a.sess.Name + "." + t.Schema})
			}
		}
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
	for _, sql := range a.state.SQL[a.sess.Name] {
		items = append(items, paletteItem{itemSQL, sql, a.icons.Console, sql, ""})
	}
	recent := func(it paletteItem) int {
		if i := slices.Index(a.state.Recent, it.recent()); i >= 0 {
			return i
		}
		return len(a.state.Recent)
	}
	slices.SortStableFunc(items, func(x, y paletteItem) int { return recent(x) - recent(y) })
	return items
}

// paletteMatches ranks the candidates in scope for what is typed: fzf over
// "name where", every kind mixed by score (§12). Picking a tab, they are
// the tabs.
func (a *App) paletteMatches() (items []paletteItem, ms []ui.Match) {
	scope, query := a.paletteScope()
	switch t := a.palette.pick; {
	case t != nil:
		items, query = a.tabItems(*t), a.palette.input.Text
	case scope == sqlScope && strings.TrimSpace(query) != "": // SQL typed: nothing to list
	default:
		for _, it := range a.paletteItems() {
			if slices.Contains(scopes[scope].kinds, it.kind) {
				items = append(items, it)
			}
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

// tabItems is the window's tabs of t: where each is, "① · 2", and what it
// shows when not all rows in the default order, in dim (§7.8「打开已有的表」).
func (a *App) tabItems(t db.Table) []paletteItem {
	var items []paletteItem
	for k, at := range a.tabsOf(t) {
		where := []string{a.icons.Number(at.n) + " · " + strconv.Itoa(at.i+1)}
		s := at.p.Tabs[at.i].Data.shown
		if strings.TrimSpace(s.applied) != "" {
			where = append(where, s.applied)
		}
		if s.order != "" {
			where = append(where, sortedBy(s.order, s.desc))
		}
		items = append(items, paletteItem{itemTab, strconv.Itoa(k), a.icons.Table, t.Name, strings.Join(where, " · ")})
	}
	return items
}

// paletteView is what the palette draws.
func (a *App) paletteView() ui.Palette {
	items, ms := a.paletteMatches()
	scope, query := a.paletteScope()
	p := ui.Palette{Search: a.icons.Search, Input: a.palette.input, Scope: scope, Sel: a.palette.sel, Top: a.palette.top}
	for _, s := range scopes {
		p.Scopes = append(p.Scopes, strings.TrimSpace(s.label+" "+s.prefix)) // "表 @": the tab says what to type
	}
	if a.palette.pick != nil { // no scopes: only the tabs are there to pick
		p.Scopes, p.Scope = []string{"选择 tab"}, 0
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
	scopeKeys := a.hints("palette", "/", "palette.scope.next", "palette.scope.prev")
	if a.palette.pick != nil {
		scopeKeys = ""
	}
	p.Footer = bound(
		ui.Hint{Key: a.hints("palette", "/", "palette.up", "palette.down"), Label: "移动"},
		ui.Hint{Key: scopeKeys, Label: "范围"},
		ui.Hint{Key: a.keys.Hint("palette.close", "palette"), Label: "关闭", Action: "palette.close"},
	)
	switch run := a.keys.Hint("palette.run", "palette"); {
	case scope == sqlScope && a.palette.pick == nil && strings.TrimSpace(query) != "":
		label := "执行"
		if q := a.palette.quick; q != nil && run != "" && query != q.last() { // F-03
			run, label = "已修改，"+run, "重新执行"
		}
		p.Enter = bound(ui.Hint{Key: run, Label: label, Action: "palette.run"})
	case a.palette.sel < len(ms):
		it := items[ms[a.palette.sel].Index]
		enter := [...]string{itemWindow: "切换", itemPane: "聚焦", itemTable: "打开", itemCommand: "执行", itemTab: "切过去", itemSQL: "执行"}[it.kind]
		if it.kind == itemCommand && actions[it.id].On != nil {
			enter = "切换"
		}
		p.Enter = bound(ui.Hint{Key: run, Label: enter, Action: "palette.run"})
		if it.kind == itemTable || it.kind == itemTab {
			p.Enter = append(p.Enter, bound(ui.Hint{Key: a.keys.Hint("palette.open.tab", "palette"), Label: "新 tab", Action: "palette.open.tab"})...)
		}
	}
	if a.quickShows() {
		p.Result = a.quickView(a.palette.quick)
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
	_, rows, _ := a.paletteBox(len(ms))
	p.top = max(min(p.top, p.sel), p.sel-rows+1)
}

// paletteBox is where the palette sits for n candidates, and its result's
// table goes: quick SQL has one once it has run.
func (a *App) paletteBox(n int) (box uv.Rectangle, rows int, grid uv.Rectangle) {
	return ui.PaletteBox(a.window(), n, a.quickShows())
}

// quickShows is whether the palette shows a quick SQL's result area.
func (a *App) quickShows() bool {
	s, _ := a.paletteScope()
	return s == sqlScope && a.palette.pick == nil && a.palette.quick != nil
}

// paletteScopeTo switches to scope i, wrapping around, by rewriting the
// input's prefix; what was typed after it stays.
func (a *App) paletteScopeTo(i int) {
	if a.palette.pick != nil {
		return
	}
	scope, query := a.paletteScope()
	i = (i + len(scopes)) % len(scopes)
	pos := max(a.palette.input.Pos-len(scopes[scope].prefix), 0) + len(scopes[i].prefix)
	a.palette.input = ui.Input{Text: scopes[i].prefix + query, Pos: pos}
	a.palette.sel, a.palette.top, a.palette.comp = 0, 0, nil
}

// paletteRun runs candidate i (K-04); newTab is C-t, which only tables and
// the tab being picked take.
// A toggle leaves the palette open, so its ON / OFF can be seen to change
// (§12); a window only closes it until windows can switch (M5).
func (a *App) paletteRun(i int, newTab bool) tea.Cmd {
	items, ms := a.paletteMatches()
	if t := a.palette.pick; t != nil && (newTab || i < len(ms)) { // C-t opens t again whatever is picked
		a.palette = nil
		if newTab {
			return a.openTable(*t, true)
		}
		k, _ := strconv.Atoi(items[ms[i].Index].id)
		a.showTab(a.tabsOf(*t)[k])
		return nil
	}
	if scope, sql := a.paletteScope(); scope == sqlScope { // run what is typed, or the history's pick
		if strings.TrimSpace(sql) == "" && i < len(ms) {
			sql = items[ms[i].Index].id
			a.palette.input = ui.Input{Text: scopes[scope].prefix + sql, Pos: len(scopes[scope].prefix + sql)}
		}
		if newTab || strings.TrimSpace(sql) == "" { // C-t sends a result to a pane: M3
			return nil
		}
		return a.runQuick(sql)
	}
	if i >= len(ms) {
		return nil
	}
	it := items[ms[i].Index]
	if newTab && it.kind != itemTable {
		return nil
	}
	r := it.recent()
	a.state.Recent = slices.Insert(slices.DeleteFunc(a.state.Recent, func(k config.Recent) bool { return k == r }), 0, r)
	a.state.Recent = a.state.Recent[:min(len(a.state.Recent), recentRows)]
	return tea.Batch(a.saveState(), a.paletteDo(it, newTab))
}

// paletteDo does what candidate it stands for.
func (a *App) paletteDo(it paletteItem, newTab bool) tea.Cmd {
	if it.kind == itemCommand && actions[it.id].On != nil {
		cmd := a.run(it.id, 0)
		items, ms := a.paletteMatches() // it may have moved up among the recent ones
		a.palette.sel = slices.IndexFunc(ms, func(m ui.Match) bool { return items[m.Index].key() == it.key() })
		a.paletteMove(0) // and the list scrolls to it
		return cmd
	}
	a.palette = nil
	switch it.kind {
	case itemCommand:
		return a.run(it.id, 0)
	case itemTable:
		if i := slices.IndexFunc(a.sess.Tables, func(t db.Table) bool { return t.Schema+"."+t.Name == it.id }); i >= 0 {
			return a.openTable(a.sess.Tables[i], newTab)
		}
	case itemPane:
		id, _ := strconv.Atoi(it.id)
		a.showPane(id)
	}
	return nil
}

// paletteKey edits the palette's input. With quick SQL's candidates up, ↵
// takes the one picked on purpose, else runs, and esc closes the list
// first (§9.7).
func (a *App) paletteKey(k keymap.Key) tea.Cmd {
	p := a.palette
	switch {
	case p.comp != nil && k == "<CR>" && p.comp.chosen:
		a.acceptCompletion()
		return nil
	case p.comp != nil && k == "<CR>":
		p.comp = nil
		return a.paletteRun(p.sel, false)
	case p.comp != nil && k == keymap.Esc:
		p.comp = nil
		return nil
	}
	if editInput(&p.input, k) {
		p.sel, p.top = 0, 0
	}
	return a.completeSQL()
}
