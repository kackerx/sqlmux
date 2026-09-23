package ui

import (
	"fmt"
	"image/color"
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
)

// Hint is a clickable key hint. Key text always comes from the keymap.
type Hint struct {
	Label, Key string
	Action     string
	Button     bool        // Label drawn as a filled button, e.g. "▶ run" (§7.8); Key unused
	Color      color.Color // Label color when not a button; default dim
	Prio       int         // title hints: higher is dropped first when space runs out
}

// titleText is how a hint reads on a border: "Label Key".
func (h Hint) titleText() string {
	switch {
	case h.Button:
		return " " + h.Label + " "
	case h.Label == "":
		return h.Key
	case h.Key == "":
		return h.Label
	}
	return h.Label + " " + h.Key
}

// Block is a single-line box with a title on the left of the top border and
// hints on its right (tech-design §7.2, §7.8):
//
//	┌─ ⟨1⟩ data · t_order ──────── hint hint ─┐
type Block struct {
	N       int    // shown as ⟨n⟩
	Title   string // "<icon> type"
	Object  string // "· object" part; truncated first
	Hints   []Hint // in drawing order
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
	border := uv.NormalBorder().Style(bs)
	border.Draw(f.Buf, r)
	x0, y0, x1 := r.Min.X, r.Min.Y, r.Max.X-1

	// "┌─ title ─…─ h1 h2 ─┐": room is what's left between the corners after
	// "─ " + " " before the title and "─" before the corner.
	title, hints := b.fit(x1 - x0 - 1 - 4)
	if title != "" {
		f.Text(x0+2, y0, x1, " "+title+" ", uv.Style{Fg: tc, Bg: th.PaneBg})
	}
	if len(hints) > 0 {
		x := x1 - 1 - hintsWidth(hints) - 1
		for _, h := range hints {
			x = f.Text(x, y0, x1, " ", bs)
			x = drawTitleHint(f, x, y0, x1, h, b.Pane)
		}
		f.Text(x, y0, x1, " ", bs)
	}
	y1 := r.Max.Y - 1
	return uv.Rect(x0+1, y0+1, x1-x0-1, y1-y0-1)
}

// fit applies §7.8's fallback when the top border is too narrow: first cut
// the object name down to "⟨n⟩ <icon> type", then drop hints lowest priority
// first, and finally show only "⟨n⟩".
func (b Block) fit(room int) (string, []Hint) {
	head := fmt.Sprintf("⟨%d⟩", b.N)
	if b.Title != "" {
		head += " " + b.Title
	}
	full := head
	if b.Object != "" {
		full += " · " + b.Object
	}
	hints := slices.Clone(b.Hints)
	for {
		avail := room
		if len(hints) > 0 {
			avail -= hintsWidth(hints) + 1
		}
		if avail >= Width(head) {
			if Width(full) <= avail || b.Object == "" {
				return full, hints
			}
			if obj := avail - Width(head+" · "); obj >= 2 { // at least "x…"
				return head + " · " + Truncate(b.Object, obj), hints
			}
			return head, hints
		}
		if len(hints) == 0 {
			return Truncate(fmt.Sprintf("⟨%d⟩", b.N), room), nil
		}
		drop := 0
		for i, h := range hints {
			if h.Prio > hints[drop].Prio {
				drop = i
			}
		}
		hints = slices.Delete(hints, drop, drop+1)
	}
}

func hintsWidth(hs []Hint) int {
	w := 0
	for _, h := range hs {
		w += 1 + Width(h.titleText()) // each hint is preceded by a space
	}
	return w
}

// drawTitleHint draws h at x and registers what it drew as h's hit region.
func drawTitleHint(f *Frame, x, y, right int, h Hint, pane int) int {
	start := x
	x = titleHintText(f, x, y, right, h)
	f.Region(uv.Rect(start, y, x-start, 1), Target{Kind: KindHint, Pane: pane, Action: h.Action})
	return x
}

func titleHintText(f *Frame, x, y, right int, h Hint) int {
	th := f.Theme
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	if h.Button {
		return f.Text(x, y, right, " "+h.Label+" ", uv.Style{Fg: th.Bg, Bg: th.Focus, Attrs: uv.AttrBold})
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
