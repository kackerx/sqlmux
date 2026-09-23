package ui

import (
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
)

// Run is a stretch of text in one style.
type Run struct {
	Text  string
	Style uv.Style
}

// Segment is one flat block of the status bar; its runs carry their own
// 1-column padding (§7.8).
type Segment struct {
	Runs   []Run
	Action string // clicking it runs this
	Drop   int    // when too narrow, droppable segments go lowest Drop first, right to left; 0 never
}

func (s Segment) width() int {
	w := 0
	for _, r := range s.Runs {
		w += Width(r.Text)
	}
	return w
}

// StatusLine is the one-row bar at the bottom (B-01~B-03, §7.8): Left from
// the left edge, Info then Right against the right edge. When space runs out,
// Info goes first, then droppable segments, and last the name in Left[0]'s
// second run (the session name) is cut.
type StatusLine struct {
	Left  []Segment
	Info  string // mode extra info, ellipsized
	Right []Segment
}

// minInfo is the narrowest the extra info is still worth showing.
const minInfo = 12

func (s StatusLine) Draw(f *Frame, r uv.Rectangle) {
	th := f.Theme
	f.Fill(r, uv.Style{Bg: th.Row})
	left, right := dropToFit(s.Left, s.Right, r.Dx())
	if over := segsWidth(left) + segsWidth(right) - r.Dx(); over > 0 && len(left) > 0 && len(left[0].Runs) > 1 {
		left[0].Runs = slices.Clone(left[0].Runs)
		name := &left[0].Runs[1]
		name.Text = Truncate(name.Text, Width(name.Text)-over)
	}
	x := r.Min.X
	for _, seg := range left {
		x = drawSegment(f, x, r.Min.Y, r.Max.X, seg)
	}
	rx := r.Max.X - segsWidth(right)
	// Info is the first to go: once any segment had to be dropped, it's gone too.
	whole := len(left)+len(right) == len(s.Left)+len(s.Right)
	if spare := rx - x; s.Info != "" && whole && spare >= minInfo {
		info := " " + Truncate(s.Info, spare-2) + " "
		f.Text(rx-Width(info), r.Min.Y, rx, info, uv.Style{Fg: th.Dim, Bg: th.Row})
	}
	for _, seg := range right {
		rx = drawSegment(f, rx, r.Min.Y, r.Max.X, seg)
	}
}

// dropToFit removes droppable segments until both groups fit in w: lowest
// Drop first, and among equals the rightmost first. It returns copies.
func dropToFit(left, right []Segment, w int) ([]Segment, []Segment) {
	left, right = slices.Clone(left), slices.Clone(right)
	for segsWidth(left)+segsWidth(right) > w {
		best, side, at := 0, -1, -1
		for si, g := range [][]Segment{left, right} {
			for i, seg := range g {
				if seg.Drop > 0 && (best == 0 || seg.Drop < best || seg.Drop == best && si >= side) {
					best, side, at = seg.Drop, si, i
				}
			}
		}
		switch side {
		case -1:
			return left, right
		case 0:
			left = slices.Delete(left, at, at+1)
		default:
			right = slices.Delete(right, at, at+1)
		}
	}
	return left, right
}

func segsWidth(ss []Segment) int {
	w := 0
	for _, s := range ss {
		w += s.width()
	}
	return w
}

func drawSegment(f *Frame, x, y, right int, s Segment) int {
	start := x
	for _, r := range s.Runs {
		x = f.Text(x, y, right, r.Text, r.Style)
	}
	if s.Action != "" {
		f.Region(uv.Rect(start, y, x-start, 1), Target{Kind: KindHint, Action: s.Action})
	}
	return x
}
