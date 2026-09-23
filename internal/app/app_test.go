package app

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
)

// keys turns "a", "<CR>", "<Esc>", "<C-c>", "<BS>" into key presses.
func keys(ss ...string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, s := range ss {
		switch s {
		case "<CR>":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyEnter})
		case "<Esc>":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyEscape})
		case "<BS>":
			out = append(out, tea.KeyPressMsg{Code: tea.KeyBackspace})
		case "<C-c>":
			out = append(out, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		default:
			for _, r := range s {
				out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
	return out
}

// feed sends key presses and reports whether any produced tea.Quit.
func feed(a *App, ss ...string) (quit bool) {
	for _, k := range keys(ss...) {
		_, cmd := a.Update(k)
		if cmd != nil {
			if _, ok := cmd().(tea.QuitMsg); ok {
				quit = true
			}
		}
	}
	return quit
}

func sized(w, h int, icons string) *App {
	c := config.Default()
	c.Icons = icons
	keys, _ := keymap.New(c)
	a := New(c, keys)
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

func TestQuit(t *testing.T) {
	if !feed(sized(160, 45, "nerd"), ":qa", "<CR>") {
		t.Fatal(":qa did not quit")
	}
	if feed(sized(160, 45, "nerd"), ":q", "<BS>", "<Esc>", "qa", "<CR>") {
		t.Fatal("esc'd cmdline still quit")
	}
}

func TestCtrlCToasts(t *testing.T) {
	a := sized(160, 45, "nerd")
	if feed(a, "<C-c>") {
		t.Fatal("C-c quit")
	}
	if a.toast == "" {
		t.Fatal("no toast after C-c")
	}
	_, cmd := a.Update(toastExpired{a.toastSeq})
	if cmd != nil || a.toast != "" {
		t.Fatal("toast did not expire")
	}
}

func TestResizeNoPanic(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(a, "<C-c>")
	for _, s := range [][2]int{{100, 30}, {160, 45}, {80, 24}, {1, 1}, {0, 0}} {
		a.Update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		a.View()
	}
}

func TestMain(m *testing.M) {
	toastTTL = time.Millisecond
	os.Exit(m.Run())
}
