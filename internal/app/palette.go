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
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// palette is the open command palette (§12).
type palette struct {
	input    ui.Input
	sel, top int   // selected candidate, first one shown
	into     *Pane // a landing tab's 打开表: the table picked opens there (§5)
	// conn is the current connection as it opened (§5): its quick SQL, C-c,
	// C-e and the tables' order go by it, whatever connects meanwhile.
	conn *Conn

	// The table item the selection was on when last looked at, the
	// preview's timer for it, and what stops the DDL fetches out once the
	// palette closes (F4.2).
	preview    string
	previewSeq int
	// pvTop is how far the preview is scrolled (F9.7); a selection that moves
	// to another table, or more typed, has it at the top again.
	pvTop   int
	pvEd    *editor.Editor // the preview has the focus: its text in a read only vim (F17.6)
	fetches context.Context
	stop    context.CancelFunc

	// Quick SQL's (§12).
	comp  *completion
	quick *quickSQL
	hint  *queryTab        // what a Redis command's key and field names are asked for by (F17.2)
	asked map[tableID]bool // tables whose columns were fetched for completion
	keys  *keyList         // the Redis keys it lists, with what they were last matched for (F20.3)
	// what the background was last asked to make of them, and the number of the ask
	// the answer of which counts (F20.8)
	keyAsked, keyAskedSig string
	keySeq                int
}

// itemKind is what a palette row stands for (K-03), in the order an empty
// input lists them.
type itemKind int

const (
	itemSession itemKind = iota
	itemWindow
	itemPane
	itemTab // an open tab, any window's (F4.3)
	itemTable
	itemQuery // a query of the Queries node (F12.10)
	itemCommand
	itemSQL      // a quick SQL from the history
	itemTip      // a line of the status bar's tips, which runs the action it is about (F15.4)
	itemRedisKey // a Redis key of a db the index of key names or the tree has read (F20.3)
)

// itemTags name a row's kind. The SQL history, a list of one kind, has
// none (§12).
var itemTags = [...]string{itemSession: i18n.N("会话"), itemWindow: i18n.N("窗口"), itemPane: "Pane", itemTab: "Tab", itemTable: i18n.N("表"), itemQuery: "Query", itemCommand: i18n.N("命令"), itemSQL: "", itemTip: i18n.N("提示"), itemRedisKey: "Key"}

// scopes are the palette's tabs (K-02). The prefix typed in the input is the
// only scope state: Tab rewrites it, and palette.command is `>` typed (§12).
type scopeTab struct {
	label, prefix string
	kinds         []itemKind
}

// tipPrefix is the scope of the tips: typed by tips.show, not a tab of the
// palette, which Tab does not stop at either (F15.4).
const tipPrefix = "?"

var scopes = []scopeTab{
	{i18n.N("所有"), "", []itemKind{itemSession, itemWindow, itemPane, itemTab, itemTable, itemRedisKey, itemQuery, itemCommand}}, // not the SQL history (§12)
	{i18n.N("会话"), "$", []itemKind{itemSession}},
	{i18n.N("窗口·Pane"), "%", []itemKind{itemWindow, itemPane, itemTab}},
	{i18n.N("窗口"), "&", []itemKind{itemWindow}},
	{"Tab", "#", []itemKind{itemTab}},
	{i18n.N("表"), "@", []itemKind{itemTable, itemRedisKey, itemQuery}},
	{i18n.N("命令"), ">", []itemKind{itemCommand}},
	{"SQL", ";", []itemKind{itemSQL}},              // what follows the ; is SQL, the history while there is none
	{i18n.N("提示"), tipPrefix, []itemKind{itemTip}}, // the tips of the status bar, ↵ does what one is about (F15.4); the last: not a tab
}

// sqlScope is where the input is quick SQL (§12): the ; one.
var sqlScope = slices.IndexFunc(scopes, func(s scopeTab) bool { return s.prefix == ";" })

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
	// never kept for SQL (the history is its own) nor for a tab or a
	// session (paletteRun), but paletteItems sorts them too
	kind := [...]string{itemSession: "session", itemWindow: "window", itemPane: "pane", itemTab: "tab", itemTable: "table", itemQuery: "query", itemCommand: "command", itemSQL: "sql", itemTip: "tip", itemRedisKey: "key"}[it.kind]
	return config.Recent{Kind: kind, ID: it.id}
}

// recentRows is how many palette picks state.json keeps.
const recentRows = 50

// exAliases rank their command first when typed exactly in the all or
// the command scope, so :q↵ and :qa↵ work as they always have, : opening
// either (§12); a query's editor runs its : commands by them (§11).
var exAliases = map[string]string{"q": "tab.close", "qa": "quit", "w": "save", "wq": "tab.save.close"}

func (a *App) openPalette(text string) {
	a.palette = &palette{input: ui.Input{Text: text, Pos: len(text)}, conn: a.conn()}
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
// order: the sessions, windows as the tree's workspace has them, this one's
// panes by ⟨n⟩,
// each pane's tabs after it (F4.3), then tables, queries, commands by action id
// (§12). A pane and a tab are placed by the tree's names for them:
// doraemon › 0: data › pane-1.
func (a *App) paletteItems() []paletteItem {
	var items []paletteItem
	for i, s := range a.sessions {
		items = append(items, paletteItem{itemSession, strconv.Itoa(i), a.icons.Conn, s.Name, windowCount(len(s.Windows))})
	}
	for wi, w := range a.sess.Windows {
		name := fmt.Sprintf("%d: %s", wi, w.Name)
		// by ID: the recent ones outlive a close (F5.2)
		items = append(items, paletteItem{itemWindow, strconv.Itoa(w.ID), a.icons.Window, name, a.sess.Name})
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
	// the current connection's Schema first, as the tree lists it, its other
	// schemas, then the other connections connected (§12)
	cur := a.palette.conn
	for _, c := range append([]*Conn{cur}, slices.DeleteFunc(a.sess.conns(), func(c *Conn) bool { return c == cur })...) { // every (connection, database) connected (F15.1)
		if c == nil { // a start with no connection (F7.4)
			continue
		}
		for _, here := range []bool{true, false} {
			for _, t := range c.Tables {
				if (c == cur && t.Schema == c.Schema) == here {
					items = append(items, paletteItem{itemTable, c.tableKey(t), a.icons.Table, t.Name, c.label() + "." + t.Schema})
				}
			}
		}
	}
	for _, name := range a.queryList() { // after the tables, as the tree lists them (F12.10)
		items = append(items, paletteItem{itemQuery, name, a.icons.Query, name, a.state.Queries[name].Conn})
	}
	var ids []string
	for id, act := range actions {
		if act.Title != "" && !act.Local && !(act.SQL && cur != nil && !cur.sql()) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		items = append(items, paletteItem{itemCommand, id, a.icons.Command, i18n.T(actions[id].Title), id})
	}
	for _, t := range tips { // the ones with a key now
		if line := a.tipLine(t); line != "" {
			items = append(items, paletteItem{itemTip, t.action, a.icons.Command, line, i18n.T(actions[t.action].Title)})
		}
	}
	if cur != nil {
		for _, sql := range a.state.SQL[cur.Spec.Name] {
			items = append(items, paletteItem{itemSQL, sql, a.icons.Query, sql, ""})
		}
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
// "name where", every kind mixed by score, ties as the empty input has
// them (§12).
func (a *App) paletteMatches() (items []paletteItem, ms []ui.Match) {
	scope, query := a.paletteScope()
	if scope != sqlScope || strings.TrimSpace(query) == "" { // SQL typed: nothing to list
		for _, it := range a.paletteItems() {
			if slices.Contains(scopes[scope].kinds, it.kind) {
				items = append(items, it)
			}
		}
	}
	switch scopes[scope].prefix {
	case "&": // by number, whatever was picked lately
		slices.SortStableFunc(items, func(x, y paletteItem) int {
			wx, _ := strconv.Atoi(x.id)
			wy, _ := strconv.Atoi(y.id)
			return a.sess.windowByID(wx) - a.sess.windowByID(wy)
		})
	case "#":
		items = a.tabsByUse(items)
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.name + " " + it.where
	}
	ms = ui.Filter(query, texts)
	if slices.Contains(scopes[scope].kinds, itemRedisKey) && strings.TrimSpace(query) != "" { // the keys come matched and cut, after what is not a key (F20.3)
		kitems, kms := a.keyMatches(query)
		for _, m := range kms {
			m.Index += len(items)
			ms = append(ms, m)
		}
		items = append(items, kitems...)
	}
	slices.SortStableFunc(ms, func(x, y ui.Match) int { // not the shorter first: a table of the current schema before its namesakes
		return cmp.Or(y.Score-x.Score, x.Index-y.Index)
	})
	alias := itemKey{itemCommand, exAliases[strings.TrimSpace(query)]}
	if i := slices.IndexFunc(items, func(it paletteItem) bool { return it.key() == alias }); i >= 0 && (scope == 0 || scopes[scope].prefix == ">") {
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
		if s.prefix != tipPrefix {
			p.Scopes = append(p.Scopes, strings.TrimSpace(i18n.T(s.label)+" "+s.prefix)) // "表 @": the tab says what to type
		}
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
		p.Rows = append(p.Rows, ui.PaletteRow{Icon: it.icon, Name: it.name, Where: it.where, Pos: m.Pos, Right: right, Tag: i18n.T(itemTags[it.kind])})
	}
	scopeKeys := a.hints("palette", "/", "palette.scope.next", "palette.scope.prev")
	p.Preview = a.palettePreview()
	scrollKeys := ""
	if pv, _ := a.scrollablePreview(); pv != nil { // the hint where there is something to scroll
		scrollKeys = a.hints("palette", "/", "palette.preview.up", "palette.preview.down")
	}
	p.Footer = bound(
		ui.Hint{Key: a.hints("palette", "/", "palette.up", "palette.down"), Label: i18n.T("移动")},
		ui.Hint{Key: scrollKeys, Label: i18n.T("滚动预览")},
		ui.Hint{Key: scopeKeys, Label: i18n.T("范围")},
		ui.Hint{Key: a.keys.Hint("palette.close", "palette"), Label: i18n.T("关闭"), Action: "palette.close"},
	)
	switch run := a.keys.Hint("palette.run", "palette"); {
	case scope == sqlScope && strings.TrimSpace(query) != "":
		label := i18n.T("执行")
		if q := a.palette.quick; q != nil && run != "" && query != q.last() { // F-03
			run, label = i18n.T("已修改，%s", run), i18n.T("重新执行")
		}
		p.Enter = bound(ui.Hint{Key: run, Label: label, Action: "palette.run"})
	case a.palette.sel < len(ms):
		it := items[ms[a.palette.sel].Index]
		enter := [...]string{itemSession: i18n.T("切换"), itemWindow: i18n.T("切换"), itemPane: i18n.T("聚焦"), itemTab: i18n.T("切换"), itemTable: i18n.T("打开"), itemQuery: i18n.T("打开"), itemCommand: i18n.T("执行"), itemSQL: i18n.T("执行"), itemTip: i18n.T("执行"), itemRedisKey: i18n.T("打开")}[it.kind]
		if it.kind == itemCommand && actions[it.id].On != nil {
			enter = i18n.T("切换")
		}
		p.Enter = bound(ui.Hint{Key: run, Label: enter, Action: "palette.run"}) // C-t is ↵ for a table: no hint of its own (F3.37)
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
	_, rows, _ := a.paletteBox(len(ms))
	p.sel, p.top = ui.ListView(p.sel+d, p.top, len(ms), rows)
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
		conn  *Conn
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

// previewed is the table the palette's selection is on, if any, with its
// item's id and its connection: in 所有 and 表, the scopes listing tables
// (F4.2).
func (a *App) previewed() (id string, c *Conn, t db.Table, ok bool) {
	if a.palette == nil {
		return "", nil, db.Table{}, false
	}
	items, ms := a.paletteMatches()
	if a.palette.sel >= len(ms) || items[ms[a.palette.sel].Index].kind != itemTable {
		return "", nil, db.Table{}, false
	}
	id = items[ms[a.palette.sel].Index].id
	c, t, ok = a.tableByID(id)
	return id, c, t, ok
}

// tableByID is the table of a palette item's id (Conn.tableKey), and its
// connection: the database's, not the DSN's by the name.
func (a *App) tableByID(id string) (*Conn, db.Table, bool) {
	for _, c := range a.sess.conns() {
		table, ok := strings.CutPrefix(id, c.keyPrefix())
		if !ok {
			continue
		}
		if i := slices.IndexFunc(c.Tables, func(t db.Table) bool { return t.Schema+"."+t.Name == table }); i >= 0 {
			return c, c.Tables[i], true
		}
	}
	return nil, db.Table{}, false
}

// previewedView is the DDL of the view the palette previews, "" for none,
// with its connection and schema: its names are colored by the tables its
// query names (§7.3), whose columns wantCols fetches.
func (a *App) previewedView() (c *Conn, text, schema string) {
	if _, c, t, ok := a.previewed(); ok && t.View() {
		if d := c.ddl[idOf(t)]; d.got && d.err == "" {
			return c, d.text, t.Schema
		}
	}
	return nil, "", ""
}

// palettePreview is the preview of the table selected, once its DDL is in:
// in SQL's colors, a table's own columns among them, fetched with it; a
// view's by the tables its query names, as a query's (F3.27).
func (a *App) palettePreview() *ui.PalettePreview {
	_, c, t, ok := a.previewed()
	if !ok {
		return a.queryPreview()
	}
	if !c.ddl[idOf(t)].got {
		return nil
	}
	d := c.ddl[idOf(t)]
	return a.withEditor(&ui.PalettePreview{Text: d.text, Err: d.err, Names: a.ddlNames(c, t, d.text), Dialect: c.eng.Dialect(), Top: a.palette.pvTop})
}

// withEditor is pv with the editor that has its text, when the preview has the
// focus (F17.6).
func (a *App) withEditor(pv *ui.PalettePreview) *ui.PalettePreview {
	if ed := a.palette.pvEd; ed != nil && pv.Err == "" {
		q := a.textView(ed, ui.ValuePane, false)
		q.Colors = ui.SQLColors(a.theme, pv.Text, pv.Dialect, pv.Names)
		pv.Edit = &q
	}
	return pv
}

// queryPreviewRows and queryPreviewBytes are how much of a query file the
// preview shows: its first rows, and no more than a few KB of them, one line
// of a file being as long as it likes.
const (
	queryPreviewRows  = 100
	queryPreviewBytes = 8 << 10
)

// selectedQuery is the name of the query the palette's selection is on, "" when
// it is on none.
func (a *App) selectedQuery() string {
	items, ms := a.paletteMatches()
	if a.palette.sel >= len(ms) || items[ms[a.palette.sel].Index].kind != itemQuery {
		return ""
	}
	return items[ms[a.palette.sel].Index].id
}

// queryPreview is the start of the query the palette's selection is on, as a
// query colors it, by the catalog of the connection it opens on; none when the
// selection is not a query, or it has no file or nothing in it (F12.10).
// ponytail: the file is read whole, three times a frame this is asked for; keep
// the text by the selection if a big file or a slow disk lags.
func (a *App) queryPreview() *ui.PalettePreview {
	name := a.selectedQuery()
	if name == "" {
		return nil
	}
	text, err := config.ReadQuery(config.QueryPath(name))
	if err != nil || strings.TrimSpace(text) == "" {
		return nil
	}
	if lines := strings.SplitAfterN(text, "\n", queryPreviewRows+1); len(lines) > queryPreviewRows {
		text = strings.Join(lines[:queryPreviewRows], "")
	}
	if len(text) > queryPreviewBytes {
		n := queryPreviewBytes
		for n > 0 && !utf8.RuneStart(text[n]) { // not through a character
			n--
		}
		text = text[:n]
	}
	pv := &ui.PalettePreview{Text: strings.TrimRight(text, "\n"), Top: a.palette.pvTop}
	if c, schema := a.queryPlace(name, a.palette.conn); c != nil {
		pv.Dialect = c.dialect()
		pv.Names, _ = a.sqlNames(c, text, schema, nil)
	}
	return a.withEditor(pv)
}

// ddlNames are the names in t's DDL text the catalog knows, for their colors:
// a table's own columns among them, a view's by the tables its query names.
func (a *App) ddlNames(c *Conn, t db.Table, text string) ui.SQLNames {
	if t.View() {
		names, _ := a.sqlNames(c, text, t.Schema, nil)
		return names
	}
	cols := c.cols[idOf(t)]
	names, _ := a.sqlNames(c, text, "", &cols)
	return names
}

// previewWindow is the preview of the table selected and how many rows the
// palette shows of it: none when it has no room (§12「预览」).
func (a *App) previewWindow() (*ui.PalettePreview, int) {
	_, ms := a.paletteMatches()
	_, _, grid := a.paletteBox(len(ms))
	return a.palettePreview(), grid.Dy()
}

// scrollablePreview is the preview and its rows when the palette shows it and
// it has more lines than that; none otherwise (F9.7).
func (a *App) scrollablePreview() (*ui.PalettePreview, int) {
	pv, rows := a.previewWindow()
	if pv == nil || rows <= 0 || pv.Lines() <= rows {
		return nil, 0
	}
	return pv, rows
}

// scrollPreview is C-f (d 1) and C-b (-1) over the preview: half its rows,
// from where it is held now, to the ends and no further (F9.7).
func (a *App) scrollPreview(d int) {
	if _, rows := a.scrollablePreview(); rows > 0 {
		a.scrollPreviewBy(d * max(rows/2, 1))
	}
}

// scrollPreviewBy scrolls the preview by n rows, to the ends and no further:
// the wheel's (F17.6).
func (a *App) scrollPreviewBy(n int) {
	if pv, rows := a.scrollablePreview(); pv != nil {
		a.palette.pvTop = ui.TextView{Text: pv.Text, Top: pv.Top}.Scroll(rows, n)
	}
}

// previewEd is the editor that has the preview's text, when the preview has the
// focus and is shown: a selection that moved off what it showed, a window with no
// room for it, take the focus back to the list (F17.6).
func (a *App) previewEd() *editor.Editor {
	p := a.palette
	if p == nil || p.pvEd == nil {
		return nil
	}
	if pv, rows := a.previewWindow(); pv == nil || pv.Err != "" || rows <= 0 {
		p.pvEd = nil
	}
	return p.pvEd
}

// previewRect is where the preview draws its text (ui.Palette.Draw).
func (a *App) previewRect() uv.Rectangle {
	_, ms := a.paletteMatches()
	box, _, grid := a.paletteBox(len(ms))
	return uv.Rect(box.Min.X+2, grid.Min.Y, box.Dx()-4, grid.Dy())
}

// previewSize gives the editor the size of the preview's area, which H M L, C-d and
// the scrolling go by.
func (a *App) previewSize(ed *editor.Editor) {
	a.sizeEditor(ed, ui.Query{Lines: ed.Lines()}.TextArea(a.previewRect()))
}

// previewFocus is palette.preview.focus: the preview's text in a read only editor,
// which has the keys, as a JSON view's has (F17.6); none where the preview is
// not shown or is an error.
func (a *App) previewFocus() {
	p := a.palette
	if pv, rows := a.previewWindow(); p.pvEd == nil && pv != nil && pv.Err == "" && rows > 0 {
		ed := editor.New("")
		ed.ReadOnly = true
		ed.Load(pv.Text)
		ed.SetPlain(a.plain)
		p.pvEd = ed
		a.previewSize(ed)
	}
}

// previewKey gives key k, which no binding of the palette's preview took, to
// its editor: esc in NORMAL, with no search lit, takes the focus back to the list,
// and so does :q (F17.6).
func (a *App) previewKey(k keymap.Key) tea.Cmd {
	p, ed := a.palette, a.previewEd()
	if ed == nil {
		return nil
	}
	a.previewSize(ed)
	var did func(editor.Effect) tea.Cmd
	did = func(eff editor.Effect) tea.Cmd {
		return a.roDid(eff, func(text string, pt editor.ClipPut) tea.Cmd {
			if p.pvEd != ed {
				return nil
			}
			return did(ed.PutClip(text, pt))
		}, func(c string) tea.Cmd {
			if c = strings.TrimSpace(c); c == "q" || c == "q!" {
				return a.previewLeave()
			}
			return a.showToast(i18n.T("不支持的命令：%s", c))
		})
	}
	return a.roKey(ed, k, false, a.previewLeave, did)
}

// previewLeave takes the focus back to the list.
func (a *App) previewLeave() tea.Cmd {
	a.palette.pvEd = nil
	return nil
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
	id, c, t, ok := a.previewed()
	if !ok { // a query's preview is held by its name, apart from a table's id
		id = a.selectedQuery() // a table's id has a /, a query's name has none
	}
	if id == p.preview {
		return nil
	}
	p.preview, p.pvTop, p.pvEd = id, 0, nil // another table or query, or none: the top, and the list has the focus again
	if !ok {
		return nil
	}
	if _, cached := c.ddl[idOf(t)]; cached {
		return nil
	}
	p.previewSeq++
	due := previewDue{p, p.previewSeq}
	return tea.Tick(previewDelay, func(time.Time) tea.Msg { return due })
}

// fetchDDL asks its connection's Meta for the DDL of the table the
// selection rested on, a table's columns with it, bounded by countTimeout:
// a table another session locks holds pg_get_expr up (§8.3, PG 16 on).
func (a *App) fetchDDL(m previewDue) tea.Cmd {
	_, conn, t, ok := a.previewed()
	if m.p != a.palette || m.seq != m.p.previewSeq || !ok {
		return nil
	}
	if _, cached := conn.ddl[idOf(t)]; cached {
		return nil
	}
	conn.ddl[idOf(t)] = ddlText{}
	if a.fetching != m.p {
		m.p.fetches, m.p.stop = context.WithCancel(context.Background())
		a.fetching = m.p
	}
	return a.ddlCmd(m.p.fetches, conn, t)
}

// ddlCmd reads t's DDL, and a table's columns, on conn's Meta, in ctx and for
// countTimeout at most.
func (a *App) ddlCmd(ctx context.Context, conn *Conn, t db.Table) tea.Cmd {
	meta, eng := conn.Meta, conn.eng
	key := redis.Key{Meta: meta, Name: t.Name, LoadAll: a.loadAll}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, countTimeout)
		defer cancel()
		m := ddlMsg{conn: conn, table: t}
		if eng == nil { // a Redis key: the commands that make it again
			m.text, m.err = key.Rebuild(ctx)
			return m
		}
		m.err = meta.RunSafe(ctx, func(ctx context.Context, c db.Conn) (err error) {
			if m.text, err = eng.DDL(ctx, c, t.Schema, t.Name); err != nil || t.View() {
				return err
			}
			m.cols, err = eng.TableColumns(ctx, c, t.Schema, t.Name)
			return err
		})
		return m
	}
}

// gotDDL caches a DDL, shown if the selection is still on its table, and
// a table's columns. One out of time, or cancelled, is not kept: the next
// rest asks again; one the cache dropped meanwhile (R) neither.
func (a *App) gotDDL(m ddlMsg) {
	c, id := m.conn, idOf(m.table)
	if _, asked := c.ddl[id]; !asked {
		return
	}
	if errors.Is(m.err, context.DeadlineExceeded) || errors.Is(m.err, context.Canceled) {
		delete(c.ddl, id)
		return
	}
	d := ddlText{text: m.text, got: true}
	if m.err != nil {
		d.err = m.err.Error()
	}
	c.ddl[id] = d
	if m.cols.Cols != nil {
		c.cols[id] = m.cols
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
	n := len(scopes) - 1 // the tabs: not the typed ones, which come last
	i = (i + n) % n
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
	if it.kind == itemTab || it.kind == itemSession || it.kind == itemTip || it.kind == itemRedisKey { // a tip is a way to run an action, not a pick to remember; ponytail: not kept among the recent: a pane's ID, a tab's place and a session's name change from run to run, and a key of a hundred thousand is no list the recent ones go first in
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
	case itemCommand, itemTip:
		return a.run(it.id, 0)
	case itemTable:
		if c, t, ok := a.tableByID(it.id); ok {
			if into != nil {
				return a.openTableIn(into, c, t)
			}
			return a.openTable(c, t)
		}
	case itemRedisKey:
		if c, key, ok := a.keyByID(it.id); ok {
			t := db.Table{Name: key, Kind: "k"} // as the tree's ↵ opens it
			if into != nil {
				return a.openTableIn(into, c, t)
			}
			return a.openTable(c, t)
		}
	case itemQuery:
		return a.openQueryFile(it.id)
	case itemSession:
		i, _ := strconv.Atoi(it.id)
		a.attach(a.sessions[i])
	case itemPane:
		id, _ := strconv.Atoi(it.id)
		a.showPane(id)
	case itemWindow:
		id, _ := strconv.Atoi(it.id)
		a.selectWindow(a.sess.windowByID(id))
	case itemTab: // another window's: that window first (F5.2)
		var wi, id, i int
		_, _ = fmt.Sscan(it.id, &wi, &id, &i)
		if a.selectWindow(wi); a.win().pane(id) != nil {
			a.showTab(a.win().pane(id), i)
		}
	case itemSQL: // paletteRun runs it as quick SQL, before this
	}
	return nil
}

// paletteEnter is ↵ in the palette: with quick SQL's candidates up it
// takes the selected one and does no more (§9.7).
func (a *App) paletteEnter() tea.Cmd {
	if c := a.palette.comp; c != nil && c.sel >= 0 {
		return a.acceptCompletion()
	}
	a.palette.comp = nil // with nothing selected the list is not in the way (F8.17)
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
	if !edit(&p.input, k) { // a key that types nothing, an arrow: the list is not opened anew (§9.7)
		p.comp = nil
		return nil
	}
	p.sel, p.top, p.pvTop = 0, 0, 0
	return a.completeSQL()
}

// tabsByUse is the tab items by when they were focused, newest first, the
// current one last, so opening the Tab scope and ↵ goes back to the one
// before (§12, telescope's buffers with sort_mru and ignore_current_buffer);
// those never focused come after the focused ones, as they are listed.
// Landing tabs are left out.
func (a *App) tabsByUse(items []paletteItem) []paletteItem {
	s := a.sess
	rank := map[string]int{}
	var kept []paletteItem
	for _, it := range items {
		var wi, id, i int
		_, _ = fmt.Sscan(it.id, &wi, &id, &i)
		tab := &a.sess.Windows[wi].pane(id).Tabs[i]
		key := tabKey(tab)
		if key == nil {
			continue
		}
		rank[it.id] = s.tabSeq[key]
		if key == s.focused {
			rank[it.id] = -1
		}
		kept = append(kept, it)
	}
	slices.SortStableFunc(kept, func(x, y paletteItem) int { return rank[y.id] - rank[x.id] })
	return kept
}
