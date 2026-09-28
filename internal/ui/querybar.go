package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// Chip is one of the query bar's second row (Q-03): " LABEL value ". With
// Input set, the value is being typed (PAGE's page number) and Suffix
// follows it. An Icon after the value is a button of its own: ORDER's
// direction runs IconAction, the rest of the chip Action (§7.8).
type Chip struct {
	Label, Value string
	Action       string
	Input        *Input
	Suffix       string
	Icon         Icon
	IconAction   string
}

func (c Chip) value() string {
	if c.Input != nil {
		return "[" + c.Input.Text + "]" + c.Suffix
	}
	return c.Value
}

func (c Chip) text() string {
	if c.Icon.Text != "" {
		return " " + c.Label + " " + c.value() + " " + c.Icon.Text + " "
	}
	return " " + c.Label + " " + c.value() + " "
}

// Button is an icon that runs Action when clicked ("": none yet).
type Button struct {
	Icon   Icon
	Action string
}

// QueryBar is the two rows above a data pane's table (§7.8「查询条」):
// WHERE and its input, then the chips, the buttons (Q-05) and what the last
// query returned.
type QueryBar struct {
	Where   Input
	Typing  bool // the WHERE input has the keys
	Chips   []Chip
	Buttons []Button
	Right   string // "auto · 6000 行 · 12ms"
	Pane    int
}

// QueryBarRows is how tall a query bar is.
const QueryBarRows = 2

// InputRect is where the WHERE input goes when the bar is drawn in r: after
// "WHERE ", short of the ▾.
func (q QueryBar) InputRect(r uv.Rectangle) uv.Rectangle {
	x := r.Min.X + 1 + Width("WHERE") + 1
	return uv.Rect(x, r.Min.Y, max(r.Max.X-2-x-1, 0), 1)
}

// ChipRect is where the chip running action goes when the bar is drawn in r.
func (q QueryBar) ChipRect(r uv.Rectangle, action string) uv.Rectangle {
	for i, c := range q.Chips {
		if c.Action == action {
			return q.chipRects(r)[i]
		}
	}
	return uv.Rectangle{}
}

func (q QueryBar) chipRects(r uv.Rectangle) []uv.Rectangle {
	var rects []uv.Rectangle
	x := r.Min.X + 1
	for _, c := range q.Chips {
		w := Width(c.text())
		rects = append(rects, uv.Rect(x, r.Min.Y+1, min(w, max(r.Max.X-x, 0)), 1))
		x += w + 1
	}
	return rects
}

// Draw paints the bar on r's first two rows and returns where the cursor
// goes when an input in it has the keys, else (-1, -1).
func (q QueryBar) Draw(f *Frame, r uv.Rectangle) uv.Position {
	th := f.Theme
	cursor := uv.Pos(-1, -1)
	if r.Dy() < 1 {
		return cursor
	}
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	f.Region(uv.Rect(r.Min.X, r.Min.Y, r.Dx(), 1), Target{Kind: KindHint, Pane: q.Pane, Action: "grid.where"}) // a click types in it (Q-01)
	x := f.Text(r.Min.X+1, r.Min.Y, r.Max.X-1, "WHERE", uv.Style{Fg: th.Keyword, Bg: th.PaneBg, Attrs: uv.AttrBold})
	vx := r.Max.X - 2 // ▾: the history and favorites (Q-02)
	vst := dim
	if f.Region(uv.Rect(vx, r.Min.Y, 1, 1), Target{Kind: KindHint, Pane: q.Pane, Action: "where.history"}) {
		vst.Bg = th.Select
	}
	f.Text(vx, r.Min.Y, r.Max.X, "▾", vst)
	if c := q.Where.Draw(f, q.InputRect(r), bg); q.Typing {
		cursor = c
	}
	if r.Dy() < 2 {
		return cursor
	}
	y := r.Min.Y + 1
	for i, cr := range q.chipRects(r) {
		c := q.Chips[i]
		chip := uv.Style{Fg: th.Fg, Bg: th.Sep}
		var icon uv.Rectangle // its own button: lit on its own, the rest of the chip without it (§7.8)
		if c.Icon.Text != "" {
			w := Width(c.Icon.Text) + 1 // and the space after it
			icon = uv.Rect(cr.Max.X-w, y, w, 1).Intersect(cr)
		}
		if f.Region(cr, Target{Kind: KindHint, Pane: q.Pane, Action: c.Action}) && !f.Mouse.In(icon) {
			chip.Bg = th.Select
		}
		x := f.Text(cr.Min.X, y, cr.Max.X, " "+c.Label+" ", uv.Style{Fg: th.Dim, Bg: chip.Bg})
		if c.Input != nil { // after the [
			cursor = uv.Pos(x+1+Width(c.Input.Text[:c.Input.Pos]), y)
		}
		x = f.Text(x, y, cr.Max.X, c.value()+" ", chip)
		if c.Icon.Text != "" { // registered after the chip: on top for clicks
			ist := uv.Style{Fg: th.Warn, Bg: th.Sep}
			if f.Region(icon, Target{Kind: KindHint, Pane: q.Pane, Action: c.IconAction}) {
				ist.Bg = th.Select
			}
			f.Text(x, y, cr.Max.X, c.Icon.Text, c.Icon.On(ist)) // a theme's color for it wins (§7.7)
			f.Text(x+Width(c.Icon.Text), y, cr.Max.X, " ", uv.Style{Bg: ist.Bg})
		}
	}
	x = r.Min.X + 1
	if rects := q.chipRects(r); len(rects) > 0 {
		x = rects[len(rects)-1].Max.X + 2
	}
	for _, b := range q.Buttons { // " <icon> ", a column apart, lit whole under the pointer (§7.8)
		st := uv.Style{Fg: th.Info, Bg: th.PaneBg}
		w := min(Width(b.Icon.Text)+2, max(r.Max.X-1-x, 0))
		if b.Action != "" && f.Region(uv.Rect(x, y, w, 1), Target{Kind: KindHint, Pane: q.Pane, Action: b.Action}) {
			st.Bg = th.Select
		}
		f.Text(x, y, r.Max.X-1, " ", st)
		f.Text(x+1, y, r.Max.X-1, b.Icon.Text, b.Icon.On(st)) // a theme's color for it wins (§7.7)
		f.Text(x+1+Width(b.Icon.Text), y, r.Max.X-1, " ", st)
		x += Width(b.Icon.Text) + 3
	}
	if rx := r.Max.X - 1 - Width(q.Right); rx > x {
		f.Text(rx, y, r.Max.X-1, q.Right, dim)
	}
	return cursor
}
