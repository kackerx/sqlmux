package app

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"sqlmux/internal/config"
	"sqlmux/internal/keymap"
)

func TestMain(m *testing.M) {
	toastTTL, whichKeyDelay, quitWindow = time.Millisecond, time.Millisecond, time.Millisecond
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
	_, cmd := a.Update(teaKey("<C-c>"))
	a.Update(cmd()) // quitWindow passes: the toast goes
	if a.toast != "" {
		t.Fatal("the quit toast should last quitWindow")
	}
	if feed(t, a, "<C-c>") || a.toast == "" {
		t.Fatal("C-c after the toast is gone must count as a new first press")
	}
	if !feed(t, a, "<C-c>") {
		t.Fatal("…and the next one quits")
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
	for _, want := range []string{"e\u0301👍🏽", "e\u0301", ""} { // backspace takes whole clusters, as in nvim
		feed(t, a, "<BS>")
		if a.cmdline == nil || *a.cmdline != want {
			t.Fatalf("after <BS>: %v, want %q", a.cmdline, want)
		}
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

// due delivers the which-key tick the last key press asked for.
func due(a *App) { a.Update(whichKeyDue{a.res.Seq()}) }

func TestWhichKeyShowsAfterDelay(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	if a.whichKey {
		t.Fatal("which-key must wait for its delay")
	}
	due(a)
	if !a.whichKey {
		t.Fatal("which-key did not show")
	}
	var keys []string
	for _, it := range a.whichKeyOverlay().Items {
		keys = append(keys, it.Key)
		if it.Title == "" || strings.Contains(it.Title, ".") {
			t.Errorf("%s has no registry title: %q", it.Key, it.Title)
		}
	}
	// §6.8's SPC keys, in default.toml order
	if got := strings.Join(keys, " "); got != `s 0 1 2 3 4 5 6 7 8 9 c , & % " z x h j k l H J K L q b n` {
		t.Errorf("SPC items: %s", got)
	}
	if row := strings.Split(a.render().String(), "\n")[a.h-2]; !strings.HasPrefix(row, "└") {
		t.Errorf("the overlay should sit right above the status bar: %q", row)
	}
}

func TestWhichKeyNotForQuickKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	seq := a.res.Seq()
	feed(t, a, "s")
	a.Update(whichKeyDue{seq})
	if a.whichKey {
		t.Fatal("SPC s typed quickly must not show which-key")
	}
}

// Keys typed with the overlay up do what they do without it; esc closes it
// and clears the pending keys.
func TestWhichKeyKeys(t *testing.T) {
	c, err := config.Parse("[keys.normal]\n\"<Leader>:\" = \"cmdline.open\"")
	if err != nil {
		t.Fatal(err)
	}
	a := sizedWith(160, 45, c)
	feed(t, a, "<Space>")
	due(a)
	feed(t, a, ":")
	if a.whichKey || a.cmdline == nil {
		t.Fatal("SPC : through which-key should open the command line and close the overlay")
	}

	a = sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	due(a)
	feed(t, a, "<Esc>")
	if a.whichKey || len(a.res.Pending()) != 0 {
		t.Fatalf("esc: overlay %v, pending %v", a.whichKey, a.res.Pending())
	}
}

// Every default binding's action has a title, so which-key and the palette
// never show a raw ID.
func TestEveryDefaultActionHasATitle(t *testing.T) {
	keys, _ := keymap.New(config.Default())
	for _, id := range keys.Actions() {
		if actions[id].Title == "" {
			t.Errorf("%s has no title", id)
		}
	}
}

func titles(a *App) []string {
	var out []string
	for i, p := range a.win().Root.Leaves() {
		out = append(out, fmt.Sprintf("⟨%d⟩%s", i+1, p.Kind))
	}
	return out
}

func TestSplitAndClosePanes(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, `<Space>"`) // split ⟨1⟩ data below
	if got := strings.Join(titles(a), " "); got != "⟨1⟩data ⟨2⟩data ⟨3⟩console" {
		t.Fatalf("after SPC \": %s", got)
	}
	if p := a.focused(); len(p.Tabs) != 0 || a.win().Focus != 3 {
		t.Fatalf("the new, empty pane should have focus: %+v", p)
	}
	rects := a.layout()
	if top, bot := rects[1], rects[3]; top.Max.Y != bot.Min.Y || top.Dx() != bot.Dx() {
		t.Errorf("stacked halves: %v %v", top, bot)
	}
	feed(t, a, "<Space>%") // and the new one right
	if got := strings.Join(titles(a), " "); got != "⟨1⟩data ⟨2⟩data ⟨3⟩data ⟨4⟩console" {
		t.Fatalf("after SPC %%: %s", got)
	}

	feed(t, a, "<Space>x") // close the focused (newest) pane
	after := a.layout()
	if len(a.win().Root.Leaves()) != 3 || after[3] != rects[3] {
		t.Errorf("closing should give its sibling the whole space back: %v vs %v", after[3], rects[3])
	}
	feed(t, a, "<Space>x<Space>x<Space>x") // down to one pane, which stays
	if n := len(a.win().Root.Leaves()); n != 1 {
		t.Errorf("the last pane must stay, have %d", n)
	}
}

func TestFocusFollowsGeometry(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, `<Space>"`) // 1 over 3, console 2 on the right
	for _, c := range []struct {
		keys string
		want int
	}{
		{"<C-k>", 1},
		{"<C-l>", 2},
		{"<C-h>", 1}, // the closest on the left; ties go to the larger overlap
		{"<C-h>", 0}, // the sidebar
		{"<Space>l", 1},
		{"<C-j>", 3},
		{"<C-j>", 3}, // nothing below: stays
	} {
		feed(t, a, c.keys)
		if a.win().Focus != c.want {
			t.Fatalf("%s: focus %d, want %d", c.keys, a.win().Focus, c.want)
		}
	}
}

func TestZoom(t *testing.T) {
	a := sized(160, 45, "nerd")
	before := a.layout()
	feed(t, a, "<Space>z")
	if r := a.layout(); len(r) != 1 || r[1] != uv.Rect(0, 0, 160, 44) {
		t.Fatalf("zoomed layout: %v", r)
	}
	if strings.Contains(a.render().String(), "console") {
		t.Error("other panes must not be drawn while zoomed")
	}
	feed(t, a, "<Space>z")
	if !reflect.DeepEqual(a.layout(), before) {
		t.Error("SPC z again should restore the layout")
	}
}

func TestResizeKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	r0 := a.win().Root.Ratio
	feed(t, a, "<Space>L")
	if got := a.win().Root.Ratio; math.Abs(got-r0-resizeStep) > 1e-9 {
		t.Fatalf("SPC L: ratio %v, want %v", got, r0+resizeStep)
	}
	feed(t, a, "3<Space>H") // the count scales the step
	if got := a.win().Root.Ratio; math.Abs(got-r0+2*resizeStep) > 1e-9 {
		t.Fatalf("3 SPC H: ratio %v", got)
	}
	if a.layout()[1].Dx() >= sized(160, 45, "nerd").layout()[1].Dx() {
		t.Error("the data pane should have narrowed")
	}
}

func TestPaneNumbers(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>q")
	if !a.paneNumbers || !strings.Contains(a.render().String(), " 2 ") {
		t.Fatal("SPC q should show the numbers")
	}
	feed(t, a, "2")
	if a.paneNumbers || a.win().Focus != 2 {
		t.Fatalf("2 should jump to ⟨2⟩: focus %d", a.win().Focus)
	}
	feed(t, a, "<Space>q0")
	if a.win().Focus != 0 {
		t.Fatal("0 is the sidebar")
	}
	feed(t, a, "<Space>qj")
	if a.paneNumbers || a.win().Focus != 0 {
		t.Fatal("a non-digit just closes the numbers")
	}
	feed(t, a, "<Space>q9")
	if a.win().Focus != 0 {
		t.Fatal("no ⟨9⟩: nothing moves")
	}
}

func TestFoldSidebar(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.win().Focus = 0
	feed(t, a, "<Space>b")
	if r := a.layout()[0]; r.Dx() != 3 || a.win().Focus == 0 {
		t.Fatalf("folded: bar %v, focus %d", r, a.win().Focus)
	}
	col := func(y int) string { return string([]rune(strings.Split(a.render().String(), "\n")[y])[1]) }
	var down string
	for y := 1; y <= 15; y++ {
		down += col(y)
	}
	if down != "»schema · SPC b" {
		t.Errorf("thin bar reads %q", down)
	}
	feed(t, a, "<C-h><C-h>")
	if a.win().Focus == 0 {
		t.Error("a folded sidebar takes no focus")
	}
	feed(t, a, "<Space>b")
	if r := a.layout()[0]; r.Dx() != 32 {
		t.Errorf("unfolded width %d", r.Dx())
	}
}

// Closing a zoomed pane (here with :q) ends the zoom: the zoom must never
// point at a pane that is gone.
func TestCloseZoomedPane(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>z:q<CR>:q<CR>")
	if a.win().Zoom != 0 || len(a.layout()) != 2 || !strings.Contains(a.render().String(), "console") {
		t.Fatalf("zoom %d, layout %v", a.win().Zoom, a.layout())
	}
}

// Split IDs are never handed out twice, even after the highest is closed.
func TestSplitIDsStayUnique(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>%")
	first := a.win().Focus
	feed(t, a, "<Space>x<Space>%")
	if a.win().Focus == first {
		t.Fatalf("pane ID %d reused", first)
	}
}

// quitOnMode stops the program once the mode report Init sends comes back.
type quitOnMode struct{ *App }

func (q quitOnMode) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.ModeReportMsg); ok {
		return q, tea.Quit
	}
	q.App.Update(msg)
	return q, nil
}

// Init's borrowed mode report must reach the renderer: with no terminal
// answering 2027, the output still switches to grapheme widths (§7.1).
func TestRendererUsesGraphemeWidths(t *testing.T) {
	var out bytes.Buffer
	c := config.Default()
	keys, _ := keymap.New(c)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second) // Init stopped sending it
	defer cancel()
	p := tea.NewProgram(quitOnMode{New(c, keys)}, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(&out),
		tea.WithWindowSize(80, 24), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), ansi.SetModeUnicodeCore) {
		t.Fatal("the renderer never switched to grapheme widths")
	}
}
