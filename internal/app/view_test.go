package app

import (
	"image/color"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/ui"
)

// Run `go test ./internal/app -update` to regenerate.
func TestGolden160x45(t *testing.T) {
	golden.RequireEqual(t, sized(160, 45, "nerd").render().String())
}

func TestGolden80x24ASCII(t *testing.T) {
	golden.RequireEqual(t, sized(80, 24, "ascii").render().String())
}

func TestFocusColors(t *testing.T) {
	a := twoPanes(160, 45)
	f, rects := a.render(), a.layout()
	th := ui.TokyonightStorm
	if th.Focus != ansi.XParseColor("#9ece6a") {
		t.Fatalf("focus token = %v", th.Focus)
	}
	for id, want := range map[int][2]color.Color{
		0: {th.Border, th.Dim},  // sidebar
		1: {th.Focus, th.Focus}, // ⟨1⟩ data, focused
		2: {th.Border, th.Dim},  // ⟨2⟩ query
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

// §7.8: at 160×45 the query title has "▶ run ↵", the connection and the
// schema; its name, cut, the rest (no "query" beside its icon, §7.7).
func TestQueryTitleAt160(t *testing.T) {
	top := strings.Split(twoPanes(160, 45).render().String(), "\n")[0]
	for _, want := range []string{"② " + ui.NerdIcons.Query.Text + " q…  Tx: 自动 ▾ doraemon · doraemon.public ▾  ▶ run  ↵ ─┐"} {
		if !strings.Contains(top, want) {
			t.Errorf("top row lacks %q: %q", want, top)
		}
	}
}

// §7.8: sidebar 32 cols (24 below 100), then a 1-col gap; data and the
// query share the rest at 1 : 1, a 1-col gap between them (§5, F8.9).
func TestLayoutSizes(t *testing.T) {
	for _, c := range []struct{ w, side int }{{160, 32}, {100, 32}, {99, 24}, {80, 24}} {
		r := sized(c.w, 45, "nerd").layout()
		side, data, cons := r[0], r[1], r[2]
		if len(r) != 3 || side.Min.X != 0 || side.Dx() != c.side || data.Min.X != side.Max.X+1 || cons.Min.X != data.Max.X+1 || cons.Max.X != c.w {
			t.Errorf("w=%d: %v; want the sidebar %d wide, then data and the query to the edge", c.w, r, c.side)
		}
		if d := data.Dx() - cons.Dx(); d < -1 || d > 1 {
			t.Errorf("w=%d: data %d, query %d wide, not even", c.w, data.Dx(), cons.Dx())
		}
		if side.Dy() != 44 || data.Dy() != 44 || cons.Dy() != 44 {
			t.Errorf("w=%d: panes must fill every row above the status bar", c.w)
		}
	}
}

// The sidebar's hint row drops unbound actions and items that don't fit
// whole (§6.7): no key-less "open", no dangling "↵".
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
		{160, "", "│ j/k move · ↵ open"},
		{160, `"<CR>" = ""`, "│ j/k move"},
		{160, "\"j\" = \"\"\n\"<Down>\" = \"\"", "│ ↵ open"}, // the arrow key is the action's too since F16.6
	} {
		if got := strings.TrimRight(row(configured(t, c.w, 45, "[keys.tree]\n"+c.bind)), " │"); got != c.want {
			t.Errorf("w=%d %s: %q, want %q", c.w, c.bind, got, c.want)
		}
	}
	a := sized(160, 45, "nerd")
	a.win().TreeW = 18
	if got := strings.TrimRight(row(a), " │"); got != "│ j/k move" { // "↵ open" doesn't fit whole
		t.Errorf("narrow: %q", got)
	}
}

// The window's only pane, once its last tab is closed, has no tab: the
// title just "⟨n⟩", the landing page's two buttons in the middle with
// their keys, clickable, and a tab bar holding just a clickable + (§5).
func TestEmptyPane(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, ":q<CR>:q<CR>:q<CR>") // both data tabs, then the query's
	p := a.win().Root.Leaves()[0]
	if len(a.win().Root.Leaves()) != 1 || len(p.Tabs) != 0 {
		t.Fatalf("expected one empty pane, got %v", a.win().Root.Leaves())
	}
	f, r := a.render(), a.layout()[p.ID]
	lines := strings.Split(f.String(), "\n")
	cells := func(y int) string { return strings.TrimSpace(ansi.Cut(lines[y], r.Min.X+1, r.Max.X-1)) } // by cells: the tree's 工作区 is wide
	if top := string([]rune(lines[r.Min.Y])[r.Min.X:r.Max.X]); !strings.HasPrefix(top, "┌─ ① ─") || strings.Contains(top, "run") {
		t.Errorf("title, with no hints: %q", top)
	}
	var body []string
	for y := r.Min.Y + 1; y < r.Max.Y-2; y++ {
		if got := cells(y); got != "" {
			body = append(body, got)
		}
	}
	if want := []string{ui.NerdIcons.Table.Text + " 打开表 t", ui.NerdIcons.Query.Text + " 新建 query c"}; !slices.Equal(body, want) {
		t.Errorf("landing page: %q", body)
	}
	if got := cells(r.Max.Y - 2); got != "+" {
		t.Errorf("tab bar: %q, want just +", got)
	}
	for _, act := range []string{"tab.new", "tab.table", "query.new"} {
		find(t, a, ui.Target{Kind: ui.KindHint, Pane: p.ID, Action: act})
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
		{"s", "·"}, // SPC s completes: the session list, which esc closes
		{"<Esc>", "·"},
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
		before, _, _ := strings.Cut(row, ui.NerdIcons.Search.Text)
		return ui.Width(before)
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
		{160, []string{"doraemon ▾", " 0: data* ", " 1: report ", " 1,1 ", "doraemon · pg@localhost:5432", " NORMAL "}, nil},
		{78, []string{"doraemon ▾", " 1: report ", " 1,1 ", " NORMAL "}, []string{"pg@localhost"}},
		{55, []string{"doraemon ▾", " 0: data* ", " 1,1 "}, []string{" 1: report "}},
		{40, []string{"doraemon ▾", " 0: data* ", "·", " NORMAL "}, []string{" 1,1 "}},
		{36, []string{" 0: data* ", "·", " NORMAL "}, []string{" 1,1 ", "doraemon"}},
	} {
		a := twoPanes(c.w, 24)
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
	th, ic, _, err := ui.ParseTheme(`
row = "#6c6a6d"
[icon]
query = { text = "C", fg = "#ff0000" }
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
	if st := cell(0, " C q…"); st.Fg != th.Dim {
		t.Fatal("the title around the icon keeps its color")
	}
	if st := cell(0, "C q…"); st.Fg != ic.Query.Fg {
		t.Errorf("query icon %v, want %v", st.Fg, ic.Query.Fg)
	}
	if st := cell(7, ui.NerdIcons.Table.Text); st.Fg != ic.Table.Fg { // the first table, under doraemon, public and Tables
		t.Errorf("table icon %v, want %v", st.Fg, ic.Table.Fg)
	}
}

// A pane holding a table, a query and a new tab (§5): each tab's icon in
// the tab bar, the current one's title and body; switching tabs switches
// the title's icon, ▶ run and the keys' scope along.
func TestGoldenMixedTabs160x45(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := wide(160, 45)
	p := a.focused()
	loadOrders(t, a, 3)
	a.run("query.new", 0)
	a.run("tab.new", 0)
	golden.RequireEqual(t, a.render().String())
	for _, c := range []struct {
		keys, scope string
		run         bool
	}{{"gt", "grid", false}, {"gt", "query", true}, {"gt", "landing", false}} {
		feed(t, a, c.keys)
		top := strings.Split(a.render().String(), "\n")[0]
		icon, _ := a.tabIcon(p.tab())
		if a.paneScope() != c.scope || strings.Contains(top, "▶ run") != c.run || icon.Text != "" && !strings.Contains(top, icon.Text+" "+p.Object()) {
			t.Errorf("%s to %s: scope %s, title %q", c.keys, p.Object(), a.paneScope(), top)
		}
	}
}

// `border` changes the lines of every box, the sidebar, the panes, the
// result area and the overlays alike, never the layout: hidden has the
// titles still (§7.1, F8.10).
func TestGoldenBorders(t *testing.T) {
	for _, b := range []string{"rounded", "bold", "double", "hidden"} {
		t.Run(b, func(t *testing.T) {
			a := sizedWith(160, 45, userConfig(t, `border = "`+b+`"`, ""))
			loadOrders(t, a, 30)
			feed(t, a, "<C-p>")
			golden.RequireEqual(t, a.render().String())
		})
	}
}

// The default layout's data and query are as wide as each other, give or
// take a column, with the sidebar open or folded to its thin bar (F8.9).
func TestDefaultLayoutEven(t *testing.T) {
	for _, open := range []bool{true, false} {
		a := sized(160, 45, "nerd")
		a.win().TreeOpen = open
		r := a.layout()
		if d := r[1].Dx() - r[2].Dx(); d < -1 || d > 1 || r[2].Max.X != 160 {
			t.Errorf("sidebar open %v: data %d, query %d wide", open, r[1].Dx(), r[2].Dx())
		}
		if !open {
			golden.RequireEqual(t, a.render().String())
		}
	}
}

// A zoomed pane has the zoom icon after its number and its border in the zoom
// color, focused or not; the others, and the pane once the zoom is over, have
// neither (F9.6).
func TestZoomedPaneIsMarked(t *testing.T) {
	for _, icons := range []string{"nerd", "ascii"} {
		a := sized(160, 45, icons)
		a.win().focus(2)
		feed(t, a, "<Space>z")
		f := a.render()
		r := a.window()
		fg := func(x, y int) any { return f.Buf.CellAt(x, y).Style.Fg }
		if got := fg(r.Min.X, r.Min.Y); got != a.theme.Zoom {
			t.Errorf("%s: corner %v, want the zoom color %v", icons, got, a.theme.Zoom)
		}
		top := strings.Split(f.String(), "\n")[0]
		if want := a.icons.Number(2) + " " + a.icons.Zoom.Text + " "; !strings.Contains(top, want) {
			t.Errorf("%s: title %q lacks %q", icons, top, want)
		}
		if !strings.Contains(top, a.icons.Unzoom.Text) || strings.Count(top, a.icons.Zoom.Text) != 1 { // the pane's button turns into the way back, so the zoom icon is the state's alone (F19.3)
			t.Errorf("%s: title %q: want the unzoom button and one zoom icon", icons, top)
		}
		glyph, _ := utf8.DecodeRuneInString(a.icons.Zoom.Text)
		for x := range r.Dx() { // the icon is drawn in the zoom color too
			if c := f.Buf.CellAt(x, 0); c.String() == string(glyph) {
				if c.Style.Fg != a.theme.Zoom {
					t.Errorf("%s: the icon is %v", icons, c.Style.Fg)
				}
				break
			}
		}
		a.win().focus(1) // not the pane zoomed: its border is the zoom color all the same
		if got := a.render().Buf.CellAt(r.Min.X, r.Min.Y).Style.Fg; got != a.theme.Zoom {
			t.Errorf("%s: unfocused zoomed corner %v", icons, got)
		}
		feed(t, a, "<Space>z")
		g := a.render()
		if got := g.Buf.CellAt(0, 0).Style.Fg; got == a.theme.Zoom {
			t.Errorf("%s: the border keeps the zoom color", icons)
		}
		if top := strings.Split(g.String(), "\n")[0]; strings.Contains(top, a.icons.Number(2)+" "+a.icons.Zoom.Text) { // the button stays (F19.3)
			t.Errorf("%s: the zoom icon stays: %q", icons, top)
		}
	}
	a := sized(160, 45, "nerd")
	a.win().focus(2)
	feed(t, a, "<Space>z")
	golden.RequireEqual(t, a.render().String())
}
