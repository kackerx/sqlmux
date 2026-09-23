// Package ui draws the immediate-mode frame (tech-design §7).
package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// Frame is one render pass: a cell buffer plus the theme used to paint it.
type Frame struct {
	Buf   uv.ScreenBuffer
	Theme *Theme
}

func NewFrame(w, h int, th *Theme) *Frame {
	buf := uv.NewScreenBuffer(max(w, 0), max(h, 0))
	buf.Method = ansi.GraphemeWidth
	return &Frame{Buf: buf, Theme: th}
}

func (f *Frame) Bounds() uv.Rectangle { return f.Buf.Bounds() }

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
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		gr := g.Str()
		w := ansi.GraphemeWidth.StringWidth(gr)
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

func (f *Frame) Render() string { return f.Buf.Render() }
