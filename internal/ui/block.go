package ui

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// Hint is a clickable key hint. Key text always comes from the keymap.
type Hint struct {
	Label, Key string
	Action     string
	Button     bool        // Label drawn as a filled button, e.g. "▶ run" (§7.8)
	Color      color.Color // Label color when not a button; default dim
}

// titleText is how a hint reads on a border: "Label Key".
func (h Hint) titleText() string {
	switch {
	case h.Label == "":
		return h.Key
	case h.Key == "":
		return h.Label
	case h.Button:
		return " " + h.Label + "  " + h.Key
	}
	return h.Label + " " + h.Key
}

// Block is a single-line box with a title on the left of the top border and
// hints on its right (tech-design §7.2, §7.8):
//
//	┌─ ⟨1⟩ data · t_order ──────── hint hint ─┐
type Block struct {
	Title   string
	Hints   []Hint
	Focused bool
	Pane    int
}

// Draw paints the box over r and returns the inner area.
func (b Block) Draw(f *Frame, r uv.Rectangle) uv.Rectangle {
	th := f.Theme
	f.Fill(r, uv.Style{Bg: th.PaneBg})
	if r.Dx() < 2 || r.Dy() < 2 {
		return uv.Rectangle{}
	}
	bc, tc := th.Border, th.Dim
	if b.Focused {
		bc, tc = th.Focus, th.Focus
	}
	bs := uv.Style{Fg: bc, Bg: th.PaneBg}
	x0, y0, x1, y1 := r.Min.X, r.Min.Y, r.Max.X-1, r.Max.Y-1
	for x := x0 + 1; x < x1; x++ {
		f.Text(x, y0, x+1, "─", bs)
		f.Text(x, y1, x+1, "─", bs)
	}
	for y := y0 + 1; y < y1; y++ {
		f.Text(x0, y, x0+1, "│", bs)
		f.Text(x1, y, x1+1, "│", bs)
	}
	f.Text(x0, y0, x0+1, "┌", bs)
	f.Text(x1, y0, x1+1, "┐", bs)
	f.Text(x0, y1, x0+1, "└", bs)
	f.Text(x1, y1, x1+1, "┘", bs)

	// Title: "┌─ title ─"; hints: "─ h1 h2 ─┐". Hints go first when space runs out.
	room := x1 - x0 - 1 - 4 // cells between the corners, less "─ " … " " … "─"
	hints, hw := b.Hints, 0
	for _, h := range hints {
		hw += Width(h.titleText()) + 1
	}
	for len(hints) > 0 && Width(b.Title)+hw+1 > room { // drop from the left
		hw -= Width(hints[0].titleText()) + 1
		hints = hints[1:]
	}
	if room > 0 && b.Title != "" {
		t := " " + Truncate(b.Title, room) + " "
		f.Text(x0+2, y0, x1, t, uv.Style{Fg: tc, Bg: th.PaneBg})
	}
	if hw > 0 {
		x := x1 - 1 - hw - 1
		for _, h := range hints {
			x = f.Text(x, y0, x1, " ", bs)
			x = drawTitleHint(f, x, y0, x1, h, b.Pane)
		}
		f.Text(x, y0, x1, " ", bs)
	}
	return uv.Rect(x0+1, y0+1, x1-x0-1, y1-y0-1)
}

func drawTitleHint(f *Frame, x, y, right int, h Hint, pane int) int {
	th := f.Theme
	f.Region(uv.Rect(x, y, Width(h.titleText()), 1), Target{Kind: KindHint, Pane: pane, Action: h.Action})
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	if h.Button {
		x = f.Text(x, y, right, " "+h.Label+" ", uv.Style{Fg: th.Bg, Bg: th.Focus, Attrs: uv.AttrBold})
		return f.Text(x, y, right, " "+h.Key, dim)
	}
	if h.Label != "" {
		lc := dim
		if h.Color != nil {
			lc.Fg = h.Color
		}
		x = f.Text(x, y, right, h.Label, lc)
		if h.Key == "" {
			return x
		}
		x = f.Text(x, y, right, " ", dim)
	}
	return f.Text(x, y, right, h.Key, dim)
}
