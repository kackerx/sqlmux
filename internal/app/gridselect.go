package app

import (
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// gridStateOf is the state of the grid pane p shows in its current tab, a
// table's or a result's, or nil: a log or a landing page has no records.
func gridStateOf(p *Pane) *gridState {
	switch t := p.tab(); {
	case t == nil:
	case t.Data != nil:
		return &t.Data.gridState
	case t.Result != nil && t.Result.page.Cols != nil:
		return &t.Result.gridState
	}
	return nil
}

// selecting is whether records are marked on s, or VISUAL is on: the
// select scope then applies (§6.4, F10.7).
func (s *gridState) selecting() bool { return s.visual || len(s.marked) > 0 }

// ranged is the records from the anchor to the cursor in VISUAL or UNSET.
func (s *gridState) ranged() map[int]bool {
	if !s.visual {
		return nil
	}
	out := map[int]bool{}
	for r := min(s.anchor, s.row); r <= max(s.anchor, s.row); r++ {
		out[r] = true
	}
	return out
}

// selected is the records selected, by position among the rows shown: those
// marked, and in VISUAL those from the anchor to the cursor too; in UNSET
// without them.
func (s *gridState) selected() map[int]bool {
	if !s.visual {
		return s.marked
	}
	out := maps.Clone(s.marked)
	if out == nil {
		out = map[int]bool{}
	}
	for r := range s.ranged() {
		if s.unset {
			delete(out, r)
		} else {
			out[r] = true
		}
	}
	return out
}

// settle ends VISUAL or UNSET, its range marked or taken out; deselect
// forgets it all.
func (s *gridState) settle()   { s.marked, s.visual, s.unset = s.selected(), false, false }
func (s *gridState) deselect() { s.marked, s.visual, s.unset = nil, false, false }

// positions are the records selected, in the order they show.
func (s *gridState) positions() []int {
	return slices.Sorted(maps.Keys(s.selected()))
}

// records is how many records the grid of tab has.
func records(tab *Tab) int {
	switch {
	case tab.Data != nil:
		return len(tab.Data.shownRows())
	case tab.Result != nil:
		return len(tab.Result.page.Rows)
	}
	return 0
}

// selectToggle is m: the record under the cursor marked, or not any more.
func (a *App) selectToggle() {
	p := a.focused()
	if s := gridStateOf(p); s != nil && s.row < records(p.tab()) {
		if s.marked == nil {
			s.marked = map[int]bool{}
		}
		if s.marked[s.row] {
			delete(s.marked, s.row)
		} else {
			s.marked[s.row] = true
		}
	}
}

// selectVisual is v, and V with unset: VISUAL begins at the record under the
// cursor, and the next one of either ends it, its range marked, or with unset
// taken out of the marked (F10.7, F12.12).
func (a *App) selectVisual(unset bool) {
	p := a.focused()
	s := gridStateOf(p)
	switch {
	case s == nil || s.row >= records(p.tab()):
	case s.visual:
		s.settle()
	default:
		s.visual, s.unset, s.anchor = true, unset, s.row
	}
}

// selectClear is esc: out of VISUAL, its range marked, or out of UNSET, its
// range taken out; with only marks, none.
func (a *App) selectClear() {
	if s := gridStateOf(a.focused()); s != nil {
		if s.visual {
			s.settle()
		} else {
			s.deselect()
		}
	}
}

// selectDelete is d on the records selected of a table (F10.7).
func (a *App) selectDelete() tea.Cmd {
	t, ok := a.focusedGrid()
	if !ok {
		return nil
	}
	pos := t.positions()
	if len(pos) == 0 { // an UNSET took them all out: the mode is over, there is nothing to delete
		t.deselect()
		return nil
	}
	return a.deleteRecords(t, pos)
}

// selectYank is y on the records selected: a JSON array of them, in the order
// they show, each as Y writes it; their numbers flash. Nothing is selected
// after (F10.7).
func (a *App) selectYank() tea.Cmd {
	p := a.focused()
	s := gridStateOf(p)
	if s == nil {
		return nil
	}
	var objects []string
	for _, pos := range s.positions() {
		fs, ok := jsonFields(p.tab(), pos)
		if !ok {
			continue
		}
		raw, err := rowRaw(fs)
		if err != nil {
			return a.showToast(err.Error())
		}
		objects = append(objects, string(raw))
	}
	flash := yankFlash{pane: p.ID, nums: s.selected()}
	s.deselect()
	if len(objects) == 0 {
		return nil
	}
	text, err := indentJSON([]byte("[" + strings.Join(objects, ",") + "]"))
	if err != nil {
		return a.showToast(err.Error())
	}
	return tea.Batch(a.clipCopy(text), a.flashYank(flash))
}
