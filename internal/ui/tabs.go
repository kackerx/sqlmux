package ui

import (
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
)

// Tabs is a pane's tab bar, drawn on the last inner row (§7.2, §7.8), each
// tab with its type's icon (a landing tab has none, §7.7):
//
//	1:▦ t_order* │ 2:▦ t_user- │ 3:新 tab │ +      hjkl · ↵ edit · gt/gT
type Tabs struct {
	Names     []string
	Icons     []Icon // by tab; a zero Icon for none
	Cur, Prev int
	Hints     []Hint // right-aligned "Key Label" items
	Pane      int
	NoNew     bool // no + to open a tab: the result area's (§11)
	// Close is what the tab under the pointer shows at its mark, a click
	// on it closing that tab (F3.35); none for none. The first Keep tabs
	// have none: the result area's log.
	Close Icon
	Keep  int
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
		num, ic := fmt.Sprintf(" %d:", i+1), t.Icons[i]
		w := Width(num+n+mark) + 1
		if ic.Text != "" {
			w += Width(ic.Text) + 1
		}
		tab := uv.Rect(x, y, max(min(w, r.Max.X-x), 0), 1) // what shows of it: the pane's border is no tab's
		f.Region(tab, Target{Kind: KindTab, Pane: t.Pane, I: i})
		x = f.Text(x, y, r.Max.X, num, st)
		if ic.Text != "" {
			x = f.Text(x, y, r.Max.X, ic.Text, ic.On(st))
			x = f.Text(x, y, r.Max.X, " ", st)
		}
		x = f.Text(x, y, r.Max.X, n, st)
		tail := mark + " "
		if t.Close.Text != "" && i >= t.Keep && f.Mouse.In(tab) { // × in the mark's place, or the blank's after the name
			cst := st
			if f.Region(uv.Rect(x, y, max(min(Width(t.Close.Text), r.Max.X-x), 0), 1), Target{Kind: KindButton, Pane: t.Pane, Action: fmt.Sprintf("tab.close.at %d %d", t.Pane, i)}) {
				cst.Bg = th.Select
			}
			x, tail = f.Text(x, y, r.Max.X, t.Close.Text, t.Close.On(cst)), tail[1:]
		}
		x = f.Text(x, y, r.Max.X, tail, st)
	}
	if !t.NoNew {
		if len(t.Names) > 0 {
			x = f.Text(x, y, r.Max.X, "│", uv.Style{Fg: th.Sep, Bg: th.Bg})
		}
		plus := base
		if f.Region(uv.Rect(x, y, min(3, r.Max.X-x), 1), Target{Kind: KindHint, Pane: t.Pane, Action: "tab.new"}) {
			plus.Bg = th.Select
		}
		x = f.Text(x, y, r.Max.X, " + ", plus)
	}

	if w := hintRowWidth(t.Hints); w > 0 && x+1+w+1 <= r.Max.X {
		hintRow(f, r.Max.X-1-w, y, r.Max.X, t.Hints, base, Target{Kind: KindHint, Pane: t.Pane})
	}
}

// hintRow draws "Key Label" items from x, " · " between them, each clickable
// as t with its own action.
func hintRow(f *Frame, x, y, right int, hs []Hint, st uv.Style, t Target) {
	for i, h := range hs {
		if i > 0 {
			x = f.Text(x, y, right, " · ", st)
		}
		hst := st
		if t.Action = h.Action; h.Action != "" && f.Region(uv.Rect(x, y, min(Width(h.tabText()), right-x), 1), t) {
			hst.Bg = f.Theme.Select
		}
		x = f.Text(x, y, right, h.tabText(), hst)
	}
}

// hintRowWidth is how wide hintRow draws hs; negative when hs is empty.
func hintRowWidth(hs []Hint) int {
	w := -3 // no " · " before the first item
	for _, h := range hs {
		w += Width(h.tabText()) + 3
	}
	return w
}

// tabText is how a hint reads in a tab bar: "Key Label"; its width is the
// hint's hit region, measured before drawing to pick the hover style.
func (h Hint) tabText() string {
	if h.Label == "" {
		return h.Key
	}
	return h.Key + " " + h.Label
}
