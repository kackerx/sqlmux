package ui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// A password is drawn as a * for each character the user sees, a wide one,
// an emoji and a letter with its accent each one, and the cursor goes
// where the text's does (§7.8).
func TestFormMask(t *testing.T) {
	text := "密码😀é" // four graphemes, 13 bytes
	in := Input{Text: text, Pos: len(text)}
	fm := Form{Save: "s", Cancel: "c", Sel: 0, Fields: []FormField{{Label: "password", Mask: true, Input: in}}}
	draw := func(in Input) (stars int, cursor uv.Position, row string) {
		fm.Fields[0].Input = in
		f := NewFrame(80, 12, TokyonightStorm)
		cursor = ConnManager{Title: "t", Rows: []ConnRow{{Name: "a"}}, Form: fm}.Draw(f, f.Bounds())
		for _, l := range strings.Split(f.String(), "\n") {
			if n := strings.Count(l, "*"); n > 0 {
				return n, cursor, l
			}
		}
		return 0, cursor, ""
	}
	n, end, row := draw(in)
	if n != 4 || strings.Contains(row, "码") || strings.Contains(row, "😀") {
		t.Fatalf("%d stars in %q", n, row)
	}
	in.Left()
	in.Left()
	if _, at, _ := draw(in); at.X != end.X-2 || at.Y != end.Y {
		t.Errorf("two ← from the end %v: cursor %v", end, at)
	}
}

// The list takes the form's height: with more connections than rows it shows
// a window of them that has the selection in it, whatever Top says (F8.16).
func TestConnManagerScrolls(t *testing.T) {
	var rows []ConnRow
	for i := range 40 {
		rows = append(rows, ConnRow{Name: fmt.Sprintf("conn_%02d", i)})
	}
	m := ConnManager{Title: "t", Rows: rows, Form: Form{Save: "s", Cancel: "c", Fields: make([]FormField, 3)}}
	for _, c := range []struct {
		sel, top      int
		shows, hidden string
	}{{0, 0, "conn_00", "conn_08"}, {30, 0, "conn_30", "conn_00"}, {5, 30, "conn_05", "conn_30"}, {39, 39, "conn_39", "conn_00"}} {
		m.Sel, m.Top = c.sel, c.top
		f := NewFrame(80, 20, TokyonightStorm)
		m.Draw(f, f.Bounds())
		if out := f.String(); !strings.Contains(out, c.shows) || strings.Contains(out, c.hidden) {
			t.Errorf("sel %d top %d: shows %s %v, hides %s %v", c.sel, c.top, c.shows, strings.Contains(out, c.shows), c.hidden, !strings.Contains(out, c.hidden))
		}
	}
	if m.ListRows(80) != 3+5-2 {
		t.Errorf("rows %d", m.ListRows(80))
	}
}

// The row above the buttons is a test's result in the ok color when it
// connected, in error otherwise; an input with nothing in it shows its
// example, dim (F12.5).
func TestFormResultAndExample(t *testing.T) {
	fgOf := func(fm Form, mark string) color.Color {
		f := NewFrame(80, 14, TokyonightStorm)
		ConnManager{Title: "t", Rows: []ConnRow{{Name: "a"}}, Form: fm}.Draw(f, f.Bounds())
		for y := range 14 {
			for x := range 80 {
				if c := f.Buf.CellAt(x, y); c != nil && c.Content == mark {
					return c.Style.Fg
				}
			}
		}
		t.Fatalf("no %s drawn:\n%s", mark, f.String())
		return nil
	}
	fm := Form{Save: "s", Test: "t", Cancel: "c", Err: "ZZ", Fields: []FormField{{Label: "dsn", Example: "QQ://x"}}}
	if got := fgOf(fm, "Z"); got != TokyonightStorm.Error {
		t.Errorf("an error: %v", got)
	}
	fm.OK = true
	if got := fgOf(fm, "Z"); got != TokyonightStorm.Focus {
		t.Errorf("a connection: %v", got)
	}
	if got := fgOf(fm, "Q"); got != TokyonightStorm.Dim {
		t.Errorf("the example: %v", got)
	}
}

// An error too long for a row wraps to the form's width, every word of it
// showing and no …; the box is as many rows higher as the lines are more, and
// in a window too low for all of them it is the window's high with the lines
// that fit (F18.13).
func TestFormErrorWraps(t *testing.T) {
	why := "unable to open tcp connection with host 'localhost:1433': dial tcp [::1]:1433: connectex: No connection could be made because the target machine actively refused it"
	fm := Form{Save: "s", Test: "t", Cancel: "c", Err: why, Fields: []FormField{{Label: "dsn"}}}
	m := ConnManager{Title: "t", Rows: []ConnRow{{Name: "a"}}, Form: fm}
	_, fw := managerWidth(100)
	lines := fm.errLines(fw)
	if len(lines) != 3 || fm.heightIn(fw) != 1+4+len(lines) {
		t.Fatalf("%d lines, height %d", len(lines), fm.heightIn(fw))
	}
	draw := func(h int) string {
		f := NewFrame(100, h, TokyonightStorm)
		m.Draw(f, f.Bounds())
		return f.String()
	}
	out := draw(20)
	if strings.Contains(out, "…") {
		t.Errorf("truncated:\n%s", out)
	}
	for _, w := range strings.Fields(why) {
		if !strings.Contains(out, w) {
			t.Fatalf("%q is not drawn:\n%s", w, out)
		}
	}
	// three lines of error, a field, the rule, the buttons and the border do not fit in 7 rows: the first lines show, the buttons too
	out = draw(7)
	if !strings.Contains(out, "unable") || !strings.Contains(out, "[ s ]") || strings.Contains(out, "refused") {
		t.Errorf("in a low window:\n%s", out)
	}
}
