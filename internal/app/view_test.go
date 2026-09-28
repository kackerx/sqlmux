package app

import (
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"sqlmux/internal/config"
	"sqlmux/internal/ui"
)

// Run `go test ./internal/app -update` to regenerate.
func TestGolden160x45(t *testing.T) {
	golden.RequireEqual(t, sized(160, 45, "nerd").render().String())
}

func TestGolden80x24ASCII(t *testing.T) {
	golden.RequireEqual(t, sized(80, 24, "ascii").render().String())
}

func TestFocusColors(t *testing.T) {
	a := twoPanes(160, 45, "nerd")
	f, rects := a.render(), a.layout()
	th := ui.TokyonightStorm
	if th.Focus != ansi.XParseColor("#9ece6a") {
		t.Fatalf("focus token = %v", th.Focus)
	}
	for id, want := range map[int][2]color.Color{
		0: {th.Border, th.Dim},  // sidebar
		1: {th.Focus, th.Focus}, // ⟨1⟩ data, focused
		2: {th.Border, th.Dim},  // ⟨2⟩ console
	} {
		r := rects[id]
		border := f.Buf.CellAt(r.Min.X, r.Min.Y).Style.Fg
		title := f.Buf.CellAt(r.Min.X+3, r.Min.Y).Style.Fg // "┌─ ⟨n⟩"
		if border != want[0] || title != want[1] {
			t.Errorf("pane %d: border %v title %v, want %v %v", id, border, title, want[0], want[1])
		}
	}
}

func privateUse(s string) rune {
	for _, r := range s {
		if r >= 0xE000 && r <= 0xF8FF {
			return r
		}
	}
	return 0
}

func TestASCIIIconsHaveNoNerdGlyphs(t *testing.T) {
	if privateUse(sized(160, 45, "nerd").render().String()) == 0 {
		t.Fatal("nerd frame has no private-use glyphs; the check below would prove nothing")
	}
	for _, s := range [][2]int{{160, 45}, {80, 24}} {
		if r := privateUse(sized(s[0], s[1], "ascii").render().String()); r != 0 {
			t.Fatalf("%dx%d ascii frame contains %U", s[0], s[1], r)
		}
	}
}

// Titles never overrun their box: every pane's top border keeps its ┐ at the
// right edge, whatever the size.
func TestSmallSizes(t *testing.T) {
	for w := 0; w <= 90; w += 3 {
		for h := 0; h <= 26; h += 2 {
			a := sized(w, h, "nerd")
			f := a.render()
			for id, r := range a.layout() {
				if r.Dx() < 2 || r.Dy() < 2 {
					continue
				}
				if c := f.Buf.CellAt(r.Max.X-1, r.Min.Y); c == nil || c.Content != "┐" {
					t.Fatalf("%dx%d pane %d: top-right is %v", w, h, id, c)
				}
			}
		}
	}
}

// §7.8: at 160×45 the console title keeps both the schema dropdown and
// "▶ run ↵", cutting the object name instead.
func TestConsoleTitleAt160(t *testing.T) {
	top := strings.Split(twoPanes(160, 45, "nerd").render().String(), "\n")[0]
	// no "console" beside its icon (§7.7), so the tab name fits whole
	for _, want := range []string{"② " + ui.NerdIcons.Console.Text + " console_1 ─", "doraemon.public ▾", " ▶ run  ↵ ─┐"} {
		if !strings.Contains(top, want) {
			t.Errorf("top row lacks %q: %q", want, top)
		}
	}
}

// §7.8: sidebar 32 cols (24 below 100), then a 1-col gap; the data pane
// takes the rest (§5).
func TestLayoutSizes(t *testing.T) {
	for _, c := range []struct{ w, side int }{{160, 32}, {100, 32}, {99, 24}, {80, 24}} {
		r := sized(c.w, 45, "nerd").layout()
		side, data := r[0], r[1]
		if len(r) != 2 || side.Min.X != 0 || side.Dx() != c.side || data.Min.X != side.Max.X+1 || data.Max.X != c.w {
			t.Errorf("w=%d: %v; want the sidebar %d wide, then data to the edge", c.w, r, c.side)
		}
		if side.Dy() != 44 || data.Dy() != 44 {
			t.Errorf("w=%d: panes must fill every row above the status bar", c.w)
		}
	}
}

// The sidebar's hint row drops unbound actions and items that don't fit
// whole (§6.7): no key-less "open", no dangling "t".
func TestSidebarHintRow(t *testing.T) {
	row := func(a *App) string {
		r := a.layout()[0]
		line := []rune(strings.Split(a.render().String(), "\n")[r.Max.Y-2]) // all 1-cell runes here
		return string(line[r.Min.X : r.Min.X+r.Dx()])
	}
	for _, c := range []struct {
		w    int
		bind string
		want string
	}{
		{160, "", "│ j/k move · ↵ open · t tab"},
		{160, `"<CR>" = ""`, "│ j/k move · t tab"},
		{160, `"j" = ""`, "│ ↵ open · t tab"},
		{80, "", "│ j/k move · ↵ open"}, // "t tab" doesn't fit whole
	} {
		if got := strings.TrimRight(row(configured(t, c.w, 45, "[keys.tree]\n"+c.bind)), " │"); got != c.want {
			t.Errorf("w=%d %s: %q, want %q", c.w, c.bind, got, c.want)
		}
	}
}

// The window's only pane, once its last tab is closed, is empty: no
// placeholder text, the title just "⟨n⟩ <icon>", and a tab bar holding just a
// clickable +.
func TestEmptyPane(t *testing.T) {
	a := twoPanes(160, 45, "nerd")
	feed(t, a, ":q<CR>:q<CR>:q<CR>") // both data tabs, then the console's
	p := a.win().Root.Leaves()[0]
	if len(a.win().Root.Leaves()) != 1 || len(p.Tabs) != 0 {
		t.Fatalf("expected one empty pane, got %v", a.win().Root.Leaves())
	}
	f, r := a.render(), a.layout()[p.ID]
	lines := strings.Split(f.String(), "\n")
	cells := func(y int) string { return strings.TrimSpace(ansi.Cut(lines[y], r.Min.X+1, r.Max.X-1)) } // by cells: the tree's 工作区 is wide
	if top := string([]rune(lines[r.Min.Y])[r.Min.X:r.Max.X]); !strings.HasPrefix(top, "┌─ ① "+ui.NerdIcons.Console.Text+" ─") || strings.Contains(top, "run") || strings.Contains(top, "▾") {
		t.Errorf("title, with no hints: %q", top)
	}
	for y := r.Min.Y + 1; y < r.Max.Y-2; y++ {
		if got := cells(y); got != "" {
			t.Fatalf("row %d is not empty: %q", y, got)
		}
	}
	if got := cells(r.Max.Y - 2); got != "+" {
		t.Errorf("tab bar: %q, want just +", got)
	}
	plus := false
	for _, h := range f.Hits {
		plus = plus || h.Target == ui.Target{Kind: ui.KindHint, Pane: p.ID, Action: "tab.new"}
	}
	if !plus {
		t.Error("the + has no hit region")
	}
}

func statusRow(a *App) string {
	return strings.Split(a.render().String(), "\n")[a.h-1]
}

func TestStatusModeBlock(t *testing.T) {
	a := sized(160, 45, "nerd")
	th := ui.TokyonightStorm
	modeBg := func() color.Color { return a.render().Buf.CellAt(a.w-2, a.h-1).Style.Bg }
	if !strings.HasSuffix(statusRow(a), " NORMAL ") || modeBg() != th.Focus {
		t.Errorf("NORMAL block: %q bg %v", statusRow(a), modeBg())
	}
	feed(t, a, ":")
	f := ui.NewFrame(a.w, 1, th) // the bar alone: over it the palette's backdrop dims everything
	a.statusLine().Draw(f, uv.Rect(0, 0, a.w, 1))
	if !strings.HasSuffix(f.String(), " COMMAND ") || f.Buf.CellAt(a.w-2, 0).Style.Bg != th.Info || !strings.Contains(f.String(), "doraemon") {
		t.Errorf("COMMAND block, and no command line: %q bg %v", f.String(), f.Buf.CellAt(a.w-2, 0).Style.Bg)
	}
}

func TestStatusPendingKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	pending := func() string {
		row := statusRow(a)
		i := strings.Index(row, ui.NerdIcons.Keys.Text)
		return strings.Fields(row[i+len(ui.NerdIcons.Keys.Text):])[0]
	}
	for _, c := range []struct{ in, want string }{
		{"", "·"},
		{"<Space>", "SPC"},
		{"s", "·"}, // SPC s completes (session.list comes later) and clears
		{"g", "g"},
		{"t", "·"},
		{"5", "5"},
		{"<Esc>", "·"},
	} {
		if c.in != "" {
			feed(t, a, c.in)
		}
		if got := pending(); got != c.want {
			t.Errorf("after %q: pending %q, want %q", c.in, got, c.want)
		}
	}
}

// The pending-keys block is at least 3 columns: typing SPC, g or a count
// leaves the C-p entry where it was (§7.8).
func TestStatusPendingKeepsPlace(t *testing.T) {
	cp := func(a *App) int {
		row := statusRow(a)
		return ui.Width(row[:strings.Index(row, ui.NerdIcons.Search.Text)])
	}
	idle := cp(sized(160, 45, "nerd"))
	for _, in := range []string{"<Space>", "g", "5", "12"} {
		a := sized(160, 45, "nerd")
		feed(t, a, in)
		if got := cp(a); got != idle {
			t.Errorf("%s: C-p at %d, idle at %d", in, got, idle)
		}
	}
}

// Whatever the width, the session block, the current window, the C-p entry,
// the pending keys and the mode block stay (§7.8).
func TestStatusNarrowing(t *testing.T) {
	for _, c := range []struct {
		w          int
		has, lacks []string
	}{
		{160, []string{"doraemon ▾", " 0: data* ", " 1: report ", " 1,1 ", "pg@localhost:5432", " NORMAL "}, nil},
		{78, []string{"doraemon ▾", " 1: report ", " 1,1 ", " NORMAL "}, []string{"pg@localhost"}},
		{55, []string{"doraemon ▾", " 0: data* ", " 1,1 "}, []string{" 1: report "}},
		{40, []string{" 0: data* ", "·", " NORMAL "}, []string{" 1,1 ", "doraemon"}},
	} {
		a := twoPanes(c.w, 24, "nerd")
		loadOrders(t, a, 3) // row,col shows with a table loaded (§7.8)
		row := statusRow(a)
		// the palette entry is its icon alone (§7.7)
		for _, s := range append(c.has, " 0: data* ", " "+ui.NerdIcons.Search.Text+" ", "·", " NORMAL ") {
			if !strings.Contains(row, s) {
				t.Errorf("w=%d lacks %q: %q", c.w, s, row)
			}
		}
		for _, s := range c.lacks {
			if strings.Contains(row, s) {
				t.Errorf("w=%d still has %q: %q", c.w, s, row)
			}
		}
	}
}

// A theme file changes only what it names; icons with a color keep it (§7.3, §7.7).
func TestThemeColors(t *testing.T) {
	th, ic, err := ui.ParseTheme(`
row = "#6c6a6d"
[icon]
console = { text = "C", fg = "#ff0000" }
table = { fg = "#a9dc76" }
`, ui.NerdIcons)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Theme, c.Icons = th, ic
	a := m0Layout(sizedWith(160, 45, c))
	f := a.render()
	lines := strings.Split(f.String(), "\n")
	// cell finds s on row y and returns its cell style
	cell := func(y int, s string) uv.Style {
		t.Helper()
		i := strings.Index(lines[y], s)
		if i < 0 {
			t.Fatalf("row %d has no %q: %s", y, s, lines[y])
		}
		return f.Buf.CellAt(len([]rune(lines[y][:i])), y).Style
	}
	if bg := f.Buf.CellAt(80, a.h-1).Style.Bg; bg != th.Bar || th.Bar == th.Row {
		t.Errorf("status bar %v, want bar %v whatever row is", bg, th.Bar)
	}
	if st := cell(0, " C console"); st.Fg != th.Dim {
		t.Fatal("the title around the icon keeps its color")
	}
	if st := cell(0, "C console"); st.Fg != ic.Console.Fg {
		t.Errorf("console icon %v, want %v", st.Fg, ic.Console.Fg)
	}
	if st := cell(7, ui.NerdIcons.Table.Text); st.Fg != ic.Table.Fg { // the first table, under doraemon, public and Tables
		t.Errorf("table icon %v, want %v", st.Fg, ic.Table.Fg)
	}
}
