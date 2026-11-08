package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// A table's properties (§7.8「属性」), as DBeaver's Properties tab has them: a
// table tab whose rows are not the table's but what the catalog says of it, a
// section at a time, the sections chips over the filter as a key's Info row is
// (§8.7). Being a table tab, it has the grid's moves, search, JSON view and
// export; it is read only, and its filter is plain text over the section's rows.

// propsSections are the sections, in their order, by the names the actions take.
var propsSections = []string{"columns", "keys", "foreign_keys", "indexes", "ddl"}

// propsTitles are the sections as their chips say them.
var propsTitles = map[string]string{"columns": i18n.N("列"), "keys": i18n.N("主键与唯一键"), "foreign_keys": i18n.N("外键"), "indexes": i18n.N("索引"), "ddl": "DDL"}

// propsReadOnly is why a properties tab takes no edit and no save.
var propsReadOnly = i18n.N("属性是只读的")

// propsSource is the rows of section of a table's properties, read on its
// connection's Meta once: a page, its count and its columns are asked of it
// apart, and Oracle's DDL takes seconds to make. A section switched to, and R,
// make a source anew (newProps), which reads again.
type propsSource struct {
	conn    *Conn
	table   db.Table
	section string
	memo    *propsMemo
}

// propsMemo is what a propsSource read, kept for the asks after the first; the
// page and the count ask at once, from goroutines of their own.
type propsMemo struct {
	sync.Mutex
	done bool
	cols []db.Col
	rows [][]db.Val
	err  error
}

func newProps(c *Conn, t db.Table, section string) propsSource {
	return propsSource{conn: c, table: t, section: section, memo: &propsMemo{}}
}

// rows is the section's columns and rows, all of them, read the first time.
func (s propsSource) rows(ctx context.Context) ([]db.Col, [][]db.Val, error) {
	m := s.memo
	m.Lock()
	defer m.Unlock()
	if !m.done {
		m.cols, m.rows, m.err = s.read(ctx)
		m.done = m.err == nil // an error, a cancel among them, is asked again
	}
	return m.cols, m.rows, m.err
}

// read reads the section from the catalog.
func (s propsSource) read(ctx context.Context) ([]db.Col, [][]db.Val, error) {
	e, w, t := s.conn.eng, s.conn.Meta, s.table
	text := func(ss ...string) []db.Val {
		out := make([]db.Val, len(ss))
		for i, v := range ss {
			out[i] = db.Val{S: v}
		}
		return out
	}
	head := func(names ...string) []db.Col {
		cols := make([]db.Col, len(names))
		for i, n := range names {
			cols[i] = db.Col{Name: n, Type: "text"}
		}
		return cols
	}
	switch s.section {
	case "columns":
		cols, err := e.TableColumns(ctx, w, t.Schema, t.Name)
		if err != nil {
			return nil, nil, err
		}
		var rows [][]db.Val
		for i, c := range cols.Cols {
			notNull, key := "", ""
			if c.NotNull {
				notNull = "✓"
			}
			switch {
			case slices.Contains(cols.PK, c.Name):
				key = "PK"
			case slices.ContainsFunc(cols.Unique, func(u []string) bool { return slices.Contains(u, c.Name) }):
				key = "UNIQUE"
			}
			note := c.ReadOnly
			if note == "" && c.Generated {
				note = i18n.T("服务器生成")
			}
			rows = append(rows, text(strconv.Itoa(i+1), c.Name, c.Type, notNull, c.Default, key, strings.Join(c.Enum, ", "), note))
		}
		return head("#", "name", "type", "not_null", "default", "key", "values", "note"), rows, nil
	case "keys", "foreign_keys":
		keys, err := e.Keys(ctx, w, t.Schema, t.Name)
		if err != nil {
			return nil, nil, err
		}
		var rows [][]db.Val
		for _, k := range keys {
			switch {
			case s.section == "keys" && k.Ref == nil:
				rows = append(rows, text(k.Name, "PRIMARY KEY", strings.Join(k.Cols, ", ")))
			case s.section == "foreign_keys" && k.Ref != nil:
				ref := k.Ref.Table
				if k.Ref.Schema != "" && k.Ref.Schema != t.Schema {
					ref = k.Ref.Schema + "." + ref
				}
				rows = append(rows, text(k.Name, strings.Join(k.Cols, ", "), ref, strings.Join(k.Ref.Cols, ", ")))
			}
		}
		if s.section == "foreign_keys" {
			return head("name", "columns", "references", "referenced_columns"), rows, nil
		}
		if cols, err := e.TableColumns(ctx, w, t.Schema, t.Name); err == nil { // the unique keys over NOT NULL columns, which stand in for a primary key (§10.1)
			for _, u := range cols.Unique {
				rows = append(rows, text("", "UNIQUE", strings.Join(u, ", ")))
			}
		}
		return head("name", "kind", "columns"), rows, nil
	case "indexes":
		ixs, err := e.Indexes(ctx, w, t.Schema, t.Name)
		if err != nil {
			return nil, nil, err
		}
		var rows [][]db.Val
		for _, ix := range ixs {
			unique := ""
			if ix.Unique {
				unique = "✓"
			}
			rows = append(rows, text(ix.Name, unique, strings.Join(ix.Cols, ", "), ix.Def))
		}
		return head("name", "unique", "columns", "definition"), rows, nil
	case "ddl":
		ddl, err := e.DDL(ctx, w, t.Schema, t.Name)
		if err != nil {
			return nil, nil, err
		}
		var rows [][]db.Val
		for _, line := range strings.Split(strings.TrimRight(ddl, "\n"), "\n") {
			rows = append(rows, text(line))
		}
		return head("ddl"), rows, nil
	}
	return nil, nil, nil
}

// kept is rows the filter keeps: those with a cell that holds it, in any case.
func kept(rows [][]db.Val, filter string) [][]db.Val {
	f := strings.ToLower(strings.TrimSpace(filter))
	if f == "" {
		return rows
	}
	return slices.DeleteFunc(slices.Clone(rows), func(r []db.Val) bool {
		return !slices.ContainsFunc(r, func(v db.Val) bool { return strings.Contains(strings.ToLower(v.S), f) })
	})
}

func (s propsSource) Columns(ctx context.Context) (db.Columns, error) {
	cols, _, err := s.rows(ctx)
	why := i18n.T(propsReadOnly)
	out := db.Columns{ReadOnly: why, NoInsert: why, NoUpdate: why, Sortable: []string{}}
	for _, c := range cols {
		out.Cols = append(out.Cols, db.Column{Name: c.Name, Type: c.Type})
	}
	return out, err
}

func (s propsSource) Page(ctx context.Context, q db.PageQuery) (db.Paged, error) {
	cols, rows, err := s.rows(ctx)
	rows = kept(rows, q.Filter)
	from := min(q.Offset, len(rows))
	to := len(rows)
	if q.Limit > 0 {
		to = min(from+q.Limit, len(rows))
	}
	return db.Paged{Result: db.Result{Cols: cols, Rows: rows[from:to], Tag: "SELECT " + strconv.Itoa(to-from)}, Next: to < len(rows)}, err
}

func (s propsSource) Count(ctx context.Context, filter string) (int64, error) {
	_, rows, err := s.rows(ctx)
	return int64(len(kept(rows, filter))), err
}

func (s propsSource) Each(ctx context.Context, q db.PageQuery, head func([]db.Col) error, row func([]db.Val) error) error {
	cols, rows, err := s.rows(ctx)
	if err != nil {
		return err
	}
	if err := head(cols); err != nil {
		return err
	}
	for _, r := range kept(rows, q.Filter) {
		if err := row(r); err != nil {
			return err
		}
	}
	return nil
}

func (propsSource) Save(context.Context, db.Changes, bool) (int, byte, error) {
	return -1, 0, errors.New(i18n.T(propsReadOnly))
}

func (propsSource) Preview(db.Changes) []string { return nil }

// openProps is a table's properties in a tab of the pane tables open in, the
// one there is if it is open already, on its columns at first.
func (a *App) openProps(c *Conn, t db.Table) tea.Cmd {
	if c == nil || !c.sql() {
		return a.showToast(i18n.T("这个连接没有表的属性"))
	}
	p := a.openTarget()
	a.showPane(p.ID)
	if i := slices.IndexFunc(p.Tabs, func(tb Tab) bool {
		return tb.Data != nil && tb.Data.props != "" && tb.Data.conn == c && idOf(tb.Data.table) == idOf(t)
	}); i >= 0 {
		selectTab(p, i)
		return nil
	}
	d := newDataTab(c, t)
	d.props = "columns"
	d.src = newProps(c, t, d.props)
	putTab(p, Tab{Name: i18n.T("%s 属性", c.tabName(t)), Data: d}, p.tab().landing())
	return a.fetch(d, true)
}

// propsSection is a chip of the section row: t shows section name, from its
// first row, the filter kept.
func (a *App) propsSection(t *dataTab, name string) tea.Cmd {
	if t == nil || t.props == "" || !slices.Contains(propsSections, name) || name == t.props {
		return nil
	}
	t.props = name
	t.src = newProps(t.conn, t.table, name)
	t.row, t.col, t.top, t.left = 0, 0, 0, 0
	r := t.request
	r.pageNo, r.order, r.desc = 0, nil, false
	t.setRequest(r)
	return a.fetch(t, true)
}

// propsInfo is the row over a properties tab's filter: a chip for each section,
// the one shown marked.
func (a *App) propsInfo(t *dataTab) []ui.Chip {
	var chips []ui.Chip
	for _, s := range propsSections {
		c := ui.Chip{Label: i18n.T(propsTitles[s]), Action: "props.section " + s}
		if s == t.props {
			c.Label = "▸ " + c.Label
		}
		chips = append(chips, c)
	}
	return chips
}

// propsTarget is the table properties.open is about: the tree node's under the
// cursor when the tree has the keys, else the focused table tab's.
func (a *App) propsTarget() (*Conn, db.Table, bool) {
	if a.focused() == a.win().Tree {
		if n, ok := a.treeNode(); ok && n.table.Name != "" && n.table.Kind != "k" {
			return n.conn, n.table, true
		}
		return nil, db.Table{}, false
	}
	if t := dataOf(a.focused()); t != nil && t.table.Kind != "k" {
		return t.conn, t.table, true
	}
	return nil, db.Table{}, false
}
