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
}

const paletteRows = 12

// PaletteBox is where the palette sits for n candidates (§12): at most 100
// wide and centered, its top edge a sixth of the way down, leaving the lower
// half to SQL results, and fixed so the input stays put as the list grows
// and shrinks; list rows = how many show.
func PaletteBox(screen uv.Rectangle, n int) (box uv.Rectangle, rows int) {
	w := min(100, screen.Dx()-4)
	top := screen.Min.Y + screen.Dy()/6
	// border, scopes, input, rule | list | rule, footer, border
	rows = max(min(n, paletteRows, screen.Max.Y-top-7), 0)
	return uv.Rect(screen.Min.X+(screen.Dx()-w)/2, top, w, rows+7), rows
}

// Draw dims what is behind (§7.5), paints the box over it and returns where
// the input's cursor goes.
func (p Palette) Draw(f *Frame, screen uv.Rectangle) uv.Position {
	th := f.Theme
	box, rows := PaletteBox(screen, len(p.Rows))
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
	hl := uv.Style{Fg: th.Bg, Bg: th.Warn}
	// Columns: every row's location starts where the others' do (§12). The
	// name column is as wide as the widest name, at most 40% of the box.
	iconW, nameW := 0, 0
	for _, r := range p.Rows {
		iconW, nameW = max(iconW, Width(r.Icon.Text)), max(nameW, Width(r.Name))
	}
	nameW = min(nameW, box.Dx()*2/5)
	for i := p.Top; i < min(p.Top+rows, len(p.Rows)); i, y = i+1, y+1 {
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
		name := Truncate(r.Name, nameW)
		nameEnd := len([]rune(r.Name)) // Where's positions come after Name and the space
		var inName, inWhere []int
		for _, i := range r.Pos {
			switch {
			case i > nameEnd:
				inWhere = append(inWhere, i-nameEnd-1)
			case name == r.Name || i < len([]rune(name))-1: // not under the …
				inName = append(inName, i)
			}
		}
		nx := x0 + iconW + 1
		f.TextMatch(nx, y, min(nx+nameW, right-1), name, inName, st, hl)
		f.TextMatch(nx+nameW+2, y, right-1, r.Where, inWhere, faint, hl)
	}
	rule(y)
	y++
	// the footer reads like a tab bar's hints
	hintRow(f, x0, y, x1, p.Footer, dim, Target{Kind: KindButton})
	hintRow(f, x1-hintRowWidth(p.Enter), y, x1, p.Enter, dim, Target{Kind: KindButton})
	return cursor
}
