package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// A table dragged from the tree into a query (§7.8「拖放表名」), as DBeaver
// drops one in its SQL editor: pressed on a table's row, the open waits for
// the release; let go on that row it opens, as the click did; dragged and let
// go on a query's text, the table's name goes in where it was let go.

// tableDrag is a table's row pressed on, the button still down.
type tableDrag struct {
	at    uv.Position // where it was pressed
	conn  *Conn
	table db.Table
	moved bool // off the row it was pressed on: a drag, not a click
}

// startTableDrag holds the open of node n, pressed at p, for the release.
func (a *App) startTableDrag(n node, p uv.Position) {
	a.dragTable = &tableDrag{at: p, conn: n.conn, table: n.table}
}

// moveTableDrag follows the pointer: a move off the row, or along it past
// a slip of the hand, makes the press a drag.
func (a *App) moveTableDrag(p uv.Position) {
	d := a.dragTable
	d.moved = d.moved || p.Y != d.at.Y || p.X < d.at.X-2 || p.X > d.at.X+2
}

// endTableDrag is the release at p: the table opened when it did not move,
// its name put in a query's text it was let go on, else nothing.
func (a *App) endTableDrag(p uv.Position) tea.Cmd {
	d := a.dragTable
	a.dragTable = nil
	if !d.moved {
		return a.openTable(d.conn, d.table)
	}
	t, _ := ui.HitAt(a.hits, p)
	if t.Kind != ui.KindText || t.Pane == ui.ValuePane || a.overlaid() {
		return nil
	}
	q, n, v := a.queryAt(t.Pane, p)
	if q == nil {
		return nil
	}
	a.focusPane(t.Pane)
	q.ed.Click(n, v)
	return a.queryDid(q, q.ed.Paste(d.conn.dropName(d.table)))
}

// dropName is table t of c as a dropped table names it, DBeaver's way: the
// database, or Trino's catalog, first where the engine has a level over the
// schema a query can name (PG's, SQL Server's, Trino's), then the schema and
// the table, each quoted where it must be. MySQL's and ClickHouse's schema is
// their database already; Oracle's and SQLite's have none over it.
func (c *Conn) dropName(t db.Table) string {
	parts := []string{t.Schema, t.Name}
	if slices.Contains([]string{"postgres", "mssql", "trino"}, c.Spec.Engine) {
		parts = append([]string{c.DB}, parts...)
	}
	parts = slices.DeleteFunc(parts, func(s string) bool { return s == "" })
	for i, s := range parts {
		parts[i] = c.ident(s)
	}
	return strings.Join(parts, ".")
}

// drawTableDrag paints the name being dragged beside the pointer.
func (a *App) drawTableDrag(f *ui.Frame) {
	d := a.dragTable
	if d == nil || !d.moved {
		return
	}
	s := " " + d.conn.dropName(d.table) + " "
	f.Text(max(min(a.mouse.X+2, a.w-ui.Width(s)), 0), a.mouse.Y, a.w, s, uv.Style{Fg: a.theme.Focus, Bg: a.theme.Bar})
}
