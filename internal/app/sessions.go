package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// sessionList is the open session list (§7.8「session 列表」): the row
// selected, the sessions showing their windows, the name being typed.
type sessionList struct {
	sel, top int
	open     map[*Session]bool
	rename   *ui.Input // $ on the selected session's row
}

// sessionRow is a row of the list: session s's own, win -1, or one of its
// windows.
type sessionRow struct {
	s   *Session
	win int
}

func (a *App) sessionRows() []sessionRow {
	var rows []sessionRow
	for _, s := range a.sessions {
		rows = append(rows, sessionRow{s, -1})
		if a.sessList.open[s] {
			for wi := range s.Windows {
				rows = append(rows, sessionRow{s, wi})
			}
		}
	}
	return rows
}

// openSessions is SPC s, a click on the status bar's session block: the
// list, its selection on the session attached, in sight.
func (a *App) openSessions() {
	a.sessList = &sessionList{open: map[*Session]bool{}, sel: slices.Index(a.sessions, a.sess)}
	a.sessionsMove(0)
}

func (a *App) sessionsMove(d int) {
	l := a.sessList
	n := len(a.sessionRows())
	_, rows := ui.SessionBox(a.window(), n)
	l.sel, l.top = ui.ListView(l.sel+d, l.top, n, rows)
}

// sessionsFold is l (open) and h on the selection: a session shows or hides
// its windows; h on a window goes to its session's row, closed.
func (a *App) sessionsFold(open bool) {
	l := a.sessList
	r := a.sessionRows()[l.sel]
	l.open[r.s] = open
	l.sel = slices.Index(a.sessionRows(), sessionRow{r.s, -1})
	if open || r.win < 0 {
		l.sel = slices.Index(a.sessionRows(), r)
	}
	a.sessionsMove(0)
}

// sessionsAttach is ↵ on row i: its session attached, a window's switched
// to, the list closed.
func (a *App) sessionsAttach(i int) {
	if rows := a.sessionRows(); i < len(rows) {
		a.attach(rows[i].s)
		if rows[i].win >= 0 {
			a.selectWindow(rows[i].win)
		}
	}
	a.sessList = nil
}

// cycleSession is session.next (d 1) and session.prev: the session after or
// before the one shown, in the list's order, round the ends (F9.10).
func (a *App) cycleSession(d int) {
	if n := len(a.sessions); n > 1 {
		a.attach(a.sessions[((slices.Index(a.sessions, a.sess)+d)%n+n)%n])
	}
}

// attachLast is session.last: the session attached before this one, if it is
// still there (F9.10).
func (a *App) attachLast() {
	if s := a.lastSess; s != nil && slices.Contains(a.sessions, s) {
		a.attach(s)
	}
}

// attach makes s the session shown; every overlay goes, a question or a
// name being typed in the one left among them (F5.1).
func (a *App) attach(s *Session) {
	if s != a.sess {
		a.lastSess = a.sess
	}
	a.sess = s
	a.whichKey, a.paneNumbers, a.palette, a.drop, a.cols, a.keyHelp = false, false, nil, nil, nil, nil
	a.confirm, a.form, a.winRename, a.askName, a.sessList, a.menu, a.ddl = nil, nil, nil, nil, nil, nil, nil
	a.res.Reset()
}

// sessionDialed answers dialSession's dial: the new session's connection to
// the current one's spec, connected, or why not.
type sessionDialed struct {
	name string // the new session's, asked for first (F10.2)
	spec config.Connection
	d    dialed
	err  error
}

// askNewSession is session.new and n in the list (§7.8「新建 session」): the
// name typed first in the status bar, tmux's least number free in it to
// begin with, which a blank ↵ takes; the list closes. A name another
// session has is refused with the input left (F10.2).
func (a *App) askNewSession() tea.Cmd {
	a.sessList = nil
	if a.conn() == nil {
		return a.noConnection()
	}
	pre := a.freeSessionName()
	a.askName = &nameAsk{label: i18n.T("session 名："), in: ui.Input{Text: pre, Pos: len(pre)}, blank: pre, done: func(a *App, name string) (tea.Cmd, bool) {
		if a.sessionNamed(name) {
			return a.showToast(i18n.T("已有同名 session")), true
		}
		return a.dialSession(name, a.conn().Spec), false
	}}
	return nil
}

// freeSessionName is tmux's: the least number no session is named.
func (a *App) freeSessionName() string {
	n := 1
	for a.sessionNamed(strconv.Itoa(n)) {
		n++
	}
	return strconv.Itoa(n)
}

// dialSession makes a session called name of its own connections, once spec,
// the one current here, is connected for it, attached then. One not connecting
// makes none, the session staying the one it is (F5.1).
func (a *App) dialSession(name string, spec config.Connection) tea.Cmd {
	return tea.Batch(a.showToast(i18n.T("正在连接 %s…", spec.Name)), func() tea.Msg {
		d, err := dial(context.Background(), spec)
		return sessionDialed{name, spec, d, err}
	})
}

// windowCount is how a session says how many windows it has: one's own text, the
// others' a number (F17.4).
func windowCount(n int) string {
	if n == 1 {
		return i18n.T("1 个窗口")
	}
	return i18n.T("%d 个窗口", n)
}

// gotSessionDialed makes the session: every connection there is now, one
// the form added meanwhile too (§7.8).
func (a *App) gotSessionDialed(m sessionDialed) tea.Cmd {
	if a.toast == toastLine(i18n.T("正在连接 %s…", m.spec.Name)) { // it is done: the first session says nothing of it (F17.4)
		a.toast = ""
	}
	if m.err != nil {
		say := func() tea.Cmd { return a.showToast(i18n.T("%s：%s", m.spec.Name, m.err)) }
		if a.offerLibrary(m.err, func() tea.Cmd { return a.dialSession(m.name, m.spec) }, say) { // no libduckdb (F24.3)
			return nil
		}
		return say()
	}
	if a.sessionNamed(m.name) { // another session took it while this one dialed
		return tea.Batch(a.showToast(i18n.T("已有同名 session")), m.d.discard())
	}
	conns, start := make([]*Conn, len(a.sess.Conns)), -1
	for i, c := range a.sess.Conns {
		if conns[i] = newConn(c.Spec); c.Spec.Name == m.spec.Name {
			start = i
		}
	}
	if start < 0 { // deleted while it dialed, or waited for libduckdb (F24.3)
		return m.d.discard()
	}
	conns[start].take(m.d)
	s := bareSession(m.name, conns, conns[start])
	s.Windows = []*Window{s.blankWindow()} // no query: one is made when it is wanted (F8.18)
	a.sessions = append(a.sessions, s)
	a.attach(s)
	return a.loadCatalog(conns[start])
}

// killSession is x on a session's row (§7.8).
func (a *App) killSession() tea.Cmd {
	r := a.sessionRows()[a.sessList.sel]
	if r.win >= 0 {
		return nil
	}
	return a.closeSession(r.s)
}

// closeSession closes s, as x on its row in the list does and session.close
// does the one attached (§7.8, F8.12): asked first when changes or a
// transaction would go; its queries written, its connections closed, which
// rolls a transaction back. The one attached gives way to the one before it,
// or after; the last one gone is quitting. A list open stays.
func (a *App) closeSession(s *Session) tea.Cmd {
	if len(a.sessions) == 1 {
		return a.quit()
	}
	ps := s.panes()
	return a.unlessLost(unsaved(ps...), i18n.T("%s 里", s.Name), txConns(s), verbClose, func() tea.Cmd {
		if cmd, ok := a.flushAll(ps...); !ok { // the queries' text is not lost (§11)
			return cmd
		}
		i := slices.Index(a.sessions, s)
		a.sessions = slices.Delete(a.sessions, i, i+1)
		if l := a.sessList; a.sess == s {
			a.attach(a.sessions[max(i-1, 0)])
			a.sessList = l // the list it was closed in stays (F5.1)
		}
		if l := a.sessList; l != nil {
			l.sel = min(l.sel, len(a.sessionRows())-1)
		}
		return closeWorkers(s.workers()) // taken here: a connection being made may come in, the loop writing it, as the Cmd reads
	})
}

// closeWorkers is a Cmd that closes ws, which waits on what they run.
func closeWorkers(ws []*db.Worker) tea.Cmd {
	return func() tea.Msg {
		for _, w := range ws {
			w.Close()
		}
		return nil
	}
}

// renameSession is $ on a session's row: its name typed in its place.
func (a *App) renameSession() {
	if r := a.sessionRows()[a.sessList.sel]; r.win < 0 {
		a.sessList.rename = &ui.Input{Text: r.s.Name, Pos: len(r.s.Name)}
	}
}

// renameKey edits the name typed: ↵ takes it, but nothing typed or another
// session's name; esc drops it.
func (a *App) renameKey(k keymap.Key) tea.Cmd {
	l := a.sessList
	name, done := typeName(l.rename, k)
	if !done {
		return nil
	}
	l.rename = nil
	switch s := a.sessionRows()[l.sel].s; {
	case name == "" || name == s.Name:
	case a.sessionNameTaken(s, name):
		return a.showToast(i18n.T("已有同名 session"))
	default:
		s.Name = name
	}
	return nil
}

func (a *App) sessionsView() ui.SessionList {
	l := a.sessList
	v := ui.SessionList{Sel: l.sel, Top: l.top}
	for i, r := range a.sessionRows() {
		row := ui.SessionRow{Name: r.s.Name, Note: windowCount(len(r.s.Windows)), Current: r.s == a.sess}
		if r.win >= 0 {
			row = ui.SessionRow{Window: true, Name: fmt.Sprintf("%d: %s", r.win, r.s.Windows[r.win].Name), Current: r.win == r.s.Active} // each session's own, as tmux's
		}
		if i == l.sel && l.rename != nil {
			row.Edit = l.rename
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}
