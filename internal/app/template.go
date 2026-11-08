package app

import (
	"regexp"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// The structure changes (F14.4, §7.8, §12): no form, a statement of the
// engine's own that opens in a new query, a placeholder in it, <name> or
// <type>, selected for the user to write over; C-e runs it, with the same
// question a dangerous one gets.

// alterKind is which template: the actions' names are its.
type alterKind string

const (
	addColumn  alterKind = "add_column"
	addIndex   alterKind = "add_index"
	renameTbl  alterKind = "rename"
	dropColumn alterKind = "drop_column"
	dropIndex  alterKind = "drop_index"
)

// alterTarget is the table the templates are about, and the column or index
// the cursor is on, if it is on one: the tree node under its cursor when the
// tree has the keys, else the focused table tab's table and its cursor's
// column.
func (a *App) alterTarget() (c *Conn, t db.Table, col, index string, ok bool) {
	if p := a.focused(); p == a.win().Tree {
		n, found := a.treeNode()
		if !found || n.table.Name == "" {
			return nil, t, "", "", false
		}
		if n.kind == nodeIndex {
			index = n.index.Name
		}
		return n.conn, n.table, n.column, index, true
	}
	d := dataOf(a.focused())
	if d == nil {
		return nil, t, "", "", false
	}
	_, col, _ = a.cursorColumn()
	return d.conn, d.table, col, "", true
}

// alterSQL is the statement of kind for table t of an engine, a column or an index
// written in when the cursor was on one, else the placeholders <name>, <type>,
// <column>, <index>, <new_name>; "" says why not in why: a view has no column to add,
// SQLite does not rename one.
func alterSQL(c *Conn, kind alterKind, t db.Table, col, index string) (sql, why string) {
	q, d := c.eng.Quote, c.eng.Dialect()
	table := q(t.Schema, t.Name)
	name := func(known, placeholder string) string {
		if known != "" {
			return q(known)
		}
		return placeholder
	}
	if t.View() && kind != renameTbl {
		return "", i18n.T("%s 是视图", t.Name)
	}
	switch kind {
	case addColumn:
		if d == sqlkit.Oracle { // ADD takes no COLUMN there
			return "alter table " + table + " add (<name> <type>);", ""
		}
		return "alter table " + table + " add column <name> <type>;", ""
	case addIndex:
		switch d {
		case sqlkit.ClickHouse: // a data-skipping index, of the table (§8.9)
			return "alter table " + table + " add index <name> " + name(col, "<column>") + " type minmax granularity 1;", ""
		case sqlkit.Trino:
			return "", i18n.T("Trino 没有索引")
		}
		on := table
		if d == sqlkit.SQLite { // its ON takes a table with no schema
			on = q(t.Name)
		}
		return "create index <name> on " + on + " (" + name(col, "<column>") + ");", ""
	case renameTbl:
		switch {
		case d == sqlkit.MySQL, d == sqlkit.ClickHouse:
			return "rename table " + table + " to " + q(t.Schema) + ".<new_name>;", "" // in its own database, not the query's
		case d == sqlkit.SQLite && t.View():
			return "", i18n.T("SQLite 不能给视图改名")
		case d == sqlkit.Oracle && t.View(): // RENAME, of the user's own schema's alone
			return "rename " + q(t.Name) + " to <new_name>;", ""
		}
		object := "table"
		switch t.Kind {
		case "v":
			object = "view"
		case "m":
			object = "materialized view"
		}
		return "alter " + object + " " + table + " rename to <new_name>;", ""
	case dropColumn:
		return "alter table " + table + " drop column " + name(col, "<column>") + ";", ""
	case dropIndex:
		switch d {
		case sqlkit.MySQL, sqlkit.MSSQL: // T-SQL's is the ON form too (F18.6)
			return "drop index " + name(index, "<index>") + " on " + table + ";", ""
		case sqlkit.SQLite:
			return "drop index " + name(index, "<index>") + ";", ""
		case sqlkit.ClickHouse:
			return "alter table " + table + " drop index " + name(index, "<index>") + ";", ""
		case sqlkit.Trino:
			return "", i18n.T("Trino 没有索引")
		case sqlkit.PG:
		}
		return "drop index " + q(t.Schema) + "." + name(index, "<index>") + ";", ""
	}
	return "", ""
}

// alterTitles are the templates' titles, in the menu and the palette.
var alterTitles = map[alterKind]string{
	addColumn: i18n.N("加列…"), addIndex: i18n.N("加索引…"), renameTbl: i18n.N("改名…"),
	dropColumn: i18n.N("删除列…"), dropIndex: i18n.N("删除索引…"),
}

// alterIcons are their icons in the menu (§7.6).
var alterIcons = map[alterKind]string{addColumn: "column", addIndex: "index", renameTbl: "rename", dropColumn: "hide", dropIndex: "trash"}

// alter is the action of kind: its statement in a new query on the table's
// connection, the first placeholder selected (F14.4).
func (a *App) alter(kind alterKind) tea.Cmd {
	c, t, col, index, ok := a.alterTarget()
	if !ok {
		return a.showToast(i18n.T("先选一张表"))
	}
	if c.State != connReady {
		return a.showToast(i18n.T("连接还没连上"))
	}
	sql, why := alterSQL(c, kind, t, col, index)
	if why != "" {
		return a.showToast(why)
	}
	return a.queryWith(c, t.Schema, sql, true)
}

var placeholder = regexp.MustCompile(`<[a-z_]+>`)

// selectPlaceholder makes the statement of query t a snippet, its placeholders
// the tabstops: the first is selected, and what is typed takes its place, Tab
// goes on to the next (F15.8).
func selectPlaceholder(t *queryTab) {
	var stops []editor.Range
	lines := t.ed.Lines()
	for i, l := range lines {
		for _, loc := range placeholder.FindAllStringIndex(l, -1) {
			stops = append(stops, editor.Range{From: editor.Pos{Line: i, Col: loc[0]}, To: editor.Pos{Line: i, Col: loc[1]}})
		}
	}
	t.ed.Snippet(editor.Pos{}, editor.Pos{Line: len(lines) - 1, Col: len(lines[len(lines)-1])}, stops)
}

// alterItems are the table node's menu items for the templates; those alterSQL
// says no to are left out, so a view's is only the rename (F14.4).
func (a *App) alterItems(n node) (items []menuItem) {
	for _, k := range []alterKind{addColumn, addIndex, renameTbl, dropColumn, dropIndex} {
		if _, why := alterSQL(n.conn, k, n.table, "", ""); why == "" {
			items = append(items, menuItem{i18n.T(alterTitles[k]), alterIcons[k], func() tea.Cmd { return a.alter(k) }})
		}
	}
	return items
}
