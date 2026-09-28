package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/golden"

	"sqlmux/internal/db"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// editsOf is tab's changes as "row/col=value", DEFAULT as <default>.
func editsOf(tab *dataTab) string {
	var out []string
	for k, e := range tab.edits {
		v := e.val.S
		switch {
		case e.def:
			v = "<default>"
		case e.val.Null:
			v = "<null>"
		}
		out = append(out, k.row+"/"+k.col+"="+v)
	}
	return strings.Join(out, " ")
}

// i edits the current cell, its text all selected: typing replaces it, esc
// and ↵ keep it as the cell's change, typing the original back undoes it
// (§10.1). The save button counts the changed cells (Q-05).
func TestEditCell(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "li")
	if tab.cell == nil || a.mode() != keymap.Insert || a.context().Focus[0] != "cell" || !tab.cell.in.All || tab.cell.in.Text != "running" {
		t.Fatalf("i on status: %+v, mode %v", tab.cell, a.mode())
	}
	if info := a.statusLine().Info; info != "-- editing status --" {
		t.Errorf("status info %q", info)
	}
	feed(t, a, "done<Esc>")
	if tab.cell != nil || a.mode() != keymap.Normal || editsOf(tab) != "1/status=done" {
		t.Fatalf("esc keeps it: %q", editsOf(tab))
	}
	g := a.grid(a.focused(), tab)
	if !g.Edited[[2]int{0, 1}] || g.Rows[0][1].S != "done" {
		t.Errorf("the grid shows the change: %v %v", g.Edited, g.Rows[0][1])
	}
	if b := a.queryBar(a.focused(), tab).Buttons[0]; b.Action != "save" || b.Count != 1 {
		t.Errorf("save button %+v", b)
	}
	feed(t, a, "<CR>")
	if tab.cell.in.Text != "done" || !tab.cell.in.All {
		t.Fatalf("again: starts from the change, %+v", tab.cell.in)
	}
	feed(t, a, "<Right><BS><BS><BS><BS>running<CR>")
	if len(tab.edits) != 0 {
		t.Errorf("back to what was loaded: %q", editsOf(tab))
	}
}

// Left as it started, an edit changes nothing: a NULL stays NULL, not ”
// (§10.1).
func TestEditUntouched(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "jj$h") // row 3's note: NULL
	if v := tab.page.Rows[tab.row][tab.fieldAt(tab.col)]; !v.Null {
		t.Fatalf("not on the NULL: %+v", v)
	}
	feed(t, a, "i<Esc>ix<BS><Esc>")
	if len(tab.edits) != 0 {
		t.Errorf("untouched: %q", editsOf(tab))
	}
	feed(t, a, "i<BS>x<CR>")
	if editsOf(tab) != "3/note=x" {
		t.Errorf("typed: %q", editsOf(tab))
	}
}

// Changes stay through other pages and queries: a row coming back shows
// its change again (§10.1).
func TestEditsOutlivePages(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "lix<Esc>")
	_, cols, page := ordersTable(3)
	other := db.Result{Cols: page.Cols, Rows: page.Rows[1:]}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: other})
	if g := a.grid(a.focused(), tab); len(g.Edited) != 0 || a.queryBar(a.focused(), tab).Buttons[0].Count != 1 {
		t.Fatalf("row 1 is not on this page: %v", g.Edited)
	}
	a.Update(pageMsg{tab: tab, seq: tab.seq, cols: cols, page: page})
	if g := a.grid(a.focused(), tab); !g.Edited[[2]int{0, 1}] || g.Rows[0][1].S != "x" {
		t.Errorf("back again: %v", g.Edited)
	}
}

// A table with no row identity can't be saved to: no edit, a toast says
// why. A unique index over not-null columns will do (§10.1).
func TestEditNeedsRowIdentity(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	tab.cols.PK = nil
	a.Update(teaKey("i")) // not fed: its toast's Cmd waits out the toast
	if tab.cell != nil || !strings.Contains(a.toast, "t_order 没有主键") {
		t.Fatalf("no key: cell %+v, toast %q", tab.cell, a.toast)
	}
	tab.cols.Unique = [][]string{{"note", "id"}}
	if feed(t, a, "i"); tab.cell == nil || tab.cell.key.row != "note 1\x001" {
		t.Errorf("a unique index: %+v", tab.cell)
	}
}

// A paste goes into the input that has the keys, newlines as spaces; on a
// grid in NORMAL it starts editing the cell with it (§10.1).
func TestPaste(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	a.Update(tea.PasteMsg{Content: "a\nb"})
	if tab.cell == nil || tab.cell.in.Text != "a b" || tab.cell.in.All {
		t.Fatalf("paste in NORMAL: %+v", tab.cell)
	}
	a.Update(tea.PasteMsg{Content: "<Esc>c"})
	if tab.cell.in.Text != "a b<Esc>c" {
		t.Fatalf("paste while editing: %q", tab.cell.in.Text)
	}
	feed(t, a, "<Esc>/")
	a.Update(tea.PasteMsg{Content: "id > 1"})
	if tab.where.Text != "id > 1" {
		t.Errorf("paste in the WHERE: %q", tab.where.Text)
	}
}

// Anything else ends the edit first, keeping it: a click elsewhere (a cell
// then just moves the cursor), the wheel, a global key; C-c is esc (§10.1).
// A double click edits; a click in the input changes nothing.
func TestEditEndsFirst(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	p := a.focused()
	feed(t, a, "lix")
	c := find(t, a, ui.Target{Kind: ui.KindCell, Pane: p.ID, Action: "grid.goto 2 3"})
	click(a, c.Min)
	if tab.cell != nil || tab.row != 2 || tab.col != 3 || editsOf(tab) != "1/status=x" {
		t.Fatalf("click on a cell: cell %+v at %d,%d, %q", tab.cell, tab.row, tab.col, editsOf(tab))
	}
	feed(t, a, "iy")
	a.Update(tea.MouseWheelMsg{X: c.Min.X, Y: c.Min.Y, Button: tea.MouseWheelDown})
	if tab.cell != nil || len(tab.edits) != 2 {
		t.Fatalf("wheel: %q", editsOf(tab))
	}
	feed(t, a, "hiz<C-s>") // C-p is the cell's: its options (§10.2)
	if tab.cell != nil || len(tab.edits) != 3 {
		t.Fatalf("C-s: %q", editsOf(tab))
	}
	feed(t, a, "i<C-c>")
	if tab.cell != nil || a.toast != "" {
		t.Errorf("C-c: cell %+v, toast %q", tab.cell, a.toast)
	}
	e := find(t, a, ui.Target{Kind: ui.KindCell, Pane: p.ID, Action: "grid.goto 0 1"})
	click(a, e.Min)
	click(a, e.Min)
	if tab.cell == nil || tab.cell.key.col != "status" {
		t.Errorf("a double click edits: %+v", tab.cell)
	}
	click(a, e.Min) // inside the input: stays
	if tab.cell == nil {
		t.Error("a click in the input ended the edit")
	}
}

// The input sits on the cell in the cursor's color, all selected, and
// widens past the cell for a longer text; changed cells read warn on
// edited_bg, underlined with dots (§7.6, §10.1).
func TestEditLooks(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 3)
	feed(t, a, "li")
	f := a.render()
	c := find(t, a, ui.Target{Kind: ui.KindCell, Pane: a.focused().ID, Action: "grid.goto 0 1"})
	if st := f.Buf.CellAt(c.Min.X+1, c.Min.Y).Style; st.Bg != a.theme.Select {
		t.Errorf("selected: %v", st.Bg)
	}
	if f.Cursor == nil || f.Cursor.Y != c.Min.Y {
		t.Errorf("cursor %v", f.Cursor)
	}
	feed(t, a, strings.Repeat("w", 30))
	f = a.render()
	row := strings.Split(f.String(), "\n")[c.Min.Y]
	if !strings.Contains(row, strings.Repeat("w", 30)) {
		t.Errorf("wider than the cell: %q", row)
	}
	feed(t, a, "<Esc>j")
	f = a.render()
	st := f.Buf.CellAt(c.Min.X+1, c.Min.Y).Style
	if st.Fg != a.theme.Warn || st.Bg != a.theme.EditedBg || st.Underline != uv.UnderlineDotted {
		t.Errorf("changed: %+v", st)
	}
	_ = tab
}

// Editing a cell with more text than it holds: the input runs on over the
// cells to its right (§10.1).
func TestGoldenCellEdit160x45(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 60)
	feed(t, a, "jlli"+"a longer amount than the column holds")
	golden.RequireEqual(t, a.render().String())
}

// The three kinds of change as the grid shows them: text, NULL, DEFAULT
// (§7.6, §10.2).
func TestGoldenEdits160x45(t *testing.T) {
	a := sized(160, 45, "nerd")
	tab := loadOrders(t, a, 60)
	tab.edits = map[editKey]edit{
		{"1", "status"}: {val: db.Val{S: "done"}},
		{"2", "note"}:   {val: db.Val{Null: true}},
		{"3", "amount"}: {def: true},
	}
	golden.RequireEqual(t, a.render().String())
}
