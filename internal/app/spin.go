package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// spinEvery is how often the dots of a request on its way move (§7.8「转圈」).
var spinEvery = 150 * time.Millisecond

// spinTick is the one tick that moves every spinner: the program sends it
// while something on the screen has one, and not otherwise, so an idle
// program redraws nothing.
type spinTick struct{}

func spinTimer() tea.Cmd { return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinTick{} }) }

// spinning is whether anything on the screen has the dots: the status bar's
// busy, a run's placeholder in the result area, a table's row count being
// counted, a connection being connected in the tree (§7.8「转圈」).
func (a *App) spinning() bool {
	win := a.win()
	if c := a.conn(); c != nil && c.busy > 0 {
		return true
	}
	if r := win.Result; r != nil && !win.resultHidden {
		if rt := resultOf(r); rt != nil && rt.run != nil && !rt.run.done {
			return true
		}
	}
	for _, p := range win.Root.Leaves() {
		if t := dataOf(p); t != nil && t.counted == counting {
			return true
		}
	}
	if win.TreeOpen {
		for _, c := range a.sess.conns() {
			if c.State == connConnecting {
				return true
			}
		}
	}
	return false
}

// spinStart is the tick that starts the dots when there are some and none is
// on its way, at their first frame; the tick moves them on from there.
func (a *App) spinStart() tea.Cmd {
	if a.spinOn || !a.spinning() {
		return nil
	}
	a.spinOn, a.spin = true, 0
	return spinTimer()
}

// spinNext moves the dots on a frame, and ticks again while there are any.
func (a *App) spinNext() tea.Cmd {
	a.spin++
	if a.spinOn = a.spinning(); !a.spinOn {
		return nil
	}
	return spinTimer()
}
