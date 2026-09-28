package ui

import (
	"fmt"
	"strconv"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// TreeItem is one table in the schema tree.
type TreeItem struct {
	Name string
	Rows float64 // estimated row count; < 0 when unknown
	Pos  []int   // filter match positions in Name
	Open bool    // the table ↵ would land on (§7.8)
}

// Tree is the ⟨0⟩ sidebar inside its border (§7.8): the filter row, the
// tables, and the hint row.
type Tree struct {
	Items       []TreeItem // what passes the filter
	Total       int        // the schema's tables, filter or not
	Filter      Input
	Filtering   bool // the filter row is the input keys go to
	Cursor, Top int  // into Items
	Focused     bool
	Icons       *Icons
	Hints       []Hint
	Pane        int
}

// TreeRows is how many tables a sidebar of inner height h lists.
func TreeRows(h int) int { return max(h-4, 0) }

// Draw paints the tree over in and returns where the filter's cursor goes,
// (-1, -1) unless it is being typed into.
func (t Tree) Draw(f *Frame, in uv.Rectangle) uv.Position {
	th := f.Theme
	cursor := uv.Pos(-1, -1)
	if in.Dy() < 1 {
		return cursor
	}
	x, y, right := in.Min.X+1, in.Min.Y, in.Max.X-1
	info := uv.Style{Fg: th.Info, Bg: th.PaneBg}
	dim := uv.Style{Fg: th.Dim, Bg: th.PaneBg}
	x = f.Text(x, y, right, t.Icons.Filter.Text, t.Icons.Filter.On(info))
	x = f.Text(x, y, right, " ", info)
	if t.Icons.Labeled {
		x = f.Text(x, y, right, "/ ", uv.Style{Fg: th.Fg, Bg: th.PaneBg})
	}
	if t.Filtering || t.Filter.Text != "" {
		count := fmt.Sprintf("%d/%d", len(t.Items), t.Total)
		cx := right - Width(count)
		f.Text(cx, y, right, count, dim)
		if c := t.Filter.Draw(f, uv.Rect(x, y, max(cx-1-x, 0), 1), uv.Style{Fg: th.Fg, Bg: th.PaneBg}); t.Filtering {
			cursor = c
		}
	} else {
		f.Text(x, y, right, fmt.Sprintf("%d tables", t.Total), dim)
	}

	sep := func(y int) {
		f.Text(in.Min.X, y, in.Max.X, strings.Repeat("─", in.Dx()), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	sep(y + 1)
	list := uv.Rect(in.Min.X, y+2, in.Dx(), TreeRows(in.Dy()))
	for i := t.Top; i < min(t.Top+list.Dy(), len(t.Items)); i++ {
		it, row := t.Items[i], list.Min.Y+i-t.Top
		line := uv.Rect(list.Min.X, row, list.Dx(), 1)
		bg := th.PaneBg
		switch hover := f.Region(line, Target{Kind: KindTable, Pane: t.Pane, I: i}); {
		case i == t.Cursor && t.Focused:
			bg = th.Select
		case i == t.Cursor || hover:
			bg = th.Row
		}
		f.Fill(line, uv.Style{Bg: bg})
		icon, name := uv.Style{Fg: th.Func, Bg: bg}, uv.Style{Fg: th.Fg, Bg: bg}
		if it.Open {
			icon.Fg, name.Fg = th.Focus, th.Focus
		}
		rows := Magnitude(it.Rows)
		cx := right - Width(rows)
		x := f.Text(list.Min.X+1, row, right, t.Icons.Table.Text, t.Icons.Table.On(icon))
		x = f.Text(x, row, right, " ", icon)
		// cut with …, as titles and the palette are: mv_order_by_stat would read as another table (§7.8)
		n, pos := TruncateMatch(it.Name, it.Pos, cx-1-x)
		f.TextMatch(x, row, cx-1, n, pos, name)
		f.Text(cx, row, right, rows, uv.Style{Fg: th.Border, Bg: bg})
	}
	if in.Dy() >= 4 {
		sep(in.Max.Y - 2)
		// A hint that doesn't fit whole is left out; the next may still fit.
		var hs []Hint
		for _, h := range t.Hints {
			if in.Min.X+1+hintRowWidth(append(hs, h)) <= right {
				hs = append(hs, h)
			}
		}
		hintRow(f, in.Min.X+1, in.Max.Y-1, right, hs, dim, Target{Kind: KindHint, Pane: t.Pane})
	}
	return cursor
}

// Magnitude is a row count as the tree shows it (§7.8): exact below 1000,
// then floored to k, m or b, with a decimal while one digit leads ("8.1k",
// "2.1m") and none after ("48k"); "?" when unknown.
func Magnitude(n float64) string {
	if n < 0 {
		return "?"
	}
	v := int64(n)
	for _, u := range []struct {
		div    int64
		suffix string
	}{{1e9, "b"}, {1e6, "m"}, {1e3, "k"}} {
		switch {
		case v >= 10*u.div:
			return strconv.FormatInt(v/u.div, 10) + u.suffix
		case v >= u.div:
			tenths := v * 10 / u.div
			return fmt.Sprintf("%d.%d%s", tenths/10, tenths%10, u.suffix)
		}
	}
	return strconv.FormatInt(v, 10)
}
