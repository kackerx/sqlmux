package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

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
	key    editKey // of a row added, the column alone
	add    *newRow // the row added it is in, if it is (§10.6)
	orig   db.Val  // as loaded
	start  string  // the text the edit started with: left so, nothing changes
	in     ui.Input
	sel    int  // the option picked; -1 for none, as they open (§10.2)
	folded bool // its ▾ hid the options
	seg    int  // a time's part the keys step
}

// option is one of what the cell being edited offers (§10.2), and the
// change it makes.
type option struct {
	label, note string
	pos         []int
	set         edit
	now         bool // ◷ 现在: the time now in the text, the edit going on (§10.2)
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

// newRow is a row added in the grid, not saved yet (§10.6): on page page,
// under that page's row after (-1: above the first), with the cells set
// in it; the rest go DEFAULT.
type newRow struct {
	page, after int
	cells       map[string]edit
}

// cellsOf is r's cells set, none for no row.
func (r *newRow) cellsOf() map[string]edit {
	if r == nil {
		return nil
	}
	return r.cells
}

// shownRow is a row the grid shows: the page's row rec, or one added.
type shownRow struct {
	rec int // -1 for one added
	add *newRow
}

// shownRows is the grid's rows: the page's, each followed by the rows
// added under it. One whose page is past the last or whose row is past
// its page's goes at the last page's end, so none is out of sight
// (§10.6).
func (t *dataTab) shownRows() []shownRow {
	n, p := len(t.page.Rows), t.shown.pageNo
	under := func(after int) (out []shownRow) {
		for _, r := range t.added {
			switch {
			case r.page == p && r.after < n && r.after == after:
			case after == n-1 && !t.next && (r.page > p || r.page == p && r.after >= n): // the last page's end
			default:
				continue
			}
			out = append(out, shownRow{-1, r})
		}
		return out
	}
	out := under(-1)
	if n == 0 { // what under(-1) took as the end, too
		return out
	}
	for i := range n {
		out = append(append(out, shownRow{rec: i}), under(i)...)
	}
	return out
}

// changes is how many changes t holds, as its save button and a confirm
// box count them (§10.6): rows added and marked for deletion, and changed
// cells but those of rows marked.
func (t *dataTab) changes() int {
	n := len(t.added) + len(t.deleted)
	for k := range t.edits {
		if !t.deleted[k.row] {
			n++
		}
	}
	return n
}

// cursor is the grid's current row, and the row key of a page's one.
func (t *dataTab) cursor() (shownRow, string, bool) {
	rows := t.shownRows()
	if t.row >= len(rows) || len(t.shownCols()) == 0 {
		return shownRow{}, "", false
	}
	if sr := rows[t.row]; sr.add == nil {
		return sr, t.rowKey(sr.rec), true
	}
	return rows[t.row], "", true
}

// put makes e the change of column col of row sr, a row added's or a
// page row's by its key (§10.1, §10.6).
func (t *dataTab) put(add *newRow, k editKey, e edit) {
	if add == nil {
		t.setEdit(k, e)
		return
	}
	t.note, t.failed = ui.Note{}, ""
	if add.cells == nil {
		add.cells = map[string]edit{}
	}
	add.cells[k.col] = e
}

// readOnly is the toast for a table with no row identity: it can't be
// saved to, so it isn't changed (§10.1).
func (a *App) readOnly(t *dataTab) tea.Cmd {
	return a.showToast(t.table.Name+" 没有主键，也没有全部列都非空的唯一索引，只读", toastTTL)
}

// editCell starts editing the focused grid's current cell (§10.1), its
// text all selected, or with text pasted in its place. A table without a
// row identity can't be saved to, so it isn't edited.
func (a *App) editCell(pasted *string) tea.Cmd {
	_, t, ok := a.focusedGrid()
	if !ok {
		return nil
	}
	sr, key, ok := t.cursor()
	if !ok {
		return nil
	}
	if t.cols.Key() == nil {
		return a.readOnly(t)
	}
	field := t.fieldAt(t.col)
	c := &cellEdit{key: editKey{key, t.page.Cols[field].Name}, add: sr.add, orig: db.Val{Null: true}} // a row added's: DEFAULT, till set
	if sr.add == nil {
		c.orig = t.page.Rows[sr.rec][field]
	}
	cur := c.orig
	if e, ok := t.edits[c.key]; ok && sr.add == nil {
		cur = e.val
	} else if e, ok := sr.add.cellsOf()[c.key.col]; ok {
		cur = e.val
	}
	if !cur.Null { // a NULL or DEFAULT starts empty
		c.start = cur.S
	}
	c.in, c.sel = ui.Input{Text: c.start, Pos: len(c.start), All: true}, -1
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
		t.put(c.add, c.key, edit{val: db.Val{S: c.in.Text}, orig: c.orig})
	}
}

// options is what the cell being edited offers under it (§10.2): a
// boolean's or an enum's values, filtered by what is typed once it is,
// then NULL, DEFAULT and back to what was loaded, as the column allows.
func (t *dataTab) options() []option {
	c := t.cell
	col := t.column(c.key.col)
	labels, vals := col.Enum, col.Enum
	if col.Type == "boolean" { // PG's own text for them, so one back to what was loaded is no change
		labels, vals = []string{"true", "false"}, []string{"t", "f"}
	}
	pattern := c.in.Text
	if c.in.All {
		pattern = ""
	}
	var out []option
	if timeKind(col.Type) != ui.NotTime {
		out = append(out, option{label: "◷ 现在", now: true})
	}
	for _, m := range ui.Filter(pattern, labels) {
		out = append(out, option{label: labels[m.Index], note: "值", pos: m.Pos, set: edit{val: db.Val{S: vals[m.Index]}, orig: c.orig}})
	}
	if !col.NotNull {
		out = append(out, option{label: "∅ NULL", set: edit{val: db.Val{Null: true}, orig: c.orig}})
	}
	if col.Default != "" {
		out = append(out, option{label: "DEFAULT", set: edit{val: db.Val{Null: true}, def: true, orig: c.orig}})
	}
	if _, ok := t.edits[c.key]; ok && c.add == nil { // a row added's r takes the whole row back
		out = append(out, option{label: "↺ 原值", set: edit{val: c.orig, orig: c.orig}})
	}
	return out
}

// moveOption moves the pick by d around the ends; with none picked, down
// picks the first and up the last (§10.2).
func (t *dataTab) moveOption(d int) {
	c := t.cell
	n := len(t.options())
	switch {
	case c.folded || n == 0:
	case c.sel < 0 && d > 0:
		c.sel = 0
	case c.sel < 0:
		c.sel = n - 1
	default:
		c.sel = ((c.sel+d)%n + n) % n
	}
}

// acceptCell is ↵ in a cell: the option picked, else the text, unless it
// is no value of the column (§10.2, §10.7).
func (t *dataTab) acceptCell() {
	if os := t.options(); !t.cell.folded && t.cell.sel >= 0 && t.cell.sel < len(os) {
		t.applyOption(os[t.cell.sel])
		return
	}
	if t.cellHint() == "" {
		t.commitCell()
	}
}

// applyOption ends the edit with o's change, what was typed dropped; now
// only puts the time in the text.
func (t *dataTab) applyOption(o option) {
	if c := t.cell; o.now {
		s := ui.NowTime(timeKind(t.typeOf(c.key.col)), time.Now())
		c.in, c.sel = ui.Input{Text: s, Pos: len(s)}, -1
		return
	}
	c := t.cell
	t.typing, t.cell = "", nil
	t.put(c.add, c.key, o.set)
}

// setSpecial makes the grid's current cell NULL, or DEFAULT (cell.null,
// cell.default, §10.2); an edit of it under way ends so. Not for a column
// that can't hold it, nor a table with no row identity.
func (a *App) setSpecial(def bool) {
	_, t, ok := a.focusedGrid()
	if !ok || t.cols.Key() == nil {
		return
	}
	sr, key, ok := t.cursor()
	field := t.fieldAt(t.col)
	name := t.page.Cols[field].Name
	if col := t.column(name); !ok || def && col.Default == "" || !def && col.NotNull {
		return
	}
	if t.cell != nil {
		t.typing, t.cell = "", nil
	}
	e := edit{val: db.Val{Null: true}, def: def}
	if sr.add == nil {
		e.orig = t.page.Rows[sr.rec][field]
	}
	t.put(sr.add, editKey{key, name}, e)
}

// cellKind is the time the cell being edited holds, if any (§10.2).
func (t *dataTab) cellKind() ui.TimeKind { return timeKind(t.typeOf(t.cell.key.col)) }

// stepSeg steps a time's part i by d (0: just makes it the current part),
// rewriting that part of the text alone (§10.2). Text that doesn't parse
// steps nothing.
func (t *dataTab) stepSeg(i, d int) {
	c, k := t.cell, t.cellKind()
	if segs := ui.TimeSegs(k, c.in.Text); i < 0 || i >= len(segs) {
		return
	}
	c.seg = i
	if s := ui.StepTime(k, c.in.Text, i, d); d != 0 {
		c.in, c.sel = ui.Input{Text: s, Pos: len(s)}, -1
	}
}

// moveSeg moves a time's current part by d, around the ends.
func (t *dataTab) moveSeg(d int) {
	if n := len(ui.TimeSegs(t.cellKind(), t.cell.in.Text)); n > 0 {
		t.cell.seg = ((t.cell.seg+d)%n + n) % n
	}
}

// drawCellMenu draws what a cell being edited offers under its edit, or
// over it (§10.2): a time's parts and options in a row, or a list.
func (a *App) drawCellMenu(f *ui.Frame, p *Pane, t *dataTab) {
	os := t.options()
	at := a.grid(p, t).EditRect(gridRect(a.layout()[p.ID], t)).Min
	var box uv.Rectangle
	var draw func()
	switch k := t.cellKind(); {
	case t.cell.folded:
	case k != ui.NotTime:
		v := ui.TimePick{Kind: k, Text: t.cell.in.Text, Seg: t.cell.seg, Sel: t.cell.sel}
		for _, o := range os {
			v.Options = append(v.Options, o.label)
		}
		w, h := v.Size()
		box, _ = ui.CompleteBox(a.window(), at, w, h-2)
		draw = func() { v.Draw(f, box) }
	case len(os) > 0:
		v := ui.Complete{Sel: t.cell.sel}
		w := 20
		for _, o := range os {
			v.Items = append(v.Items, ui.CompleteItem{Text: o.label, Pos: o.pos, Note: o.note})
			w = max(w, ui.Width(o.label+"  "+o.note)+4)
		}
		var rows int
		box, rows = ui.CompleteBox(a.window(), at, w, len(v.Items))
		v.Top = max(0, v.Sel-rows+1)
		draw = func() { v.Draw(f, box, rows) }
	}
	if hint := t.cellHint(); hint != "" { // right at the edit, the menu past it, down or up (§10.7)
		h := ui.CellHint{Text: hint}
		hb, _ := ui.CompleteBox(a.window(), at, h.Width(), 1)
		if draw != nil {
			d := ui.CellHintRows
			if box.Min.Y < at.Y {
				hb.Min.Y, d = at.Y-d, -d
			} else {
				hb.Min.Y = at.Y + 1
			}
			hb.Max.Y, box = hb.Min.Y+ui.CellHintRows, box.Add(uv.Pos(0, d))
		}
		h.Draw(f, hb)
	}
	if draw != nil {
		draw()
	}
}

// cellHint is what is wrong with the text of the cell being edited, if
// anything (§10.7).
func (t *dataTab) cellHint() string { return cellCheck(t.typeOf(t.cell.key.col), t.cell.in.Text) }

// setEdit makes e cell k's change; one giving back what was loaded is
// none. A change starts over what the last save said (§10.3).
func (t *dataTab) setEdit(k editKey, e edit) {
	t.note, t.failed = ui.Note{}, ""
	if !e.def && e.val == e.orig {
		delete(t.edits, k)
		return
	}
	if t.edits == nil {
		t.edits = map[editKey]edit{}
	}
	t.edits[k] = e
}

// revertCell is r (§10.1): the grid's current cell back to what was
// loaded, as its options' ↺ 原值; a row added goes, a row marked for
// deletion isn't any more (§10.6).
func (a *App) revertCell() {
	_, t, ok := a.focusedGrid()
	if !ok {
		return
	}
	sr, key, ok := t.cursor()
	switch k := (editKey{key, t.page.Cols[t.fieldAt(t.col)].Name}); {
	case !ok:
	case sr.add != nil:
		t.dropAdded(sr.add)
	case t.deleted[key]:
		delete(t.deleted, key)
	default:
		if e, ok := t.edits[k]; ok {
			t.setEdit(k, edit{val: e.orig, orig: e.orig})
		}
	}
}

// dropAdded takes row r added out of the grid, the cursor staying in it.
func (t *dataTab) dropAdded(r *newRow) {
	t.added = slices.DeleteFunc(t.added, func(o *newRow) bool { return o == r })
	t.row = max(min(t.row, len(t.shownRows())-1), 0)
}

// addRow is o and the query bar's + (§10.6): a row under the cursor's, on
// its page, the cursor to its first column.
func (a *App) addRow() tea.Cmd {
	_, t, ok := a.focusedGrid()
	if !ok {
		return nil
	}
	if t.cols.Key() == nil {
		return a.readOnly(t)
	}
	r := &newRow{page: t.shown.pageNo, after: -1}
	at := len(t.added)
	if sr, _, ok := t.cursor(); ok && sr.add != nil { // right after it
		r.page, r.after = sr.add.page, sr.add.after
		at = slices.Index(t.added, sr.add) + 1
	} else if ok { // before those added under it before
		r.after = sr.rec
		if i := slices.IndexFunc(t.added, func(o *newRow) bool { return o.page == r.page && o.after == r.after }); i >= 0 {
			at = i
		}
	}
	t.added = slices.Insert(t.added, at, r)
	t.note, t.failed = ui.Note{}, ""
	t.row = slices.IndexFunc(t.shownRows(), func(sr shownRow) bool { return sr.add == r })
	t.col = 0
	a.gridMove(func(r, c, _, _ int) (int, int) { return r, c }) // into view
	return nil
}

// deleteRow is dd and the query bar's − (§10.6): the cursor's row marked
// for deletion, or not any more; a row added goes.
func (a *App) deleteRow() tea.Cmd {
	_, t, ok := a.focusedGrid()
	if !ok {
		return nil
	}
	if t.cols.Key() == nil {
		return a.readOnly(t)
	}
	switch sr, key, ok := t.cursor(); {
	case !ok:
	case sr.add != nil:
		t.dropAdded(sr.add)
	case t.deleted[key]:
		delete(t.deleted, key)
	default:
		if t.deleted == nil {
			t.deleted = map[string]bool{}
		}
		t.deleted[key] = true
		t.note, t.failed = ui.Note{}, ""
	}
	return nil
}

// endEdit commits the focused cell's edit before anything else happens:
// a click elsewhere, the wheel, a key that isn't the cell's (§10.1). It is
// false when the text is no value of the column: the edit stays, and so
// does what asked (§10.7).
func (a *App) endEdit() bool {
	t := a.typingTab()
	if t == nil || t.cell == nil {
		return true
	}
	if t.cellHint() != "" { // no value of the column: what would end it waits (§10.7)
		return false
	}
	t.commitCell()
	return true
}

// saveMsg answers save.
type saveMsg struct {
	tab    *dataTab
	sent   map[editKey]edit
	dels   []string       // the rows deleted, by row key
	rows   []postgres.Row // updated
	adds   []*newRow      // inserted, in the order they show
	failed int            // into dels, rows, adds in turn; -1 for none
	err    error
	took   time.Duration
}

// save writes the focused tab's changes to its table in one transaction
// on Main (§10.3, §10.6): the rows marked deleted, an UPDATE a row with
// changes in row identity order, those rows' changes dropped, and the
// rows added, in the order they show.
func (a *App) save() tea.Cmd {
	p := a.focused()
	if c := consoleOf(p); c != nil { // its file (§11)
		if err := c.flush(); err != nil {
			return a.saveFailed(err)
		}
		return nil
	}
	t := dataOf(p)
	if t == nil || t.changes() == 0 || t.saving {
		return nil
	}
	m := saveMsg{tab: t, sent: maps.Clone(t.edits), dels: slices.Sorted(maps.Keys(t.deleted)), adds: slices.Clone(t.added)}
	byRow := map[string]bool{}
	for k := range m.sent {
		byRow[k.row] = !t.deleted[k.row]
	}
	var ch postgres.Changes
	for _, key := range m.dels {
		ch.Deletes = append(ch.Deletes, strings.Split(key, "\x00"))
	}
	for _, key := range slices.Sorted(maps.Keys(byRow)) {
		if !byRow[key] {
			continue
		}
		r := postgres.Row{Key: strings.Split(key, "\x00")}
		for _, c := range t.cols.Cols { // in table order
			if e, ok := m.sent[editKey{key, c.Name}]; ok {
				r.Cols = append(r.Cols, postgres.Change{Name: c.Name, Val: e.val, Default: e.def, Old: e.orig})
			}
		}
		m.rows = append(m.rows, r)
	}
	ch.Updates = m.rows
	slices.SortStableFunc(m.adds, func(x, y *newRow) int { return cmp.Or(cmp.Compare(x.page, y.page), cmp.Compare(x.after, y.after)) })
	for _, r := range m.adds {
		var cols []postgres.Change
		for _, c := range t.cols.Cols {
			if e, ok := r.cells[c.Name]; ok {
				cols = append(cols, postgres.Change{Name: c.Name, Val: e.val, Default: e.def})
			}
		}
		ch.Inserts = append(ch.Inserts, cols)
	}
	t.saving = true
	a.busy++
	t.out++
	main, table, key := a.sess.Main, t.table, t.cols.Key()
	return func() tea.Msg {
		start := time.Now()
		m.failed, m.err = postgres.Save(context.Background(), main, table.Schema, table.Name, key, ch)
		m.took = time.Since(start)
		return m
	}
}

// gotSave shows how the save went: saved or cancelled on the query bar's
// right (Q-06), failed on the error bar (§7.8「错误栏」). Saved, the
// changes sent go, those made since stay, and the page loads again;
// failed, all is rolled back and the changes stay, the row at fault named
// by its row identity (it may be on another page) and marked, a row added
// by its place among them (§10.3, §10.6).
func (a *App) gotSave(m saveMsg) tea.Cmd {
	a.busy--
	t := m.tab
	t.saving = false
	t.out--
	closing := t.closing
	t.closing = false
	switch {
	case errors.Is(m.err, context.Canceled):
		t.note = ui.Note{Head: "已取消，已回滚", Fg: a.theme.Warn}
	case m.err != nil:
		head := ""
		named := func(key string) string {
			var named []string
			for i, v := range strings.Split(key, "\x00") {
				named = append(named, t.cols.Key()[i]+" = "+v)
			}
			if t.failed = key; errors.Is(m.err, postgres.ErrStale) || errors.Is(m.err, postgres.ErrGone) {
				return strings.Join(named, ", ") + " 的"
			}
			return strings.Join(named, ", ") + "："
		}
		switch i := m.failed; {
		case i < 0:
		case i < len(m.dels):
			head = named(m.dels[i])
		case i < len(m.dels)+len(m.rows):
			head = named(strings.Join(m.rows[i-len(m.dels)].Key, "\x00"))
		default:
			head = fmt.Sprintf("新增的第 %d 行：", i-len(m.dels)-len(m.rows)+1)
		}
		t.bar = newErrorBar("save", postgres.ServerErrorOf(m.err), head, "，已回滚")
	default:
		clearBar(&t.bar, "save")
		for k, e := range m.sent {
			switch now, ok := t.edits[k]; {
			case now == e:
				delete(t.edits, k)
			case ok && !e.def: // changed again meanwhile: the row holds what was sent now
				now.orig = e.val
				t.edits[k] = now
			}
		}
		for _, k := range m.dels {
			delete(t.deleted, k)
		}
		t.added = slices.DeleteFunc(t.added, func(r *newRow) bool { return slices.Contains(m.adds, r) })
		if p := a.paneShowing(t); closing && t.changes() == 0 && p != nil { // :wq, and nothing changed since
			a.closeTab(p)
			return nil
		}
		cmd := a.fetch(t, true) // the rows may leave the WHERE now
		t.note = ui.Note{Head: fmt.Sprintf("已保存 %d 行 · %s", len(m.dels)+len(m.rows)+len(m.adds), m.took.Round(time.Millisecond))}
		return cmd
	}
	return nil
}

// confirmBox is the question put before changes are thrown away (§10.5).
type confirmBox struct {
	text, yes string
	then      func() tea.Cmd
}

// unsaved is how many changes the tabs of panes hold (§10.6).
func unsaved(panes ...*Pane) int {
	n := 0
	for _, p := range panes {
		for _, tab := range p.Tabs {
			if tab.Data != nil {
				n += tab.Data.changes()
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
	return a.unlessUnsaved(unsaved(ps...), "", "退出", func() tea.Cmd {
		if cmd, ok := a.flushAll(ps...); !ok { // the consoles' text is not lost (§11)
			return cmd
		}
		return tea.Quit
	})
}

// intBits are the integer types' sizes, by format_type's names (§10.7).
var intBits = map[string]int{"smallint": 16, "integer": 32, "bigint": 64}

// pgInteger and pgDecimal are integer's and numeric's text past a sign as
// PG 17 reads them: digits with a _ between two, or 0x, 0o and 0b ones;
// numeric's also with a fraction and an exponent.
var (
	pgInteger = regexp.MustCompile(`^(\d+(_\d+)*|0[xX](_?[0-9a-fA-F])+|0[oO](_?[0-7])+|0[bB](_?[01])+)$`)
	pgDecimal = regexp.MustCompile(`^(\d+(_\d+)*(\.(\d+(_\d+)*)?)?|\.\d+(_\d+)*)([eE][+-]?\d+(_\d+)*)?$`)
)

// cellCheck is what is wrong with text as a value of catalog type typ, as
// the edit's hint says it (§10.7); "" when nothing is, or typ goes
// unchecked. Valid is what PG 17's input functions take, spaces around
// it and all; the database has the last word.
func cellCheck(typ, text string) string {
	s := strings.TrimSpace(text)
	switch t := baseType(typ); {
	case text == "":
	case intBits[t] > 0:
		n := strings.TrimLeft(s, "+-")
		if len(s)-len(n) > 1 || !pgInteger.MatchString(n) {
			return "不是有效的整数"
		}
		base, n := 10, strings.ReplaceAll(n, "_", "")
		if len(n) > 1 && strings.ContainsAny(n[1:2], "xXoObB") { // 010 is ten, as in PG: base 0 only past a prefix
			base = 0
		}
		if _, err := strconv.ParseInt(s[:len(s)-len(strings.TrimLeft(s, "+-"))]+n, base, intBits[t]); err != nil {
			return fmt.Sprintf("超出 int%d 的范围", intBits[t]/8)
		}
	case t == "numeric", t == "real", t == "double precision":
		n := strings.TrimLeft(s, "+-")
		switch w := strings.ToLower(n); {
		case len(s)-len(n) > 1:
			return "不是有效的数字"
		case w == "nan" || w == "inf" || w == "infinity":
		case t == "numeric":
			if !pgDecimal.MatchString(n) && !pgInteger.MatchString(n) {
				return "不是有效的数字"
			}
		default:
			// ponytail: Go's ParseFloat, not strtod: 0x10 is refused and
			// 1e-400 taken, PG the other way round
			bits := map[string]int{"real": 32, "double precision": 64}[t]
			if _, err := strconv.ParseFloat(s, bits); errors.Is(err, strconv.ErrRange) {
				return fmt.Sprintf("超出 float%d 的范围", bits/8)
			} else if err != nil || strings.Contains(s, "_") { // Go's takes 1_000, strtod doesn't
				return "不是有效的数字"
			}
		}
	case t == "boolean": // a prefix of one of PG's words, on and off by two letters
		w := strings.ToLower(s)
		for _, word := range []string{"true", "false", "yes", "no", "on", "off", "1", "0"} {
			if strings.HasPrefix(word, w) && (len(w) > 1 || word[0] != 'o') && w != "" {
				return ""
			}
		}
		return "不是有效的布尔值"
	case t == "uuid":
		if !validUUID(text) {
			return "不是有效的 UUID"
		}
	case t == "json" || t == "jsonb":
		if !json.Valid([]byte(text)) {
			return "不是有效的 JSON"
		}
	case timeKind(typ) != ui.NotTime:
		k := timeKind(typ)
		words := []string{"now", "today", "tomorrow", "yesterday", "epoch", "infinity", "-infinity", "+infinity"}
		if k == ui.Time || k == ui.TimeTZ {
			words = []string{"now", "allballs"}
		}
		if !slices.Contains(words, strings.ToLower(s)) && !isoTime[k].MatchString(s) {
			return "不是有效的日期 / 时间"
		}
	}
	return ""
}

// isoTime is what cellCheck takes as each kind's text besides its words:
// ISO dates and times, wider than the parts a time steps (§10.2), a zone
// on any, which PG drops for a type without one.
// ponytail: PG takes much more (2026/09/20, month names); add them when
// someone is kept from one
var isoTime = func() map[ui.TimeKind]*regexp.Regexp {
	date, tm, zone := `\d{4,}-\d{1,2}-\d{1,2}`, `\d{1,2}:\d{2}(:\d{2}(\.\d+)?)?`, `( ?([+-]\d{1,2}(:?\d{2})?|[zZ]))?`
	at := `^` + date + `([ Tt]` + tm + zone + `)?( (?i:bc))?$`
	return map[ui.TimeKind]*regexp.Regexp{
		ui.Date: regexp.MustCompile(`^` + date + `( (?i:bc))?$`), ui.Timestamp: regexp.MustCompile(at), ui.TimestampTZ: regexp.MustCompile(at),
		ui.Time: regexp.MustCompile(`^` + tm + zone + `$`), ui.TimeTZ: regexp.MustCompile(`^` + tm + zone + `$`),
	}
}()

// validUUID is uuid_in's rule: 32 hex digits, a - after any four of them
// but the last, in braces or not; no spaces.
func validUUID(s string) bool {
	if strings.HasPrefix(s, "{") != strings.HasSuffix(s, "}") {
		return false
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	n := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case '0' <= c && c <= '9', 'a' <= c && c <= 'f', 'A' <= c && c <= 'F':
			n++
		case c != '-' || n == 0 || n%4 != 0 || n == 32 || i+1 == len(s) || s[i+1] == '-':
			return false
		}
	}
	return n == 32
}
