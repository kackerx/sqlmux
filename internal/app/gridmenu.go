package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// gridMenu is the right-click menu of a cell or a head of the grid pane the
// pointer is on (§7.6「右键菜单」, F12.3), the cursor put on the cell first; the
// head's is the head's column. It opens nothing where there are no items: a
// result's head, or a table's in the turned view, which has none.
func (a *App) gridMenu(t ui.Target, p uv.Position) {
	pane := a.win().pane(t.Pane)
	if pane == nil {
		return
	}
	a.focusPane(t.Pane)
	var items []menuItem
	switch d, r := dataOf(pane), resultOf(pane); {
	case d != nil && t.Kind == ui.KindHead:
		a.cursorToHead(d, t.I)
		items = a.columnItems(d, false)
	case d != nil:
		a.gridGoto(strings.TrimPrefix(t.Action, "grid.goto "))
		items = a.columnItems(d, true)
	case r != nil && t.Kind == ui.KindCell:
		a.gridGoto(strings.TrimPrefix(t.Action, "grid.goto "))
		items = []menuItem{
			{i18n.T("在值编辑器中打开"), "edit", func() tea.Cmd { return a.valueOpen() }},
			{i18n.T("复制这一格"), "copy", func() tea.Cmd { return a.yankGrid(false) }},
			{i18n.T("以 JSON 查看"), "json", func() tea.Cmd { return a.viewJSON() }},
			{i18n.T("导出…"), "export", a.exportOpen},
		}
	}
	if len(items) > 0 {
		a.menu = &menu{at: p, items: items}
	}
}

// columnItems are the menu of the cell under the cursor of table t, or of
// its column's head: what sorts, hides and, of a cell, opens, filters and
// copies, each what the action of its name does.
func (a *App) columnItems(t *dataTab, cell bool) []menuItem {
	col := t.page.Cols[t.fieldAt(t.col)].Name
	sort := []menuItem{
		{i18n.T("按这一列排序 ↑"), "sort_asc", func() tea.Cmd { return a.orderOnly(t, col, false) }},
		{i18n.T("按这一列排序 ↓"), "sort_desc", func() tea.Cmd { return a.orderOnly(t, col, true) }},
		{i18n.T("加入排序"), "sort_asc", func() tea.Cmd { return a.orderToggle(t, col) }},
		{i18n.T("隐藏这一列"), "hide", func() tea.Cmd { return a.run("grid.col.hide", 0) }},
	}
	if !cell {
		return sort
	}
	items := []menuItem{
		{i18n.T("在值编辑器中打开"), "edit", func() tea.Cmd { return a.valueOpen() }},
		{i18n.T("按这个值过滤"), "filter", func() tea.Cmd { return a.filterCell() }},
	}
	items = append(items, sort...)
	return append(items,
		menuItem{i18n.T("复制这一格"), "copy", func() tea.Cmd { return a.yankGrid(false) }},
		menuItem{i18n.T("复制行为 JSON"), "copy", func() tea.Cmd { return a.yankJSON() }},
		menuItem{i18n.T("以 JSON 查看"), "json", func() tea.Cmd { return a.viewJSON() }},
		menuItem{i18n.T("导出…"), "export", a.exportOpen},
		menuItem{i18n.T("属性"), "table", func() tea.Cmd { return a.run("table.properties", 0) }},
	)
}

// hideColumn hides the page's column field, as COLS does, the cursor moved
// off it to the nearest shown one.
func (a *App) hideColumn(t *dataTab, field int) {
	t.hidden[t.page.Cols[field].Name] = true
	t.col = t.nearestShown(field)
}

// headClick is a click on the head of the shown column i of table pane: that
// column is the cursor's, and it sorts, or with Shift is added to the sorting.
func (a *App) headClick(pane, i int, shift bool) tea.Cmd {
	a.focusPane(pane)
	t := dataOf(a.focused())
	if t == nil || t.fieldAt(i) < 0 {
		return nil
	}
	a.cursorToHead(t, i)
	if shift {
		return a.run("grid.order.add", 0)
	}
	return a.run("grid.order.cycle", 0)
}

// cursorToHead puts table t's cursor on the shown column i, scrolled in
// sight as a move there is: a head cut at the edge is brought in.
func (a *App) cursorToHead(t *dataTab, i int) {
	t.col = i
	a.gridMove(func(r, c, _, _ int) (int, int) { return r, c })
}
