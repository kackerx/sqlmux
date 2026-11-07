package ui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// A transparent frame leaves the theme's backgrounds to the terminal, through
// an overlay's dimming too, and paints every other color as it was (§7.3).
func TestUnpaint(t *testing.T) {
	th := TokyonightStorm
	for _, dim := range []bool{false, true} {
		f := NewFrame(5, 1, th)
		f.Transparent = true
		f.Fill(uv.Rect(0, 0, 1, 1), uv.Style{Fg: th.Fg, Bg: th.Bg})
		f.Fill(uv.Rect(1, 0, 1, 1), uv.Style{Fg: th.Fg, Bg: th.PaneBg})
		f.Fill(uv.Rect(2, 0, 1, 1), uv.Style{Fg: th.Fg, Bg: th.Gap})
		f.Fill(uv.Rect(3, 0, 1, 1), uv.Style{Fg: th.Bg, Bg: th.Focus}) // a chip: bg as its text
		f.Fill(uv.Rect(4, 0, 1, 1), uv.Style{Fg: th.Fg, Bg: th.Row})
		if dim {
			f.Dim()
		}
		f.Unpaint()
		for x := range 3 {
			if bg := f.Buf.CellAt(x, 0).Style.Bg; bg != nil {
				t.Errorf("dim %v: cell %d kept background %v", dim, x, bg)
			}
		}
		for x := 3; x < 5; x++ {
			c := f.Buf.CellAt(x, 0)
			if c.Style.Bg == nil || c.Style.Fg == nil {
				t.Errorf("dim %v: cell %d lost its colors: %+v", dim, x, c.Style)
			}
		}
		if !dim && !sameColor(f.Buf.CellAt(3, 0).Style.Bg, th.Focus) {
			t.Errorf("the chip's background changed: %v", f.Buf.CellAt(3, 0).Style.Bg)
		}
	}
}

// Without it, Dim mixes every background as it always has.
func TestDimOpaque(t *testing.T) {
	th := TokyonightStorm
	f := NewFrame(1, 1, th)
	f.Fill(f.Bounds(), uv.Style{Fg: th.Fg, Bg: th.PaneBg})
	f.Dim()
	if bg := f.Buf.CellAt(0, 0).Style.Bg; bg == nil || sameColor(bg, th.PaneBg) {
		t.Errorf("an opaque frame's dimmed background: %v", bg)
	}
}
