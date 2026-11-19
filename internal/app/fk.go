package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// wantKeys reads the keys of the table a page came of when the tree has not:
// the foreign keys of its grid are the tree's Keys (n), one cache (§7.6「外键跳转」,
// F22.13). A view has none to read.
func (a *App) wantKeys(m pageMsg) tea.Cmd {
	t := m.tab
	if _, ok := t.conn.keys[idOf(t.table)]; ok || m.err != nil || !t.conn.sql() || t.conn.Spec.Engine == "exec" || a.sessionOf(t.conn) == nil {
		return nil
	}
	return a.fetchObjects(t.conn, t.table)
}

// fkOf is the foreign key col is a column of, from the keys the tree has.
// ponytail: the first one when a column is in several; pick from a list if that is wanted.
func (t *dataTab) fkOf(col string) (db.Key, bool) {
	for _, k := range t.conn.keys[idOf(t.table)] {
		if k.Ref != nil && slices.Contains(k.Cols, col) {
			return k, true
		}
	}
	return db.Key{}, false
}

// cursorFK is the foreign key the focused table's cursor column is one of.
func (a *App) cursorFK() (*dataTab, db.Key, bool) {
	t, ok := a.focusedGrid()
	if !ok || !t.conn.sql() || t.conn.Spec.Engine == "exec" || t.fieldAt(t.col) < 0 {
		return nil, db.Key{}, false
	}
	k, ok := t.fkOf(t.page.Cols[t.fieldAt(t.col)].Name)
	return t, k, ok
}

// followFK is gd (grid.fk.follow): the table the foreign key of the cursor's
// column points at opens in this pane, its WHERE the referenced columns = the
// values of this row, as the cell filter writes a value; one open in the pane
// goes to that WHERE (§7.6「外键跳转」, F22.13). The keys are read first when
// the tree has not.
func (a *App) followFK() tea.Cmd {
	t, ok := a.focusedGrid()
	if !ok || !t.conn.sql() || t.conn.Spec.Engine == "exec" {
		return nil
	}
	sr, key, ok := t.cursor()
	if !ok || t.fieldAt(t.col) < 0 {
		return nil
	}
	if _, ok := t.conn.keys[idOf(t.table)]; !ok {
		cmd := a.fetchObjects(t.conn, t.table)
		if cmd == nil { // a view
			return a.showToast(i18n.T("这一列不是外键"))
		}
		return func() tea.Msg {
			m := cmd().(objectsMsg)
			m.then = a.followFK
			return m
		}
	}
	k, ok := t.fkOf(t.page.Cols[t.fieldAt(t.col)].Name)
	if !ok {
		return a.showToast(i18n.T("这一列不是外键"))
	}
	c, d := t.conn, t.conn.eng.Dialect()
	conds := make([]string, len(k.Cols))
	for i, col := range k.Cols {
		v, set := t.value(sr, key, col)
		if !set || v.Null {
			return a.showToast(i18n.T("外键为空，没有被引用的行"))
		}
		sc := t.column(col)
		cond, ok := cellCondition(d, c.eng.Quote(k.Ref.Cols[i]), sc.Type, sc.Fixed, v)
		if !ok {
			return a.showToast(i18n.T("这一列的类型不支持按值过滤"))
		}
		conds[i] = cond
	}
	ref, ok := c.table(k.Ref.Schema, k.Ref.Table)
	if !ok {
		return a.showToast(i18n.T("%s 里没有表 %s", c.label(), k.Ref.Schema+"."+k.Ref.Table))
	}
	cmd := a.openWhere(a.focused(), c, ref, strings.Join(conds, " and "))
	a.revealTab(a.focused())
	return cmd
}
