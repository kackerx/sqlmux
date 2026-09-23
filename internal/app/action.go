package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/keymap"
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
		// Keys inside the palette; run from anywhere else (the palette lists
		// them too, and config may bind them) they do nothing.
		"palette.up":    {Title: "上一项", Run: inPalette(func(a *App) tea.Cmd { a.paletteMove(-1); return nil })},
		"palette.down":  {Title: "下一项", Run: inPalette(func(a *App) tea.Cmd { a.paletteMove(1); return nil })},
		"palette.run":   {Title: "执行选中项", Run: inPalette(func(a *App) tea.Cmd { return a.paletteRun(a.palette.sel) })},
		"palette.close": {Title: "关闭命令面板", Run: inPalette(func(a *App) tea.Cmd { a.palette = nil; return nil })},
		"cancel": {Title: "取消 / 连按两次退出", Run: func(a *App, _ Args) tea.Cmd {
			if a.mode() != keymap.Normal { // in any input C-c is esc, as in vim (§6.8)
				return a.press(keymap.Esc)
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

		"pane.resize.left":  {Title: "向左调整大小", Run: do(func(a *App, args Args) { a.resizePane(Horiz, -1, args.Count) })},
		"pane.resize.down":  {Title: "向下调整大小", Run: do(func(a *App, args Args) { a.resizePane(Vert, 1, args.Count) })},
		"pane.resize.up":    {Title: "向上调整大小", Run: do(func(a *App, args Args) { a.resizePane(Vert, -1, args.Count) })},
		"pane.resize.right": {Title: "向右调整大小", Run: do(func(a *App, args Args) { a.resizePane(Horiz, 1, args.Count) })},
	}
	// Bound by default.toml but built by later features: titled already, so
	// which-key can name them; running them does nothing yet.
	for id, title := range map[string]string{
		"save":         "保存",
		"session.list": "session 列表", "session.new": "新建连接",
		"window.new": "新建 window", "window.rename": "重命名 window", "window.close": "关闭 window",
		"window.next": "下一个 window", "window.prev": "上一个 window", "window.last": "上次用的 window",
		"tab.next": "下一个 tab", "tab.prev": "上一个 tab",
		"grid.left": "左移", "grid.down": "下移", "grid.up": "上移", "grid.right": "右移",
		"grid.top": "第一行", "grid.bottom": "最后一行", "grid.first": "第一列", "grid.last": "最后一列",
		"grid.edit": "编辑单元格", "grid.refresh": "刷新", "grid.transpose": "转置",
		"grid.where": "WHERE 条件", "grid.order": "ORDER", "grid.limit": "LIMIT",
		"grid.page": "PAGE", "grid.cols": "COLS", "grid.page.next": "下一页", "grid.page.prev": "上一页",
		"grid.yank": "复制单元格", "grid.yank.insert": "复制为 INSERT",
		"result.pin": "固定结果", "result.close": "关闭结果",
		"tree.down": "下移", "tree.up": "上移", "tree.top": "第一项", "tree.bottom": "最后一项",
		"tree.open": "打开", "tree.open.tab": "在新 tab 打开", "tree.filter": "过滤", "tree.schema": "切换 schema",
		"console.run": "执行", "console.format": "格式化", "console.schema": "切换 schema",
		"palette.open.tab": "在新 tab 打开", "quicksql.copy": "复制为 CSV", "quicksql.edit": "在 console 中打开",
		"cell.segment.next": "下一段", "cell.segment.prev": "上一段", "cell.up": "加一", "cell.down": "减一",
		"cell.option.next": "下一个选项", "cell.option.prev": "上一个选项", "cell.accept": "确定", "cell.done": "完成编辑",
	} {
		actions[id] = Action{Title: title}
	}
}

// inPalette adapts an action that only means something with the palette open.
func inPalette(f func(*App) tea.Cmd) func(*App, Args) tea.Cmd {
	return func(a *App, _ Args) tea.Cmd {
		if a.palette == nil {
			return nil
		}
		return f(a)
	}
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
