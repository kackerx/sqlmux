package app

import (
	"encoding/csv"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/db"
	"sqlmux/internal/editor"
	"sqlmux/internal/ui"
)

// copiedRow is the row yy took, for p (§10.6): its table, and each
// column's value as it showed, changes in; none for a DEFAULT.
type copiedRow struct {
	table tableID
	vals  map[string]db.Val
}

// yankFlash is a yank flashing in pane (F3.32, nvim's vim.hl.on_yank): a
// grid's record and field, -1 for all of it, or a console's text.
type yankFlash struct {
	pane int
	cell [2]int
	text ui.Sel
	seq  int
}

type flashDone struct{ seq int }

// yankFlashFor is how long a yank flashes.
var yankFlashFor = 150 * time.Millisecond

// flashYank shows f till yankFlashFor is up, or the next one.
func (a *App) flashYank(f yankFlash) tea.Cmd {
	a.flashSeq++
	f.seq = a.flashSeq
	a.flash = &f
	return tea.Tick(yankFlashFor, func(time.Time) tea.Msg { return flashDone{f.seq} })
}

// value is column col of row sr, of row key key, as it shows, changes in;
// not ok for a DEFAULT.
func (t *dataTab) value(sr shownRow, key, col string) (db.Val, bool) {
	e, ok := t.edits[editKey{key, col}]
	if sr.add != nil {
		e, ok = sr.add.cells[col]
	}
	switch {
	case ok:
		return e.val, !e.def
	case sr.add != nil:
		return db.Val{}, false
	}
	return t.page.Rows[sr.rec][slices.IndexFunc(t.page.Cols, func(c db.Col) bool { return c.Name == col })], true
}

// yankGrid is yy (row) and yl on the focused grid (§7.6, F3.32): the row
// as TSV of the columns shown, or the cell, to the clipboard, flashing; a
// table's row kept for p too, every column of it. NULL and DEFAULT are
// empty.
func (a *App) yankGrid(row bool) tea.Cmd {
	p := a.focused()
	gs, _, _, ok := a.gridOf(p)
	if !ok {
		return nil
	}
	var cells []string // the columns shown, in order
	switch tab := p.tab(); {
	case tab.Data != nil:
		t := tab.Data
		sr, key, ok := t.cursor()
		if !ok {
			return nil
		}
		vals := map[string]db.Val{}
		for _, c := range t.page.Cols {
			if v, set := t.value(sr, key, c.Name); set {
				vals[c.Name] = v
			}
		}
		for _, f := range t.shownCols() {
			cells = append(cells, vals[t.page.Cols[f].Name].S)
		}
		if row {
			a.rowCopy = &copiedRow{idOf(t.table), vals}
		}
	case gs.row < len(tab.Result.page.Rows): // a result's
		for _, v := range tab.Result.page.Rows[gs.row] {
			cells = append(cells, v.S)
		}
	default:
		return nil
	}
	flash := yankFlash{pane: p.ID, cell: [2]int{gs.row, -1}}
	text := tsv(cells)
	if !row {
		flash.cell[1], text = gs.col, cells[gs.col]
	}
	return tea.Batch(tea.SetClipboard(text), a.flashYank(flash))
}

// tsv is one row of tab separated values, quoted where a value needs it
// as a spreadsheet reads them: a tab, a newline or a " in it.
func tsv(vals []string) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = '\t'
	w.Write(vals)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

// pasteRow is p (§10.6, F3.32): the row yy took, a row added under the
// cursor's with its values, but for the row identity's columns that have
// a default (serial, identity): theirs. A row of another table, or none
// taken, pastes nothing.
// ponytail: a generated column is copied too, and the save fails on it;
// leave those out when the catalog tells them apart
func (a *App) pasteRow() tea.Cmd {
	_, t, ok := a.focusedGrid()
	if !ok || a.rowCopy == nil || a.rowCopy.table != idOf(t.table) {
		return nil
	}
	cmd := a.addRow()
	sr, _, ok := t.cursor()
	if !ok || sr.add == nil { // read only: no row added
		return cmd
	}
	sr.add.cells = map[string]edit{}
	for name, v := range a.rowCopy.vals {
		if !slices.Contains(t.cols.Key(), name) || t.column(name).Default == "" {
			sr.add.cells[name] = edit{val: v}
		}
	}
	return cmd
}

// yankSel is what a console's yank took, as its flash covers it.
func yankSel(y editor.Yank) ui.Sel {
	s := ui.Sel{Mode: ui.SelChars, From: ui.TextPos(y.From), To: ui.TextPos(y.To)}
	switch {
	case y.Block:
		s.Mode, s.Left, s.Right = ui.SelBlock, y.Left, y.Right
	case y.Linewise:
		s.Mode = ui.SelLines
	default:
		s.To.Col-- // To is not in: what starts before it is
	}
	return s
}
