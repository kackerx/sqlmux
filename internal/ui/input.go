package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Input is a single-line text field (the palette's, and M1's WHERE). The
// cursor moves and backspace deletes a whole grapheme cluster, as in nvim.
type Input struct {
	Text string
	Pos  int // byte offset of the cursor, on a grapheme boundary
}

func (in *Input) Insert(s string) {
	in.Text = in.Text[:in.Pos] + s + in.Text[in.Pos:]
	in.Pos += len(s)
}

func (in *Input) Backspace() {
	before := DropLastGrapheme(in.Text[:in.Pos])
	in.Text = before + in.Text[in.Pos:]
	in.Pos = len(before)
}

func (in *Input) Left() { in.Pos = len(DropLastGrapheme(in.Text[:in.Pos])) }

func (in *Input) Right() {
	gr, _ := ansi.FirstGraphemeCluster(in.Text[in.Pos:], ansi.GraphemeWidth)
	in.Pos += len(gr)
}

// Draw paints the text on r's first row, scrolled so the cursor stays in
// view, and returns where the cursor goes.
func (in Input) Draw(f *Frame, r uv.Rectangle, st uv.Style) uv.Position {
	start := 0
	for Width(in.Text[start:in.Pos]) >= r.Dx() && start < in.Pos { // the cursor takes a cell too
		gr, _ := ansi.FirstGraphemeCluster(in.Text[start:], ansi.GraphemeWidth)
		start += len(gr)
	}
	f.Text(r.Min.X, r.Min.Y, r.Max.X, in.Text[start:], st)
	return uv.Pos(r.Min.X+Width(in.Text[start:in.Pos]), r.Min.Y)
}
