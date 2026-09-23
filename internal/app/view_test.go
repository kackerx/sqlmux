package app

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
	a := sized(160, 45, "nerd")
	f, rects := a.render(), a.layout()
	th := ui.TokyonightStorm
	if th.Focus != lipgloss.Color("#9ece6a") {
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
	top := strings.Split(sized(160, 45, "nerd").render().String(), "\n")[0]
	for _, want := range []string{"console · cons…", "doraemon.public ▾", " ▶ run  ↵ ─┐"} {
		if !strings.Contains(top, want) {
			t.Errorf("top row lacks %q: %q", want, top)
		}
	}
}

// §7.8: sidebar 32 cols (24 below 100), data : console = 5 : 4, 1-col gaps.
func TestLayoutSizes(t *testing.T) {
	for _, c := range []struct{ w, side, data, cons int }{
		{160, 32, 70, 56},
		{100, 32, 37, 29},
		{99, 24, 41, 32},
		{80, 24, 30, 24},
	} {
		r := sized(c.w, 45, "nerd").layout()
		side, data, cons := r[0], r[1], r[2]
		if side.Min.X != 0 || side.Dx() != c.side || data.Min.X != side.Max.X+1 ||
			cons.Min.X != data.Max.X+1 || cons.Max.X != c.w || data.Dx() != c.data || cons.Dx() != c.cons {
			t.Errorf("w=%d: side %v data %v console %v; want widths %d/%d/%d", c.w, side, data, cons, c.side, c.data, c.cons)
		}
		if side.Dy() != 44 || data.Dy() != 44 || cons.Dy() != 44 {
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
		{160, "", "│ j/k move  ↵ open  t tab"},
		{160, `"<CR>" = ""`, "│ j/k move  t tab"},
		{160, `"j" = ""`, "│ ↵ open  t tab"},
		{80, "", "│ j/k move  ↵ open"}, // "t tab" doesn't fit whole
	} {
		cfg, err := config.Parse("[keys.tree]\n" + c.bind)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimRight(row(sizedWith(c.w, 45, cfg)), " │"); got != c.want {
			t.Errorf("w=%d %s: %q, want %q", c.w, c.bind, got, c.want)
		}
	}
}

// §F0.4: the window's only pane, once its last tab is closed, is empty: no
// placeholder text, title "⟨n⟩ <icon> type", and a tab bar holding just a
// clickable +.
func TestEmptyPane(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, ":q<CR>:q<CR>:q<CR>") // both data tabs, then the console's
	p := a.win.Root.Leaves()[0]
	if len(a.win.Root.Leaves()) != 1 || len(p.Tabs) != 0 {
		t.Fatalf("expected one empty pane, got %v", a.win.Root.Leaves())
	}
	f, r := a.render(), a.layout()[p.ID]
	lines := strings.Split(f.String(), "\n")
	cells := func(y int) string { return strings.TrimSpace(string([]rune(lines[y])[r.Min.X+1 : r.Max.X-1])) }
	if top := string([]rune(lines[r.Min.Y])[r.Min.X:r.Max.X]); !strings.HasPrefix(top, "┌─ ⟨1⟩ "+ui.NerdIcons.Console+" console ─") {
		t.Errorf("title: %q", top)
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
		plus = plus || h.Target.Kind == ui.KindTab && h.Target.Pane == p.ID && h.Target.I == -1
	}
	if !plus {
		t.Error("the + has no hit region")
	}
}
