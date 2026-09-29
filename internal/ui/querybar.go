package ui

import (
	"cmp"
	"image/color"
	"slices"

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

// Button is a tool button (§7.8「工具按钮」): an icon that runs Action when
// clicked ("": not clickable, nothing to do yet or now).
type Button struct {
	Icon   Icon
	Action string
	Tail   string      // after the icon: save's changes (Q-05), auto refresh's interval
	Fg     color.Color // its color; the icon's own from [icon] wins, but when Plain
	Plain  bool        // a state button unlit: Fg, whatever [icon] says
}

// ButtonGroup is buttons side by side on one sep box, or none when Bare
// (§7.8「工具按钮」). On a short row the group with the least Stay gives
// way first.
type ButtonGroup struct {
	Buttons []Button
	Bare    bool
	Stay    int
}

// width is " <icon> ", or " <icon> tail ".
func (b Button) width() int {
	if b.Tail != "" {
		return Width(b.Icon.Text) + Width(b.Tail) + 3
	}
	return Width(b.Icon.Text) + 2
}

// QueryBar is the two rows above a data pane's table (§7.8「查询条」):
// WHERE and its input, then the chips, the buttons (Q-05) and what the last
// query returned.
type QueryBar struct {
	Where   Input
	Typing  bool // the WHERE input has the keys
	Chips   []Chip
	Buttons []ButtonGroup // left to right (§7.8「工具按钮」)
	Right   string        // "auto · 6000 行 · 12ms"
	Note    Note          // how a save went, in Right's place while it stands (Q-06)
	Pane    int
	Names   SQLNames // the WHERE's tables and columns, for its colors (§7.3)
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

// ButtonRect is where the button running action goes when the bar is
// drawn in r; empty when it gave way.
func (q QueryBar) ButtonRect(r uv.Rectangle, action string) uv.Rectangle {
	chips, groups := q.shown(r)
	for i, g := range q.buttonRects(r, chips, groups) {
		for j, br := range g {
			if q.Buttons[i].Buttons[j].Action == action {
				return br
			}
		}
	}
	return uv.Rectangle{}
}

// buttonRects is where the buttons of groups, the shown ones, go on the
// second row past chips chips, by group: side by side, two columns
// between groups; none for a group given way.
func (q QueryBar) buttonRects(r uv.Rectangle, chips int, groups []int) [][]uv.Rectangle {
	x := q.end(r, chips, nil)
	out := make([][]uv.Rectangle, len(q.Buttons))
	for i, g := range groups {
		if i > 0 {
			x += 2
		}
		for _, b := range q.Buttons[g].Buttons {
			out[g] = append(out[g], uv.Rect(x, r.Min.Y+1, b.width(), 1))
			x += b.width()
		}
	}
	return out
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
	if c := q.Where.DrawSQL(f, q.InputRect(r), bg, q.Names); q.Typing {
		cursor = c
	}
	if r.Dy() < 2 {
		return cursor
	}
	y := r.Min.Y + 1
	chips, groups := q.shown(r)
	for i, cr := range q.chipRects(r)[:chips] {
		c := q.Chips[i]
		chip := uv.Style{Fg: th.Fg, Bg: th.Sep}
		var icon uv.Rectangle // its own button: lit on its own, the rest of the chip without it (§7.8)
		if c.Icon.Text != "" {
			w := Width(c.Icon.Text) + 1 // and the space after it; where the chip's text puts it, so none when cut off
			icon = uv.Rect(cr.Min.X+Width(c.text())-w, y, w, 1).Intersect(cr)
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
			if !icon.Empty() && f.Region(icon, Target{Kind: KindHint, Pane: q.Pane, Action: c.IconAction}) {
				ist.Bg = th.Select
			}
			f.Text(x, y, cr.Max.X, c.Icon.Text, c.Icon.On(ist)) // a theme's color for it wins (§7.7)
			f.Text(x+Width(c.Icon.Text), y, cr.Max.X, " ", uv.Style{Bg: ist.Bg})
		}
	}
	for i, g := range q.buttonRects(r, chips, groups) { // a group's box on sep, a button lit whole under the pointer (§7.8)
		box := th.Sep
		if q.Buttons[i].Bare {
			box = th.PaneBg
		}
		for j, br := range g {
			b := q.Buttons[i].Buttons[j]
			st := uv.Style{Fg: cmp.Or[color.Color](b.Fg, th.Info), Bg: box}
			if b.Action != "" && f.Region(br, Target{Kind: KindHint, Pane: q.Pane, Action: b.Action}) {
				st.Bg = th.Select
			}
			icon := b.Icon.On(st) // a theme's color for it wins (§7.7), lit
			if b.Plain {
				icon = st
			}
			f.Text(br.Min.X, y, r.Max.X-1, " ", st)
			x := f.Text(br.Min.X+1, y, r.Max.X-1, b.Icon.Text, icon)
			if b.Tail != "" {
				x = f.Text(x, y, r.Max.X-1, " "+b.Tail, st)
			}
			f.Text(x, y, r.Max.X-1, " ", st)
		}
	}
	x = q.end(r, chips, groups)
	right, st := q.Right, dim
	if n := q.Note; n != (Note{}) {
		if right = n.Fit(r.Max.X - 2 - x); n.Fg != nil {
			st.Fg = n.Fg
		}
	}
	if rx := r.Max.X - 1 - Width(right); rx > x {
		f.Text(rx, y, r.Max.X-1, right, st)
	}
	return cursor
}

// shown is how many chips show and which groups of buttons, left to
// right: all that fit, and a note, which goes before them: whole groups
// give way, the least Stay first, then the chips from the right (§7.8
// 「查询条」). The count in Right just is not shown.
func (q QueryBar) shown(r uv.Rectangle) (chips int, groups []int) {
	chips = len(q.Chips)
	for i := range q.Buttons {
		groups = append(groups, i)
	}
	need := Width(q.Note.Head + q.Note.Mid + q.Note.Tail)
	for chips+len(groups) > 0 && q.end(r, chips, groups)+need >= r.Max.X-1 {
		if len(groups) == 0 {
			chips--
			continue
		}
		least := slices.MinFunc(groups, func(a, b int) int { return cmp.Compare(q.Buttons[a].Stay, q.Buttons[b].Stay) })
		groups = slices.DeleteFunc(groups, func(g int) bool { return g == least })
	}
	return chips, groups
}

// end is where the first chips and the groups of buttons end on the
// second row: what is right of them starts a column after it.
func (q QueryBar) end(r uv.Rectangle, chips int, groups []int) int {
	x := r.Min.X + 1
	for _, c := range q.Chips[:chips] {
		x += Width(c.text()) + 1
	}
	if chips > 0 {
		x++ // two columns after the last chip
	}
	for i, g := range groups {
		if i > 0 {
			x += 2 // two between groups
		}
		for _, b := range q.Buttons[g].Buttons {
			x += b.width()
		}
	}
	if len(groups) > 0 {
		x++
	}
	return x
}

// Note is how a save went (§10.3): Mid, a database's error, is cut to fit
// between Head (the row) and Tail (，已回滚).
type Note struct {
	Head, Mid, Tail string
	Fg              color.Color // dim when nil
}

// Fit is n in w columns: its middle cut to fit, the ends whole.
func (n Note) Fit(w int) string {
	if room := w - Width(n.Head+n.Tail); Width(n.Mid) > room {
		n.Mid = Truncate(n.Mid, max(room, 1))
	}
	return n.Head + n.Mid + n.Tail
}
