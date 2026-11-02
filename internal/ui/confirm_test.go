package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// A line the screen cannot hold goes on the next, whole: the question
// is read to its end (§10.5).
func TestConfirmWraps(t *testing.T) {
	const text = "doraemon, play, far has an uncommitted transaction; quitting would roll it back."
	f := NewFrame(40, 12, TokyonightStorm)
	Confirm{Text: text, YesKey: "y", Yes: "Quit", NoKey: "n", No: "Cancel"}.Draw(f, uv.Rect(0, 0, 40, 12))
	var words []string
	for _, l := range strings.Split(f.String(), "\n") {
		words = append(words, strings.Fields(strings.Trim(strings.TrimSpace(l), "│┌┐└┘─ "))...)
	}
	if got := strings.Join(words, " "); !strings.Contains(got, strings.Join(strings.Fields(text), " ")) {
		t.Errorf("the box says %q", got)
	}
}

// An overlay's border is the theme's border_focus (F20.6).
func TestConfirmBorderFocus(t *testing.T) {
	th := *TokyonightStorm
	th.BorderFocus = c("#010203")
	f := NewFrame(30, 12, &th)
	Confirm{Text: "Quit?", YesKey: "y", Yes: "Quit", NoKey: "n", No: "Cancel"}.Draw(f, uv.Rect(0, 0, 30, 12))
	n := 0
	for y := range 12 {
		for x := range 30 {
			if cell := f.Buf.CellAt(x, y); cell.String() == "┌" {
				n++
				if cell.Style.Fg != th.BorderFocus {
					t.Errorf("the corner is in %v, want border_focus", cell.Style.Fg)
				}
			}
		}
	}
	if n != 1 {
		t.Errorf("%d corners", n)
	}
}

// A danger confirm has its title, the first line however it wraps, in the
// error color, and no other line (§13).
func TestConfirmDangerTitle(t *testing.T) {
	th := TokyonightStorm
	// the rows of the box with a cell in the error color, and what they say
	errRows := func(danger bool) (rows []string) {
		f := NewFrame(30, 12, th)
		Confirm{Text: "Drop table public.t_user, which cannot be undone:\n  DROP TABLE t", Danger: danger, YesKey: "y", Yes: "Drop", NoKey: "n", No: "Cancel"}.Draw(f, uv.Rect(0, 0, 30, 12))
		for y, line := range strings.Split(f.String(), "\n") {
			for x := range 30 {
				if c := f.Buf.CellAt(x, y); c.Style.Fg == th.Error && strings.TrimSpace(c.String()) != "" {
					rows = append(rows, strings.TrimSpace(strings.Trim(line, "│ ")))
					break
				}
			}
		}
		return rows
	}
	if got := errRows(true); len(got) != 2 || !strings.HasPrefix(got[0], "Drop table") || !strings.HasSuffix(got[1], "undone:") {
		t.Errorf("the title: %q", got)
	}
	if got := errRows(false); len(got) != 0 {
		t.Errorf("no danger: %q", got)
	}
}

// More lines than the screen has room for are cut with a … where the last would
// be, none drawn past the box (F17.10).
func TestConfirmCutsWhatDoesNotFit(t *testing.T) {
	var lines []string
	for i := range 30 {
		lines = append(lines, "statement "+strings.Repeat("x", i%3)+" "+string(rune('a'+i%26)))
	}
	f := NewFrame(60, 12, TokyonightStorm)
	Confirm{Text: strings.Join(lines, "\n"), YesKey: "y", Yes: "Save", NoKey: "n", No: "Cancel"}.Draw(f, uv.Rect(0, 0, 60, 12))
	out := f.String()
	if !strings.Contains(out, "…") || !strings.Contains(out, "statement") || strings.Contains(out, lines[29]) || !strings.Contains(out, "Save") {
		t.Errorf("the box:\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n != 12 {
		t.Errorf("%d rows", n)
	}
}
