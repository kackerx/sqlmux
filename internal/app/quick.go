package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/sqlkit"
	"sqlmux/internal/ui"
)

// quickSQL is the palette's quick SQL (§12): the statement whose result
// shows, and the one out on Meta.
type quickSQL struct {
	ran, running string // "" for none; running is the SQL scope's input as ↵ ran it
	res          db.Result
	err          string
	write        bool // ran is a write, not run: a console runs it (F-05)
	top, left    int  // the result's scroll
}

// quickRows is how many rows a quick SQL shows, quickHistory how many runs
// a connection's quick SQL history keeps (§12).
const (
	quickRows    = 100
	quickHistory = 50
)

// quickMsg answers runQuick.
type quickMsg struct {
	p   *palette
	res db.Result
	err error
}

// colsMsg brings the columns of a table the quick SQL names (§12「补全」).
type colsMsg struct {
	table db.Table
	cols  db.Columns
	err   error
}

// last is the statement the result area stands for: the one running, else
// the one that ran. An input that differs from it is modified (F-03).
func (q *quickSQL) last() string {
	if q.running != "" {
		return q.running
	}
	return q.ran
}

// runQuick runs sql on Meta for the palette (§12), unless one is running
// already, and puts it at the top of the connection's history.
func (a *App) runQuick(sql string) tea.Cmd {
	p := a.palette
	if p.quick == nil {
		p.quick = &quickSQL{}
	}
	if p.quick.running != "" { // ↵ waits for it, or for C-c
		return nil
	}
	p.comp = nil
	if a.state.SQL == nil {
		a.state.SQL = map[string][]string{}
	}
	h := slices.Insert(slices.DeleteFunc(a.state.SQL[a.sess.Name], func(s string) bool { return s == sql }), 0, sql)
	a.state.SQL[a.sess.Name] = h[:min(len(h), quickHistory)]
	if q := p.quick; !sqlkit.IsRead(sql, sqlkit.PG) { // a write is for a console, where running it is the user's own ↵ (F-05)
		q.ran, q.res, q.err, q.write, q.top, q.left = sql, db.Result{}, "", true, 0, 0
		return a.saveState()
	}
	p.quick.running = sql
	a.busy++
	meta, schema := a.sess.Meta, a.sess.Schema
	return tea.Batch(a.saveState(), func() tea.Msg {
		r, err := postgres.Quick(context.Background(), meta, sql, schema, quickRows)
		return quickMsg{p, r, err}
	})
}

// gotQuick shows a quick SQL's answer, if its palette is still open: the
// result goes with the palette (§12). Cancelled, the last result stays.
func (a *App) gotQuick(m quickMsg) tea.Cmd {
	a.busy--
	if m.p != a.palette {
		return nil
	}
	q := m.p.quick
	sql := q.running
	q.running = ""
	if errors.Is(m.err, context.Canceled) {
		return a.showToast("查询已取消", toastTTL)
	}
	q.ran, q.res, q.err, q.write, q.top, q.left = sql, m.res, "", false, 0, 0
	if m.err != nil {
		q.res, q.err = db.Result{Took: m.res.Took}, m.err.Error()
	}
	return nil
}

// quickView is the result area: "100+ 行 · 12ms · 只读" over the table, or
// the database's error, or what a write not run says; the buttons for what
// can follow (F-04).
func (a *App) quickView(q *quickSQL) *ui.PaletteResult {
	var title []string
	switch {
	case q.running != "":
		title = append(title, "… 行")
	case q.err != "", q.write:
	case q.res.Cols == nil: // no rows to show: what it did
		title = append(title, q.res.Tag)
	case q.res.Truncated:
		title = append(title, fmt.Sprintf("%d+ 行", len(q.res.Rows)))
	default:
		title = append(title, fmt.Sprintf("%d 行", len(q.res.Rows)))
	}
	if q.running == "" && q.res.Took > 0 {
		title = append(title, q.res.Took.Round(time.Millisecond).String())
	}
	r := &ui.PaletteResult{
		Title: strings.Join(append(title, "只读"), " · "),
		Err:   q.err,
		Grid:  ui.Grid{Rows: q.res.Rows, Row: -1, Col: -1, Top: q.top, Left: q.left, Key: a.icons.Key},
	}
	for _, c := range q.res.Cols {
		r.Grid.Cols = append(r.Grid.Cols, ui.GridCol{Name: c.Name, Type: colType(c.Type)})
	}
	edit := ui.Hint{Key: a.keys.Hint("quicksql.edit", "palette"), Label: "console", Action: "quicksql.edit"}
	if q.write {
		r.Err, r.Warn = "写语句不在这里执行", true
		if edit.Key != "" {
			r.Err += " · " + edit.Key + " 在 console 中打开"
		}
	}
	if q.res.Cols != nil { // rows to copy, or to put in the result area
		r.Hints = append(r.Hints,
			ui.Hint{Key: a.keys.Hint("quicksql.copy", "palette"), Label: "CSV", Action: "quicksql.copy"},
			ui.Hint{Key: a.keys.Hint("palette.open.tab", "palette"), Label: "结果区", Action: "palette.open.tab"})
	}
	r.Hints = bound(append(r.Hints, edit)...)
	return r
}

// quickToResult is C-t in the SQL scope (§12「C-t 送到结果区」): the rows
// shown, as they are, a pinned tab quick #n of the result area, which
// comes out if SPC r hid it; a line in the log; the palette stays. None
// without rows.
func (a *App) quickToResult() tea.Cmd {
	q := a.palette.quick
	if q == nil || q.res.Cols == nil {
		return nil
	}
	a.sess.RunSeq++
	r := &run{name: "quick", sql: q.ran, seq: a.sess.RunSeq, start: time.Now(), win: a.win(), done: true}
	p := a.showResult(r.win)
	a.log(r, r.sql, resultText(q.res), false)
	p.Tabs = append(p.Tabs, Tab{Name: r.label(1), Result: &resultTab{gridState: gridState{page: q.res}, run: r, part: 1, pinned: true}})
	selectTab(p, len(p.Tabs)-1)
	return nil
}

// quickRerun answers rerunQuick.
type quickRerun struct {
	rt  *resultTab
	res db.Result
	err error
}

// rerunQuick is R on a quick SQL's tab (§12): its SQL again as the palette
// runs it, on Meta, read only, the tree's schema first; the rows in the
// tab's place, a line in the log.
func (a *App) rerunQuick(rt *resultTab) tea.Cmd {
	r := rt.run
	if !r.done {
		return nil
	}
	r.done, r.start = false, time.Now()
	a.busy++
	meta, sql, schema := a.sess.Meta, r.sql, a.sess.Schema
	return tea.Batch(func() tea.Msg {
		res, err := postgres.Quick(context.Background(), meta, sql, schema, quickRows)
		return quickRerun{rt, res, err}
	}, a.runTick(r))
}

// gotQuickRerun shows what rerunQuick got: the rows, or a failure in the
// log, which shows, the rows as they were; a cancel says so (§11).
func (a *App) gotQuickRerun(m quickRerun) tea.Cmd {
	a.busy--
	rt, r := m.rt, m.rt.run
	r.done = true
	switch {
	case errors.Is(m.err, context.Canceled):
		a.log(r, r.sql, "已取消", false)
		return a.showToast("查询已取消", toastTTL)
	case m.err != nil:
		e := postgres.ServerErrorOf(m.err)
		a.log(r, r.sql, e.Severity+": "+e.Message, true)
		if p := r.win.Result; p != nil {
			selectTab(p, 0)
		}
		return nil
	}
	rt.gridState = gridState{page: m.res, transpose: rt.transpose}
	a.log(r, r.sql, resultText(m.res), false)
	return nil
}

// quickEdit is C-e (§12「C-e 在 console 中打开」): the SQL scope's text, the
// ; off, at the end of a new console in openTarget's pane, a blank line
// before it when there is text; one undo step, the cursor at its start in
// NORMAL; the palette goes, the console has the focus.
func (a *App) quickEdit() tea.Cmd {
	scope, sql := a.paletteScope()
	if scope != sqlScope || strings.TrimSpace(sql) == "" {
		return nil
	}
	a.palette = nil
	if cmd := a.newConsole(); cmd != nil { // its file can't be read
		return cmd
	}
	t := consoleOf(a.focused())
	pre := strings.Join(t.ed.Lines(), "\n")
	switch {
	case pre == "":
	case strings.HasSuffix(pre, "\n"): // a blank line last: that one goes before the SQL
		pre += "\n"
	default:
		pre += "\n\n"
	}
	cmd := a.consoleDid(t, t.ed.Load(pre+sql))
	t.ed.Click(strings.Count(pre, "\n"), 0)
	return cmd
}

// copyQuick puts the result on the clipboard as CSV (F-04):
// the header and the rows shown.
func (a *App) copyQuick() tea.Cmd {
	q := a.palette.quick
	if q == nil || q.res.Cols == nil {
		return nil
	}
	return clipCopy(csvOf(q.res))
}

// completeSQL finds the candidates for the quick SQL's cursor (§12「补全」),
// as a console's (sqlComplete).
func (a *App) completeSQL() tea.Cmd {
	p := a.palette
	p.comp = nil
	scope, sql := a.paletteScope()
	pos := p.input.Pos - len(scopes[scope].prefix) // none while the cursor is in the ; itself
	if scope != sqlScope || pos < 0 {
		return nil
	}
	if p.asked == nil {
		p.asked = map[tableID]bool{}
	}
	c, cmds := a.sqlComplete(sql, pos, a.sess.Schema, p.asked, false)
	if c != nil {
		c.start += len(scopes[scope].prefix)
	}
	p.comp = c
	return tea.Batch(cmds...)
}

// fetchCols reads table's columns on Meta for completion.
func (a *App) fetchCols(t db.Table) tea.Cmd {
	meta := a.sess.Meta
	return func() tea.Msg {
		cols, err := postgres.TableColumns(context.Background(), meta, t.Schema, t.Name)
		return colsMsg{t, cols, err}
	}
}

// gotCols caches what fetchCols read and completes again with it: the
// quick SQL, or the focused console, still in INSERT where it asked.
func (a *App) gotCols(m colsMsg) tea.Cmd {
	if m.err != nil {
		return nil
	}
	a.sess.cols[idOf(m.table)] = m.cols
	if a.palette != nil {
		return a.completeSQL()
	}
	if t := a.focusedConsole(); t != nil && t.wait != nil && t.wait.at == t.ed.Cursor() && t.wait.ver == t.ver {
		return a.consoleComplete(t, t.wait.manual) // in INSERT only
	}
	return nil
}
