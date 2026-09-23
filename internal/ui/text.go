package ui

import "github.com/charmbracelet/x/ansi"

// Width is the display width of s in cells.
func Width(s string) int { return ansi.StringWidth(s) }
