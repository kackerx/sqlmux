package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestText(t *testing.T) {
	for _, c := range []struct {
		name         string
		x, right     int
		s            string
		wantRow      string
		wantReturned int
	}{
		{"fits", 1, 10, "ab", " ab", 3},
		{"clipped at right", 0, 3, "abcdef", "abc", 3},
		{"wide char straddling right is dropped", 0, 3, "a中文", "a中", 3},
		{"wide char exactly fits", 0, 5, "a中文", "a中文", 5},
		{"negative x clips the left", -2, 10, "abcd", "cd", 2},
		{"right beyond the frame", 6, 99, "abcdef", "      ab", 8},
		{"a combining mark stays in its cluster", 0, 10, "e\u0301x", "e\u0301x", 2},
	} {
		f := NewFrame(8, 1, TokyonightStorm)
		got := f.Text(c.x, 0, c.right, c.s, uv.Style{})
		row := strings.TrimRight(f.String(), " ")
		if row != c.wantRow || got != c.wantReturned {
			t.Errorf("%s: row %q returned %d; want %q %d", c.name, row, got, c.wantRow, c.wantReturned)
		}
	}
}
