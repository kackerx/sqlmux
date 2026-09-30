package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"
)

// consoleTheme gives every token the console draws a color of its own, so
// consoleShot can tell them apart (comment and dim, sql_string and focus
// share one in tokyonight-storm).
var consoleTheme = func() *Theme {
	th := *TokyonightStorm
	for i, p := range []*color.Color{&th.Fg, &th.Keyword, &th.Number, &th.SQLString, &th.Comment, &th.Func, &th.Dim, &th.Focus, &th.Error, &th.PaneBg, &th.Row, &th.Visual, &th.SQLTable, &th.SQLColumn, &th.SQLOperator} {
		*p = color.RGBA{uint8(i + 1), 0, 0, 0xff}
	}
	return &th
}()

// consoleShot draws c on a w × h frame, as colorShot shows it.
func consoleShot(c Console, w, h int) string {
	f := NewFrame(w, h, consoleTheme)
	return colorShot(f, c.Draw(f, uv.Rect(0, 0, w, h)))
}

// colorShot shows each row of f three times: as text, its foreground by
// token (k keyword, n number, s string, c comment, f func, t table, l
// column, o operator, . fg, d dim, > focus, e error) and its background
// (r row, v visual), ? for any other; @ marks the cursor at cur.
func colorShot(f *Frame, cur uv.Position) string {
	th, w, h := consoleTheme, f.Bounds().Dx(), f.Bounds().Dy()
	fgs := map[color.Color]string{th.Fg: ".", th.Keyword: "k", th.Number: "n", th.SQLString: "s", th.Comment: "c", th.Func: "f", th.Dim: "d", th.Focus: ">", th.Error: "e",
		th.SQLTable: "t", th.SQLColumn: "l", th.SQLOperator: "o"}
	bgs := map[color.Color]string{th.PaneBg: " ", th.Row: "r", th.Visual: "v"}
	var b strings.Builder
	for y := range h {
		var text, fg, bg strings.Builder
		for x := range w {
			cell := f.Buf.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			text.WriteString(cell.Content)
			for range cell.Width {
				fg.WriteString(cmp.Or(fgs[cell.Style.Fg], "?"))
				bg.WriteString(cmp.Or(bgs[cell.Style.Bg], "?"))
			}
		}
		mark := " "
		if cur.Y == y {
			mark = "@"
		}
		fmt.Fprintf(&b, "%s|%s|\n |%s|\n |%s|\n", mark, text.String(), fg.String(), bg.String())
	}
	fmt.Fprintf(&b, "cursor %d,%d\n", cur.X, cur.Y)
	return b.String()
}

const consoleSQL = `-- the users
select id, count(*) from t_user
where name = 'a' and n > 42
group by id;

select now();`

// sqlNames says the tables and columns are those it is given.
func sqlNames(table string, columns ...string) SQLNames {
	return func(_ int, word string) SQLName {
		switch {
		case word == table:
			return TableName
		case slices.Contains(columns, word):
			return ColumnName
		}
		return OtherName
	}
}

// The console's highlighting, gutter, statement range, selections and
// command line (§7.8, §9.2, §11).
func TestGoldenConsole(t *testing.T) {
	sql := strings.Split(consoleSQL, "\n")
	scrolled := []string{"中" + strings.Repeat("x", 30), "select 1"}
	for _, c := range []struct {
		name string
		c    Console
		w, h int
	}{
		{"normal", Console{Lines: sql, Cursor: TextPos{2, 1}, CursorCol: 1, Normal: true}, 40, 7},
		{"visual", Console{Lines: sql, Cursor: TextPos{2, 9}, CursorCol: 9, Sel: Sel{Mode: SelChars, From: TextPos{1, 7}, To: TextPos{2, 9}}}, 40, 7},
		{"vline", Console{Lines: sql, Cursor: TextPos{2, 0}, Sel: Sel{Mode: SelLines, From: TextPos{1, 0}, To: TextPos{2, 0}}}, 40, 7},
		{"vblock", Console{Lines: []string{"a\tbc", "abcdef", "ab"}, Cursor: TextPos{2, 2}, CursorCol: 2, Sel: Sel{Mode: SelBlock, From: TextPos{0, 1}, To: TextPos{2, 2}, Left: 1, Right: 2}}, 20, 3},
		{"vblock over part of a tab", Console{Lines: []string{"\tbc", "abcdef"}, Cursor: TextPos{1, 1}, CursorCol: 1, Sel: Sel{Mode: SelBlock, From: TextPos{0, 0}, To: TextPos{1, 1}, Left: 1, Right: 1}}, 20, 2},
		{"cmdline", Console{Lines: sql, Prompt: ":", Text: "%s/a/b", Pos: 6}, 40, 7},
		{"search", Console{Lines: sql, Prompt: "/", Text: "count", Pos: 5}, 40, 7},
		{"failed", Console{Lines: sql, Cursor: TextPos{5, 0}, Normal: true, Failed: 5}, 40, 7},
		{"scrolled", Console{Lines: scrolled, Left: 24, Cursor: TextPos{0, 32}, CursorCol: 31, Normal: true}, 20, 2},
		{"wide cut at the left", Console{Lines: []string{"a中" + strings.Repeat("x", 20)}, Left: 2, Cursor: TextPos{0, 17}, CursorCol: 16, Normal: true}, 20, 1},
		{"insert", Console{Lines: []string{"select 1;", ""}, Cursor: TextPos{1, 0}}, 20, 3},
		{"names", Console{Lines: []string{"select * from t_order where user_id = 'df'"}, Cursor: TextPos{0, 0}, Names: sqlNames("t_order", "user_id")}, 50, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.c.TabWidth = 2
			if c.c.Failed == 0 {
				c.c.Failed = -1
			}
			golden.RequireEqual(t, consoleShot(c.c, c.w, c.h))
		})
	}
}

// A table's name is a table's before ( too, a function's name else;
// columns by names (§7.3).
func TestSQLColorsNames(t *testing.T) {
	th := TokyonightStorm
	text := "references t_user(id), count(id)"
	names := func(_ int, w string) SQLName {
		return map[string]SQLName{"t_user": TableName, "id": ColumnName}[w]
	}
	cs := SQLColors(th, text, names)
	for w, want := range map[string]any{"t_user": th.SQLTable, "count": th.Func, "id": th.SQLColumn} {
		if got := cs[strings.Index(text, w)]; got != want {
			t.Errorf("%s: %v", w, got)
		}
	}
}
