package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// quickSQL is the palette's quick SQL (§12): the statement whose result
// shows, and the one out on Meta.
type quickSQL struct {
	ran, running string // "" for none; running is the SQL scope's input as ↵ ran it
	conn         *Conn  // where ran ran, schema first on its search_path: C-t's tab runs again there (§12)
	schema       string
	res          db.Result
	err          string
	write        bool // ran is a write, not run: a query runs it (F-05)
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
	p      *palette
	conn   *Conn
	schema string
	res    db.Result
	err    error
}

// colsMsg brings the columns of a table the quick SQL names (§12「补全」).
type colsMsg struct {
	conn  *Conn
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

// runQuick runs sql on the current connection's Meta for the palette
// (§12), unless one is running already, and puts it at the top of the
// connection's history.
func (a *App) runQuick(sql string) tea.Cmd {
	p := a.palette
	if p.conn == nil { // a start with no connection (F7.4)
		return a.noConnection()
	}
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
	c := p.conn
	if !c.sql() && !c.redis() {
		return a.showToast(i18n.T("这个连接没有快速 SQL"))
	}
	if c.State != connReady { // a layout's other session, still connecting (F6.5)
		return a.notReady(c)
	}
	if !c.sql() && !c.cmdsReady { // what writes and what is dangerous is not known yet (§13)
		return a.showToast(i18n.T("还在读 %s 的命令列表", c.label()))
	}
	if !c.sql() && len(c.lang().Stmts(sql)) == 0 { // a blank line, a comment
		return nil
	}
	if c.sql() || !redis.Sensitive(sql) { // a password is not kept (§9.8)
		h := slices.Insert(slices.DeleteFunc(a.state.SQL[c.Spec.Name], func(s string) bool { return s == sql }), 0, sql)
		a.state.SQL[c.Spec.Name] = h[:min(len(h), quickHistory)]
	}
	if c.sql() {
		if q := p.quick; sqlkit.IsWrite(sql, c.eng.Dialect()) { // a write is for a query, where running it is the user's own ↵ (F-05)
			q.ran, q.res, q.err, q.write, q.top, q.left = sql, db.Result{}, "", true, 0, 0
			return a.saveState()
		}
		return tea.Batch(a.saveState(), a.sendQuick(c, sql))
	}
	return tea.Batch(a.saveState(), a.redisQuick(c, sql)) // a command goes, as the console's does (F17.2)
}

// redisQuick is a command typed in the quick SQL of a Redis connection: it goes
// as a console's line does, kept back on a read-only connection when it writes,
// asked about when it is dangerous (§13).
func (a *App) redisQuick(c *Conn, sql string) tea.Cmd {
	q, l := a.palette.quick, c.lang()
	if c.readOnly && l.Writes(sql) {
		q.ran, q.res, q.err, q.write, q.top, q.left = sql, db.Result{}, i18n.T("%s 是只读连接", c.Spec.Name), false, 0, 0
		return nil
	}
	if (a.confirmDangerous && l.Dangerous(sql)) || (c.Spec.Production && l.Writes(sql)) { // a production connection asks about every write (F17.9)
		a.confirm = writeBox(c, dangerText([]string{sql}, c.Spec.Production), i18n.T("执行"), true, func() tea.Cmd { return a.sendQuick(c, sql) })
		return nil
	}
	return a.sendQuick(c, sql)
}

// quickOn is how c runs the statement of a quick SQL, with what it needs read
// now, for a goroutine: a SQL connection's Meta, in schema, read only; a Redis
// connection's Main, as the console's line goes, the reply cut to the rows
// shown.
func quickOn(c *Conn, schema string) func(sql string) (db.Result, error) {
	meta, eng := c.Meta, c.eng
	if c.sql() {
		return func(sql string) (db.Result, error) {
			return eng.Quick(context.Background(), meta, sql, schema, quickRows)
		}
	}
	main, l := c.Main, c.lang()
	return func(sql string) (res db.Result, err error) {
		err = mainRunner(main, l, sql)(context.Background(), func(ctx context.Context, conn db.Conn) error {
			rs, err := db.ExecEach(ctx, conn, []string{sql}, 0, nil)
			if len(rs) > 0 {
				res = rs[0]
			}
			return err
		})
		if len(res.Rows) > quickRows {
			res.Rows, res.Truncated = res.Rows[:quickRows], true
		}
		return res, err
	}
}

// sendQuick sends sql, which is to go, for the palette's quick SQL.
func (a *App) sendQuick(c *Conn, sql string) tea.Cmd {
	p := a.palette
	p.quick.running = sql
	c.busy++
	schema := c.Schema
	run := quickOn(c, schema)
	return func() tea.Msg {
		r, err := run(sql)
		return quickMsg{p, c, schema, r, err}
	}
}

// gotQuick shows a quick SQL's answer, if its palette is still open: the
// result goes with the palette (§12). Cancelled, the last result stays.
func (a *App) gotQuick(m quickMsg) tea.Cmd {
	m.conn.busy--
	q := m.p.quick
	sql := q.running
	q.running = ""
	canceled := errors.Is(m.err, context.Canceled)
	if !m.conn.sql() && !canceled { // it ran, whether or not the palette is still there to see it
		errText := ""
		if m.err != nil {
			errText = m.err.Error()
		}
		recordHistory(&run{conn: m.conn}, sql, m.res, errText) // a console's line is in the history; a SQL one is not (§9.8)
		m.conn.forgetHints(sql)
	}
	if m.p != a.palette {
		return nil
	}
	if canceled {
		return a.showToast(i18n.T("查询已取消"))
	}
	q.ran, q.res, q.err, q.write, q.top, q.left = sql, m.res, "", false, 0, 0
	q.conn, q.schema = m.conn, m.schema
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
		title = append(title, i18n.T("… 行"))
	case q.err != "", q.write:
	case q.res.Cols == nil: // no rows to show: what it did
		title = append(title, q.res.Tag)
	case q.res.Truncated:
		title = append(title, fmt.Sprintf(i18n.T("%d+ 行"), len(q.res.Rows)))
	default:
		title = append(title, fmt.Sprintf(i18n.T("%d 行"), len(q.res.Rows)))
	}
	if q.running == "" && q.res.Took > 0 {
		title = append(title, q.res.Took.Round(time.Millisecond).String())
	}
	r := &ui.PaletteResult{
		Title: strings.Join(append(title, i18n.T("只读")), " · "),
		Err:   q.err,
		Grid:  ui.Grid{Rows: q.res.Rows, Row: -1, Col: -1, Top: q.top, Left: q.left, FullWidth: a.fullWidth, Key: a.icons.Key},
	}
	for _, c := range q.res.Cols {
		r.Grid.Cols = append(r.Grid.Cols, ui.GridCol{Name: c.Name, Type: colType(c.Type)})
	}
	edit := ui.Hint{Key: a.keys.Hint("quicksql.edit", "palette"), Label: "query", Action: "quicksql.edit"}
	if q.write {
		r.Err, r.Warn = i18n.T("写语句不在这里执行"), true
		if edit.Key != "" {
			r.Err += " · " + i18n.T("%s 在 query 中打开", edit.Key)
		}
	}
	if q.res.Cols != nil { // rows to copy, or to put in the result area
		r.Hints = append(r.Hints,
			ui.Hint{Key: a.keys.Hint("quicksql.copy", "palette"), Label: "CSV", Action: "quicksql.copy"},
			ui.Hint{Key: a.keys.Hint("palette.open.tab", "palette"), Label: i18n.T("结果区"), Action: "palette.open.tab"})
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
	r := &run{conn: q.conn, schema: q.schema, name: "quick", sql: q.ran, seq: a.sess.RunSeq, start: time.Now(), win: a.win(), done: true}
	p := a.showResult(r.win)
	a.log(r, r.sql, resultText(q.res, 0), false)
	p.Tabs = append(p.Tabs, Tab{Name: r.label(1), Result: &resultTab{gridState: gridState{page: q.res}, run: r, part: 1, pinned: true, arrived: r.start}})
	selectTab(p, len(p.Tabs)-1)
	a.capResults(p)
	return nil
}

// quickRerun answers rerunQuick.
type quickRerun struct {
	rt  *resultTab
	res db.Result
	err error
}

// rerunQuick is R on a quick SQL's tab (§12): its SQL again as the palette
// ran it, on the same connection's Meta, read only, the same schema first;
// the rows in the tab's place, a line in the log.
func (a *App) rerunQuick(rt *resultTab) tea.Cmd {
	r := rt.run
	if !r.done {
		return nil
	}
	if !r.conn.sql() && !r.conn.lang().Reruns(r.sql) { // a command with an effect is not sent again by an R (§8.7)
		return a.showToast(i18n.T("这条命令不是只读的，不重跑"))
	}
	r.done, r.start = false, time.Now()
	r.conn.busy++
	sql, quick := r.sql, quickOn(r.conn, r.schema)
	return func() tea.Msg {
		res, err := quick(sql)
		return quickRerun{rt, res, err}
	}
}

// gotQuickRerun shows what rerunQuick got: the rows, or a failure in the
// log, which shows, the rows as they were; a cancel says so (§11).
func (a *App) gotQuickRerun(m quickRerun) tea.Cmd {
	rt, r := m.rt, m.rt.run
	r.conn.busy--
	r.done = true
	switch {
	case errors.Is(m.err, context.Canceled):
		a.log(r, r.sql, i18n.T("已取消"), false)
		return a.showToast(i18n.T("查询已取消"))
	case m.err != nil:
		a.logError(r, r.sql, m.err)
		selectTab(r.win.Result, 0) // the log's, made if need be
		return nil
	}
	rt.gridState, rt.arrived = gridState{page: m.res, transpose: rt.transpose, jv: rt.jv}, time.Now()
	a.log(r, r.sql, resultText(m.res, 0), false)
	return nil
}

// quickEdit is C-e (§12「C-e 在 query 中打开」): the SQL scope's text, the
// ; off, in a new query in openTarget's pane: one undo step, the cursor at
// its start in NORMAL; the palette goes, the query has the focus. A new
// query is empty (F8.21).
func (a *App) quickEdit() tea.Cmd {
	scope, sql := a.paletteScope()
	if scope != sqlScope || strings.TrimSpace(sql) == "" {
		return nil
	}
	if a.palette.conn == nil {
		return a.noConnection()
	}
	kept, ok := a.newQuery(a.palette.conn)
	if !ok { // its file can't be read: the palette stays, the SQL in it
		return kept
	}
	a.palette = nil
	t := queryOf(a.focused())
	cmd := a.queryDid(t, t.ed.Load(sql))
	return tea.Batch(kept, cmd)
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
// as a query's (sqlComplete).
func (a *App) completeSQL() tea.Cmd {
	p := a.palette
	p.comp = nil
	scope, sql := a.paletteScope()
	pos := p.input.Pos - len(scopes[scope].prefix) // none while the cursor is in the ; itself
	if scope != sqlScope || pos < 0 || p.conn == nil {
		return nil
	}
	if p.asked == nil {
		p.asked = map[tableID]bool{}
	}
	c := p.conn
	var comp *completion
	var cmds []tea.Cmd
	if c.sql() {
		comp, cmds = a.sqlComplete(c, sql, pos, c.Schema, p.asked, false)
	} else { // a Redis command's (F17.2): the names the server gives are asked for by a tab of no place
		if p.hint == nil || p.hint.conn != c {
			p.hint = &queryTab{conn: c}
		}
		comp, cmds = a.redisComplete(p.hint, sql[:pos], pos, false)
	}
	if comp != nil {
		comp.start += len(scopes[scope].prefix)
	}
	p.comp = comp
	return tea.Batch(cmds...)
}

// fetchCols reads the columns of c's table t on its Meta for completion.
func (a *App) fetchCols(c *Conn, t db.Table) tea.Cmd {
	meta, eng := c.Meta, c.eng
	return func() tea.Msg {
		cols, err := eng.TableColumns(context.Background(), meta, t.Schema, t.Name)
		return colsMsg{c, t, cols, err}
	}
}

// gotCols caches what fetchCols read and completes again with it: the
// quick SQL, or the focused query, still in INSERT where it asked.
func (a *App) gotCols(m colsMsg) tea.Cmd {
	if m.err != nil {
		return nil
	}
	m.conn.cols[idOf(m.table)] = m.cols
	old := a.completing()
	var cmd tea.Cmd
	if a.palette != nil {
		cmd = a.completeSQL()
	} else if t := a.focusedQuery(); t != nil && t.wait != nil && t.wait.at == t.ed.Cursor() && t.wait.ver == t.ver {
		cmd = a.queryComplete(t, t.wait.manual) // in INSERT only
	}
	if now := a.completing(); old != nil && now != nil && old.moved { // what was picked stays picked when its candidate is still there
		if i := slices.IndexFunc(now.items, func(c candidate) bool { return c.label == old.items[old.sel].label }); i >= 0 {
			now.sel, now.moved = i, true
		}
	}
	return cmd
}
