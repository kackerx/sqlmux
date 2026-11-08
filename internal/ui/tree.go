package ui

import (
	"cmp"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// TreeNode is one row of the tree (§7.8): at Depth, with ▸ / ▾ before it
// when it has children, its icon (IconFg unless a theme colors it), the
// text, dim Aside after it, and Note on the right in NoteFg.
type TreeNode struct {
	Depth        int
	Branch, Open bool
	Prefix       string // dim, before the icon: a tab's pane number (F10.5)
	Icon         Icon
	IconFg       color.Color
	Text         string
	TextFg       color.Color // fg when nil: a failed connection's is error (§7.8)
	Pos          []int       // filter match positions in Text
	Aside        string
	Note         string
	NoteFg       color.Color
	Current      bool // the table ↵ lands on, the focused tab: in focus color
	// Sep is a rule across the tree, Text its title in dim, "" for none: it
	// is no node, the cursor never rests on it and a click is not on it (F10.6)
	Sep bool
}

// Tree is the ⟨0⟩ sidebar inside its border (§7.8): the filter row, the
// nodes, and the hint row.
type Tree struct {
	Nodes       []TreeNode // those showing, filter or not
	Total       int        // the tables and views of every schema
	Noun        string     // what Total counts: tables, or the keys of Redis (F16.2)
	Matches     int        // those the filter keeps
	Filter      Input
	Filtering   bool // the filter row is the input keys go to
	Cursor, Top int  // into Nodes
	Focused     bool
	Icons       *Icons
	Hints       []Hint
	Pane        int
	Empty       string // the one line of a tree with no connection, a click on it is the ＋ (F7.4)
}

// TreeRows is how many nodes a sidebar of inner height h lists.
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
	add := t.Icons.RowAdd // at the right end of the row: the form for a new connection (F7.4)
	addX := right - Width(add.Text)
	addSt := add.On(info)
	if f.Region(uv.Rect(addX, y, Width(add.Text), 1), Target{Kind: KindButton, Pane: t.Pane, Action: "conn.new"}) {
		addSt.Bg = th.Select
	}
	f.Text(addX, y, right, add.Text, addSt)
	hdr := addX - 1 // the header row ends before the button
	if t.Filtering || t.Filter.Text != "" {
		count := fmt.Sprintf("%d/%d", t.Matches, t.Total)
		cx := hdr - Width(count)
		f.Text(cx, y, hdr, count, dim)
		if c := t.Filter.Draw(f, uv.Rect(x, y, max(cx-1-x, 0), 1), uv.Style{Fg: th.Fg, Bg: th.PaneBg}); t.Filtering {
			cursor = c
		}
	} else {
		f.Text(x, y, hdr, fmt.Sprintf("%d %s", t.Total, cmp.Or(t.Noun, "tables")), dim)
	}

	sep := func(y int) {
		f.Text(in.Min.X, y, in.Max.X, strings.Repeat("─", in.Dx()), uv.Style{Fg: th.Sep, Bg: th.PaneBg})
	}
	sep(y + 1)
	list := uv.Rect(in.Min.X, y+2, in.Dx(), TreeRows(in.Dy()))
	if t.Empty != "" && list.Dy() > 0 {
		line := uv.Rect(list.Min.X, list.Min.Y, list.Dx(), 1)
		st := dim
		if f.Region(line, Target{Kind: KindButton, Pane: t.Pane, Action: "conn.new"}) {
			st.Bg = th.Row
		}
		f.Fill(line, uv.Style{Bg: st.Bg})
		f.Text(list.Min.X+1, line.Min.Y, in.Max.X-1, t.Empty, st)
	}
	for i := t.Top; i < min(t.Top+list.Dy(), len(t.Nodes)); i++ {
		n, row := t.Nodes[i], list.Min.Y+i-t.Top
		if n.Sep {
			drawRule(f, list.Min.X+1, row, right, n.Text)
			continue
		}
		line := uv.Rect(list.Min.X, row, list.Dx(), 1)
		bg := th.PaneBg
		switch hover := f.Region(line, Target{Kind: KindNode, Pane: t.Pane, I: i}); {
		case i == t.Cursor && t.Focused:
			bg = th.Select
		case i == t.Cursor || hover:
			bg = th.Row
		}
		f.Fill(line, uv.Style{Bg: bg})
		icon, text := uv.Style{Fg: n.IconFg, Bg: bg}, uv.Style{Fg: cmp.Or[color.Color](n.TextFg, th.Fg), Bg: bg}
		if n.IconFg == nil {
			icon.Fg = th.Fg
		}
		if n.Current {
			icon.Fg, text.Fg = th.Focus, th.Focus
		}
		x := list.Min.X + 1 + 2*n.Depth
		if n.Branch { // its own button: open or close, whatever the row does (§7.8)
			f.Region(uv.Rect(x, row, 1, 1), Target{Kind: KindFold, Pane: t.Pane, I: i})
			fold := "▸"
			if n.Open {
				fold = "▾"
			}
			f.Text(x, row, right, fold, uv.Style{Fg: th.Dim, Bg: bg})
		}
		x += 2
		if n.Prefix != "" {
			x = f.Text(x, row, right, n.Prefix+" ", uv.Style{Fg: th.Dim, Bg: bg})
		}
		if n.Icon.Text != "" {
			x = f.Text(x, row, right, n.Icon.Text, n.Icon.On(icon))
			x = f.Text(x, row, right, " ", icon) // a space after every icon (§7.7)
		}
		room := right - x
		if w := Width(n.Note); n.Note != "" && Width(n.Text)+1+w <= room { // the name goes first (§7.8)
			f.Text(right-w, row, right, n.Note, uv.Style{Fg: n.NoteFg, Bg: bg})
			room -= w + 1
		}
		name, pos := TruncateMatch(n.Text, n.Pos, room)
		x = f.TextMatch(x, row, x+room, name, pos, text)
		if n.Aside != "" && name == n.Text {
			f.Text(x, row, x+max(room-Width(name), 0), Truncate("  "+n.Aside, room-Width(name)), uv.Style{Fg: th.Dim, Bg: bg})
		}
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

// Size is n bytes as a size is read, by 1024s: 512 B, 12 KB, 3.4 MB, 1.2 GB,
// a tenth below ten of a unit; "" for 0, a size not known.
func Size(n int64) string {
	if n <= 0 {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, u := float64(n), 0
	for v >= 1024 && u < len(units)-1 {
		v, u = v/1024, u+1
	}
	if v = math.Round(v*10) / 10; u == 0 || v >= 10 { // to the nearest tenth, none from ten up
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64) + " " + units[u]
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + " " + units[u]
}

// drawRule is a rule across row y from x to right, with title in it at the
// start when there is one: a group's, in the tree and in the manager (F10.6).
func drawRule(f *Frame, x, y, right int, title string) {
	th := f.Theme
	rule := uv.Style{Fg: th.Sep, Bg: th.PaneBg}
	if title != "" {
		x = f.Text(x, y, right, "── ", rule)
		x = f.Text(x, y, right, title, uv.Style{Fg: th.Dim, Bg: th.PaneBg})
		x = f.Text(x, y, right, " ", rule)
	}
	f.Text(x, y, right, strings.Repeat("─", max(right-x, 0)), rule)
}
