package ui

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/db"
)

// vals is rows of non-NULL values.
func vals(rows ...[]string) [][]db.Val {
	out := make([][]db.Val, len(rows))
	for i, r := range rows {
		for _, s := range r {
			out[i] = append(out[i], db.Val{S: s})
		}
	}
	return out
}

func testGrid() Grid {
	return Grid{
		Cols: []GridCol{{Name: "id", PK: true, Type: ColNumber}, {Name: "name"}, {Name: "note"}},
		Rows: vals(
			[]string{"1", "alpha", "short"},
			[]string{"22", "b", "a much longer note here"},
			[]string{"333", "gamma", "mid note"},
			[]string{"4", "d", ""},
		),
		Row: 2, Col: 1, Focused: true, Key: Icon{Text: "*"},
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
	g.Key.Fg = th.Error
	f = NewFrame(60, 7, th)
	g.Draw(f, uv.Rect(0, 0, 60, 7))
	if f.Buf.CellAt(nameX, 4).Style.Bg != th.CursorBlur {
		t.Error("an unfocused grid shows the blurred cursor")
	}
	head := strings.Split(f.String(), "\n")[0]
	keyX := len([]rune(head[:strings.Index(head, "* id")]))
	if f.Buf.CellAt(keyX, 0).Style.Fg != th.Error || f.Buf.CellAt(keyX+2, 0).Style.Fg != th.Func {
		t.Error("the key icon keeps its own color, the name stays func")
	}
}

func TestGridWidths(t *testing.T) {
	g := Grid{Cols: []GridCol{{Name: "a"}, {Name: "long header"}}}
	for i := range 10 {
		v := "xx"
		if i == 9 {
			v = strings.Repeat("x", 60) // one outlier: above the 90th percentile
		}
		g.Rows = append(g.Rows, vals([]string{v, strings.Repeat("y", 50)})...)
	}
	if got := g.view().widths(100); !reflect.DeepEqual(got, []int{2, 40}) {
		t.Errorf("roomy: %v, want p90 2 and the 40 cap", got)
	}
	if got := g.view().widths(20); got[0] < 1 || got[1] < len("long header") || got[1] > 40 {
		t.Errorf("squeezed: %v, never below the header", got)
	}
}

// Values take their column type's color (§7.6).
func TestGridColorsByType(t *testing.T) {
	th := TokyonightStorm
	g := Grid{
		Cols: []GridCol{{Name: "n", Type: ColNumber}, {Name: "s", Type: ColString}, {Name: "t", Type: ColTime}},
		Rows: vals([]string{"689", "goal", "2026-09-21 10:00:00"}), Row: -1, Col: -1,
	}
	f := NewFrame(60, 4, th)
	g.Draw(f, f.Bounds())
	row := strings.Split(f.String(), "\n")[2] // under the header and its rule
	for s, want := range map[string]color.Color{"689": th.Number, "goal": th.String, "2026-09-21 10:00:00": th.Time} {
		x := Width(row[:strings.Index(row, s)])
		if fg := f.Buf.CellAt(x, 2).Style.Fg; fg != want {
			t.Errorf("%q: %v, want %v", s, fg, want)
		}
	}
}

// Nothing a value holds reaches the terminal as a control (§7.6).
func TestCell(t *testing.T) {
	for v, want := range map[db.Val]string{
		{Null: true}:                       "<null>",
		{S: ""}:                            "",
		{S: "a\nb\tc\x1b[31md\x7f\u0085e"}: "a↵b c[31mde",
	} {
		if got := Cell(v); got != want {
			t.Errorf("Cell(%q) = %q, want %q", v.S, got, want)
		}
	}
}

// View scrolls just enough to show the cursor's column whole, and not at
// all while it is in view (§7.6).
func TestGridView(t *testing.T) {
	var g Grid // headers 10 wide, so squeezing stops short: the rest scrolls
	for _, h := range []string{"a", "b", "c", "d"} {
		g.Cols = append(g.Cols, GridCol{Name: strings.Repeat(h, 10)})
	}
	g.Rows = vals([]string{"1", "2", "3", "4"})
	area := uv.Rect(0, 0, 30, 5) // " 1 │ " then 13 a column: two show whole
	for _, c := range []struct{ col, left, want int }{{0, 0, 0}, {1, 0, 0}, {2, 0, 1}, {3, 0, 2}, {2, 2, 2}, {1, 2, 1}} {
		g.Col, g.Left = c.col, c.left
		if _, left := g.View(area); left != c.want {
			t.Errorf("cursor on %d from left %d: left %d, want %d", c.col, c.left, left, c.want)
		}
	}
}
