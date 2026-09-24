package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Dropdown picks from a list under a filter input (§8.6, §7.8): the one-pick
// dropdowns (schema, ORDER, LIMIT), and with Checks the COLS list.
type Dropdown struct {
	Search   Icon // leads the input
	Input    Input
	Typing   bool     // the input has the keys: its cursor shows
	Items    []string // what passes the filter
	Pos      [][]int  // each item's match positions
	Notes    []string // dim, after each item (COLS: the type and the key); nil for none
	Checks   []bool   // COLS: each item's box; nil for none
	Mark     int      // the item drawn in pk color (the current one); -1 for none
	Count    string   // right of the input (COLS: how many match)
	Hints    []Hint   // under the list (COLS: all / none)
	Sel, Top int
	Pane     int // the pane the hints act on
}

const dropdownRows = 10

// DropdownBox is where a dropdown of n items opens: under anchor and left
// aligned with it, w wide, moved left to stay on screen; rows is how many
// items show. hints adds a row for Hints.
func DropdownBox(screen, anchor uv.Rectangle, w, n int, hints bool) (box uv.Rectangle, rows int) {
	w = min(w, screen.Dx())
	x := min(anchor.Min.X, screen.Max.X-w)
	chrome := 4 // border, input, rule | items | border
	if hints {
		chrome += 2 // rule, hints
	}
	rows = max(min(n, dropdownRows, screen.Max.Y-anchor.Max.Y-chrome), 0)
	return uv.Rect(x, anchor.Max.Y, w, rows+chrome), rows
}

// Draw paints the dropdown in box, which shows rows items, and returns
// where the input's cursor goes, (-1, -1) unless Typing.
func (d Dropdown) Draw(f *Frame, box uv.Rectangle, rows int) uv.Position {
	th := f.Theme
	f.Region(f.Bounds(), Target{Kind: KindBackdrop}) // a click outside closes it
	if box.Dx() < 6 || box.Dy() < 4 {
		return uv.Pos(-1, -1)
	}
	f.Region(box, Target{}) // inside, a click is not outside
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	f.Fill(box, bg)
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	x0, x1, y := box.Min.X+2, box.Max.X-2, box.Min.Y+1
	x := f.Text(x0, y, x1, d.Search.Text, d.Search.On(uv.Style{Fg: th.Info, Bg: th.PaneBg}))
	cx := x1 - Width(d.Count)
	f.Text(cx, y, x1, d.Count, dim)
	cursor := d.Input.Draw(f, uv.Rect(x+1, y, max(cx-x-2, 0), 1), bg)
	if !d.Typing {
		cursor = uv.Pos(-1, -1)
	}
	rule := func(y int) {
		f.Text(box.Min.X+1, y, box.Max.X-1, strings.Repeat("─", box.Dx()-2), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	rule(y + 1)
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
		x := x0
		if d.Checks != nil {
			box := "[ ] "
			if d.Checks[i] {
				box = "[x] "
			}
			x = f.Text(x, line.Min.Y, x1, box, st)
		}
		note := ""
		if d.Notes != nil {
			note = d.Notes[i]
		}
		nx := x1 - Width(note)
		f.TextMatch(x, line.Min.Y, nx-1, d.Items[i], d.Pos[i], st, hl)
		f.Text(nx, line.Min.Y, x1, note, uv.Style{Fg: th.Dim, Bg: st.Bg})
	}
	if len(d.Hints) > 0 && box.Dy() >= 6 {
		rule(box.Max.Y - 3)
		hintRow(f, x0, box.Max.Y-2, x1, d.Hints, dim, Target{Kind: KindHint, Pane: d.Pane})
	}
	return cursor
}
