package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/editor"
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
	Title string // shown by which-key, the ? help and the palette (§6.7)
	Run   func(*App, Args) tea.Cmd
	On    func(*App) bool // a toggle's state: the palette shows ON / OFF and stays open (§12)
	Local bool            // an overlay's or an input's own key, which the palette leaves out (§12)
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
		"palette.sql":     {Title: "快速 SQL", Run: do(func(a *App, _ Args) { a.openPalette(";") })},  // ; as if ; was typed (§12)
		// Keys inside the palette. Like every overlay's own actions they are
		// Local, which the palette does not list (§12); bound elsewhere in
		// config, they do nothing.
		"palette.up":         {Title: "上移", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.paletteMove(-1); return nil })},
		"palette.down":       {Title: "下移", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.paletteMove(1); return nil })},
		"palette.run":        {Title: "执行", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.paletteRun(a.palette.sel, false) })},
		"palette.open.tab":   {Title: "在新 tab 打开", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.paletteRun(a.palette.sel, true) })},
		"palette.close":      {Title: "关闭命令面板", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { a.palette = nil; return nil })},
		"quicksql.copy":      {Title: "复制结果", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { return a.copyQuick() })},
		"palette.scope.next": {Title: "下一个范围", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { s, _ := a.paletteScope(); a.paletteScopeTo(s + 1); return nil })},
		"palette.scope.prev": {Title: "上一个范围", Local: true, Run: when(inPalette, func(a *App) tea.Cmd { s, _ := a.paletteScope(); a.paletteScopeTo(s - 1); return nil })},
		// "palette.scope <i>" is a click on a scope tab.
		"palette.scope": {Run: func(a *App, args Args) tea.Cmd {
			if i, err := strconv.Atoi(args.Arg); err == nil && a.palette != nil {
				a.paletteScopeTo(i)
			}
			return nil
		}},
		"cancel": {Title: "取消 / 连按两次退出", Run: func(a *App, _ Args) tea.Cmd {
			if p := a.palette; p != nil && p.quick != nil && p.quick.running != "" { // the palette stays (§12)
				a.sess.Meta.Cancel()
				return nil
			}
			if a.mode() != keymap.Normal || inWhere(a) { // in any input C-c is esc, as in vim (§6.8), in a WHERE's NORMAL too (F3.39)
				return a.press(keymap.Esc)
			}
			if a.busy > 0 { // a query or a save is running: cancel it (§8.3, §10.3)
				a.sess.Meta.Cancel()
				a.sess.Main.Cancel()
				return nil
			}
			if a.toast != "" && a.toastSeq == a.quitToast { // the first press's toast is still up
				return a.quit()
			}
			cmd := a.showToast(fmt.Sprintf("再按一次 %s 退出", a.keys.Hint("cancel", "global")), quitWindow)
			a.quitToast = a.toastSeq
			return cmd
		}},
		"quit": {Title: "退出", Run: func(a *App, _ Args) tea.Cmd { return a.quit() }},
		"tab.close": {Title: "关闭 tab", Run: func(a *App, _ Args) tea.Cmd {
			if t := consoleOf(a.focused()); t != nil { // written, not asked about (§11)
				if err := t.flush(); err != nil {
					return a.saveFailed(err)
				}
				a.closeTab(a.focused())
				return nil
			}
			n, name := 0, ""
			if t := dataOf(a.focused()); t != nil {
				n, name = t.changes(), t.table.Name+" "
			}
			return a.unlessUnsaved(n, name, "关闭", func() tea.Cmd { a.closeTab(a.focused()); return nil })
		}},
		// "tab.close.at <pane> <i>" is a click on a tab's × (F3.35): x on that
		// tab, the one current before it back once it goes
		"tab.close.at": {Run: func(a *App, args Args) tea.Cmd {
			var id, i int
			p := (*Pane)(nil)
			if _, err := fmt.Sscan(args.Arg, &id, &i); err == nil {
				p = a.win().pane(id)
			}
			if p == nil || i >= len(p.Tabs) {
				return nil
			}
			a.focusPane(id)
			selectTab(p, i)
			if p == a.win().Result {
				return a.run("result.close", 0)
			}
			return a.run("tab.close", 0)
		}},
		// :wq (§11): a table's changes are saved first, and it closes once
		// they are; a console is written by closing it anyway.
		"tab.save.close": {Title: "保存并关闭 tab", Run: func(a *App, _ Args) tea.Cmd {
			if t := dataOf(a.focused()); t != nil && t.changes() > 0 {
				cmd := a.save()
				t.closing = t.saving
				return cmd
			}
			return a.run("tab.close", 0)
		}},
		"tab.next": {Title: "下一个 tab", Run: do(func(a *App, args Args) { a.cycleTab(1, args.Count) })},
		"tab.prev": {Title: "上一个 tab", Run: do(func(a *App, args Args) { a.cycleTab(-1, args.Count) })},
		"tab.new":  {Title: "新建 tab", Run: do(func(a *App, _ Args) { a.newTab() })},
		// a landing page's two buttons (§5「引导页」)
		"tab.table": {Title: "打开表", Run: do(func(a *App, _ Args) {
			p := a.focused()
			a.openPalette("@")
			if p != a.win().Tree && p.tab().landing() {
				a.palette.into = p
			}
		})},
		"console.new": {Title: "新建 console", Run: func(a *App, _ Args) tea.Cmd { return a.newConsole() }},
		// "console.run <line>" is a click on a ▶ (§11「执行」).
		"console.run":    {Title: "执行", Run: func(a *App, args Args) tea.Cmd { return a.consoleRun(args.Arg) }},
		"console.schema": {Title: "切换 schema", Run: do(func(a *App, _ Args) { a.openSchemaDrop() })},
		// The result area's (§11).
		"result.rerun":  {Title: "重跑", Run: func(a *App, _ Args) tea.Cmd { return a.rerun() }},
		"result.export": {Title: "导出 CSV", Run: func(a *App, _ Args) tea.Cmd { return a.exportResult() }},
		"result.pin": {Title: "固定结果", Run: do(func(a *App, _ Args) {
			if rt := resultOf(a.focused()); rt != nil && rt.run != nil && rt.run.done {
				rt.pinned = !rt.pinned
			}
		})},
		"result.close": {Title: "关闭结果", Run: do(func(a *App, _ Args) {
			if p := a.focused(); p == a.win().Result {
				a.closeTab(p)
			}
		})},

		"pane.split.right": {Title: "左右分割", Run: do(func(a *App, _ Args) { a.splitPane(Horiz) })},
		"pane.split.below": {Title: "上下分割", Run: do(func(a *App, _ Args) { a.splitPane(Vert) })},
		"pane.close": {Title: "关闭 pane", Run: func(a *App, _ Args) tea.Cmd {
			if cmd, ok := a.flushAll(a.focused()); !ok {
				return cmd
			}
			return a.unlessUnsaved(unsaved(a.focused()), "这个 pane 里", "关闭", func() tea.Cmd { a.closePane(); return nil })
		}},
		"pane.zoom": {Title: "缩放 / 还原", Run: do(func(a *App, _ Args) { a.toggleZoom() }),
			On: func(a *App) bool { return a.win().Zoom != 0 }},
		"pane.number": {Title: "按编号跳转", Run: do(func(a *App, _ Args) { a.paneNumbers = true })},
		"tree.toggle": {Title: "折叠 / 展开 schema 树", Run: do(func(a *App, _ Args) { a.toggleTree() }),
			On: func(a *App) bool { return a.win().TreeOpen }},
		"tree.down":     {Title: "下移", Run: do(func(a *App, args Args) { a.treeMove(max(args.Count, 1)) })},
		"tree.up":       {Title: "上移", Run: do(func(a *App, args Args) { a.treeMove(-max(args.Count, 1)) })},
		"tree.top":      {Title: "第一项", Run: do(func(a *App, _ Args) { a.treeGo(0) })},
		"tree.bottom":   {Title: "最后一项", Run: do(func(a *App, _ Args) { ns, _ := a.treeNodes(); a.treeGo(len(ns) - 1) })},
		"tree.expand":   {Title: "展开", Run: func(a *App, _ Args) tea.Cmd { return a.treeExpand() }},
		"tree.collapse": {Title: "折叠 / 到上一级", Run: func(a *App, _ Args) tea.Cmd { return a.treeCollapse() }},
		"tree.open":     {Title: "打开", Run: func(a *App, _ Args) tea.Cmd { return a.treeOpen(false) }},
		"tree.open.tab": {Title: "在新 tab 打开", Run: func(a *App, _ Args) tea.Cmd { return a.treeOpen(true) }},
		"tree.filter":   {Title: "过滤", Run: do(func(a *App, _ Args) { a.treeFilter() })},
		"tree.refresh":  {Title: "刷新表列表", Run: func(a *App, _ Args) tea.Cmd { a.sess.dropCols(); return a.loadCatalog() }},
		// Keys inside the dropdowns and the COLS list (§6.8): untitled, like the palette's.
		"dropdown.up":     {Title: "上移", Local: true, Run: when(inDrop, func(a *App) tea.Cmd { a.dropMove(-1); return nil })},
		"dropdown.down":   {Title: "下移", Local: true, Run: when(inDrop, func(a *App) tea.Cmd { a.dropMove(1); return nil })},
		"dropdown.select": {Title: "选中", Local: true, Run: when(inDrop, func(a *App) tea.Cmd { return a.dropPick(a.drop.sel) })},
		"dropdown.close":  {Title: "关闭下拉框", Local: true, Run: when(inDrop, func(a *App) tea.Cmd { a.drop = nil; return nil })},
		"complete.up":     {Title: "上一个候选", Local: true, Run: when(inComplete, func(a *App) tea.Cmd { a.completing().move(-1); return nil })},
		"complete.down":   {Title: "下一个候选", Local: true, Run: when(inComplete, func(a *App) tea.Cmd { a.completing().move(1); return nil })},
		"complete.accept": {Run: when(inComplete, func(a *App) tea.Cmd { _, cmd := a.acceptCompletion(); return cmd })},
		"where.up":        {Title: "上移", Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.histMove(a.typingTab(), -1); return nil })},
		"where.down":      {Title: "下移", Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.histMove(a.typingTab(), 1); return nil })},
		"where.apply":     {Title: "执行 WHERE", Local: true, Run: when(inHist, func(a *App) tea.Cmd { t := a.typingTab(); return a.histApply(t, t.hist.sel) })},
		"where.star":      {Title: "收藏 / 取消收藏", Local: true, Run: when(inHist, func(a *App) tea.Cmd { return a.histStar(a.typingTab()) })},
		"where.close":     {Title: "关闭下拉", Local: true, Run: when(inHist, func(a *App) tea.Cmd { a.typingTab().hist = nil; return nil })},
		// C-r in the WHERE input, or a click on its ▾ from the grid (Q-02).
		"where.history": {Title: "历史 / 收藏", Local: true, Run: func(a *App, _ Args) tea.Cmd {
			t := dataOf(a.focused())
			if t == nil || t.page.Cols == nil || a.drop != nil || a.cols != nil || t.typing == "page" {
				return nil
			}
			var cmd tea.Cmd
			switch {
			case t.typing != "where":
				a.startWhere(t)
			case t.ed.Mode() != editor.Insert: // NORMAL's /: what is typed then filters it (F3.39)
				cmd = a.whereDid(t, t.ed.Feed("A"))
			}
			t.comp, t.hist = nil, &histMenu{}
			return cmd
		}},
		// The WHERE input in its vim's NORMAL (F3.39).
		"where.run":   {Title: "执行", Local: true, Run: when(inWhere, func(a *App) tea.Cmd { return a.runWhere(a.typingTab()) })},
		"where.leave": {Title: "回到表格", Local: true, Run: when(inWhere, func(a *App) tea.Cmd { a.typingTab().stopTyping(); return nil })},
		"cols.up":     {Title: "上移", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsMove(-1); return nil })},
		"cols.down":   {Title: "下移", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsMove(1); return nil })},
		"cols.toggle": {Title: "显示 / 隐藏这一列", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsToggle(a.cols.sel); return nil })},
		"cols.all":    {Title: "全部显示", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsSetAll(true); return nil })},
		"cols.none":   {Title: "全部隐藏", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsSetAll(false); return nil })},
		"cols.filter": {Title: "过滤列", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.cols.typing = true; return nil })},
		"cols.close":  {Title: "关闭 COLS", Local: true, Run: when(inCols, func(a *App) tea.Cmd { a.colsEsc(); return nil })},
		"pane.error.close": {Title: "关闭错误栏", Run: func(a *App, args Args) tea.Cmd { // "pane.error.close <id>" is its ×
			p := a.focused()
			if id, err := strconv.Atoi(args.Arg); err == nil {
				p = a.win().pane(id)
			}
			switch {
			case p == nil:
			case dataOf(p) != nil:
				dataOf(p).bar = nil
			case consoleOf(p) != nil:
				consoleOf(p).bar = nil
			}
			return nil
		}},
		// The ? help (§6.5).
		"keyhelp.open": {Title: "键位帮助", Run: do(func(a *App, _ Args) {
			if h := a.keyHelp; h != nil { // a Ctrl leader's <C-a>? in it: back to its top, of what it opened on
				h.prefix, h.top = nil, 0
				return
			}
			a.keyHelp = &keyHelp{ctx: a.context()}
		})},
		"keyhelp.close": {Title: "关闭键位帮助", Local: true, Run: do(func(a *App, _ Args) { a.keyHelp = nil })},
		"keyhelp.back": {Title: "上一层", Local: true, Run: when(inKeyHelp, func(a *App) tea.Cmd {
			if h := a.keyHelp; len(h.prefix) > 0 {
				h.prefix, h.top = h.prefix[:len(h.prefix)-1], 0
			}
			return nil
		})},
		"keyhelp.scroll.down": {Title: "向下滚动", Local: true, Run: when(inKeyHelp, func(a *App) tea.Cmd { a.scrollKeyHelp(1, true); return nil })},
		"keyhelp.scroll.up":   {Title: "向上滚动", Local: true, Run: when(inKeyHelp, func(a *App) tea.Cmd { a.scrollKeyHelp(-1, true); return nil })},

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
		"grid.left":   {Title: "左移", Run: do(func(a *App, args Args) { a.gridMove(by(0, -max(args.Count, 1))) })},
		"grid.down":   {Title: "下移", Run: do(func(a *App, args Args) { a.gridMove(by(max(args.Count, 1), 0)) })},
		"grid.up":     {Title: "上移", Run: do(func(a *App, args Args) { a.gridMove(by(-max(args.Count, 1), 0)) })},
		"grid.right":  {Title: "右移", Run: do(func(a *App, args Args) { a.gridMove(by(0, max(args.Count, 1))) })},
		"grid.top":    {Title: "第一行", Run: func(a *App, args Args) tea.Cmd { return a.gridLine(args.Count, false) }},
		"grid.bottom": {Title: "最后一行", Run: func(a *App, args Args) tea.Cmd { return a.gridLine(args.Count, true) }},
		"grid.first": {Title: "第一列", Run: do(func(a *App, _ Args) {
			a.gridMove(func(r, _, _, _ int) (int, int) { return r, 0 })
		})},
		"grid.last": {Title: "最后一列", Run: do(func(a *App, _ Args) {
			a.gridMove(func(r, _, _, cols int) (int, int) { return r, cols - 1 })
		})},
		"grid.transpose":    {Title: "转置", Run: do(func(a *App, _ Args) { a.gridTranspose() })},
		"grid.edit":         {Title: "编辑单元格", Run: func(a *App, _ Args) tea.Cmd { return a.editCell(nil) }},
		"grid.revert":       {Title: "撤回这一格的修改", Run: do(func(a *App, _ Args) { a.revertCell() })},
		"grid.row.add":      {Title: "新增一行", Run: func(a *App, _ Args) tea.Cmd { return a.addRow() }},
		"grid.yank":         {Title: "复制单元格", Run: func(a *App, _ Args) tea.Cmd { return a.yankGrid(false) }},
		"grid.yank.row":     {Title: "复制整行", Run: func(a *App, _ Args) tea.Cmd { return a.yankGrid(true) }},
		"grid.paste":        {Title: "粘贴成新行", Run: func(a *App, _ Args) tea.Cmd { return a.pasteRow() }},
		"grid.row.delete":   {Title: "标记 / 取消删除这一行", Run: func(a *App, _ Args) tea.Cmd { return a.deleteRow() }},
		"result.toggle":     {Title: "显示 / 隐藏结果区", Run: do(func(a *App, _ Args) { a.toggleResult() })},
		"grid.refresh.auto": {Title: "自动刷新", Run: do(func(a *App, _ Args) { a.openDrop(dropAuto) })},
		"grid.stop": {Title: "停止", Run: do(func(a *App, _ Args) { // the query bar's stop: what C-c cancels (§8.3)
			a.sess.Meta.Cancel()
			a.sess.Main.Cancel()
		})},
		// ↵ and esc end a cell's edit alike, keeping it (G-02); text no value
		// of the column, ↵ waits, esc drops it (§10.7)
		"cell.accept": {Title: "确定这一格", Local: true, Run: onCell(func(t *dataTab) { t.acceptCell() })},
		"cell.done": {Title: "结束编辑", Local: true, Run: do(func(a *App, _ Args) { // esc: an edit no value of the column goes, what was before it back (§10.7)
			if t := a.typingTab(); t != nil && t.cell != nil && !a.endEdit() {
				t.typing, t.cell = "", nil
			}
		})},
		// a row added's cell being edited: the next field's edit (F3.33)
		"cell.field.next": {Title: "下一个字段", Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.cellField(1) }},
		"cell.field.prev": {Title: "上一个字段", Local: true, Run: func(a *App, _ Args) tea.Cmd { return a.cellField(-1) }},
		// the options under a cell being edited (§10.2)
		"cell.option.next":  {Title: "下一个选项", Local: true, Run: onCell(func(t *dataTab) { t.moveOption(1) })},
		"cell.option.prev":  {Title: "上一个选项", Local: true, Run: onCell(func(t *dataTab) { t.moveOption(-1) })},
		"cell.up":           {Title: "当前段加一", Local: true, Run: onCell(func(t *dataTab) { t.stepCurrent(1) })},
		"cell.down":         {Title: "当前段减一", Local: true, Run: onCell(func(t *dataTab) { t.stepCurrent(-1) })},
		"cell.segment.next": {Title: "下一段", Local: true, Run: onCell(func(t *dataTab) { t.moveSeg(1) })},
		"cell.segment.prev": {Title: "上一段", Local: true, Run: onCell(func(t *dataTab) { t.moveSeg(-1) })},
		// a time's parts clicked: "cell.seg 3" picks one, "cell.inc 3" / "cell.dec 3" step it
		"cell.seg":         {Run: onSeg(func(t *dataTab, i int) { t.stepSeg(i, 0) })},
		"cell.inc":         {Run: onSeg(func(t *dataTab, i int) { t.stepSeg(i, 1) })},
		"cell.dec":         {Run: onSeg(func(t *dataTab, i int) { t.stepSeg(i, -1) })},
		"cell.options":     {Run: onCell(func(t *dataTab) { t.cell.folded = !t.cell.folded })},
		"cell.null":        {Title: "设为 NULL", Run: do(func(a *App, _ Args) { a.setSpecial(false) })},
		"cell.default":     {Title: "设为 DEFAULT", Run: do(func(a *App, _ Args) { a.setSpecial(true) })},
		"save":             {Title: "保存", Run: func(a *App, _ Args) tea.Cmd { return a.save() }},
		"console.external": {Title: "在 $EDITOR 中编辑", Run: func(a *App, _ Args) tea.Cmd { return a.external() }},
		"confirm.yes": {Title: "确定", Local: true, Run: when(inConfirm, func(a *App) tea.Cmd {
			then := a.confirm.then
			a.confirm = nil
			return then()
		})},
		"confirm.no": {Title: "取消", Local: true, Run: when(inConfirm, func(a *App) tea.Cmd { a.confirm = nil; return nil })},
		// The query bar (§7.8「查询条」).
		"grid.where": {Title: "WHERE 条件", Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil && t.typing != "where" {
				a.startWhere(t)
			}
		})},
		"grid.page": {Title: "PAGE", Run: do(func(a *App, _ Args) {
			if t := dataOf(a.focused()); t != nil && t.page.Cols != nil {
				n := strconv.Itoa(t.shown.pageNo + 1)
				t.typing, t.pageIn = "page", ui.Input{Text: n, Pos: len(n)}
			}
		})},
		"grid.order": {Title: "ORDER", Run: do(func(a *App, _ Args) { a.openDrop(dropOrder) })},
		"grid.order.toggle": {Title: "切换排序方向", Run: func(a *App, _ Args) tea.Cmd {
			if t := dataOf(a.focused()); t != nil && t.page.Cols != nil {
				return a.toggleOrder(t)
			}
			return nil
		}},
		"grid.limit":     {Title: "LIMIT", Run: do(func(a *App, _ Args) { a.openDrop(dropLimit) })},
		"grid.cols":      {Title: "COLS", Run: do(func(a *App, _ Args) { a.openCols() })},
		"grid.page.next": {Title: "下一页", Run: func(a *App, _ Args) tea.Cmd { return a.turnPage(1) }},
		"grid.page.prev": {Title: "上一页", Run: func(a *App, _ Args) tea.Cmd { return a.turnPage(-1) }},
		"grid.refresh": {Title: "刷新", Run: func(a *App, _ Args) tea.Cmd {
			t := dataOf(a.focused())
			if t == nil {
				return nil
			}
			return a.unlessUnsaved(t.changes(), "", "刷新", func() tea.Cmd { // R drops the changes (§10.4)
				t.edits, t.added, t.deleted = nil, nil, nil
				return a.fetch(t, true)
			})
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
		"session.list": "session 列表", "session.new": "新建连接",
		"window.new": "新建 window", "window.rename": "重命名 window", "window.close": "关闭 window",
		"window.next": "下一个 window", "window.prev": "上一个 window", "window.last": "上次用的 window",
		"grid.yank.insert": "复制为 INSERT",
	} {
		actions[id] = Action{Title: title}
	}
	actions["quicksql.edit"] = Action{Title: "在 console 里编辑", Local: true} // the palette's C-e: M4
}

// Titles maps each titled action ID to its title: the keymap export notes
// them (§6.7), and keymap cannot import app.
func Titles() map[string]string {
	ts := map[string]string{}
	for id, a := range actions {
		if a.Title != "" {
			ts[id] = a.Title
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
func inConfirm(a *App) bool  { return a.confirm != nil }
func inDrop(a *App) bool     { return a.drop != nil }
func inCols(a *App) bool     { return a.cols != nil }
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
	t := actions[id].Title
	if t == "" {
		t = id
	}
	return strings.TrimSpace(t + " " + arg)
}

// run executes "id [arg]" from the registry.
func (a *App) run(action string, count int) tea.Cmd {
	id, arg, _ := strings.Cut(action, " ")
	if !strings.HasPrefix(id, "cell.") && id != "cancel" && !a.endEdit() { // cancel is esc here, which ends it itself
		return nil
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
