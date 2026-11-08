package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// SessionRow is a row of the session list (§7.8「session 列表」): a
// session, or one of its windows a level in.
type SessionRow struct {
	Window  bool   // a window's row
	Name    string // its name; a window's "0: data"
	Note    string // in dim after it: "3 windows"
	Current bool   // the session attached, its window: in focus color
	Edit    *Input // the name typed in its place ($), nil for none
}

// SessionList is the session list: the palette's box (§12) with rows of
// sessions; Top is the first row showing.
type SessionList struct {
	Rows     []SessionRow
	Sel, Top int
}

// SessionBox is where the list of n rows sits, the palette's place and
// width, and how many rows show.
func SessionBox(screen uv.Rectangle, n int) (uv.Rectangle, int) {
	w := min(100, screen.Dx()-4)
	top := screen.Min.Y + screen.Dy()/6
	rows := max(min(n, paletteRows, screen.Max.Y-top-2), 0)
	return uv.Rect(screen.Min.X+(screen.Dx()-w)/2, top, w, rows+2), rows
}

// Draw dims what is behind (§7.5), paints the list over it and returns
// where a name being typed has its cursor, else (-1, -1).
func (l SessionList) Draw(f *Frame, screen uv.Rectangle) uv.Position {
	th := f.Theme
	box, rows := SessionBox(screen, len(l.Rows))
	cursor := uv.Pos(-1, -1)
	f.Dim()
	f.Region(f.Bounds(), Target{Kind: KindBackdrop}) // a click outside closes it
	if box.Dx() < 8 {
		return cursor
	}
	f.Region(box, Target{}) // inside, a click is not outside
	bg := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
	f.Fill(box, bg)
	border := f.Lines(true).Style(uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y, box.Max.X-1, " session ", uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg, Attrs: uv.AttrBold})
	x1 := box.Max.X - 2
	for i, y := l.Top, box.Min.Y+1; i < min(l.Top+rows, len(l.Rows)); i, y = i+1, y+1 {
		r, st := l.Rows[i], bg
		if i == l.Sel {
			st.Bg = th.Select
			f.Fill(uv.Rect(box.Min.X+1, y, box.Dx()-2, 1), st)
		}
		f.Region(uv.Rect(box.Min.X+1, y, box.Dx()-2, 1), Target{Kind: KindRow, I: i})
		if r.Current {
			st.Fg = th.Focus
		}
		x := box.Min.X + 2
		if r.Window {
			x += 2
		}
		if r.Edit != nil {
			cursor = r.Edit.Draw(f, uv.Rect(x, y, x1-x, 1), st)
			continue
		}
		x = f.Text(x, y, x1, r.Name, st)
		f.Text(x, y, x1, strings.TrimRight("  "+r.Note, " "), uv.Style{Fg: th.Dim, Bg: st.Bg})
	}
	return cursor
}
