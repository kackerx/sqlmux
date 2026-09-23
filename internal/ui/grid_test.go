package ui

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func testGrid() Grid {
	return Grid{
		Cols: []GridCol{{Name: "id", PK: true, Numeric: true}, {Name: "name"}, {Name: "note"}},
		Rows: [][]string{
			{"1", "alpha", "short"},
			{"22", "b", "a much longer note here"},
			{"333", "gamma", "mid note"},
			{"4", "d", ""},
		},
		Row: 2, Col: 1, Focused: true, Key: "*",
	}
}

// sepCols is where │ or ┼ sit on a row.
func sepCols(row string) []int {
	var out []int
	for i, r := range []rune(row) {
		if r == '│' || r == '┼' {
			out = append(out, i)
		}
	}
	return out
}

func TestGridLines(t *testing.T) {
	for _, w := range []int{60, 30, 18} { // roomy, squeezed, too narrow for the headers
		f := NewFrame(w, 7, TokyonightStorm)
		testGrid().Draw(f, uv.Rect(0, 0, w, 7))
		rows := strings.Split(f.String(), "\n")
		head := sepCols(rows[0])
		if len(head) != 3 {
			t.Fatalf("w=%d: header separators %v in %q", w, head, rows[0])
		}
		if !strings.HasPrefix(rows[1], "──") || strings.Count(rows[1], "┼") != 3 {
			t.Errorf("w=%d: rule %q", w, rows[1])
		}
		for y := 1; y < 6; y++ {
			if got := sepCols(rows[y]); !reflect.DeepEqual(got, head) {
				t.Errorf("w=%d row %d: separators %v, header %v:\n%s", w, y, got, head, f.String())
			}
		}
		for _, r := range rows {
			if Width(r) > w {
				t.Errorf("w=%d: row overflows: %q", w, r)
			}
		}
	}
}

func TestGridContent(t *testing.T) {
	f := NewFrame(60, 7, TokyonightStorm)
	testGrid().Draw(f, uv.Rect(0, 0, 60, 7))
	rows := strings.Split(f.String(), "\n")
	if !strings.Contains(rows[0], "* id") || !strings.Contains(rows[0], "name") {
		t.Errorf("header %q", rows[0])
	}
	if !strings.Contains(rows[2], "│    1 │") || !strings.Contains(rows[4], "│  333 │") {
		t.Errorf("numbers right-aligned: %q / %q", rows[2], rows[4])
	}
}

func TestGridColors(t *testing.T) {
	th := TokyonightStorm
	f := NewFrame(60, 7, th)
	g := testGrid()
	g.Draw(f, uv.Rect(0, 0, 60, 7))
	bg := func(x, y int) any { return f.Buf.CellAt(x, y).Style.Bg }
	// data rows start at y=2; row index 1 (row number 2) is even: zebra
	if bg(0, 2) != th.PaneBg || bg(0, 3) != th.RowAlt || bg(0, 5) != th.RowAlt {
		t.Errorf("zebra: %v %v %v", bg(0, 2), bg(0, 3), bg(0, 5))
	}
	if bg(0, 4) != th.Row {
		t.Errorf("current row: %v", bg(0, 4))
	}
	nameX := strings.Index(strings.Split(f.String(), "\n")[4], "gamma")
	if bg(nameX, 4) != th.Cursor {
		t.Errorf("current cell: %v", bg(nameX, 4))
	}
	g.Focused = false
	f = NewFrame(60, 7, th)
	g.Draw(f, uv.Rect(0, 0, 60, 7))
	if f.Buf.CellAt(nameX, 4).Style.Bg != th.CursorBlur {
		t.Error("an unfocused grid shows the blurred cursor")
	}
}

func TestGridWidths(t *testing.T) {
	g := Grid{Cols: []GridCol{{Name: "a"}, {Name: "long header"}}}
	for i := range 10 {
		v := "xx"
		if i == 9 {
			v = strings.Repeat("x", 60) // one outlier: above the 90th percentile
		}
		g.Rows = append(g.Rows, []string{v, strings.Repeat("y", 50)})
	}
	if got := g.widths(100); !reflect.DeepEqual(got, []int{2, 40}) {
		t.Errorf("roomy: %v, want p90 2 and the 40 cap", got)
	}
	if got := g.widths(20); got[0] < 1 || got[1] < len("long header") || got[1] > 40 {
		t.Errorf("squeezed: %v, never below the header", got)
	}
}
