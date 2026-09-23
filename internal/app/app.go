// Package app holds the single state tree and its Update/View (tech-design §3).
package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

var toastTTL = 3 * time.Second

type App struct {
	w, h  int
	theme *ui.Theme
	icons *ui.Icons
	keys  *keymap.Map
	res   *keymap.Resolver
	win   *Window

	cmdline *string // non-nil while the : command line is open (COMMAND mode)

	toast     string
	toastSeq  int
	quitArmed time.Time // first C-c of a quitting pair
}

type (
	toastExpired struct{ seq int }
	keyTimeout   struct{ seq int }
)

func New(cfg *config.Config, keys *keymap.Map) *App {
	return &App{
		theme: ui.TokyonightStorm, icons: ui.IconSet(cfg.Icons),
		keys: keys, res: keymap.NewResolver(keys), win: fakeWindow(),
	}
}

func (a *App) Init() tea.Cmd { return nil }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
	case toastExpired:
		if msg.seq == a.toastSeq {
			a.toast = ""
		}
	case tea.KeyPressMsg:
		out, wait := a.res.Feed(a.context(), keymap.FromTea(msg.Key()))
		cmd := a.dispatch(out)
		if wait {
			seq := a.res.Seq()
			cmd = tea.Batch(cmd, tea.Tick(a.keys.Timeout, func(time.Time) tea.Msg { return keyTimeout{seq} }))
		}
		return a, cmd
	case keyTimeout:
		return a, a.dispatch(a.res.Timeout(a.context(), msg.seq))
	}
	return a, nil
}

// dispatch runs what the keymap resolved: actions through the registry, and
// unclaimed keys to the focused widget.
func (a *App) dispatch(out []keymap.Result) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range out {
		if r.Action != "" {
			cmds = append(cmds, a.run(r.Action, r.Count))
		} else if a.cmdline != nil {
			for _, k := range r.Keys {
				cmds = append(cmds, a.cmdlineKey(k))
			}
		}
	}
	return tea.Batch(cmds...)
}

// Mode is derived from state, never stored (§3 principle 3).
type Mode uint8

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual
	ModeCommand
)

// ponytail: only NORMAL and COMMAND exist until inputs (M1) and the console
// editor (M3) arrive.
func (a *App) mode() Mode {
	if a.cmdline != nil {
		return ModeCommand
	}
	return ModeNormal
}

// context tells the keymap which scopes apply to the next key (§6.4).
func (a *App) context() keymap.Context {
	if a.mode() == ModeCommand {
		return keymap.Context{Overlay: "cmdline", Mode: keymap.Insert}
	}
	scope := [...]string{KindSchema: "tree", KindData: "grid", KindConsole: "console"}[a.focused().Kind]
	return keymap.Context{Focus: []string{scope}, Pane: scope}
}

func (a *App) focused() *Pane {
	if a.win.Focus == a.win.Tree.ID {
		return a.win.Tree
	}
	for _, p := range a.win.Root.Leaves() {
		if p.ID == a.win.Focus {
			return p
		}
	}
	return a.win.Tree
}

// cmdlineKey edits the : command line; it is a plain input, so its own
// editing keys are not bindings.
func (a *App) cmdlineKey(k keymap.Key) tea.Cmd {
	switch k {
	case keymap.Esc:
		a.cmdline = nil
	case "<CR>":
		cmd := *a.cmdline
		a.cmdline = nil
		return a.exec(cmd)
	case "<BS>":
		r := []rune(*a.cmdline)
		if len(r) == 0 {
			a.cmdline = nil
		} else {
			*a.cmdline = string(r[:len(r)-1])
		}
	default:
		*a.cmdline += keymap.Text(k)
	}
	return nil
}

func (a *App) showToast(s string) tea.Cmd {
	a.toast = s
	a.toastSeq++
	seq := a.toastSeq
	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastExpired{seq} })
}

func (a *App) View() tea.View {
	v := tea.NewView(a.render().Render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}
