package ui

import (
	"image/color"
	"strings"

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
	// A WHERE's vim (F3.39), as a console's line 0, drawn by DrawSQL: what
	// VISUAL selects, and what a yank took, flashing (F3.32).
	Sel, Yank Sel
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
// a dim ↵, §7.6), selected on the visual color, scrolled so the cursor
// stays in view, and returns where the cursor goes.
func (in Input) Draw(f *Frame, r uv.Rectangle, st uv.Style) uv.Position {
	return in.draw(f, r, st, nil)
}

// DrawSQL is Draw with the text in SQL's colors, names telling its tables
// and columns apart (§7.3): a WHERE's.
func (in Input) DrawSQL(f *Frame, r uv.Rectangle, st uv.Style, names SQLNames) uv.Position {
	return in.draw(f, r, st, SQLColors(f.Theme, in.Text, names))
}

// draw is Draw, the text's bytes in colors, when there are any.
func (in Input) draw(f *Frame, r uv.Rectangle, st uv.Style, colors []color.Color) uv.Position {
	start := in.Start(r.Dx())
	if in.All {
		st.Bg = f.Theme.Visual
	}
	mark := st
	mark.Fg = f.Theme.Dim
	if colors == nil {
		drawCell(f, r.Min.X, r.Min.Y, r.Max.X, Printable(in.Text[start:]), st, mark)
	}
	// v is the display column, as Sel's
	v := Width(Printable(in.Text[:start]))
	for i, x := start, r.Min.X; colors != nil && i < len(in.Text); { // grapheme by grapheme, in its first byte's color
		gr, _ := ansi.FirstGraphemeCluster(in.Text[i:], ansi.GraphemeWidth)
		cst, w := mark, Width(Printable(gr))
		if !strings.Contains(gr, "\n") {
			cst = st
			cst.Fg = colors[i]
		}
		switch {
		case in.Yank.has(0, i, v, w):
			cst.Fg, cst.Bg = f.Theme.Bg, f.Theme.Yank
		case in.Sel.has(0, i, v, w):
			cst.Bg = f.Theme.Visual
		}
		x, i, v = f.Text(x, r.Min.Y, r.Max.X, Printable(gr), cst), i+len(gr), v+w
	}
	return uv.Pos(r.Min.X+Width(Printable(in.Text[start:in.Pos])), r.Min.Y)
}

// Start is the first byte shown when w columns show the text: the cursor
// stays in view.
func (in Input) Start(w int) int {
	start := 0
	for Width(Printable(in.Text[start:in.Pos])) >= w && start < in.Pos { // the cursor takes a cell too
		gr, _ := ansi.FirstGraphemeCluster(in.Text[start:], ansi.GraphemeWidth)
		start += len(gr)
	}
	return start
}
