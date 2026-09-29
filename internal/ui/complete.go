package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// CompleteItem is one row of a Complete list.
type CompleteItem struct {
	Icon Icon // before Text: a WHERE history's star or history (§9.7), in dim unless it has a color
	Text string
	Pos  []int  // match positions in Text
	Note string // dim, on the right: a column's type, 关键字, 值, a time
}

// Complete is a list under an input with no filter of its own (§9.7): the
// completion candidates, and the WHERE history.
type Complete struct {
	Items    []CompleteItem
	Sel, Top int // Sel -1: nothing picked yet
}

const completeRows = 8

// CompleteBox is where a list of n items opens under the input cell at, or
// over it when they don't all fit below and there is more room above
// (§10.2): from at's column, w wide and moved left to stay on screen; rows
// is how many items show.
func CompleteBox(screen uv.Rectangle, at uv.Position, w, n int) (box uv.Rectangle, rows int) {
	w, x := min(w, screen.Dx()), min(at.X, screen.Max.X-min(w, screen.Dx()))
	rows = max(min(n, completeRows, screen.Max.Y-at.Y-3), 0) // below the input, in a border
	if up := max(min(n, completeRows, at.Y-screen.Min.Y-2), 0); rows < min(n, completeRows) && up > rows {
		return uv.Rect(x, at.Y-up-2, w, up+2), up
	}
	return uv.Rect(x, at.Y+1, w, rows+2), rows
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
	for i := c.Top; i < min(c.Top+rows, len(c.Items)); i++ {
		it := c.Items[i]
		line := uv.Rect(x0, box.Min.Y+1+i-c.Top, x1-x0, 1)
		// picked on select, the one under the pointer on row: which ↵ takes
		// shows apart from where the pointer is (§10.2)
		st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		switch hover := f.Region(line, Target{Kind: KindRow, I: i}); {
		case i == c.Sel:
			st.Bg = th.Select
		case hover:
			st.Bg = th.Row
		}
		f.Fill(line, st)
		nx, x := x1-1-Width(it.Note), x0+1
		if it.Icon.Text != "" {
			x = f.Text(x, line.Min.Y, nx-1, it.Icon.Text+" ", it.Icon.On(uv.Style{Fg: th.Dim, Bg: st.Bg}))
		}
		f.TextMatch(x, line.Min.Y, nx-1, it.Text, it.Pos, st)
		f.Text(nx, line.Min.Y, x1, it.Note, uv.Style{Fg: th.Dim, Bg: st.Bg})
	}
}

// CellHint is what is wrong with a cell's edit, in a box of its own right
// at it (§10.7).
type CellHint struct{ Text string }

// CellHintRows is a hint box's height: its line in a border.
const CellHintRows = 3

func (h CellHint) Width() int { return Width(h.Text) + 4 }

func (h CellHint) Draw(f *Frame, box uv.Rectangle) {
	th := f.Theme
	if box.Dx() < 4 || box.Dy() < CellHintRows {
		return
	}
	f.Region(box, Target{}) // a click inside is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Error, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y+1, box.Max.X-1, h.Text, uv.Style{Fg: th.Error, Bg: th.PaneBg})
}
