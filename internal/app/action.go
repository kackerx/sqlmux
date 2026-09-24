package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// Args is what an action gets from the key press or command that ran it.
type Args struct {
	Count int    // count prefix; 0 when none was typed
	Arg   string // the "3" of "window.select 3"
}

// Action is the single path every key, click and palette pick goes through
// (tech-design §6.1).
type Action struct {
	Title string // shown by which-key and the palette
	Run   func(*App, Args) tea.Cmd
	On    func(*App) bool // a toggle's state: the palette shows ON / OFF and stays open (§12)
}

// quitWindow is how long the "press C-c again" toast stays; a second press
// quits only while it is up (§6.8).
var quitWindow = 2 * time.Second

// actions is the registry, keyed by action ID. Bound IDs that are not here
// yet belong to later features and do nothing. It is filled in init because
// actions run keys through the registry themselves.
var actions map[string]Action

func init() {
	actions = map[string]Action{
		"palette.open":    {Title: "命令面板", Run: do(func(a *App, _ Args) { a.openPalette("") })},
		"palette.command": {Title: "命令面板：命令", Run: do(func(a *App, _ Args) { a.openPalette(">") })}, // : opens it as if > was typed
		// Keys inside the palette. Like every overlay's own actions they have
		// no title, so the palette does not list them (§12); bound elsewhere
		// in config, they do nothing.
		"palette.up":         {Run: when(inPalette, func(a *App) tea.Cmd { a.paletteMove(-1); return nil })},
		"palette.down":       {Run: when(inPalette, func(a *App) tea.Cmd { a.paletteMove(1); return nil })},
		"palette.run":        {Run: when(inPalette, func(a *App) tea.Cmd { return a.paletteRun(a.palette.sel, false) })},
		"palette.open.tab":   {Run: when(inPalette, func(a *App) tea.Cmd { return a.paletteRun(a.palette.sel, true) })},
		"palette.close":      {Run: when(inPalette, func(a *App) tea.Cmd { a.palette = nil; return nil })},
		"palette.scope.next": {Run: when(inPalette, func(a *App) tea.Cmd { s, _ := a.paletteScope(); a.paletteScopeTo(s + 1); return nil })},
		"palette.scope.prev": {Run: when(inPalette, func(a *App) tea.Cmd { s, _ := a.paletteScope(); a.paletteScopeTo(s - 1); return nil })},
		// "palette.scope <i>" is a click on a scope tab.
		"palette.scope": {Run: func(a *App, args Args) tea.Cmd {
			if i, err := strconv.Atoi(args.Arg); err == nil && a.palette != nil {
				a.paletteScopeTo(i)
			}
			return nil
		}},
		"cancel": {Title: "取消 / 连按两次退出", Run: func(a *App, _ Args) tea.Cmd {
			if a.mode() != keymap.Normal { // in any input C-c is esc, as in vim (§6.8)
				return a.press(keymap.Esc)
			}
			if a.busy > 0 { // a query is running: cancel it (§8.3)
				a.sess.Meta.Cancel()
				return nil
			}
			if a.toast != "" && a.toastSeq == a.quitToast { // the first press's toast is still up
				return tea.Quit
			}
			cmd := a.showToast(fmt.Sprintf("再按一次 %s 退出", a.keys.Hint("cancel", "global")), quitWindow)
			a.quitToast = a.toastSeq
			return cmd
		}},
		"quit":      {Title: "退出", Run: func(*App, Args) tea.Cmd { return tea.Quit }},
		"tab.close": {Title: "关闭 tab", Run: func(a *App, _ Args) tea.Cmd { a.closeTab(); return nil }},

		"pane.split.right": {Title: "左右分割", Run: do(func(a *App, _ Args) { a.splitPane(Horiz) })},
		"pane.split.below": {Title: "上下分割", Run: do(func(a *App, _ Args) { a.splitPane(Vert) })},
		"pane.close":       {Title: "关闭 pane", Run: do(func(a *App, _ Args) { a.closePane() })},
		"pane.zoom": {Title: "缩放 / 还原", Run: do(func(a *App, _ Args) { a.toggleZoom() }),
			On: func(a *App) bool { return a.win().Zoom != 0 }},
		"pane.number": {Title: "按编号跳转", Run: do(func(a *App, _ Args) { a.paneNumbers = true })},
		"tree.toggle": {Title: "折叠 / 展开 schema 树", Run: do(func(a *App, _ Args) { a.toggleTree() }),
			On: func(a *App) bool { return a.win().TreeOpen }},
		"tree.down":     {Title: "下移", Run: do(func(a *App, args Args) { a.treeMove(max(args.Count, 1)) })},
		"tree.up":       {Title: "上移", Run: do(func(a *App, args Args) { a.treeMove(-max(args.Count, 1)) })},
		"tree.top":      {Title: "第一项", Run: do(func(a *App, _ Args) { a.treeMove(-a.win().tree.cursor) })},
		"tree.bottom":   {Title: "最后一项", Run: do(func(a *App, _ Args) { _, ms := a.treeTables(); a.treeMove(len(ms)) })}, // clamped to the last
		"tree.open":     {Title: "打开", Run: func(a *App, _ Args) tea.Cmd { return a.treeOpen(false) }},
		"tree.open.tab": {Title: "在新 tab 打开", Run: func(a *App, _ Args) tea.Cmd { return a.treeOpen(true) }},
		"tree.filter":   {Title: "过滤", Run: do(func(a *App, _ Args) { a.treeFilter() })},
		"tree.schema":   {Title: "切换 schema", Run: do(func(a *App, _ Args) { a.openDrop(dropSchema) })},
		"tree.refresh":  {Title: "刷新表列表", Run: func(a *App, _ Args) tea.Cmd { clear(a.sess.cols); return a.loadCatalog() }},
		// Keys inside the dropdowns and the COLS list (§6.8): untitled, like the palette's.
		"dropdown.up":     {Run: when(inDrop, func(a *App) tea.Cmd { a.dropMove(-1); return nil })},
		"dropdown.down":   {Run: when(inDrop, func(a *App) tea.Cmd { a.dropMove(1); return nil })},
		"dropdown.select": {Run: when(inDrop, func(a *App) tea.Cmd { return a.dropPick(a.drop.sel) })},
		"dropdown.close":  {Run: when(inDrop, func(a *App) tea.Cmd { a.drop = nil; return nil })},
		"complete.up":     {Run: when(inComplete, func(a *App) tea.Cmd { a.typingTab().completeMove(-1); return nil })},
		"complete.down":   {Run: when(inComplete, func(a *App) tea.Cmd { a.typingTab().completeMove(1); return nil })},
		"complete.accept": {Run: when(inComplete, func(a *App) tea.Cmd { a.typingTab().acceptCompletion(); return nil })},
		"where.up":        {Run: when(inHist, func(a *App) tea.Cmd { a.histMove(a.typingTab(), -1); return nil })},
		"where.down":      {Run: when(inHist, func(a *App) tea.Cmd { a.histMove(a.typingTab(), 1); return nil })},
		"where.apply":     {Run: when(inHist, func(a *App) tea.Cmd { t := a.typingTab(); return a.histApply(t, t.hist.sel) })},
		"where.star":      {Run: when(inHist, func(a *App) tea.Cmd { return a.histStar(a.typingTab()) })},
		"where.close":     {Run: when(inHist, func(a *App) tea.Cmd { a.typingTab().hist = nil; return nil })},
		// C-r in the WHERE input, or a click on its ▾ from the grid (Q-02).
		"where.history": {Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil && t.page.Cols != nil && a.drop == nil && a.cols == nil && t.typing != "page" {
				t.typing, t.comp, t.hist = "where", nil, &histMenu{}
			}
		})},
		"cols.up":     {Run: when(inCols, func(a *App) tea.Cmd { a.colsMove(-1); return nil })},
		"cols.down":   {Run: when(inCols, func(a *App) tea.Cmd { a.colsMove(1); return nil })},
		"cols.toggle": {Run: when(inCols, func(a *App) tea.Cmd { a.colsToggle(a.cols.sel); return nil })},
		"cols.all":    {Run: when(inCols, func(a *App) tea.Cmd { a.colsSetAll(true); return nil })},
		"cols.none":   {Run: when(inCols, func(a *App) tea.Cmd { a.colsSetAll(false); return nil })},
		"cols.filter": {Run: when(inCols, func(a *App) tea.Cmd { a.cols.typing = true; return nil })},
		"cols.close":  {Run: when(inCols, func(a *App) tea.Cmd { a.colsEsc(); return nil })},

		// "pane.focus <id>" is what a click runs; untitled, it stays out of the palette.
		"pane.focus": {Run: do(func(a *App, args Args) {
			if id, err := strconv.Atoi(args.Arg); err == nil {
				a.focusPane(id)
			}
		})},
		"pane.focus.left":  {Title: "焦点移到左边", Run: do(func(a *App, _ Args) { a.focusSide("left") })},
		"pane.focus.down":  {Title: "焦点移到下边", Run: do(func(a *App, _ Args) { a.focusSide("down") })},
		"pane.focus.up":    {Title: "焦点移到上边", Run: do(func(a *App, _ Args) { a.focusSide("up") })},
		"pane.focus.right": {Title: "焦点移到右边", Run: do(func(a *App, _ Args) { a.focusSide("right") })},

		// The grid moves in screen terms: j is down whichever way it is turned (§7.6).
		"grid.left":  {Title: "左移", Run: do(func(a *App, args Args) { a.gridMove(by(0, -max(args.Count, 1))) })},
		"grid.down":  {Title: "下移", Run: do(func(a *App, args Args) { a.gridMove(by(max(args.Count, 1), 0)) })},
		"grid.up":    {Title: "上移", Run: do(func(a *App, args Args) { a.gridMove(by(-max(args.Count, 1), 0)) })},
		"grid.right": {Title: "右移", Run: do(func(a *App, args Args) { a.gridMove(by(0, max(args.Count, 1))) })},
		"grid.top": {Title: "第一行", Run: do(func(a *App, _ Args) {
			a.gridMove(func(_, c, _, _ int) (int, int) { return 0, c })
		})},
		"grid.bottom": {Title: "最后一行", Run: do(func(a *App, _ Args) {
			a.gridMove(func(_, c, rows, _ int) (int, int) { return rows - 1, c })
		})},
		"grid.first": {Title: "第一列", Run: do(func(a *App, _ Args) {
			a.gridMove(func(r, _, _, _ int) (int, int) { return r, 0 })
		})},
		"grid.last": {Title: "最后一列", Run: do(func(a *App, _ Args) {
			a.gridMove(func(r, _, _, cols int) (int, int) { return r, cols - 1 })
		})},
		"grid.transpose": {Title: "转置", Run: do(func(a *App, _ Args) { a.gridTranspose() })},
		// The query bar (§7.8「查询条」).
		"grid.where": {Title: "WHERE 条件", Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil {
				t.typing, t.where.Pos = "where", len(t.where.Text)
			}
		})},
		"grid.page": {Title: "PAGE", Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil && t.page.Cols != nil {
				n := strconv.Itoa(t.pageNo + 1)
				t.typing, t.pageIn = "page", ui.Input{Text: n, Pos: len(n)}
			}
		})},
		"grid.order":     {Title: "ORDER", Run: do(func(a *App, _ Args) { a.openDrop(dropOrder) })},
		"grid.limit":     {Title: "LIMIT", Run: do(func(a *App, _ Args) { a.openDrop(dropLimit) })},
		"grid.cols":      {Title: "COLS", Run: do(func(a *App, _ Args) { a.openCols() })},
		"grid.page.next": {Title: "下一页", Run: func(a *App, _ Args) tea.Cmd { return a.turnPage(1) }},
		"grid.page.prev": {Title: "上一页", Run: func(a *App, _ Args) tea.Cmd { return a.turnPage(-1) }},
		"grid.refresh": {Title: "刷新", Run: func(a *App, _ Args) tea.Cmd {
			if t := dataOf(a.focused()); t != nil {
				return a.fetch(t, true)
			}
			return nil
		}},
		// "grid.goto <rec> <field>" is a click on a cell.
		"grid.goto": {Run: do(func(a *App, args Args) { a.gridGoto(args.Arg) })},

		"pane.resize.left":  {Title: "向左调整大小", Run: do(func(a *App, args Args) { a.resizePane(Horiz, -1, args.Count) })},
		"pane.resize.down":  {Title: "向下调整大小", Run: do(func(a *App, args Args) { a.resizePane(Vert, 1, args.Count) })},
		"pane.resize.up":    {Title: "向上调整大小", Run: do(func(a *App, args Args) { a.resizePane(Vert, -1, args.Count) })},
		"pane.resize.right": {Title: "向右调整大小", Run: do(func(a *App, args Args) { a.resizePane(Horiz, 1, args.Count) })},
	}
	// Built by later features but titled already: which-key names the ones
	// default.toml binds, the palette lists them all (§6.8); running them
	// does nothing yet.
	for id, title := range map[string]string{
		"save":         "保存",
		"session.list": "session 列表", "session.new": "新建连接",
		"window.new": "新建 window", "window.rename": "重命名 window", "window.close": "关闭 window",
		"window.next": "下一个 window", "window.prev": "上一个 window", "window.last": "上次用的 window",
		"tab.next": "下一个 tab", "tab.prev": "上一个 tab",
		"grid.edit": "编辑单元格",
		"grid.yank": "复制单元格", "grid.yank.insert": "复制为 INSERT",
		"result.pin": "固定结果", "result.close": "关闭结果",
		"console.run": "执行", "console.format": "格式化", "console.schema": "切换 schema",
	} {
		actions[id] = Action{Title: title}
	}
}

// when adapts an action that only means something while open holds: the
// overlay it belongs to is up. Bound elsewhere in config, it does nothing.
func when(open func(*App) bool, f func(*App) tea.Cmd) func(*App, Args) tea.Cmd {
	return func(a *App, _ Args) tea.Cmd {
		if !open(a) {
			return nil
		}
		return f(a)
	}
}

func inPalette(a *App) bool { return a.palette != nil }
func inDrop(a *App) bool    { return a.drop != nil }
func inCols(a *App) bool    { return a.cols != nil }
func inComplete(a *App) bool {
	t := a.typingTab()
	return t != nil && t.comp != nil
}
func inHist(a *App) bool {
	t := a.typingTab()
	return t != nil && t.hist != nil
}

// by is a grid move of dr rows and dc columns.
func by(dr, dc int) func(r, c, _, _ int) (int, int) {
	return func(r, c, _, _ int) (int, int) { return r + dr, c + dc }
}

// do adapts an action that only changes state.
func do(f func(*App, Args)) func(*App, Args) tea.Cmd {
	return func(a *App, args Args) tea.Cmd { f(a, args); return nil }
}

// title names "id [arg]" for which-key and the palette, the arg after the title.
func title(action string) string {
	id, arg, _ := strings.Cut(action, " ")
	t := actions[id].Title
	if t == "" {
		t = id
	}
	return strings.TrimSpace(t + " " + arg)
}

// run executes "id [arg]" from the registry.
func (a *App) run(action string, count int) tea.Cmd {
	id, arg, _ := strings.Cut(action, " ")
	act := actions[id]
	if act.Run == nil {
		return nil
	}
	return act.Run(a, Args{Count: count, Arg: arg})
}
