package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Confirm asks before changes are thrown away (§10.5): lines of text and
// two buttons, in a box at the middle of screen; with No empty, the one, as
// a notice is read (§6.7). A click outside it says no.
type Confirm struct {
	Text        string // lines apart by \n
	Danger      bool   // the first line, the title, is in the error color (§13)
	YesKey, Yes string // "y", "退出": runs confirm.yes
	NoKey, No   string // "n", "取消": runs confirm.no
}

func (c Confirm) Draw(f *Frame, screen uv.Rectangle) {
	th := f.Theme
	f.Region(screen, Target{Kind: KindBackdrop})
	// a line the screen cannot hold goes on the next: a question that loses
	// what it asks about is read wrong
	var lines []string
	title := 0 // how many of them are the first line's
	for i, l := range strings.Split(c.Text, "\n") {
		lines = append(lines, strings.Split(ansi.Wrap(l, max(screen.Dx()-4, 1), ""), "\n")...)
		if i == 0 {
			title = len(lines)
		}
	}
	yes, no := " "+c.YesKey+" "+c.Yes+" ", ""
	buttons := Width(yes)
	if c.No != "" {
		no = " " + c.NoKey + " " + c.No + " "
		buttons += 1 + Width(no)
	}
	if room := screen.Dy() - 4; len(lines) > room && room > 1 { // what the box cannot hold is said so, not drawn past it
		lines = append(lines[:room-1], "…")
	}
	w := Width(lines[0])
	for _, l := range lines[1:] {
		w = max(w, Width(l))
	}
	w = min(max(w, buttons)+4, screen.Dx())
	h := min(len(lines)+4, screen.Dy())
	box := uv.Rect(screen.Min.X+(screen.Dx()-w)/2, screen.Min.Y+(screen.Dy()-h)/2, w, h)
	f.Region(box, Target{}) // a click inside is not outside
	f.Fill(box, uv.Style{Bg: th.PaneBg})
	border := f.Lines(true).Style(uv.Style{Fg: th.BorderFocus, Bg: th.PaneBg})
	border.Draw(f.Buf, box)
	for i, l := range lines {
		fg := th.Fg
		if c.Danger && i < title {
			fg = th.Error
		}
		f.Text(box.Min.X+2, box.Min.Y+1+i, box.Max.X-2, l, uv.Style{Fg: fg, Bg: th.PaneBg})
	}
	x, y := box.Max.X-2-buttons, box.Max.Y-2 // at the right, under a blank line
	for _, b := range []struct{ key, text, action string }{{c.YesKey, yes, "confirm.yes"}, {c.NoKey, no, "confirm.no"}} {
		if b.text == "" {
			continue
		}
		st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		if f.Region(uv.Rect(x, y, Width(b.text), 1), Target{Kind: KindButton, Action: b.action}) {
			st.Bg = th.Select
		}
		f.Text(x, y, box.Max.X-1, b.text, st)
		f.Text(x+1, y, box.Max.X-1, b.key, uv.Style{Fg: th.Warn, Bg: st.Bg, Attrs: uv.AttrBold})
		x += Width(b.text) + 1
	}
}
