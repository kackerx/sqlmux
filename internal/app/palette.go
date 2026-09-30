package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// palette is the open command palette (§12).
type palette struct {
	input    ui.Input
	sel, top int   // selected candidate, first one shown
	into     *Pane // a landing tab's 打开表: the table picked opens there (§5)

	// The table the selection was on when last looked at, the preview's
	// timer for it, and what stops the DDL fetches out once the palette
	// closes (F4.2).
	preview    tableID
	previewSeq int
	fetches    context.Context
	stop       context.CancelFunc

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
	itemTab // an open tab, any window's (F4.3)
	itemTable
	itemCommand
	itemSQL // a quick SQL from the history
)

// itemTags name a row's kind. The SQL history, a list of one kind, has
// none (§12).
var itemTags = [...]string{itemWindow: "窗口", itemPane: "Pane", itemTab: "Tab", itemTable: "表", itemCommand: "命令", itemSQL: ""}

// scopes are the palette's tabs (K-02). The prefix typed in the input is the
// only scope state: Tab rewrites it, and `:` is `>` typed (§12).
var scopes = []struct {
	label, prefix string
	kinds         []itemKind
}{
	{"所有", "", []itemKind{itemWindow, itemPane, itemTab, itemTable, itemCommand}}, // not the SQL history (§12)
	{"窗口·Pane", "%", []itemKind{itemWindow, itemPane, itemTab}},
	{"表", "@", []itemKind{itemTable}},
	{"命令", ">", []itemKind{itemCommand}},
	{"SQL", ";", []itemKind{itemSQL}}, // what follows the ; is SQL, the history while there is none
}

// sqlScope is where the input is quick SQL (§12).
const sqlScope = 4

// paletteItem is one candidate. id tells it apart within its kind: the
// action, the table (schema.name), the pane's ID, the window's number, or
// a tab's window number, pane ID and place in the pane ("0 1 2").
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
	// never kept for SQL (the history is its own) nor for a tab (paletteRun),
	// but paletteItems sorts them too
	kind := [...]string{itemWindow: "window", itemPane: "pane", itemTab: "tab", itemTable: "table", itemCommand: "command", itemSQL: "sql"}[it.kind]
	return config.Recent{Kind: kind, ID: it.id}
}

// recentRows is how many palette picks state.json keeps.
const recentRows = 50

// exAliases rank their command first when typed exactly in the command
// scope, so :q↵ and :qa↵ work as they always have (§12); a console's
// editor runs its : commands by them (§11).
var exAliases = map[string]string{"q": "tab.close", "qa": "quit", "w": "save", "wq": "tab.save.close"}

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
// order: windows as the tree's workspace has them, this one's panes by ⟨n⟩,
// each pane's tabs after it (F4.3), then tables, commands by action id
// (§12). A pane and a tab are placed by the tree's names for them:
// doraemon › 0: data › pane-1.
func (a *App) paletteItems() []paletteItem {
	var items []paletteItem
	for wi, w := range a.sess.Windows {
		name := fmt.Sprintf("%d: %s", wi, w.Name)
		items = append(items, paletteItem{itemWindow, strconv.Itoa(wi), a.icons.Window, name, a.sess.Name})
		place := a.sess.Name + " › " + name // where its panes are
		if wi == a.sess.Active {
			items = append(items, a.paneItem(0, w.Tree, place))
		}
		for n, p := range w.Root.Leaves() { // pane-<n> by ⟨n⟩, the sidebar being 0
			if wi == a.sess.Active {
				items = append(items, a.paneItem(n+1, p, place))
			}
			for i := range p.Tabs {
				icon, _ := a.tabIcon(&p.Tabs[i])
				icon.Text = cmp.Or(icon.Text, " ") // a landing tab's: the names in line
				id := fmt.Sprintf("%d %d %d", wi, p.ID, i)
				items = append(items, paletteItem{itemTab, id, icon, p.Tabs[i].Name, fmt.Sprintf("%s › pane-%d", place, n+1)})
			}
		}
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
		if act.Title != "" && !act.Local {
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

// paneItem is pane ⟨n⟩ p of this window, placed at place: by the current
// tab's type, words and all, which are what is searched (§7.7).
func (a *App) paneItem(n int, p *Pane, place string) paletteItem {
	icon, word := a.tabIcon(p.tab())
	switch p {
	case a.win().Tree:
		icon, word = a.icons.Schema, "schema"
	case a.win().Result:
		icon, word = a.icons.Result, "result"
	}
	name := strings.TrimSpace(a.icons.Number(n) + " " + word)
	if obj := p.Object(); obj != "" && word != "" {
		name += " · " + obj
	} else if obj != "" {
		name += " " + obj
	}
	return paletteItem{itemPane, strconv.Itoa(p.ID), icon, name, place}
}

// paletteMatches ranks the candidates in scope for what is typed: fzf over
// "name where", every kind mixed by score (§12).
func (a *App) paletteMatches() (items []paletteItem, ms []ui.Match) {
	scope, query := a.paletteScope()
	if scope != sqlScope || strings.TrimSpace(query) == "" { // SQL typed: nothing to list
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

// paletteView is what the palette draws.
func (a *App) paletteView() ui.Palette {
	items, ms := a.paletteMatches()
	scope, query := a.paletteScope()
	p := ui.Palette{Search: a.icons.Search, Input: a.palette.input, Scope: scope, Sel: a.palette.sel, Top: a.palette.top}
	for _, s := range scopes {
		p.Scopes = append(p.Scopes, strings.TrimSpace(s.label+" "+s.prefix)) // "表 @": the tab says what to type
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
	p.Footer = bound(
		ui.Hint{Key: a.hints("palette", "/", "palette.up", "palette.down"), Label: "移动"},
		ui.Hint{Key: scopeKeys, Label: "范围"},
		ui.Hint{Key: a.keys.Hint("palette.close", "palette"), Label: "关闭", Action: "palette.close"},
	)
	switch run := a.keys.Hint("palette.run", "palette"); {
	case scope == sqlScope && strings.TrimSpace(query) != "":
		label := "执行"
		if q := a.palette.quick; q != nil && run != "" && query != q.last() { // F-03
			run, label = "已修改，"+run, "重新执行"
		}
		p.Enter = bound(ui.Hint{Key: run, Label: label, Action: "palette.run"})
	case a.palette.sel < len(ms):
		it := items[ms[a.palette.sel].Index]
		enter := [...]string{itemWindow: "切换", itemPane: "聚焦", itemTab: "切换", itemTable: "打开", itemCommand: "执行", itemSQL: "执行"}[it.kind]
		if it.kind == itemCommand && actions[it.id].On != nil {
			enter = "切换"
		}
		p.Enter = bound(ui.Hint{Key: run, Label: enter, Action: "palette.run"}) // C-t is ↵ for a table: no hint of its own (F3.37)
	}
	if a.quickShows() {
		p.Result = a.quickView(a.palette.quick)
	}
	p.Preview = a.palettePreview()
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
	lines := 0
	if pv := a.palettePreview(); pv != nil {
		lines = pv.Lines()
	}
	return ui.PaletteBox(a.window(), n, a.quickShows(), lines)
}

// previewDelay is how long the palette's selection rests on a table before
// its DDL is asked for (§12「预览」).
var previewDelay = 150 * time.Millisecond

type (
	// previewDue is previewDelay after the selection went to a table.
	previewDue struct {
		p   *palette
		seq int
	}
	// ddlMsg brings a table's DDL, or why there is none; a table's columns
	// come with it, cols.Cols nil for a view's.
	ddlMsg struct {
		table db.Table
		text  string
		cols  db.Columns
		err   error
	}
	// ddlText is a table's DDL as the cache keeps it; got is false while it
	// is on its way.
	ddlText struct {
		text, err string
		got       bool
	}
)

// previewed is the table the palette's selection is on, if any: in 所有
// and 表, the scopes listing tables (F4.2).
func (a *App) previewed() (db.Table, bool) {
	if a.palette == nil {
		return db.Table{}, false
	}
	items, ms := a.paletteMatches()
	if a.palette.sel >= len(ms) || items[ms[a.palette.sel].Index].kind != itemTable {
		return db.Table{}, false
	}
	return a.tableByID(items[ms[a.palette.sel].Index].id)
}

// tableByID is the session's table of a palette item's id, schema.name.
func (a *App) tableByID(id string) (db.Table, bool) {
	if i := slices.IndexFunc(a.sess.Tables, func(t db.Table) bool { return t.Schema+"."+t.Name == id }); i >= 0 {
		return a.sess.Tables[i], true
	}
	return db.Table{}, false
}

// previewedView is the DDL of the view the palette previews, "" for none,
// and its schema: its names are colored by the tables its query names
// (§7.3), whose columns wantCols fetches.
func (a *App) previewedView() (text, schema string) {
	if t, ok := a.previewed(); ok && t.View() {
		if d := a.sess.ddl[idOf(t)]; d.got && d.err == "" {
			return d.text, t.Schema
		}
	}
	return "", ""
}

// palettePreview is the preview of the table selected, once its DDL is in:
// in SQL's colors, a table's own columns among them, fetched with it; a
// view's by the tables its query names, as a console's (F3.27).
func (a *App) palettePreview() *ui.PalettePreview {
	t, ok := a.previewed()
	d := a.sess.ddl[idOf(t)]
	if !ok || !d.got {
		return nil
	}
	var names ui.SQLNames
	if cols := a.sess.cols[idOf(t)]; t.View() {
		names, _ = a.sqlNames(d.text, t.Schema, nil)
	} else {
		names, _ = a.sqlNames(d.text, "", &cols)
	}
	return &ui.PalettePreview{Text: d.text, Err: d.err, Names: names}
}

// wantDDL times the preview: the selection moved to a table whose DDL is
// not cached, previewDelay from now it is asked for if still there; a
// move meanwhile starts over (F4.2). Checked after every message, as the
// selection moves with keys, the mouse and the input's filter.
func (a *App) wantDDL() tea.Cmd {
	if w := a.fetching; w != nil && w != a.palette { // it closed: a DDL on its way would hold Meta up, the next request and C-c behind it
		w.stop()
		a.fetching = nil
	}
	p := a.palette
	if p == nil {
		return nil
	}
	t, ok := a.previewed()
	if idOf(t) == p.preview {
		return nil
	}
	p.preview = idOf(t)
	if _, cached := a.sess.ddl[idOf(t)]; !ok || cached {
		return nil
	}
	p.previewSeq++
	due := previewDue{p, p.previewSeq}
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return due })
}

// fetchDDL asks Meta for the DDL of the table the selection rested on, a
// table's columns with it, bounded by countTimeout: a table another
// session locks holds pg_get_expr up (§8.3, PG 16 on).
func (a *App) fetchDDL(m previewDue) tea.Cmd {
	t, ok := a.previewed()
	if m.p != a.palette || m.seq != m.p.previewSeq || !ok {
		return nil
	}
	if _, cached := a.sess.ddl[idOf(t)]; cached {
		return nil
	}
	a.sess.ddl[idOf(t)] = ddlText{}
	if a.fetching != m.p {
		m.p.fetches, m.p.stop = context.WithCancel(context.Background())
		a.fetching = m.p
	}
	meta, ctx := a.sess.Meta, m.p.fetches
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, countTimeout)
		defer cancel()
		m := ddlMsg{table: t}
		m.err = meta.Run(ctx, func(ctx context.Context, c db.Conn) (err error) {
			if m.text, err = postgres.DDL(ctx, c, t.Schema, t.Name); err != nil || t.View() {
				return err
			}
			m.cols, err = postgres.TableColumns(ctx, c, t.Schema, t.Name)
			return err
		})
		return m
	}
}

// gotDDL caches a DDL, shown if the selection is still on its table, and
// a table's columns. One out of time, or cancelled, is not kept: the next
// rest asks again; one the cache dropped meanwhile (R) neither.
func (a *App) gotDDL(m ddlMsg) {
	id := idOf(m.table)
	if _, asked := a.sess.ddl[id]; !asked {
		return
	}
	if errors.Is(m.err, context.DeadlineExceeded) || errors.Is(m.err, context.Canceled) {
		delete(a.sess.ddl, id)
		return
	}
	d := ddlText{text: m.text, got: true}
	if m.err != nil {
		d.err = m.err.Error()
	}
	a.sess.ddl[id] = d
	if m.cols.Cols != nil {
		a.sess.cols[id] = m.cols
	}
}

// quickShows is whether the palette shows a quick SQL's result area.
func (a *App) quickShows() bool {
	s, _ := a.paletteScope()
	return s == sqlScope && a.palette.quick != nil
}

// paletteScopeTo switches to scope i, wrapping around, by rewriting the
// input's prefix; what was typed after it stays.
func (a *App) paletteScopeTo(i int) {
	scope, query := a.paletteScope()
	i = (i + len(scopes)) % len(scopes)
	pos := max(a.palette.input.Pos-len(scopes[scope].prefix), 0) + len(scopes[i].prefix)
	a.palette.input = ui.Input{Text: scopes[i].prefix + query, Pos: pos}
	a.palette.sel, a.palette.top, a.palette.comp = 0, 0, nil
}

// paletteRun runs candidate i (K-04); newTab is C-t, which only tables
// take, opening them as ↵ does (F3.37).
// A toggle leaves the palette open, so its ON / OFF can be seen to change
// (§12); a window only closes it until windows can switch (M5).
func (a *App) paletteRun(i int, newTab bool) tea.Cmd {
	items, ms := a.paletteMatches()
	if scope, sql := a.paletteScope(); scope == sqlScope { // run what is typed, or the history's pick
		if newTab { // C-t: the rows to the result area
			return a.quickToResult()
		}
		if strings.TrimSpace(sql) == "" && i < len(ms) {
			sql = items[ms[i].Index].id
			a.palette.input = ui.Input{Text: scopes[scope].prefix + sql, Pos: len(scopes[scope].prefix + sql)}
		}
		if strings.TrimSpace(sql) == "" {
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
	if it.kind == itemTab { // ponytail: not kept among the recent: a pane's ID and a tab's place change from run to run
		return a.paletteDo(it)
	}
	r := it.recent()
	a.state.Recent = slices.Insert(slices.DeleteFunc(a.state.Recent, func(k config.Recent) bool { return k == r }), 0, r)
	a.state.Recent = a.state.Recent[:min(len(a.state.Recent), recentRows)]
	return tea.Batch(a.saveState(), a.paletteDo(it))
}

// paletteDo does what candidate it stands for.
func (a *App) paletteDo(it paletteItem) tea.Cmd {
	if it.kind == itemCommand && actions[it.id].On != nil {
		cmd := a.run(it.id, 0)
		items, ms := a.paletteMatches() // it may have moved up among the recent ones
		a.palette.sel = slices.IndexFunc(ms, func(m ui.Match) bool { return items[m.Index].key() == it.key() })
		a.paletteMove(0) // and the list scrolls to it
		return cmd
	}
	into := a.palette.into
	a.palette = nil
	switch it.kind {
	case itemCommand:
		return a.run(it.id, 0)
	case itemTable:
		if t, ok := a.tableByID(it.id); ok {
			if into != nil {
				return a.openTableIn(into, t)
			}
			return a.openTable(t)
		}
	case itemPane:
		id, _ := strconv.Atoi(it.id)
		a.showPane(id)
	case itemTab: // this window's; another's waits for M5, as its window's switch
		var wi, id, i int
		fmt.Sscan(it.id, &wi, &id, &i)
		if p := a.win().pane(id); wi == a.sess.Active && p != nil {
			a.showTab(p, i)
		}
	}
	return nil
}

// paletteEnter is ↵ in the palette: with quick SQL's candidates up it
// takes the selected one, and runs only when that changes nothing (§9.7).
func (a *App) paletteEnter() tea.Cmd {
	if a.palette.comp != nil {
		if changed, _ := a.acceptCompletion(); changed {
			return nil
		}
	}
	return a.paletteRun(a.palette.sel, false)
}

// paletteKey edits the palette's input, quick SQL's candidates following
// it (§9.7).
func (a *App) paletteKey(k keymap.Key) tea.Cmd {
	p := a.palette
	edit := editInput
	if scope, _ := a.paletteScope(); scope == sqlScope && p.input.Pos > 0 { // the SQL after the ; pairs (§7.9)
		edit = a.editPaired
	}
	if edit(&p.input, k) {
		p.sel, p.top = 0, 0
	}
	return a.completeSQL()
}
