// Package ui draws the immediate-mode frame (tech-design §7).
package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Kind is what a hit region stands for (tech-design §7.4).
type Kind uint8

const (
	KindNone Kind = iota
	KindPane
	KindTitle
	KindBorder
	KindTab
	KindItem
	KindHint
	KindBackdrop
)

// Target is what a click on a hit region resolves to.
type Target struct {
	Kind   Kind
	Pane   int    // pane ID
	I, J   int    // kind-specific indices
	Action string // action with args, e.g. "window.select 3"
}

type Hit struct {
	Rect   uv.Rectangle
	Target Target
}

// Frame is one render pass: a cell buffer, the hit table built while
// drawing, and what drawing needs to pick styles.
type Frame struct {
	Buf   uv.ScreenBuffer
	Hits  []Hit
	Mouse uv.Position // pointer, for hover styles; (-1,-1) when unknown
	Theme *Theme
	Icons *Icons
}

func NewFrame(w, h int, th *Theme, ic *Icons) *Frame {
	buf := uv.NewScreenBuffer(max(w, 0), max(h, 0))
	buf.Method = ansi.GraphemeWidth
	return &Frame{Buf: buf, Mouse: uv.Pos(-1, -1), Theme: th, Icons: ic}
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

// HitAt returns the topmost target at p. Later hits are drawn on top.
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

func (f *Frame) Render() string { return f.Buf.Render() }

// String is the frame as plain text, for golden tests.
func (f *Frame) String() string { return f.Buf.String() }
