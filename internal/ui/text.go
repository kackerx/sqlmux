package ui

import "github.com/charmbracelet/x/ansi"

// Width is the display width of s in cells.
func Width(s string) int { return ansi.StringWidth(s) }

// Truncate cuts s to at most w cells, ending in … when cut.
func Truncate(s string, w int) string { return ansi.Truncate(s, max(w, 0), "…") }
