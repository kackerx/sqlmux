package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Input is a single-line text field (the palette's, a WHERE, a cell being
// edited). The cursor moves and backspace deletes a whole grapheme cluster,
// as in nvim.
type Input struct {
	Text string
	Pos  int  // byte offset of the cursor, on a grapheme boundary
	All  bool // all of it selected, as a cell's edit starts (§10.1): typing replaces it
}

func (in *Input) Insert(s string) {
	if in.All {
		*in = Input{}
	}
	in.Text = in.Text[:in.Pos] + s + in.Text[in.Pos:]
	in.Pos += len(s)
}

func (in *Input) Backspace() {
	if in.All {
		*in = Input{}
		return
	}
	before := DropLastGrapheme(in.Text[:in.Pos])
	in.Text = before + in.Text[in.Pos:]
	in.Pos = len(before)
}

// DeleteBack deletes from byte offset q to the cursor, all of the text
// when it is all selected (C-w and C-u, §7.9).
func (in *Input) DeleteBack(q int) {
	if in.All {
		*in = Input{}
		return
	}
	in.Text, in.Pos = in.Text[:q]+in.Text[in.Pos:], q
}

// Left and Right first drop a selection, the cursor to its end that way.
func (in *Input) Left() {
	if in.All {
		in.All, in.Pos = false, 0
		return
	}
	in.Pos = len(DropLastGrapheme(in.Text[:in.Pos]))
}

func (in *Input) Right() {
	if in.All {
		in.All, in.Pos = false, len(in.Text)
		return
	}
	gr, _ := ansi.FirstGraphemeCluster(in.Text[in.Pos:], ansi.GraphemeWidth)
	in.Pos += len(gr)
}

// Draw paints the text on r's first row as a grid cell reads (a newline
// a dim ↵, §7.6), selected on the select color, scrolled so the cursor
// stays in view, and returns where the cursor goes.
func (in Input) Draw(f *Frame, r uv.Rectangle, st uv.Style) uv.Position {
	start := 0
	for Width(Printable(in.Text[start:in.Pos])) >= r.Dx() && start < in.Pos { // the cursor takes a cell too
		gr, _ := ansi.FirstGraphemeCluster(in.Text[start:], ansi.GraphemeWidth)
		start += len(gr)
	}
	if in.All {
		st.Bg = f.Theme.Select
	}
	mark := st
	mark.Fg = f.Theme.Dim
	drawCell(f, r.Min.X, r.Min.Y, r.Max.X, Printable(in.Text[start:]), st, mark)
	return uv.Pos(r.Min.X+Width(Printable(in.Text[start:in.Pos])), r.Min.Y)
}
