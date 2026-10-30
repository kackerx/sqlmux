package ui

import (
	"cmp"
	"image/color"
	"math"
	"slices"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Chip is one of the query bar's second row (Q-03): " LABEL value ". With
// Input set, the value is being typed (PAGE's page number) and Suffix
// follows it. An Icon after the value is a button of its own: ORDER's
// direction runs IconAction, the rest of the chip Action (§7.8).
type Chip struct {
	Label, Value string
	Short        string // Value when the chips do not all fit; "" has none
	Action       string
	Input        *Input
	Suffix       string
	Icon         Icon
	IconAction   string
	Stay         int // against the button groups' Stay: the least gives way first (shown)
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

// ButtonGroup is buttons side by side, a │ between it and the next on the
// row's one sep strip (§7.8「工具按钮」). On a short row the group or chip
// with the least Stay gives way first.
type ButtonGroup struct {
	Buttons []Button
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
	// Info is a Redis key's row over WHERE (§8.7): chips that say what the key
	// is, and run what can be done to it; none for a table, which has no such row.
	Info []Chip
	// Label is the word before the input, "WHERE" when empty; five columns, as
	// that is: a key's MATCH.
	Label   string
	Where   Input
	Typing  bool // the WHERE input has the keys
	Chips   []Chip
	Buttons []ButtonGroup // left to right (§7.8「工具按钮」)
	Right   string        // "auto · 6000 行 · 12ms"
	// Keep is Right cut short, "3 行": the last of the second row to give way,
	// after ORDER, shown when Right does not fit (F19.4). A note stands in
	// Right's place, and Keep with it.
	Keep string
	// Selected is the count of records selected, after the buttons and ahead
	// of Right, which it does not go with: it gives way by SelectedStay as a
	// chip or a group does (F10.7).
	Selected     string
	SelectedStay int
	Note         Note // how a save went, in Right's place while it stands (Q-06)
	Pane         int
	Names        SQLNames // the WHERE's tables and columns, for its colors (§7.3)
	Dialect      sqlkit.Dialect
	// History and HistKey, the icon and the key that open the WHERE's
	// history, show before its ▾ when the input keeps room (F8.14); no key,
	// none shown.
	History Icon
	HistKey string
}

// QueryBarRows is how tall a query bar is, its Info row not counted: InfoRows.
const QueryBarRows = 2

// InfoRows is how many rows more than QueryBarRows a bar with an Info takes.
const InfoRows = 1

// below is the rows of r that WHERE and the chips have: the Info row is the first.
func (q QueryBar) below(r uv.Rectangle) uv.Rectangle {
	if len(q.Info) > 0 {
		r.Min.Y = min(r.Min.Y+InfoRows, r.Max.Y)
	}
	return r
}

// minWhere is the room the WHERE input keeps before the history's hint
// takes any.
const minWhere = 16

// histHint is the history's icon and key, "󰋚 C-r", when the bar drawn in r
// has room for them besides the input and the ▾; "" when not.
func (q QueryBar) histHint(r uv.Rectangle) string {
	if q.HistKey == "" {
		return ""
	}
	h := q.History.Text + " " + q.HistKey
	if r.Max.X-2-1-Width(h)-1-(r.Min.X+1+Width("WHERE")+1) < minWhere {
		return ""
	}
	return h
}

// InputRect is where the WHERE input goes when the bar is drawn in r: after
// "WHERE ", short of the history's hint and the ▾.
func (q QueryBar) InputRect(r uv.Rectangle) uv.Rectangle {
	r = q.below(r)
	x := r.Min.X + 1 + Width("WHERE") + 1
	end := r.Max.X - 2 - 1 // the ▾ and a space before it
	if h := q.histHint(r); h != "" {
		end -= Width(h) + 1
	}
	return uv.Rect(x, r.Min.Y, max(end-x, 0), 1)
}

// fit is q with every chip that has a Short showing it when the chips would
// not fit r as they are, so one long value cannot push the others off (F12.1).
func (q QueryBar) fit(r uv.Rectangle) QueryBar {
	if q.end(r, len(q.Chips), nil) < r.Max.X-1 {
		return q
	}
	q.Chips = slices.Clone(q.Chips)
	for i, c := range q.Chips {
		if c.Short != "" {
			q.Chips[i].Value = c.Short
		}
	}
	return q
}

// ChipRect is where the chip running action goes when the bar is drawn in r.
func (q QueryBar) ChipRect(r uv.Rectangle, action string) uv.Rectangle {
	for i, c := range q.Info {
		if c.Action == action {
			return rowRects(q.Info, r, r.Min.Y)[i]
		}
	}
	r = q.below(r)
	q = q.fit(r)
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
	r = q.below(r)
	q = q.fit(r)
	chips, groups, _ := q.shown(r)
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
// second row past chips chips, by group: side by side, a column for the │
// between groups; none for a group given way.
func (q QueryBar) buttonRects(r uv.Rectangle, chips int, groups []int) [][]uv.Rectangle {
	x := q.end(r, chips, nil)
	out := make([][]uv.Rectangle, len(q.Buttons))
	for i, g := range groups {
		if i > 0 {
			x++
		}
		for _, b := range q.Buttons[g].Buttons {
			out[g] = append(out[g], uv.Rect(x, r.Min.Y+1, b.width(), 1))
			x += b.width()
		}
	}
	return out
}

func (q QueryBar) chipRects(r uv.Rectangle) []uv.Rectangle { return rowRects(q.Chips, r, r.Min.Y+1) }

// rowRects is where chips go side by side on row y of a bar drawn in r, the last
// ones cut short where r ends.
func rowRects(chips []Chip, r uv.Rectangle, y int) []uv.Rectangle {
	var rects []uv.Rectangle
	x := r.Min.X + 1
	for _, c := range chips {
		w := Width(c.text())
		rects = append(rects, uv.Rect(x, y, min(w, max(r.Max.X-x, 0)), 1))
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
	bar := r // InputRect takes it whole, as it does everywhere else: it moves past the Info row itself
	if len(q.Info) > 0 {
		for i, cr := range rowRects(q.Info, r, r.Min.Y) {
			q.drawChip(f, q.Info[i], cr)
		}
		if r = q.below(r); r.Dy() < 1 {
			return cursor
		}
	}
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	f.Region(uv.Rect(r.Min.X, r.Min.Y, r.Dx(), 1), Target{Kind: KindHint, Pane: q.Pane, Action: "grid.where"}) // a click types in it (Q-01)
	f.Text(r.Min.X+1, r.Min.Y, r.Max.X-1, cmp.Or(q.Label, "WHERE"), uv.Style{Fg: th.Keyword, Bg: th.PaneBg, Attrs: uv.AttrBold})
	vx := r.Max.X - 2 // ▾: the history and favorites (Q-02), with its key before it when there is room (F8.14)
	vst, hx := dim, vx
	h := q.histHint(r)
	if h != "" {
		hx = vx - 1 - Width(h)
	}
	if f.Region(uv.Rect(hx, r.Min.Y, vx+1-hx, 1), Target{Kind: KindHint, Pane: q.Pane, Action: "where.history"}) {
		vst.Bg = th.Select
	}
	if h != "" {
		f.Text(hx, r.Min.Y, r.Max.X, h+" ", vst)
	}
	f.Text(vx, r.Min.Y, r.Max.X, "▾", vst)
	if c := q.Where.DrawSQL(f, q.InputRect(bar), bg, q.Dialect, q.Names); q.Typing {
		cursor = c
	}
	if r.Dy() < 2 {
		return cursor
	}
	y := r.Min.Y + 1
	q = q.fit(r)
	chips, groups, sel := q.shown(r)
	for i, cr := range q.chipRects(r)[:chips] {
		if c := q.drawChip(f, q.Chips[i], cr); c.X >= 0 {
			cursor = c
		}
	}
	rects := q.buttonRects(r, chips, groups)
	for k, i := range groups { // one sep strip, │ between groups, a button lit whole under the pointer (§7.8)
		if k > 0 {
			f.Text(rects[i][0].Min.X-1, y, r.Max.X-1, "│", uv.Style{Fg: th.Border, Bg: th.Sep})
		}
		for j, br := range rects[i] {
			b := q.Buttons[i].Buttons[j]
			st := uv.Style{Fg: cmp.Or[color.Color](b.Fg, th.Info), Bg: th.Sep}
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
	x := q.end(r, chips, groups)
	if sel {
		selected := dim
		selected.Fg = f.Theme.Warn // the color of the records' own (F12.12)
		x = f.Text(x, y, r.Max.X-1, q.Selected, selected) + 1
	}
	right, st := q.Right, dim
	if n := q.Note; n != (Note{}) {
		if right = n.Fit(r.Max.X - 2 - x); n.Fg != nil {
			st.Fg = n.Fg
		}
	}
	rx := r.Max.X - 1 - Width(right)
	if k := q.keep(); rx <= x && k != "" {
		right = k
		rx = r.Max.X - 1 - Width(right)
	}
	if rx > x {
		f.Text(rx, y, r.Max.X-1, right, st)
	}
	return cursor
}

// drawChip paints chip c in cr, and says where the cursor goes when its input
// has the keys, else (-1, -1).
func (q QueryBar) drawChip(f *Frame, c Chip, cr uv.Rectangle) uv.Position {
	th, y, cursor := f.Theme, cr.Min.Y, uv.Pos(-1, -1)
	chip := uv.Style{Fg: th.Fg, Bg: th.Sep}
	var icon uv.Rectangle // its own button: lit on its own, the rest of the chip without it (§7.8)
	if c.Icon.Text != "" {
		w := Width(c.Icon.Text) + 1 // and the space after it; where the chip's text puts it, so none when cut off
		icon = uv.Rect(cr.Min.X+Width(c.text())-w, y, w, 1).Intersect(cr)
	}
	if f.Region(cr, Target{Kind: KindHint, Pane: q.Pane, Action: c.Action}) && !f.Over(icon) {
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
	return cursor
}

// keep is the text of the second row that stays, none while a note stands.
func (q QueryBar) keep() string {
	if q.Note != (Note{}) {
		return ""
	}
	return q.Keep
}

// shown is how many chips show, which groups of buttons and whether the
// count selected does, left to right: all that fit, and a note, which goes
// before them: a whole group, the last chip or the count gives way, the least
// Stay first (§7.8「查询条」). The count in Right just is not shown.
func (q QueryBar) shown(r uv.Rectangle) (chips int, groups []int, sel bool) {
	chips, sel = len(q.Chips), q.Selected != ""
	for i := range q.Buttons {
		groups = append(groups, i)
	}
	note := Width(q.Note.Head + q.Note.Mid + q.Note.Tail)
	for chips+len(groups) > 0 || sel {
		need := note
		if sel {
			need += Width(q.Selected)
		}
		if k := q.keep(); k != "" {
			need += Width(k) + 1
		}
		if q.end(r, chips, groups)+need < r.Max.X-1 {
			break
		}
		least, group := math.MaxInt, -1
		for i, g := range groups {
			if s := q.Buttons[g].Stay; s < least {
				least, group = s, i
			}
		}
		chip := math.MaxInt // the last chip's
		if chips > 0 {
			chip = q.Chips[chips-1].Stay
		}
		switch {
		case sel && q.SelectedStay < min(least, chip):
			sel = false
		case chips > 0 && chip < least, group < 0:
			chips--
		default:
			groups = slices.Delete(groups, group, group+1)
		}
	}
	return chips, groups, sel
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
			x++ // the │ between groups
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
