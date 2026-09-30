package app

import (
	"encoding/csv"
	"os"
	"os/exec"
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
	return tea.Batch(clipCopy(text), a.flashYank(flash))
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
// a default (serial, identity) and the generated always identity columns,
// which take none (PG's 428C9): theirs. A row of another table, or none
// taken, pastes nothing.
// ponytail: a generated (stored) column is copied too, and the save fails
// on it; leave those out when the catalog tells them apart
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
		def := t.column(name).Default
		if def != "generated always as identity" && (!slices.Contains(t.cols.Key(), name) || def == "") {
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

// clipTools are the system clipboard's commands, local tools first and
// OSC 52 without them as nvim's clipboard provider has it, with its
// arguments (runtime/autoload/provider/clipboard.vim, v0.12.4); the order
// is §11's, not quite nvim's, which tries xsel before xclip and wants
// wl-paste on PATH with wl-copy. The first on PATH, with its display set
// for Wayland's and X11's, is used, so a tmux dropping OSC 52 doesn't
// matter; with none the terminal is asked over OSC 52 (F3.38). nvim's
// xclip -quiet and xsel --nodetach keep the process it waits on as the
// selection's owner; without them the tools fork one off and exit.
var clipTools = []clipTool{
	{"", []string{"pbcopy"}, []string{"pbpaste"}},
	{"WAYLAND_DISPLAY", []string{"wl-copy", "--type", "text/plain"}, []string{"wl-paste", "--no-newline"}},
	{"DISPLAY", []string{"xclip", "-i", "-selection", "clipboard"}, []string{"xclip", "-o", "-selection", "clipboard"}},
	{"DISPLAY", []string{"xsel", "-i", "-b"}, []string{"xsel", "-o", "-b"}},
}

type clipTool struct {
	env         string // what must be set for it: its display
	copy, paste []string
}

// clipCmd is the tool copying to the clipboard, or pasting from it, nil
// for OSC 52.
func clipCmd(paste bool) *exec.Cmd {
	for _, t := range clipTools {
		argv := t.copy
		if paste {
			argv = t.paste
		}
		if c := exec.Command(argv[0], argv[1:]...); c.Err == nil && (t.env == "" || os.Getenv(t.env) != "") { // Err: not on PATH
			return c
		}
	}
	return nil
}

// clipCopy puts text on the system clipboard, over OSC 52 when no tool
// is there or it fails.
func clipCopy(text string) tea.Cmd {
	return func() tea.Msg {
		if c := clipCmd(false); c != nil {
			c.Stdin = strings.NewReader(text) // no pipe out: the forked owner would hold Run up
			if c.Run() == nil {
				return nil
			}
		}
		return tea.SetClipboard(text)()
	}
}

// clipPaste asks for what the system clipboard holds: it comes as a
// ClipboardMsg, from the tool or else the terminal, or never.
func clipPaste() tea.Msg {
	if c := clipCmd(true); c != nil {
		if out, err := c.Output(); err == nil {
			return tea.ClipboardMsg{Content: string(out), Selection: 'c'}
		}
	}
	return tea.ReadClipboard()
}
