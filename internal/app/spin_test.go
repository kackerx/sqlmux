package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/ui"
)

// The dots go round on one tick, started when something on the screen has
// them and stopped when nothing does: an idle program asks for no tick and
// redraws nothing (§7.8「转圈」, F19.2).
func TestSpinTick(t *testing.T) {
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45}); cmd != nil || a.spinOn || a.spinning() {
		t.Fatalf("idle: cmd %v, on %v", cmd != nil, a.spinOn)
	}
	c.busy = 1
	_, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	if cmd == nil || !a.spinOn || a.spin != 0 {
		t.Fatalf("busy: cmd %v, on %v, frame %d", cmd != nil, a.spinOn, a.spin)
	}
	if _, ok := cmd().(spinTick); !ok {
		t.Fatalf("the command is not the tick")
	}
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45}); cmd != nil {
		t.Error("a second tick asked for while one is on its way")
	}
	for i, want := range []string{"•●•", "••●", "•●•", "●••"} {
		_, cmd := a.Update(spinTick{})
		if cmd == nil || a.spin != i+1 || !strings.Contains(statusRow(a), " busy "+want+" · C-c 取消 ") {
			t.Fatalf("tick %d: cmd %v, frame %d, status %q", i+1, cmd != nil, a.spin, statusRow(a))
		}
	}
	c.busy = 0 // done: the tick that is due is the last
	if _, cmd := a.Update(spinTick{}); cmd != nil || a.spinOn {
		t.Fatalf("idle again: cmd %v, on %v", cmd != nil, a.spinOn)
	}
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45}); cmd != nil || strings.Contains(statusRow(a), "busy") {
		t.Error("it asks again with nothing to show")
	}
	c.busy = 1 // a new request starts at the first frame
	a.spin = 3
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 160, Height: 45}); cmd == nil || a.spin != 0 {
		t.Errorf("restarted: frame %d", a.spin)
	}
}

// Four places have the dots, and each of them keeps the tick going on its
// own: the status bar's busy, a run's placeholder, a table's count being
// counted and a connection being connected in the tree, which a folded
// sidebar does not show (§7.8「转圈」).
func TestSpinningWhere(t *testing.T) {
	a := sized(160, 45, "nerd")
	c := a.sess.Conns[0]
	if a.spinning() {
		t.Fatal("idle")
	}
	c.busy = 1
	if !a.spinning() || !strings.Contains(statusRow(a), " busy ●•• ") {
		t.Errorf("busy: %q", statusRow(a))
	}
	c.busy = 0

	a = wide(160, 45) // the data pane to the edge: the count has room
	c = a.sess.Conns[0]
	tab := loadOrders(t, a, 60) // more than a page, the count on its way
	if !a.spinning() || !strings.Contains(a.render().String(), "auto · ●•• 行") {
		t.Errorf("counting: spinning %v", a.spinning())
	}
	tab.next = false // all of it is on the page: nothing to count, nothing spins (F19.4)
	if a.spinning() || strings.Contains(a.render().String(), "●••") {
		t.Errorf("a table under a page: spinning %v", a.spinning())
	}
	tab.next = true
	a.Update(countMsg{tab: tab, seq: tab.countSeq, n: 6000})
	if a.spinning() {
		t.Error("counted")
	}

	c.State = connConnecting
	if !a.spinning() || !strings.Contains(a.render().String(), "doraemon  ●••") {
		t.Errorf("connecting: spinning %v", a.spinning())
	}
	a.win().TreeOpen = false
	if a.spinning() {
		t.Error("connecting, the sidebar folded")
	}
	a.win().TreeOpen, c.State = true, connReady

	d := &execDB{}
	ra, rc := inRun(t, d, "select 1")
	_ = rc
	ra.Update(teaKey("<CR>"))
	ra.sess.Conns[0].busy = 0 // the run's own, apart from the status bar's
	if !ra.spinning() || !strings.Contains(ra.render().String(), "执行中 ●•• · ") {
		t.Errorf("a run: spinning %v", ra.spinning())
	}
	ra.win().resultHidden = true
	if ra.spinning() {
		t.Error("a run with the result area hidden")
	}
}

// With ascii icons the dots are o's (§7.8「转圈」).
func TestSpinAscii(t *testing.T) {
	a := sized(160, 45, "ascii")
	a.icons = ui.ASCIIIcons
	a.sess.Conns[0].busy = 1
	if !strings.Contains(statusRow(a), " busy o.. ") {
		t.Errorf("status %q", statusRow(a))
	}
	a.spin = 2
	if !strings.Contains(statusRow(a), " busy ..o ") {
		t.Errorf("status %q", statusRow(a))
	}
}

// What is under a page keeps its count: "auto · 3 行" in the query bar, and
// "3 行" where that does not fit; a page that is not the whole table keeps
// the count on its way (F19.4).
func TestQueryBarRowsUnderAPage(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	bar := func() ui.QueryBar { return a.queryBar(a.focused(), tab) }
	if r := bar(); !strings.HasPrefix(r.Right, "auto · ●•• 行") || r.Keep != "" {
		t.Fatalf("more pages: %q, keep %q", r.Right, r.Keep)
	}
	tab.next = false
	if r := bar(); !strings.HasPrefix(r.Right, "auto · 3 行") || r.Keep != "3 行" {
		t.Fatalf("under a page: %q, keep %q", r.Right, r.Keep)
	}
	tab.shown.pageNo = 1 // the last page is not the table
	if r := bar(); r.Keep != "" {
		t.Errorf("page 2: keep %q", r.Keep)
	}
	tab.shown.pageNo = 0
	if tab.table.Kind = "k"; bar().Keep != "" { // a Redis key's bar is not a table's
		t.Errorf("a key: keep %q", bar().Keep)
	}
	tab.table.Kind = ""
	tab.page.Rows = nil // a WHERE that keeps nothing
	if r := bar(); r.Keep != "0 行" || !strings.HasPrefix(r.Right, "auto · 0 行") {
		t.Errorf("no rows: %q, keep %q", r.Right, r.Keep)
	}
	for _, w := range []int{160, 90, 70, 62} { // narrow: the bar still says how many
		a.Update(tea.WindowSizeMsg{Width: w, Height: 45})
		tab.page.Rows = tab.page.Rows[:0]
		if !strings.Contains(a.render().String(), " 0 行") {
			t.Errorf("width %d: no count in\n%s", w, a.render().String())
		}
	}
}
