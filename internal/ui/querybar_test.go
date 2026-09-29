package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"
)

// ORDER's direction button sits where the chip's text puts the icon: a
// chip cut off at the bar's edge has none, rather than one over its name.
func TestQueryBarIconCutOff(t *testing.T) {
	q := QueryBar{Chips: []Chip{{Label: "ORDER", Value: "occurred_at,id", Action: "grid.order", Icon: Icon{Text: "^"}, IconAction: "grid.order.toggle"}}}
	for w, want := range map[int]bool{16: false, 40: true} {
		f := NewFrame(w, 2, TokyonightStorm)
		q.Draw(f, uv.Rect(0, 0, w, 2))
		got := false
		for _, h := range f.Hits {
			if h.Target.Action == "grid.order.toggle" {
				got = true
				if cell := f.Buf.CellAt(h.Rect.Min.X, 1); cell == nil || cell.Content != "^" {
					t.Errorf("width %d: the button is not on the icon: %v", w, h.Rect)
				}
			}
		}
		if got != want {
			t.Errorf("width %d: a direction button %v, want %v", w, got, want)
		}
	}
}

// A save's note that doesn't fit is cut in its middle, the database's
// error, its row and its tail whole (§10.3).
func TestQueryBarNoteCut(t *testing.T) {
	n := Note{Head: "id = 12：", Mid: strings.Repeat("x", 80), Tail: "，已回滚"}
	f := NewFrame(60, 2, TokyonightStorm)
	QueryBar{Note: n}.Draw(f, uv.Rect(0, 0, 60, 2))
	row := strings.Split(f.String(), "\n")[1]
	if !strings.Contains(row, "id = 12：xx") || !strings.Contains(row, "x…，已回滚") {
		t.Errorf("cut: %q", row)
	}
	n.Mid = "short"
	f = NewFrame(60, 2, TokyonightStorm)
	QueryBar{Note: n}.Draw(f, uv.Rect(0, 0, 60, 2))
	if row := strings.Split(f.String(), "\n")[1]; !strings.HasSuffix(strings.TrimRight(row, " "), "id = 12：short，已回滚") {
		t.Errorf("whole: %q", row)
	}
}

// The WHERE in SQL's colors, its table's columns among them, focused or
// not (§7.3).
func TestGoldenWhereColors(t *testing.T) {
	q := QueryBar{Where: Input{Text: "user_id = '2' and status = 'running'"}, Names: sqlNames("t_order", "user_id", "status")}
	f := NewFrame(50, 1, consoleTheme)
	q.Draw(f, uv.Rect(0, 0, 50, 1))
	golden.RequireEqual(t, colorShot(f, uv.Pos(-1, -1)))
}
