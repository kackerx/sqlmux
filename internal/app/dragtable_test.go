package app

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/ui"
)

// A table's row dragged onto a query's text puts its name in there, the
// database first on PG; let go on its own row it opens, as a click (§7.8).
func TestDragTableToQuery(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.win().Root.Leaves()[0]
	qp := a.win().pane(2)
	q := queryOf(qp)
	q.ed.Load("select * from ")
	goal := slices.IndexFunc(func() []node { ns, _ := a.treeNodes(); return ns }(), func(n node) bool { return n.Text == "goal" })
	row := find(t, a, ui.Target{Kind: ui.KindNode, Pane: 0, I: goal})
	var text uv.Rectangle
	for _, h := range a.hits {
		if h.Target.Kind == ui.KindText && h.Target.Pane == qp.ID {
			text = h.Rect
		}
	}
	if text.Empty() {
		t.Fatal("no query text region")
	}
	drop := uv.Pos(text.Min.X+40, text.Min.Y) // past the line's end: after it

	a.Update(tea.MouseClickMsg{X: row.Min.X + 3, Y: row.Min.Y, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: drop.X, Y: drop.Y, Button: tea.MouseLeft})
	if !strings.Contains(a.render().String(), " doraemon.public.goal ") {
		t.Error("the name dragged is not drawn by the pointer")
	}
	a.Update(tea.MouseReleaseMsg{X: drop.X, Y: drop.Y, Button: tea.MouseLeft})
	if got := strings.Join(q.ed.Lines(), "\n"); got != "select * from doraemon.public.goal" {
		t.Errorf("query %q", got)
	}
	if a.win().Focus != qp.ID || tabNames(data) == "goal" {
		t.Errorf("focus %d, tabs %v: the query is focused, nothing opened", a.win().Focus, tabNames(data))
	}

	// dragged and let go off any query: nothing
	a.View()
	a.Update(tea.MouseClickMsg{X: row.Min.X + 3, Y: row.Min.Y, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: row.Min.X + 3, Y: row.Min.Y + 4, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: row.Min.X + 3, Y: row.Min.Y + 4, Button: tea.MouseLeft})
	if a.dragTable != nil || tabNames(data) == "goal" {
		t.Errorf("drag left %v, tabs %v", a.dragTable, tabNames(data))
	}
}

// The name dropped, as each engine has a query name a table.
func TestDropName(t *testing.T) {
	for _, c := range []struct{ engine, db, schema, name, want string }{
		{"postgres", "shop", "public", "orders", "shop.public.orders"},
		{"postgres", "shop", "Sales", "Order Lines", `shop."Sales"."Order Lines"`},
		{"mssql", "shop", "dbo", "orders", "shop.dbo.orders"},
		{"trino", "hive", "web", "events", "hive.web.events"},
		{"mysql", "shop", "shop", "orders", "shop.orders"},
		{"clickhouse", "default", "logs", "hits", "logs.hits"},
		{"oracle", "ORCL", "APP", "T_ORDER", "APP.T_ORDER"},
		{"sqlite", "", "main", "notes", "main.notes"},
	} {
		conn := newConn(config.Connection{Name: "x", Engine: c.engine})
		conn.DB = c.db
		if got := conn.dropName(db.Table{Schema: c.schema, Name: c.name}); got != c.want {
			t.Errorf("%s: %q, want %q", c.engine, got, c.want)
		}
	}
}
