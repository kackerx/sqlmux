package ui

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
)

// GridCol is one column of a Grid.
type GridCol struct {
	Name string
	PK   bool // header gets the key icon
	Type ColType
}

// ColType is the class of a column's values: it picks their color, and
// numbers are right-aligned (§7.6).
type ColType int

const (
	ColOther ColType = iota
	ColNumber
	ColString
	ColTime
	ColBool
	ColJSON
)

// Grid is a data table in the §7.6 style: │ between columns and after the
// row numbers, a ─┼─ rule under the header, zebra rows, and the current row
// and cell highlighted. Positions are the data's, whichever way it is drawn:
// Transpose turns records into columns and fields into rows (G-05). Every
// row has a value for each of Cols.
type Grid struct {
	Cols      []GridCol
	Rows      [][]db.Val
	First     int // the row number before Rows[0]: the page's offset
	Row, Col  int // the current cell; -1 for none
	Top, Left int // the first record and field shown
	Transpose bool
	Focused   bool
	Key       Icon // for primary key headers
	Pane      int
}

// maxColWidth caps a column's wish (§7.6).
const maxColWidth = 40

// Cell is how a value reads in a grid (§7.6): NULL as <null>; a newline as
// ↵, a tab as a space and any other control character dropped, so nothing
// the value holds can drive the terminal.
func Cell(v db.Val) string {
	if v.Null {
		return "<null>"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return '↵'
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, v.S)
}

// view is the grid as drawn: screen rows and columns, whichever way the
// data is turned.
type view struct {
	g          Grid
	rows, cols int
}

func (g Grid) view() view {
	if g.Transpose {
		return view{g, len(g.Cols), len(g.Rows)}
	}
	return view{g, len(g.Rows), len(g.Cols)}
}

// data is the record and field at screen row r, column c.
func (v view) data(r, c int) (rec, field int) {
	if v.g.Transpose {
		return c, r
	}
	return r, c
}

// screen is where record rec, field field is drawn.
func (v view) screen(rec, field int) (r, c int) {
	if v.g.Transpose {
		return field, rec
	}
	return rec, field
}

// val is the value at screen row r, column c, and its field's type.
func (v view) val(r, c int) (db.Val, ColType) {
	rec, field := v.data(r, c)
	return v.g.Rows[rec][field], v.g.Cols[field].Type
}

// head is the header of screen column c: a field's name, or a record's
// number when turned.
func (v view) head(c int) string {
	if v.g.Transpose {
		return v.g.number(c)
	}
	return v.g.header(v.g.Cols[c])
}

// label is what leads screen row r: a record's number, or a field's name.
func (v view) label(r int) string {
	if v.g.Transpose {
		return v.g.header(v.g.Cols[r])
	}
	return v.g.number(r)
}

func (g Grid) number(rec int) string { return strconv.Itoa(g.First + rec + 1) }

func (g Grid) header(col GridCol) string {
	if col.PK && g.Key.Text != "" {
		return g.Key.Text + " " + col.Name
	}
	return col.Name
}

// labelWidth is the width of the column that leads each row.
func (v view) labelWidth() int {
	w := 0
	for r := range v.rows {
		w = max(w, Width(v.label(r)))
	}
	return min(w, maxColWidth)
}

// widths sizes each screen column (§7.6): the larger of its header and the
// 90th percentile of its values, at most maxColWidth; squeezed in proportion
// when they don't fit in room, but never below the header. What still
// doesn't fit scrolls. Turned, records are not squeezed: a page of them
// never fits, and squeezed to their numbers they would show nothing.
func (v view) widths(room int) []int {
	ws := make([]int, v.cols)
	heads := make([]int, v.cols)
	total := 0
	for c := range v.cols {
		heads[c] = Width(v.head(c))
		vals := make([]int, 0, v.rows)
		for r := range v.rows {
			val, _ := v.val(r, c)
			vals = append(vals, Width(Cell(val)))
		}
		p90 := 0
		if len(vals) > 0 {
			slices.Sort(vals)
			p90 = vals[(len(vals)*9+9)/10-1]
		}
		ws[c] = min(max(heads[c], p90), maxColWidth)
		total += ws[c]
	}
	if total > room && total > 0 && !v.g.Transpose {
		for c := range ws {
			ws[c] = max(ws[c]*room/total, heads[c])
		}
	}
	return ws
}

// layout is where the grid's columns go in area: the label column's width,
// each screen column's width, and how many rows of data show.
func (v view) layout(area uv.Rectangle) (labelW int, ws []int, rows int) {
	labelW = v.labelWidth()
	// each cell: 1 space, content, 1 space; │ after the labels and between columns
	room := area.Dx() - (labelW + 2 + 1) - 3*v.cols + 1
	return labelW, v.widths(room), max(area.Dy()-2, 0) // the header and its rule
}

// View is Top and Left moved just enough for the current cell to show
// whole in area, as vim scrolls: callers store it after the cursor moves.
func (g Grid) View(area uv.Rectangle) (top, left int) {
	v := g.view()
	labelW, ws, rows := v.layout(area)
	voff, hoff := v.screen(g.Top, g.Left)
	cr, cc := v.screen(max(g.Row, 0), max(g.Col, 0))
	voff = max(min(voff, cr, v.rows-rows), cr-rows+1, 0)
	hoff = max(min(hoff, cc, v.cols-1), 0)
	x0 := area.Min.X + labelW + 3 // the first column's cell
	for hoff < cc && x0+colSpan(ws[hoff:cc+1]) > area.Max.X {
		hoff++
	}
	return v.data(voff, hoff)
}

// Scroll is the wheel (§7.6): the view moved dr rows and dc columns on
// screen, at most until the last row sits at the bottom, and the cursor
// pulled back into it. It returns Top, Left, Row and Col.
func (g Grid) Scroll(area uv.Rectangle, dr, dc int) (top, left, row, col int) {
	v := g.view()
	labelW, ws, rows := v.layout(area)
	voff, hoff := v.screen(g.Top, g.Left)
	cr, cc := v.screen(g.Row, g.Col)
	voff = max(min(voff+dr, v.rows-rows), 0)
	hoff = max(min(hoff+dc, v.cols-1), 0)
	last := hoff // the last column that shows whole, at least the first
	for c := hoff + 1; c < v.cols && area.Min.X+labelW+3+colSpan(ws[hoff:c+1]) <= area.Max.X; c++ {
		last = c
	}
	cr = max(min(cr, voff+rows-1), voff)
	cc = max(min(cc, last), hoff)
	top, left = v.data(voff, hoff)
	row, col = v.data(cr, cc)
	return top, left, row, col
}

// colSpan is how far columns of widths ws reach from the first one's cell.
func colSpan(ws []int) int {
	x := 0
	for _, w := range ws {
		x += w + 3
	}
	return x - 2
}

func (g Grid) Draw(f *Frame, area uv.Rectangle) {
	th := f.Theme
	v := g.view()
	if area.Dx() <= 0 || area.Dy() <= 0 || len(g.Cols) == 0 {
		return
	}
	labelW, ws, _ := v.layout(area)
	g.Top, g.Left = g.View(area)
	voff, hoff := v.screen(g.Top, g.Left)
	cr, cc := v.screen(g.Row, g.Col)
	line := uv.Style{Fg: th.Sep, Bg: th.PaneBg}
	fg := [...]color.Color{ColOther: th.Fg, ColNumber: th.Number, ColString: th.String, ColTime: th.Time, ColBool: th.Bool, ColJSON: th.JSON}
	name := uv.Style{Fg: th.Func, Bg: th.PaneBg, Attrs: uv.AttrBold}
	num := func(current bool, bg color.Color) uv.Style {
		if current {
			return uv.Style{Fg: th.Focus, Bg: bg}
		}
		return uv.Style{Fg: th.Dim, Bg: bg}
	}

	// seps are the x of each │: after the labels, then between the columns shown.
	seps := []int{area.Min.X + labelW + 2}
	cols := []int{}
	for c := hoff; c < v.cols && seps[len(seps)-1] < area.Max.X; c++ {
		cols = append(cols, c)
		seps = append(seps, seps[len(seps)-1]+ws[c]+3)
	}
	seps = seps[:len(cols)]
	cellX := func(i int) int { return seps[i] + 2 }

	// a row number is a button to its row, the column kept (G-04)
	rowNo := func(r uv.Rectangle, rec int) {
		f.Region(r.Intersect(area), Target{Kind: KindRowNo, Pane: g.Pane, Action: "grid.goto " + strconv.Itoa(rec) + " " + strconv.Itoa(max(g.Col, 0))})
	}
	y := area.Min.Y
	for i, c := range cols {
		right := min(cellX(i)+ws[c], area.Max.X)
		h := Truncate(v.head(c), ws[c])
		switch {
		case g.Transpose:
			rowNo(uv.Rect(cellX(i)-1, y, ws[c]+2, 1), c)
			f.Text(cellX(i), y, right, h, num(c == cc, th.PaneBg))
		default:
			f.Text(cellX(i), y, right, h, name)
			if g.Cols[c].PK && g.Key.Fg != nil { // the key icon keeps its own color (§7.7)
				f.Text(cellX(i), y, right, g.Key.Text, g.Key.On(name))
			}
		}
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

	for r := voff; r < v.rows && y < area.Max.Y; r, y = r+1, y+1 {
		bg := th.PaneBg
		switch {
		case r == cr, f.Mouse.In(uv.Rect(area.Min.X, y, area.Dx(), 1)): // the current row, and the one under the pointer (G-06)
			bg = th.Row
		case (r+1)%2 == 0: // even row numbers: zebra
			bg = th.RowAlt
		}
		f.Fill(uv.Rect(area.Min.X, y, area.Dx(), 1), uv.Style{Bg: bg})
		label := v.label(r)
		if g.Transpose {
			st := name
			st.Bg = bg
			f.Text(area.Min.X+1, y, seps[0], Truncate(label, labelW), st)
			if field := g.Cols[r]; field.PK && g.Key.Fg != nil {
				f.Text(area.Min.X+1, y, seps[0], g.Key.Text, g.Key.On(st))
			}
		} else {
			rowNo(uv.Rect(area.Min.X, y, labelW+2, 1), r)
			f.Text(area.Min.X+1+labelW-Width(label), y, seps[0], label, num(r == cr, bg))
		}
		for i, c := range cols {
			val, typ := v.val(r, c)
			s := Truncate(Cell(val), ws[c])
			rec, field := v.data(r, c)
			st := uv.Style{Fg: fg[typ], Bg: bg}
			if val.Null {
				st.Fg = th.Dim
			}
			x := cellX(i)
			if typ == ColNumber {
				x += ws[c] - Width(s)
			}
			if r == cr && c == cc {
				st.Bg = th.CursorBlur
				if g.Focused {
					st.Bg = th.Cursor
				}
				f.Fill(uv.Rect(cellX(i)-1, y, ws[c]+2, 1).Intersect(area), uv.Style{Bg: st.Bg})
			}
			f.Region(uv.Rect(cellX(i)-1, y, ws[c]+2, 1).Intersect(area), Target{Kind: KindCell, Pane: g.Pane, Action: "grid.goto " + strconv.Itoa(rec) + " " + strconv.Itoa(field)})
			drawCell(f, x, y, min(cellX(i)+ws[c], area.Max.X), s, st, uv.Style{Fg: th.Dim, Bg: st.Bg})
		}
		for _, x := range seps {
			f.Text(x, y, area.Max.X, "│", uv.Style{Fg: th.Sep, Bg: bg})
		}
	}
}

// drawCell draws a Cell's text with its ↵ marks in mark (§7.6).
// ponytail: a ↵ that was in the value itself is dimmed too.
func drawCell(f *Frame, x, y, right int, s string, st, mark uv.Style) {
	for i, part := range strings.Split(s, "↵") {
		if i > 0 {
			x = f.Text(x, y, right, "↵", mark)
		}
		x = f.Text(x, y, right, part, st)
	}
}
