package ui

import (
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
)

// Tabs is a pane's tab bar, drawn on the last inner row (§7.2, §7.8):
//
//	1:t_order* │ 2:t_user- │ +            hjkl · ↵ edit · gt/gT
type Tabs struct {
	Names     []string
	Cur, Prev int
	Hints     []Hint // right-aligned "Key Label" items
	Pane      int
}

func (t Tabs) Draw(f *Frame, r uv.Rectangle) {
	th := f.Theme
	base := uv.Style{Fg: th.Dim, Bg: th.Bg}
	f.Fill(r, base)
	x, y := r.Min.X, r.Min.Y
	for i, n := range t.Names {
		if i > 0 {
			x = f.Text(x, y, r.Max.X, "│", uv.Style{Fg: th.Sep, Bg: th.Bg})
		}
		mark, st := "", base
		switch i {
		case t.Cur:
			mark, st = "*", uv.Style{Fg: th.Focus, Bg: th.PaneBg}
		case t.Prev:
			mark = "-"
		}
		start := x
		x = f.Text(x, y, r.Max.X, fmt.Sprintf(" %d:%s%s ", i+1, n, mark), st)
		f.Region(uv.Rect(start, y, x-start, 1), Target{Kind: KindTab, Pane: t.Pane, I: i})
	}
	if len(t.Names) > 0 {
		x = f.Text(x, y, r.Max.X, "│", uv.Style{Fg: th.Sep, Bg: th.Bg})
	}
	plus := base
	if f.Region(uv.Rect(x, y, min(3, r.Max.X-x), 1), Target{Kind: KindTab, Pane: t.Pane, I: -1}) {
		plus.Bg = th.Select
	}
	x = f.Text(x, y, r.Max.X, " + ", plus)

	w := -3 // " · " before the first item is not drawn
	for _, h := range t.Hints {
		w += Width(h.tabText()) + 3
	}
	if w <= 0 || x+1+w+1 > r.Max.X {
		return
	}
	hx := r.Max.X - 1 - w
	for i, h := range t.Hints {
		if i > 0 {
			hx = f.Text(hx, y, r.Max.X, " · ", base)
		}
		st := base
		if h.Action != "" && f.Region(uv.Rect(hx, y, Width(h.tabText()), 1), Target{Kind: KindHint, Pane: t.Pane, Action: h.Action}) {
			st.Bg = th.Select
		}
		hx = f.Text(hx, y, r.Max.X, h.tabText(), st)
	}
}

// tabText is how a hint reads in a tab bar: "Key Label".
func (h Hint) tabText() string {
	if h.Label == "" {
		return h.Key
	}
	return h.Key + " " + h.Label
}
