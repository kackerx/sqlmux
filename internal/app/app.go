// Package app holds the single state tree and its Update/View (tech-design §3).
package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/ui"
)

var toastTTL = 3 * time.Second

type App struct {
	w, h  int
	theme *ui.Theme

	cmdline *string // non-nil while the : command line is open (COMMAND mode)

	toast    string
	toastSeq int
}

type toastExpired struct{ seq int }

func New() *App { return &App{theme: ui.TokyonightStorm} }

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
		return a, a.key(msg)
	}
	return a, nil
}

func (a *App) key(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		a.cmdline = nil
		return a.showToast("输入 :qa 退出")
	}
	if a.cmdline != nil {
		return a.cmdlineKey(k)
	}
	if k.String() == ":" {
		s := ""
		a.cmdline = &s
	}
	return nil
}

func (a *App) cmdlineKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.Code {
	case tea.KeyEscape:
		a.cmdline = nil
	case tea.KeyEnter:
		cmd := *a.cmdline
		a.cmdline = nil
		return a.exec(cmd)
	case tea.KeyBackspace:
		r := []rune(*a.cmdline)
		if len(r) == 0 {
			a.cmdline = nil
		} else {
			*a.cmdline = string(r[:len(r)-1])
		}
	default:
		*a.cmdline += k.Text
	}
	return nil
}

func (a *App) exec(cmd string) tea.Cmd {
	switch cmd {
	case "":
		return nil
	case "qa", "qa!", "qall":
		return tea.Quit
	}
	return a.showToast("未知命令: " + cmd)
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

func (a *App) render() *ui.Frame {
	th := a.theme
	f := ui.NewFrame(a.w, a.h, th)
	base := uv.Style{Fg: th.Fg, Bg: th.Bg}
	f.Fill(f.Bounds(), base)
	f.Text(1, 0, a.w, "sqlmux", base)

	y := a.h - 1
	if a.cmdline != nil {
		f.Text(0, y, a.w, ":"+*a.cmdline, base)
	} else if a.toast != "" {
		w := ui.Width(a.toast)
		f.Text(max(a.w-w-1, 0), y, a.w, a.toast, uv.Style{Fg: th.Warn, Bg: th.Bg})
	}
	return f
}
