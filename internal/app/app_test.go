package app

import (
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
)

func TestMain(m *testing.M) {
	toastTTL = time.Millisecond
	os.Exit(m.Run())
}

func sized(w, h int, icons string) *App {
	c := config.Default()
	c.Icons = icons
	return sizedWith(w, h, c)
}

func sizedWith(w, h int, c *config.Config) *App {
	keys, _ := keymap.New(c)
	a := New(c, keys)
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// teaKey turns a key in vim notation back into the press a terminal sends.
func teaKey(k keymap.Key) tea.KeyPressMsg {
	switch k {
	case "<CR>":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "<Esc>":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "<BS>":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "<Space>":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if s := string(k); len(s) == 5 && s[:3] == "<C-" {
		return tea.KeyPressMsg{Code: rune(s[3]), Mod: tea.ModCtrl}
	}
	r := []rune(keymap.Text(k))[0]
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// feed presses keys written in vim notation and reports whether any of them
// quit the program.
func feed(t *testing.T, a *App, s string) (quit bool) {
	t.Helper()
	ks, err := keymap.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ks {
		_, cmd := a.Update(teaKey(k))
		quit = quits(cmd) || quit
	}
	return quit
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if quits(c) {
				return true
			}
		}
	}
	return false
}

func TestQuitCommand(t *testing.T) {
	if !feed(t, sized(160, 45, "nerd"), ":qa<CR>") {
		t.Fatal(":qa did not quit")
	}
	a := sized(160, 45, "nerd")
	if feed(t, a, ":foo<CR>") || a.toast != "未知命令: foo" {
		t.Fatalf("unknown command: toast %q", a.toast)
	}
}

func TestCmdlineCloses(t *testing.T) {
	for _, in := range []string{":ab<Esc>", ":a<BS><BS>"} {
		a := sized(160, 45, "nerd")
		feed(t, a, in)
		if a.cmdline != nil || a.mode() != keymap.Normal {
			t.Errorf("%q: cmdline still open", in)
		}
	}
	a := sized(160, 45, "nerd")
	feed(t, a, ":q a")
	if a.mode() != keymap.Command || *a.cmdline != "q a" {
		t.Fatalf("cmdline = %v", a.cmdline)
	}
}

func TestDoubleCtrlCQuits(t *testing.T) {
	a := sized(160, 45, "nerd")
	if feed(t, a, "<C-c>") || a.toast != "再按一次 C-c 退出" {
		t.Fatalf("first C-c: toast %q", a.toast)
	}
	if !feed(t, a, "<C-c>") {
		t.Fatal("second C-c within 2s did not quit")
	}

	a = sized(160, 45, "nerd")
	feed(t, a, "<C-c>")
	a.quitArmed = a.quitArmed.Add(-quitWindow - time.Millisecond)
	if feed(t, a, "<C-c>") {
		t.Fatal("C-c after more than 2s quit")
	}
	if !feed(t, a, "<C-c>") {
		t.Fatal("the late C-c should count as a new first press")
	}
}

func TestCloseTab(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.focused()
	feed(t, a, ":q<CR>")
	if !reflect.DeepEqual(data.Tabs, []string{"t_user"}) || data.Cur != 0 {
		t.Fatalf("after :q: tabs %v cur %d", data.Tabs, data.Cur)
	}
	feed(t, a, ":q<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || leaves[0].Kind != KindConsole || a.win().Focus != leaves[0].ID {
		t.Fatalf("closing the last tab should close the pane and focus the console: %v focus %d", leaves, a.win().Focus)
	}
	feed(t, a, ":q<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || len(leaves[0].Tabs) != 0 {
		t.Fatalf("the only pane stays, empty: %v", leaves)
	}
	a.win().Focus = a.win().Tree.ID
	feed(t, a, ":q<CR>") // the sidebar never closes
	if a.win().Focus != a.win().Tree.ID {
		t.Fatal("focus moved")
	}
	a.View()
}

// Every key goes through an action: running the action directly gives the
// same state as pressing its keys.
func TestKeysRunActions(t *testing.T) {
	pressed, direct := sized(160, 45, "nerd"), sized(160, 45, "nerd")
	feed(t, pressed, ":")
	direct.run("cmdline.open", 0)
	if !reflect.DeepEqual(pressed.cmdline, direct.cmdline) {
		t.Error(": and cmdline.open differ")
	}

	pressed, direct = sized(160, 45, "nerd"), sized(160, 45, "nerd")
	feed(t, pressed, ":q<CR>")
	direct.run("tab.close", 0)
	if !reflect.DeepEqual(pressed.win(), direct.win()) {
		t.Error(":q and tab.close differ")
	}

	if !quits(sized(160, 45, "nerd").run("quit", 0)) {
		t.Error("quit did not quit")
	}
}

// With the cmdline open, keys go to it, not to the focused pane's scope: ↵
// runs the command instead of console.run.
func TestCmdlineShadowsPaneKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.win().Focus = 2 // console
	if ctx := a.context(); ctx.Mode != keymap.Normal || ctx.Focus[0] != "console" {
		t.Fatalf("console context %+v", ctx)
	}
	feed(t, a, ":")
	if ctx := a.context(); ctx.Overlay != "cmdline" || ctx.Focus != nil || ctx.Mode != keymap.Command {
		t.Fatalf("cmdline context %+v", ctx)
	}
	if !feed(t, a, "qa<CR>") {
		t.Fatal(":qa typed over the console did not quit")
	}
	a = sized(160, 45, "nerd")
	if feed(t, a, ":<C-c>"); a.cmdline != nil || a.toast != "" {
		t.Fatal("C-c should just leave the command line")
	}
}

// An ambiguous binding fires when its timeoutlen tick comes back.
func TestAmbiguousKeyTimesOut(t *testing.T) {
	c := config.Default()
	c.Bindings = []config.Binding{{Table: "keys.normal", Key: "g", Value: "cmdline.open"}}
	a := sizedWith(160, 45, c)
	if _, cmd := a.Update(teaKey("g")); cmd == nil || a.cmdline != nil {
		t.Fatal("g should wait for timeoutlen")
	}
	a.Update(keyTimeout{a.res.Seq() - 1}) // stale
	if a.cmdline != nil {
		t.Fatal("a stale timeout fired")
	}
	a.Update(keyTimeout{a.res.Seq()})
	if a.cmdline == nil {
		t.Fatal("the timeout did not run g")
	}
}

func TestResizeNoPanic(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-c>:x")
	for _, s := range [][2]int{{100, 30}, {160, 45}, {80, 24}, {1, 1}, {0, 0}} {
		a.Update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		a.View()
	}
}

// Typed text reaches the command line whole: multi-rune clusters are one key.
func TestCmdlineTakesGraphemes(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, ":")
	for _, s := range []string{"e\u0301", "👍🏽", "🇨🇳"} {
		a.Update(tea.KeyPressMsg{Code: tea.KeyExtended, Text: s})
	}
	if *a.cmdline != "e\u0301👍🏽🇨🇳" {
		t.Fatalf("cmdline = %q", *a.cmdline)
	}
}

// The quit toast names whatever key cancel is bound to (§6.7).
func TestQuitToastFollowsKeymap(t *testing.T) {
	c, err := config.Parse("[keys.global]\n\"<C-c>\" = \"\"\n\"<C-q>\" = \"cancel\"")
	if err != nil {
		t.Fatal(err)
	}
	a := sizedWith(160, 45, c)
	if feed(t, a, "<C-q>") || a.toast != "再按一次 C-q 退出" {
		t.Fatalf("toast %q", a.toast)
	}
	if !feed(t, a, "<C-q>") {
		t.Fatal("second C-q did not quit")
	}
}

func TestCloseTabPicksNext(t *testing.T) {
	for _, c := range []struct {
		tabs      []string
		cur, prev int
		want      []string
		wantCur   int
	}{
		{[]string{"a", "b", "c"}, 0, 2, []string{"b", "c"}, 1},  // prev after closed shifts left
		{[]string{"a", "b", "c"}, 2, 0, []string{"a", "b"}, 0},  // prev before closed stays
		{[]string{"a", "b", "c"}, 1, -1, []string{"a", "c"}, 1}, // no prev: the tab that slid in
		{[]string{"a", "b", "c"}, 2, -1, []string{"a", "b"}, 1}, // no prev, last one: its left neighbour
		{[]string{"a", "b"}, 1, 1, []string{"a"}, 0},            // prev == closed counts as none
	} {
		a := sized(160, 45, "nerd")
		p := a.focused()
		p.Tabs, p.Cur, p.Prev = slices.Clone(c.tabs), c.cur, c.prev
		a.closeTab()
		if !slices.Equal(p.Tabs, c.want) || p.Cur != c.wantCur || p.Prev != -1 {
			t.Errorf("%v cur %d prev %d: got %v cur %d prev %d; want %v cur %d", c.tabs, c.cur, c.prev, p.Tabs, p.Cur, p.Prev, c.want, c.wantCur)
		}
	}
}
