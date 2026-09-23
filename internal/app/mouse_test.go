package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/ui"
)

// find returns the first hit region matching t's kind, pane and action.
func find(t *testing.T, a *App, want ui.Target) uv.Rectangle {
	t.Helper()
	a.View()
	for _, h := range a.hits {
		if h.Target.Kind == want.Kind && h.Target.Pane == want.Pane && h.Target.Action == want.Action && h.Target.I == want.I {
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
	a := sized(160, 45, "nerd")
	r := a.layout()[2]
	click(a, uv.Pos(r.Min.X+5, r.Min.Y+5))
	if a.win().Focus != 2 {
		t.Fatalf("focus %d after clicking the console", a.win().Focus)
	}
}

func TestDoubleClickTitleZooms(t *testing.T) {
	a := sized(160, 45, "nerd")
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
	a := sized(160, 45, "nerd")
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
	a := sized(160, 45, "nerd")
	run := find(t, a, ui.Target{Kind: ui.KindHint, Pane: 2, Action: "console.run"})
	before := a.render().Buf.CellAt(run.Min.X, run.Min.Y).Style.Bg
	a.Update(tea.MouseMotionMsg{X: run.Min.X, Y: run.Min.Y})
	if after := a.render().Buf.CellAt(run.Min.X, run.Min.Y).Style.Bg; after == before {
		t.Fatalf("hovering ▶ run left its background at %v", before)
	}
}

// The wheel scrolls the pane under the pointer, whatever has focus.
func TestWheelScrollsPaneUnderPointer(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.win().Focus = 2
	r := a.layout()[1]
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: tea.MouseWheelDown})
	data, cons := a.win().Root.Leaves()[0], a.win().Root.Leaves()[1]
	if data.Scroll != wheelStep || cons.Scroll != 0 {
		t.Fatalf("data scroll %d, console %d", data.Scroll, cons.Scroll)
	}
	// inside the border: the WHERE line, the grid's header and rule, then rows
	first := strings.Split(a.render().String(), "\n")[r.Min.Y+4]
	if !strings.Contains(first, " "+data.Rows[wheelStep][0]+" ") {
		t.Errorf("the data pane should start %d rows down: %q", wheelStep, first)
	}
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: tea.MouseWheelUp})
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: tea.MouseWheelUp})
	if data.Scroll != 0 {
		t.Fatalf("scrolling up stops at the top, got %d", data.Scroll)
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
		{"console", func(a *App) uv.Position { r := a.layout()[2]; return uv.Pos(r.Min.X+5, r.Min.Y+5) }, 2},
		{"sidebar", func(a *App) uv.Position { return uv.Pos(5, 10) }, 0},
		{"status bar", func(*App) uv.Position { return statusBar }, 1},
	} {
		a := sized(160, 45, "nerd")
		feed(t, a, "<Space>q")
		click(a, c.at(a))
		if a.paneNumbers || a.win().Focus != c.focus {
			t.Errorf("%s: numbers %v, focus %d, want %d", c.name, a.paneNumbers, a.win().Focus, c.focus)
		}
		if feed(t, a, ":"); a.palette == nil {
			t.Errorf("%s: the key after the click was swallowed", c.name)
		}
	}
}

// The gap right of the sidebar drags its width, from 16 columns to half the
// window; the width is the window's, so it outlasts splits, zooms and folds
// (§7.8).
func TestDragSidebarEdge(t *testing.T) {
	a := sized(160, 45, "nerd")
	edge := find(t, a, ui.Target{Kind: ui.KindBorder, I: -1})
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

	feed(t, a, "<Space>b")
	a.View()
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindBorder && h.Target.I == -1 {
			t.Fatal("a folded sidebar has no edge to drag")
		}
	}
}

// The sidebar's title is the schema, one button for tree.schema (§7.8).
func TestSidebarSchemaTitle(t *testing.T) {
	a := sized(160, 45, "nerd")
	top := strings.Split(a.render().String(), "\n")[0]
	if !strings.HasPrefix(top, "┌─ ⟨0⟩ "+ui.NerdIcons.Schema.Text+" public ▾ ─") || !strings.Contains(top, "SPC b ─┐") {
		t.Fatalf("title: %q", top)
	}
	r := find(t, a, ui.Target{Kind: ui.KindHint, Action: "tree.schema"})
	if r.Min.X != 2 || r.Dx() != ui.Width(" ⟨0⟩ "+ui.NerdIcons.Schema.Text+" public ▾ ") {
		t.Errorf("the button covers the whole title: %v", r)
	}
	click(a, uv.Pos(r.Min.X+5, 0))
	if a.win().Focus != 0 {
		t.Error("clicking the title focuses the sidebar (tree.schema itself is M1's)")
	}
}
