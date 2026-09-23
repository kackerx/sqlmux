package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme holds the semantic color tokens (tech-design §7.3).
type Theme struct {
	Bg, PaneBg          color.Color
	Fg, FgMuted, Dim    color.Color
	Border, Focus, Warn color.Color
	Cursor, CursorBlur  color.Color
	Select, Row, RowAlt color.Color // RowAlt: the grid's zebra rows (§7.6)
	EditedBg            color.Color
	Keyword, Info       color.Color
	Error               color.Color
	Number, PK, Func    color.Color
	Sep                 color.Color // separators and tab dividers (§7.8)
}

var TokyonightStorm = &Theme{
	Bg: c("#1f2335"), PaneBg: c("#24283b"),
	Fg: c("#c0caf5"), FgMuted: c("#a9b1d6"), Dim: c("#565f89"),
	Border: c("#3b4261"), Focus: c("#9ece6a"), Warn: c("#e0af68"),
	Cursor: c("#3d59a1"), CursorBlur: c("#2f3549"),
	Select: c("#364a82"), Row: c("#292e42"), RowAlt: c("#1f2335"),
	EditedBg: c("#2d2a24"),
	Keyword:  c("#bb9af7"), Info: c("#7dcfff"),
	Error:  c("#f7768e"),
	Number: c("#ff9e64"), PK: c("#73daca"), Func: c("#7aa2f7"),
	Sep: c("#2f3549"),
}

func c(s string) color.Color { return lipgloss.Color(s) }
