package ui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Every row's location starts in one column: as wide as the widest name, at
// most 40% of the box, longer names cut with … (§12).
func TestPaletteColumns(t *testing.T) {
	long := strings.Repeat("x", 60)
	p := Palette{Search: Icon{Text: "~"}, Rows: []PaletteRow{
		{Icon: Icon{Text: "+"}, Name: "t_user", Where: "w-one", Tag: "表"},
		{Icon: Icon{Text: "[]"}, Name: "0: data", Where: "w-two", Tag: "窗口"},
		{Icon: Icon{Text: ":"}, Name: long, Where: "w-three", Tag: "命令", Pos: []int{59}},
	}}
	f := NewFrame(160, 45, TokyonightStorm)
	box, _, _ := PaletteBox(uv.Rect(0, 0, 160, 44), len(p.Rows), false)
	p.Draw(f, uv.Rect(0, 0, 160, 44))
	lines := strings.Split(f.String(), "\n")
	var starts []int
	for _, w := range []string{"w-one", "w-two", "w-three"} {
		for _, l := range lines {
			if i := strings.Index(l, w); i >= 0 {
				starts = append(starts, Width(l[:i]))
			}
		}
	}
	// border, padding, the widest icon ("[]") and a space, 40 name columns, two spaces
	want := box.Min.X + 2 + 2 + 1 + 40 + 2
	if len(starts) != 3 || starts[0] != want || starts[1] != want || starts[2] != want {
		t.Errorf("location columns %v, want all at %d", starts, want)
	}
	for y, l := range lines {
		if !strings.Contains(l, "w-three") {
			continue
		}
		if !strings.Contains(l, strings.Repeat("x", 39)+"… ") {
			t.Errorf("the long name is cut to 40 with …: %q", l)
		}
		if f.Buf.CellAt(want-3, y).Style.Fg == TokyonightStorm.Match {
			t.Error("a match cut off by the … must not light the … up")
		}
	}
}

// With a result the box reaches a row above the status bar and the table
// keeps 8 rows, the list giving up rows first (§12).
func TestPaletteBoxResult(t *testing.T) {
	for _, c := range []struct{ h, n, rows, gridH int }{
		{44, 20, 12, 15}, // tall: 12 listed, the table the rest
		{30, 20, 7, 8},   // shorter: the list shrinks to keep the table at 8
		{20, 20, 0, 8},   // short: no list at all
		{44, 0, 0, 28},   // nothing listed: no rule under the list either
	} {
		screen := uv.Rect(0, 0, 160, c.h)
		box, rows, grid := PaletteBox(screen, c.n, true)
		if box.Max.Y != c.h-1 || rows != c.rows || grid.Dy() != c.gridH || grid.Max.Y != box.Max.Y-3 {
			t.Errorf("h %d n %d: box %v rows %d grid %v", c.h, c.n, box, rows, grid)
		}
	}
}

// A list that doesn't all fit below its input opens above it when there is
// more room there (§10.2); else below, as much as fits.
func TestCompleteBoxFlips(t *testing.T) {
	screen := uv.Rect(0, 0, 80, 24)
	if box, rows := CompleteBox(screen, uv.Pos(10, 5), 20, 5); box.Min.Y != 6 || rows != 5 {
		t.Errorf("room below: %v %d", box, rows)
	}
	if box, rows := CompleteBox(screen, uv.Pos(10, 20), 20, 5); box.Max.Y != 20 || rows != 5 {
		t.Errorf("near the bottom: %v %d", box, rows)
	}
	if box, rows := CompleteBox(uv.Rect(0, 0, 80, 6), uv.Pos(10, 2), 20, 5); box.Min.Y != 3 || rows != 1 {
		t.Errorf("short either way, more below: %v %d", box, rows)
	}
}

// The pick is on select, the row under the pointer on row: which one ↵
// takes shows apart from where the pointer is (§10.2).
func TestCompleteHover(t *testing.T) {
	c := Complete{Items: []CompleteItem{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	f := NewFrame(20, 5, TokyonightStorm)
	f.Mouse = uv.Pos(3, 3) // over c
	c.Draw(f, uv.Rect(0, 0, 20, 5), 3)
	if pick, hover := f.Buf.CellAt(2, 1).Style.Bg, f.Buf.CellAt(2, 3).Style.Bg; pick != TokyonightStorm.Select || hover != TokyonightStorm.Row {
		t.Errorf("picked %v, hovered %v", pick, hover)
	}
}
