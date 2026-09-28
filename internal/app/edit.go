package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/ui"
)

// edit is a cell's change, not saved yet (§10.1): text, NULL or DEFAULT.
// Changes outlive pages and queries: a row is known by its identity.
type edit struct {
	val  db.Val
	def  bool   // DEFAULT, the keyword
	orig db.Val // as loaded: the row must still hold it to be saved (§10.3)
}

// editKey is a changed cell: its row by the row identity's values as
// loaded, NUL between them (no PG text holds one), and its column.
type editKey struct{ row, col string }

// cellEdit is the cell being typed into (§10.1).
type cellEdit struct {
	key   editKey
	orig  db.Val // as loaded
	start string // the text the edit started with: left so, nothing changes
	in    ui.Input
}

// rowKey is page row rec's identity (§10.1): its row identity's values.
func (t *dataTab) rowKey(rec int) string {
	var vals []string
	for _, k := range t.cols.Key() {
		i := slices.IndexFunc(t.page.Cols, func(c db.Col) bool { return c.Name == k }) // select * has them all
		vals = append(vals, t.page.Rows[rec][i].S)
	}
	return strings.Join(vals, "\x00")
}

// editCell starts editing the focused grid's current cell (§10.1), its
// text all selected, or with text pasted in its place. A table without a
// row identity can't be saved to, so it isn't edited.
func (a *App) editCell(pasted *string) tea.Cmd {
	p, t, ok := a.focusedGrid()
	if !ok || p.Kind != KindData || len(t.page.Rows) == 0 {
		return nil
	}
	if t.cols.Key() == nil {
		return a.showToast(t.table.Name+" 没有主键，也没有全部列都非空的唯一索引，只读", toastTTL)
	}
	field := t.fieldAt(t.col)
	c := &cellEdit{key: editKey{t.rowKey(t.row), t.page.Cols[field].Name}, orig: t.page.Rows[t.row][field]}
	cur := c.orig
	if e, ok := t.edits[c.key]; ok {
		cur = e.val
	}
	if !cur.Null { // a NULL or DEFAULT starts empty
		c.start = cur.S
	}
	c.in = ui.Input{Text: c.start, Pos: len(c.start), All: true}
	if pasted != nil {
		c.in = ui.Input{Text: *pasted, Pos: len(*pasted)}
	}
	t.typing, t.cell = "cell", c
	return nil
}

// commitCell ends the cell's edit: what was typed is its change, none when
// back to what was loaded; left as it started, nothing changes (§10.1).
func (t *dataTab) commitCell() {
	c := t.cell
	t.typing, t.cell = "", nil
	if c.in.Text != c.start {
		t.setEdit(c.key, edit{val: db.Val{S: c.in.Text}, orig: c.orig})
	}
}

// setEdit makes e cell k's change; one giving back what was loaded is
// none. A change starts over what the last save said (§10.3).
func (t *dataTab) setEdit(k editKey, e edit) {
	t.note, t.failed = "", ""
	if !e.def && e.val == e.orig {
		delete(t.edits, k)
		return
	}
	if t.edits == nil {
		t.edits = map[editKey]edit{}
	}
	t.edits[k] = e
}

// endEdit commits the focused cell's edit before anything else happens:
// a click elsewhere, the wheel, a key that isn't the cell's (§10.1).
func (a *App) endEdit() {
	if t := a.typingTab(); t != nil && t.cell != nil {
		t.commitCell()
	}
}

// saveMsg answers save.
type saveMsg struct {
	tab    *dataTab
	sent   map[editKey]edit
	rows   []postgres.Row
	failed int // into rows; -1 for none
	err    error
	took   time.Duration
}

// save writes the focused tab's changes to its table (§10.3): on Main, an
// UPDATE a row in row identity order, in one transaction.
func (a *App) save() tea.Cmd {
	p := a.focused()
	t := dataOf(p)
	if p.Kind != KindData || t == nil || len(t.edits) == 0 || t.saving {
		return nil
	}
	sent := maps.Clone(t.edits)
	byRow := map[string]bool{}
	for k := range sent {
		byRow[k.row] = true
	}
	var rows []postgres.Row
	for _, key := range slices.Sorted(maps.Keys(byRow)) {
		r := postgres.Row{Key: strings.Split(key, "\x00")}
		for _, c := range t.cols.Cols { // in table order
			if e, ok := sent[editKey{key, c.Name}]; ok {
				r.Cols = append(r.Cols, postgres.Change{Name: c.Name, Val: e.val, Default: e.def, Old: e.orig})
			}
		}
		rows = append(rows, r)
	}
	t.saving = true
	a.busy++
	main, table, key := a.sess.Main, t.table, t.cols.Key()
	return func() tea.Msg {
		start := time.Now()
		failed, err := postgres.Save(context.Background(), main, table.Schema, table.Name, key, rows)
		return saveMsg{t, sent, rows, failed, err, time.Since(start)}
	}
}

// gotSave shows how the save went on the query bar's right (Q-06). Saved,
// the changes sent go, those made since stay, and the page loads again;
// failed, all is rolled back and the changes stay, the row at fault named
// by its row identity (it may be on another page) and marked (§10.3).
func (a *App) gotSave(m saveMsg) tea.Cmd {
	a.busy--
	t := m.tab
	t.saving = false
	switch {
	case errors.Is(m.err, context.Canceled):
		t.note, t.noteFg = "已取消，已回滚", a.theme.Warn
	case m.err != nil:
		msg := m.err.Error() + "，已回滚"
		if m.failed >= 0 {
			key := m.rows[m.failed].Key
			var named []string
			for i, c := range t.cols.Key() {
				named = append(named, c+" = "+key[i])
			}
			row := strings.Join(named, ", ")
			if msg = row + "：" + msg; errors.Is(m.err, postgres.ErrStale) {
				msg = row + " 的" + m.err.Error() + "，已回滚"
			}
			t.failed = strings.Join(key, "\x00")
		}
		t.note, t.noteFg = msg, a.theme.Error
	default:
		for k, e := range m.sent {
			switch now, ok := t.edits[k]; {
			case now == e:
				delete(t.edits, k)
			case ok && !e.def: // changed again meanwhile: the row holds what was sent now
				now.orig = e.val
				t.edits[k] = now
			}
		}
		cmd := a.fetch(t, true) // the rows may leave the WHERE now
		t.note, t.noteFg = fmt.Sprintf("已保存 %d 行 · %s", len(m.rows), m.took.Round(time.Millisecond)), nil
		return cmd
	}
	return nil
}

// confirmBox is the question put before changes are thrown away (§10.5).
type confirmBox struct {
	text, yes string
	then      func() tea.Cmd
}

// unsaved is how many changed cells the tabs of panes hold.
func unsaved(panes ...*Pane) int {
	n := 0
	for _, p := range panes {
		for _, tab := range p.Tabs {
			if tab.Data != nil {
				n += len(tab.Data.edits)
			}
		}
	}
	return n
}

// unlessUnsaved runs then, or with n changed cells to lose asks first:
// "<what> 有 N 处修改未保存，<verb>会丢弃。" (§10.5).
func (a *App) unlessUnsaved(n int, what, verb string, then func() tea.Cmd) tea.Cmd {
	if n == 0 {
		return then()
	}
	a.confirm = &confirmBox{fmt.Sprintf("%s有 %d 处修改未保存，%s会丢弃。", what, n, verb), verb, then}
	return nil
}

// quit ends the program, asking first when changes would go (§10.5).
func (a *App) quit() tea.Cmd {
	var ps []*Pane
	for _, w := range a.sess.Windows {
		if w.Root != nil {
			ps = append(ps, w.Root.Leaves()...)
		}
	}
	return a.unlessUnsaved(unsaved(ps...), "", "退出", func() tea.Cmd { return tea.Quit })
}
