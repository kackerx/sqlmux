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
	f := NewFrame(50, 1, queryTheme)
	q.Draw(f, uv.Rect(0, 0, 50, 1))
	golden.RequireEqual(t, colorShot(f, uv.Pos(-1, -1)))
}

// The history's hint takes 6 columns of the bar and shows only while the
// WHERE input keeps 16 of the rest, minWhere (F8.14): at 32 columns it
// does, one less and it gives way, the input getting its columns back.
func TestInputRectKeepsMinWhere(t *testing.T) {
	q := QueryBar{History: Icon{Text: "H"}, HistKey: "C-r"}
	if got := q.InputRect(uv.Rect(0, 0, 32, 2)).Dx(); got != 16 {
		t.Errorf("32 wide: the input is %d", got)
	}
	if got := q.InputRect(uv.Rect(0, 0, 31, 2)).Dx(); got != 21 {
		t.Errorf("31 wide: the input is %d, the hint gone", got)
	}
}

// On a short second row the chips and the groups of buttons give way, the
// least Stay first: transpose, auto refresh, + −, COLS, PAGE, LIMIT, save
// and refresh, ORDER (§7.8, F8.9); the hit regions follow what is left,
// the buttons right after the last chip (F8.9).
func TestQueryBarYieldOrder(t *testing.T) {
	bar := func(note Note) QueryBar {
		btn := func(action, tail string) Button { return Button{Icon: Icon{Text: "x"}, Action: action, Tail: tail} }
		return QueryBar{
			Chips: []Chip{
				{Label: "ORDER", Value: "id", Action: "order", Stay: 7},
				{Label: "LIMIT", Value: "100", Action: "limit", Stay: 5},
				{Label: "PAGE", Value: "1/60", Action: "page", Stay: 4},
				{Label: "COLS", Value: "10/10", Action: "cols", Stay: 3},
			},
			Buttons: []ButtonGroup{
				{Buttons: []Button{btn("add", ""), btn("del", "")}, Stay: 2},
				{Buttons: []Button{btn("save", "3"), btn("refresh", "")}, Stay: 6},
				{Buttons: []Button{btn("auto", "5s"), btn("stop", "")}, Stay: 1},
				{Buttons: []Button{btn("transpose", "")}, Stay: 0},
			},
			Note: note,
		}
	}
	items := []string{"transpose", "auto", "add", "cols", "page", "limit", "save", "order"}
	for _, note := range []Note{{}, {Head: "已保存 1 行 · 3ms"}} {
		q := bar(note)
		gone := map[string]int{} // the width an item first is not there at, narrowing
		for w := 120; w >= 12; w-- {
			r := uv.Rect(0, 0, w, 2)
			chips, groups, _ := q.shown(r)
			shown := map[string]bool{"cols": chips > 3, "page": chips > 2, "limit": chips > 1, "order": chips > 0}
			for _, g := range groups {
				shown[[]string{"add", "save", "auto", "transpose"}[g]] = true
			}
			for _, it := range items {
				if _, ok := gone[it]; !shown[it] && !ok {
					gone[it] = w
				}
			}
			if shown["save"] && chips > 1 && !shown["limit"] {
				t.Fatalf("width %d: the save group is there and LIMIT not", w)
			}
			if !shown["save"] && chips > 1 {
				t.Fatalf("width %d: the save group gave way before the chips but ORDER (%d)", w, chips)
			}
		}
		for i := 1; i < len(items); i++ {
			if gone[items[i-1]] < gone[items[i]] || gone[items[i]] == 0 {
				t.Errorf("note %q: %s gives way at %d, %s at %d: not in the order %v", note.Head, items[i-1], gone[items[i-1]], items[i], gone[items[i]], items)
			}
		}
	}
	q := bar(Note{})
	for w := 70; w > 30; w-- { // the width where the three chips have gone and the save group is still there
		r := uv.Rect(0, 0, w, 2)
		if chips, groups, _ := q.shown(r); chips == 1 && len(groups) == 1 {
			save, order := q.ButtonRect(r, "save"), q.ChipRect(r, "order")
			if save.Empty() || order.Empty() || save.Min.X < order.Max.X || !q.ButtonRect(r, "add").Empty() {
				t.Errorf("width %d: save %v, order %v, add %v", w, save, order, q.ButtonRect(r, "add"))
			}
			return
		}
	}
	t.Error("no width has ORDER and the save group alone")
}

// A result under a page keeps its count: where "auto · 3 行 · 12ms" does not
// fit it is "3 行", and the chips and the buttons, ORDER last, give way for
// it; a note takes the place of both (F19.4).
func TestQueryBarKeepsTheCount(t *testing.T) {
	btn := func(action string) Button { return Button{Icon: Icon{Text: "x"}, Action: action} }
	bar := func(keep string, note Note) QueryBar {
		return QueryBar{
			Chips: []Chip{
				{Label: "ORDER", Value: "id", Action: "order", Stay: 7},
				{Label: "LIMIT", Value: "100", Action: "limit", Stay: 5},
				{Label: "PAGE", Value: "1/?", Action: "page", Stay: 4},
			},
			Buttons: []ButtonGroup{{Buttons: []Button{btn("add"), btn("del")}, Stay: 2}, {Buttons: []Button{btn("save")}, Stay: 6}},
			Right:   "auto · 3 行 · 12ms", Keep: keep, Note: note,
		}
	}
	row := func(q QueryBar, w int) string {
		f := NewFrame(w, 2, TokyonightStorm)
		q.Draw(f, uv.Rect(0, 0, w, 2))
		return strings.TrimRight(strings.Split(f.String(), "\n")[1], " ")
	}
	short, order := 0, 0
	for w := 120; w >= 14; w-- {
		r := row(bar("3 行", Note{}), w)
		full, kept, hasOrder := strings.HasSuffix(r, "auto · 3 行 · 12ms"), strings.HasSuffix(r, " 3 行"), strings.Contains(r, "ORDER")
		if hasOrder && !full && !kept {
			t.Fatalf("width %d: ORDER is there and the count not: %q", w, r)
		}
		if kept && !full && short == 0 {
			short = w
		}
		if !hasOrder && order == 0 {
			order = w
		}
	}
	if short == 0 || order == 0 || short < order {
		t.Errorf("the short count first shows at width %d, ORDER gives way at %d", short, order)
	}
	for w := 14; w < 60; w++ { // without a Keep the count just goes, as it did
		if r := row(bar("", Note{}), w); strings.HasSuffix(r, "3 行") && !strings.HasSuffix(r, "12ms") {
			t.Fatalf("width %d: a cut count with no Keep: %q", w, r)
		}
	}
	for w := 14; w < 120; w++ { // a note in their place: the same bar with a Keep or without
		n := Note{Head: "已保存 1 行"}
		if got, want := row(bar("3 行", n), w), row(bar("", n), w); got != want {
			t.Fatalf("width %d, a note and the count: %q, without it %q", w, got, want)
		}
	}
}

// With an Info row over it the WHERE is on the second row, its text and its
// cursor, where InputRect says; the chips are the third, untouched (F18.10).
func TestQueryBarInfoRowInput(t *testing.T) {
	r := uv.Rect(0, 0, 60, 3)
	q := QueryBar{
		Label: "MATCH", Where: Input{Text: "e*", Pos: 2}, Typing: true,
		Info:  []Chip{{Label: "type", Value: "hash"}},
		Chips: []Chip{{Label: "ORDER", Value: "none", Action: "grid.order"}},
	}
	f := NewFrame(60, 3, TokyonightStorm)
	cur := q.Draw(f, r)
	in, rows := q.InputRect(r), strings.Split(f.String(), "\n")
	if in.Min.Y != 1 || cur != uv.Pos(in.Min.X+2, 1) || !strings.Contains(rows[1], "MATCH e*") || !strings.Contains(rows[2], "ORDER none") || strings.Contains(rows[2], "e*") {
		t.Errorf("input %v, cursor %v:\n%s", in, cur, f.String())
	}
}
