package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Dropdown picks from a list under a filter input (§8.6, §7.8): the one-pick
// dropdowns (schema, LIMIT), and with Checks or Icons the COLS and ORDER lists.
type Dropdown struct {
	Title    string // on the top border; "" for none (the pick lists of F17.13)
	Search   Icon   // leads the input
	Input    Input
	Typing   bool     // the input has the keys: its cursor shows
	Items    []string // what passes the filter
	Pos      [][]int  // each item's match positions
	Notes    []string // dim, after each item (COLS, ORDER: the type and the key); nil for none
	Icons    []Icon   // info, before each item (connections: the engine's, ORDER: its place in the sort); nil for none
	Checks   []bool   // COLS: each item's box; nil for none
	Mark     int      // the item drawn in pk color (the current one); -1 for none
	Count    string   // right of the input (COLS: how many match)
	Hints    []Hint   // under the list (COLS: all / none; ORDER: its keys)
	Sel, Top int
	Pane     int // the pane the hints act on
}

const dropdownRows = 10

// hintRows is the hints in the rows of a box w wide: each row as many whole
// ones as fit, the ones that do not fit in two rows left out, the last first
// (F15.6).
func (d Dropdown) hintRows(w int) (rows [][]Hint) {
	hs := d.Hints
	for len(hs) > 0 && len(rows) < 2 {
		n := max(leadingHints(hs, w-4), 1) // one is always shown, cut by the box's edge if it must
		rows, hs = append(rows, hs[:n]), hs[n:]
	}
	return rows
}

// Box is where d opens: under anchor and left aligned with it, w wide, moved
// left to stay on screen; its hints are wrapped in it, not it widened to them
// (F15.6); rows is how many of its items show.
func (d Dropdown) Box(screen, anchor uv.Rectangle, w int) (box uv.Rectangle, rows int) {
	w = min(w, screen.Dx())
	x := min(anchor.Min.X, screen.Max.X-w)
	chrome := 4 // border, input, rule | items | border
	if hr := d.hintRows(w); len(hr) > 0 {
		chrome += 1 + len(hr) // rule, hints
	}
	rows = max(min(len(d.Items), dropdownRows, screen.Max.Y-anchor.Max.Y-chrome), 0)
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
	border := f.Border.Style(uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	if d.Title != "" {
		f.Text(box.Min.X+2, box.Min.Y, box.Max.X-2, " "+d.Title+" ", uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	}
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
		if d.Icons != nil { // a blank after it (§7.7)
			x = f.Text(x, line.Min.Y, x1, d.Icons[i].Text, d.Icons[i].On(uv.Style{Fg: th.Info, Bg: st.Bg})) + 1
		}
		note := ""
		if d.Notes != nil {
			note = d.Notes[i]
		}
		listText(f, x, line.Min.Y, x1, d.Items[i], d.Pos[i], note, st, uv.Style{Fg: th.Dim, Bg: st.Bg})
	}
	if hr := d.hintRows(box.Dx()); len(hr) > 0 && box.Dy() >= 5+len(hr) {
		rule(box.Max.Y - 2 - len(hr))
		for i, hs := range hr {
			hintRow(f, x0, box.Max.Y-1-len(hr)+i, x1, hs, dim, Target{Kind: KindHint, Pane: d.Pane})
		}
	}
	return cursor
}
