package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// queryDir is the query files' names as the directory last read held
// them, and when it changed (F8.21).
type queryDir struct {
	names []string
	at    time.Time
	read  bool
}

// queryFiles is the query files' names (config.QueryFiles), read again
// when the directory changed: a query file coming and going changes it,
// what is written in one does not. The tree asks on every frame.
// ponytail: a change in the same tick of the file system's clock as the
// read before it is not seen (deleteQuery drops the read itself: on WSL a test
// deleted a file within the tick of the listing before it).
func (a *App) queryFiles() []string {
	dir := filepath.Dir(config.QueryPath("x"))
	if fi, err := os.Stat(dir); a.files.read && err == nil && fi.ModTime().Equal(a.files.at) {
		return a.files.names
	}
	names, at, err := config.QueryFiles()
	if err != nil { // none is listed: the tree is not where this is told
		names, at = nil, time.Time{}
	}
	a.files = queryDir{names, at, true}
	return names
}

// queryList is what the Queries node lists: the files, and the queries
// open that have none yet, a query being written once a word is typed in
// it (§7.8「Queries 节点」, F8.21).
func (a *App) queryList() []string {
	names := slices.Clone(a.queryFiles())
	for _, t := range a.queries() {
		if !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, t.name) }) {
			names = append(names, t.name)
		}
	}
	config.SortQueries(names)
	return names
}

// queryPlace is where query name opens: the connection state.json says it
// last used and the schema it was in; with none kept, or one that is not in the
// session, cur and its schema (§11).
func (a *App) queryPlace(name string, cur *Conn) (c *Conn, schema string) {
	last, kept := a.state.Queries[name]
	if i := slices.IndexFunc(a.sess.Conns, func(cn *Conn) bool { return cn.Spec.Name == last.Conn }); kept && i >= 0 {
		return a.sess.Conns[i].inDB(last.DB), last.Schema
	}
	if cur == nil {
		return nil, ""
	}
	return cur, cur.Schema
}

// openQueryFile is ↵ on a query of the Queries node: one that is open
// is gone to, as tab.reopen does, and one that is not opens in a tab of the
// data pane that takes what is opened, on the connection it last used, the
// current one when that is not in connections.toml (§11).
func (a *App) openQueryFile(name string) tea.Cmd {
	if a.focusQuery(name) {
		return nil
	}
	c, schema := a.queryPlace(name, a.conn())
	if c == nil {
		return a.noConnection()
	}
	t, err := openQuery(c, name)
	if err != nil {
		return a.showToast(err.Error())
	}
	t.schema = schema
	p := a.openTarget()
	a.showPane(p.ID)
	putTab(p, Tab{Name: name, Query: t}, p.tab().landing())
	return a.keepQuery(t)
}

// deleteQuery is tree.delete on a query of the Queries node: after a
// question the file goes, and what state.json kept of it, and the tabs that
// have it open, in every session (F8.21, F10.10). On any other node it
// does nothing.
func (a *App) deleteQuery() tea.Cmd {
	n, ok := a.treeNode()
	if !ok || n.kind != nodeQueryFile {
		return nil
	}
	name := n.query
	a.confirm = &confirmBox{text: i18n.T("删除 %s？", name), yes: i18n.T("删除"), then: func() tea.Cmd {
		// closing its tabs is asked about as any close is, when a transaction
		// would be left with no query, or refused while it runs
		open := slices.DeleteFunc(a.queries(), func(t *queryTab) bool { return !strings.EqualFold(t.name, name) })
		return a.closeAsking(0, "", open, func() tea.Cmd {
			if err := config.DeleteQuery(name); err != nil {
				return a.showToast(err.Error())
			}
			a.files.read = false
			a.dropQueryTabs(name)
			delete(a.state.Queries, name)
			a.clampTree()
			return a.saveState()
		})
	}}
	return nil
}

// dropQueryTabs closes the tabs that have query name open, in every
// session, as a dropped table's go (F8.23): what was typed and not written
// goes with the file, and an autosave still to come writes nothing; a pane
// left with no tab shows the landing page; tab.reopen no longer has it
// (F10.10).
func (a *App) dropQueryTabs(name string) {
	for _, s := range a.sessions {
		s.closed = slices.DeleteFunc(s.closed, func(c closedTab) bool { return strings.EqualFold(c.query, name) })
		for _, p := range s.panes() {
			for i := len(p.Tabs) - 1; i >= 0; i-- {
				if t := p.Tabs[i].Query; t != nil && strings.EqualFold(t.name, name) {
					t.saved = t.ver
					dropTab(p, i)
				}
			}
		}
	}
}
