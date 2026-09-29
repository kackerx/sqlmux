package ui

import (
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
)

// ErrorBar is a database's error at the bottom of a pane's content, over
// its tab bar (§7.8「错误栏」): First on the first row in the error color,
// its middle cut to fit (§10.3), then More a row each, cut at the right,
// all on error_bg; the × at the first row's end closes it.
type ErrorBar struct {
	First Note
	More  []string
	Pane  int
}

// errorBarRows is the most rows a bar takes; the last reads … when More
// has more.
const errorBarRows = 6

// Rows is how tall b draws.
func (b ErrorBar) Rows() int { return min(1+len(b.More), errorBarRows) }

// Draw paints b over the last Rows rows of r.
func (b ErrorBar) Draw(f *Frame, r uv.Rectangle) {
	th := f.Theme
	n := min(b.Rows(), r.Dy())
	if n <= 0 || r.Dx() < 4 {
		return
	}
	r.Min.Y = r.Max.Y - n
	st := uv.Style{Fg: th.Fg, Bg: th.ErrorBg}
	f.Fill(r, st)
	x := r.Max.X - 3 // " × "
	close := uv.Style{Fg: th.Dim, Bg: th.ErrorBg}
	if f.Region(uv.Rect(x, r.Min.Y, 3, 1), Target{Kind: KindButton, Action: fmt.Sprintf("pane.error.close %d", b.Pane)}) {
		close.Bg = th.Select
	}
	f.Text(x, r.Min.Y, r.Max.X, " × ", close)
	f.Text(r.Min.X+1, r.Min.Y, x, b.First.Fit(x-r.Min.X-2), uv.Style{Fg: th.Error, Bg: th.ErrorBg})
	for i, l := range b.More[:n-1] {
		if i == errorBarRows-2 && len(b.More) > n-1 {
			l = "…"
		}
		f.Text(r.Min.X+1, r.Min.Y+1+i, r.Max.X-1, Truncate(l, r.Dx()-2), st)
	}
}
