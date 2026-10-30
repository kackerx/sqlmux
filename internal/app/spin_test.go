package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
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
	tab.recount = true // the same table asked again, now all of it fits on a page: nothing to count, nothing spins (F19.4)
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: tab.cols, page: tab.page, next: false})
	if a.spinning() || strings.Contains(a.render().String(), "●••") {
		t.Errorf("a table under a page: spinning %v", a.spinning())
	}
	tab.recount = true
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: tab.cols, page: tab.page, next: true})
	if !a.spinning() {
		t.Fatal("counting again")
	}
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

// A first page under LIMIT is the count: no COUNT is asked for, PAGE is 1/1
// at once, and the query bar keeps "3 行" where the full count does not fit;
// a page with another after it counts as before, and a count that was on its
// way for the table asked before is no answer (F19.4).
func TestFirstPageUnderLimitIsTheCount(t *testing.T) {
	a := wide(160, 45)
	more := loadOrders(t, a, 3)
	if more.counted != counting || more.out != 1 || a.queryBar(a.focused(), more).Keep != "" {
		t.Fatalf("a page with more after it: counted %v, %d on its way, keep %q", more.counted, more.out, a.queryBar(a.focused(), more).Keep)
	}
	stale := more.countSeq
	more.recount, more.out = true, more.out+1 // asked again, the first count still out
	a.Update(pageMsg{tab: more, seq: more.seq, cols: more.cols, page: more.page, next: false})
	bar := a.queryBar(a.focused(), more)
	if more.counted != countDone || more.count != 3 || more.out != 1 || !strings.HasPrefix(bar.Right, "auto · 3 行") || bar.Keep != "3 行" {
		t.Fatalf("all of it on the page: counted %v, count %d, %d on its way, %q, keep %q", more.counted, more.count, more.out, bar.Right, bar.Keep)
	}
	if n, ok := more.pages(); !ok || n != 1 || !strings.Contains(a.render().String(), " PAGE 1/1 ") {
		t.Errorf("pages %d %v", n, ok)
	}
	a.Update(countMsg{tab: more, seq: stale, n: 9999}) // the earlier one answers
	if more.count != 3 || more.counted != countDone || more.out != 0 {
		t.Errorf("the earlier count came in: %d, %v, %d on its way", more.count, more.counted, more.out)
	}
	more.shown.pageNo = 1 // the second page is not the table
	if bar := a.queryBar(a.focused(), more); bar.Keep != "" {
		t.Errorf("page 2: keep %q", bar.Keep)
	}
	more.shown.pageNo = 0
	if more.table.Kind = "k"; a.queryBar(a.focused(), more).Keep != "" { // a Redis key's bar is not a table's
		t.Error("a key keeps a count")
	}
	more.table.Kind = ""
	more.recount, more.out = true, more.out+1
	a.Update(pageMsg{tab: more, seq: more.seq, cols: more.cols, page: db.Result{Cols: more.page.Cols}, next: false}) // a WHERE that keeps nothing
	if bar := a.queryBar(a.focused(), more); bar.Keep != "0 行" || !strings.HasPrefix(bar.Right, "auto · 0 行") {
		t.Errorf("no rows: %q, keep %q", bar.Right, bar.Keep)
	}
	for _, w := range []int{160, 90, 70, 62} { // narrow: the bar still says how many
		a.Update(tea.WindowSizeMsg{Width: w, Height: 45})
		if !strings.Contains(a.render().String(), " 0 行") {
			t.Errorf("width %d: no count in\n%s", w, a.render().String())
		}
	}
}

// Every place that has the dots moves them on the one tick: the status bar,
// the run's placeholder, the count, a connection and a database in the tree
// (§7.8「转圈」).
func TestSpinFramesMove(t *testing.T) {
	var a *App
	frame := func(want string, in ...string) {
		t.Helper()
		screen := a.render().String()
		for _, s := range in {
			if !strings.Contains(screen, strings.ReplaceAll(s, "•••", want)) {
				t.Errorf("frame %q: no %q on\n%s", want, s, screen)
			}
		}
	}
	a = sized(160, 45, "nerd")
	a.sess.Conns[0].State = connConnecting
	frame("●••", "doraemon  •••")
	a.Update(spinTick{})
	frame("•●•", "doraemon  •••")

	a = wide(160, 45)
	loadOrders(t, a, 60)
	frame("●••", "auto · ••• 行")
	a.Update(spinTick{})
	frame("•●•", "auto · ••• 行")

	a, _ = inRun(t, &execDB{}, "select 1")
	a.Update(teaKey("<CR>"))
	frame("●••", "执行中 ••• · ", " busy ••• · ")
	a.Update(spinTick{})
	frame("•●•", "执行中 ••• · ", " busy ••• · ")
	a.Update(spinTick{})
	frame("••●", "执行中 ••• · ", " busy ••• · ")

	var alpha *Conn // a database of the connection, connecting
	a, _, alpha = withAlpha(t)
	alpha.State = connConnecting
	frame("●••", "alpha  •••")
	a.Update(spinTick{})
	frame("•●•", "alpha  •••")
}
