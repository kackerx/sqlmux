package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// PaletteRow is one candidate (K-03): its name, then where it lives in dim.
// Pos are match positions in the string Name + " " + Where.
type PaletteRow struct {
	Name, Where string
	Pos         []int
	Right       string // its key, or ON / OFF
}

// Palette is the command palette box (§12). Rows are all the candidates;
// Top is the first one the list shows.
type Palette struct {
	Input    Input
	Rows     []PaletteRow
	Sel, Top int
	Footer   []Hint // left: moving and closing
	Enter    Hint   // right: what ↵ does
}

const paletteRows = 10

// PaletteBox is where the palette sits for n candidates (§12): at most 80
// wide and centered, its top edge a quarter of the way down so the input
// row stays put as the list grows and shrinks; list rows = how many show.
func PaletteBox(screen uv.Rectangle, n int) (box uv.Rectangle, rows int) {
	w := min(80, screen.Dx()-4)
	top := screen.Min.Y + screen.Dy()/4
	// border, input, rule | list | rule, footer, border
	rows = max(min(n, paletteRows, screen.Max.Y-top-6), 0)
	return uv.Rect(screen.Min.X+(screen.Dx()-w)/2, top, w, rows+6), rows
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
	f.Fill(box, bg)
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y, box.Max.X-1, " 命令面板 ", uv.Style{Fg: th.Focus, Bg: th.PaneBg, Attrs: uv.AttrBold})

	x0, x1 := box.Min.X+2, box.Max.X-2 // one column of padding inside the border
	y := box.Min.Y + 1
	cursor := p.Input.Draw(f, uv.Rect(x0, y, x1-x0, 1), bg)
	rule := func(y int) {
		f.Text(box.Min.X+1, y, box.Max.X-1, strings.Repeat("─", box.Dx()-2), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	rule(y + 1)
	y += 2
	hl := uv.Style{Fg: th.Bg, Bg: th.Warn}
	for i := p.Top; i < min(p.Top+rows, len(p.Rows)); i, y = i+1, y+1 {
		r := p.Rows[i]
		st := bg
		if i == p.Sel {
			st.Bg = th.Select
			f.Fill(uv.Rect(box.Min.X+1, y, box.Dx()-2, 1), st)
		}
		f.Region(uv.Rect(box.Min.X+1, y, box.Dx()-2, 1), Target{Kind: KindRow, I: i})
		right := x1 - Width(r.Right)
		f.Text(right, y, x1, r.Right, uv.Style{Fg: th.Dim, Bg: st.Bg})
		nameEnd := len([]rune(r.Name)) // Where's positions come after Name and the space
		x := f.TextMatch(x0, y, right-1, r.Name, r.Pos, st, hl)
		x = f.Text(x, y, right-1, "  ", st)
		var where []int
		for _, i := range r.Pos {
			if i > nameEnd {
				where = append(where, i-nameEnd-1)
			}
		}
		f.TextMatch(x, y, right-1, r.Where, where, uv.Style{Fg: th.Dim, Bg: st.Bg}, hl)
	}
	rule(y)
	y++
	x := x0
	for i, h := range p.Footer {
		if i > 0 {
			x = f.Text(x, y, x1, " · ", uv.Style{Fg: th.Dim, Bg: th.PaneBg})
		}
		x = footerHint(f, x, y, x1, h)
	}
	footerHint(f, x1-Width(p.Enter.tabText()), y, x1, p.Enter)
	return cursor
}

// footerHint draws "key label" as the tab bar does, clickable as its action.
func footerHint(f *Frame, x, y, right int, h Hint) int {
	st := uv.Style{Fg: f.Theme.Dim, Bg: f.Theme.PaneBg}
	if f.Region(uv.Rect(x, y, min(Width(h.tabText()), right-x), 1), Target{Kind: KindButton, Action: h.Action}) {
		st.Bg = f.Theme.Select
	}
	return f.Text(x, y, right, h.tabText(), st)
}
