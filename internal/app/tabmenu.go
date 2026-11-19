package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// reorderTab moves pane p's tab from to index to, the current and previous tabs
// staying the tabs they were.
func (p *Pane) reorderTab(from, to int) {
	if from == to {
		return
	}
	at := func(i int) int {
		switch {
		case i == from:
			return to
		case from < i && i <= to:
			return i - 1
		case to <= i && i < from:
			return i + 1
		}
		return i
	}
	tb := p.Tabs[from]
	p.Tabs = slices.Insert(slices.Delete(p.Tabs, from, from+1), to, tb)
	p.Cur = at(p.Cur)
	if p.Prev >= 0 {
		p.Prev = at(p.Prev)
	}
}

// settleTab puts tab i where its pinning puts it, after the other pinned tabs: at the
// end of the pinned ones when it is pinned, the first of the others when it is not
// (F22.6).
func (p *Pane) settleTab(i int) {
	n := 0
	for j, tb := range p.Tabs {
		if j != i && tb.pinned() {
			n++
		}
	}
	p.reorderTab(i, n)
}

// pinTab is tab.pin: the current tab of the focused pane pinned, or not any more. A
// landing page and a result, which has its own P, are not pinned (§7.8).
func (a *App) pinTab() tea.Cmd {
	p := a.focused()
	tb := p.tab()
	if tb == nil || tb.Data == nil && tb.Query == nil {
		return nil
	}
	tb.Pinned = !tb.Pinned
	p.settleTab(p.Cur)
	return nil
}

// closeTabs closes the tabs of pane p that pick says, but the pinned ones and the ones
// asking would be needed for (changes unsaved, a transaction that would go with them, a
// run going on), which stay; the toast says how many of those. Each is closed as x closes
// it, closeTabAsking's, its question not asked (F22.6).
func (a *App) closeTabs(p *Pane, pick func(i int) bool) tea.Cmd {
	var cmds []tea.Cmd
	kept := 0
	for i := len(p.Tabs) - 1; i >= 0; i-- {
		if !pick(i) || p.Tabs[i].pinned() || p.Tabs[i].landing() {
			continue
		}
		before := len(p.Tabs)
		cmds = append(cmds, a.closeTabAsking(p, i))
		if a.confirm != nil {
			a.confirm = nil
		}
		if len(p.Tabs) == before { // asked, or a query running, or the log
			kept++
		}
	}
	if kept > 0 {
		cmds = append(cmds, a.showToast(i18n.T("%d 个 tab 没关：有未保存的修改、未提交的事务或正在执行", kept)))
	}
	return tea.Batch(cmds...)
}

// closeOthers is tab.close.others (d 0), tab.close.left (-1) and tab.close.right (1): the
// tabs of the focused pane but the current one, or those before it or after it.
func (a *App) closeOthers(d int) tea.Cmd {
	p := a.focused()
	if p == a.win().Tree || p == a.win().Result { // the result area has no batch close (§11)
		return nil
	}
	cur := p.Cur
	return a.closeTabs(p, func(i int) bool { return i != cur && (d == 0 || (i-cur)*d > 0) })
}

// tabMenu is the menu of tab i of pane id (§7.8「tab 的右键菜单」, F22.6): the tab
// is the pane's current one first, the focus on the pane.
func (a *App) tabMenu(id, i int, at uv.Position) {
	p := a.win().pane(id)
	if p == nil || p == a.win().Tree || i < 0 || i >= len(p.Tabs) {
		return
	}
	a.focusPane(id)
	selectTab(p, i)
	var items []menuItem
	tb := p.Tabs[i]
	closeIt := menuItem{i18n.T("关闭"), "close", "tab.close", func() tea.Cmd { return a.closeTabAsking(p, p.Cur) }} // x in a table's pane and a landing page's
	switch {
	case tb.Result != nil && tb.Result.run == nil: // the log stays (§11)
	case tb.Result != nil:
		pin := i18n.T("固定")
		if tb.Result.pinned {
			pin = i18n.T("取消固定")
		}
		items = []menuItem{closeIt, {pin, "pin", "result.pin", func() tea.Cmd { return a.run("result.pin", 0) }}}
	default:
		split := func(d Dir) func() tea.Cmd {
			return func() tea.Cmd { a.splitWithTab(d); return nil }
		}
		splits := []menuItem{
			{i18n.T("向右拆分"), "split_right", "pane.split.right", split(Horiz)},
			{i18n.T("向下拆分"), "split_below", "pane.split.below", split(Vert)},
		}
		if tb.landing() {
			items = append([]menuItem{closeIt}, splits...)
			break
		}
		move := menuItem{i18n.T("移到下一个 pane"), "pane", "tab.move.next", func() tea.Cmd { return a.moveTab(1) }}
		if !a.canMoveTab() {
			move.run = nil
		}
		pin := i18n.T("固定")
		if tb.Pinned {
			pin = i18n.T("取消固定")
		}
		items = []menuItem{closeIt,
			{i18n.T("关闭其他"), "close", "tab.close.others", func() tea.Cmd { return a.closeOthers(0) }},
			{i18n.T("关闭左侧"), "close", "tab.close.left", func() tea.Cmd { return a.closeOthers(-1) }},
			{i18n.T("关闭右侧"), "close", "tab.close.right", func() tea.Cmd { return a.closeOthers(1) }},
		}
		items = append(items, splits...)
		items = append(items, move, menuItem{pin, "pin", "tab.pin", a.pinTab})
	}
	if len(items) > 0 {
		a.menu = &menu{at: at, items: items}
	}
}

// splitWithTab splits the focused pane along d and moves its current tab into the new
// pane; the pane's only tab stays, the new pane is empty then.
func (a *App) splitWithTab(d Dir) {
	src := a.focused()
	if len(src.Tabs) < 2 {
		a.splitPane(d)
		return
	}
	tb := src.Tabs[src.Cur]
	dropTab(src, src.Cur)
	a.splitPane(d)
	dst := a.focused()
	putTab(dst, tb, dst.tab().landing())
}

// canMoveTab is whether tab.move.next has a pane to go to: there are two ordinary
// panes at least.
func (a *App) canMoveTab() bool { return len(a.ordinaryPanes()) >= 2 }
