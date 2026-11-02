package ui

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// TextView is a text seen through a window of rows (§12「预览」, F9.7): Top
// lines scrolled past, a "…" in place of the window's first row when there
// are lines above it and of its last row when there are more below. Colors
// are the text's, a color to each byte (SQLColors); none is an error, drawn
// in its color.
type TextView struct {
	Text   string
	Colors []color.Color
	Fg     color.Color // the text's color when it has none of its own: the error's by default
	Top    int
}

// Lines is how many rows the text takes whole.
func (v TextView) Lines() int { return strings.Count(v.Text, "\n") + 1 }

// Fit is Top held to what there is to scroll in a window of rows.
func (v TextView) Fit(rows int) int { return max(min(v.Top, v.Lines()-rows), 0) }

// Scroll is Top moved by d rows in a window of rows, from where Fit holds it
// now, to the ends and no further.
func (v TextView) Scroll(rows, d int) int {
	v.Top = v.Fit(rows) + d
	return v.Fit(rows)
}

// Draw paints the window in area, over bg.
func (v TextView) Draw(f *Frame, area uv.Rectangle, bg uv.Style) {
	th := f.Theme
	lines, top := strings.Split(v.Text, "\n"), v.Fit(area.Dy())
	at := 0 // the byte the line starts at
	for _, l := range lines[:top] {
		at += len(l) + 1
	}
	for i := range min(area.Dy(), len(lines)-top) {
		l, r := lines[top+i], uv.Rect(area.Min.X, area.Min.Y+i, area.Dx(), 1)
		switch {
		case i == 0 && top > 0, i == area.Dy()-1 && top+i < len(lines)-1:
			f.Text(r.Min.X, r.Min.Y, r.Max.X, "…", uv.Style{Fg: th.Dim, Bg: bg.Bg})
		case v.Colors == nil:
			fg := v.Fg
			if fg == nil {
				fg = th.Error
			}
			f.Text(r.Min.X, r.Min.Y, r.Max.X, l, uv.Style{Fg: fg, Bg: bg.Bg})
		default:
			Input{Text: l}.draw(f, r, bg, v.Colors[at:at+len(l)])
		}
		at += len(l) + 1
	}
}

// TextBox is a TextView in a box of its own, a title on its top border, over
// everything else: a click outside it is a click on the backdrop (F9.8).
type TextBox struct {
	Title string
	View  TextView
	Hints []Hint // a row above the bottom border; the last give way when it is too narrow (F12.8)
}

// Inner is where the text goes in a box: inside the border and a column of
// padding, and over the hints' row.
func (b TextBox) Inner(box uv.Rectangle) uv.Rectangle {
	rows := box.Dy() - 2
	if b.hinted(box) {
		rows--
	}
	return uv.Rect(box.Min.X+2, box.Min.Y+1, max(box.Dx()-4, 0), max(rows, 0))
}

// hinted is whether the hints have a row in box, with a row of text left.
func (b TextBox) hinted(box uv.Rectangle) bool { return len(b.Hints) > 0 && box.Dy() > 3 }

func (b TextBox) Draw(f *Frame, box uv.Rectangle) {
	if b.Frame(f, box) {
		b.View.Draw(f, b.Inner(box), uv.Style{Fg: f.Theme.Fg, Bg: f.Theme.PaneBg})
	}
}

// Frame draws the box without the text, and is whether there was room for it.
func (b TextBox) Frame(f *Frame, box uv.Rectangle) bool {
	th := f.Theme
	f.Region(f.Bounds(), Target{Kind: KindBackdrop})
	if box.Dx() < 6 || box.Dy() < 3 {
		return false
	}
	f.Region(box, Target{}) // inside, a click is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := f.Border.Style(uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	if b.Title != "" {
		f.Text(box.Min.X+2, box.Min.Y, box.Max.X-1, " "+b.Title+" ", uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg, Attrs: uv.AttrBold})
	}
	if hs := b.Hints; b.hinted(box) {
		x0, x1 := box.Min.X+2, box.Max.X-2
		hs = hs[:max(leadingHints(hs, x1-x0), 1)] // the first is shown, cut by the border if it must
		hintRow(f, x0, box.Max.Y-2, x1, hs, uv.Style{Fg: th.Dim, Bg: th.PaneBg}, Target{Kind: KindButton})
	}
	return true
}
