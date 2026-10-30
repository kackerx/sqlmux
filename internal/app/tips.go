package app

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// tip is a line of the status bar about a feature (§7.8「特性提示」, F15.4):
// the action it is about, the keys scope that action is bound in, and the
// sentence, {key} where the action's key goes. A tip is every action's to have;
// add one with a feature.
type tip struct{ action, scope, text string }

var tips = []tip{
	{"grid.yank", "grid", i18n.N("table pane 里 {key} 复制当前单元格")},
	{"grid.yank.insert", "grid", i18n.N("table pane 里 {key} 把当前行复制成 INSERT 语句")},
	{"grid.where", "grid", i18n.N("table pane 里 {key} 写 WHERE 过滤")},
	{"where.history", "grid", i18n.N("table pane 里 {key} 翻这张表的 WHERE 历史和收藏")},
	{"grid.filter.cell", "grid", i18n.N("table pane 里 {key} 用当前单元格的值过滤")},
	{"grid.order", "grid", i18n.N("table pane 里 {key} 设置排序，可以按多列")},
	{"grid.cols", "grid", i18n.N("table pane 里 {key} 选择显示哪些列")},
	{"grid.value", "grid", i18n.N("table pane 里 {key} 在浮窗里看或改长文本、JSON")},
	{"grid.view.json", "grid", i18n.N("table pane 里 {key} 把这一页看成 JSON")},
	{"grid.transpose", "grid", i18n.N("table pane 里 {key} 转置，一屏看完宽表的一行")},
	{"grid.select.visual", "grid", i18n.N("table pane 里 {key} 选一片单元格，再复制或删除")},
	{"grid.row.add", "grid", i18n.N("table pane 里 {key} 新增一行")},
	{"grid.export", "grid", i18n.N("table pane 里 {key} 导出当前结果")},
	{"grid.limit", "grid", i18n.N("table pane 里 {key} 改每页的行数")},
	{"jump.start", "grid", i18n.N("{key} 跳转：按屏幕上标出的字母直达那里")},
	{"palette.open", "global", i18n.N("{key} 打开命令面板：搜表、命令、query")},
	{"palette.sql", "grid", i18n.N("{key} 快速 SQL：不开 query 直接跑一句")},
	{"keyhelp.open", "normal", i18n.N("{key} 查看这里所有的键位")},
	{"tree.filter", "tree", i18n.N("树里 {key} 过滤节点")},
	{"tree.open.tab", "tree", i18n.N("树里 {key} 在新 tab 里打开表")},
	{"query.history", "queryinsert", i18n.N("query 的 INSERT 下 {key} 翻查询历史")},
	{"query.schema", "query", i18n.N("query 里 {key} 切换库和 schema")},
	{"pane.zoom", "normal", i18n.N("{key} 放大当前 pane，再按一次还原")},
	{"pane.split.right", "normal", i18n.N("{key} 向右分屏，把两张表并排对照")},
	{"result.toggle", "normal", i18n.N("{key} 显示或隐藏结果区")},
	{"tree.toggle", "normal", i18n.N("{key} 打开 / 聚焦 / 收起侧栏")},
	{"window.new", "normal", i18n.N("{key} 新建 window，一件事一个 window")},
	{"session.list", "normal", i18n.N("{key} 切换或新建 session")},
}

// tipEvery is how long a tip stays.
var tipEvery = 5 * time.Minute

// tipTick says the tip is due to change.
type tipTick struct{}

// tipState is the tips going round: each once before one comes again.
type tipState struct {
	cur  int          // the one shown, -1 before the first
	seen map[int]bool // those shown this round
	pick func(n int) int
}

// tipText is the tip that is up, "" when there is none: with tips off, or
// its key unbound since.
func (a *App) tipText() string {
	if !a.tipsOn || a.tip.cur < 0 {
		return ""
	}
	return a.tipLine(tips[a.tip.cur])
}

// tipLine is t as it reads now, "" when its action has no key.
func (a *App) tipLine(t tip) string {
	key := a.keys.Hint(t.action, t.scope)
	if key == "" {
		return ""
	}
	return strings.ReplaceAll(i18n.T(t.text), "{key}", key)
}

// nextTip makes another tip the one up: of those with a key, one not shown
// this round; the round starts again once all have been, with a tip other than
// the last (tips.next).
func (a *App) nextTip() {
	t := &a.tip
	if t.seen == nil {
		t.seen = map[int]bool{}
	}
	var left, all []int
	for i, tp := range tips {
		if a.tipLine(tp) == "" {
			continue
		}
		all = append(all, i)
		if !t.seen[i] {
			left = append(left, i)
		}
	}
	if len(left) == 0 {
		clear(t.seen)
		if left = slices.DeleteFunc(slices.Clone(all), func(i int) bool { return i == t.cur }); len(left) == 0 {
			left = all
		}
	}
	if len(left) == 0 {
		t.cur = -1
		return
	}
	t.cur = left[t.pick(len(left))]
	t.seen[t.cur] = true
}

// tipTimer is the tick that brings the next tip.
func tipTimer() tea.Cmd {
	return tea.Tick(tipEvery, func(time.Time) tea.Msg { return tipTick{} })
}
