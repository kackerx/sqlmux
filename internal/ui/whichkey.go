package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// WhichKeyItem is one way to continue a pending sequence: "key → title".
type WhichKeyItem struct{ Key, Title string }

// WhichKey is the overlay listing what can follow a pending prefix (§6.5):
// as wide as its area and resting on its bottom edge, items in columns top
// to bottom.
type WhichKey struct {
	Prefix string // the pending keys, shown on the top border
	Items  []WhichKeyItem
}

const whichKeyGap = 3

func (w WhichKey) Draw(f *Frame, area uv.Rectangle) {
	th := f.Theme
	if len(w.Items) == 0 || area.Dx() < 4 || area.Dy() < 3 {
		return
	}
	kw, tw := 0, 0
	for _, it := range w.Items {
		kw, tw = max(kw, Width(it.Key)), max(tw, Width(it.Title))
	}
	colw := kw + Width(" → ") + tw + whichKeyGap
	cols := max((area.Dx()-4+whichKeyGap)/colw, 1)
	rows := (len(w.Items) + cols - 1) / cols
	h := min(rows+2, area.Dy())
	r := uv.Rect(area.Min.X, area.Max.Y-h, area.Dx(), h)
	f.Region(f.Bounds(), Target{Kind: KindBackdrop}) // a click anywhere else closes it
	f.Fill(r, uv.Style{Bg: th.PaneBg})
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Border, Bg: th.PaneBg})
	border.Draw(f.Buf, r)
	f.Text(r.Min.X+2, r.Min.Y, r.Max.X-1, " "+w.Prefix+" ", uv.Style{Fg: th.Warn, Bg: th.PaneBg, Attrs: uv.AttrBold})

	for i, it := range w.Items {
		col, row := i/rows, i%rows
		y := r.Min.Y + 1 + row
		if y >= r.Max.Y-1 {
			continue // clipped: not enough rows above the status bar
		}
		x := r.Min.X + 2 + col*colw
		bg := th.PaneBg
		if f.Region(uv.Rect(x, y, min(colw-whichKeyGap, r.Max.X-1-x), 1), Target{Kind: KindItem, I: i}) {
			bg = th.Select
		}
		x = f.Text(x, y, r.Max.X-1, it.Key, uv.Style{Fg: th.Warn, Bg: bg, Attrs: uv.AttrBold})
		x = f.Text(x, y, r.Max.X-1, strings.Repeat(" ", kw-Width(it.Key))+" → ", uv.Style{Fg: th.Dim, Bg: bg})
		x = f.Text(x, y, r.Max.X-1, it.Title, uv.Style{Fg: th.Fg, Bg: bg})
		f.Text(x, y, r.Min.X+2+col*colw+colw-whichKeyGap, strings.Repeat(" ", tw-Width(it.Title)), uv.Style{Bg: bg})
	}
}
