package ui

import uv "github.com/charmbracelet/ultraviolet"

// Log is the result area's log (§11): a line each, the newest at the
// bottom, from line Top, which is kept to where the last line still
// shows at the bottom at most.
type Log struct {
	Lines []LogLine
	Top   int
}

// LogLine is Head, then Tail in error when Err.
type LogLine struct {
	Head, Tail string
	Err        bool
}

// LogTop is where a log of n lines in h rows starts, from top.
func LogTop(top, n, h int) int { return max(min(top, n-h), 0) }

func (l Log) Draw(f *Frame, r uv.Rectangle) {
	th := f.Theme
	top := LogTop(l.Top, len(l.Lines), r.Dy())
	for y := r.Min.Y; y < r.Max.Y && top+y-r.Min.Y < len(l.Lines); y++ {
		line := l.Lines[top+y-r.Min.Y]
		tail := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		if line.Err {
			tail.Fg = th.Error
		}
		x := f.Text(r.Min.X+1, y, r.Max.X-1, line.Head, uv.Style{Fg: th.Fg, Bg: th.PaneBg})
		f.Text(x, y, r.Max.X-1, line.Tail, tail)
	}
}
