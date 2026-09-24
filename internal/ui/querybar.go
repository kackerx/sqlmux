package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// Chip is one of the query bar's second row (Q-03): " LABEL value ". With
// Input set, the value is being typed (PAGE's page number) and Suffix
// follows it.
type Chip struct {
	Label, Value string
	Action       string
	Input        *Input
	Suffix       string
}

func (c Chip) value() string {
	if c.Input != nil {
		return "[" + c.Input.Text + "]" + c.Suffix
	}
	return c.Value
}

func (c Chip) text() string { return " " + c.Label + " " + c.value() + " " }

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
	if c := q.Where.Draw(f, uv.Rect(x+1, r.Min.Y, max(r.Max.X-1-x-1, 0), 1), bg); q.Typing {
		cursor = c
	}
	if r.Dy() < 2 {
		return cursor
	}
	y := r.Min.Y + 1
	for i, cr := range q.chipRects(r) {
		c := q.Chips[i]
		chip := uv.Style{Fg: th.Fg, Bg: th.Sep}
		if f.Region(cr, Target{Kind: KindHint, Pane: q.Pane, Action: c.Action}) {
			chip.Bg = th.Select
		}
		x := f.Text(cr.Min.X, y, cr.Max.X, " "+c.Label+" ", uv.Style{Fg: th.Dim, Bg: chip.Bg})
		f.Text(x, y, cr.Max.X, c.value()+" ", chip)
		if c.Input != nil { // after the [
			cursor = uv.Pos(x+1+Width(c.Input.Text[:c.Input.Pos]), y)
		}
	}
	x = r.Min.X + 1
	if rects := q.chipRects(r); len(rects) > 0 {
		x = rects[len(rects)-1].Max.X + 2
	}
	for _, b := range q.Buttons {
		st := dim
		if b.Action != "" && f.Region(uv.Rect(x, y, min(Width(b.Icon.Text), max(r.Max.X-x, 0)), 1), Target{Kind: KindHint, Pane: q.Pane, Action: b.Action}) {
			st.Bg = th.Select
		}
		x = f.Text(x, y, r.Max.X-1, b.Icon.Text, b.Icon.On(st)) + 1 // a theme's color for it wins (§7.7)
	}
	if rx := r.Max.X - 1 - Width(q.Right); rx > x {
		f.Text(rx, y, r.Max.X-1, q.Right, dim)
	}
	return cursor
}
