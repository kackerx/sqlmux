package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// CompleteItem is one row of a Complete list.
type CompleteItem struct {
	Text string
	Pos  []int  // match positions in Text
	Note string // dim, on the right: a column's type, 关键字, 值, a time
	Head bool   // a group's title (收藏, 历史), not a pick
}

// Complete is a list under an input with no filter of its own (§9.7): the
// completion candidates, and the WHERE history.
type Complete struct {
	Items    []CompleteItem
	Sel, Top int // Sel -1: nothing picked yet
}

const completeRows = 8

// CompleteBox is where a list of n items opens under the input cell at:
// from at's column, w wide and moved left to stay on screen; rows is how
// many items show.
func CompleteBox(screen uv.Rectangle, at uv.Position, w, n int) (box uv.Rectangle, rows int) {
	w = min(w, screen.Dx())
	rows = max(min(n, completeRows, screen.Max.Y-at.Y-3), 0) // below the input, in a border
	return uv.Rect(min(at.X, screen.Max.X-w), at.Y+1, w, rows+2), rows
}

// Draw paints the list in box, rows of it showing.
func (c Complete) Draw(f *Frame, box uv.Rectangle, rows int) {
	th := f.Theme
	if box.Dx() < 4 || rows == 0 {
		return
	}
	f.Region(box, Target{}) // a click inside is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	x0, x1 := box.Min.X+1, box.Max.X-1
	hl := uv.Style{Fg: th.Bg, Bg: th.Warn}
	for i := c.Top; i < min(c.Top+rows, len(c.Items)); i++ {
		it := c.Items[i]
		line := uv.Rect(x0, box.Min.Y+1+i-c.Top, x1-x0, 1)
		if it.Head {
			f.Text(x0+1, line.Min.Y, x1, it.Text, uv.Style{Fg: th.Dim, Bg: th.PaneBg, Attrs: uv.AttrBold})
			continue
		}
		st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		if f.Region(line, Target{Kind: KindRow, I: i}) || i == c.Sel {
			st.Bg = th.Select
		}
		f.Fill(line, st)
		nx := x1 - 1 - Width(it.Note)
		f.TextMatch(x0+1, line.Min.Y, nx-1, it.Text, it.Pos, st, hl)
		f.Text(nx, line.Min.Y, x1, it.Note, uv.Style{Fg: th.Dim, Bg: st.Bg})
	}
}
