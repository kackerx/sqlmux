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
	"sqlmux/internal/ui"
)

// quickSQL is the palette's quick SQL (§12): the statement whose result
// shows, and the one out on Meta.
type quickSQL struct {
	ran, running string // "" for none; running is the SQL scope's input as ↵ ran it
	res          db.Result
	err          string
	top, left    int // the result's scroll
}

// quickRows is how many rows a quick SQL shows (§12).
const quickRows = 100

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
	p.quick.running, p.comp = sql, nil
	if a.state.SQL == nil {
		a.state.SQL = map[string][]string{}
	}
	h := slices.Insert(slices.DeleteFunc(a.state.SQL[a.sess.Name], func(s string) bool { return s == sql }), 0, sql)
	a.state.SQL[a.sess.Name] = h[:min(len(h), historyRows)]
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
	q.ran, q.res, q.err, q.top, q.left = sql, m.res, "", 0, 0
	if m.err != nil {
		q.res, q.err = db.Result{Took: m.res.Took}, m.err.Error()
	}
	return nil
}

// quickView is the result area: "100+ 行 · 12ms · 只读" over the table, or
// the database's error.
func (a *App) quickView(q *quickSQL) *ui.PaletteResult {
	var title []string
	switch {
	case q.running != "":
		title = append(title, "… 行")
	case q.err != "":
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
	if q.res.Cols != nil { // rows to copy
		r.Hints = bound(ui.Hint{Key: a.keys.Hint("quicksql.copy", "palette"), Label: "CSV", Action: "quicksql.copy"})
	}
	return r
}

// copyQuick puts the result on the clipboard over OSC 52 as CSV (F-04):
// the header and the rows shown.
func (a *App) copyQuick() tea.Cmd {
	q := a.palette.quick
	if q == nil || q.res.Cols == nil {
		return nil
	}
	return tea.SetClipboard(csvOf(q.res))
}

// completeSQL finds the candidates for the quick SQL's cursor (§12「补全」),
// as a console's (sqlComplete).
func (a *App) completeSQL() tea.Cmd {
	p := a.palette
	p.comp = nil
	scope, sql := a.paletteScope()
	pos := p.input.Pos - len(scopes[scope].prefix) // none while the cursor is in the ; itself
	if scope != sqlScope || p.pick != nil || pos < 0 {
		return nil
	}
	if p.asked == nil {
		p.asked = map[tableID]bool{}
	}
	c, cmds := a.sqlComplete(sql, pos, p.asked, false)
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
