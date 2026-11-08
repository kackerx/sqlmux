package app

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// confirmBox is the question put before what cannot be taken back: changes
// thrown away, a delete, a drop, a run that deletes (§10.5, §13).
type confirmBox struct {
	text, yes string
	then      func() tea.Cmd
	danger    bool // its first line is in the error color: what is asked is a delete or a drop (§13)
}

// unsaved is how many changes the tabs of panes hold (§10.6).
func unsaved(panes ...*Pane) int {
	n := 0
	for _, p := range panes {
		for _, tab := range p.Tabs {
			if tab.Data != nil {
				n += tab.Data.changes()
			}
		}
	}
	return n
}

// lostVerb is what a question about changes to lose is for, in the words of
// its button and its two sentences (§10.5).
type lostVerb struct{ label, discard, rollback string }

var (
	verbClose   = lostVerb{i18n.N("关闭"), i18n.N("关闭会丢弃。"), i18n.N("关闭会回滚。")}
	verbQuit    = lostVerb{i18n.N("退出"), i18n.N("退出会丢弃。"), i18n.N("退出会回滚。")}
	verbRefresh = lostVerb{label: i18n.N("刷新"), discard: i18n.N("刷新会丢弃。")}
)

// unlessLost runs then, or asks first (§10.5): with n changes, of what,
// to lose, "<what>有 N 处修改未保存，<verb>会丢弃。"; with the
// transactions of conns to roll back, "<names> 有未提交的事务，<verb>会回滚。".
func (a *App) unlessLost(n int, what string, conns []*Conn, verb lostVerb, then func() tea.Cmd) tea.Cmd {
	var sentences []string // a line each: one runs on past a 。 in Chinese only
	if n > 0 {
		if what == "" {
			sentences = append(sentences, i18n.T("有 %d 处修改未保存，", n)+i18n.T(verb.discard))
		} else {
			sentences = append(sentences, i18n.T("%s有 %d 处修改未保存，", what, n)+i18n.T(verb.discard))
		}
	}
	if s := rollbackLine(conns, verb); s != "" {
		sentences = append(sentences, s)
	}
	if len(sentences) == 0 {
		return then()
	}
	text := strings.Join(sentences, "\n")
	a.confirm = &confirmBox{text: text, yes: i18n.T(verb.label), then: then}
	return nil
}

// rollbackLine is "<names> 有未提交的事务，<verb>会回滚。" for the connections
// of conns, "" for none.
func rollbackLine(conns []*Conn, verb lostVerb) string {
	var names []string
	for _, c := range conns {
		if !slices.Contains(names, c.label()) { // sessions' connections of one name
			names = append(names, c.label())
		}
	}
	if len(names) == 0 {
		return ""
	}
	return i18n.T("%s 有未提交的事务，", strings.Join(names, i18n.T("、"))) + i18n.T(verb.rollback)
}

// txConns is the connections of sessions in a transaction.
func txConns(sessions ...*Session) []*Conn {
	var out []*Conn
	for _, s := range sessions {
		for _, c := range s.conns() {
			if c.inTx() {
				out = append(out, c)
			}
		}
	}
	return out
}

// quit ends the program, asking first when changes or transactions of any
// session would go (§10.5): closing the connections rolls those back.
func (a *App) quit() tea.Cmd {
	var ps []*Pane
	for _, s := range a.sessions {
		ps = append(ps, s.panes()...)
	}
	return a.unlessLost(unsaved(ps...), "", txConns(a.sessions...), verbQuit, func() tea.Cmd {
		if cmd, ok := a.flushAll(ps...); !ok { // the queries' text is not lost (§11)
			return cmd
		}
		a.saveLast()
		if a.logosGone != "" { // on the alternate screen still, where the logos are: Raw goes out with the
			// renderer's next frame, and a quit at once restores the screen before it
			return tea.Sequence(tea.Raw(a.logosGone), tea.Tick(logosDelay/5, func(time.Time) tea.Msg { return tea.QuitMsg{} }))
		}
		return tea.Quit
	})
}
