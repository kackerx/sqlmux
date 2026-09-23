package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func seg(drop int, runs ...string) Segment {
	s := Segment{Drop: drop}
	for _, r := range runs {
		s.Runs = append(s.Runs, Run{Text: r})
	}
	return s
}

// §7.8's order: info, then the connection, then other windows right to left,
// then the cursor position, and finally the session name is cut.
func TestStatusLineNarrowing(t *testing.T) {
	s := StatusLine{
		Left:  []Segment{seg(0, " ", "doraemon", " ▾ "), seg(0, " 0: a* "), seg(2, " 1: b "), seg(2, " 2: c ")},
		Info:  "some extra info",
		Right: []Segment{seg(0, " C-p "), seg(0, " · "), seg(3, " 1,1 "), seg(1, " pg@host "), seg(0, " NORMAL ")},
	}
	// widths: left 12+7+6+6 = 31, right 5+3+5+9+8 = 30
	for _, c := range []struct {
		w                 int
		left, info, right string
	}{
		{80, " doraemon ▾  0: a*  1: b  2: c ", " some extra info ", " C-p  ·  1,1  pg@host  NORMAL "},
		{73, " doraemon ▾  0: a*  1: b  2: c ", " some extr… ", " C-p  ·  1,1  pg@host  NORMAL "},
		{72, " doraemon ▾  0: a*  1: b  2: c ", "", " C-p  ·  1,1  pg@host  NORMAL "}, // 11 spare: no info
		{60, " doraemon ▾  0: a*  1: b  2: c ", "", " C-p  ·  1,1  NORMAL "},          // the connection goes
		{46, " doraemon ▾  0: a*  1: b ", "", " C-p  ·  1,1  NORMAL "},                // then windows, right to left
		{40, " doraemon ▾  0: a* ", "", " C-p  ·  1,1  NORMAL "},
		{35, " doraemon ▾  0: a* ", "", " C-p  ·  NORMAL "}, // then the cursor
		{31, " dor… ▾  0: a* ", "", " C-p  ·  NORMAL "},     // then the session name is cut
	} {
		f := NewFrame(c.w, 1, TokyonightStorm)
		s.Draw(f, uv.Rect(0, 0, c.w, 1))
		row := f.String()
		row += strings.Repeat(" ", c.w-Width(row)) // String trims trailing blanks
		gap := c.w - Width(c.left+c.info+c.right)
		if gap < 0 {
			t.Fatalf("w=%d: bad case, parts are wider than the bar", c.w)
		}
		if want := c.left + strings.Repeat(" ", gap) + c.info + c.right; row != want {
			t.Errorf("w=%d:\n got %q\nwant %q", c.w, row, want)
		}
	}
	if s.Left[2].Runs[0].Text != " 1: b " || s.Left[0].Runs[1].Text != "doraemon" {
		t.Error("Draw must not change the caller's segments")
	}
}
