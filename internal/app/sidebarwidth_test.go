package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// The sidebar is widened and narrowed by its keys, in the tree and from any
// pane, and fitted to its longest name by = or a double click on its edge,
// which back again go to the configured width (§7.8).
func TestSidebarWidthKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	width := func() int { return a.sidebarRect().Dx() }
	w0 := width()

	a.focusPane(0)
	feed(t, a, ">")
	if width() != w0+treeStep {
		t.Fatalf("> in the tree: %d, was %d", width(), w0)
	}
	feed(t, a, "2<")
	if width() != w0-treeStep {
		t.Fatalf("2< in the tree: %d", width())
	}
	a.focusPane(2) // a query: the leader's keys
	feed(t, a, "<Esc><Space>>")
	if width() != w0 {
		t.Fatalf("SPC > from a query: %d", width())
	}
	for range 20 {
		feed(t, a, "<Space><lt>")
	}
	if width() != 16 {
		t.Fatalf("narrowed past its least: %d", width())
	}

	// = fits the longest name; again, back to the configured width
	c := a.sess.Conns[0]
	long := "a_table_whose_name_is_much_longer_than_the_sidebar"
	c.Tables = append(c.Tables, db.Table{Schema: "public", Name: long, Rows: 1})
	ns, _ := a.treeNodes()
	want := 0
	for _, n := range ns {
		want = max(want, n.Width())
	}
	if want <= w0 {
		t.Fatalf("the long name is not in the tree: widest %d", want)
	}
	a.focusPane(0)
	feed(t, a, "=")
	if width() != want {
		t.Fatalf("= : %d, want %d", width(), want)
	}
	if !strings.Contains(a.render().String(), long) {
		t.Error("fitted, the long name is still cut")
	}
	feed(t, a, "=")
	if width() != w0 {
		t.Fatalf("= again: %d, want the default %d", width(), w0)
	}

	// a double click on the edge does what = does
	edge := find(t, a, ui.Target{Kind: ui.KindTreeEdge})
	click(a, edge.Min)
	click(a, edge.Min)
	if width() != want {
		t.Fatalf("double click on the edge: %d, want %d", width(), want)
	}
	a.Update(tea.MouseMotionMsg{X: 70, Y: 10}) // no button held: no drag left over
	if width() != want {
		t.Fatalf("the double click left a drag: %d", width())
	}
}
