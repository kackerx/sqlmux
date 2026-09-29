package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// WhichKeyItem is one way to continue a pending sequence: "key → title",
// under the heading of Group, the config table it comes from (§6.5).
type WhichKeyItem struct{ Key, Title, Group string }

// WhichKey is the overlay listing what can follow a pending prefix, and
// the ? help (§6.5): as wide as its area and resting on its bottom edge,
// each group's items in columns top to bottom under a "[table]" row.
type WhichKey struct {
	Prefix string // the pending keys, shown on the top border
	Items  []WhichKeyItem
	Top    int // rows scrolled past, the ? help's
}

const whichKeyGap = 3

type whichKeyRow struct {
	y    int
	text string
}

// layout is the widest key and title, the rows all of it takes, where
// each item goes (column, row) and the group headings.
func (w WhichKey) layout(width int) (kw, tw, rows int, at []uv.Position, heads []whichKeyRow) {
	for _, it := range w.Items {
		kw, tw = max(kw, Width(it.Key)), max(tw, Width(it.Title))
	}
	colw := kw + Width(" → ") + tw + whichKeyGap
	cols := max((width-4+whichKeyGap)/colw, 1)
	for i := 0; i < len(w.Items); {
		g := w.Items[i].Group
		j := i + 1
		for j < len(w.Items) && w.Items[j].Group == g {
			j++
		}
		if g != "" {
			heads = append(heads, whichKeyRow{rows, "[" + g + "]"})
			rows++
		}
		n := (j - i + cols - 1) / cols
		for k := range j - i {
			at = append(at, uv.Pos(k/n, rows+k%n))
		}
		rows, i = rows+n, j
	}
	return kw, tw, rows, at, heads
}

// Fit is top kept to what can scroll in area, and the rows area shows.
func (w WhichKey) Fit(area uv.Rectangle, top int) (int, int) {
	_, _, rows, _, _ := w.layout(area.Dx())
	shown := max(min(rows, area.Dy()-2), 0)
	return max(min(top, rows-shown), 0), shown
}

func (w WhichKey) Draw(f *Frame, area uv.Rectangle) {
	th := f.Theme
	if len(w.Items) == 0 || area.Dx() < 4 || area.Dy() < 3 {
		return
	}
	kw, tw, _, at, heads := w.layout(area.Dx())
	colw := kw + Width(" → ") + tw + whichKeyGap
	top, shown := w.Fit(area, w.Top)
	r := uv.Rect(area.Min.X, area.Max.Y-shown-2, area.Dx(), shown+2)
	f.Region(f.Bounds(), Target{Kind: KindBackdrop}) // a click anywhere else closes it
	f.Fill(r, uv.Style{Bg: th.PaneBg})
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Border, Bg: th.PaneBg})
	border.Draw(f.Buf, r)
	f.Text(r.Min.X+2, r.Min.Y, r.Max.X-1, " "+w.Prefix+" ", uv.Style{Fg: th.Warn, Bg: th.PaneBg, Attrs: uv.AttrBold})
	row := func(y int) (int, bool) { y += r.Min.Y + 1 - top; return y, y > r.Min.Y && y < r.Max.Y-1 }

	for _, h := range heads {
		if y, ok := row(h.y); ok {
			f.Text(r.Min.X+2, y, r.Max.X-1, h.text, uv.Style{Fg: th.Dim, Bg: th.PaneBg})
		}
	}
	for i, it := range w.Items {
		y, ok := row(at[i].Y)
		if !ok {
			continue // scrolled away, or not enough rows above the status bar
		}
		x := r.Min.X + 2 + at[i].X*colw
		bg := th.PaneBg
		if f.Region(uv.Rect(x, y, min(colw-whichKeyGap, r.Max.X-1-x), 1), Target{Kind: KindItem, I: i}) {
			bg = th.Select
		}
		x = f.Text(x, y, r.Max.X-1, it.Key, uv.Style{Fg: th.Warn, Bg: bg, Attrs: uv.AttrBold})
		x = f.Text(x, y, r.Max.X-1, strings.Repeat(" ", kw-Width(it.Key))+" → ", uv.Style{Fg: th.Dim, Bg: bg})
		x = f.Text(x, y, r.Max.X-1, it.Title, uv.Style{Fg: th.Fg, Bg: bg})
		f.Text(x, y, r.Min.X+2+at[i].X*colw+colw-whichKeyGap, strings.Repeat(" ", tw-Width(it.Title)), uv.Style{Bg: bg})
	}
}
