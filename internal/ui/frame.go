// Package ui draws the immediate-mode frame (tech-design §7).
package ui

import (
	"cmp"
	"image/color"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Kind is what a hit region stands for (tech-design §7.4).
type Kind uint8

const (
	KindPane     Kind = iota + 1 // a pane's area: click focuses, wheel scrolls
	KindTitle                    // a pane's title: double-click zooms
	KindTab                      // a tab in a pane's tab bar (I: index, -1 the +)
	KindHint                     // a key hint inside a pane: focus it, then run Action
	KindButton                   // a clickable outside any pane (status bar): run Action
	KindItem                     // a which-key item (I: its index)
	KindNumber                   // a pane under SPC q's numbers (I: its ⟨n⟩)
	KindBorder                   // a split's drag handle (I: the split's index)
	KindBackdrop                 // behind an overlay: a click closes it
	KindRow                      // a palette candidate (I: its index): hover selects, click runs
	KindTreeEdge                 // the gap right of the sidebar: drag to set its width
)

// Target is what a click on a hit region resolves to. Targets compare with
// == to spot a double click.
type Target struct {
	Kind   Kind
	Pane   int    // pane ID, for the pane kinds
	I      int    // kind-specific index
	Action string // action with args, e.g. "window.select 3"
}

type Hit struct {
	Rect   uv.Rectangle
	Target Target
}

// Frame is one render pass: a cell buffer, the hit table built while
// drawing, and what drawing needs to pick styles.
type Frame struct {
	Buf    uv.ScreenBuffer
	Hits   []Hit
	Mouse  uv.Position // pointer, for hover styles; (-1,-1) when unknown
	Theme  *Theme
	Cursor *uv.Position // the terminal cursor, while an input has focus
}

func NewFrame(w, h int, th *Theme) *Frame {
	buf := uv.NewScreenBuffer(max(w, 0), max(h, 0))
	buf.Method = ansi.GraphemeWidth
	return &Frame{Buf: buf, Mouse: uv.Pos(-1, -1), Theme: th}
}

func (f *Frame) Bounds() uv.Rectangle { return f.Buf.Bounds() }

// Region registers r as a hit region for t and reports whether the pointer
// is over it.
func (f *Frame) Region(r uv.Rectangle, t Target) (hover bool) {
	if r.Empty() {
		return false
	}
	f.Hits = append(f.Hits, Hit{r, t})
	return f.Mouse.In(r)
}

// HitAt returns the topmost target at p: later hits were drawn on top.
func HitAt(hits []Hit, p uv.Position) (Target, bool) {
	for i := len(hits) - 1; i >= 0; i-- {
		if p.In(hits[i].Rect) {
			return hits[i].Target, true
		}
	}
	return Target{}, false
}

// Fill paints every cell of r with a blank in style st.
func (f *Frame) Fill(r uv.Rectangle, st uv.Style) {
	cell := uv.EmptyCell
	cell.Style = st
	f.Buf.FillArea(&cell, r.Intersect(f.Bounds()))
}

// Text draws s at (x, y) clipped to [x, right), returning the x after the last
// drawn cell. A wide grapheme that would straddle right is not drawn.
func (f *Frame) Text(x, y, right int, s string, st uv.Style) int {
	b := f.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return x
	}
	right = min(right, b.Max.X)
	for s != "" {
		gr, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		s = s[len(gr):]
		if w == 0 {
			continue
		}
		if x+w > right {
			break
		}
		if x >= b.Min.X {
			f.Buf.SetCell(x, y, &uv.Cell{Content: gr, Width: w, Style: st})
		}
		x += w
	}
	return x
}

// TextMatch is Text with the runes at pos (a Match's) drawn in hl: a
// grapheme lights up when any of its runes matched.
func (f *Frame) TextMatch(x, y, right int, s string, pos []int, st, hl uv.Style) int {
	r := 0 // rune offset of the grapheme
	for s != "" {
		gr, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		s = s[len(gr):]
		n := utf8.RuneCountInString(gr)
		for len(pos) > 0 && pos[0] < r {
			pos = pos[1:]
		}
		style := st
		if len(pos) > 0 && pos[0] < r+n {
			style = hl
		}
		x = f.Text(x, y, right, gr, style)
		r += n
	}
	return x
}

// Dim blends every cell 60% of the way to bg, so what an overlay covers
// reads as behind it (§7.5): a terminal has no alpha.
func (f *Frame) Dim() {
	th := f.Theme
	for y := range f.Buf.Height() {
		for x := range f.Buf.Width() {
			c := f.Buf.CellAt(x, y)
			c.Style.Fg = mix(cmp.Or(c.Style.Fg, th.Fg), th.Bg, 0.6)
			c.Style.Bg = mix(cmp.Or(c.Style.Bg, th.Bg), th.Bg, 0.6)
		}
	}
}

// mix is the color t of the way from a to b.
func mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	m := func(x, y uint32) uint8 { return uint8((float64(x)*(1-t) + float64(y)*t) / 0x101) }
	return color.RGBA{m(ar, br), m(ag, bg), m(ab, bb), 0xff}
}

func (f *Frame) Render() string { return f.Buf.Render() }

// String is the frame as plain text, for golden tests.
func (f *Frame) String() string { return f.Buf.String() }
