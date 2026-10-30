package app

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// Args is what an action gets from the key press or command that ran it.
type Args struct {
	Count int    // count prefix; 0 when none was typed
	Arg   string // the "3" of "window.select 3"
}

// Action is the single path every key, click and palette pick goes through
// (tech-design §6.1).
type Action struct {
	Title string // shown by which-key, the ? help and the palette (§6.7)
	Run   func(*App, Args) tea.Cmd
	On    func(*App) bool // a toggle's state: the palette shows ON / OFF and stays open (§12)
	Local bool            // an overlay's or an input's own key, which the palette leaves out (§12)
	SQL   bool            // for a SQL connection: the palette leaves it out for another (§5)
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
		"palette.open":    {Title: i18n.N("命令面板"), Run: do(func(a *App, _ Args) { a.openPalette("") })},
		"palette.command": {Title: i18n.N("命令面板：命令"), Run: do(func(a *App, _ Args) { a.openPalette(">") })}, // as if > was typed; bind : to it for the command scope (§6.8)
		"theme.switch":    {Title: i18n.N("切换主题…"), Run: do(func(a *App, _ Args) { a.openThemeDrop() })},
		"layout.save":     {Title: i18n.N("保存布局为…"), Run: do(func(a *App, _ Args) { a.askLayoutName() })},
		"session.close":   {Title: i18n.N("关闭 session"), Run: func(a *App, _ Args) tea.Cmd { return a.closeSession(a.sess) }},
		"session.rename":  {Title: i18n.N("重命名 session…"), Run: do(func(a *App, _ Args) { a.askSessionName() })},
		"query.rename":    {Title: i18n.N("重命名 query…"), Run: do(func(a *App, _ Args) { a.askQueryName() })},
		"layout.open": {Title: i18n.N("打开布局"), Run: func(a *App, _ Args) tea.Cmd {
			if len(a.sess.Conns) == 0 { // a layout is of connections (F7.4)
				return a.noConnection()
			}
			a.openLayoutDrop()
			return nil
		}},
		"palette.window":  {Title: i18n.N("命令面板：窗口"), Run: do(func(a *App, _ Args) { a.openPalette("&") })},
		"palette.tab":     {Title: i18n.N("命令面板：Tab"), Run: do(func(a *App, _ Args) { a.openPalette("#") })},
		"palette.table":   {Title: i18n.N("命令面板：表"), Run: do(func(a *App, _ Args) { a.openPalette("@") })},
		"palette.session": {Title: i18n.N("命令面板：session"), Run: do(func(a *App, _ Args) { a.openPalette("$") })},
		"tips.next":       {Title: i18n.N("下一条提示"), Run: do(func(a *App, _ Args) { a.nextTip() })},
		"tips.show":       {Title: i18n.N("所有提示…"), Run: do(func(a *App, _ Args) { a.openPalette("?") })},  // ? as if ? was typed: the tips scope
		"palette.sql":     {Title: i18n.N("快速 SQL"), Run: do(func(a *App, _ Args) { a.openPalette(";") })}, // ; as if ; was typed (§12)
		// Keys inside the palette. Like every overlay's own actions they are
		// Local, which the palette does not list (§12); bound elsewhere in
		// config, they do nothing.
		"palette.preview.focus": {Title: i18n.N("进入预览区"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.previewFocus(); return nil })},
		"palette.preview.leave": {Title: i18n.N("回到列表"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.previewKey(keymap.Esc) })},
		"palette.preview.up":    {Title: i18n.N("向上滚动预览"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.scrollPreview(-1); return nil })},
		"palette.preview.down":  {Title: i18n.N("向下滚动预览"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.scrollPreview(1); return nil })},
		"palette.up":            {Title: i18n.N("上移"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.paletteMove(-1); return nil })},
		"palette.down":          {Title: i18n.N("下移"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.paletteMove(1); return nil })},
		"palette.run":           {Title: i18n.N("执行"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.paletteEnter() })},
		"palette.open.tab":      {Title: i18n.N("打开"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.paletteRun(a.palette.sel, true) })},
		"palette.close": {Title: i18n.N("关闭命令面板"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { // quick SQL's candidates first (§9.7)
			if a.palette.comp != nil {
				a.palette.comp = nil
			} else {
				a.palette = nil
			}
			return nil
		})},
		"quicksql.copy":      {Title: i18n.N("复制结果"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.copyQuick() })},
		"palette.scope.next": {Title: i18n.N("下一个范围"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { s, _ := a.paletteScope(); a.paletteScopeTo(s + 1); return nil })},
		"palette.scope.prev": {Title: i18n.N("上一个范围"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { s, _ := a.paletteScope(); a.paletteScopeTo(s - 1); return nil })},
		// "palette.scope <i>" is a click on a scope tab.
		"palette.scope": {Run: func(a *App, args Args) tea.Cmd {
			if i, err := strconv.Atoi(args.Arg); err == nil && a.palette != nil {
				a.paletteScopeTo(i)
			}
			return nil
		}},
		"cancel": {Title: i18n.N("取消 / 连按两次退出"), Run: func(a *App, _ Args) tea.Cmd {
			if a.valueActive() { // esc, as in any input (§6.8)
				return a.valueKey(keymap.Esc)
			}
			if p := a.palette; p != nil && p.quick != nil && p.quick.running != "" { // the palette stays (§12)
				p.conn.Meta.Cancel()
				return nil
			}
			if ed := a.vim(); ed != nil && ed.Plain() { // a plain editor's: the selection, then the list, else the first press (§6.9)
				switch {
				case ed.HasSelection():
					ed.ClearSelection()
					return nil
				case a.completing() != nil:
					return a.press(keymap.Esc)
				}
			} else if a.mode() != keymap.Normal || inWhere(a) { // in any input C-c is esc, as in vim (§6.8), in a WHERE's NORMAL too (F3.39)
				return a.press(keymap.Esc)
			}
			if c := a.conn(); c != nil && c.busy > 0 { // a query or a save of the current connection is running: cancel it (§8.3, §10.3)
				c.cancel()
				return nil
			}
			if a.toast != "" && a.toastSeq == a.quitToast { // the first press's toast is still up
				return a.quit()
			}
			cmd := a.showToastFor(fmt.Sprintf(i18n.T("再按一次 %s 退出"), a.keys.Hint("cancel", "global")), quitWindow)
			a.quitToast = a.toastSeq
			return cmd
		}},
		"quit":      {Title: i18n.N("退出"), Run: func(a *App, _ Args) tea.Cmd { return a.quit() }},
		"tab.close": {Title: i18n.N("关闭 tab"), Run: func(a *App, _ Args) tea.Cmd { return a.closeTabAsking(a.focused(), a.focused().Cur) }},
		// "tab.close.at <pane> <i>" is a click on a tab's × (F3.35): x on that
		// tab, the current one staying so
		"tab.close.at": {Run: func(a *App, args Args) tea.Cmd {
			var id, i int
			p := (*Pane)(nil)
			if _, err := fmt.Sscan(args.Arg, &id, &i); err == nil {
				p = a.win().pane(id)
			}
			if p == nil {
				return nil
			}
			a.focusPane(id)
			return a.closeTabAsking(p, i)
		}},
		// "tab.scroll <pane> <dir>" is a click on a tab bar's ‹N or N›: a screen
		// of tabs toward them (F17.1); Draw puts it right
		"tab.scroll": {Run: func(a *App, args Args) tea.Cmd {
			var id, dir int
			if _, err := fmt.Sscan(args.Arg, &id, &dir); err == nil {
				if p := a.win().pane(id); p != nil {
					p.tabScroll.First += dir * max(p.tabScroll.Shown, 1)
				}
			}
			return nil
		}},
		// :wq (§11): a table's changes are saved first, and it closes once
		// they are; a query is written by closing it anyway.
		"tab.save.close": {Title: i18n.N("保存并关闭 tab"), Run: func(a *App, _ Args) tea.Cmd {
			if t := dataOf(a.focused()); t != nil && t.changes() > 0 {
				return a.saveTab(t, true)
			}
			return a.run("tab.close", 0)
		}},
		"tab.quit":      {Title: i18n.N("关闭 tab，空了连 pane / window 一起关"), Run: func(a *App, _ Args) tea.Cmd { return a.quitTab() }},
		"tab.reopen":    {Title: i18n.N("重开关掉的 tab"), Run: func(a *App, _ Args) tea.Cmd { return a.reopenTab() }},
		"tab.next":      {Title: i18n.N("下一个 tab"), Run: do(func(a *App, args Args) { a.cycleTab(1, args.Count) })},
		"tab.prev":      {Title: i18n.N("上一个 tab"), Run: do(func(a *App, args Args) { a.cycleTab(-1, args.Count) })},
		"tab.move.next": {Title: i18n.N("把 tab 移到下一个 pane"), Run: func(a *App, _ Args) tea.Cmd { return a.moveTab(1) }},
		"tab.move.prev": {Title: i18n.N("把 tab 移到上一个 pane"), Run: func(a *App, _ Args) tea.Cmd { return a.moveTab(-1) }},
		"tab.new":       {Title: i18n.N("新建 tab"), Run: do(func(a *App, _ Args) { a.newTab() })},
		// a landing page's two buttons (§5「引导页」)
		"tab.table": {Title: i18n.N("打开表"), Run: do(func(a *App, _ Args) {
			p := a.focused()
			a.openPalette("@")
			if p != a.win().Tree && p.tab().landing() {
				a.palette.into = p
			}
		})},
		"query.new": {Title: i18n.N("新建 query"), Run: func(a *App, _ Args) tea.Cmd {
			c := a.conn()
			if c == nil {
				return a.noConnection()
			}
			cmd, _ := a.newQuery(c)
			return cmd
		}},
		// "query.run <line>" is a click on a ▶ (§11「执行」).
		"query.run":     {Title: i18n.N("执行"), Run: func(a *App, args Args) tea.Cmd { return a.queryRun(args.Arg) }},
		"query.schema":  {Title: i18n.N("切换 schema"), Run: do(func(a *App, _ Args) { a.openQueryDrop(dropSchema) })},
		"query.history": {Title: i18n.N("查询历史…"), Run: do(func(a *App, _ Args) { a.openQueryDrop(dropHistory) })},
		"query.conn":    {Title: i18n.N("切换连接…"), Run: do(func(a *App, _ Args) { a.openQueryDrop(dropQueryConn) })},
		"query.tx":      {Run: do(func(a *App, _ Args) { a.openQueryDrop(dropTx) })}, // a click on Tx: 自动 ▾; the palette has tx.mode (§7.8)
		// The current connection's transaction (§5, F5.3).
		"tx.mode": {Title: i18n.N("手动提交事务"), Run: func(a *App, _ Args) tea.Cmd {
			c := a.conn()
			return a.setTxMode(c, c == nil || !c.Manual)
		},
			On: func(a *App) bool { c := a.conn(); return c != nil && c.Manual }},
		"tx.commit":   {Title: i18n.N("提交"), Run: func(a *App, _ Args) tea.Cmd { return a.endTx(a.conn(), "commit") }},
		"tx.rollback": {Title: i18n.N("回滚"), Run: func(a *App, _ Args) tea.Cmd { return a.endTx(a.conn(), "rollback") }},
		// The result area's (§11).
		"result.rerun": {Title: i18n.N("重跑"), Run: func(a *App, _ Args) tea.Cmd { return a.rerun() }},
		// records selected on a table (F10.7): m marks one, v begins and ends VISUAL, V its UNSET
		"grid.select.toggle": {Title: i18n.N("标记 / 取消标记这条记录"), Run: do(func(a *App, _ Args) { a.selectToggle() })},
		"grid.select.visual": {Title: i18n.N("VISUAL 扩选记录"), Run: do(func(a *App, _ Args) { a.selectVisual(false) })},
		"grid.select.unset":  {Title: i18n.N("取消选择（范围）"), Run: do(func(a *App, _ Args) { a.selectVisual(true) })},
		"grid.select.clear":  {Title: i18n.N("清除选中 / 结束 VISUAL"), Local: true, Run: do(func(a *App, _ Args) { a.selectClear() })},
		"grid.select.delete": {Title: i18n.N("标记删除选中的记录"), Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.selectDelete() }},
		"grid.select.yank":   {Title: i18n.N("复制选中的记录为 JSON"), Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.selectYank() }},
		// the flash-style jump (§6.10, F10.8)
		"jump.start":       {Title: i18n.N("跳转到屏幕上的字符"), Run: func(a *App, _ Args) tea.Cmd { return a.jumpStart() }},
		"jump.cancel":      {Title: i18n.N("退出跳转"), Local: true, Run: do(func(a *App, _ Args) { a.jump = nil })},
		"jump.back":        {Title: i18n.N("跳转：删一个字符"), Local: true, Run: do(func(a *App, _ Args) { a.jumpBack() })},
		"jump.confirm":     {Title: i18n.N("跳到最近的匹配"), Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.jumpConfirm() }},
		"result.page.next": {Title: i18n.N("结果下一页"), Run: func(a *App, _ Args) tea.Cmd { return a.resultPage(1) }},
		"result.page.prev": {Title: i18n.N("结果上一页"), Run: func(a *App, _ Args) tea.Cmd { return a.resultPage(-1) }},
		"result.export":    {Title: i18n.N("导出…"), Run: func(a *App, _ Args) tea.Cmd { return a.exportOpen() }},
		"grid.export":      {Title: i18n.N("导出…"), Run: func(a *App, _ Args) tea.Cmd { return a.exportOpen() }},
		"result.pin": {Title: i18n.N("固定结果"), Run: do(func(a *App, _ Args) {
			if rt := resultOf(a.focused()); rt != nil && rt.run != nil && rt.run.done {
				rt.pinned = !rt.pinned
			}
		})},
		"result.close": {Title: i18n.N("关闭结果"), Run: do(func(a *App, _ Args) {
			if p := a.focused(); p == a.win().Result {
				a.closeTab(p, p.Cur)
			}
		})},

		"pane.split.right": {Title: i18n.N("左右分割"), Run: do(func(a *App, _ Args) { a.splitPane(Horiz) })},
		"pane.split.below": {Title: i18n.N("上下分割"), Run: do(func(a *App, _ Args) { a.splitPane(Vert) })},
		"pane.close": {Title: i18n.N("关闭 pane"), Run: func(a *App, _ Args) tea.Cmd {
			if p := a.focused(); p == a.win().Tree || a.win().onlyPane(p.ID) { // stays: nothing to ask about
				return nil
			}
			if cmd, ok := a.flushAll(a.focused()); !ok {
				return cmd
			}
			return a.closeAsking(unsaved(a.focused()), i18n.T("这个 pane 里"), queriesIn(a.focused()), func() tea.Cmd { a.closePane(); return nil })
		}},
		"pane.zoom": {Title: i18n.N("缩放 / 还原"), Run: do(func(a *App, _ Args) { a.toggleZoom() }),
			On: func(a *App) bool { return a.win().Zoom != 0 }},
		"pane.number": {Title: i18n.N("按编号跳转"), Run: do(func(a *App, _ Args) { a.paneNumbers = true })},
		"tree.toggle": {Title: i18n.N("打开 / 聚焦 / 收起侧栏"), Run: do(func(a *App, _ Args) { a.toggleTree() }),
			On: func(a *App) bool { return a.win().TreeOpen }},
		"tree.fold":     {Title: i18n.N("收起侧栏"), Local: true, Run: do(func(a *App, _ Args) { a.foldTree() })}, // the « on its title
		"tree.down":     {Title: i18n.N("下移"), Run: do(func(a *App, args Args) { a.treeMove(max(args.Count, 1)) })},
		"tree.up":       {Title: i18n.N("上移"), Run: do(func(a *App, args Args) { a.treeMove(-max(args.Count, 1)) })},
		"tree.top":      {Title: i18n.N("第一项"), Run: do(func(a *App, _ Args) { a.treeGo(0) })},
		"tree.bottom":   {Title: i18n.N("最后一项"), Run: do(func(a *App, _ Args) { ns, _ := a.treeNodes(); a.treeGo(len(ns) - 1) })},
		"tree.expand":   {Title: i18n.N("展开"), Run: func(a *App, _ Args) tea.Cmd { return a.treeExpand() }},
		"tree.collapse": {Title: i18n.N("折叠 / 到上一级"), Run: func(a *App, _ Args) tea.Cmd { return a.treeCollapse() }},
		"tree.open":     {Title: i18n.N("打开"), Run: func(a *App, _ Args) tea.Cmd { return a.treeOpen(false) }},
		"tree.open.tab": {Title: i18n.N("打开"), Run: func(a *App, _ Args) tea.Cmd { return a.treeOpen(true) }},
		"tree.filter":   {Title: i18n.N("过滤"), Run: do(func(a *App, _ Args) { a.treeFilter() })},
		"tree.refresh":  {Title: i18n.N("刷新表列表"), Run: func(a *App, _ Args) tea.Cmd { return a.refreshTree() }},
		"tree.pick": {Title: i18n.N("选择库 / schema…"), Run: func(a *App, _ Args) tea.Cmd {
			n, ok := a.treeNode()
			if !ok {
				return a.showToast(i18n.T("这个节点没有可选的库或 schema"))
			}
			return a.openPick(n)
		}},
		"tree.delete": {Title: i18n.N("删除 query 文件、表、视图"), Run: func(a *App, _ Args) tea.Cmd { return a.treeDelete() }},
		// Keys inside the dropdowns and the COLS list (§6.8): untitled, like the palette's.
		"grid.ddl.column": {Title: i18n.N("列的定义"), Run: func(a *App, _ Args) tea.Cmd { return a.openDDL(true) }},
		"grid.value":      {Title: i18n.N("在值编辑器中打开"), Run: func(a *App, _ Args) tea.Cmd { return a.valueOpen() }},
		"value.leave": {Title: i18n.N("值编辑器：退出"), Local: true, Run: func(a *App, args Args) tea.Cmd { // "value.leave close" is the button's: it closes with the matches of a search lit too
			switch {
			case !a.valueActive():
			case args.Arg == "close":
				return a.valueClose(false)
			default:
				return a.valueKey(keymap.Esc)
			}
			return nil
		}},
		"value.save":     {Title: i18n.N("写回并关闭"), Local: true, Run: when(inValue, func(a *App) tea.Cmd { return a.valueSave() })},
		"key.ttl":        {Title: i18n.N("设置键的 TTL"), Run: func(a *App, _ Args) tea.Cmd { return a.onKey(a.ttlKey) }},
		"key.rename":     {Title: i18n.N("键改名"), Run: func(a *App, _ Args) tea.Cmd { return a.onKey(a.renameRedisKey) }},
		"key.delete":     {Title: i18n.N("删除键"), Run: func(a *App, _ Args) tea.Cmd { return a.onKey(a.deleteRedisKey) }},
		"grid.view.json": {Title: i18n.N("以 JSON 查看"), Run: func(a *App, _ Args) tea.Cmd { return a.viewJSON() }},
		"json.leave": {Title: i18n.N("回到表格"), Local: true, Run: func(a *App, args Args) tea.Cmd { // "json.leave q" is q's, "json.leave close" the button's: they leave with the matches of a search lit too
			if args.Arg == "close" {
				if jsonViewOf(a.focused()) == nil {
					return nil
				}
				return a.viewJSON()
			}
			return a.jsonKey(a.focused(), cmp.Or(keymap.Key(args.Arg), keymap.Esc))
		}},
		"json.yank":       {Title: i18n.N("复制整个 JSON"), Run: func(a *App, _ Args) tea.Cmd { return a.jsonYank() }},
		"grid.ddl.table":  {Title: i18n.N("表的 DDL"), Run: func(a *App, _ Args) tea.Cmd { return a.openDDL(false) }},
		"ddl.down":        {Title: i18n.N("下移"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.scrollDDL(1, false); return nil })},
		"ddl.up":          {Title: i18n.N("上移"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.scrollDDL(-1, false); return nil })},
		"ddl.page.down":   {Title: i18n.N("向下滚动"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.scrollDDL(1, true); return nil })},
		"ddl.page.up":     {Title: i18n.N("向上滚动"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.scrollDDL(-1, true); return nil })},
		"ddl.top":         {Title: i18n.N("到顶"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.ddlEdge(false); return nil })},
		"ddl.bottom":      {Title: i18n.N("到底"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.ddlEdge(true); return nil })},
		"ddl.close":       {Title: i18n.N("关闭"), Local: true, Run: when(inDDL, func(a *App) tea.Cmd { a.ddl = nil; return nil })},
		"menu.up":         {Title: i18n.N("上移"), Local: true, Run: when(inMenu, func(a *App) tea.Cmd { a.menuMove(-1); return nil })},
		"menu.down":       {Title: i18n.N("下移"), Local: true, Run: when(inMenu, func(a *App) tea.Cmd { a.menuMove(1); return nil })},
		"menu.run":        {Title: i18n.N("执行"), Local: true, Run: when(inMenu, func(a *App) tea.Cmd { return a.menuRun(a.menu.sel) })},
		"menu.close":      {Title: i18n.N("关闭菜单"), Local: true, Run: when(inMenu, func(a *App) tea.Cmd { a.menu = nil; return nil })},
		"dropdown.up":     {Title: i18n.N("上移"), Local: true, Run: when(inDrop, func(a *App) tea.Cmd { a.dropMove(-1); return nil })},
		"dropdown.down":   {Title: i18n.N("下移"), Local: true, Run: when(inDrop, func(a *App) tea.Cmd { a.dropMove(1); return nil })},
		"dropdown.select": {Title: i18n.N("选中"), Local: true, Run: when(inDrop, func(a *App) tea.Cmd { return a.dropPick(a.drop.sel) })},
		"dropdown.close":  {Title: i18n.N("关闭下拉框"), Local: true, Run: when(inDrop, func(a *App) tea.Cmd { a.drop = nil; return nil })},
		"complete.up":     {Title: i18n.N("上一个候选"), Local: true, Run: when(inComplete, func(a *App) tea.Cmd { a.completing().move(-1); return nil })},
		"complete.down":   {Title: i18n.N("下一个候选"), Local: true, Run: when(inComplete, func(a *App) tea.Cmd { a.completing().move(1); return nil })},
		"complete.accept": {Run: when(inComplete, func(a *App) tea.Cmd { return a.acceptCompletion() })},
		"where.up":        {Title: i18n.N("上移"), Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.histMove(a.typingTab(), -1); return nil })},
		"where.down":      {Title: i18n.N("下移"), Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.histMove(a.typingTab(), 1); return nil })},
		"where.apply":     {Title: i18n.N("执行 WHERE"), Local: true, Run: when(inHist, func(a *App) tea.Cmd { t := a.typingTab(); return a.histApply(t, t.hist.sel) })},
		"where.edit":      {Title: i18n.N("填入输入框"), Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.histEdit(a.typingTab()); return nil })},
		// C-f: the selected entry of the list, else what is typed in the WHERE (F9.2); in any other input it is nothing.
		"where.star": {Title: i18n.N("收藏 / 取消收藏"), Local: true, Run: func(a *App, _ Args) tea.Cmd {
			switch t := a.typingTab(); {
			case t == nil:
			case t.hist != nil:
				return a.histStar(t)
			case t.typing == "where":
				return a.starWhere(t)
			}
			return nil
		}},
		"where.close": {Title: i18n.N("关闭下拉"), Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.typingTab().hist = nil; return nil })},
		// C-r in the WHERE input or on the grid, or a click on its ▾ (Q-02, F9.4).
		"where.history": {Title: i18n.N("历史 / 收藏"), Run: func(a *App, _ Args) tea.Cmd {
			t := dataOf(a.focused())
			if t == nil || t.page.Cols == nil || a.drop != nil || a.cols != nil || t.typing == "page" {
				return nil
			}
			var cmd tea.Cmd
			switch ed := t.ed; {
			case t.typing != "where":
				a.startWhere(t)
			case ed.Mode() != editor.Insert && ed.Mode() != editor.Replace: // NORMAL's /, or ▾: in INSERT what is typed then filters it (F3.39)
				ed.Feed("<Esc>") // out of VISUAL, or a command pending
				cmd = a.whereDid(t, ed.Feed("A"))
			}
			t.comp, t.hist = nil, &histMenu{}
			return cmd
		}},
		// The WHERE input in its vim's NORMAL (F3.39).
		"where.run":   {Title: i18n.N("执行"), Local: true, Run: when(inWhere, func(a *App) tea.Cmd { return a.runWhere(a.typingTab()) })},
		"where.leave": {Title: i18n.N("回到表格"), Local: true, Run: when(inWhere, func(a *App) tea.Cmd { a.typingTab().stopTyping(); return nil })},
		"session.new": {Title: i18n.N("新建 session"), Run: func(a *App, _ Args) tea.Cmd { return a.askNewSession() }},
		"query.format": {Title: i18n.N("格式化整个 query"), SQL: true, Run: func(a *App, _ Args) tea.Cmd {
			if t := a.focusedQuery(); t != nil {
				return a.formatAll(t)
			}
			return nil
		}},
		"session.list": {Title: i18n.N("session 列表"), Run: do(func(a *App, _ Args) { a.openSessions() })},
		"conn.new":     {Title: i18n.N("新建连接…"), Run: do(func(a *App, _ Args) { a.openConnForm() })},
		"conn.manage":  {Title: i18n.N("管理连接…"), Run: do(func(a *App, _ Args) { a.openConnManager() })},
		"window.new":   {Title: i18n.N("新建 window"), Run: func(a *App, _ Args) tea.Cmd { return a.addWindow() }},
		"session.next": {Title: i18n.N("下一个 session"), Run: do(func(a *App, _ Args) { a.cycleSession(1) })},
		"session.prev": {Title: i18n.N("上一个 session"), Run: do(func(a *App, _ Args) { a.cycleSession(-1) })},
		"session.last": {Title: i18n.N("上次用的 session"), Run: do(func(a *App, _ Args) { a.attachLast() })},
		"window.next":  {Title: i18n.N("下一个 window"), Run: do(func(a *App, _ Args) { a.cycleWindow(1) })},
		"window.prev":  {Title: i18n.N("上一个 window"), Run: do(func(a *App, _ Args) { a.cycleWindow(-1) })},
		"window.last":  {Title: i18n.N("上次用的 window"), Run: do(func(a *App, _ Args) { a.selectWindow(a.sess.windowByID(a.sess.prevWin)) })},
		"window.close": {Title: i18n.N("关闭 window"), Run: func(a *App, _ Args) tea.Cmd { return a.closeWindow() }},
		"window.rename": {Title: i18n.N("重命名 window"), Run: do(func(a *App, _ Args) {
			a.winRename = &ui.Input{Text: a.win().Name, Pos: len(a.win().Name)}
		})},
		// "window.select <i>" is a click on the status bar's window i.
		"window.select": {Run: do(func(a *App, args Args) {
			if i, err := strconv.Atoi(args.Arg); err == nil {
				a.selectWindow(i)
			}
		})},
		"form.next":   {Title: i18n.N("下一项"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { a.formMove(1); return nil })},
		"form.prev":   {Title: i18n.N("上一项"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { a.formMove(-1); return nil })},
		"form.save":   {Title: i18n.N("保存"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { return a.saveConnForm() })},
		"form.test":   {Title: i18n.N("测试连接"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { return a.testConnForm() })},
		"form.cancel": {Title: i18n.N("取消"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { a.formCancel(); return nil })},
		// the group field's list of the groups there are (F10.6): Tab and ↵ take the one selected, or do what they do without it
		"form.group.tab": {Title: i18n.N("接受分组 / 下一项"), Local: true, Run: when(inForm, func(a *App) tea.Cmd {
			if !a.groupAccept() {
				a.formMove(1)
			}
			return nil
		})},
		"form.group.enter": {Title: i18n.N("接受分组 / 保存"), Local: true, Run: when(inForm, func(a *App) tea.Cmd {
			if a.groupAccept() {
				return nil
			}
			return a.saveConnForm()
		})},
		"form.group.next":  {Title: i18n.N("分组下一项"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { a.groupMove(1); return nil })},
		"form.group.prev":  {Title: i18n.N("分组上一项"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { a.groupMove(-1); return nil })},
		"form.group.close": {Title: i18n.N("关闭分组列表"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { a.form.groupOff = true; return nil })},
		// the connection manager's list (F8.16)
		"conns.down":   {Title: i18n.N("下移"), Local: true, Run: when(inConns, func(a *App) tea.Cmd { a.connsMove(1); return nil })},
		"conns.up":     {Title: i18n.N("上移"), Local: true, Run: when(inConns, func(a *App) tea.Cmd { a.connsMove(-1); return nil })},
		"conns.form":   {Title: i18n.N("编辑"), Local: true, Run: when(inConns, func(a *App) tea.Cmd { a.form.list = false; return nil })},
		"conns.delete": {Title: i18n.N("删除连接"), Local: true, Run: when(inForm, func(a *App) tea.Cmd { return a.deleteConn() })},
		"conns.close":  {Title: i18n.N("关闭"), Local: true, Run: when(inConns, func(a *App) tea.Cmd { a.form = nil; return nil })},
		// the session list's (§7.8)
		"sessions.new":      {Title: i18n.N("新建 session"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { return a.askNewSession() })},
		"sessions.down":     {Title: i18n.N("下移"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.sessionsMove(1); return nil })},
		"sessions.up":       {Title: i18n.N("上移"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.sessionsMove(-1); return nil })},
		"sessions.expand":   {Title: i18n.N("展开"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.sessionsFold(true); return nil })},
		"sessions.collapse": {Title: i18n.N("收起"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.sessionsFold(false); return nil })},
		"sessions.attach":   {Title: i18n.N("attach"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.sessionsAttach(a.sessList.sel); return nil })},
		"sessions.kill":     {Title: i18n.N("关闭 session"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { return a.killSession() })},
		"sessions.rename":   {Title: i18n.N("重命名 session"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.renameSession(); return nil })},
		"sessions.close":    {Title: i18n.N("关闭列表"), Local: true, Run: when(inSessions, func(a *App) tea.Cmd { a.sessList = nil; return nil })},
		"cols.up":           {Title: i18n.N("上移"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsMove(-1); return nil })},
		"cols.down":         {Title: i18n.N("下移"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsMove(1); return nil })},
		"cols.move.up":      {Title: i18n.N("列上移"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsShift(-1); return nil })},
		"cols.move.down":    {Title: i18n.N("列下移"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsShift(1); return nil })},
		"cols.toggle":       {Title: i18n.N("显示 / 隐藏这一列"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsToggle(a.cols.sel); return nil })},
		"cols.all":          {Title: i18n.N("全部显示"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsSetAll(true); return nil })},
		"cols.none":         {Title: i18n.N("全部隐藏"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsSetAll(false); return nil })},
		"cols.filter":       {Title: i18n.N("过滤列"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.cols.typing = true; return nil })},
		"order.up":          {Title: i18n.N("上移"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { a.colsMove(-1); return nil })},
		"order.down":        {Title: i18n.N("下移"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { a.colsMove(1); return nil })},
		"order.filter":      {Title: i18n.N("过滤列"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { a.cols.typing = true; return nil })},
		"order.close":       {Title: i18n.N("关闭 ORDER"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { return a.colsEsc() })},
		"order.toggle":      {Title: i18n.N("加入 / 翻转排序"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { return a.orderToggleSel() })},
		"order.remove":      {Title: i18n.N("移出排序"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { return a.orderRemove() })},
		"order.clear":       {Title: i18n.N("清空排序"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { return a.orderClear() })},
		"order.only":        {Title: i18n.N("只按这一列排序"), Local: true, Run: when(inOrder, func(a *App) tea.Cmd { return a.orderPick(a.cols.sel) })},
		"cols.close":        {Title: i18n.N("关闭 COLS"), Local: true, Run: when(inCols, func(a *App) tea.Cmd { return a.colsEsc() })},
		"pane.error.close": {Title: i18n.N("关闭错误栏"), Run: func(a *App, args Args) tea.Cmd { // "pane.error.close <id>" is its ×
			p := a.focused()
			if id, err := strconv.Atoi(args.Arg); err == nil {
				p = a.win().pane(id)
			}
			switch {
			case p == nil:
			case dataOf(p) != nil:
				dataOf(p).bar = nil
			case queryOf(p) != nil:
				queryOf(p).bar = nil
				queryOf(p).ed.Feed(string(keymap.Esc)) // NORMAL's esc, which puts out the search's matches (F15.9)
			}
			return nil
		}},
		// The ? help (§6.5).
		"keyhelp.open": {Title: i18n.N("键位帮助"), Run: do(func(a *App, _ Args) {
			if h := a.keyHelp; h != nil { // a Ctrl leader's <C-a>? in it: back to its top, of what it opened on
				h.prefix, h.top = nil, 0
				return
			}
			a.keyHelp = &keyHelp{ctx: a.context()}
		})},
		"keyhelp.close": {Title: i18n.N("关闭键位帮助"), Local: true, Run: do(func(a *App, _ Args) { a.keyHelp = nil })},
		"keyhelp.back": {Title: i18n.N("上一层"), Local: true, Run: when(inKeyHelp, func(a *App) tea.Cmd {
			if h := a.keyHelp; len(h.prefix) > 0 {
				h.prefix, h.top = h.prefix[:len(h.prefix)-1], 0
			}
			return nil
		})},
		"keyhelp.scroll.down": {Title: i18n.N("向下滚动"), Local: true, Run: when(inKeyHelp, func(a *App) tea.Cmd { a.scrollKeyHelp(1, true); return nil })},
		"keyhelp.scroll.up":   {Title: i18n.N("向上滚动"), Local: true, Run: when(inKeyHelp, func(a *App) tea.Cmd { a.scrollKeyHelp(-1, true); return nil })},

		// "pane.focus <id>" is what a click runs; untitled, it stays out of the palette.
		"pane.focus": {Run: do(func(a *App, args Args) {
			if id, err := strconv.Atoi(args.Arg); err == nil {
				a.focusPane(id)
			}
		})},
		"pane.focus.next":  {Title: i18n.N("下一个 pane"), Run: do(func(a *App, _ Args) { a.focusNext() })},
		"pane.focus.left":  {Title: i18n.N("焦点移到左边"), Run: do(func(a *App, _ Args) { a.focusSide("left") })},
		"pane.focus.down":  {Title: i18n.N("焦点移到下边"), Run: do(func(a *App, _ Args) { a.focusSide("down") })},
		"pane.focus.up":    {Title: i18n.N("焦点移到上边"), Run: do(func(a *App, _ Args) { a.focusSide("up") })},
		"pane.focus.right": {Title: i18n.N("焦点移到右边"), Run: do(func(a *App, _ Args) { a.focusSide("right") })},

		// The grid moves in screen terms: j is down whichever way it is turned (§7.6).
		"grid.left":   {Title: i18n.N("左移"), Run: do(func(a *App, args Args) { a.gridMove(by(0, -max(args.Count, 1))) })},
		"grid.down":   {Title: i18n.N("下移"), Run: do(func(a *App, args Args) { a.gridMove(by(max(args.Count, 1), 0)) })},
		"grid.up":     {Title: i18n.N("上移"), Run: do(func(a *App, args Args) { a.gridMove(by(-max(args.Count, 1), 0)) })},
		"grid.right":  {Title: i18n.N("右移"), Run: do(func(a *App, args Args) { a.gridMove(by(0, max(args.Count, 1))) })},
		"grid.top":    {Title: i18n.N("第一行"), Run: func(a *App, args Args) tea.Cmd { return a.gridLine(args.Count, false) }},
		"grid.bottom": {Title: i18n.N("最后一行"), Run: func(a *App, args Args) tea.Cmd { return a.gridLine(args.Count, true) }},
		"grid.first": {Title: i18n.N("第一列"), Run: do(func(a *App, _ Args) {
			a.gridMove(func(r, _, _, _ int) (int, int) { return r, 0 })
		})},
		"grid.last": {Title: i18n.N("最后一列"), Run: do(func(a *App, _ Args) {
			a.gridMove(func(r, _, _, cols int) (int, int) { return r, cols - 1 })
		})},
		"grid.transpose":   {Title: i18n.N("转置"), Run: do(func(a *App, _ Args) { a.gridTranspose() })},
		"grid.edit":        {Title: i18n.N("编辑单元格"), Run: func(a *App, _ Args) tea.Cmd { return a.editCell(nil) }},
		"grid.revert":      {Title: i18n.N("撤回这一格的修改"), Run: do(func(a *App, _ Args) { a.revertCell() })},
		"grid.row.add":     {Title: i18n.N("新增一行"), Run: func(a *App, _ Args) tea.Cmd { return a.addRow() }},
		"grid.yank":        {Title: i18n.N("复制单元格"), Run: func(a *App, _ Args) tea.Cmd { return a.yankGrid(false) }},
		"grid.yank.row":    {Title: i18n.N("复制整行"), Run: func(a *App, _ Args) tea.Cmd { return a.yankGrid(true) }},
		"grid.yank.json":   {Title: i18n.N("复制为 JSON"), Run: func(a *App, _ Args) tea.Cmd { return a.yankJSON() }},
		"grid.yank.insert": {Title: i18n.N("复制为 INSERT"), Run: func(a *App, _ Args) tea.Cmd { return a.yankInsert() }},
		"grid.paste":       {Title: i18n.N("粘贴成新行"), Run: func(a *App, _ Args) tea.Cmd { return a.pasteRow() }},
		"grid.row.delete":  {Title: i18n.N("标记 / 取消删除这一行"), Run: func(a *App, _ Args) tea.Cmd { return a.deleteRow() }},
		"result.toggle": {Title: i18n.N("显示 / 隐藏结果区"), Run: do(func(a *App, _ Args) { a.toggleResult() }),
			On: func(a *App) bool { return a.win().Result != nil && !a.win().resultHidden }},
		"grid.refresh.auto": {Title: i18n.N("自动刷新"), Run: do(func(a *App, _ Args) { a.openDrop(dropAuto) })},
		"grid.stop":         {Title: i18n.N("停止"), Run: do(func(a *App, _ Args) { a.conn().cancel() })}, // the query bar's stop: what C-c cancels (§8.3)
		// ↵ and esc end a cell's edit alike, keeping it (G-02); text no value
		// of the column, ↵ waits, esc drops it (§10.7)
		"cell.accept": {Title: i18n.N("确定这一格"), Local: true, Run: onCell(func(t *dataTab) { t.acceptCell() })},
		"cell.done": {Title: i18n.N("结束编辑"), Local: true, Run: do(func(a *App, _ Args) { // esc: an edit no value of the column goes, what was before it back (§10.7)
			if t := a.typingTab(); t != nil && t.cell != nil && !a.endEdit() {
				t.typing, t.cell = "", nil
			}
		})},
		// a row added's cell being edited: the next field's edit (F3.33)
		"cell.field.next": {Title: i18n.N("下一个字段"), Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.cellField(1) }},
		"cell.field.prev": {Title: i18n.N("上一个字段"), Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.cellField(-1) }},
		// the options under a cell being edited (§10.2)
		"cell.option.next":  {Title: i18n.N("下一个选项"), Local: true, Run: onCell(func(t *dataTab) { t.moveOption(1) })},
		"cell.option.prev":  {Title: i18n.N("上一个选项"), Local: true, Run: onCell(func(t *dataTab) { t.moveOption(-1) })},
		"cell.up":           {Title: i18n.N("当前段加一"), Local: true, Run: onCell(func(t *dataTab) { t.stepCurrent(1) })},
		"cell.down":         {Title: i18n.N("当前段减一"), Local: true, Run: onCell(func(t *dataTab) { t.stepCurrent(-1) })},
		"cell.segment.next": {Title: i18n.N("下一段"), Local: true, Run: onCell(func(t *dataTab) { t.moveSeg(1) })},
		"cell.segment.prev": {Title: i18n.N("上一段"), Local: true, Run: onCell(func(t *dataTab) { t.moveSeg(-1) })},
		// a time's parts clicked: "cell.seg 3" picks one, "cell.inc 3" / "cell.dec 3" step it
		"cell.seg":       {Run: onSeg(func(t *dataTab, i int) { t.stepSeg(i, 0) })},
		"cell.inc":       {Run: onSeg(func(t *dataTab, i int) { t.stepSeg(i, 1) })},
		"cell.dec":       {Run: onSeg(func(t *dataTab, i int) { t.stepSeg(i, -1) })},
		"cell.options":   {Run: onCell(func(t *dataTab) { t.cell.folded = !t.cell.folded })},
		"cell.null":      {Title: i18n.N("设为 NULL"), Run: do(func(a *App, _ Args) { a.setSpecial(false) })},
		"cell.default":   {Title: i18n.N("设为 DEFAULT"), Run: do(func(a *App, _ Args) { a.setSpecial(true) })},
		"save":           {Title: i18n.N("保存"), Run: func(a *App, _ Args) tea.Cmd { return a.save() }},
		"query.external": {Title: i18n.N("在 $EDITOR 中编辑"), Run: func(a *App, _ Args) tea.Cmd { return a.external() }},
		"confirm.yes": {Title: i18n.N("确定"), Local: true, Run: when(inConfirm, func(a *App) tea.Cmd {
			then := a.confirm.then
			a.confirm = nil
			if then == nil { // a notice
				return nil
			}
			return then()
		})},
		"confirm.no": {Title: i18n.N("取消"), Local: true, Run: when(inConfirm, func(a *App) tea.Cmd { a.confirm = nil; return nil })},
		// The query bar (§7.8「查询条」).
		"grid.where": {Title: i18n.N("WHERE 条件"), Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil && t.typing != "where" {
				a.startWhere(t)
			}
		})},
		"grid.page": {Title: i18n.N("PAGE"), Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil && t.page.Cols != nil {
				n := strconv.Itoa(t.shown.pageNo + 1)
				t.typing, t.pageIn = "page", ui.Input{Text: n, Pos: len(n)}
			}
		})},
		"grid.order": {Title: i18n.N("ORDER"), Run: do(func(a *App, _ Args) { a.openCols(true) })},
		"grid.order.cycle": {Title: i18n.N("按这一列排序"), Run: func(a *App, _ Args) tea.Cmd {
			if t, col, ok := a.cursorColumn(); ok {
				return a.orderHead(t, col)
			}
			return nil
		}},
		"grid.order.add": {Title: i18n.N("加入排序"), Run: func(a *App, _ Args) tea.Cmd {
			if t, col, ok := a.cursorColumn(); ok {
				return a.orderToggle(t, col)
			}
			return nil
		}},
		"grid.col.hide": {Title: i18n.N("隐藏这一列"), Run: func(a *App, _ Args) tea.Cmd {
			if t, _, ok := a.cursorColumn(); ok {
				a.hideColumn(t, t.fieldAt(t.col))
			}
			return nil
		}},
		"grid.col.wider":    {Title: i18n.N("加宽这一列"), Run: do(func(a *App, args Args) { a.resizeCol(max(args.Count, 1)) })},
		"grid.col.narrower": {Title: i18n.N("收窄这一列"), Run: do(func(a *App, args Args) { a.resizeCol(-max(args.Count, 1)) })},
		"grid.col.fit":      {Title: i18n.N("列宽回到自动"), Run: do(func(a *App, _ Args) { a.fitCols() })},
		"grid.filter.cell":  {Title: i18n.N("按这个值过滤"), Run: func(a *App, _ Args) tea.Cmd { return a.filterCell() }},
		"grid.order.toggle": {Title: i18n.N("切换排序方向"), Run: func(a *App, _ Args) tea.Cmd {
			if t := dataOf(a.focused()); t != nil && t.page.Cols != nil {
				return a.toggleOrder(t)
			}
			return nil
		}},
		"grid.limit":     {Title: i18n.N("LIMIT"), Run: do(func(a *App, _ Args) { a.openDrop(dropLimit) })},
		"grid.cols":      {Title: i18n.N("COLS"), Run: do(func(a *App, _ Args) { a.openCols(false) })},
		"grid.page.next": {Title: i18n.N("下一页"), Run: func(a *App, _ Args) tea.Cmd { return a.turnPage(1) }},
		"grid.page.prev": {Title: i18n.N("上一页"), Run: func(a *App, _ Args) tea.Cmd { return a.turnPage(-1) }},
		"grid.refresh": {Title: i18n.N("刷新"), Run: func(a *App, _ Args) tea.Cmd {
			t := dataOf(a.focused())
			if t == nil {
				return nil
			}
			return a.unlessLost(t.changes(), "", nil, verbRefresh, func() tea.Cmd { // R drops the changes (§10.4)
				t.edits, t.added, t.deleted = nil, nil, nil
				t.deselect() // the rows are others
				return a.fetch(t, true)
			})
		}},
		// "grid.goto <rec> <field>" is a click on a cell.
		"grid.goto": {Run: do(func(a *App, args Args) { a.gridGoto(args.Arg) })},

		"pane.resize.left":  {Title: i18n.N("向左调整大小"), Run: do(func(a *App, args Args) { a.resizePane(Horiz, -1, args.Count) })},
		"pane.resize.down":  {Title: i18n.N("向下调整大小"), Run: do(func(a *App, args Args) { a.resizePane(Vert, 1, args.Count) })},
		"pane.resize.up":    {Title: i18n.N("向上调整大小"), Run: do(func(a *App, args Args) { a.resizePane(Vert, -1, args.Count) })},
		"pane.resize.right": {Title: i18n.N("向右调整大小"), Run: do(func(a *App, args Args) { a.resizePane(Horiz, 1, args.Count) })},
	}
	for kind, title := range alterTitles { // the structure changes' templates (F14.4)
		if kind == renameTbl { // apart from the renames of a query and a session in the palette; the menu says 改名…
			title = i18n.N("改名表…")
		}
		actions["table.alter."+string(kind)] = Action{Title: title, Run: func(a *App, _ Args) tea.Cmd { return a.alter(kind) }}
	}
	actions["quicksql.edit"] = Action{Title: i18n.N("在 query 里编辑"), Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.quickEdit() })}
}

// Titles maps each titled action ID to its title: the keymap export notes
// them (§6.7), and keymap cannot import app.
func Titles() map[string]string {
	ts := map[string]string{}
	for id, a := range actions {
		if a.Title != "" {
			ts[id] = i18n.T(a.Title)
		}
	}
	return ts
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

// onSeg adapts an action on part Arg of the time being edited.
func onSeg(f func(t *dataTab, i int)) func(*App, Args) tea.Cmd {
	return func(a *App, args Args) tea.Cmd {
		if t, i := a.typingTab(), -1; t != nil && t.cell != nil {
			if n, err := strconv.Atoi(args.Arg); err == nil {
				i = n
			}
			f(t, i)
		}
		return nil
	}
}

// onCell adapts an action on the cell being edited; with none it does nothing.
func onCell(f func(*dataTab)) func(*App, Args) tea.Cmd {
	return func(a *App, _ Args) tea.Cmd {
		if t := a.typingTab(); t != nil && t.cell != nil {
			f(t)
		}
		return nil
	}
}

func inPalette(a *App) bool  { return a.palette != nil }
func inSessions(a *App) bool { return a.sessList != nil }
func inForm(a *App) bool     { return a.form != nil }
func inConns(a *App) bool    { return a.form != nil && a.form.list }
func inConfirm(a *App) bool  { return a.confirm != nil }
func inDrop(a *App) bool     { return a.drop != nil }
func inCols(a *App) bool     { return a.cols != nil }
func inOrder(a *App) bool    { return a.cols != nil && a.cols.order }
func inComplete(a *App) bool { return a.completing() != nil }
func inHist(a *App) bool {
	t := a.typingTab()
	return t != nil && t.hist != nil
}

func inWhere(a *App) bool {
	t := a.typingTab()
	return t != nil && t.typing == "where"
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
	t := i18n.T(actions[id].Title)
	if t == "" {
		t = id
	}
	return strings.TrimSpace(t + " " + arg)
}

// run executes "id [arg]" from the registry.
func (a *App) run(action string, count int) tea.Cmd {
	id, arg, _ := strings.Cut(action, " ")
	// what a box that takes only its own keys has no key for is no one's: cancel is esc, which closes it (F8.23, F9.8, F12.2, §6.4)
	if ov := a.topOverlay(); (ov == "value" || ov == "menu" || ov == "ddl" || ov == "confirm") && !strings.HasPrefix(id, ov+".") && id != "cancel" {
		return nil
	}
	if !strings.HasPrefix(id, "cell.") && id != "cancel" && !a.endEdit() { // cancel is esc here, which ends it itself
		return nil
	}
	if a.jump != nil && !strings.HasPrefix(id, "jump.") && id != "cancel" { // a global key (C-p, C-s) ends it and does what it does; C-c is esc in it
		a.jump = nil
	}
	if !strings.HasPrefix(id, "keyhelp.") && id != "cancel" { // a global key (C-p) goes past the ? help: it closes (§6.5)
		a.keyHelp = nil
	}
	act := actions[id]
	if act.Run == nil {
		return nil
	}
	return act.Run(a, Args{Count: count, Arg: arg})
}
