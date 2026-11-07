package ui

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// FormKind is what a form row holds.
type FormKind int

const (
	FormInput    FormKind = iota // a line of text
	FormSegments                 // one of Choices, all shown
	FormCheck                    // [x] or [ ]
	FormFold                     // a row that opens and closes the rows after it
)

// FormField is a row of a form (§7.8「新建连接表单」): an icon, a name and
// what it holds. On is the segment, 1 for checked, or 1 for open.
type FormField struct {
	Label   string
	Icon    Icon
	Kind    FormKind
	Input   Input
	Choices []string
	On      int
	Mask    bool // the input is drawn as * (a password)
	Dim     bool // not taking part: drawn dim
	Bad     bool // its name in the error color: why ↵ did not save
	Hidden  bool // not drawn
	// Example is drawn dim in an input with nothing in it (F12.5)
	Example string
}

// Form is a box of fields over the screen: Sel is the field with the keys,
// Err why it did not save, in error above the buttons, or, with OK, what a
// test found or that one is going, in the ok color.
type Form struct {
	Fields             []FormField
	Sel                int
	Err                string
	OK                 bool
	Save, Test, Cancel string // the buttons' words
	Delete             string // a third button's word, when a connection there already is can go (F8.16); "" for none
	Hints              []Hint
	// Pick is a list opened under the input of field PickAt, drawn over what
	// is below it (the connection form's groups, F10.6); nil for none
	Pick   *Complete
	PickAt int
}

// buttons are the buttons' words and what they run.
func (fm Form) buttons() []struct{ text, action string } {
	bs := []struct{ text, action string }{{fm.Save, "form.save"}, {fm.Test, "form.test"}, {fm.Cancel, "form.cancel"}}
	if fm.Delete != "" {
		bs = append(bs, struct{ text, action string }{fm.Delete, "conns.delete"})
	}
	return bs
}

// keysBelow is whether, in a form width wide, the keys do not fit beside the
// buttons and go on a row of their own under them (§7.8, F8.16).
func (fm Form) keysBelow(width int) bool {
	used := 0
	for _, b := range fm.buttons() {
		used += Width(b.text) + 5 // [ text ] and a space
	}
	return len(fm.Hints) > 0 && width-2-used < hintRowWidth(fm.Hints)
}

// errLines is Err wrapped to the inside of a form width wide, a line at
// least: a driver's error, a dial's, is longer than one (F18.13).
func (fm Form) errLines(width int) []string {
	var lines []string
	for _, l := range strings.Split(fm.Err, "\n") {
		lines = append(lines, strings.Split(ansi.Wrap(l, max(width-2, 1), ""), "\n")...)
	}
	return lines
}

// heightIn is the form's rows, its border in: fields, the error's lines,
// rule, buttons, and the keys' row when they go under the buttons, in a form
// width wide.
func (fm Form) heightIn(width int) int {
	n := fm.rows() + 4 + len(fm.errLines(width))
	if fm.keysBelow(width) {
		n++
	}
	return n
}

// rows is how many fields show.
func (fm Form) rows() int {
	n := 0
	for _, fd := range fm.Fields {
		if !fd.Hidden {
			n++
		}
	}
	return n
}

// drawIn paints the fields, the error, the rule and the buttons inside in,
// the border's inner area, the field Sel having the keys when focus; it
// returns where its input has the cursor, else (-1, -1).
// segmentsWidth is how wide choices are written, padding and separators aside.
func segmentsWidth(choices []string) (w int) {
	for _, ch := range choices {
		w += Width(ch)
	}
	return w
}

func (fm Form) drawIn(f *Frame, area uv.Rectangle, focus bool) uv.Position {
	th := f.Theme
	cursor := uv.Pos(-1, -1)
	bg, dim := uv.Style{Fg: th.Fg, Bg: th.PaneBg}, uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	x0, x1 := area.Min.X+1, area.Max.X-1
	iconW, labelW := 0, 0
	for _, fd := range fm.Fields {
		iconW, labelW = max(iconW, Width(fd.Icon.Text)), max(labelW, Width(fd.Label)) // the hidden ones too: nothing shifts as the fold opens
	}
	y := area.Min.Y
	var pickAt uv.Position
	for i, fd := range fm.Fields {
		if fd.Hidden {
			continue
		}
		f.Region(uv.Rect(area.Min.X, y, area.Dx(), 1), Target{Kind: KindRow, I: i})
		sel := focus && i == fm.Sel
		name, icon := dim, dim
		if sel {
			name, icon = bg, bg
		}
		if fd.Bad {
			name.Fg = th.Error
		}
		f.Text(x0, y, x1, fd.Icon.Text, fd.Icon.On(icon))
		f.Text(x0+iconW+1+labelW-Width(fd.Label), y, x1, fd.Label, name)
		r := uv.Rect(x0+iconW+labelW+2, y, max(x1-x0-iconW-labelW-2, 0), 1)
		in := uv.Style{Fg: th.Fg, Bg: th.Sep}
		if sel {
			in.Bg = th.Select
		}
		if fd.Dim {
			in.Fg = th.Dim
		}
		if i == fm.PickAt {
			pickAt = uv.Pos(r.Min.X, y)
		}
		switch fd.Kind {
		case FormInput:
			f.Fill(r, in)
			if fd.Mask {
				fd.Input = fd.Input.masked()
			}
			if c := fd.Input.Draw(f, r, in); sel {
				cursor = c
			}
			if fd.Input.Text == "" && fd.Example != "" {
				in.Fg = th.Dim
				f.Text(r.Min.X, y, r.Max.X, fd.Example, in)
			}
		case FormSegments:
			x, pad := r.Min.X, " "
			if segmentsWidth(fd.Choices)+3*len(fd.Choices)-1 > r.Dx() { // padded, they would not fit: the engines' seven
				pad = ""
			}
			for j, ch := range fd.Choices {
				st := uv.Style{Fg: th.Dim, Bg: th.Sep}
				if j == fd.On {
					st = uv.Style{Fg: th.Fg, Bg: th.Select}
				}
				if j > 0 {
					x = f.Text(x, y, r.Max.X, "│", dim)
				}
				f.Region(uv.Rect(x, y, Width(ch)+2*len(pad), 1), Target{Kind: KindSegment, Pane: i, I: j})
				x = f.Text(x, y, r.Max.X, pad+ch+pad, st)
			}
		case FormCheck:
			mark := "[ ]"
			if fd.On == 1 {
				mark = "[x]"
			}
			f.Text(r.Min.X, y, r.Max.X, mark, in)
		case FormFold:
			mark := "▸"
			if fd.On == 1 {
				mark = "▾"
			}
			f.Text(r.Min.X, y, r.Max.X, mark, name)
		}
		y++
	}
	note := uv.Style{Fg: th.Error, Bg: th.PaneBg}
	if fm.OK {
		note.Fg = th.Focus // the theme has no ok token: the green of what is on
	}
	lines := fm.errLines(area.Dx())
	below := 0
	if fm.keysBelow(area.Dx()) {
		below = 1
	}
	n := max(min(len(lines), area.Dy()-fm.rows()-2-below), 1) // a window too low shows the lines that fit
	for i, l := range lines[:n] {
		f.Text(x0, y+i, x1, l, note)
	}
	y += n - 1
	f.Text(area.Min.X, y+1, area.Max.X, strings.Repeat("─", area.Dx()), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	x, y := x0, y+2
	for _, b := range fm.buttons() {
		st := bg
		if f.Region(uv.Rect(x, y, Width(b.text)+4, 1), Target{Kind: KindButton, Action: b.action}) {
			st.Bg = th.Select
		}
		edge := st
		edge.Fg = th.Focus
		x = f.Text(x, y, x1, "[", edge)
		x = f.Text(x, y, x1, " "+b.text+" ", st)
		x = f.Text(x, y, x1, "]", edge) + 1
	}
	if hx := x1 - hintRowWidth(fm.Hints); fm.keysBelow(area.Dx()) {
		hintRow(f, max(hx, x0), y+1, x1, fm.Hints, dim, Target{Kind: KindButton})
	} else {
		hintRow(f, hx, y, x1, fm.Hints, dim, Target{Kind: KindButton})
	}
	if fm.Pick != nil && len(fm.Pick.Items) > 0 {
		w := 20
		for _, it := range fm.Pick.Items {
			w = max(w, Width(it.Text)+4)
		}
		box, rows := CompleteBox(f.Bounds(), pickAt, min(w, area.Max.X-pickAt.X), len(fm.Pick.Items), 0, CompleteRows)
		_, fm.Pick.Top = ListView(fm.Pick.Sel, fm.Pick.Top, len(fm.Pick.Items), rows) // the app keeps it for the next frame
		fm.Pick.Draw(f, box, rows)
	}
	return cursor
}

// masked is in with a * for each of its graphemes, the cursor after as many
// as it was.
func (in Input) masked() Input {
	count := func(s string) (n int) {
		for ; s != ""; n++ {
			gr, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
			s = s[len(gr):]
		}
		return n
	}
	return Input{Text: strings.Repeat("*", count(in.Text)), Pos: count(in.Text[:in.Pos])}
}
