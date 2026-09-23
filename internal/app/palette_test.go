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

// The palette's own keys are not commands in it (§12).
func TestPaletteListsNoOverlayKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	for _, q := range []string{"关闭", "palette"} {
		feed(t, a, "<C-p>"+q)
		for _, r := range rowsOf(a) {
			if strings.HasPrefix(r, "palette.") && r != "palette.open=C-p" && r != "palette.command=:" {
				t.Errorf("%s lists %s", q, r)
			}
		}
		feed(t, a, "<Esc>")
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
	if rows := rowsOf(a); len(rows) != 1 || rows[0] != "pane.zoom=OFF" || a.paletteView().Enter[0].Label != "切换" {
		t.Fatalf("zoom: %v", rows)
	}
	feed(t, a, "<CR>")
	if a.palette == nil || rowsOf(a)[0] != "pane.zoom=ON" || a.win().Zoom == 0 {
		t.Fatalf("after ↵: palette %v rows %v zoom %d", a.palette, rowsOf(a), a.win().Zoom)
	}
	feed(t, a, "<Esc><C-p>split<Down><CR><C-p>")
	// the split also left the zoom: OFF; then the first window
	if rows := rowsOf(a); !strings.HasPrefix(rows[0], "pane.split.right=") || rows[1] != "pane.zoom=OFF" || rows[2] != "doraemon=" {
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

// namesOf is "tag:name" for each row the palette lists.
func namesOf(a *App) []string {
	var out []string
	for _, r := range a.paletteView().Rows {
		out = append(out, r.Tag+":"+r.Name)
	}
	return out
}

// Tab and S-Tab rewrite the input's prefix, keeping the query (§12).
func TestPaletteScopesCycle(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>ord")
	for _, want := range []string{"%ord", "@ord", ">ord", "ord"} {
		feed(t, a, "<Tab>")
		if got := a.palette.input.Text; got != want {
			t.Fatalf("Tab: %q, want %q", got, want)
		}
	}
	feed(t, a, "<S-Tab>")
	if in := a.palette.input; in.Text != ">ord" || in.Pos != len(">ord") {
		t.Fatalf("S-Tab wraps round: %+v", in)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "palette.scope 2"}).Min)
	if a.palette.input.Text != "@ord" {
		t.Errorf("clicking 表: %q", a.palette.input.Text)
	}
}

func TestPaletteScopesFilter(t *testing.T) {
	for in, want := range map[string]string{
		"@ord": "表:t_order 表:t_order_item",
		"%con": "Pane:⟨2⟩ console · console_1",
	} {
		a := sized(160, 45, "nerd")
		if feed(t, a, "<C-p>"+in); strings.Join(namesOf(a), " ") != want {
			t.Errorf("%s: %v", in, namesOf(a))
		}
	}
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>")
	all := strings.Join(namesOf(a), " ")
	for _, s := range []string{"窗口:0: data", "Pane:⟨1⟩ data · t_order", "表:t_user", "命令:左右分割"} {
		if !strings.Contains(all, s) {
			t.Errorf("所有 lacks %s", s)
		}
	}
	if !strings.HasPrefix(all, "窗口:0: data 窗口:1: report Pane:⟨0⟩ schema") {
		t.Errorf("windows, then panes, when nothing is typed: %.80s", all)
	}
}

// A table opens in the focused data pane, else the window's first (§12).
func TestPaletteOpensTables(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.focused()
	feed(t, a, "<C-p>@t_user<CR>")
	if a.palette != nil || strings.Join(data.Tabs, " ") != "t_user t_user" || data.Cur != 0 {
		t.Fatalf("↵: tabs %v cur %d", data.Tabs, data.Cur)
	}
	a.win().Focus = a.win().Tree.ID // from the sidebar: the first data pane
	feed(t, a, "<C-p>@t_sku<C-t>")
	if strings.Join(data.Tabs, " ") != "t_user t_user t_sku" || data.Cur != 2 || data.Prev != 0 {
		t.Fatalf("C-t: tabs %v cur %d prev %d", data.Tabs, data.Cur, data.Prev)
	}
	if feed(t, a, "<C-p>"); namesOf(a)[0] != "表:t_sku" {
		t.Errorf("recent first whatever the kind: %v", namesOf(a)[:2])
	}
	feed(t, a, "<Esc><C-p>%report<C-t>")
	if a.palette == nil {
		t.Error("C-t is for tables only")
	}
	feed(t, a, "<CR>")
	if a.palette != nil || a.sess.Active != 0 {
		t.Error("a window's ↵ only closes the palette until M5")
	}

	a = sized(160, 45, "nerd")
	data = a.focused()
	data.Tabs, data.Cur, data.Prev = nil, 0, -1 // an empty data pane
	feed(t, a, "<C-p>@t_user<CR>")
	if strings.Join(data.Tabs, " ") != "t_user" || data.Cur != 0 || data.Prev != -1 {
		t.Errorf("into an empty pane: tabs %v cur %d prev %d, want no previous tab", data.Tabs, data.Cur, data.Prev)
	}

	a = sized(160, 45, "nerd")
	feed(t, a, ":q<CR>:q<CR>") // no data pane left
	feed(t, a, "<C-p>@t_user<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || leaves[0].Kind != KindConsole || leaves[0].Object() != "console_1" {
		t.Errorf("with no data pane nothing opens: %v", leaves[0].Tabs)
	}
}

func TestPaletteFocusesPanes(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>%console<CR>")
	if a.palette != nil || a.win().Focus != 2 {
		t.Fatalf("focus %d, want the console", a.win().Focus)
	}
}
