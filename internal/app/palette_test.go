package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
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
	if after := a.render(); after.Buf.Render() != before.Buf.Render() || after.Cursor != nil {
		t.Error("closing should give the screen back as it was")
	}
}

// Titles are Chinese; the action id after them is what `split` finds (§12).
func TestPaletteFindsByID(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, "<C-p>split")
	if got := strings.Join(rowsOf(a), " "); got != `pane.split.below=SPC " pane.split.right=SPC %` {
		t.Fatalf("split: %s", got)
	}
	panes := len(a.win().Root.Leaves())
	feed(t, a, "<CR>")
	if a.palette != nil || len(a.win().Root.Leaves()) != panes+1 {
		t.Fatal("↵ should split and close the palette")
	}

	a = twoPanes(160, 45)
	feed(t, a, "<C-p>resize")
	if got := strings.Join(rowsOf(a), " "); strings.Count(got, "pane.resize.") != 4 || strings.Count(got, "= ") != 3 || !strings.HasSuffix(got, "=") {
		t.Fatalf("resize has no default keys: %s", got)
	}
	r0 := a.win().Root.Ratio
	feed(t, a, " left<CR>")
	if a.win().Root.Ratio >= r0 {
		t.Error("the unbound resize ran from the palette")
	}

	a = twoPanes(160, 45)
	if feed(t, a, "<C-p>Split"); len(rowsOf(a)) != 0 {
		t.Errorf("smartcase: an upper-case letter matches case: %v", rowsOf(a))
	}
}

// The palette's own keys are not commands in it (§12); the ones opening
// it are, by the key the focused pane has for them: : for palette.open,
// none for palette.command (F5.8).
func TestPaletteListsNoOverlayKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	for _, q := range []string{"关闭", "palette"} {
		feed(t, a, "<C-p>"+q)
		for _, r := range rowsOf(a) {
			if strings.HasPrefix(r, "palette.") && r != "palette.open=:" && r != "palette.command=" && r != "palette.window=" && r != "palette.tab=" && r != "palette.table=" && r != "palette.key=" && r != "palette.session=" && r != "palette.sql=;" {
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

// : opens the all scope, where an ex alias typed whole comes first, as in
// the command scope; recent picks after it (§12, F5.8).
func TestPaletteExAliases(t *testing.T) {
	for in, want := range map[string]string{":q": "tab.close", ":qa": "quit", ":w": "save", "<C-p>>q": "tab.close"} {
		a := sized(160, 45, "nerd")
		a.state.Recent = []config.Recent{{Kind: "command", ID: "quit"}} // first for q but by the alias
		feed(t, a, in)
		if rows := rowsOf(a); a.palette.input.Text != strings.TrimPrefix(strings.TrimPrefix(in, ":"), "<C-p>") || len(rows) == 0 || !strings.HasPrefix(rows[0], want+"=") {
			t.Errorf("%s: input %q, first %v, want %s", in, a.palette.input.Text, rows, want)
		}
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
	// the split also left the zoom: OFF; then the session
	if rows := rowsOf(a); !strings.HasPrefix(rows[0], "pane.split.right=") || rows[1] != "pane.zoom=OFF" || rows[2] != "1 个窗口=" {
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
	for _, want := range []string{"$ord", "%ord", "&ord", "#ord", "@ord", ":ord", ">ord", ";ord", "ord"} {
		feed(t, a, "<Tab>")
		if got := a.palette.input.Text; got != want {
			t.Fatalf("Tab: %q, want %q", got, want)
		}
	}
	feed(t, a, "<S-Tab>")
	if in := a.palette.input; in.Text != ";ord" || in.Pos != len(";ord") {
		t.Fatalf("S-Tab wraps round: %+v", in)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "palette.scope 5"}).Min)
	if a.palette.input.Text != "@ord" {
		t.Errorf("clicking 表: %q", a.palette.input.Text)
	}
}

func TestPaletteScopesFilter(t *testing.T) {
	for in, want := range map[string]string{
		"@ord": "表:t_order 表:t_order_item",
		"%que": "Pane:② query · query_1 Tab:query_1",
	} {
		a := twoPanes(160, 45)
		if feed(t, a, "<C-p>"+in); strings.Join(namesOf(a), " ") != want {
			t.Errorf("%s: %v", in, namesOf(a))
		}
	}
	a := twoPanes(160, 45)
	feed(t, a, "<C-p>")
	all := strings.Join(namesOf(a), " ")
	for _, s := range []string{"窗口:0: data", "Pane:① table · t_order", "表:t_user", "命令:左右分割"} {
		if !strings.Contains(all, s) {
			t.Errorf("所有 lacks %s", s)
		}
	}
	if want := "会话:doraemon 窗口:0: data Pane:⓪ schema Pane:① table · t_order Tab:t_order Tab:t_user Pane:② query · query_1 Tab:query_1 窗口:1: report 表:"; !strings.HasPrefix(all, want) {
		t.Errorf("as the tree's workspace, when nothing is typed: a window, its panes, each pane's tabs:\n%.200s", all)
	}
}

// From the tree too a table not open yet takes a new tab, over neither a
// table's tab nor a query's; a landing tab it replaces (§7.8, §12).
func TestTreeOpensInNewTab(t *testing.T) {
	a := twoPanes(160, 45)
	data := a.focused()
	a.win().focus(0)
	treeTo(t, a, "t_sku")
	if feed(t, a, "<CR>"); tabNames(data) != "t_order t_user t_sku" || data.Cur != 2 {
		t.Fatalf("over a table's tab: %v cur %d", tabNames(data), data.Cur)
	}
	query := a.win().pane(2)
	a.win().focus(2)
	feed(t, a, "<C-p>@t_user<CR>")
	if tabNames(query) != "query_1 t_user" || query.Cur != 1 {
		t.Fatalf("over a query's: %v", tabNames(query))
	}
	a.run("tab.new", 0)
	feed(t, a, "<C-p>@t_order<CR>")
	if tabNames(query) != "query_1 t_user t_order" {
		t.Errorf("a landing tab is replaced: %v", tabNames(query))
	}
}

// A table opens in a new tab of the focused pane, else of the one focused
// last; the tab that was current stays (§12).
func TestPaletteOpensTables(t *testing.T) {
	a := twoPanes(160, 45)
	data := a.focused()
	feed(t, a, "<C-p>@t_user<CR>")
	if a.palette != nil || tabNames(data) != "t_order t_user t_user" || data.Cur != 2 {
		t.Fatalf("↵: tabs %v cur %d", tabNames(data), data.Cur)
	}
	a.win().focus(2)
	a.win().focus(0) // from the tree, ⟨2⟩ focused last, a query's pane (F3.37)
	feed(t, a, "<C-p>@t_sku<C-t>")
	if query := a.win().pane(2); tabNames(query) != "query_1 t_sku" || query.Cur != 1 || query.Prev != 0 {
		t.Fatalf("C-t: tabs %v cur %d prev %d", tabNames(query), query.Cur, query.Prev)
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
	if a.palette != nil || a.sess.Active != 1 {
		t.Error("a window's ↵ switches to it (F5.2)")
	}

	a = twoPanes(160, 45)
	data = a.focused()
	data.Tabs, data.Cur, data.Prev = nil, 0, -1 // an empty data pane
	feed(t, a, "<C-p>@t_user<CR>")
	if tabNames(data) != "t_user" || data.Cur != 0 || data.Prev != -1 {
		t.Errorf("into an empty pane: tabs %v cur %d prev %d, want no previous tab", tabNames(data), data.Cur, data.Prev)
	}

	a = twoPanes(160, 45)
	feed(t, a, ":q<CR>:q<CR>") // the query's pane left
	feed(t, a, "<C-p>@t_user<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || tabNames(leaves[0]) != "query_1 t_user" || leaves[0].Cur != 1 {
		t.Errorf("a query is not replaced, the table opens beside it (§5): %v", tabNames(leaves[0]))
	}
}

func TestPaletteFocusesPanes(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, "<C-p>%query<CR>")
	if a.palette != nil || a.win().Focus != 2 {
		t.Fatalf("focus %d, want the query", a.win().Focus)
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
		{"table from the zoomed query: beside it", "<C-p>@t_user<CR>", 2, 2, 2},
		{"table in the zoomed data pane", "<C-p>@t_user<CR>", 1, 1, 1},
		{"pane hidden by the zoom", "<C-p>%t_order<CR>", 2, 1, 0},
	} {
		a := twoPanes(160, 45)
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
		if tabs := lines[8]; !strings.Contains(tabs, " 所有   会话 $   窗口·Pane %   窗口 &   Tab #   表 @   键 :   命令 > ") {
			t.Errorf("%s: scope tabs row %q", icons, tabs)
		}
		if in := lines[9]; !strings.Contains(in, "│ "+search+" ab ") {
			t.Errorf("%s: input row %q", icons, in)
		}
	}
}

// ddlOrder is t_order's DDL as PG's Engine.DDL puts it together from the
// seed: 14 lines, 2 past what a preview shows.
const ddlOrder = `create table public.t_order (
  id bigint not null default nextval('t_order_id_seq'::regclass),
  user_id bigint not null,
  status order_status not null default 'pending'::order_status,
  amount numeric(10,2) not null,
  paid boolean,
  meta jsonb,
  raw json,
  note text,
  created_at timestamp with time zone not null default now(),
  deleted_at timestamp with time zone,
  constraint t_order_pkey PRIMARY KEY (id),
  constraint t_order_user_id_fkey FOREIGN KEY (user_id) REFERENCES t_user(id)
);`

// rest is previewDelay up on the palette's selection: the Cmd asking for
// its DDL, nil when none is asked for.
func rest(a *App) tea.Cmd {
	_, cmd := a.Update(previewDue{a.palette, a.palette.previewSeq})
	return cmd
}

func tableNamed(a *App, name string) db.Table {
	return a.sess.Conns[0].Tables[slices.IndexFunc(a.sess.Conns[0].Tables, func(t db.Table) bool { return t.Name == name })]
}

// The selection resting on a table asks for its DDL once the delay is
// up, only for the one it stopped on; the DDL shows under the list, its
// columns coming with it, cached till R drops it with the columns (F4.2).
// Out of time it is not cached; a database's error is, shown in its
// color; an answer R dropped meanwhile is not kept. The palette closing
// stops what is on its way.
func TestPalettePreview(t *testing.T) {
	a := wide(160, 45)
	feed(t, a, "<C-p>@t_order")
	p := a.palette
	first := previewDue{p, p.previewSeq}
	feed(t, a, "<Down>") // t_order_item
	if _, cmd := a.Update(first); cmd != nil || len(a.sess.Conns[0].ddl) != 0 {
		t.Fatal("moved on: t_order's asked for")
	}
	feed(t, a, "<Up>")
	if rest(a) == nil || a.paletteView().Preview != nil {
		t.Fatal("rested on t_order: not asked for")
	}
	order := tableNamed(a, "t_order")
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: order, text: ddlOrder, cols: db.Columns{Cols: []db.Column{{Name: "status"}}}})
	if pv := a.paletteView().Preview; pv == nil || pv.Text != ddlOrder || a.sess.Conns[0].cols[idOf(order)].Cols == nil {
		t.Fatalf("the preview: %+v", pv)
	}
	if st := styleOf(t, a.render(), "status order_status"); st.Fg != a.theme.SQLColumn {
		t.Errorf("its column: %+v", st)
	}
	seq := p.previewSeq
	feed(t, a, "<Down><Up>") // t_order_item timed, t_order cached
	if a.paletteView().Preview == nil || p.previewSeq != seq+1 {
		t.Errorf("back on t_order: cached, not timed again (seq %d, was %d)", p.previewSeq, seq)
	}
	a.sess.Conns[0].dropCols()
	if len(a.sess.Conns[0].ddl) != 0 {
		t.Fatal("R keeps the DDL")
	}
	feed(t, a, "<Down>")
	rest(a)
	item := tableNamed(a, "t_order_item")
	if a.Update(ddlMsg{conn: a.sess.Conns[0], table: item, err: context.DeadlineExceeded}); len(a.sess.Conns[0].ddl) != 0 {
		t.Error("out of time: cached")
	}
	feed(t, a, "<Up><Down>")
	rest(a)
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: item, err: errors.New(`ERROR: relation "t_order_item" does not exist (SQLSTATE 42P01)`)})
	if pv := a.paletteView().Preview; pv == nil || pv.Err == "" || styleOf(t, a.render(), "relation").Fg != a.theme.Error {
		t.Errorf("an error: %+v", pv)
	}
	feed(t, a, "<Up>")
	rest(a)
	a.sess.Conns[0].dropCols()
	if a.Update(ddlMsg{conn: a.sess.Conns[0], table: order, text: ddlOrder}); len(a.sess.Conns[0].ddl) != 0 {
		t.Error("an answer R dropped: kept")
	}
	if feed(t, a, "<BS><BS><BS><BS><BS><BS><BS>>"); a.paletteView().Preview != nil {
		t.Error("a command selected: a preview")
	}
	// the palette closing stops the fetches out: they would hold Meta up
	feed(t, a, "<C-u>@t_order_item")
	if rest(a) == nil || p.fetches.Err() != nil {
		t.Fatal("not asked for")
	}
	if feed(t, a, "<Esc>"); p.fetches.Err() == nil || a.fetching != nil {
		t.Error("closed: the fetch goes on")
	}
}

// The palette with a table's DDL under the list, in SQL's colors, cut at
// 12 rows with a dim … (F4.2).
func TestGoldenPalettePreview160x45(t *testing.T) {
	a := wide(160, 45)
	feed(t, a, "<C-p>@t_order")
	rest(a)
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: tableNamed(a, "t_order"), text: ddlOrder})
	f := a.render()
	if st := styleOf(t, f, "create table"); st.Fg != a.theme.Keyword {
		t.Errorf("create table: %+v", st)
	}
	if st := styleOf(t, f, "…"); st.Fg != a.theme.Dim {
		t.Errorf("the …: %+v", st)
	}
	golden.RequireEqual(t, f.String())
}

// % lists every open tab too, as the tree's workspace names it: its type's
// icon, its name, doraemon › 0: data › pane-1; ↵ switches to it and
// focuses its pane, and is not kept among the recent (F4.3). A pane is
// placed doraemon › 0: data.
func TestPaletteTabs(t *testing.T) {
	a := twoPanes(160, 45) // ① t_order and t_user, ② query_1
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
	// another window's tab is listed, not its pane; ↵ on it switches to that
	// window first (F5.2); its pane's ID is ①'s: IDs go by window
	a.sess.Windows[1].Root = leaf(&Pane{ID: 1, Tabs: []Tab{{Name: "t_sku", Data: newDataTab(a.sess.Conns[0], tableNamed(a, "t_sku"))}, landing()}, Cur: 1})
	feed(t, a, "<Esc><C-p>%report")
	var tab ui.PaletteRow
	for _, r := range a.paletteView().Rows {
		if r.Tag == "Pane" && strings.Contains(r.Where, "1: report") {
			t.Errorf("another window's pane: %+v", r)
		}
		if r.Name == "t_sku" {
			tab = r
		}
	}
	if tab.Where != "doraemon › 1: report › pane-1" {
		t.Fatalf("another window's tab: %+v", tab)
	}
	a.win().focus(2)
	one := a.win().pane(1).Cur
	feed(t, a, "<C-u>%t_sku<CR>")
	if a.sess.Active != 1 || a.win().Focus != 1 || a.win().Root.Pane.Cur != 0 || a.sess.Windows[0].pane(1).Cur != one {
		t.Errorf("↵ on another window's tab: window %d, focus %d, cur %d, data's ① %d", a.sess.Active, a.win().Focus, a.win().Root.Pane.Cur, a.sess.Windows[0].pane(1).Cur)
	}
}

// Tables of one name in several schemas tie: the current schema's goes
// first, as with nothing typed, though its place is the longer (§12).
func TestPaletteTiesKeepOrder(t *testing.T) {
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	c.Schemas = append(c.Schemas, "a")
	c.Tables = append(c.Tables, db.Table{Schema: "a", Name: "t_user"})
	feed(t, a, "<C-p>@t_user")
	if rows := a.paletteView().Rows; len(rows) != 4 || rows[0].Name != "t_user" || rows[0].Where != "doraemon.public" || rows[3].Where != "doraemon.a" {
		t.Errorf("rows %+v", rows)
	}
}

// The Window scope lists this session's windows by number; the Tab scope
// every table, query and result tab of it, the one focused last first and
// the current one last, a landing tab none; palette.window and palette.tab
// open them (F5.19, §12).
func TestPaletteWindowAndTabScopes(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	a.run("palette.window", 0)
	if a.palette.input.Text != "&" || len(namesOf(a)) != 1 || namesOf(a)[0] != "窗口:0: data" {
		t.Fatalf("palette.window: %q %v", a.palette.input.Text, namesOf(a))
	}
	feed(t, a, "<Esc><Space>c")
	a.run("query.new", 0) // query_2 in window 1: a new window has none
	feed(t, a, "<Space>c")
	a.run("query.new", 0)                                       // query_3 in window 2
	a.state.Recent = []config.Recent{{Kind: "window", ID: "3"}} // picked last, the number goes first
	a.run("palette.window", 0)
	if got := namesOf(a); !slices.Equal(got, []string{"窗口:0: data", "窗口:1: data-2", "窗口:2: data-3"}) {
		t.Errorf("by number: %v", got)
	}
	feed(t, a, "<Esc><Space>p<Space>p") // back on window 0, query_1 and a table in it
	loadOrders(t, a, 3)                 // focused: t_order
	a.win().focus(2)                    // query_1
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	a.run("palette.tab", 0)
	if got := namesOf(a); len(got) == 0 || got[len(got)-1] != "Tab:query_1" || got[0] != "Tab:t_order" {
		t.Fatalf("Tab scope: %v", got)
	}
	got := namesOf(a)
	for _, n := range got {
		if n == "Tab:新 tab" {
			t.Errorf("a landing tab is listed: %v", got)
		}
	}
	// the other windows' queries are listed too, and the order follows the focus
	if !slices.Contains(got, "Tab:query_2") || !slices.Contains(got, "Tab:query_3") {
		t.Errorf("other windows' tabs: %v", got)
	}
	feed(t, a, "<CR>") // the first: back to the table
	if dataOf(a.focused()) == nil || a.focused().ID != 1 {
		t.Errorf("↵ goes back to the table: pane %d", a.focused().ID)
	}
	a.run("palette.tab", 0)
	if got := namesOf(a); got[len(got)-1] != "Tab:t_order" || got[0] != "Tab:query_1" {
		t.Errorf("after the switch: %v", got)
	}
}

// palette.session opens the palette in its session scope (F9.9, §12).
func TestPaletteSessionCommand(t *testing.T) {
	a, _ := withSessions()
	a.run("palette.session", 0)
	if a.palette == nil || a.palette.input.Text != "$" {
		t.Fatalf("palette.session: %+v", a.palette)
	}
	if names := namesOf(a); !slices.Equal(names, []string{"会话:doraemon", "会话:report"}) {
		t.Errorf("the sessions: %v", names)
	}
}

// palette.table opens the palette in its table scope, the tables of every
// connection that is connected in it (F8.13, §12).
func TestPaletteTableCommand(t *testing.T) {
	a := sized(160, 45, "nerd")
	other := readyConn("seed")
	other.Schema, other.Schemas, other.home = "public", []string{"public"}, "public"
	other.Tables = []db.Table{{Schema: "public", Name: "t_seed_only"}}
	a.sess.Conns = append(a.sess.Conns, other)
	a.run("palette.table", 0)
	if a.palette == nil || a.palette.input.Text != "@" {
		t.Fatalf("palette.table: %+v", a.palette)
	}
	names := namesOf(a)
	if !slices.Contains(names, "表:t_order") || !slices.Contains(names, "表:t_seed_only") {
		t.Errorf("the tables of both connections: %v", names)
	}
}

// C-f and C-b scroll the DDL preview by half its rows, stopping at the ends, a
// … in its first row when lines are above and in its last when more are below;
// another table has it at the top, hovered over or keyed to, or the scope
// changed, or more typed; with nothing to scroll the keys do nothing, and there
// is no hint of them, not where the palette has no room for the preview either;
// C-u is the input's, as it was (F9.7).
func TestPalettePreviewScroll(t *testing.T) {
	a := wide(160, 45)
	long := ddlOrder + "\n" + strings.Repeat("create index i on t_order (id);\n", 20) + "create index last_one on t_order (note);"
	feed(t, a, "<C-p>@t_order")
	rest(a)
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: tableNamed(a, "t_order"), text: long})
	pv := a.palettePreview()
	_, ms := a.paletteMatches()
	_, _, grid := a.paletteBox(len(ms))
	rows, lines := grid.Dy(), pv.Lines()
	if rows != 12 || lines != 35 {
		t.Fatalf("rows %d, lines %d", rows, lines)
	}
	row := func(i int) string { // the preview's i-th row
		f, line := a.render(), ""
		for x := grid.Min.X; x < grid.Max.X; x++ {
			line += f.Buf.CellAt(x, grid.Min.Y+i).String()
		}
		return strings.TrimSpace(line)
	}
	if !strings.Contains(a.render().String(), "C-b/C-f 滚动预览") || row(rows-1) != "…" || row(0) != "create table public.t_order (" {
		t.Fatalf("at the top: first %q, last %q", row(0), row(rows-1))
	}
	feed(t, a, "<C-f>")
	if a.palette.pvTop != 6 || row(0) != "…" || row(1) == "create table public.t_order (" || row(rows-1) != "…" {
		t.Errorf("C-f: top %d, first %q, last %q", a.palette.pvTop, row(0), row(rows-1))
	}
	feed(t, a, "<C-f><C-f><C-f><C-f><C-f><C-f>")
	if a.palette.pvTop != lines-rows || row(rows-1) != "create index last_one on t_order (note);" || row(0) != "…" {
		t.Errorf("at the end: top %d, last %q", a.palette.pvTop, row(rows-1))
	}
	feed(t, a, "<C-b>")
	if a.palette.pvTop != lines-rows-6 {
		t.Errorf("C-b: top %d", a.palette.pvTop)
	}
	feed(t, a, "<C-b><C-b><C-b><C-b><C-b><C-b><C-b>")
	if a.palette.pvTop != 0 || row(0) != "create table public.t_order (" {
		t.Errorf("back at the top: %d, %q", a.palette.pvTop, row(0))
	}
	// another candidate: the top again, and typing more
	feed(t, a, "<C-f><C-f>")
	if a.palette.pvTop == 0 {
		t.Fatal("not scrolled")
	}
	feed(t, a, "<C-n><C-p>")
	if pv := a.palettePreview(); pv == nil || pv.Top != 0 {
		t.Errorf("another candidate and back: %+v", pv)
	}
	feed(t, a, "<C-f><BS>r") // the same table is selected again
	if pv := a.palettePreview(); pv == nil || pv.Top != 0 || a.palette.input.Text != "@t_order" {
		t.Errorf("typing: %+v, input %q", pv, a.palette.input.Text)
	}

	// a DDL that fits: nothing to scroll, no hint, and C-u is the input's still
	b := wide(160, 45)
	feed(t, b, "<C-p>@t_order")
	rest(b)
	b.Update(ddlMsg{conn: b.sess.Conns[0], table: tableNamed(b, "t_order"), text: "create table t_order (id int);"})
	feed(t, b, "<C-f>")
	if b.palette.pvTop != 0 || b.palette.input.Text != "@t_order" || strings.Contains(b.render().String(), "滚动预览") {
		t.Errorf("nothing to scroll: top %d, input %q", b.palette.pvTop, b.palette.input.Text)
	}
	feed(t, b, "<C-u>")
	if b.palette.input.Text != "@" && b.palette.input.Text != "" {
		t.Errorf("C-u is the input's: %q", b.palette.input.Text)
	}
}

// A scrolled preview has a dim … at both ends (F9.7).
func TestGoldenPalettePreviewScrolled(t *testing.T) {
	a := wide(160, 45)
	feed(t, a, "<C-p>@t_order")
	rest(a)
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: tableNamed(a, "t_order"), text: ddlOrder + "\n" + strings.Repeat("create index i on t_order (id);\n", 8) + "create index last_one on t_order (note);"})
	feed(t, a, "<C-f>")
	golden.RequireEqual(t, a.render().String())
}

// Hovering to another table and back, or the scope away and back, has the
// preview at the top: the selection that changed is what counts, whoever
// changed it (F9.7).
func TestPalettePreviewTopWhenTheSelectionMoves(t *testing.T) {
	long := ddlOrder + "\n" + strings.Repeat("create index i on t_order (id);\n", 20) + "create index last_one on t_order (note);"
	a := wide(160, 45)
	feed(t, a, "<C-p>@t_o")
	rest(a)
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: tableNamed(a, "t_order"), text: long})
	first := a.palette.sel
	feed(t, a, "<C-f><C-f>")
	if a.palette.pvTop == 0 {
		t.Fatal("not scrolled")
	}
	other := find(t, a, ui.Target{Kind: ui.KindRow, I: first + 1})
	a.Update(tea.MouseMotionMsg{X: other.Min.X + 4, Y: other.Min.Y}) // the pointer takes the selection
	if a.palette.sel == first {
		t.Fatal("the pointer did not move the selection")
	}
	back := find(t, a, ui.Target{Kind: ui.KindRow, I: first})
	a.Update(tea.MouseMotionMsg{X: back.Min.X + 4, Y: back.Min.Y})
	if a.palette.sel != first || a.palette.pvTop != 0 {
		t.Errorf("hovered away and back: row %d, top %d", a.palette.sel, a.palette.pvTop)
	}
	feed(t, a, "<C-f><C-f>")
	feed(t, a, "<Tab><S-Tab>") // the scope away and back
	if a.palette.pvTop != 0 {
		t.Errorf("the scope away and back: top %d", a.palette.pvTop)
	}
}

// Where the palette has no room for the preview there is none to scroll: no
// hint, and the keys do nothing (§12「预览」, F9.7).
func TestPalettePreviewHiddenIsNotScrolled(t *testing.T) {
	long := ddlOrder + "\n" + strings.Repeat("create index i on t_order (id);\n", 20)
	a := sized(100, 12, "nerd")
	feed(t, a, "<C-p>@t_order")
	rest(a)
	a.Update(ddlMsg{conn: a.sess.Conns[0], table: tableNamed(a, "t_order"), text: long})
	if pv, rows := a.previewWindow(); pv == nil || rows != 0 {
		t.Fatalf("the preview is shown: %d rows", rows)
	}
	feed(t, a, "<C-f><C-f>")
	if a.palette.pvTop != 0 || strings.Contains(a.render().String(), "滚动预览") {
		t.Errorf("a preview not shown: top %d", a.palette.pvTop)
	}
}

// The scope tabs scroll as a pane's tabs do when they do not fit: at 80 columns the current
// one is always in sight, the ones cut off say how many with ‹N and N› (§12, F22.5).
func TestPaletteScopesScroll(t *testing.T) {
	a := sized(80, 24, "nerd")
	feed(t, a, "<C-p>")
	row := func() string {
		lines := strings.Split(a.render().String(), "\n")
		for i, l := range lines {
			if strings.Contains(l, "命令面板") {
				return lines[i+1]
			}
		}
		t.Fatal("no palette")
		return ""
	}
	labels := []string{"所有", "会话 $", "窗口·Pane %", "窗口 &", "Tab #", "表 @", "键 :", "命令 >", "SQL ;"}
	for i := range 2 * len(labels) { // all the way round, twice
		want := labels[i%len(labels)]
		if r := row(); !strings.Contains(r, " "+want+" ") {
			t.Errorf("scope %d: %q is not in sight in %q", i%len(labels), want, r)
		}
		feed(t, a, "<Tab>")
	}
	feed(t, a, "<S-Tab>")
	if r := row(); !strings.Contains(r, "SQL ;") || !strings.Contains(r, "‹") || strings.Contains(r, "›") {
		t.Errorf("the last scope: %q", r)
	}
}
