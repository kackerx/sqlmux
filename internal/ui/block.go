package ui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Hint is a clickable key hint. Key text always comes from the keymap.
type Hint struct {
	Label, Key string
	Action     string
	Button     bool        // Label drawn as a filled button, e.g. "▶ run" (§7.8); Key unused
	Color      color.Color // Label color when not a button; default dim
	Prio       int         // title hints: lower is placed first when space runs out
	Attached   bool        // key text of the hint before it: shown only with it (§7.8); needs a higher Prio than that hint
}

// titleText is how a hint reads on a border: "Label Key". It must match what
// titleHintText draws: layout and the hit region are measured from it.
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
	N       int // shown as ⟨n⟩
	Icon    Icon
	Title   string // the pane type, after the icon
	Object  string // "· object" part; truncated first
	Hints   []Hint // in drawing order
	Focused bool
	Pane    int
	// TitleAction makes the drawn title a button (the sidebar's schema, §7.8).
	TitleAction string
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
		st := uv.Style{Fg: tc, Bg: th.PaneBg}
		t := uv.Rect(x0+2, y0, min(Width(" "+title+" "), x1-x0-2), 1)
		if b.TitleAction != "" && f.Region(t, Target{Kind: KindHint, Pane: b.Pane, Action: b.TitleAction}) {
			st.Bg = th.Select
		}
		f.Text(x0+2, y0, x1, " "+title+" ", st)
		// An icon with its own color keeps it (§7.7).
		if n := fmt.Sprintf("⟨%d⟩ ", b.N); b.Icon.Fg != nil && strings.HasPrefix(title, n+b.Icon.Text) {
			f.Text(x0+3+Width(n), y0, x1, b.Icon.Text, b.Icon.On(st))
		}
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

// fit shares the top border per §7.8: reserve the shortest title
// "⟨n⟩ <icon> type", place hints greedily from the highest priority (one that
// doesn't fit is skipped, the next is still tried), give what's left to the
// object name, and fall back to "⟨n⟩" alone.
func (b Block) fit(room int) (string, []Hint) {
	n := fmt.Sprintf("⟨%d⟩", b.N)
	head := n
	if b.Title != "" {
		head += " " + strings.TrimLeft(b.Icon.Text+" "+b.Title, " ")
	}
	if Width(head) > room {
		return Truncate(n, room), nil
	}
	order := make([]int, len(b.Hints))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int { return b.Hints[i].Prio - b.Hints[j].Prio })
	keep := make([]bool, len(b.Hints))
	used := 0
	for _, i := range order {
		if b.Hints[i].Attached && (i == 0 || !keep[i-1]) {
			continue
		}
		// +1: the space between the title and the first hint.
		if w := 1 + Width(b.Hints[i].titleText()); Width(head)+1+used+w <= room {
			keep[i], used = true, used+w
		}
	}
	var hints []Hint
	for i, h := range b.Hints {
		if keep[i] {
			hints = append(hints, h)
		}
	}
	avail := room
	if len(hints) > 0 {
		avail -= used + 1
	}
	full := head + " · " + b.Object
	switch obj := avail - Width(head+" · "); {
	case b.Object == "":
		return head, hints
	case Width(full) <= avail:
		return full, hints
	case obj >= 2: // at least "x…"
		return head + " · " + Truncate(b.Object, obj), hints
	}
	return head, hints
}

func hintsWidth(hs []Hint) int {
	w := 0
	for _, h := range hs {
		w += 1 + Width(h.titleText()) // each hint is preceded by a space
	}
	return w
}

// drawTitleHint draws h at x as a button: its hit region is what it covers,
// and it lights up under the pointer.
func drawTitleHint(f *Frame, x, y, right int, h Hint, pane int) int {
	r := uv.Rect(x, y, min(Width(h.titleText()), right-x), 1)
	hover := f.Region(r, Target{Kind: KindHint, Pane: pane, Action: h.Action})
	return titleHintText(f, x, y, right, h, hover)
}

func titleHintText(f *Frame, x, y, right int, h Hint, hover bool) int {
	th := f.Theme
	bg := th.PaneBg
	if hover {
		bg = th.Select
	}
	dim := uv.Style{Fg: th.Dim, Bg: bg}
	if h.Button {
		st := uv.Style{Fg: th.Bg, Bg: th.Focus, Attrs: uv.AttrBold}
		if hover {
			st.Bg = th.Warn
		}
		return f.Text(x, y, right, " "+h.Label+" ", st)
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
