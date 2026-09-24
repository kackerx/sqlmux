package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Backspace and the cursor go by grapheme cluster, as in nvim.
func TestInputGraphemes(t *testing.T) {
	for _, g := range []string{"é", "👍🏽", "👨‍👩‍👧"} {
		in := Input{}
		in.Insert("x" + g + "y")
		in.Left()
		in.Left()
		if in.Pos != 1 {
			t.Errorf("%q: two lefts from the end land at %d", g, in.Pos)
		}
		in.Right()
		in.Backspace()
		if in.Text != "xy" || in.Pos != 1 {
			t.Errorf("%q: backspace left %q at %d", g, in.Text, in.Pos)
		}
	}
	in := Input{}
	in.Backspace()
	in.Left()
	in.Right()
	if in != (Input{}) {
		t.Errorf("an empty input stays empty: %+v", in)
	}
}

func TestInputScrollsToCursor(t *testing.T) {
	in := Input{}
	in.Insert(strings.Repeat("a", 7) + "bcd")
	f := NewFrame(5, 1, TokyonightStorm)
	cur := in.Draw(f, uv.Rect(0, 0, 5, 1), uv.Style{})
	if f.String() != "abcd" || cur != uv.Pos(4, 0) {
		t.Errorf("at the end: %q, cursor %v", f.String(), cur)
	}
	in.Pos = 0
	f = NewFrame(5, 1, TokyonightStorm)
	if cur := in.Draw(f, uv.Rect(0, 0, 5, 1), uv.Style{}); f.String() != "aaaaa" || cur != uv.Pos(0, 0) {
		t.Errorf("at the start: %q, cursor %v", f.String(), cur)
	}
}
