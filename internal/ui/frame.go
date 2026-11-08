// Package ui draws the immediate-mode frame (tech-design §7).
package ui

import (
	"cmp"
	"image/color"
	"slices"
	"strings"
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
	KindRow                      // a candidate in an overlay's list (I: its index): click picks it
	KindTreeEdge                 // the gap right of the sidebar: drag to set its width
	KindNode                     // a node of the schema tree (I: its index): click is its ↵
	KindFold                     // a tree node's ▸ / ▾ (I: its index): click opens or closes it
	KindCell                     // a grid cell: focus its pane, then run Action (grid.goto r c)
	KindRowNo                    // a grid row number: as KindCell, to that row in the current column
	KindText                     // a query's text: a click puts the cursor there, a drag selects (§11)
	KindSegment                  // a segment of a form's switch (Pane: its field, I: the segment)
	KindConn                     // a row of the connection manager's list (I: its index)
	KindHead                     // a table's column head (I: the column's index among those shown): click sorts by it (§7.6)
	KindColEdge                  // the │ right of a column head (I: the column): drag to set the column's width (F17.14)
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
	mouse  uv.Position    // pointer, for hover styles, read by Over only; (-1,-1) when unknown
	Probes []uv.Rectangle // what Over was asked about
	Theme  *Theme
	Cursor *uv.Position // the terminal cursor, while an input has focus
	Border uv.Border    // the characters the boxes are drawn with (§7.1, F8.10)
	// FocusBorder is those of the box in focus and of the overlays: Lines picks (F21.5)
	FocusBorder uv.Border
}

// Lines is the characters a box's border is drawn with: a focused pane's and an
// overlay's (focused) are `border_focus_style`'s, the others' `border`'s (§7.3). The one
// place that picks.
func (f *Frame) Lines(focused bool) uv.Border {
	if focused {
		return f.FocusBorder
	}
	return f.Border
}

func NewFrame(w, h int, th *Theme) *Frame {
	buf := uv.NewScreenBuffer(max(w, 0), max(h, 0))
	buf.Method = ansi.GraphemeWidth
	return frameIn(buf, th)
}

func frameIn(buf uv.ScreenBuffer, th *Theme) *Frame {
	return &Frame{Buf: buf, mouse: uv.Pos(-1, -1), Theme: th, Border: uv.NormalBorder(), FocusBorder: uv.NormalBorder()}
}

// BorderOf is the box characters `border` names (§14): single, rounded,
// double, bold, or hidden, which is blanks: the titles show, the lines don't.
// ok is false for any other name.
func BorderOf(name string) (b uv.Border, ok bool) {
	switch name {
	case "single":
		return uv.NormalBorder(), true
	case "rounded":
		return uv.RoundedBorder(), true
	case "double":
		return uv.DoubleBorder(), true
	case "bold":
		return uv.ThickBorder(), true
	case "hidden":
		return uv.HiddenBorder(), true
	}
	return b, false
}

func (f *Frame) Bounds() uv.Rectangle { return f.Buf.Bounds() }

// Region registers r as a hit region for t and reports whether the pointer
// is over it.
func (f *Frame) Region(r uv.Rectangle, t Target) (hover bool) {
	if r.Empty() {
		return false
	}
	f.Hits = append(f.Hits, Hit{r, t})
	return f.Over(r)
}

// SetMouse tells the frame where the pointer is.
func (f *Frame) SetMouse(p uv.Position) { f.mouse = p }

// Over is whether the pointer is over r, the only way to ask: every rect
// asked is kept in Probes, for a frame to stand while the pointer moves, if
// it crosses no edge of them (F8.20).
func (f *Frame) Over(r uv.Rectangle) bool {
	f.Probes = append(f.Probes, r)
	return f.mouse.In(r)
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
		gr, w := firstGrapheme(s)
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

// TextMatch is Text with the runes at pos (a Match's) in the match color,
// on st's own background (§7.3): a grapheme lights up when any of its
// runes matched. Every fuzzy list draws its matches here.
func (f *Frame) TextMatch(x, y, right int, s string, pos []int, st uv.Style) int {
	hl := st
	hl.Fg = f.Theme.Match
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

// Rendered is the frame last drawn: its buffer and the text of each line,
// for RenderReusing to carry to the next frame, and the buffer of the frame
// before it, which the next one is drawn in rather than in a new one: 7200
// cells are 1.1 MB, and the garbage of it, at a frame per pointer move, was
// a third of the work (F8.20).
type Rendered struct {
	text  []string
	cur   uv.ScreenBuffer
	spare uv.ScreenBuffer
}

// NewFrame is ui.NewFrame, in the spare buffer when it is the size wanted;
// the frame is drawn over what it held, which Fill of the whole frame
// first, as App.render does, paints out.
func (r *Rendered) NewFrame(w, h int, th *Theme) *Frame {
	if r.spare.RenderBuffer == nil || r.spare.Width() != w || r.spare.Height() != h {
		return NewFrame(w, h, th)
	}
	return frameIn(r.spare, th)
}

// RenderReusing is Render for a frame that follows another, last: a line
// of the same cells as that frame's has its text, so a pointer moving, which
// changes two lines of 45, does not have the other 43 written again
// (F8.20). A line's text does not depend on the lines around it. last
// becomes this frame's.
func (f *Frame) RenderReusing(last *Rendered) string {
	lines := f.Buf.Lines
	text := make([]string, len(lines))
	for y, l := range lines {
		if y < len(last.text) && y < len(last.cur.Lines) && slices.Equal(l, last.cur.Lines[y]) {
			text[y] = last.text[y]
			continue
		}
		text[y] = l.Render()
	}
	last.text = text
	last.spare, last.cur = last.cur, f.Buf
	return strings.Join(text, "\n")
}

// String is the frame as plain text, for golden tests.
func (f *Frame) String() string { return f.Buf.String() }

// listText draws a list row's text from x on row y, the match positions pos
// lit in st, and its note ending at noteRight in noteSt: the name goes first,
// the note is left out unless it fits with the name whole, and a name too long
// ends in … (F13.9, as the tree's rows, §7.8).
func listText(f *Frame, x, y, noteRight int, text string, pos []int, note string, st, noteSt uv.Style) {
	end := noteRight
	if w := Width(note); note != "" && Width(text)+1+w <= noteRight-x {
		f.Text(noteRight-w, y, noteRight, note, noteSt)
		end = noteRight - w - 1
	}
	name, inName := TruncateMatch(text, pos, end-x)
	f.TextMatch(x, y, end, name, inName, st)
}

// InView is the first row a list shows with selection sel in view and rows
// rows to show: top, where the selection is in view already, as nvim's
// completion menu holds still, else the least that brings it in (F13.10).
func InView(top, sel, rows int) int {
	if rows <= 0 { // nothing shows: the top stays, not the row after the selection
		return top
	}
	return max(min(top, sel), sel-rows+1, 0)
}

// ListView is sel and top kept on a list of n rows and in a view of rows.
func ListView(sel, top, n, rows int) (int, int) {
	sel = max(min(sel, n-1), 0)
	return sel, InView(min(top, n-rows), sel, rows)
}
