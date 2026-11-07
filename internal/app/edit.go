package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// edit is a cell's change, not saved yet (§10.1): text, NULL or DEFAULT.
// Changes outlive pages and queries: a row is known by its identity.
type edit struct {
	val  db.Val
	def  bool   // DEFAULT, the keyword
	orig db.Val // as loaded: the row must still hold it to be saved (§10.3)
}

// editKey is a changed cell: its row by the row identity's values as
// loaded, joinKey's, and its column.
type editKey struct{ row, col string }

// cellEdit is the cell being typed into (§10.1).
type cellEdit struct {
	key    editKey // of a row added, the column alone
	add    *newRow // the row added it is in, if it is (§10.6)
	orig   db.Val  // as loaded
	start  string  // the text the edit started with: left so, nothing changes
	in     ui.Input
	sel    int  // the option picked; -1 for none, as they open (§10.2)
	top    int  // the first option shown, held while the one picked is in view (F13.10)
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
	return joinKey(vals)
}

// joinKey is a row identity's values as one string, splitKey's way back:
// JSON, as a value may hold any character, MySQL's a NUL.
func joinKey(vals []string) string {
	b, _ := json.Marshal(vals)
	return string(b)
}

func splitKey(key string) (vals []string) {
	_ = json.Unmarshal([]byte(key), &vals)
	return vals
}

// newRow is a row added in the grid, not saved yet (§10.6): on page page,
// under that page's row after (-1: above the first), with the cells set
// in it; the rest go DEFAULT.
type newRow struct {
	page, after int
	cells       map[string]edit
}

// shownRow is a row the grid shows: the page's row rec, or one added.
type shownRow struct {
	rec int // -1 for one added
	add *newRow
}

// shownRows is the grid's rows: the page's, each followed by the rows
// added under it. One whose page is past the last or whose row is past
// its page's, a page before's under a LIMIT since cut, goes at the last
// page's end, so none is out of sight (§10.6).
func (t *dataTab) shownRows() []shownRow {
	n, p := len(t.page.Rows), t.shown.pageNo
	under := func(after int) (out []shownRow) {
		for _, r := range t.added {
			switch {
			case r.page == p && r.after < n && r.after == after:
			case after == n-1 && !t.next && (r.page > p || r.page == p && r.after >= n || r.page < p && r.after >= t.shown.limit): // the last page's end
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
func (t *dataTab) cursor() (shownRow, string, bool) { return t.recordAt(t.row) }

// recordAt is the row at position row among the rows shown, and its key, ""
// for one added.
func (t *dataTab) recordAt(row int) (shownRow, string, bool) {
	rows := t.shownRows()
	if row >= len(rows) || len(t.shownCols()) == 0 {
		return shownRow{}, "", false
	}
	if sr := rows[row]; sr.add == nil {
		return sr, t.rowKey(sr.rec), true
	}
	return rows[row], "", true
}

// put makes e the change of column col of row sr, a row added's or a
// page row's by its key (§10.1, §10.6).
func (t *dataTab) put(add *newRow, k editKey, e edit) {
	if add == nil {
		t.setEdit(k, e)
		return
	}
	t.note, t.failed = ui.Note{}, nil
	if add.cells == nil {
		add.cells = map[string]edit{}
	}
	add.cells[k.col] = e
}

// whyNoEdit is why t's table takes no change, as its toast says it; ""
// when it does (§10.1): a read-only connection's table, or one with no
// row identity, which can't be saved to.
func (t *dataTab) whyNoEdit() string {
	switch {
	case t.conn.readOnly:
		return i18n.T("%s 是只读连接", t.conn.Spec.Name)
	case t.cols.Key() == nil:
		return i18n.T("%s 没有主键，也没有全部列都非空的唯一索引，只读", t.table.Name)
	case t.cols.ReadOnly != "":
		return i18n.T("%s 的%s，只读", t.table.Name, t.cols.ReadOnly)
	}
	return ""
}

// hasDefault is whether column col offers DEFAULT in a row, added or not:
// MySQL's auto_increment in a row added alone, an UPDATE to it writing 0;
// SQLite's in a row added only.
func hasDefault(col db.Column, added bool) bool {
	return col.Default != "" && (added || col.Default != "auto_increment" && !col.AddOnly)
}

// cellSrc is the cell under the cursor as an editor of it, the cell's own
// input or the value editor, starts (§10.1).
type cellSrc struct {
	t      *dataTab
	key    editKey
	add    *newRow // the row added it is in, if it is
	orig   db.Val  // as loaded; a row added's is DEFAULT till set
	text   string  // what it starts with: the change's value, else the loaded one; a NULL or DEFAULT has none
	before edit    // the change it had, if had
	had    bool
	col    db.Column
	// editable is whether the row takes changes at all; why is what keeps this
	// cell from them (a read-only connection, a column that cannot be set), ""
	// when nothing does.
	editable bool
	why      string
}

// cellAtCursor is the cell under the cursor of t, if there is one.
func (t *dataTab) cellAtCursor() (cellSrc, bool) {
	sr, key, ok := t.cursor()
	if !ok {
		return cellSrc{}, false
	}
	field := t.fieldAt(t.col)
	name := t.page.Cols[field].Name
	s := cellSrc{t: t, key: editKey{key, name}, add: sr.add, orig: db.Val{Null: true}, col: t.column(name), editable: t.editable(sr, key)}
	if sr.add == nil {
		s.orig = t.page.Rows[sr.rec][field]
	}
	cur := s.orig
	if e, ok := t.change(sr, key, name); ok {
		cur, s.before, s.had = e.val, e, true
	}
	if !cur.Null {
		s.text = cur.S
	}
	if s.why = t.whyNoEdit(); s.why == "" && s.col.Fixed {
		s.why = i18n.T("%s 是 %s，只读", s.col.Name, s.col.Type)
	} else if s.why == "" {
		s.why = s.col.ReadOnly
		if s.why == "" && sr.add == nil {
			s.why = t.cols.NoUpdate
		}
	}
	return s, true
}

// editCell starts editing the focused grid's current cell (§10.1), its
// text all selected, or with text pasted in its place; not on a table
// whyNoEdit names.
func (a *App) editCell(pasted *string) tea.Cmd {
	t, ok := a.focusedGrid()
	if !ok || jsonViewOf(a.focused()) != nil { // the table is not in sight (F14.5)
		return nil
	}
	s, ok := t.cellAtCursor()
	if !ok || !s.editable {
		return nil
	}
	if s.why != "" {
		return a.showToast(s.why)
	}
	c := &cellEdit{key: s.key, add: s.add, orig: s.orig, start: s.text}
	c.in, c.sel = ui.Input{Text: c.start, Pos: len(c.start), All: true}, -1
	if pasted != nil {
		c.in = ui.Input{Text: *pasted, Pos: len(*pasted)}
	}
	t.typing, t.cell = "cell", c
	return nil
}

// editable reports whether row sr, of row key key, takes changes: not one
// marked for deletion, with no toast, r taking the mark back first
// (§10.6); nor one added while a save inserting it is out, which would
// lose them (§10.3).
func (t *dataTab) editable(sr shownRow, key string) bool {
	if sr.add != nil {
		return !t.saving
	}
	return !t.deleted[key]
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

// choices are the values a column offers by its type: an enum's labels, a
// boolean's true and false; vals are the engine's own text for them, so one
// back to what was loaded is no change (§10.2).
func choices(col db.Column) (labels, vals []string) {
	switch col.Type {
	case "boolean":
		return []string{"true", "false"}, []string{"t", "f"}
	case "tinyint(1)": // MySQL's boolean
		return []string{"true", "false"}, []string{"1", "0"}
	}
	return col.Enum, col.Enum
}

// options is what the cell being edited offers under it (§10.2): a
// boolean's or an enum's values, filtered by what is typed once it is,
// then NULL, DEFAULT and back to what was loaded, as the column allows.
func (t *dataTab) options() []option {
	c := t.cell
	col := t.column(c.key.col)
	labels, vals := choices(col)
	pattern := c.in.Text
	if c.in.All {
		pattern = ""
	}
	var out []option
	if timeKind(col.Type) != ui.NotTime {
		out = append(out, option{label: i18n.T("◷ 现在"), now: true})
	}
	for _, m := range ui.Filter(pattern, labels) {
		out = append(out, option{label: labels[m.Index], note: i18n.T("值"), pos: m.Pos, set: edit{val: db.Val{S: vals[m.Index]}, orig: c.orig}})
	}
	if !col.NotNull {
		out = append(out, option{label: "∅ NULL", set: edit{val: db.Val{Null: true}, orig: c.orig}})
	}
	if hasDefault(col, c.add != nil) {
		out = append(out, option{label: "DEFAULT", set: edit{val: db.Val{Null: true}, def: true, orig: c.orig}})
	}
	if _, ok := t.edits[c.key]; ok && c.add == nil { // a row added's r takes the whole row back
		out = append(out, option{label: i18n.T("↺ 原值"), set: edit{val: c.orig, orig: c.orig}})
	}
	return out
}

// moveOption moves the pick by d around the ends; with none picked, down
// picks the first and up the last (§10.2).
func (t *dataTab) moveOption(d int) {
	c := t.cell
	n := len(t.options())
	if !c.folded && n > 0 {
		c.sel = step(c.sel, d, n)
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
// that can't hold it, nor a table whyNoEdit names.
func (a *App) setSpecial(def bool) {
	t, ok := a.focusedGrid()
	if !ok || t.whyNoEdit() != "" {
		return
	}
	sr, key, ok := t.cursor()
	field := t.fieldAt(t.col)
	name := t.page.Cols[field].Name
	if col := t.column(name); !ok || !t.editable(sr, key) || col.Fixed || col.ReadOnly != "" || def && !hasDefault(col, sr.add != nil) || !def && col.NotNull {
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
// rewriting that part of the text alone (§10.2); the option picked goes.
// Text that doesn't parse steps nothing.
func (t *dataTab) stepSeg(i, d int) {
	c, k := t.cell, t.cellKind()
	if segs := ui.TimeSegs(k, c.in.Text); i < 0 || i >= len(segs) {
		return
	}
	c.seg, c.sel = i, -1
	if s := ui.StepTime(k, c.in.Text, i, d); d != 0 {
		c.in = ui.Input{Text: s, Pos: len(s)}
	}
}

// stepCurrent is ↑ and ↓ over a time's box: its current part stepped;
// on an option, nothing (§10.2).
func (t *dataTab) stepCurrent(d int) {
	if t.cell.sel < 0 {
		t.stepSeg(t.cell.seg, d)
	}
}

// moveSeg is Tab and S-Tab over a time's box (§10.2, F3.34): d through its
// parts, then the options' row, around the ends. On an option no part is
// current; ↵ takes it. Text with no parts (empty, infinity) has the
// options alone.
func (t *dataTab) moveSeg(d int) {
	c := t.cell
	n := len(ui.TimeSegs(t.cellKind(), c.in.Text))
	if n == 0 {
		t.moveOption(d)
		return
	}
	all := n + len(t.options())
	at := c.seg
	if c.sel >= 0 {
		at = n + c.sel
	}
	if at = ((at+d)%all + all) % all; at < n {
		c.seg, c.sel = at, -1
	} else {
		c.sel = at - n
	}
}

// drawCellMenu draws what a cell being edited offers under its edit, or
// over it (§10.2): a time's parts and options in a row, or a list. A hint
// goes right at the edit, the menu past it: the two placed as one, so
// neither leaves the screen (§10.7).
func (a *App) drawCellMenu(f *ui.Frame, p *Pane, t *dataTab) {
	os := t.options()
	at := a.grid(p, t).EditRect(gridRect(a.layout()[p.ID], t)).Min
	h := ui.CellHint{Text: t.cellHint()}
	extra := 0 // the hint's rows
	if h.Text != "" {
		extra = ui.CellHintRows
	}
	hb, _ := ui.CompleteBox(a.window(), at, h.Width(), 1, 0, ui.CompleteRows) // a hint alone
	// place is where a menu w wide with n rows goes, and how many show
	place := func(w, n int) (uv.Rectangle, int) {
		box, rows := ui.CompleteBox(a.window(), at, w, n+extra, 0, ui.CompleteRows)
		if box.Min.Y > at.Y { // down: the hint on top
			hb.Min.Y, box.Min.Y = box.Min.Y, box.Min.Y+extra
		} else {
			box.Max.Y -= extra
			hb.Min.Y = box.Max.Y
		}
		hb.Max.Y = hb.Min.Y + extra
		return box, max(rows-extra, 0)
	}
	var draw func()
	switch k := t.cellKind(); {
	case t.cell.folded:
	case k != ui.NotTime:
		v := ui.TimePick{Kind: k, Text: t.cell.in.Text, Seg: t.cell.seg, Sel: t.cell.sel}
		if t.cell.sel >= 0 { // on an option, no part is current (F3.34)
			v.Seg = -1
		}
		for _, o := range os {
			v.Options = append(v.Options, o.label)
		}
		w, ht := v.Size()
		box, _ := place(w, ht-2)
		draw = func() { v.Draw(f, box) }
	case len(os) > 0:
		v := ui.Complete{Sel: t.cell.sel}
		w := 20
		for _, o := range os {
			v.Items = append(v.Items, ui.CompleteItem{Text: o.label, Pos: o.pos, Note: o.note})
			w = max(w, ui.Width(o.label+"  "+o.note)+4)
		}
		box, rows := place(w, len(v.Items))
		_, t.cell.top = ui.ListView(v.Sel, t.cell.top, len(v.Items), rows)
		v.Top = t.cell.top
		draw = func() { v.Draw(f, box, rows) }
	}
	if h.Text != "" {
		h.Draw(f, hb)
	}
	if draw != nil {
		draw()
	}
}

// cellHint is what is wrong with the text of the cell being edited, if
// anything (§10.7). Text as the edit started is let be: no change, and
// maybe PG's own output, which cellCheck may not take (1900's +08:05:43).
// ponytail: PG's types alone; MySQL's text goes to the database as typed
func (t *dataTab) cellHint() string {
	if c := t.cell; c.in.Text != c.start {
		return t.checkText(c.key.col, c.in.Text)
	}
	return ""
}

// checkText is what is wrong with text as a value of column col, "" when
// nothing is (the cell's input and the value editor both ask it).
func (t *dataTab) checkText(col, text string) string {
	if t.conn.sql() && t.conn.eng.Dialect() == sqlkit.PG {
		return cellCheck(t.typeOf(col), text)
	}
	return ""
}

// setEdit makes e cell k's change; one giving back what was loaded is
// none. A change starts over what the last save said (§10.3).
func (t *dataTab) setEdit(k editKey, e edit) {
	t.note, t.failed = ui.Note{}, nil
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
	t, ok := a.focusedGrid()
	if !ok {
		return
	}
	sr, key, ok := t.cursor()
	switch k := (editKey{key, t.page.Cols[t.fieldAt(t.col)].Name}); {
	case !ok:
	case sr.add != nil || t.deleted[key]: // as dd
		a.deleteRow()
	default:
		if e, ok := t.edits[k]; ok {
			t.setEdit(k, edit{val: e.orig, orig: e.orig})
		}
	}
}

// dropAdded takes row r added out of the grid, the cursor staying in it.
func (t *dataTab) dropAdded(r *newRow) {
	t.deselect() // the positions of the rows after it move
	t.added = slices.DeleteFunc(t.added, func(o *newRow) bool { return o == r })
	t.row = max(min(t.row, len(t.shownRows())-1), 0)
}

// addRow is o and the query bar's + (§10.6): a row under the cursor's, on
// its page, the cursor to its first column.
func (a *App) addRow() tea.Cmd {
	t, ok := a.focusedGrid()
	if !ok {
		return nil
	}
	if why := t.whyNoEdit(); why != "" {
		return a.showToast(why)
	}
	if why := t.cols.NoInsert; why != "" {
		return a.showToast(why)
	}
	// added keeps the order they show in, (page, after), a save's too:
	// right after the cursor's, or before those added under its row before
	// ponytail: at the last page's end the rows of pages before it, a LIMIT
	// cut since, show after this page's though they sort before; sort by
	// where shownRows puts them if 新增的第 N 行 is ever read off there
	t.deselect() // the positions of the rows after it move
	r := &newRow{page: t.shown.pageNo, after: -1}
	at := len(t.added)
	if sr, _, ok := t.cursor(); ok && sr.add != nil {
		r.page, r.after = sr.add.page, sr.add.after
		at = slices.Index(t.added, sr.add) + 1
	} else {
		if ok {
			r.after = sr.rec
		}
		if i := slices.IndexFunc(t.added, func(o *newRow) bool { return cmp.Or(cmp.Compare(o.page, r.page), cmp.Compare(o.after, r.after)) >= 0 }); i >= 0 {
			at = i
		}
	}
	t.added = slices.Insert(t.added, at, r)
	t.note, t.failed = ui.Note{}, nil
	t.row = slices.IndexFunc(t.shownRows(), func(sr shownRow) bool { return sr.add == r })
	t.col = 0
	a.gridMove(func(r, c, _, _ int) (int, int) { return r, c }) // into view
	return nil
}

// deleteRow is dd and the query bar's − (§10.6): the cursor's row marked
// for deletion, or not any more; a row added goes.
func (a *App) deleteRow() tea.Cmd {
	t, ok := a.focusedGrid()
	if !ok {
		return nil
	}
	return a.deleteRecords(t, []int{t.row})
}

// deleteRecords marks the records of t at positions for deletion, or takes the
// marks off when they all have one, which is what dd on one record is; a row
// added goes, as dd takes it out, but not when marks are taken off, which keeps
// what was typed in it. Nothing is selected after (§10.6, F10.7).
func (a *App) deleteRecords(t *dataTab, positions []int) tea.Cmd {
	if why := t.whyNoEdit(); why != "" {
		return a.showToast(why)
	}
	var keys []string
	var added []*newRow
	for _, pos := range positions {
		switch sr, key, ok := t.recordAt(pos); {
		case !ok:
		case sr.add != nil:
			added = append(added, sr.add)
		default:
			keys = append(keys, key)
		}
	}
	if len(keys)+len(added) == 0 {
		return nil
	}
	t.note, t.failed = ui.Note{}, nil // a change starts over what the last save said (§10.3)
	all := len(keys) > 0 && !slices.ContainsFunc(keys, func(k string) bool { return !t.deleted[k] })
	for _, k := range keys {
		switch {
		case all:
			delete(t.deleted, k)
		case t.deleted == nil:
			t.deleted = map[string]bool{k: true}
		default:
			t.deleted[k] = true
		}
	}
	if !all {
		for _, r := range added {
			t.dropAdded(r)
		}
	}
	t.deselect()
	return nil
}

// cellField is Tab and S-Tab in a row added (F3.33): the cell's edit
// ended as ↵ ends it, and the field d away edited; past the first or the
// last field the edit just ends. Text that is no value of the column
// keeps it where it is (§10.7).
func (a *App) cellField(d int) tea.Cmd {
	t := a.typingTab()
	if t == nil || t.cell == nil {
		return nil
	}
	if t.acceptCell(); t.cell != nil { // ◷ 现在 put the time in, which goes too; or the text is no value
		if t.cellHint() != "" {
			return nil
		}
		t.commitCell()
	}
	if next := t.col + d; next >= 0 && next < len(t.shownCols()) {
		t.col = next
		a.gridMove(func(r, c, _, _ int) (int, int) { return r, c }) // into view
		return a.editCell(nil)
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
	dels   []string  // the rows deleted, by row key
	rows   []db.Row  // updated
	adds   []*newRow // inserted, in the order they show
	failed int       // into dels, rows, adds in turn; -1 for none
	err    error
	took   time.Duration
	tx     byte // where Main's transaction is as the save ends (F5.3)
}

// save writes the focused tab's changes to its table in one transaction
// on Main (§10.3, §10.6), the one going on there in the manual mode or
// when the user began one: the rows marked deleted, an UPDATE a row with
// changes in row identity order, those rows' changes dropped, and the
// rows added, in the order they show.
func (a *App) save() tea.Cmd {
	p := a.focused()
	if c := queryOf(p); c != nil { // its file (§11)
		if err := c.flush(); err != nil {
			return a.saveFailed(err)
		}
		return nil
	}
	if t := dataOf(p); t != nil {
		return a.saveTab(t, false)
	}
	return nil
}

// saveTab is save of tab t, asked first when it deletes rows (§13「危险语句
// 先确认」): "将删除 N 行（另有 M 处修改），确定保存？"; no, and the table
// is as it was, the marks and the changes there. closing is :wq's: the tab
// closes once the save lands.
func (a *App) saveTab(t *dataTab, closing bool) tea.Cmd {
	if t.changes() > 0 && !t.saving && (a.previewSave || t.conn.Spec.Production) { // the statements first, on a production connection whatever the setting says (§13, F17.10)
		return a.askSave(t, closing)
	}
	if n := len(t.deleted); n > 0 && a.confirmDangerous && !t.saving {
		text := i18n.T("将删除 %d 行，确定保存？", n)
		if other := t.changes() - n; other > 0 {
			text = i18n.T("将删除 %d 行（另有 %d 处修改），确定保存？", n, other)
		}
		a.confirm = &confirmBox{text: text, yes: i18n.T("保存"), then: func() tea.Cmd { return a.sendSave(t, closing) }}
		return nil
	}
	return a.sendSave(t, closing)
}

// previewListed is how many statements a save's preview lists, the rest said as
// a count.
const previewListed = 20

// askSave is the question before a save (§13, F17.10): the statements it would
// send, as the source writes them with their values in, one a line, and how many
// rows it changes, adds and deletes; ↵ or y sends them, n or esc leaves the
// table as it is, the marks and the changes there.
func (a *App) askSave(t *dataTab, closing bool) tea.Cmd {
	_, ch := a.changesOf(t)
	var counts []string
	if n := len(ch.Updates); n > 0 {
		counts = append(counts, i18n.T("改 %d 行", n))
	}
	if n := len(ch.Inserts); n > 0 {
		counts = append(counts, i18n.T("增 %d 行", n))
	}
	if n := len(ch.Deletes); n > 0 {
		counts = append(counts, i18n.T("删 %d 行", n))
	}
	lines := []string{i18n.T("保存到 %s：%s", t.table.Name, strings.Join(counts, i18n.T("、")))}
	if t.conn.unatomic() { // no transaction around them (§10.3)
		lines = append(lines, i18n.T("  逐条写入，不在一个事务里：中途失败时前面的不能回滚"))
	}
	stmts := a.source(t, t.cols).Preview(ch)
	for _, s := range stmts[:min(len(stmts), previewListed)] {
		lines = append(lines, "  "+strings.NewReplacer("\r", "", "\n", "⏎", "\t", " ").Replace(s))
	}
	if len(stmts) > previewListed {
		lines = append(lines, i18n.T("  …另 %d 条", len(stmts)-previewListed))
	}
	a.confirm = writeBox(t.conn, strings.Join(lines, "\n"), i18n.T("保存"), false, func() tea.Cmd { return a.sendSave(t, closing) })
	return nil
}

// sendSave is the save of tab t, its changes taken as they are now.
func (a *App) sendSave(t *dataTab, closing bool) tea.Cmd {
	if t.changes() == 0 {
		return nil
	}
	if t.saving { // one is on its way: :wq closes the tab when it lands
		t.closing = t.closing || closing
		return nil
	}
	m, ch := a.changesOf(t)
	t.saving, t.closing = true, closing
	t.conn.busy++
	t.out++
	src, manual := a.source(t, t.cols), t.conn.Manual
	return func() tea.Msg {
		start := time.Now()
		m.failed, m.tx, m.err = src.Save(context.Background(), ch, manual)
		m.took = time.Since(start)
		return m
	}
}

// changesOf is what a save of tab t takes of it as it is now: the message that
// will answer, with what was sent, and the changes for the source (§10.3).
func (a *App) changesOf(t *dataTab) (saveMsg, db.Changes) {
	m := saveMsg{tab: t, sent: maps.Clone(t.edits), dels: slices.Sorted(maps.Keys(t.deleted)), adds: slices.Clone(t.added)}
	byRow := map[string]bool{}
	for k := range m.sent {
		byRow[k.row] = !t.deleted[k.row]
	}
	var ch db.Changes
	for _, key := range m.dels {
		ch.Deletes = append(ch.Deletes, splitKey(key))
	}
	for _, key := range slices.Sorted(maps.Keys(byRow)) {
		if !byRow[key] {
			continue
		}
		r := db.Row{Key: splitKey(key)}
		for _, c := range t.cols.Cols { // in table order
			if e, ok := m.sent[editKey{key, c.Name}]; ok {
				r.Cols = append(r.Cols, db.Change{Name: c.Name, Val: e.val, Default: e.def, Old: e.orig})
			}
		}
		m.rows = append(m.rows, r)
	}
	ch.Updates = m.rows
	for _, r := range m.adds {
		var cols []db.Change
		for _, c := range t.cols.Cols {
			if e, ok := r.cells[c.Name]; ok {
				cols = append(cols, db.Change{Name: c.Name, Val: e.val, Default: e.def})
			}
		}
		ch.Inserts = append(ch.Inserts, cols)
	}
	return m, ch
}

// landed takes off tab t what the first n statements of save m wrote, in its
// order, deletes, updates, inserts (§10.3): all of them for a save that went,
// those before the failure for an Unatomic engine's that did not (db.Written).
func (t *dataTab) landed(m saveMsg, n int) {
	take := func(have int) int { k := max(min(n, have), 0); n -= k; return k }
	dels := m.dels[:take(len(m.dels))]
	rows := m.rows[:take(len(m.rows))]
	adds := m.adds[:take(len(m.adds))]
	done := map[string]bool{}
	for _, k := range dels {
		done[k] = true
		delete(t.deleted, k)
	}
	for _, r := range rows {
		done[joinKey(r.Key)] = true
	}
	for k, e := range m.sent {
		if !done[k.row] {
			continue
		}
		switch now, ok := t.edits[k]; {
		case now == e:
			delete(t.edits, k)
		case ok && !e.def: // changed again meanwhile: the row holds what was sent now
			now.orig = e.val
			t.edits[k] = now
		}
	}
	t.added = slices.DeleteFunc(t.added, func(r *newRow) bool { return slices.Contains(adds, r) })
	if len(adds) > 0 {
		t.deselect() // the positions of the rows after them move
	}
}

// gotSave shows how the save went: saved or cancelled on the query bar's
// right (Q-06), failed on the error bar (§7.8「错误栏」). Saved, the
// changes sent go, those made since stay, and the page loads again;
// failed, all is rolled back and the changes stay, the row at fault named
// by its row identity (it may be on another page) and marked, a row added
// by its place among them (§10.3, §10.6). In a transaction, saved is not
// committed.
func (a *App) gotSave(m saveMsg) tea.Cmd {
	t := m.tab
	t.conn.busy--
	if m.tx != 0 { // 0: it never got Main
		t.conn.tx = m.tx
	}
	t.saving = false
	t.out--
	closing := t.closing
	t.closing = false
	var written db.Written
	partial := errors.As(m.err, &written) // an Unatomic engine's: what went before the failure stays (§10.3)
	undone := i18n.T("，已回滚")
	switch {
	case partial:
		undone = i18n.T("，前 %d 条已写入，不能回滚", written.N)
		t.landed(m, written.N)
	case t.conn.unatomic():
		undone = i18n.T("，没有写入")
	}
	switch {
	case errors.Is(m.err, context.Canceled):
		t.note = ui.Note{Head: i18n.T("已取消") + undone, Fg: a.theme.Warn}
		if partial {
			return a.fetch(t, true)
		}
	case errors.Is(m.err, db.ErrCommitUnknown): // may be written: not "rolled back", the marks stay for the user to look and decide (F13.1)
		t.note, t.failed = ui.Note{}, nil
		clearBar(&t.bar, "save") // an earlier failure's "已回滚" would say the opposite
		return a.showToast(i18n.T("%s：连接断开，提交结果未知：已刷新，请核对", t.conn.label()))
	case m.err != nil: // on the error bar: the query bar keeps what went well alone (§7.8「错误栏」)
		t.note = ui.Note{}
		head, e := "", t.conn.drv.ErrorOf(m.err)
		named := func(key string) string {
			var named []string
			for i, v := range splitKey(key) {
				named = append(named, t.cols.Key()[i]+" = "+v)
			}
			if t.failed = &key; errors.Is(m.err, db.ErrStale) || errors.Is(m.err, db.ErrGone) {
				return i18n.T("%s 的", strings.Join(named, ", "))
			}
			return i18n.T("%s：", strings.Join(named, ", "))
		}
		switch i := m.failed; {
		case i < 0:
		case i < len(m.dels):
			head = named(m.dels[i])
		case i < len(m.dels)+len(m.rows):
			head = named(joinKey(m.rows[i-len(m.dels)].Key))
		default:
			n := i - len(m.dels) - len(m.rows) + 1
			if errors.Is(m.err, db.ErrNotInserted) { // ours: the whole sentence, nothing of the database's to read on
				head, e.Message = i18n.T("新增的第 %d 行没有插入", n), ""
			} else {
				head = i18n.T("新增的第 %d 行：", n)
			}
		}
		t.bar = newErrorBar("save", e, head, undone)
		if partial { // the rows as they are now
			return tea.Batch(a.fetch(t, true), a.deadlocked(t.conn, m.err))
		}
		return a.deadlocked(t.conn, m.err)
	default:
		clearBar(&t.bar, "save")
		var follow tea.Cmd // a hash that lost its last field is no key now
		if t.table.Kind == "k" {
			follow = a.keysChanged(t.conn, t.table.Name)
		}
		t.landed(m, len(m.dels)+len(m.rows)+len(m.adds))
		if p := a.paneShowing(t); closing && t.changes() == 0 && t.cell == nil && p != nil { // :wq, and nothing changed since, nor being typed
			a.closeTab(p, p.Cur)
			return follow
		}
		cmd := a.fetch(t, true) // the rows may leave the WHERE now
		uncommitted := ""
		if m.tx == 'T' {
			uncommitted = i18n.T("（未提交）")
		}
		t.note = ui.Note{Head: i18n.T("已保存 %d 行%s · %s", len(m.dels)+len(m.rows)+len(m.adds), uncommitted, m.took.Round(time.Millisecond))}
		return tea.Batch(cmd, follow)
	}
	return nil
}
