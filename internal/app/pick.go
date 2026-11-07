package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// pickMenu is the list of F17.13, in the COLS list's place and with its keys: the
// databases of a PG connection, or the schemas of a database (a MySQL connection's
// are its databases), to show and load; the one the DSN names is always on. The
// list takes effect when it closes with a change: connections.toml says it, the
// catalog is read again.
type pickMenu struct {
	conn    *Conn // the connection the names are of: the root for databases, the database's for schemas
	dbs     bool
	names   []string
	on, was []bool
	fixed   string         // the DSN's database, which stays on
	tables  map[string]int // schemas: the tables loaded of each
}

// set puts name i on or off; the DSN's database cannot go off.
func (p *pickMenu) set(i int, on bool) {
	if p.names[i] != p.fixed {
		p.on[i] = on
	}
}

// note is what ends a schema's row: how many tables of it are loaded, for one that is.
func (p *pickMenu) note(i int) string {
	if p.dbs || !p.was[i] {
		return ""
	}
	return strconv.Itoa(p.tables[p.names[i]])
}

// pickable is whether n has a list of databases or schemas to pick from, and which:
// a PG, SQL Server or Trino connection its databases, a MySQL or ClickHouse one and
// a database of the others its schemas.
func pickable(n node) (dbs, ok bool) {
	switch {
	case n.kind == nodeConn && n.conn.Spec.SchemasAreDatabases():
		return false, true
	case n.kind == nodeConn && n.conn.Spec.HasDatabases():
		return true, true
	case n.kind == nodeDatabase:
		return false, n.conn.Spec.HasDatabases()
	}
	return false, false
}

// pickItem is the menu's entry on n, none for a node that has nothing to pick.
func (a *App) pickItem(n node) []menuItem {
	dbs, ok := pickable(n)
	if !ok {
		return nil
	}
	label := i18n.T("选择 schema…")
	if dbs {
		label = i18n.T("选择库…")
	}
	return []menuItem{{label, "database", func() tea.Cmd { return a.openPick(n) }}}
}

// openPick is tree.pick on n: the list of its databases or schemas, opened on a
// connection that has read them.
func (a *App) openPick(n node) tea.Cmd {
	dbs, ok := pickable(n)
	if !ok {
		return a.showToast(i18n.T("这个节点没有可选的库或 schema"))
	}
	c := n.conn
	if dbs {
		c = c.root()
	}
	if c.State != connReady || len(c.Schemas) == 0 && len(c.all.schemas) == 0 && len(c.Databases) == 0 {
		return a.showToast(i18n.T("先展开它，读到目录之后才能选"))
	}
	p := &pickMenu{conn: c, dbs: dbs, tables: map[string]int{}}
	names, loaded := cmpOr(c.all.schemas, c.Schemas), c.Schemas
	if dbs {
		names, loaded, p.fixed = cmpOr(c.all.dbs, c.Databases), c.Databases, c.DB
	}
	p.names = slices.Clone(names)
	if !dbs && slices.Contains(p.names, c.all.keep) { // the schema names go to stays picked, as the database does
		p.fixed = c.all.keep
	}
	for _, t := range c.Tables {
		p.tables[t.Schema]++
	}
	for _, name := range p.names {
		p.on = append(p.on, slices.Contains(loaded, name) || name == p.fixed)
	}
	p.was = slices.Clone(p.on)
	if len(p.names) == 0 {
		return a.showToast(i18n.T("没有可选的"))
	}
	a.cols = &colsMenu{pane: a.focused(), pick: p}
	return nil
}

// cmpOr is the first of two lists that is not empty.
func cmpOr(x, y []string) []string {
	if len(x) > 0 {
		return x
	}
	return y
}

// pickView is the list as drawn: its title, how many are on of all, each name with its
// box and, for a schema loaded, its tables.
func (a *App) pickView() ui.Dropdown {
	m := a.cols
	p := m.pick
	title := "schema"
	if p.dbs {
		title = i18n.T("库")
	}
	on := 0
	for _, b := range p.on {
		if b {
			on++
		}
	}
	v := ui.Dropdown{
		Title:  title,
		Search: a.icons.Filter, Input: m.filter, Typing: m.typing, Mark: -1, Sel: m.sel, Top: m.top,
		Pane:  m.pane.ID,
		Count: fmt.Sprintf("%d/%d", on, len(p.names)),
		Hints: bound(
			ui.Hint{Key: a.keys.Hint("cols.all", "cols"), Label: i18n.T("全选"), Action: "cols.all"},
			ui.Hint{Key: a.keys.Hint("cols.none", "cols"), Label: i18n.T("全不选"), Action: "cols.none"},
		),
	}
	for _, mt := range a.colsMatches() {
		v.Items, v.Pos = append(v.Items, p.names[mt.Index]), append(v.Pos, mt.Pos)
		v.Notes = append(v.Notes, p.note(mt.Index))
		v.Checks = append(v.Checks, p.on[mt.Index])
	}
	return v
}

// applyPick is what the list does as it closes: nothing when nothing changed; else the
// connection's databases or schemas are written into connections.toml (all of them
// on is no line at all), every connection of the name takes them, and the catalog is
// read again, the whole connection's for its databases, the database's for schemas.
func (a *App) applyPick(p *pickMenu) tea.Cmd {
	if slices.Equal(p.on, p.was) {
		return nil
	}
	var picked []string
	for i, b := range p.on {
		if b {
			picked = append(picked, p.names[i])
		}
	}
	if len(picked) == 0 { // none to keep: nothing is written
		return nil
	}
	all := len(picked) == len(p.names)
	root := p.conn.root()
	base := root.Spec
	if !root.Spec.Temp { // the file may have changed since: its lists are what this one changes
		var err error
		if base, err = config.ReadConnection(root.Spec.Name); err != nil {
			return a.showToast(err.Error())
		}
	}
	next := base
	switch {
	case p.dbs:
		next.Databases = nil
		if !all {
			next.Databases = picked
		}
	case root.Spec.SchemasAreDatabases():
		next.Schemas = nil
		if !all {
			next.Schemas = picked
		}
	default: // PG: db.schema, the other databases' entries as they are
		prefix := p.conn.DB + "."
		next.Schemas = slices.DeleteFunc(slices.Clone(base.Schemas), func(e string) bool { return strings.HasPrefix(e, prefix) })
		if !all {
			for _, s := range picked {
				next.Schemas = append(next.Schemas, prefix+s)
			}
		}
	}
	if !root.Spec.Temp {
		saved, err := config.UpdateConnection(base.Name, base, next)
		if err != nil {
			return a.showToast(err.Error())
		}
		next = saved
	}
	for _, s := range a.sessions {
		for _, c := range s.conns() {
			if c.Spec.Name == root.Spec.Name {
				spec := c.Spec
				spec.Databases, spec.Schemas = next.Databases, next.Schemas
				c.setConfig(spec)
			}
		}
	}
	p.conn.dropCols()
	return a.loadCatalog(p.conn)
}
