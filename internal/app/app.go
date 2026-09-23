// Package app holds the single state tree and its Update/View (tech-design §3).
package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	sess  *Session

	cmdline  *string // non-nil while the : command line is open (COMMAND mode)
	whichKey bool    // the which-key overlay is up (§6.5)
	// paneNumbers is SPC q's overlay: the next key picks a pane by its ⟨n⟩.
	paneNumbers bool

	toast     string
	toastSeq  int
	quitToast int // toastSeq of the "press C-c again" toast
}

type (
	toastExpired struct{ seq int }
	keyTimeout   struct{ seq int }
	whichKeyDue  struct{ seq int }
)

// whichKeyDelay is how long a pure prefix waits before which-key shows (§6.5).
var whichKeyDelay = 400 * time.Millisecond

func New(cfg *config.Config, keys *keymap.Map) *App {
	return &App{
		theme: ui.TokyonightStorm, icons: ui.IconSet(cfg.Icons),
		keys: keys, res: keymap.NewResolver(keys), sess: fakeSession(),
	}
}

// Init switches Bubble Tea's renderer to grapheme widths, the same as Frame
// (§7.1「宽度」): with wcwidth it re-flows lines holding 👍🏽 or ❤️ and pushes
// the border off the row. Bubble Tea v2.0.9 only switches when the terminal
// answers the mode 2027 query; tmux doesn't answer and Terminal.app is never
// asked. There is no public setting, so this hands its event loop the answer
// the terminal would have sent. When upgrading Bubble Tea, check that
// TestRendererUsesGraphemeWidths still passes, and whether a real option
// has appeared.
func (a *App) Init() tea.Cmd {
	return func() tea.Msg { return tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet} }
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
	case toastExpired:
		if msg.seq == a.toastSeq {
			a.toast = ""
		}
	case tea.KeyPressMsg:
		if a.paneNumbers {
			a.jumpToPane(keymap.FromTea(msg.Key()))
			return a, nil
		}
		out, wait := a.res.Feed(a.context(), keymap.FromTea(msg.Key()))
		cmd := a.dispatch(out)
		seq := a.res.Seq()
		switch {
		case wait: // ambiguous: the shorter binding fires after timeoutlen
			cmd = tea.Batch(cmd, tea.Tick(a.keys.Timeout, func(time.Time) tea.Msg { return keyTimeout{seq} }))
		case len(a.res.Next()) == 0:
			a.whichKey = false
		case !a.whichKey: // a pure prefix: which-key shows if nothing follows soon
			cmd = tea.Batch(cmd, tea.Tick(whichKeyDelay, func(time.Time) tea.Msg { return whichKeyDue{seq} }))
		}
		return a, cmd
	case keyTimeout:
		cmd := a.dispatch(a.res.Timeout(a.context(), msg.seq))
		a.whichKey = a.whichKey && len(a.res.Next()) > 0
		return a, cmd
	case whichKeyDue: // only opens it; key presses close it
		if msg.seq == a.res.Seq() && len(a.res.Next()) > 0 {
			a.whichKey = true
		}
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

// mode is derived from state, never stored (§3 principle 3).
// ponytail: only NORMAL and COMMAND exist until inputs (M1) and the console
// editor (M3) arrive.
func (a *App) mode() keymap.Mode {
	if a.cmdline != nil {
		return keymap.Command
	}
	return keymap.Normal
}

// context tells the keymap which scopes apply to the next key (§6.4).
func (a *App) context() keymap.Context {
	if a.mode() == keymap.Command {
		return keymap.Context{Overlay: "cmdline", Mode: keymap.Command}
	}
	scope := [...]string{KindSchema: "tree", KindData: "grid", KindConsole: "console"}[a.focused().Kind]
	return keymap.Context{Focus: []string{scope}, Pane: scope}
}

func (a *App) focused() *Pane {
	if a.win().Focus == a.win().Tree.ID {
		return a.win().Tree
	}
	for _, p := range a.win().Root.Leaves() {
		if p.ID == a.win().Focus {
			return p
		}
	}
	return a.win().Tree
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
		if *a.cmdline == "" {
			a.cmdline = nil
		} else {
			*a.cmdline = ui.DropLastGrapheme(*a.cmdline)
		}
	default:
		*a.cmdline += keymap.Text(k)
	}
	return nil
}

func (a *App) showToast(s string) tea.Cmd { return a.showToastFor(s, toastTTL) }

func (a *App) showToastFor(s string, ttl time.Duration) tea.Cmd {
	a.toast = s
	a.toastSeq++
	seq := a.toastSeq
	return tea.Tick(ttl, func(time.Time) tea.Msg { return toastExpired{seq} })
}

func (a *App) View() tea.View {
	v := tea.NewView(a.render().Render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}
