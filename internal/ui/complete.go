package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// CompleteItem is one row of a Complete list.
type CompleteItem struct {
	Icon Icon // before Text: a WHERE history's star or history (§9.7), in dim unless it has a color
	Text string
	Pos  []int  // match positions in Text
	Note string // dim, on the right: a column's type, 关键字, 值, a time
	Dim  bool   // Text in dim: a list with nothing in it says so (F18.9)
	Sep  bool   // no item: a rule between two groups, in sep, with no region to click (F8.14)
}

// Complete is a list under an input with no filter of its own (§9.7): the
// completion candidates, and the WHERE history.
type Complete struct {
	Items    []CompleteItem
	Sel, Top int    // Sel -1: nothing picked yet
	Act      string // the Action of a click on a row, for a list the click has to tell from other rows (F10.6)
	Foot     []Hint // a last row of dim hints in the box, "Key Label · …", when there are some (F8.14)
}

// CompleteRows is how many items a completion list shows (§9.7); the WHERE's
// history and favorites show more (F10.1).
const CompleteRows = 8

// CompleteBox is where a list of n items opens under the input cell at, or
// over it when they don't all fit below and there is more room above
// (§10.2): from at's column, w wide and moved left to stay on screen; rows
// is how many items show, at most limit; foot is the rows under them, 0 or 1 (F8.14).
func CompleteBox(screen uv.Rectangle, at uv.Position, w, n, foot, limit int) (box uv.Rectangle, rows int) {
	w, x := min(w, screen.Dx()), min(at.X, screen.Max.X-min(w, screen.Dx()))
	rows = max(min(n, limit, screen.Max.Y-at.Y-3-foot), 0) // below the input, in a border
	if up := max(min(n, limit, at.Y-screen.Min.Y-2-foot), 0); rows < min(n, limit) && up > rows {
		return uv.Rect(x, at.Y-up-2-foot, w, up+2+foot), up
	}
	return uv.Rect(x, at.Y+1, w, rows+2+foot), rows
}

// Draw paints the list in box, rows of it showing.
func (c Complete) Draw(f *Frame, box uv.Rectangle, rows int) {
	th := f.Theme
	if box.Dx() < 4 || rows == 0 && len(c.Foot) == 0 { // a box of its foot alone is one (F21.7)
		return
	}
	f.Region(box, Target{}) // a click inside is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := f.Lines(true).Style(uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	x0, x1 := box.Min.X+1, box.Max.X-1
	for i := c.Top; i < min(c.Top+rows, len(c.Items)); i++ {
		it := c.Items[i]
		line := uv.Rect(x0, box.Min.Y+1+i-c.Top, x1-x0, 1)
		if it.Sep {
			f.Text(x0, line.Min.Y, x1, strings.Repeat("─", line.Dx()), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
			continue
		}
		// picked on select, the one under the pointer on row: which ↵ takes
		// shows apart from where the pointer is (§10.2)
		st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		switch hover := f.Region(line, Target{Kind: KindRow, I: i, Action: c.Act}); {
		case i == c.Sel:
			st.Bg = th.Select
		case hover:
			st.Bg = th.Row
		}
		f.Fill(line, st)
		if it.Dim {
			st.Fg = th.Dim
		}
		x := x0 + 1
		if it.Icon.Text != "" {
			x = f.Text(x, line.Min.Y, x1-1, it.Icon.Text+" ", it.Icon.On(uv.Style{Fg: th.Dim, Bg: st.Bg}))
		}
		listText(f, x, line.Min.Y, x1-1, it.Text, it.Pos, it.Note, st, uv.Style{Fg: th.Dim, Bg: st.Bg})
	}
	if len(c.Foot) > 0 {
		hintRow(f, x0+1, box.Max.Y-2, x1, c.Foot, uv.Style{Fg: th.Dim, Bg: th.PaneBg}, Target{Kind: KindButton})
	}
}

// FootWidth is the box width the foot hints need.
func (c Complete) FootWidth() int { return hintRowWidth(c.Foot) + 4 }

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
	border := f.Lines(true).Style(uv.Style{Fg: th.Error, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y+1, box.Max.X-1, h.Text, uv.Style{Fg: th.Error, Bg: th.PaneBg})
}
