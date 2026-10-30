package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/ui"
)

// find returns the first hit region for want.
func find(t *testing.T, a *App, want ui.Target) uv.Rectangle {
	t.Helper()
	a.View()
	for _, h := range a.hits {
		if h.Target == want {
			return h.Rect
		}
	}
	t.Fatalf("no hit region %+v", want)
	return uv.Rectangle{}
}

func click(a *App, p uv.Position) tea.Cmd {
	a.View()
	_, cmd := a.Update(tea.MouseClickMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft})
	return cmd
}

func TestClickFocusesPane(t *testing.T) {
	a := twoPanes(160, 45)
	r := a.layout()[2]
	click(a, uv.Pos(r.Min.X+5, r.Min.Y+5))
	if a.win().Focus != 2 {
		t.Fatalf("focus %d after clicking the query", a.win().Focus)
	}
}

func TestDoubleClickTitleZooms(t *testing.T) {
	a := twoPanes(160, 45)
	title := find(t, a, ui.Target{Kind: ui.KindTitle, Pane: 2}).Min
	click(a, title)
	if a.win().Zoom != 0 || a.win().Focus != 2 {
		t.Fatal("a single click only focuses")
	}
	click(a, title)
	if a.win().Zoom != 2 {
		t.Fatal("a double click should zoom")
	}
	click(a, uv.Pos(0, 0)) // the zoomed pane's title is now at the corner
	click(a, uv.Pos(0, 0))
	if a.win().Zoom != 0 {
		t.Fatal("a second double click should restore")
	}
	click(a, title)
	a.lastClickAt = a.lastClickAt.Add(-doubleClick - time.Millisecond)
	click(a, title)
	if a.win().Zoom != 0 {
		t.Fatal("clicks further apart than 400ms are not a double")
	}
}

func TestDragBorder(t *testing.T) {
	a := twoPanes(160, 45)
	gap := find(t, a, ui.Target{Kind: ui.KindBorder, I: 0})
	before := a.layout()[1].Dx()
	a.View()
	a.Update(tea.MouseClickMsg{X: gap.Min.X, Y: 10, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: gap.Min.X - 20, Y: 10, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: gap.Min.X - 20, Y: 10, Button: tea.MouseLeft})
	if got := a.layout()[1].Dx(); got != before-20 {
		t.Fatalf("data width %d after dragging 20 left, was %d", got, before)
	}
	a.Update(tea.MouseMotionMsg{X: gap.Min.X, Y: 10})
	if got := a.layout()[1].Dx(); got != before-20 {
		t.Fatal("after release the border must stay put")
	}
}

func TestClickThinBarUnfolds(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>b")
	click(a, uv.Pos(1, 10))
	if !a.win().TreeOpen {
		t.Fatal("clicking the thin bar should unfold the sidebar")
	}
}

// Hints are buttons: clicking one runs its action, like pressing its key.
func TestClickHints(t *testing.T) {
	a := sized(160, 45, "nerd")
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: 0, Action: "tree.toggle"}).Min)
	if a.win().TreeOpen {
		t.Fatal("clicking SPC b should fold the sidebar")
	}
	find(t, a, ui.Target{Kind: ui.KindButton, Action: "palette.open"}) // the status bar's C-p is a button too

	a = sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	due(a)
	var b int
	for i, it := range a.whichKeyOverlay().Items {
		if it.Key == "b" {
			b = i
		}
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindItem, I: b}).Min)
	if a.win().TreeOpen || a.whichKey || len(a.res.Pending()) != 0 {
		t.Fatal("clicking which-key's b should do SPC b")
	}
}

func TestClickOutsideOverlayCloses(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	due(a)
	click(a, uv.Pos(50, 2)) // a pane, behind the overlay
	if a.whichKey || len(a.res.Pending()) != 0 || a.win().Focus != 1 {
		t.Fatalf("overlay %v pending %v focus %d", a.whichKey, a.res.Pending(), a.win().Focus)
	}
}

func TestHover(t *testing.T) {
	a := twoPanes(160, 45)
	run := find(t, a, ui.Target{Kind: ui.KindHint, Pane: 2, Action: "query.run"})
	before := a.render().Buf.CellAt(run.Min.X, run.Min.Y).Style.Bg
	a.Update(tea.MouseMotionMsg{X: run.Min.X, Y: run.Min.Y})
	if after := a.render().Buf.CellAt(run.Min.X, run.Min.Y).Style.Bg; after == before {
		t.Fatalf("hovering ▶ run left its background at %v", before)
	}
}

// The wheel scrolls the pane under the pointer, whatever has focus; the
// tree's view moves and pulls the cursor along, as in nvim (§7.8).
func TestWheelScrollsPaneUnderPointer(t *testing.T) {
	a := sized(160, 18, "nerd") // 24 nodes, 11 rows shown
	tree := &a.win().tree
	wheel := func(id int, b tea.MouseButton) {
		r := a.layout()[id]
		a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: b})
	}
	wheel(1, tea.MouseWheelDown) // the data pane, focused, has nothing to scroll yet
	if tree.top != 0 {
		t.Fatalf("the wheel over the data pane scrolled the tree to %d", tree.top)
	}
	wheel(0, tea.MouseWheelDown)
	if tree.top != wheelStep || tree.cursor != wheelStep || a.win().Focus != 1 {
		t.Fatalf("tree top %d cursor %d, focus %d", tree.top, tree.cursor, a.win().Focus)
	}
	// inside the border: the filter line and its rule, then the nodes
	first := strings.Split(a.render().String(), "\n")[a.layout()[0].Min.Y+3]
	if !strings.Contains(first, " Tables (14) ") {
		t.Errorf("the tree should start %d nodes down: %q", wheelStep, first)
	}
	for range 4 {
		wheel(0, tea.MouseWheelDown)
	}
	if tree.top != 24-11 { // 22 nodes, and the Queries root with its one
		t.Fatalf("the last node stops at the bottom: top %d", tree.top)
	}
	a.treeGo(23)
	for range 5 {
		wheel(0, tea.MouseWheelUp)
	}
	if tree.top != 0 || tree.cursor != 10 {
		t.Fatalf("scrolling up stops at the top: top %d, cursor %d kept in view", tree.top, tree.cursor)
	}
}

// Under SPC q's numbers a click on a pane is pressing its number; anywhere
// else it only closes them. Either way the next key is a key again (§5).
func TestClickUnderPaneNumbers(t *testing.T) {
	statusBar := uv.Pos(80, 44)
	for _, c := range []struct {
		name  string
		at    func(a *App) uv.Position
		focus int
	}{
		{"query", func(a *App) uv.Position { r := a.layout()[2]; return uv.Pos(r.Min.X+5, r.Min.Y+5) }, 2},
		{"sidebar", func(_ *App) uv.Position { return uv.Pos(5, 10) }, 0},
		{"status bar", func(*App) uv.Position { return statusBar }, 1},
		{"a window on the status bar", func(a *App) uv.Position {
			return find(t, a, ui.Target{Kind: ui.KindButton, Action: "window.select 1"}).Min
		}, 1},
		{"the session block", func(a *App) uv.Position {
			return find(t, a, ui.Target{Kind: ui.KindButton, Action: "session.list"}).Min
		}, 1},
	} {
		a := twoPanes(160, 45)
		feed(t, a, "<Space>q")
		click(a, c.at(a))
		if a.paneNumbers || a.win().Focus != c.focus || a.sess.Active != 0 || a.sessList != nil {
			t.Errorf("%s: numbers %v, focus %d, want %d", c.name, a.paneNumbers, a.win().Focus, c.focus)
		}
		if feed(t, a, "<C-p>"); a.palette == nil {
			t.Errorf("%s: the key after the click was swallowed", c.name)
		}
	}
}

// The gap right of the sidebar drags its width, from 16 columns to half the
// window; the width is the window's, so it outlasts splits, zooms and folds
// (§7.8).
func TestDragSidebarEdge(t *testing.T) {
	a := sized(160, 45, "nerd")
	edge := find(t, a, ui.Target{Kind: ui.KindTreeEdge})
	if edge.Min.X != 32 {
		t.Fatalf("the edge is at column %d, want right of the 32-column sidebar", edge.Min.X)
	}
	a.Update(tea.MouseClickMsg{X: 32, Y: 10, Button: tea.MouseLeft})
	for _, c := range [][2]int{{50, 50}, {5, 16}, {150, 80}, {40, 40}} {
		a.Update(tea.MouseMotionMsg{X: c[0], Y: 10, Button: tea.MouseLeft})
		if got := a.sidebarRect().Dx(); got != c[1] {
			t.Errorf("pointer at %d: sidebar %d wide, want %d", c[0], got, c[1])
		}
	}
	a.Update(tea.MouseReleaseMsg{X: 40, Y: 10, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: 60, Y: 10})
	if got := a.sidebarRect().Dx(); got != 40 {
		t.Fatalf("after release: %d wide", got)
	}
	if main := a.layout()[1]; main.Min.X != 41 {
		t.Errorf("the panes start right of the gap: %v", main)
	}
	feed(t, a, `<Space>%<Space>z<Space>z<Space>b<Space>b`)
	if got := a.sidebarRect().Dx(); got != 40 {
		t.Errorf("after split, zoom and fold: %d wide, want 40", got)
	}

	// the limits hold for the window as it is now; the dragged width comes back
	a.win().TreeW = 80
	for _, c := range [][2]int{{100, 50}, {160, 80}} {
		a.Update(tea.WindowSizeMsg{Width: c[0], Height: 45})
		if got := a.sidebarRect().Dx(); got != c[1] || a.win().TreeW != 80 {
			t.Errorf("window %d wide: sidebar %d, want %d (dragged %d)", c[0], got, c[1], a.win().TreeW)
		}
	}

	feed(t, a, "<Space>b")
	a.View()
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindTreeEdge {
			t.Fatal("a folded sidebar has no edge to drag")
		}
	}
}

// The sidebar's title is the session (§7.8): a click only focuses it; a
// narrow one keeps ⓪ <icon>, drops the « that folds it and then cuts the name.
func TestSidebarTitle(t *testing.T) {
	conn := ui.NerdIcons.Conn.Text
	for _, c := range []struct {
		icons string
		w     int
		want  string
	}{
		{"nerd", 16, "┌─ ⓪ " + conn + " dorae… ─┐"},
		{"nerd", 20, "┌─ ⓪ " + conn + " doraemon ───┐"},
		{"nerd", 28, "┌─ ⓪ " + conn + " doraemon ─────── « ─┐"},
		{"ascii", 24, "┌─ ⟨0⟩ @ doraemon ─ « ─┐"},
	} {
		a := sized(160, 45, c.icons)
		a.win().TreeW = c.w
		if top := string([]rune(strings.Split(a.render().String(), "\n")[0])[:c.w]); top != c.want {
			t.Errorf("%s %d: %q, want %q", c.icons, c.w, top, c.want)
		}
	}
	a := sized(160, 45, "nerd")
	click(a, uv.Pos(6, 0))
	if a.win().Focus != 0 || a.drop != nil {
		t.Error("clicking the title focuses the sidebar, nothing else")
	}
}

// [tree] width (F8.22) sets the sidebar of a window whose sidebar was never
// dragged: held to 16 and half the window, 24 at most under 100 columns; a
// width dragged or restored from the layout file goes before it.
func TestConfiguredSidebarWidth(t *testing.T) {
	for _, c := range []struct{ conf, w, want int }{
		{38, 160, 38}, {8, 160, 16}, {8, 99, 16}, {-3, 160, 16}, {0, 99, 16}, {38, 100, 38}, {38, 99, 24}, {20, 99, 20}, {38, 60, 24}, {38, 40, 24}, {32, 160, 32},
	} {
		cfg := config.Default()
		cfg.TreeWidth = c.conf
		a := sizedWith(c.w, 30, cfg)
		if got := a.sidebarRect().Dx(); got != c.want {
			t.Errorf("width %d in %d columns: %d, want %d", c.conf, c.w, got, c.want)
		}
		a.win().TreeW = 50
		if got := a.sidebarRect().Dx(); got != min(50, c.w/2) { // a dragged width goes before the narrow rule
			t.Errorf("width %d dragged to 50 in %d columns: %d", c.conf, c.w, got)
		}
	}
}

// A pane with more tabs than its bar holds scrolls it: gt and a click on a tab
// bring the current one in sight, and a click on ‹N or N› scrolls a screen
// (F17.1).
func TestTabBarScrolls(t *testing.T) {
	a := sized(160, 45, "nerd")
	p := a.focused()
	for range 9 {
		a.run("tab.new", 0)
	}
	inSight := func() bool {
		a.View()
		s := p.tabScroll
		return s.First <= p.Cur && p.Cur < s.First+s.Shown
	}
	for _, k := range []string{"1gt", "8gt", "gT", "10gt"} {
		feed(t, a, k)
		if !inSight() {
			t.Errorf("%s: tab %d is not shown, from %d, %d of them", k, p.Cur+1, p.tabScroll.First, p.tabScroll.Shown)
		}
	}
	feed(t, a, "1gt")
	a.View()
	if p.tabScroll.First != 0 || p.tabScroll.Shown >= 10 {
		t.Fatalf("1gt: %+v", p.tabScroll)
	}
	shown := p.tabScroll.Shown
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: p.ID, Action: fmt.Sprintf("tab.scroll %d 1", p.ID)}).Min)
	a.View()
	if p.tabScroll.First != shown || p.Cur != 0 {
		t.Errorf("a click on N›: %+v, tab %d", p.tabScroll, p.Cur+1)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: p.ID, Action: fmt.Sprintf("tab.scroll %d -1", p.ID)}).Min)
	a.View()
	if p.tabScroll.First != 0 {
		t.Errorf("a click on ‹N: %+v", p.tabScroll)
	}
}

func TestGoldenTabBarOverflow(t *testing.T) {
	a := sized(160, 45, "nerd")
	for range 9 {
		a.run("tab.new", 0)
	}
	feed(t, a, "8gt")
	golden.RequireEqual(t, a.render().String())
}

// paneButtons are the four buttons of a window's pane title (F19.3, §7.8).
var paneButtons = []string{"pane.split.right", "pane.split.below", "pane.zoom", "pane.close"}

func buttonAt(t *testing.T, a *App, pane int, action string) uv.Position {
	t.Helper()
	return find(t, a, ui.Target{Kind: ui.KindHint, Pane: pane, Action: action}).Min
}

// A pane's button acts on that pane, not on the one with the focus.
func TestPaneButtonsActOnTheirPane(t *testing.T) {
	for _, action := range paneButtons {
		t.Run(action, func(t *testing.T) {
			a := twoPanes(160, 45)
			a.win().focus(2)
			before := a.layout()
			click(a, buttonAt(t, a, 1, action))
			after := a.layout()
			switch action {
			case "pane.split.right", "pane.split.below":
				if len(after) != len(before)+1 || after[2] != before[2] || after[1] == before[1] || a.win().Focus == 2 {
					t.Errorf("pane 1 should split, 2 stay: %v -> %v, focus %d", before, after, a.win().Focus)
				}
			case "pane.zoom":
				if a.win().Zoom != 1 {
					t.Errorf("zoom %d, want pane 1", a.win().Zoom)
				}
			case "pane.close":
				if _, ok := after[1]; ok || len(after) != len(before)-1 || a.win().Focus != 2 {
					t.Errorf("pane 1 should close and 2 stay: %v, focus %d", after, a.win().Focus)
				}
			}
			if action != "pane.close" && a.win().Focus == 2 && action != "pane.zoom" {
				t.Errorf("the click should focus the pane it is on")
			}
		})
	}
}

func TestPaneButtonsPlacement(t *testing.T) {
	hasButtons := func(a *App, pane int) bool {
		a.View()
		n := 0
		for _, h := range a.hits {
			if h.Target.Kind == ui.KindHint && h.Target.Pane == pane && slices.Contains(paneButtons, h.Target.Action) {
				n++
			}
		}
		if n != 0 && n != len(paneButtons) {
			t.Errorf("pane %d: %d of the four buttons, all or none", pane, n)
		}
		return n > 0
	}
	a := twoPanes(220, 45) // wide enough for the query's title to keep them
	if !hasButtons(a, 1) || !hasButtons(a, 2) {
		t.Error("the data and the query pane have the buttons")
	}
	if hasButtons(a, a.win().Tree.ID) || hasButtons(a, a.resultPane(a.win()).ID) {
		t.Error("the sidebar and the result area have none")
	}
	a.splitPane(Horiz) // the new one is empty: the buttons act on the pane, not on a tab
	if p := a.focused(); len(p.Tabs) != 0 || !hasButtons(a, p.ID) {
		t.Errorf("an empty pane: %d tabs, buttons %v", len(p.Tabs), hasButtons(a, p.ID))
	}
	if n := a.render().String(); !strings.Contains(n, a.icons.Zoom.Text) || strings.Contains(n, a.icons.Unzoom.Text) {
		t.Error("not zoomed: expand, not compress")
	}
	a.toggleZoom()
	if n := a.render().String(); !strings.Contains(n, a.icons.Unzoom.Text) {
		t.Error("zoomed: the pane's button is compress")
	}
	if !hasButtons(a, a.focused().ID) {
		t.Error("the zoomed pane keeps the buttons")
	}
	a.toggleZoom()
	narrow := twoPanes(60, 24)
	narrow.resultPane(narrow.win())
	for id := range narrow.layout() {
		if id != narrow.win().Tree.ID && id != narrow.win().Result.ID && hasButtons(narrow, id) {
			t.Errorf("pane %d at %v is too narrow for the four", id, narrow.layout()[id])
		}
	}
}
