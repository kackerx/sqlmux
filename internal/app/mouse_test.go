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
	body := strings.Split(a.render().String(), "\n")[r.Min.Y+1]
	if !strings.Contains(body, fakeRows()[wheelStep]) {
		t.Errorf("the data pane should start %d lines down: %q", wheelStep, body)
	}
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: tea.MouseWheelUp})
	a.Update(tea.MouseWheelMsg{X: r.Min.X + 5, Y: r.Min.Y + 5, Button: tea.MouseWheelUp})
	if data.Scroll != 0 {
		t.Fatalf("scrolling up stops at the top, got %d", data.Scroll)
	}
}
