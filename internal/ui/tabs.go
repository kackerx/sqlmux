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
	// Production is by tab whether it is of a production connection: its title
	// in that color (F17.9); none for none of them.
	Production []bool
	// Pinned is by tab whether it is pinned, which the pin after its name says (F22.6); a
	// result's own pin is its icon, and not in it.
	Pinned []bool
	Pin    Icon
	// Scroll is where the bar is scrolled to when the tabs do not fit (F17.1):
	// the pane's, Draw moves it to bring the current tab in sight when that
	// or the width has changed since. Nil: not scrolled.
	Scroll *TabScroll
}

// TabScroll is a tab bar's scroll: the first tab shown, and what it was
// when the current tab was brought in sight, so that a click on ‹N or N›
// scrolls without being undone; Shown is how many tabs the last Draw showed.
type TabScroll struct {
	First, Cur, Room, Shown int
}

// tabWidth is how wide tab i draws: the blank, its number, icon, name and mark,
// and the blank after it.
func (t Tabs) tabWidth(i int) int {
	mark := ""
	switch i {
	case t.Cur:
		mark = "*"
	case t.Prev:
		mark = "-"
	}
	w := Width(fmt.Sprintf(" %d:", i+1)+t.Names[i]+mark) + 1 + t.pinWidth(i)
	if ic := t.Icons[i]; ic.Text != "" {
		w += Width(ic.Text) + 1
	}
	return w
}

// pinWidth is the columns tab i's pin takes: a blank and the icon.
func (t Tabs) pinWidth(i int) int {
	if i < len(t.Pinned) && t.Pinned[i] {
		return 1 + Width(t.Pin.Text)
	}
	return 0
}

// tabRow is a row of n items of width(i) columns with a gap of one between them, one of them
// current, which does not always fit and is scrolled: a pane's tabs, and the palette's scopes
// (§7.8, §12).
type tabRow struct {
	n, cur int
	width  func(i int) int
	scroll *TabScroll
}

func (t Tabs) row() tabRow { return tabRow{len(t.Names), t.Cur, t.tabWidth, t.Scroll} }

// window is the items from first to last that fit in room columns, with the
// ‹N before them and the │ N› after when there are some out of sight: at least
// the item at first, cut at the edge if it is wider than room.
func (r tabRow) window(first, room int) (last int) {
	n := r.n
	left := 0
	if first > 0 {
		left = Width(fmt.Sprintf("‹%d", first))
	}
	sum := make([]int, n+1) // sum[i+1]: the items first..i and the │ between
	for i := first; i < n; i++ {
		sum[i+1] = sum[i] + r.width(i)
		if i > first {
			sum[i+1]++
		}
	}
	for last = n - 1; last > first; last-- {
		right := 0
		if last < n-1 {
			right = 1 + Width(fmt.Sprintf(" %d› ", n-1-last)) // the │ before it
		}
		if left+sum[last+1]+right <= room {
			break
		}
	}
	return last
}

// scrolled is the first and last item to draw in room columns: those that fit
// with the ‹N and N› for the others, the current one among them when it
// has just changed (or the room), the first the scroll's otherwise.
func (r tabRow) scrolled(room int) (first, last int) {
	n := r.n
	if n == 0 {
		return 0, -1
	}
	s := r.scroll
	if s == nil {
		s = &TabScroll{}
	}
	first = min(max(s.First, 0), n-1)
	if s.Cur != r.cur || s.Room != room {
		first = min(first, r.cur)
		for r.window(first, room) < r.cur {
			first++
		}
		s.Cur, s.Room = r.cur, room
	}
	for first > 0 && r.window(first-1, room) == n-1 { // the room after the last item is for the ones before it
		first--
	}
	last = r.window(first, room)
	s.First, s.Shown = first, last-first+1
	return first, last
}

func (t Tabs) Draw(f *Frame, r uv.Rectangle) {
	th := f.Theme
	base := uv.Style{Fg: th.Dim, Bg: th.Bg}
	f.Fill(r, base)
	x, y := r.Min.X, r.Min.Y
	plus := 0 // the │ and the + after the tabs
	if !t.NoNew {
		plus = 4
	}
	first, last := t.row().scrolled(r.Dx() - plus)
	if first > 0 {
		x = t.arrow(f, x, y, r.Max.X, fmt.Sprintf("‹%d", first), -1)
	}
	took := false // the last tab's × has the cell after it: the │'s
	for i := first; i <= last; i++ {
		n := t.Names[i]
		if i > first && !took {
			x = f.Text(x, y, r.Max.X, "│", uv.Style{Fg: th.Sep, Bg: th.Bg})
		}
		took = false
		mark, st := "", base
		switch i {
		case t.Cur:
			mark, st = "*", uv.Style{Fg: th.Focus, Bg: th.PaneBg}
		case t.Prev:
			mark = "-"
		}
		if i < len(t.Production) && t.Production[i] {
			st.Fg = th.Production
		}
		num, ic := fmt.Sprintf(" %d:", i+1), t.Icons[i]
		w := Width(num+n+mark) + 1 + t.pinWidth(i)
		if ic.Text != "" {
			w += Width(ic.Text) + 1
		}
		tab := uv.Rect(x, y, max(min(w, r.Max.X-x), 0), 1) // what shows of it: the pane's border is no tab's
		f.Region(tab, Target{Kind: KindTab, Pane: t.Pane, I: i})
		hot := tab // where the pointer shows the ×: with no mark, the cell after the tab its blank takes too
		if mark == "" {
			hot.Max.X = min(hot.Max.X+1, r.Max.X)
		}
		x = f.Text(x, y, r.Max.X, num, st)
		if ic.Text != "" {
			x = f.Text(x, y, r.Max.X, ic.Text, ic.On(st))
			x = f.Text(x, y, r.Max.X, " ", st)
		}
		x = f.Text(x, y, r.Max.X, n, st)
		if t.pinWidth(i) > 0 {
			x = f.Text(x, y, r.Max.X, " ", st)
			x = f.Text(x, y, r.Max.X, t.Pin.Text, t.Pin.On(st))
		}
		tail := mark + " "
		if t.Close.Text != "" && i >= t.Keep && f.Over(hot) {
			// × in the mark's place, or the blank's after the name, and a
			// blank after it on its background: a nerd glyph draws past its
			// cell (§7.7). With no mark that blank is the next │'s cell, so
			// the bar does not move.
			cst := st
			cw := Width(t.Close.Text) + 1
			if f.Region(uv.Rect(x, y, max(min(cw, r.Max.X-x), 0), 1), Target{Kind: KindButton, Pane: t.Pane, Action: fmt.Sprintf("tab.close.at %d %d", t.Pane, i)}) {
				cst.Bg = th.Select
			}
			x = f.Text(x, y, r.Max.X, t.Close.Text, t.Close.On(cst))
			x = f.Text(x, y, r.Max.X, " ", cst)
			took, tail = mark == "", ""
		}
		x = f.Text(x, y, r.Max.X, tail, st)
	}
	if last < len(t.Names)-1 {
		if !took {
			x = f.Text(x, y, r.Max.X, "│", uv.Style{Fg: th.Sep, Bg: th.Bg})
		}
		took = false
		x = t.arrow(f, x, y, r.Max.X, fmt.Sprintf(" %d› ", len(t.Names)-1-last), 1)
	}
	if !t.NoNew {
		if len(t.Names) > 0 && !took {
			x = f.Text(x, y, r.Max.X, "│", uv.Style{Fg: th.Sep, Bg: th.Bg})
		}
		plusSt := base
		if f.Region(uv.Rect(x, y, min(3, r.Max.X-x), 1), Target{Kind: KindHint, Pane: t.Pane, Action: "tab.new"}) {
			plusSt.Bg = th.Select
		}
		x = f.Text(x, y, r.Max.X, " + ", plusSt)
	}

	// The cell an unmarked last tab's × takes with no + after it is kept
	// for it, hovered or not: the hints stay or go alike (F5.6).
	spare := 0
	if n := len(t.Names); t.NoNew && t.Close.Text != "" && n > t.Keep && n-1 != t.Cur && n-1 != t.Prev {
		spare = 1
		if took {
			x--
		}
	}
	if w := hintRowWidth(t.Hints); w > 0 && x+spare+1+w+1 <= r.Max.X {
		hintRow(f, r.Max.X-1-w, y, r.Max.X, t.Hints, base, Target{Kind: KindHint, Pane: t.Pane})
	}
}

// arrow draws the ‹N or N› of the tabs out of sight at x, a click on it
// scrolling a screen toward them (dir -1 or 1); the next x.
func (t Tabs) arrow(f *Frame, x, y, right int, text string, dir int) int {
	st := uv.Style{Fg: f.Theme.Dim, Bg: f.Theme.Bg}
	if f.Region(uv.Rect(x, y, min(Width(text), right-x), 1), Target{Kind: KindHint, Pane: t.Pane, Action: fmt.Sprintf("tab.scroll %d %d", t.Pane, dir)}) {
		st.Bg = f.Theme.Select
	}
	return f.Text(x, y, right, text, st)
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

// leadingHints is how many of hs, from the first, fit whole in a hint row of
// room columns: the last ones are given up, not cut.
func leadingHints(hs []Hint, room int) int {
	n := 0
	for n < len(hs) && hintRowWidth(hs[:n+1]) <= room {
		n++
	}
	return n
}

// tabText is how a hint reads in a tab bar: "Key Label"; its width is the
// hint's hit region, measured before drawing to pick the hover style.
func (h Hint) tabText() string {
	if h.Label == "" {
		return h.Key
	}
	return h.Key + " " + h.Label
}
