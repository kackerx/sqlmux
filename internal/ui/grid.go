package ui

import (
	"fmt"
	"slices"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// GridCol is one column of a Grid.
type GridCol struct {
	Name    string
	PK      bool // header gets the key icon
	Numeric bool // right-aligned, number color
}

// Grid is a data table in the §7.6 style: │ between columns and after the
// row numbers, a ─┼─ rule under the header, zebra rows, and the current row
// and cell highlighted. The caller supplies the data.
type Grid struct {
	Cols     []GridCol
	Rows     [][]string
	Top      int // index of the first row shown (scrolling)
	Row, Col int // current cell; -1 for none
	Focused  bool
	Key      string // key icon for primary key headers
}

// maxColWidth caps a column's wish (§7.6).
const maxColWidth = 40

// widths sizes each column (§7.6): the larger of its header and the 90th
// percentile of its values, at most maxColWidth; squeezed in proportion when
// they don't fit in room, but never below the header.
func (g Grid) widths(room int) []int {
	ws := make([]int, len(g.Cols))
	heads := make([]int, len(g.Cols))
	total := 0
	for c, col := range g.Cols {
		heads[c] = Width(g.header(col))
		vals := make([]int, 0, len(g.Rows))
		for _, r := range g.Rows {
			if c < len(r) {
				vals = append(vals, Width(r[c]))
			}
		}
		p90 := 0
		if len(vals) > 0 {
			slices.Sort(vals)
			p90 = vals[(len(vals)*9+9)/10-1]
		}
		ws[c] = min(max(heads[c], p90), maxColWidth)
		total += ws[c]
	}
	if total > room && total > 0 {
		for c := range ws {
			ws[c] = max(ws[c]*room/total, heads[c])
		}
	}
	return ws
}

func (g Grid) header(col GridCol) string {
	if col.PK && g.Key != "" {
		return g.Key + " " + col.Name
	}
	return col.Name
}

func (g Grid) Draw(f *Frame, area uv.Rectangle) {
	th := f.Theme
	if area.Dx() <= 0 || area.Dy() <= 0 || len(g.Cols) == 0 {
		return
	}
	noW := Width(fmt.Sprint(len(g.Rows)))
	// each cell: 1 space, content, 1 space; │ after the row numbers and between columns
	room := area.Dx() - (noW + 2 + 1) - 3*len(g.Cols) + 1
	ws := g.widths(room)
	line := uv.Style{Fg: th.Sep, Bg: th.PaneBg}

	// seps are the x of each │: after the row numbers, then between columns.
	seps := []int{area.Min.X + noW + 2}
	for _, w := range ws[:len(ws)-1] {
		seps = append(seps, seps[len(seps)-1]+w+3)
	}
	cellX := func(c int) int { return seps[c] + 2 }

	y := area.Min.Y
	for c, col := range g.Cols {
		f.Text(cellX(c), y, min(cellX(c)+ws[c], area.Max.X), Truncate(g.header(col), ws[c]), uv.Style{Fg: th.Func, Bg: th.PaneBg, Attrs: uv.AttrBold})
	}
	for _, x := range seps {
		f.Text(x, y, area.Max.X, "│", line)
	}
	if y++; y >= area.Max.Y {
		return
	}
	f.Text(area.Min.X, y, area.Max.X, strings.Repeat("─", area.Dx()), line)
	for _, x := range seps {
		f.Text(x, y, area.Max.X, "┼", line)
	}
	y++

	for i := g.Top; i < len(g.Rows) && y < area.Max.Y; i, y = i+1, y+1 {
		bg := th.PaneBg
		switch {
		case i == g.Row:
			bg = th.Row
		case (i+1)%2 == 0: // even row numbers: zebra
			bg = th.RowAlt
		}
		f.Fill(uv.Rect(area.Min.X, y, area.Dx(), 1), uv.Style{Bg: bg})
		no := fmt.Sprintf("%*d", noW, i+1)
		f.Text(area.Min.X+1, y, area.Max.X, no, uv.Style{Fg: th.Dim, Bg: bg})
		for c, col := range g.Cols {
			v := ""
			if c < len(g.Rows[i]) {
				v = Truncate(g.Rows[i][c], ws[c])
			}
			st := uv.Style{Fg: th.Fg, Bg: bg}
			x := cellX(c)
			if col.Numeric {
				st.Fg = th.Number
				x += ws[c] - Width(v)
			}
			if i == g.Row && c == g.Col {
				st.Bg = th.CursorBlur
				if g.Focused {
					st.Bg = th.Cursor
				}
				f.Fill(uv.Rect(cellX(c)-1, y, ws[c]+2, 1).Intersect(area), uv.Style{Bg: st.Bg})
			}
			f.Text(x, y, min(cellX(c)+ws[c], area.Max.X), v, st)
		}
		for _, x := range seps {
			f.Text(x, y, area.Max.X, "│", uv.Style{Fg: th.Sep, Bg: bg})
		}
	}
}
