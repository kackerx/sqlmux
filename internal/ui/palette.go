package ui

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// PaletteRow is one candidate (K-03): icon, name, where it lives in dim, its
// key or ON / OFF, and what kind of thing it is. Pos are match positions in
// the string Name + " " + Where.
type PaletteRow struct {
	Icon        Icon
	Name, Where string
	Pos         []int
	Right, Tag  string
}

// Palette is the command palette box (§12). Rows are all the candidates;
// Top is the first one the list shows.
type Palette struct {
	Search   Icon // leads the input
	Input    Input
	Scopes   []string // the scope tabs, each with its prefix; Scope is the current one
	Scope    int
	Rows     []PaletteRow
	Sel, Top int
	Footer   []Hint // left: moving, scopes, closing
	Enter    []Hint // right: what ↵ (and C-t) do
	Result   *PaletteResult
	Preview  *PalettePreview
}

// PalettePreview is the DDL of the table selected, under the list (F4.2):
// in SQL's colors, or the error that came instead.
type PalettePreview struct {
	Text, Err string
	Names     SQLNames
}

// Lines is how many rows p takes whole.
func (p *PalettePreview) Lines() int {
	if p.Err != "" {
		return 1
	}
	return strings.Count(p.Text, "\n") + 1
}

// PaletteResult is quick SQL's result area, under the list (§12): a title
// row, then the table, or the database's error in its place.
type PaletteResult struct {
	Title string // "100+ 行 · 12ms · 只读"
	Hints []Hint // right of the title: C-y CSV; the last give way first
	Err   string
	Warn  bool // Err is a warning, in its color: a write not run (F-05)
	Grid  Grid
}

const (
	paletteRows = 12
	resultRows  = 8 // the least a result's table gets while there is room (§12)
)

// PaletteBox is where the palette sits for n candidates (§12): at most 100
// wide and centered, its top edge a sixth of the way down, leaving the lower
// half to SQL results, and fixed so the input stays put as the list grows
// and shrinks; list rows = how many show. With a result the box reaches
// down to a row above the status bar, and grid is where the result's table
// goes, the list giving up rows before it does. A preview of that many
// lines goes under the list, at most 12, in grid (§12「预览」): short of
// room the preview gives up rows down to 3, then the list down to 3, then
// the preview goes; one of fewer lines keeps them all.
func PaletteBox(screen uv.Rectangle, n int, result bool, preview int) (box uv.Rectangle, rows int, grid uv.Rectangle) {
	w := min(100, screen.Dx()-4)
	x, top := screen.Min.X+(screen.Dx()-w)/2, screen.Min.Y+screen.Dy()/6
	if !result {
		// border, scopes, input, rule | list | rule, footer, border
		room := screen.Max.Y - top - 7
		rows = max(min(n, paletteRows, room), 0)
		preview = min(preview, paletteRows)
		switch least := min(preview, 3); { // | rule, preview
		case preview <= 0, rows+1+preview <= room:
		case rows+1+least <= room:
			preview = room - rows - 1
		case min(rows, 3)+1+least <= room:
			rows, preview = room-1-least, least
		default:
			preview = 0
		}
		box = uv.Rect(x, top, w, rows+7)
		if preview > 0 {
			box.Max.Y += preview + 1
			grid = uv.Rect(x+1, top+4+rows+1, w-2, preview)
		}
		return box, rows, grid
	}
	box = uv.Rect(x, top, w, max(screen.Max.Y-1-top, 0))
	// border, scopes, input, rule | list, rule | title | table | rule, footer, border
	rows = max(min(n, paletteRows, box.Dy()-9-resultRows), 0)
	y := top + 4 + rows + 1 // under the title
	if rows > 0 {
		y++ // and the list's rule
	}
	return box, rows, uv.Rect(x+1, y, w-2, max(box.Max.Y-3-y, 0))
}

// Draw dims what is behind (§7.5), paints the box over it and returns where
// the input's cursor goes.
func (p Palette) Draw(f *Frame, screen uv.Rectangle) uv.Position {
	th := f.Theme
	preview := 0
	if p.Preview != nil {
		preview = p.Preview.Lines()
	}
	box, rows, grid := PaletteBox(screen, len(p.Rows), p.Result != nil, preview)
	top := max(min(p.Top, p.Sel), p.Sel-rows+1, 0) // the selection in view as the list loses rows: to a preview, or the window
	f.Dim()
	f.Region(f.Bounds(), Target{Kind: KindBackdrop}) // a click outside closes it
	if box.Dx() < 8 {
		return uv.Pos(-1, -1)
	}
	f.Region(box, Target{}) // inside, a click is not outside
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	f.Fill(box, bg)
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y, box.Max.X-1, " 命令面板 ", uv.Style{Fg: th.Focus, Bg: th.PaneBg, Attrs: uv.AttrBold})

	x0, x1 := box.Min.X+2, box.Max.X-2 // one column of padding inside the border
	y := box.Min.Y + 1
	x := x0
	for i, s := range p.Scopes {
		st := dim
		if i == p.Scope {
			st = uv.Style{Fg: th.Bg, Bg: th.Focus, Attrs: uv.AttrBold}
		}
		t := " " + s + " "
		if f.Region(uv.Rect(x, y, min(Width(t), x1-x), 1), Target{Kind: KindButton, Action: fmt.Sprintf("palette.scope %d", i)}) && i != p.Scope {
			st.Bg = th.Select
		}
		x = f.Text(x, y, x1, t, st) + 1
	}
	y++
	x = f.Text(x0, y, x1, p.Search.Text, p.Search.On(uv.Style{Fg: th.Info, Bg: th.PaneBg}))
	cursor := p.Input.Draw(f, uv.Rect(x+1, y, x1-x-1, 1), bg)
	rule := func(y int) {
		f.Text(box.Min.X+1, y, box.Max.X-1, strings.Repeat("─", box.Dx()-2), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	rule(y + 1)
	y += 2
	// Columns: every row's location starts where the others' do (§12). The
	// name column is as wide as the widest name, at most 40% of the box.
	// A list with no locations (the SQL history) gives its names the row.
	iconW, nameW, located := 0, 0, false
	for _, r := range p.Rows {
		iconW, nameW = max(iconW, Width(r.Icon.Text)), max(nameW, Width(r.Name))
		located = located || r.Where != ""
	}
	nameW = min(nameW, box.Dx()*2/5)
	for i := top; i < min(top+rows, len(p.Rows)); i, y = i+1, y+1 {
		r := p.Rows[i]
		st := bg
		if i == p.Sel {
			st.Bg = th.Select
			f.Fill(uv.Rect(box.Min.X+1, y, box.Dx()-2, 1), st)
		}
		f.Region(uv.Rect(box.Min.X+1, y, box.Dx()-2, 1), Target{Kind: KindRow, I: i})
		faint := uv.Style{Fg: th.Dim, Bg: st.Bg}
		// right to left: the tag in a column of its own, then the key
		tag := x1 - 4
		f.Text(tag+4-Width(r.Tag), y, x1, r.Tag, faint)
		right := tag - 2 - Width(r.Right)
		f.Text(right, y, tag, r.Right, faint)
		f.Text(x0, y, right-1, r.Icon.Text, r.Icon.On(st))
		nameEnd := len([]rune(r.Name)) // Where's positions come after Name and the space
		var inName, inWhere []int
		for _, i := range r.Pos {
			if i > nameEnd {
				inWhere = append(inWhere, i-nameEnd-1)
			} else {
				inName = append(inName, i)
			}
		}
		nx := x0 + iconW + 1
		w := nameW
		if !located {
			w = right - 1 - nx
		}
		name, inName := TruncateMatch(r.Name, inName, w)
		f.TextMatch(nx, y, min(nx+w, right-1), name, inName, st)
		f.TextMatch(nx+nameW+2, y, right-1, r.Where, inWhere, faint)
	}
	if pv := p.Preview; pv != nil && grid.Dy() > 0 {
		rule(y)
		lines, colors := strings.Split(pv.Text, "\n"), SQLColors(th, pv.Text, pv.Names)
		if pv.Err != "" {
			lines, colors = []string{pv.Err}, nil
		}
		at := 0
		for i, l := range lines[:min(len(lines), grid.Dy())] {
			r := uv.Rect(x0, grid.Min.Y+i, x1-x0, 1)
			switch {
			case i == grid.Dy()-1 && i < len(lines)-1: // cut
				// ponytail: a DDL longer than the preview is cut, not scrolled
				f.Text(r.Min.X, r.Min.Y, r.Max.X, "…", dim)
			case colors == nil:
				f.Text(r.Min.X, r.Min.Y, r.Max.X, l, uv.Style{Fg: th.Error, Bg: th.PaneBg})
			default:
				Input{Text: l}.draw(f, r, bg, colors[at:at+len(l)])
			}
			at += len(l) + 1
		}
		y = grid.Max.Y
	}
	if r := p.Result; r != nil {
		if rows > 0 {
			rule(y)
			y++
		}
		f.Text(x0, y, x1, r.Title, dim)
		hs := r.Hints
		for len(hs) > 0 && Width(r.Title)+2+hintRowWidth(hs) > x1-x0 { // whole ones, the last going first
			hs = hs[:len(hs)-1]
		}
		hintRow(f, x1-hintRowWidth(hs), y, x1, hs, dim, Target{Kind: KindButton})
		if r.Err != "" { // in place of the table, as a data pane shows it (§7.6)
			fg := th.Error
			if r.Warn {
				fg = th.Warn
			}
			f.Text(grid.Min.X+1, grid.Min.Y, grid.Max.X-1, r.Err, uv.Style{Fg: fg, Bg: th.PaneBg})
		} else {
			r.Grid.Draw(f, grid)
		}
		y = grid.Max.Y
	}
	rule(y)
	y++
	// the footer reads like a tab bar's hints
	hintRow(f, x0, y, x1, p.Footer, dim, Target{Kind: KindButton})
	hintRow(f, x1-hintRowWidth(p.Enter), y, x1, p.Enter, dim, Target{Kind: KindButton})
	return cursor
}
