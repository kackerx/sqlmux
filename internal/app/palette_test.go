package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/ui"
)

// rowsOf is "id=right" for each row the palette lists.
func rowsOf(a *App) []string {
	var out []string
	for _, r := range a.paletteView().Rows {
		out = append(out, r.Where+"="+r.Right)
	}
	return out
}

func TestPaletteOpensAndCloses(t *testing.T) {
	a := sized(160, 45, "nerd")
	before := a.render()
	feed(t, a, "<C-p>")
	f := a.render()
	if !strings.Contains(f.String(), "命令面板") || f.Cursor == nil {
		t.Fatalf("no palette or no cursor:\n%s", f.String())
	}
	if f.Buf.CellAt(1, 1).Style.Bg == before.Buf.CellAt(1, 1).Style.Bg {
		t.Error("the backdrop should dim what is behind (§7.5)")
	}
	feed(t, a, "<Esc>")
	if after := a.render(); after.Render() != before.Render() || after.Cursor != nil {
		t.Error("closing should give the screen back as it was")
	}
}

// Titles are Chinese; the action id after them is what `split` finds (§12).
func TestPaletteFindsByID(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>split")
	if got := strings.Join(rowsOf(a), " "); got != `pane.split.below=SPC " pane.split.right=SPC %` {
		t.Fatalf("split: %s", got)
	}
	panes := len(a.win().Root.Leaves())
	feed(t, a, "<CR>")
	if a.palette != nil || len(a.win().Root.Leaves()) != panes+1 {
		t.Fatal("↵ should split and close the palette")
	}

	a = sized(160, 45, "nerd")
	feed(t, a, "<C-p>resize")
	if got := strings.Join(rowsOf(a), " "); strings.Count(got, "pane.resize.") != 4 || strings.Count(got, "= ") != 3 || !strings.HasSuffix(got, "=") {
		t.Fatalf("resize has no default keys: %s", got)
	}
	r0 := a.win().Root.Ratio
	feed(t, a, " left<CR>")
	if a.win().Root.Ratio >= r0 {
		t.Error("the unbound resize ran from the palette")
	}

	a = sized(160, 45, "nerd")
	if feed(t, a, "<C-p>Split"); len(rowsOf(a)) != 0 {
		t.Errorf("smartcase: an upper-case letter matches case: %v", rowsOf(a))
	}
}

func TestPaletteHighlightsMatches(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>split")
	f := a.render()
	for y, line := range strings.Split(f.String(), "\n") {
		if i := strings.Index(line, "pane.split.right"); i >= 0 {
			x := ui.Width(line[:i]) + len("pane.")
			if f.Buf.CellAt(x, y).Style.Bg != a.theme.Warn || f.Buf.CellAt(x-1, y).Style.Bg == a.theme.Warn {
				t.Errorf("only the matched s should light up: %v %v", f.Buf.CellAt(x-1, y).Style, f.Buf.CellAt(x, y).Style)
			}
			return
		}
	}
	t.Fatal("no pane.split.right row")
}

// : opens the command scope, where an ex alias typed whole comes first (§12).
func TestPaletteExAliases(t *testing.T) {
	for in, want := range map[string]string{":q": "tab.close", ":qa": "quit", ":w": "save"} {
		a := sized(160, 45, "nerd")
		feed(t, a, in)
		if rows := rowsOf(a); len(rows) == 0 || !strings.HasPrefix(rows[0], want+"=") {
			t.Errorf("%s: first %v, want %s", in, rows, want)
		}
	}
	a := sized(160, 45, "nerd")
	if feed(t, a, "<C-p>qa"); len(rowsOf(a)) > 0 && strings.HasPrefix(rowsOf(a)[0], "quit=") {
		t.Error("outside the command scope, qa is just fuzzy text")
	}
}

// A toggle shows ON / OFF and leaves the palette open; recent commands come
// first when nothing is typed.
func TestPaletteTogglesAndRecent(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>zoom")
	if rows := rowsOf(a); len(rows) != 1 || rows[0] != "pane.zoom=OFF" || a.paletteView().Enter.Label != "切换" {
		t.Fatalf("zoom: %v", rows)
	}
	feed(t, a, "<CR>")
	if a.palette == nil || rowsOf(a)[0] != "pane.zoom=ON" || a.win().Zoom == 0 {
		t.Fatalf("after ↵: palette %v rows %v zoom %d", a.palette, rowsOf(a), a.win().Zoom)
	}
	feed(t, a, "<Esc><C-p>split<Down><CR><C-p>")
	// the split also left the zoom: OFF
	if rows := rowsOf(a); !strings.HasPrefix(rows[0], "pane.split.right=") || rows[1] != "pane.zoom=OFF" || !strings.HasPrefix(rows[2], "cancel=") {
		t.Errorf("recent first, then by id: %v", rows[:3])
	}
}

// Running a toggle far down the list moves it up among the recent ones; the
// list follows it rather than leaving the selection out of sight.
func TestPaletteToggleKeepsSelectionShown(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>")
	for rowsOf(a)[a.palette.sel] != "pane.zoom=OFF" {
		feed(t, a, "<Down>")
	}
	if a.palette.top == 0 {
		t.Fatal("pane.zoom should be past the first page")
	}
	feed(t, a, "<CR>")
	if p := a.palette; p.sel != 0 || p.top != 0 {
		t.Errorf("after ↵: sel %d top %d, want pane.zoom on top and shown", p.sel, p.top)
	}
}

func TestPaletteMoves(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>"+strings.Repeat("<Down>", 12))
	if p := a.palette; p.sel != 12 || p.top != 3 {
		t.Fatalf("12 down: sel %d top %d, want the list scrolled to show it", p.sel, p.top)
	}
	feed(t, a, "<Up><C-p><C-n>")
	if p := a.palette; p.sel != 11 || p.top != 3 {
		t.Fatalf("up, up, down: sel %d top %d", p.sel, p.top)
	}
	feed(t, a, "x")
	if p := a.palette; p.sel != 0 || p.top != 0 {
		t.Error("typing starts over at the best match")
	}
	feed(t, a, strings.Repeat("<Up>", 3))
	if a.palette.sel != 0 {
		t.Error("the selection stops at the top")
	}
}

func TestPaletteMouse(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>split")
	r := find(t, a, ui.Target{Kind: ui.KindRow, I: 1})
	a.Update(tea.MouseMotionMsg{X: r.Min.X + 3, Y: r.Min.Y})
	if a.palette.sel != 1 {
		t.Fatalf("hover selects: sel %d", a.palette.sel)
	}
	panes := len(a.win().Root.Leaves())
	click(a, uv.Pos(r.Min.X+3, r.Min.Y))
	if a.palette != nil || len(a.win().Root.Leaves()) != panes+1 {
		t.Fatal("a click runs the row")
	}

	feed(t, a, "<C-p>")
	box, _ := ui.PaletteBox(a.window(), len(rowsOf(a)))
	click(a, uv.Pos(box.Min.X+3, box.Min.Y+1)) // the input row
	if a.palette == nil {
		t.Fatal("a click inside the box is not outside")
	}
	click(a, uv.Pos(1, 1))
	if a.palette != nil {
		t.Fatal("a click outside closes the palette")
	}
}

// The terminal's own cursor sits after what is typed (§12).
func TestPaletteCursor(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>ab<Left>")
	box, _ := ui.PaletteBox(a.window(), len(rowsOf(a)))
	if c := a.View().Cursor; c == nil || c.X != box.Min.X+2+1 || c.Y != box.Min.Y+1 {
		t.Fatalf("cursor %+v, box %v", c, box)
	}
	if feed(t, a, "<Esc>"); a.View().Cursor != nil {
		t.Error("no cursor with the palette closed")
	}
}
