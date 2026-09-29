package app

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// helpItems are the ? help's items as "key title [table]".
func helpItems(a *App) []string {
	var out []string
	for _, it := range a.keyHelpView().Items {
		out = append(out, it.Key+" "+it.Title+" ["+it.Group+"]")
	}
	return out
}

// ? lists the grid's keys by the table each comes from, user maps first;
// a prefix goes down a level and BS back; a key listed closes it and runs
// as typed there; esc, C-c and a global key close it (§6.5).
func TestKeyHelp(t *testing.T) {
	a := configured(t, 160, 45, "[map.grid.normal]\nJ = \"5j\"")
	tab := loadOrders(t, a, 20)
	feed(t, a, "?")
	items := helpItems(a)
	for _, want := range []string{"J 5j [map.grid.normal]", "j " + title("grid.down") + " [keys.grid]", "g … [keys.grid]", "C-h " + title("pane.focus.left") + " [keys.normal]", "C-p 命令面板 [keys.global]"} {
		if !slices.Contains(items, want) {
			t.Errorf("lacks %q: %v", want, items)
		}
	}
	if a.mode() != keymap.Command || !strings.Contains(a.render().String(), "[map.grid.normal]") {
		t.Fatalf("COMMAND, headings drawn: %v", a.mode())
	}
	if feed(t, a, "g"); a.keyHelpView().Prefix != "g" || !slices.Contains(helpItems(a), "o "+title("grid.order")+" [keys.grid]") || !slices.Contains(helpItems(a), "t "+title("tab.next")+" [keys.normal]") {
		t.Fatalf("g: %v", helpItems(a))
	}
	if feed(t, a, "<BS>"); a.keyHelpView().Prefix != "?" {
		t.Fatal("BS: back to the top")
	}
	if feed(t, a, "Z"); a.keyHelp == nil {
		t.Fatal("a key it doesn't list does nothing")
	}
	if feed(t, a, "J"); a.keyHelp != nil || tab.row != 5 {
		t.Fatalf("J runs the mapping: row %d", tab.row)
	}
	if feed(t, a, "?go"); a.keyHelp != nil || a.drop == nil {
		t.Fatal("g o opens ORDER")
	}
	feed(t, a, "<Esc>?<Esc>")
	if a.keyHelp != nil {
		t.Fatal("esc closes it")
	}
	if feed(t, a, "?<C-c>"); a.keyHelp != nil || a.toast != "" {
		t.Fatalf("C-c is esc: toast %q", a.toast)
	}
	if feed(t, a, "?<C-p>"); a.keyHelp != nil || a.palette == nil {
		t.Fatal("C-p opens the palette over it")
	}
}

// ? opens it in every pane in NORMAL, a console's too, whose VISUAL ?
// stays vim's backward search; <leader>? anywhere.
func TestKeyHelpWhere(t *testing.T) {
	a := inTree(160, 45)
	if feed(t, a, "?"); a.keyHelp == nil || !slices.Contains(helpItems(a), "t "+title("tree.open.tab")+" [keys.tree]") {
		t.Fatalf("tree: %v", helpItems(a))
	}
	a, c := inConsole(t, "")
	if feed(t, a, "?"); a.keyHelp == nil || !slices.Contains(helpItems(a), "↵ "+title("console.run")+" [keys.console]") {
		t.Fatalf("a console's ? in NORMAL: %v", helpItems(a))
	}
	if feed(t, a, "<Esc>v?"); a.keyHelp != nil || c.ed.Mode() != editor.Command {
		t.Fatal("a console's ? in VISUAL is vim's")
	}
	feed(t, a, "<Esc><Esc><Space>?")
	if a.keyHelp == nil || !slices.Contains(helpItems(a), "↵ "+title("console.run")+" [keys.console]") {
		t.Fatalf("SPC ? in a console: %v", helpItems(a))
	}
	feed(t, a, "<Esc><Space>x") // the console goes: ② is a landing pane
	if feed(t, a, "<C-l>?"); a.keyHelp == nil || !slices.Contains(helpItems(a), "c "+title("console.new")+" [keys.landing]") {
		t.Errorf("landing: %v", helpItems(a))
	}
}

// Too tall for the window, C-d and C-u scroll half of what shows, the
// wheel a row; a click on an item presses it, outside closes (§6.5).
func TestKeyHelpScrollAndMouse(t *testing.T) {
	a := sized(60, 12, "nerd")
	tab := loadOrders(t, a, 20)
	feed(t, a, "?")
	_, shown := a.keyHelpView().Fit(a.overlayArea(), 0)
	if feed(t, a, "<C-d>"); a.keyHelp.top != shown/2 {
		t.Fatalf("C-d: top %d of %d shown", a.keyHelp.top, shown)
	}
	a.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if a.keyHelp.top != shown/2+1 {
		t.Fatalf("the wheel: top %d", a.keyHelp.top)
	}
	for range 20 {
		feed(t, a, "<C-d>")
	}
	if top, _ := a.keyHelpView().Fit(a.overlayArea(), 99); a.keyHelp.top != top {
		t.Fatalf("no further than the end: %d", a.keyHelp.top)
	}
	for range 20 {
		feed(t, a, "<C-u>")
	}
	if a.keyHelp.top != 0 {
		t.Fatalf("C-u back to the top: %d", a.keyHelp.top)
	}
	i := slices.Index(helpItems(a), "j "+title("grid.down")+" [keys.grid]")
	click(a, find(t, a, ui.Target{Kind: ui.KindItem, I: i}).Min)
	if a.keyHelp != nil || tab.row != 1 {
		t.Fatalf("a click on j: row %d", tab.row)
	}
	feed(t, a, "?")
	if click(a, uv.Pos(2, 1)); a.keyHelp == nil { // its first heading
		t.Fatal("a click in it off an item does nothing")
	}
	click(a, uv.Pos(0, a.h-1))
	if a.keyHelp != nil {
		t.Error("a click outside closes it")
	}
}

// A Ctrl leader works in the help too: <C-a>? goes back to its top, not
// to a help of the help's own keys; <C-a> alone shows which-key over it.
func TestKeyHelpCtrlLeader(t *testing.T) {
	a := configured(t, 160, 45, "[keys]\nleader = \"<C-a>\"")
	loadOrders(t, a, 3)
	feed(t, a, "?g<C-a>?")
	if h := a.keyHelp; h == nil || h.prefix != nil || !slices.Contains(helpItems(a), "j "+title("grid.down")+" [keys.grid]") {
		t.Fatalf("C-a ?: %v", helpItems(a))
	}
	feed(t, a, "<C-a>")
	due(a)
	if f := a.render().String(); !a.whichKey || !strings.Contains(f, "C-a") || !strings.Contains(f, title("session.list")) {
		t.Errorf("which-key over the help:\n%s", f)
	}
	feed(t, a, "<Esc><Esc>/<C-a>?") // over a WHERE: its keys have titles too (§6.7「标题」)
	for _, it := range a.keyHelpView().Items {
		if strings.Contains(it.Title, ".") {
			t.Errorf("a raw ID: %+v", it)
		}
	}
}

// The top level is named by the key that opens the help there (§6.5).
func TestKeyHelpTitle(t *testing.T) {
	a := configured(t, 160, 45, "[keys.grid]\n\"?\" = \"\"\n\"g?\" = \"keyhelp.open\"")
	loadOrders(t, a, 3)
	if feed(t, a, "g?"); a.keyHelp == nil || a.keyHelpView().Prefix != "g?" {
		t.Fatalf("g?: %+v", a.keyHelp)
	}
	a, c := inConsole(t, "")
	if feed(t, a, "<Space>?"); a.keyHelp == nil || a.keyHelpView().Prefix != "?" || c == nil {
		t.Errorf("a console's: %q", a.keyHelpView().Prefix)
	}
}

// The ? help over a grid: its tables' headings in scope order, columns
// under each, resting on the status bar (§6.5).
func TestGoldenKeyHelp160x45(t *testing.T) {
	a := configured(t, 160, 45, "[map.grid.normal]\nJ = \"5j\"")
	loadOrders(t, a, 20)
	feed(t, a, "?")
	golden.RequireEqual(t, a.render().String())
}
