package app

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// clipped reports whether cmd, or one it batches, puts want on the
// clipboard.
func clipped(cmd tea.Cmd, want string) bool {
	if cmd == nil {
		return false
	}
	msg := cmd() // once: a Tick's fires once
	if m, ok := msg.(tea.BatchMsg); ok {
		return slices.ContainsFunc(m, func(c tea.Cmd) bool { return clipped(c, want) })
	}
	return msg == tea.SetClipboard(want)()
}

// keys presses keys and is the Cmd the last one returned.
func keys(t *testing.T, a *App, s string) (cmd tea.Cmd) {
	t.Helper()
	ks, err := keymap.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ks {
		_, cmd = a.Update(teaKey(k))
	}
	return cmd
}

// yy copies the row as TSV of the columns shown, changes in, NULL empty,
// a value with a newline or a tab quoted, the row flashing; yl the cell,
// flashing it. p pastes the row yy took as a row added under the cursor's,
// every column's value but a key's with a default; for another table it
// pastes nothing (F3.32).
func TestYankRow(t *testing.T) {
	a := wide(160, 45)
	tab := loadOrders(t, a, 3)
	tab.cols.Cols[0].Default = "nextval('t_order_id_seq'::regclass)"
	tab.hidden["meta"] = true
	feed(t, a, "jlli9<Esc>") // row 2's amount
	want := "2\tdone\t9\tt\t\"line one\nline two\tafter tab \x1b[31mred\"\t2026-09-01 00:02:00+00"
	if cmd := keys(t, a, "yy"); !clipped(cmd, want) || a.grid(a.focused(), tab).Yank == nil || *a.grid(a.focused(), tab).Yank != [2]int{1, -1} {
		t.Fatalf("yy: flash %v", a.flash)
	}
	if st := styleOf(t, a.render(), "done"); st.Bg != a.theme.Yank || st.Fg != a.theme.Bg {
		t.Errorf("the row flashing: %+v", st)
	}
	if cmd := keys(t, a, "yl"); !clipped(cmd, "9") || *a.grid(a.focused(), tab).Yank != [2]int{1, 2} {
		t.Fatalf("yl: flash %v", a.flash)
	}
	a.Update(flashDone{a.flashSeq})
	if a.grid(a.focused(), tab).Yank != nil {
		t.Error("the flash goes")
	}
	feed(t, a, "p")
	if len(tab.added) != 1 || tab.row != 2 {
		t.Fatalf("p: added %d, cursor %d", len(tab.added), tab.row)
	}
	cells := tab.added[0].cells
	if _, ok := cells["id"]; ok || cells["amount"].val.S != "9" || cells["meta"].val.S == "" || cells["note"].val != tab.page.Rows[1][5] {
		t.Errorf("pasted: %+v", cells)
	}
	a.rowCopy.table = tableID{"public", "t_user"}
	if feed(t, a, "p"); len(tab.added) != 1 {
		t.Error("another table's row pasted")
	}
}

// A console's yanks flash what they took, as vim.hl.on_yank: a line, a
// word; a delete does not (F3.32).
func TestYankFlashConsole(t *testing.T) {
	a, c := inConsole(t, "")
	c.ed.Load("select 1\nfrom t")
	p := a.focused()
	keys(t, a, "yy")
	if v, _ := a.consoleView(p, c); v.Yank != (ui.Sel{Mode: ui.SelLines}) {
		t.Fatalf("yy: %+v", v.Yank)
	}
	keys(t, a, "yiw")
	if v, _ := a.consoleView(p, c); v.Yank != (ui.Sel{Mode: ui.SelChars, To: ui.TextPos{Col: 5}}) {
		t.Fatalf("yiw: %+v", v.Yank)
	}
	if st := styleOf(t, a.render(), "select"); st.Bg != a.theme.Yank {
		t.Errorf("the word flashing: %+v", st)
	}
	a.Update(flashDone{a.flashSeq})
	if keys(t, a, "dd"); a.flash != nil {
		t.Error("dd flashes")
	}
}
