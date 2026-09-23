package app

import (
	"fmt"
	"slices"
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
}

// quitWindow is how soon a second C-c must follow the first to quit (§6.8).
const quitWindow = 2 * time.Second

// actions is the registry, keyed by action ID. Bound IDs that are not here
// yet belong to later features and do nothing. It is filled in init because
// actions run keys through the registry themselves.
var actions map[string]Action

func init() {
	actions = map[string]Action{
		"cmdline.open": {"命令行", func(a *App, _ Args) tea.Cmd {
			s := ""
			a.cmdline = &s
			return nil
		}},
		"cancel": {"取消 / 连按两次退出", func(a *App, _ Args) tea.Cmd {
			if a.mode() != keymap.Normal { // in any input C-c is esc, as in vim (§6.8)
				return a.dispatch([]keymap.Result{{Keys: []keymap.Key{keymap.Esc}}})
			}
			now := time.Now()
			if now.Sub(a.quitArmed) <= quitWindow {
				return tea.Quit
			}
			a.quitArmed = now
			return a.showToast(fmt.Sprintf("再按一次 %s 退出", a.keys.Hint("cancel", "global")))
		}},
		"quit":      {"退出", func(*App, Args) tea.Cmd { return tea.Quit }},
		"tab.close": {"关闭 tab", func(a *App, _ Args) tea.Cmd { a.closeTab(); return nil }},

		"pane.split.right": {"左右分割", do(func(a *App, _ Args) { a.splitPane(Horiz) })},
		"pane.split.below": {"上下分割", do(func(a *App, _ Args) { a.splitPane(Vert) })},
		"pane.close":       {"关闭 pane", do(func(a *App, _ Args) { a.closePane() })},
		"pane.zoom":        {"缩放 / 还原", do(func(a *App, _ Args) { a.toggleZoom() })},
		"pane.number":      {"按编号跳转", do(func(a *App, _ Args) { a.paneNumbers = true })},
		"tree.toggle":      {"折叠 / 展开 schema 树", do(func(a *App, _ Args) { a.toggleTree() })},

		"pane.focus.left":  {"焦点移到左边", do(func(a *App, _ Args) { a.focusSide("left") })},
		"pane.focus.down":  {"焦点移到下边", do(func(a *App, _ Args) { a.focusSide("down") })},
		"pane.focus.up":    {"焦点移到上边", do(func(a *App, _ Args) { a.focusSide("up") })},
		"pane.focus.right": {"焦点移到右边", do(func(a *App, _ Args) { a.focusSide("right") })},

		"pane.resize.left":  {"向左调整大小", do(func(a *App, args Args) { a.resizePane(Horiz, -1, args.Count) })},
		"pane.resize.down":  {"向下调整大小", do(func(a *App, args Args) { a.resizePane(Vert, 1, args.Count) })},
		"pane.resize.up":    {"向上调整大小", do(func(a *App, args Args) { a.resizePane(Vert, -1, args.Count) })},
		"pane.resize.right": {"向右调整大小", do(func(a *App, args Args) { a.resizePane(Horiz, 1, args.Count) })},
	}
	// Bound by default.toml but built by later features: titled already, so
	// which-key can name them; running them does nothing yet.
	for id, title := range map[string]string{
		"palette.open": "命令面板", "save": "保存",
		"session.list": "session 列表", "session.new": "新建连接",
		"window.select": "切换 window", "window.new": "新建 window",
		"window.rename": "重命名 window", "window.close": "关闭 window",
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

// do adapts an action that only changes state.
func do(f func(*App, Args)) func(*App, Args) tea.Cmd {
	return func(a *App, args Args) tea.Cmd { f(a, args); return nil }
}

// title names "id [arg]" for which-key and the palette: "切换 window 3".
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

// commands maps : commands to actions.
var commands = map[string]string{"q": "tab.close", "qa": "quit"}

// matchCommands lists the commands the typed text could become, as the
// status bar shows them in COMMAND mode: ":q", ":qa".
func matchCommands(typed string) []string {
	var out []string
	for name := range commands {
		if strings.HasPrefix(name, typed) {
			out = append(out, ":"+name)
		}
	}
	slices.Sort(out)
	return out
}

func (a *App) exec(cmd string) tea.Cmd {
	if cmd == "" {
		return nil
	}
	if action, ok := commands[cmd]; ok {
		return a.run(action, 0)
	}
	return a.showToast("未知命令: " + cmd)
}
