package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// Every overlay's border is the theme's border_focus, and the title on it too
// (§7.3, F20.6).
func TestOverlaysBorderFocus(t *testing.T) {
	th := *TokyonightStorm
	th.BorderFocus = c("#010203")
	screen := uv.Rect(0, 0, 100, 30)
	for _, tc := range []struct {
		name, title string
		draw        func(f *Frame)
	}{
		{"sessions", "session", func(f *Frame) { SessionList{Rows: []SessionRow{{}}}.Draw(f, screen) }},
		{"connmanager", "Conn", func(f *Frame) { ConnManager{Title: "Conn", Rows: []ConnRow{{Name: "a"}}}.Draw(f, screen) }},
		{"palette", strings.TrimSpace(i18n.T(" 命令面板 ")), func(f *Frame) { Palette{}.Draw(f, screen) }},
		{"textbox", "Text", func(f *Frame) { TextBox{Title: "Text"}.Draw(f, uv.Rect(5, 5, 40, 10)) }},
		{"complete", "", func(f *Frame) { Complete{Items: []CompleteItem{{Text: "a"}}}.Draw(f, uv.Rect(5, 5, 20, 3), 1) }},
		{"timepick", "", func(f *Frame) {
			p := TimePick{Kind: Timestamp, Text: "2026-09-20 02:49:23", Sel: -1}
			w, h := p.Size()
			p.Draw(f, uv.Rect(5, 5, w, h))
		}},
		{"dropdown", "Drop", func(f *Frame) {
			Dropdown{Title: "Drop", Items: []string{"id"}, Pos: [][]int{nil}, Mark: -1}.Draw(f, uv.Rect(5, 5, 30, 6), 1)
		}},
	} {
		f := NewFrame(100, 30, &th)
		tc.draw(f)
		corners, titled := 0, tc.title == ""
		for y := range 30 {
			for x := range 100 {
				cell := f.Buf.CellAt(x, y)
				if cell.String() == "┌" {
					corners++
					if cell.Style.Fg != th.BorderFocus {
						t.Errorf("%s: the corner at (%d, %d) is in %v, want border_focus", tc.name, x, y, cell.Style.Fg)
					}
					if !titled { // the title on this top line, one cell for its first letter
						if at := strings.Index(rowText(f, y, x), tc.title); at >= 0 {
							titled = f.Buf.CellAt(x+at, y).Style.Fg == th.BorderFocus
						}
					}
				}
			}
		}
		if corners == 0 || !titled {
			t.Errorf("%s: %d corners, title in border_focus: %v", tc.name, corners, titled)
		}
	}
}

// rowText is row y of f from x on, a rune a cell (the wide ones' second cell is empty and skipped).
func rowText(f *Frame, y, x int) string {
	var b strings.Builder
	for ; x < f.Bounds().Dx(); x++ {
		b.WriteString(f.Buf.CellAt(x, y).String())
	}
	return b.String()
}
