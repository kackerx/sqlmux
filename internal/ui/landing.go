package ui

import uv "github.com/charmbracelet/ultraviolet"

// Landing is what a pane with no tab and a new tab show (§5「引导页」):
// buttons in the middle, one a row, each " <icon> label key" with the key
// in dim; a click runs its action. A row that does not fit is left out.
type Landing struct {
	Buttons []LandingButton
	Pane    int
}

type LandingButton struct {
	Icon               Icon
	Label, Key, Action string
}

func (b LandingButton) text() string {
	s := " " + b.Icon.Text + " " + b.Label + " "
	if b.Key != "" {
		s += b.Key + " "
	}
	return s
}

func (l Landing) Draw(f *Frame, r uv.Rectangle) {
	th := f.Theme
	w := 0
	for _, b := range l.Buttons {
		w = max(w, Width(b.text()))
	}
	x, y := r.Min.X+max(r.Dx()-w, 0)/2, r.Min.Y+(r.Dy()-len(l.Buttons))/2
	for i, b := range l.Buttons {
		row := y + i
		if row < r.Min.Y || row >= r.Max.Y || Width(b.text()) > r.Dx() {
			continue
		}
		st := uv.Style{Fg: th.Fg, Bg: th.PaneBg}
		if f.Region(uv.Rect(x, row, Width(b.text()), 1), Target{Kind: KindHint, Pane: l.Pane, Action: b.Action}) {
			st.Bg = th.Select
		}
		at := f.Text(x, row, r.Max.X, " ", st)
		at = f.Text(at, row, r.Max.X, b.Icon.Text, b.Icon.On(st))
		at = f.Text(at, row, r.Max.X, " "+b.Label+" ", st)
		if b.Key != "" {
			dim := st
			dim.Fg = th.Dim
			f.Text(at, row, r.Max.X, b.Key+" ", dim)
		}
	}
}
