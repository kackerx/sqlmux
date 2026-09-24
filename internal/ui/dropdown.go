package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Dropdown picks one item, a filter input on top (§8.6): the schema
// dropdown, the tree's now and the console's later.
type Dropdown struct {
	Search   Icon // leads the input
	Input    Input
	Items    []string // what passes the filter
	Pos      [][]int  // each item's match positions
	Mark     int      // the item drawn in pk color (the current schema); -1 for none
	Sel, Top int
}

const dropdownRows = 10

// DropdownBox is where a dropdown of n items opens: under anchor and left
// aligned with it, w wide, moved left to stay on screen; rows is how many
// items show.
func DropdownBox(screen, anchor uv.Rectangle, w, n int) (box uv.Rectangle, rows int) {
	w = min(w, screen.Dx())
	x := min(anchor.Min.X, screen.Max.X-w)
	// border, input, rule | items | border
	rows = max(min(n, dropdownRows, screen.Max.Y-anchor.Max.Y-4), 0)
	return uv.Rect(x, anchor.Max.Y, w, rows+4), rows
}

// Draw paints the dropdown in box, which shows rows items, and returns
// where the input's cursor goes.
func (d Dropdown) Draw(f *Frame, box uv.Rectangle, rows int) uv.Position {
	th := f.Theme
	f.Region(f.Bounds(), Target{Kind: KindBackdrop}) // a click outside closes it
	if box.Dx() < 6 || box.Dy() < 4 {
		return uv.Pos(-1, -1)
	}
	f.Region(box, Target{}) // inside, a click is not outside
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	f.Fill(box, bg)
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	x0, x1, y := box.Min.X+2, box.Max.X-2, box.Min.Y+1
	x := f.Text(x0, y, x1, d.Search.Text, d.Search.On(uv.Style{Fg: th.Info, Bg: th.PaneBg}))
	cursor := d.Input.Draw(f, uv.Rect(x+1, y, max(x1-x-1, 0), 1), bg)
	f.Text(box.Min.X+1, y+1, box.Max.X-1, strings.Repeat("─", box.Dx()-2), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	hl := uv.Style{Fg: th.Bg, Bg: th.Warn}
	for i := d.Top; i < min(d.Top+rows, len(d.Items)); i++ {
		line := uv.Rect(box.Min.X+1, y+2+i-d.Top, box.Dx()-2, 1)
		st := bg
		switch hover := f.Region(line, Target{Kind: KindRow, I: i}); {
		case i == d.Sel:
			st.Bg = th.Select
		case hover:
			st.Bg = th.Row
		}
		if i == d.Mark {
			st.Fg = th.PK
		}
		f.Fill(line, st)
		f.TextMatch(x0, line.Min.Y, x1, d.Items[i], d.Pos[i], st, hl)
	}
	return cursor
}
