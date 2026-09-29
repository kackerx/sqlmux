package editor

import (
	"math"
	"strings"
	"testing"
)

// Text objects and the commands V-BLOCK has not do nothing there (§11).
func TestBlockLeftOut(t *testing.T) {
	for _, ks := range []string{"<C-v>iw", "<C-v>ap", "<C-v>i(", "<C-v>jJ", "<C-v>js"} {
		e := New("abc def\nghi jkl")
		for _, k := range keys(t, ks) {
			e.Feed(k)
		}
		if got := strings.Join(e.Lines(), "\n"); got != "abc def\nghi jkl" || e.Mode() != VisualBlock {
			t.Errorf("%s: %q in %v", ks, got, e.Mode())
		}
	}
}

// Block is what the console draws a VISUAL BLOCK from.
func TestBlock(t *testing.T) {
	for _, c := range []struct {
		keys                  string
		top, bot, left, right int
		ok                    bool
	}{
		{"l<C-v>jl", 0, 1, 1, 2, true},
		{"l<C-v>j$", 0, 1, 1, math.MaxInt, true},
		{"jl<C-v>k", 0, 1, 2, 2, true}, // the tab is two columns
		{"vj", 0, 0, 0, 0, false},
	} {
		e := New("abcd\n\tefgh")
		for _, k := range keys(t, c.keys) {
			e.Feed(k)
		}
		top, bot, left, right, ok := e.Block()
		if top != c.top || bot != c.bot || left != c.left || right != c.right || ok != c.ok {
			t.Errorf("%s: %d %d %d %d %v, want %d %d %d %d %v", c.keys, top, bot, left, right, ok, c.top, c.bot, c.left, c.right, c.ok)
		}
	}
}
