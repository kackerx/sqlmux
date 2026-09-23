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
	}
}

// run executes "id [arg]" from the registry.
func (a *App) run(action string, count int) tea.Cmd {
	id, arg, _ := strings.Cut(action, " ")
	act, ok := actions[id]
	if !ok {
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
