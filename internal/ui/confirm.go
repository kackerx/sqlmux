package ui

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// Confirm asks before changes are thrown away (§10.5): a line and two
// buttons, in a box at the middle of screen. A click outside it says no.
type Confirm struct {
	Text        string
	YesKey, Yes string // "y", "退出": runs confirm.yes
	NoKey, No   string // "n", "取消": runs confirm.no
}

func (c Confirm) Draw(f *Frame, screen uv.Rectangle) {
	th := f.Theme
	f.Region(screen, Target{Kind: KindBackdrop})
	yes, no := " "+c.YesKey+" "+c.Yes+" ", " "+c.NoKey+" "+c.No+" "
	w := min(max(Width(c.Text), Width(yes)+1+Width(no))+4, screen.Dx())
	box := uv.Rect(screen.Min.X+(screen.Dx()-w)/2, screen.Min.Y+(screen.Dy()-5)/2, w, 5)
	f.Region(box, Target{}) // a click inside is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := uv.NormalBorder().Style(uv.Style{Fg: th.Focus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	f.Text(box.Min.X+2, box.Min.Y+1, box.Max.X-2, c.Text, uv.Style{Fg: th.Fg, Bg: th.PaneBg})
	x, y := box.Max.X-2-Width(yes)-1-Width(no), box.Min.Y+3 // at the right, under a blank line
	for _, b := range []struct{ key, text, action string }{{c.YesKey, yes, "confirm.yes"}, {c.NoKey, no, "confirm.no"}} {
		st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		if f.Region(uv.Rect(x, y, Width(b.text), 1), Target{Kind: KindButton, Action: b.action}) {
			st.Bg = th.Select
		}
		f.Text(x, y, box.Max.X-1, b.text, st)
		f.Text(x+1, y, box.Max.X-1, b.key, uv.Style{Fg: th.Warn, Bg: st.Bg, Attrs: uv.AttrBold})
		x += Width(b.text) + 1
	}
}
