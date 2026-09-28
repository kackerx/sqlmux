package ui

import "github.com/charmbracelet/x/ansi"

// Width is the display width of s in cells.
func Width(s string) int { return ansi.StringWidth(s) }

// Truncate cuts s to at most w cells, ending in … when cut.
func Truncate(s string, w int) string { return ansi.Truncate(s, max(w, 0), "…") }

// TruncateMatch is Truncate for s with match positions pos (rune offsets,
// as TextMatch takes them): a match cut off does not light the … up.
func TruncateMatch(s string, pos []int, w int) (string, []int) {
	t := Truncate(s, w)
	if t == s {
		return s, pos
	}
	kept := len([]rune(t)) - 1 // the runes before the …
	var in []int
	for _, p := range pos {
		if p < kept {
			in = append(in, p)
		}
	}
	return t, in
}

// DropLastGrapheme removes the last grapheme cluster, as backspace does in
// nvim: é typed as e + U+0301 or 👍🏽 goes whole, not a code point at a time.
func DropLastGrapheme(s string) string {
	last := 0
	for i := 0; i < len(s); {
		gr, _ := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		last, i = i, i+len(gr)
	}
	return s[:last]
}
