package app

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"sqlmux/internal/db"
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
	a := twoPanes(160, 45, "nerd")
	feed(t, a, "<C-p>split")
	if got := strings.Join(rowsOf(a), " "); got != `pane.split.below=SPC " pane.split.right=SPC %` {
		t.Fatalf("split: %s", got)
	}
	panes := len(a.win().Root.Leaves())
	feed(t, a, "<CR>")
	if a.palette != nil || len(a.win().Root.Leaves()) != panes+1 {
		t.Fatal("↵ should split and close the palette")
	}

	a = twoPanes(160, 45, "nerd")
	feed(t, a, "<C-p>resize")
	if got := strings.Join(rowsOf(a), " "); strings.Count(got, "pane.resize.") != 4 || strings.Count(got, "= ") != 3 || !strings.HasSuffix(got, "=") {
		t.Fatalf("resize has no default keys: %s", got)
	}
	r0 := a.win().Root.Ratio
	feed(t, a, " left<CR>")
	if a.win().Root.Ratio >= r0 {
		t.Error("the unbound resize ran from the palette")
	}

	a = twoPanes(160, 45, "nerd")
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
			if strings.HasPrefix(r, "palette.") && r != "palette.open=C-p" && r != "palette.command=:" && r != "palette.sql=;" {
				t.Errorf("%s lists %s", q, r)
			}
		}
		feed(t, a, "<Esc>")
	}
}

// Cell editing's keys are no commands either (§12).
func TestPaletteListsNoCellKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>")
	for _, r := range namesOf(a) {
		for _, name := range []string{"确定", "完成编辑", "加一", "减一", "下一段", "上一段"} {
			if r == "命令:"+name {
				t.Errorf("lists %s", r)
			}
		}
	}
}

func TestPaletteHighlightsMatches(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>split")
	f := a.render()
	for y, line := range strings.Split(f.String(), "\n") {
		if i := strings.Index(line, "pane.split.right"); i >= 0 {
			x := ui.Width(line[:i]) + len("pane.")
			if s, before := f.Buf.CellAt(x, y).Style, f.Buf.CellAt(x-1, y).Style; s.Fg != a.theme.Match || s.Bg != before.Bg || before.Fg == a.theme.Match {
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
	if p := a.palette; p.sel != 12 || p.top != 1 { // 12 rows show
		t.Fatalf("12 down: sel %d top %d, want the list scrolled to show it", p.sel, p.top)
	}
	feed(t, a, "<Up><C-p><C-n>")
	if p := a.palette; p.sel != 11 || p.top != 1 {
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
	box, _, _ := ui.PaletteBox(a.window(), len(rowsOf(a)), false, 0)
	click(a, uv.Pos(box.Min.X+3, box.Min.Y+2)) // the input row
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
	box, _, _ := ui.PaletteBox(a.window(), len(rowsOf(a)), false, 0)
	// the input row, below the scope tabs; after the search icon and a space
	if c := a.View().Cursor; c == nil || c.X != box.Min.X+2+ui.Width(ui.NerdIcons.Search.Text)+1+1 || c.Y != box.Min.Y+2 {
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
	for _, want := range []string{"%ord", "@ord", ">ord", ";ord", "ord"} {
		feed(t, a, "<Tab>")
		if got := a.palette.input.Text; got != want {
			t.Fatalf("Tab: %q, want %q", got, want)
		}
	}
	feed(t, a, "<S-Tab>")
	if in := a.palette.input; in.Text != ";ord" || in.Pos != len(";ord") {
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
		"%con": "Tab:console_1 Pane:② console · console_1 Pane:⓪ schema", // ⓪'s doraemon too
	} {
		a := twoPanes(160, 45, "nerd")
		if feed(t, a, "<C-p>"+in); strings.Join(namesOf(a), " ") != want {
			t.Errorf("%s: %v", in, namesOf(a))
		}
	}
	a := twoPanes(160, 45, "nerd")
	feed(t, a, "<C-p>")
	all := strings.Join(namesOf(a), " ")
	for _, s := range []string{"窗口:0: data", "Pane:① table · t_order", "表:t_user", "命令:左右分割"} {
		if !strings.Contains(all, s) {
			t.Errorf("所有 lacks %s", s)
		}
	}
	if want := "窗口:0: data Pane:⓪ schema Pane:① table · t_order Tab:t_order Tab:t_user Pane:② console · console_1 Tab:console_1 窗口:1: report 表:"; !strings.HasPrefix(all, want) {
		t.Errorf("as the tree's workspace, when nothing is typed: a window, its panes, each pane's tabs:\n%.200s", all)
	}
}

// From the tree too a table not open yet takes a new tab, over neither a
// table's tab nor a console's; a landing tab it replaces (§7.8, §12).
func TestTreeOpensInNewTab(t *testing.T) {
	a := twoPanes(160, 45, "nerd")
	data := a.focused()
	a.win().focus(0)
	treeTo(t, a, "t_sku")
	if feed(t, a, "<CR>"); tabNames(data) != "t_order t_user t_sku" || data.Cur != 2 {
		t.Fatalf("over a table's tab: %v cur %d", tabNames(data), data.Cur)
	}
	console := a.win().pane(2)
	a.win().focus(2)
	feed(t, a, "<C-p>@t_user<CR>")
	if tabNames(console) != "console_1 t_user" || console.Cur != 1 {
		t.Fatalf("over a console's: %v", tabNames(console))
	}
	a.run("tab.new", 0)
	feed(t, a, "<C-p>@t_order<CR>")
	if tabNames(console) != "console_1 t_user t_order" {
		t.Errorf("a landing tab is replaced: %v", tabNames(console))
	}
}

// A table opens in a new tab of the focused pane, else of the one focused
// last; the tab that was current stays (§12).
func TestPaletteOpensTables(t *testing.T) {
	a := twoPanes(160, 45, "nerd")
	data := a.focused()
	feed(t, a, "<C-p>@t_user<CR>")
	if a.palette != nil || tabNames(data) != "t_order t_user t_user" || data.Cur != 2 {
		t.Fatalf("↵: tabs %v cur %d", tabNames(data), data.Cur)
	}
	a.win().focus(2)
	a.win().focus(0) // from the tree, ⟨2⟩ focused last, a console's pane (F3.37)
	feed(t, a, "<C-p>@t_sku<C-t>")
	if console := a.win().pane(2); tabNames(console) != "console_1 t_sku" || console.Cur != 1 || console.Prev != 0 {
		t.Fatalf("C-t: tabs %v cur %d prev %d", tabNames(console), console.Cur, console.Prev)
	}
	if a.win().Focus != 2 {
		t.Errorf("focus %d: the pane the table opened in takes it", a.win().Focus)
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

	a = twoPanes(160, 45, "nerd")
	data = a.focused()
	data.Tabs, data.Cur, data.Prev = nil, 0, -1 // an empty data pane
	feed(t, a, "<C-p>@t_user<CR>")
	if tabNames(data) != "t_user" || data.Cur != 0 || data.Prev != -1 {
		t.Errorf("into an empty pane: tabs %v cur %d prev %d, want no previous tab", tabNames(data), data.Cur, data.Prev)
	}

	a = twoPanes(160, 45, "nerd")
	feed(t, a, ":q<CR>:q<CR>") // the console's pane left
	feed(t, a, "<C-p>@t_user<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || tabNames(leaves[0]) != "console_1 t_user" || leaves[0].Cur != 1 {
		t.Errorf("a console is not replaced, the table opens beside it (§5): %v", tabNames(leaves[0]))
	}
}

func TestPaletteFocusesPanes(t *testing.T) {
	a := twoPanes(160, 45, "nerd")
	feed(t, a, "<C-p>%console<CR>")
	if a.palette != nil || a.win().Focus != 2 {
		t.Fatalf("focus %d, want the console", a.win().Focus)
	}
}

// Where the palette sends focus, the pane is on screen: a zoom on another
// pane ends, one on that pane stays (§12, §5).
func TestPaletteFocusUnzooms(t *testing.T) {
	for _, c := range []struct {
		name, keys  string
		zoomed      int // the pane zoomed before
		focus, zoom int // after
	}{
		{"table from the zoomed console: beside it", "<C-p>@t_user<CR>", 2, 2, 2},
		{"table in the zoomed data pane", "<C-p>@t_user<CR>", 1, 1, 1},
		{"pane hidden by the zoom", "<C-p>%t_order<CR>", 2, 1, 0},
	} {
		a := twoPanes(160, 45, "nerd")
		a.win().focus(c.zoomed)
		feed(t, a, "<Space>z")
		feed(t, a, c.keys)
		if a.win().Focus != c.focus || a.win().Zoom != c.zoom {
			t.Errorf("%s: focus %d zoom %d, want focus %d zoom %d", c.name, a.win().Focus, a.win().Zoom, c.focus, c.zoom)
		}
	}
}

// §12: min(100, W-4) wide, top edge at (H-1)/6; scope tabs with their
// prefixes above the input, which a search icon leads.
func TestPaletteLayout(t *testing.T) {
	for _, c := range []struct{ w, h, width, top int }{{160, 45, 100, 7}, {80, 24, 76, 3}} {
		a := sized(c.w, c.h, "nerd")
		feed(t, a, "<C-p>")
		if box, _, _ := ui.PaletteBox(a.window(), len(rowsOf(a)), false, 0); box.Dx() != c.width || box.Min.Y != c.top {
			t.Errorf("%dx%d: box %v, want %d wide at row %d", c.w, c.h, box, c.width, c.top)
		}
	}
	for icons, search := range map[string]string{"nerd": ui.NerdIcons.Search.Text, "ascii": "~"} {
		a := sized(160, 45, icons)
		feed(t, a, "<C-p>ab")
		lines := strings.Split(a.render().String(), "\n")
		if tabs := lines[8]; !strings.Contains(tabs, " 所有   窗口·Pane %   表 @   命令 > ") {
			t.Errorf("%s: scope tabs row %q", icons, tabs)
		}
		if in := lines[9]; !strings.Contains(in, "│ "+search+" ab ") {
			t.Errorf("%s: input row %q", icons, in)
		}
	}
}

// ddlOrder is t_order's DDL, as postgres.DDL puts it.
const ddlOrder = "create table public.t_order (\n  id bigint not null,\n  status text not null,\n  constraint t_order_pkey PRIMARY KEY (id)\n);\nCREATE INDEX t_order_status ON public.t_order USING btree (status);"

// The selection resting on a table asks for its DDL once the delay is
// up, only for the one it stopped on; the DDL shows under the list,
// cached, till R drops it with the columns (F4.2).
func TestPalettePreview(t *testing.T) {
	a := wide(160, 45)
	feed(t, a, "<C-p>@t_order")
	p := a.palette
	first := previewDue{p, p.previewSeq}
	feed(t, a, "<Down>") // t_order_item
	if _, cmd := a.Update(first); cmd != nil || len(a.sess.ddl) != 0 {
		t.Fatal("moved on: t_order's asked for")
	}
	feed(t, a, "<Up>")
	if _, cmd := a.Update(previewDue{p, p.previewSeq}); cmd == nil || a.paletteView().Preview != nil {
		t.Fatal("rested on t_order: not asked for")
	}
	order := a.sess.Tables[slices.IndexFunc(a.sess.Tables, func(t db.Table) bool { return t.Name == "t_order" })]
	a.Update(ddlMsg{table: order, text: ddlOrder})
	if pv := a.paletteView().Preview; pv == nil || pv.Text != ddlOrder {
		t.Fatalf("the preview: %+v", pv)
	}
	f := a.render().String()
	if !strings.Contains(f, "  status text not null,") || !strings.Contains(f, "CREATE INDEX t_order_status") {
		t.Errorf("not drawn:\n%s", f)
	}
	seq := p.previewSeq
	feed(t, a, "<Down><Up>") // t_order_item timed, t_order cached
	if a.paletteView().Preview == nil || p.previewSeq != seq+1 {
		t.Errorf("back on t_order: cached, not timed again (seq %d, was %d)", p.previewSeq, seq)
	}
	if feed(t, a, "<BS><BS><BS><BS><BS><BS><BS>>"); a.paletteView().Preview != nil {
		t.Error("a command selected: a preview")
	}
	a.sess.dropCols()
	if len(a.sess.ddl) != 0 {
		t.Error("R keeps the DDL")
	}
}

// The palette with a table's DDL under the list, in SQL's colors (F4.2).
func TestGoldenPalettePreview160x45(t *testing.T) {
	a := wide(160, 45)
	feed(t, a, "<C-p>@t_order")
	order := a.sess.Tables[slices.IndexFunc(a.sess.Tables, func(t db.Table) bool { return t.Name == "t_order" })]
	a.Update(ddlMsg{table: order, text: ddlOrder})
	if st := styleOf(t, a.render(), "PRIMARY KEY"); st.Fg != a.theme.Keyword {
		t.Errorf("PRIMARY KEY: %+v", st)
	}
	golden.RequireEqual(t, a.render().String())
}

// % lists every open tab too, as the tree's workspace names it: its type's
// icon, its name, doraemon › 0: data › pane-1; ↵ switches to it and
// focuses its pane, and is not kept among the recent (F4.3). A pane is
// placed doraemon › 0: data.
func TestPaletteTabs(t *testing.T) {
	a := twoPanes(160, 45, "nerd") // ① t_order and t_user, ② console_1
	a.win().focus(2)
	feed(t, a, "<C-p>%t_us")
	rows := a.paletteView().Rows
	if len(rows) == 0 || rows[0].Tag != "Tab" || rows[0].Name != "t_user" || rows[0].Where != "doraemon › 0: data › pane-1" || rows[0].Icon.Text != a.icons.Table.Text {
		t.Fatalf("t_user's tab: %+v", rows)
	}
	recent := len(a.state.Recent)
	feed(t, a, "<CR>")
	if p := a.focused(); a.palette != nil || p.ID != 1 || p.Object() != "t_user" || len(a.state.Recent) != recent {
		t.Fatalf("↵: focus %d on %q, recent %v", p.ID, p.Object(), a.state.Recent)
	}
	feed(t, a, "<C-p>%②")
	if rows := a.paletteView().Rows; len(rows) == 0 || rows[0].Where != "doraemon › 0: data" {
		t.Errorf("a pane's place: %+v", rows)
	}
}
