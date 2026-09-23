package ui

import "github.com/charmbracelet/x/ansi"

// Width is the display width of s in cells.
func Width(s string) int { return ansi.StringWidth(s) }

// Truncate cuts s to at most w cells, ending in … when cut.
func Truncate(s string, w int) string { return ansi.Truncate(s, max(w, 0), "…") }

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
