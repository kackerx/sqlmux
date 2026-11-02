package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// ConnRow is a connection of the manager's list (§7.8「连接管理」): its
// engine's icon, its name, a lock when it is read only; the last row is the
// one that makes a new one.
type ConnRow struct {
	Icon Icon
	Name string
	Lock Icon // Text "" for none
	// Rule is a line drawn above the row, Group the dim title on it, "" for
	// none: where a group begins (F10.6)
	Rule  bool
	Group string
}

// ConnManager is the connections as a list on the left and the form of the
// one selected on the right, in a box of its own (F8.16).
type ConnManager struct {
	Title     string
	Rows      []ConnRow
	Sel, Top  int  // Top is the first row showing
	ListFocus bool // the list has the keys, else the form's selected field
	Form      Form
}

const (
	connManagerWidth = 100
	connListWidth    = 26
)

// managerWidth is the manager's on a screen width wide, and the form's in it: the
// box less its border, the list and the rule between.
func managerWidth(screen int) (box, form int) {
	box = min(connManagerWidth, screen-4)
	return box, box - 2 - connListWidth - 1
}

// ListRows is how many lines of the list show on a screen width wide: the
// form's height, which does not grow with the connections (F8.16).
func (m ConnManager) ListRows(screen int) int {
	_, fw := managerWidth(screen)
	return m.Form.heightIn(fw) - 2
}

// lines is how many lines rows i to j-1 take: one each, and one more for a
// rule above a row.
func (m ConnManager) lines(i, j int) (n int) {
	for _, r := range m.Rows[i:j] {
		n++
		if r.Rule {
			n++
		}
	}
	return n
}

// top is the first row the list shows in rows lines: Top, or a later one
// when the selection would be out of sight.
func (m ConnManager) top(rows int) int {
	top := max(min(m.Top, m.Sel), 0)
	for top < m.Sel && m.lines(top, m.Sel+1) > rows {
		top++
	}
	return top
}

// ScrollTop is the first row to show on a screen width wide, the selection
// in sight, as Draw does.
func (m ConnManager) ScrollTop(screen int) int { return m.top(m.ListRows(screen)) }

// Draw dims what is behind, paints the manager centered and returns where
// the form's input has the cursor, else (-1, -1).
func (m ConnManager) Draw(f *Frame, screen uv.Rectangle) uv.Position {
	th := f.Theme
	w, fw := managerWidth(screen.Dx())
	h := m.Form.heightIn(fw)
	if h > screen.Dy() && h-len(m.Form.errLines(fw))+1 <= screen.Dy() { // only the error's lines do not fit: the box is the window's
		h = screen.Dy()
	}
	box := uv.Rect(screen.Min.X+(screen.Dx()-w)/2, screen.Min.Y+max(screen.Dy()-h, 0)/2, w, h)
	f.Dim()
	f.Region(f.Bounds(), Target{}) // no click goes past it
	if box.Dx() < connListWidth+10 || box.Dy() > screen.Dy() {
		return uv.Pos(-1, -1)
	}
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	f.Fill(box, bg)
	border := f.Border.Style(uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y, box.Max.X-1, " "+m.Title+" ", uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg, Attrs: uv.AttrBold})
	in := uv.Rect(box.Min.X+1, box.Min.Y+1, box.Dx()-2, box.Dy()-2)
	rule := in.Min.X + connListWidth
	top := m.top(in.Dy()) // the selection in sight, whatever Top says
	y := in.Min.Y
	for i := top; i < len(m.Rows) && y < in.Max.Y; i++ {
		r := m.Rows[i]
		if r.Rule {
			drawRule(f, in.Min.X+1, y, in.Min.X+connListWidth-1, r.Group)
			if y++; y >= in.Max.Y {
				break
			}
		}
		line := uv.Rect(in.Min.X, y, connListWidth, 1)
		y++
		st := bg
		switch hover := f.Region(line, Target{Kind: KindConn, I: i}); {
		case i == m.Sel && m.ListFocus:
			st.Bg = th.Select
		case i == m.Sel:
			st.Bg = th.Row
		case hover:
			st.Bg = th.Row
		}
		f.Fill(line, st)
		x := line.Min.X + 1
		if r.Icon.Text != "" {
			x = f.Text(x, line.Min.Y, line.Max.X-1, r.Icon.Text+" ", r.Icon.On(st))
		}
		f.Text(x, line.Min.Y, line.Max.X-1-Width(r.Lock.Text)-1, r.Name, st)
		if r.Lock.Text != "" {
			f.Text(line.Max.X-1-Width(r.Lock.Text), line.Min.Y, line.Max.X, r.Lock.Text, r.Lock.On(uv.Style{Fg: th.Dim, Bg: st.Bg}))
		}
	}
	for y := in.Min.Y; y < in.Max.Y; y++ {
		f.Text(rule, y, rule+1, "│", uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	return m.Form.drawIn(f, uv.Rect(rule+1, in.Min.Y, in.Max.X-rule-1, in.Dy()), !m.ListFocus)
}
