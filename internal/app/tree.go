package app

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// treeState is one window's schema tree (§7.8); its tables are the session's.
type treeState struct {
	cursor, top int // into the filtered list
	filter      ui.Input
	filtering   bool // the filter row has the keys (INSERT)
}

// catalogMsg is what loadCatalog read.
type catalogMsg struct {
	schemas []string
	current string
	tables  []db.Table
	err     error
}

// loadCatalog reads the schemas and every schema's tables on Meta; a
// table's columns wait until it is opened (§8.4).
func (a *App) loadCatalog() tea.Cmd {
	meta := a.sess.Meta
	return func() tea.Msg {
		ctx := context.Background()
		schemas, current, err := postgres.Schemas(ctx, meta)
		if err != nil {
			return catalogMsg{err: err}
		}
		tables, err := postgres.Tables(ctx, meta)
		return catalogMsg{schemas: schemas, current: current, tables: tables, err: err}
	}
}

func (a *App) gotCatalog(m catalogMsg) tea.Cmd {
	if m.err != nil {
		return a.showToast(m.err.Error(), toastTTL)
	}
	s := a.sess
	s.Schemas, s.Tables = m.schemas, m.tables
	if !slices.Contains(s.Schemas, s.Schema) { // the first load, or the schema is gone
		s.Schema = m.current
		if s.Schema == "" && len(s.Schemas) > 0 {
			s.Schema = s.Schemas[0]
		}
	}
	a.clampTree()
	return nil
}

// treeTables is the tree's schema's tables, and those that pass the filter
// in name order, where it matched (§7.8).
func (a *App) treeTables() ([]db.Table, []ui.Match) {
	var ts []db.Table
	var names []string
	for _, t := range a.sess.Tables {
		if t.Schema == a.sess.Schema {
			ts, names = append(ts, t), append(names, t.Name)
		}
	}
	ms := ui.Filter(a.win().tree.filter.Text, names)
	slices.SortFunc(ms, func(x, y ui.Match) int { return x.Index - y.Index })
	return ts, ms
}

// treeRows is how many tables the sidebar shows.
func (a *App) treeRows() int { return ui.TreeRows(a.sidebarRect().Dy() - 2) }

// treeView is cursor and top kept on a list of n and in a view of rows.
func treeView(cursor, top, n, rows int) (int, int) {
	cursor = max(min(cursor, n-1), 0)
	return cursor, max(min(top, cursor, n-rows), cursor-rows+1, 0)
}

func (a *App) clampTree() {
	t := &a.win().tree
	_, ms := a.treeTables()
	t.cursor, t.top = treeView(t.cursor, t.top, len(ms), a.treeRows())
}

func (a *App) treeMove(d int) {
	a.win().tree.cursor += d
	a.clampTree()
}

// scrollTree moves the view, not the cursor, which is only pulled back into
// it, as in nvim (§7.8).
func (a *App) scrollTree(notches int) {
	t := &a.win().tree
	_, ms := a.treeTables()
	rows := a.treeRows()
	t.top = max(min(t.top+notches*wheelStep, len(ms)-rows), 0)
	t.cursor = max(min(t.cursor, t.top+rows-1), t.top)
	a.clampTree()
}

// treeOpen opens the table under the cursor (§7.8).
func (a *App) treeOpen(newTab bool) tea.Cmd {
	ts, ms := a.treeTables()
	if c := a.win().tree.cursor; c < len(ms) {
		return a.openTable(ts[ms[c].Index], newTab)
	}
	return nil
}

// treeFilter puts the keys in the tree's filter row, showing and focusing
// the tree first.
func (a *App) treeFilter() {
	win := a.win()
	win.TreeOpen = true
	a.showPane(win.Tree.ID)
	if win.Focus == win.Tree.ID {
		win.tree.filtering = true
		win.tree.filter.Pos = len(win.tree.filter.Text)
	}
}

// filterKey edits the tree's filter, nvim-tree's live filter (§7.8): ↵
// keeps it and gives the keys back to the list, esc clears it, keeping the
// cursor on its table.
func (a *App) filterKey(k keymap.Key) {
	t := &a.win().tree
	switch k {
	case "<CR>":
		t.filtering, t.cursor, t.top = false, 0, 0
	case keymap.Esc:
		_, ms := a.treeTables()
		at := -1
		if t.cursor < len(ms) {
			at = ms[t.cursor].Index
		}
		t.filter, t.filtering, t.cursor = ui.Input{}, false, at
	default:
		if editInput(&t.filter, k) {
			t.cursor, t.top = 0, 0
		}
	}
	a.clampTree()
}

// editInput applies an input's own editing keys to in (they are no
// bindings) and reports whether its text changed.
func editInput(in *ui.Input, k keymap.Key) bool {
	switch k {
	case "<Left>":
		in.Left()
		return false
	case "<Right>":
		in.Right()
		return false
	case "<BS>":
		in.Backspace()
		return true
	}
	if keymap.Text(k) == "" {
		return false
	}
	in.Insert(keymap.Text(k))
	return true
}

// openTarget is the data pane a table opens in (§12): the one whose + was
// clicked, the focused one, else the window's first; nil when there is none.
func (a *App) openTarget() *Pane {
	leaves := a.win().Root.Leaves()
	if i := slices.IndexFunc(leaves, func(p *Pane) bool { return p.ID == a.win().newTabIn }); i >= 0 {
		return leaves[i]
	}
	if p := a.focused(); p.Kind == KindData {
		return p
	}
	if i := slices.IndexFunc(leaves, func(p *Pane) bool { return p.Kind == KindData }); i >= 0 {
		return leaves[i]
	}
	return nil
}
