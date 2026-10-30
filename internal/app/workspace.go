package app

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	dbexec "github.com/kackerx/sqlmux/internal/db/exec"
	"github.com/kackerx/sqlmux/internal/db/mssql"
	"github.com/kackerx/sqlmux/internal/db/mysql"
	"github.com/kackerx/sqlmux/internal/db/postgres"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/db/sqlite"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// Pane is the sidebar or one of the split tree's: tables and queries,
// side by side in its tabs (§5).
type Pane struct {
	ID        int // stable; the sidebar is 0
	Tabs      []Tab
	Cur, Prev int          // tab bar * and - (T-01)
	tabScroll ui.TabScroll // where the tab bar is scrolled to when the tabs do not fit; not saved (F17.1)
}

// Object is the title's "· name" part: the current tab.
func (p *Pane) Object() string {
	if t := p.tab(); t != nil {
		return t.Name
	}
	return ""
}

// tab is the current tab; nil when the pane has none.
func (p *Pane) tab() *Tab {
	if p.Cur < len(p.Tabs) {
		return &p.Tabs[p.Cur]
	}
	return nil
}

// landing is what a new tab is until a table or a query takes its place
// (§5「引导页」).
func landing() Tab { return Tab{Name: i18n.T("新 tab")} }

// landing reports whether t shows the landing page: a new tab, or no tab.
func (t *Tab) landing() bool { return t == nil || t.Data == nil && t.Query == nil && t.Result == nil }

// tableTab is the index of pane p's tab of table t of connection c, -1 for
// none: a pane has a table once (F3.37).
func (p *Pane) tableTab(c *Conn, t db.Table) int {
	return slices.IndexFunc(p.Tabs, func(tb Tab) bool { return tb.Data != nil && tb.Data.conn == c && idOf(tb.Data.table) == idOf(t) })
}

// putTab puts tab in pane p: over the current tab, or after the last one,
// which it becomes, the current one the previous.
func putTab(p *Pane, tab Tab, over bool) {
	if over && len(p.Tabs) > 0 {
		p.Tabs[p.Cur] = tab
		return
	}
	p.Prev = p.Cur
	if len(p.Tabs) == 0 { // an empty pane: there is no tab to go back to
		p.Prev = -1
	}
	p.Tabs = append(p.Tabs, tab)
	p.Cur = len(p.Tabs) - 1
}

type Window struct {
	ID       int // stable within its session, as a pane's: the tree's expansion and SPC l go by it
	Name     string
	TreeOpen bool  // the ⟨0⟩ sidebar is open, not folded to its thin bar
	TreeW    int   // the sidebar's width once dragged (§7.8); 0 is the default
	Tree     *Pane // ⟨0⟩ sidebar, not part of the split tree (D-04)
	// Result is the result area, at the bottom from the first run until it
	// is closed; resultRatio is the share of the rest above it, kept then
	// (0: 1 − result_height), and log the runs', kept too (§11).
	Result      *Pane
	resultRatio float64
	log         []ui.LogLine
	tree        treeState
	Root        *Node
	Focus       int // pane ID
	beforeTree  int // the pane focus came from into the sidebar, the way back (F19.5)
	Zoom        int // zoomed pane ID; 0 = none (P-03)
	lastID      int // highest pane ID handed out

	focusTick int
	focusedAt map[int]int // pane ID → focusTick when it last got focus

	resultHidden bool // SPC r took the result area out of the layout, its tabs kept (§11)
}

// focus moves focus to pane id, remembering when: moving by direction
// prefers the neighbour focused most recently (§5). Leaving a pane leaves
// its input too: the tree's filter row, a table's query bar.
func (w *Window) focus(id int) {
	if w.focusedAt == nil {
		w.focusedAt = map[int]int{}
	}
	w.focusTick++
	if id == w.Tree.ID && w.Focus != id {
		w.beforeTree = w.Focus
	}
	w.Focus, w.focusedAt[id] = id, w.focusTick
	if id != w.Tree.ID {
		w.tree.filtering = false
	}
	for _, p := range w.Root.Leaves() {
		if t := dataOf(p); t != nil && p.ID != id {
			t.stopTyping()
		}
	}
}

// pane is the window's pane id, the sidebar too; nil when there is none.
func (w *Window) pane(id int) *Pane {
	if id == w.Tree.ID {
		return w.Tree
	}
	for _, p := range w.Root.Leaves() {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Session is a workspace (tech-design §5): its windows, and every
// connection of connections.toml, each connected when first used.
type Session struct {
	Name    string
	Conns   []*Conn         // connections.toml's, in its order: the tree's roots (§7.8)
	start   *Conn           // the one it was started on, a layout's: what the file keeps (§14)
	last    *Conn           // the current connection where nothing focused names one (§5)
	open    map[string]bool // tree nodes opened or closed by hand, by node ID; the rest as §7.8 says
	Windows []*Window
	Active  int
	winID   int         // the highest window ID handed out
	prevWin int         // the ID of the window used before the one shown, SPC l's; 0 for none
	RunSeq  int         // the last run's number, #42 on its result tabs (§11)
	warning string      // query_1's file could not be read: a toast on start (§11)
	closed  []closedTab // the tabs closed last, the newest last: tab.reopen's (F5.16)

	// When each tab was last the focused pane's current one, a count by
	// what the tab holds, and the tab that is now (F5.19, §12). Not kept.
	// ponytail: a closed tab stays in the map; a session opens few enough.
	tabSeq  map[any]int
	seq     int
	focused any
}

// tabKey is what a tab is known by: its table, query or result; nil for a
// landing tab, which none are.
func tabKey(tb *Tab) any {
	switch {
	case tb == nil:
	case tb.Data != nil:
		return tb.Data
	case tb.Query != nil:
		return tb.Query
	case tb.Result != nil:
		return tb.Result
	}
	return nil
}

// touch notes that tb, the current tab of the focused pane, is the one in
// use: the Tab scope of the palette lists them newest first (F5.19). The
// sidebar and a landing tab are none, the one before staying the one.
func (s *Session) touch(tb *Tab) {
	if key := tabKey(tb); key != nil && key != s.focused {
		if s.tabSeq == nil {
			s.tabSeq = map[any]int{}
		}
		s.seq++
		s.tabSeq[key], s.focused = s.seq, key
	}
}

// connState is how far a Conn got with connecting (§7.8「连接节点」).
type connState int

const (
	connIdle connState = iota
	connConnecting
	connFailed
	connReady
)

// Conn is one connection of connections.toml as a session has it (§5):
// its Main and Meta once connected, its catalog and what is cached of it.
type Conn struct {
	Spec config.Connection
	// live is Spec as the Workers' redials read it, from their goroutines:
	// they dial with the connection as it is now, a rename or a new address
	// included (F8.19).
	liveMu sync.Mutex
	live   config.Connection
	// drv, eng and readOnly are Spec.Engine's and Spec.ReadOnly as the
	// connection was dialed: an edit of the spec meanwhile is for the next dial
	// (F8.16). eng is drv as a SQL engine, nil for another kind of server
	// (§5「连接缝」): what asks for it is not there for such a connection.
	drv        db.Driver
	eng        db.Engine
	readOnly   bool
	State      connState
	err        error      // why the last dial failed (connFailed): the table tabs waiting on it say it (F12.5)
	Main, Meta *db.Worker // §8.2; nil till connected
	Addr       string     // shown in the status bar, e.g. ctw@localhost:5432
	busy       int        // its requests out that C-c cancels (§8.3)
	// tx is where Main's transaction was as its last request ended: 'I'
	// out of one, 'T' in one, 'E' in one that failed; 0 before any. The
	// replies bring it: Main may be busy, its lock not to wait on (F5.11).
	tx byte
	// downMain and downMeta are why the last redial of Main, of Meta, failed;
	// nil once one went through (F8.19). The connection stays State
	// connReady: its next request dials again.
	downMain, downMeta error
	// Manual is the transaction mode, its queries' and its saves': a run
	// or a save begins one, which stays till committed or rolled back
	// (F5.3). Each connects in the auto mode.
	Manual bool

	Schema    string   // its Schema: the tree cursor's under it, or the last it was under (§7.8)
	Schemas   []string // the catalog's (§8.4)
	home      string   // current_schema(), whose Tables the tree opens with
	Tables    []db.Table
	cols      map[tableID]db.Columns
	colsAsked map[tableID]bool       // for names' colors (§7.3): asked once, till cols is dropped
	ddl       map[tableID]ddlText    // the palette's previews (F4.2), dropped with cols
	idx       map[tableID][]db.Index // a table's indexes and keys, read when it opens in the tree (F14.3), dropped with cols
	keys      map[tableID][]db.Key
	funcs     []db.Function // every schema's, with the catalog
	// Databases are the server's the user may connect to, with the catalog
	// (PG; F15.1); dbs the connections to those but the DSN's, made as the
	// tree opens one, by name.
	Databases []string
	dbs       map[string]*Conn
	// all are the schemas and databases the server has, of which Schemas and Databases
	// are those picked to load (F17.13)
	all catalogAll
	// scan is what the scan of a Redis db read (F16.2); what the tree's filter
	// asked the server for is each window's own: treeState.found
	scan   *keyScan
	parent *Conn // the connection a database's is of, nil for one of connections.toml
	seqs   []db.Sequence

	// DB is the database, on query titles, "" for MySQL, whose schemas
	// are its databases; startedPath Main's search_path as it connected;
	// mainPath the schema a query's run last put first on it, "" when
	// not known, touched in Main's requests only (§8.6).
	DB                    string
	startedPath, mainPath string
	mainOn                db.Conn // the connection mainPath is of; another is a redial's (F8.19)
	// cmds are a Redis server's commands, as COMMAND listed them when the
	// connection came up: what writes, what is dangerous (§9.8); nil when it
	// would not say
	cmds      redis.Commands
	docs      redis.Docs
	cmdsAsked bool
	cmdsReady bool // COMMAND has answered, with the list or without it
	// hints are the key names and hash fields the server gave for the console's
	// completion, by what they were asked for (§9.8)
	hints map[string][]string
}

// inDB is the connection to database name: c itself for the one it is dialed
// to, else one of its own, made on first use, with c's name, mode and lock
// (F15.1).
func (c *Conn) inDB(name string) *Conn {
	if name == "" || name == c.DB {
		return c
	}
	if c.dbs[name] == nil {
		if c.dbs == nil {
			c.dbs = map[string]*Conn{}
		}
		spec := c.Spec
		spec.Database = name
		c.dbs[name] = newConn(spec)
		c.dbs[name].parent = c
	}
	return c.dbs[name]
}

// root is the connection of connections.toml that c is, or is a database of.
func (c *Conn) root() *Conn {
	if c.parent != nil {
		return c.parent
	}
	return c
}

// tableKey is table t of c as the palette's ids and state.json's table keys
// have it, <connection>/[<database>/]<schema>.<table>: one place for both
// (F15.1).
func (c *Conn) tableKey(t db.Table) string {
	if t.Kind == "k" { // a Redis key: <connection>/db<n>/<key> (§14)
		return c.Spec.Name + "/" + dbText(cmp.Or(c.Spec.Database, c.DB)) + "/" + t.Name
	}
	return c.keyPrefix() + t.Schema + "." + t.Name
}

// keyPrefix is what tableKey starts with.
func (c *Conn) keyPrefix() string {
	if c.Spec.Database == "" {
		return c.Spec.Name + "/"
	}
	return c.Spec.Name + "/" + c.Spec.Database + "/"
}

// qualified is table t of c as a title or a question names it: schema.table,
// the database before them when it is not the DSN's, or it would read as the
// DSN's table of the name.
func (c *Conn) qualified(t db.Table) string {
	if c.Spec.Database == "" {
		return t.Schema + "." + t.Name
	}
	return c.Spec.Database + "." + t.Schema + "." + t.Name
}

// tabName is the name of table t's tab: the table's, the database's too when
// it is not the DSN's.
func (c *Conn) tabName(t db.Table) string {
	if c.Spec.Database == "" || c.redis() { // a key's db is the connection's, as the tree shows it
		return t.Name
	}
	return c.qualified(t)
}

// label is c as a toast or a question names it: the connection, and the
// database when it is not the DSN's.
func (c *Conn) label() string {
	if c.Spec.Database == "" {
		return c.Spec.Name
	}
	return c.Spec.Name + "/" + c.Spec.Database
}

// databaseNames are the databases of c's server as the tree lists them: the
// one it is dialed to first, the rest by name (F15.1), a Redis server's by
// number (F16.10); none for an engine that has none.
func (c *Conn) databaseNames() []string {
	if len(c.Databases) == 0 {
		return nil
	}
	if c.redis() { // gotKeys keeps them in order
		return slices.Clone(c.Databases)
	}
	return append([]string{c.DB}, slices.DeleteFunc(slices.Clone(c.Databases), func(n string) bool { return n == c.DB })...)
}

// conns is the session's connections and the databases of them made so far,
// each connection before its databases: what holds a session's workers, its
// transactions and its answers (F15.1).
func (s *Session) conns() []*Conn {
	var out []*Conn
	for _, c := range s.Conns {
		out = append(out, c)
		for _, name := range slices.Sorted(maps.Keys(c.dbs)) {
			out = append(out, c.dbs[name])
		}
	}
	return out
}

func newConn(spec config.Connection) *Conn {
	c := &Conn{cols: map[tableID]db.Columns{}, colsAsked: map[tableID]bool{}, ddl: map[tableID]ddlText{}, idx: map[tableID][]db.Index{}, keys: map[tableID][]db.Key{}}
	c.setSpec(spec)
	return c
}

// setSpec makes spec what the connection is, for the dial after the one it
// may be in.
func (c *Conn) setSpec(spec config.Connection) {
	c.setConfig(spec)
	c.setDriver(spec)
}

// redis is whether c is a connection to Redis, dialed or not: its db nodes
// have keys under them, not schemas (F16.2).
func (c *Conn) redis() bool { return c.Spec.Kind() == "redis" }

// sql is whether c is a SQL connection: what is SQL's, a catalog, a query's
// statements, quick SQL, a WHERE, asks it (§5「连接缝」, F16.1).
func (c *Conn) sql() bool { return c.eng != nil }

// dialect is the dialect c's text is drawn in: a connection that is no SQL one
// has PG's colors until its language has its own (F16.4).
func (c *Conn) dialect() sqlkit.Dialect {
	if c.eng == nil {
		return sqlkit.PG
	}
	return c.eng.Dialect()
}

// setDriver makes c's driver, and its engine when it is a SQL one, what spec
// names.
func (c *Conn) setDriver(spec config.Connection) {
	c.drv = driverOf(spec)
	c.eng, _ = c.drv.(db.Engine)
	c.readOnly = spec.ReadOnly
}

// setConfig makes spec what the connection is for the redials of the
// dial it is in, its engine and lock the dial's.
func (c *Conn) setConfig(spec config.Connection) {
	c.Spec = spec
	c.liveMu.Lock()
	c.live = spec
	c.liveMu.Unlock()
}

func (c *Conn) liveSpec() config.Connection {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	return c.live
}

// drivers are the servers connections.toml names (§8.1, §5「连接缝」).
var drivers = map[string]db.Driver{"postgres": postgres.Engine{}, "mysql": mysql.Engine{}, "sqlite": sqlite.Engine{}, "redis": redis.Driver{}, "mssql": mssql.Engine{}}

// driverOf is the driver of spec's engine: an exec connection's is the engine of its
// dialect over its command (§8.8).
func driverOf(spec config.Connection) db.Driver {
	if spec.Engine == "exec" {
		if spec.Dialect == "redis" {
			return dbexec.RedisDriver{Command: spec.Command}
		}
		return dbexec.Engine{Engine: postgres.Engine{}, Command: spec.Command}
	}
	return drivers[spec.Engine]
}

func (a *App) win() *Window { return a.sess.Windows[a.sess.Active] }

// conn is the current connection (§5): the focused tab's, a result tab's
// where it ran; on the tree, the one the cursor is under; each once
// connected, else the last one. That one may not be connected either, a
// layout's other sessions connect as the program runs (F6.5): what runs
// on it looks first (Conn.State).
func (a *App) conn() *Conn {
	var c *Conn
	if p := a.focused(); p == a.win().Tree {
		if n, ok := a.treeNode(); ok {
			c = n.conn
		}
	} else {
		c = p.tab().conn()
	}
	if c != nil && c.State == connReady {
		return c
	}
	return a.sess.last
}

// conn is the connection t is of: a table's, a query's, a result's where it ran;
// none for a landing page or no tab.
func (t *Tab) conn() *Conn {
	switch {
	case t == nil:
	case t.Data != nil:
		return t.Data.conn
	case t.Query != nil:
		return t.Query.conn
	case t.Result != nil && t.Result.run != nil:
		return t.Result.run.conn
	}
	return nil
}

// production is whether t is of a production connection (§13, F17.9).
func (t *Tab) production() bool {
	c := t.conn()
	return c != nil && c.Spec.Production
}

// inTx reports whether c's Main is in a transaction, as far as its last
// request tells.
func (c *Conn) inTx() bool { return c != nil && c.tx != 0 && c.tx != 'I' }

// cancel cancels what c's Main and Meta are running (§8.3).
func (c *Conn) cancel() {
	if c == nil || c.State != connReady {
		return
	}
	c.Meta.Cancel()
	c.Main.Cancel()
}

// dropCols forgets the columns fetched: the catalog loads anew (§7.8).
func (c *Conn) dropCols() {
	clear(c.cols)
	clear(c.colsAsked)
	clear(c.ddl)
	clear(c.idx)
	clear(c.keys)
}

// newSession is a session's default workspace (§5) over conns, start the
// one it starts on: one window, data, with the sidebar, a pane with no tab
// and query_n on start beside it at 1 : 1 (§5, F8.9), which has what its
// file kept (§11). A file that cannot be read leaves that pane with no tab
// too: opened empty, the autosave would write over it.
func newSession(name string, conns []*Conn, start *Conn, query string) *Session {
	s := bareSession(name, conns, start)
	var w *Window
	w, s.warning = s.newWindow(start, query)
	s.Windows = []*Window{w}
	return s
}

// bareSession is a session with no window yet, start its connection in use.
func bareSession(name string, conns []*Conn, start *Conn) *Session {
	return &Session{Name: name, Conns: conns, start: start, last: start, open: map[string]bool{}}
}

// freeWindowName is the name a new window gets: data, else data-2, data-3…
// the first no window of s has (F8.11).
func (s *Session) freeWindowName() string {
	name := "data"
	for n := 2; slices.ContainsFunc(s.Windows, func(w *Window) bool { return w.Name == name }); n++ {
		name = "data-" + strconv.Itoa(n)
	}
	return name
}

// newWindow is a window of the default layout (§5): the sidebar, a pane
// with no tab and query_n on conn beside it at 1 : 1 (§5, F8.9), named
// by freeWindowName; warning says why the query's file could not be read.
func (s *Session) newWindow(conn *Conn, query string) (w *Window, warning string) {
	data := &Pane{ID: 1, Prev: -1}
	queryPane := &Pane{ID: 2, Prev: -1}
	if conn != nil { // a query is a connection's: none yet at a start with none (F7.4)
		if cons, err := openQuery(conn, query); err != nil {
			warning = err.Error()
		} else {
			queryPane.Tabs = []Tab{{Name: query, Query: cons}}
		}
	}
	return s.window(&Node{Split: Horiz, Ratio: 0.5, A: leaf(data), B: leaf(queryPane)}, 2), warning
}

// blankWindow is the window window.new and session.new make: the sidebar and
// one empty pane, the landing page, no query, as tmux's new-window (§5,
// F8.18).
func (s *Session) blankWindow() *Window { return s.window(leaf(&Pane{ID: 1, Prev: -1}), 1) }

// window is a new window of s over root, whose panes number up to lastID,
// named by freeWindowName, the pane 1 focused.
func (s *Session) window(root *Node, lastID int) *Window {
	s.winID++
	w := &Window{ID: s.winID, Name: s.freeWindowName(), TreeOpen: true, Tree: &Pane{ID: 0}, Root: root, lastID: lastID}
	w.focus(1)
	return w
}

// selectWindow shows window i of the session, the one shown so far being
// SPC l's way back.
func (a *App) selectWindow(i int) {
	s := a.sess
	if i == s.Active || i < 0 || i >= len(s.Windows) {
		return
	}
	s.prevWin, s.Active = s.Windows[s.Active].ID, i
	a.winRename = nil // the name typed is the window's left
}

// windowByID is the index of the session's window id; -1 for none.
func (s *Session) windowByID(id int) int {
	return slices.IndexFunc(s.Windows, func(w *Window) bool { return w.ID == id })
}

// addWindow is SPC c: a window of one empty pane at the end, shown (F8.18).
func (a *App) addWindow() tea.Cmd {
	a.sess.Windows = append(a.sess.Windows, a.sess.blankWindow())
	a.selectWindow(len(a.sess.Windows) - 1)
	return nil
}

// cycleWindow is SPC n (d 1) and SPC p (d -1): the next or previous
// window, around the ends.
func (a *App) cycleWindow(d int) {
	n := len(a.sess.Windows)
	a.selectWindow((a.sess.Active + d + n) % n)
}

// renameWindowKey edits the name typed in the status bar: ↵ takes it but a
// blank one, or another window's of the session, which is refused with the
// input left; esc drops it (§5, F8.11).
func (a *App) renameWindowKey(k keymap.Key) tea.Cmd {
	name, done := typeName(a.winRename, k)
	if !done {
		return nil
	}
	if name != "" && slices.ContainsFunc(a.sess.Windows, func(w *Window) bool { return w != a.win() && w.Name == name }) {
		return a.showToast(i18n.T("已有同名 window"))
	}
	if name != "" {
		a.win().Name = name
	}
	a.winRename = nil
	return nil
}

// nameAsk is a name typed in the status bar, over its left: label says
// what for, ↵ gives the trimmed name, never blank, to done, which is told to
// keep the input when the name was refused with a toast (esc drops it). A
// blank name is blank's when it has one, else it drops the input.
type nameAsk struct {
	label string
	in    ui.Input
	blank string
	done  func(a *App, name string) (cmd tea.Cmd, keep bool)
}

// askNameKey is key k in that input.
func (a *App) askNameKey(k keymap.Key) tea.Cmd {
	n := a.askName
	name, done := typeName(&n.in, k)
	if !done {
		return nil
	}
	if name == "" && k != keymap.Esc { // a blank ↵, not esc, takes the name given to begin with
		name = n.blank
	}
	if name == "" {
		a.askName = nil
		return nil
	}
	cmd, keep := n.done(a, name)
	if !keep {
		a.askName = nil
	}
	return cmd
}

// askSessionName is session.rename: this session's name typed in the status
// bar; another session's is refused (F8.11).
func (a *App) askSessionName() {
	s := a.sess
	a.askName = &nameAsk{label: i18n.T("session 名："), in: ui.Input{Text: s.Name, Pos: len(s.Name)}, done: func(a *App, name string) (tea.Cmd, bool) {
		if a.sessionNameTaken(s, name) {
			return a.showToast(i18n.T("已有同名 session")), true
		}
		s.Name = name
		return nil, false
	}}
}

// sessionNameTaken is whether name is another session's than s's.
func (a *App) sessionNameTaken(s *Session, name string) bool {
	return name != s.Name && a.sessionNamed(name)
}

// sessionNamed is whether a session is called name.
func (a *App) sessionNamed(name string) bool {
	return slices.ContainsFunc(a.sessions, func(o *Session) bool { return o.Name == name })
}

// askQueryName is query.rename: the focused query's name typed in the
// status bar. Its name is its file's: the file is written, then renamed,
// the state's and the tabs' names going with it. A name that is no file's,
// or that another file, or another query open, has, is refused (F8.11).
func (a *App) askQueryName() {
	t := queryOf(a.focused())
	if t == nil {
		return
	}
	a.askName = &nameAsk{label: i18n.T("query 名："), in: ui.Input{Text: t.name, Pos: len(t.name)}, done: func(a *App, name string) (tea.Cmd, bool) {
		if name == t.name {
			return nil, false
		}
		path := config.QueryPath(name)
		if err := config.CheckQueryName(name); err != nil {
			return a.showToast(err.Error()), true
		}
		fi, err := os.Stat(path)
		old, _ := os.Stat(t.path)
		switch {
		case err == nil && !os.SameFile(fi, old) || a.queryOpen(name, t): // a name of its own file only differing in case is its own
			return a.showToast(i18n.T("已有同名 query")), true
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return a.showToast(err.Error()), true
		}
		if err := t.flush(); err != nil {
			return a.saveFailed(err), true
		}
		if err := os.Rename(t.path, path); err != nil && !errors.Is(err, fs.ErrNotExist) { // no file yet: nothing typed
			return a.showToast(err.Error()), true
		}
		delete(a.state.Queries, t.name)
		t.name, t.path = name, path // the rename keeps the time and size: what the tab had of the file is still what it is compared to
		for _, s := range a.sessions {
			for _, w := range s.Windows {
				for _, p := range w.Root.Leaves() {
					for i := range p.Tabs {
						if p.Tabs[i].Query == t {
							p.Tabs[i].Name = name
						}
					}
				}
			}
		}
		return a.keepQuery(t), false
	}}
}

// typeName is key k on in, a name typed in its place: ↵ ends it with the
// name trimmed, esc with none, ""; done is whether it ended.
func typeName(in *ui.Input, k keymap.Key) (name string, done bool) {
	switch k {
	case keymap.Esc:
		return "", true
	case "<CR>":
		return strings.TrimSpace(in.Text), true
	}
	editInput(in, k)
	return "", false
}

// closeWindow is window.close (§5): the window's queries written, asked
// first when changes would go; the one used before it shown, else the one
// before it. The last one stays, as the last pane.
func (a *App) closeWindow() tea.Cmd {
	s := a.sess
	if len(s.Windows) == 1 {
		return nil
	}
	w := a.win()
	var ps []*Pane
	if w.Root != nil {
		ps = w.Root.Leaves()
	}
	return a.closeAsking(unsaved(ps...), i18n.T("这个 window 里"), queriesIn(ps...), func() tea.Cmd {
		if cmd, ok := a.flushAll(ps...); !ok {
			return cmd
		}
		i := s.Active
		s.Windows = slices.Delete(s.Windows, i, i+1)
		if s.Active = s.windowByID(s.prevWin); s.Active < 0 {
			s.Active = max(i-1, 0)
		}
		s.prevWin = 0
		return nil
	})
}

// OpenEmpty is the session of a start with no connection: the default
// layout with no query, a query being a connection's, the connection
// form open as the App begins (§14「启动时找不到连接」).
func OpenEmpty() *Session {
	s := &Session{Name: "sqlmux", open: map[string]bool{}}
	w, _ := s.newWindow(nil, config.QueryName(1))
	s.Windows = []*Window{w}
	return s
}

// Open is a session over specs, start the connection it starts on (§5):
// none is connected yet, the program connects them as it runs (§14「恢复」).
func Open(specs []config.Connection, start int) *Session {
	conns := make([]*Conn, len(specs))
	for i, spec := range specs {
		conns[i] = newConn(spec)
	}
	first := conns[start] // before groupOrder moves it
	groupOrder(conns)
	return newSession(specs[start].Name, conns, first, config.QueryName(1))
}

// OpenTemp is the session over a connection of the command line, spec (F14.1):
// its query is a new one, not query_1, which is the user's.
func OpenTemp(spec config.Connection) *Session {
	c := newConn(spec)
	return newSession(spec.Name, []*Conn{c}, c, freeQueryName(func(string) bool { return false }))
}

// dialed is a connection's Main and Meta as dial connected them, where
// Main landed, and the spec they were dialed from: an edit of the
// connection meanwhile is for the next dial (F8.16), and a redial is of
// this one.
type dialed struct {
	main, meta db.Conn
	info       db.Info
	spec       config.Connection
}

// discard is a Cmd that closes the two connections of d, one that came when
// nothing wants it any more.
func (d dialed) discard() tea.Cmd {
	return func() tea.Msg { d.main.Close(); d.meta.Close(); return nil }
}

// secrets caches what a connection's password source gave, by its name
// (§13): the keychain asks once a run, not once a session; a failed dial
// drops it, the next one asks again.
var secrets sync.Map

// dial connects spec's Main and then its Meta (§8.2).
func dial(ctx context.Context, spec config.Connection) (dialed, error) {
	main, info, err := connectTo(ctx, spec, spec.ReadOnly) // a read-only connection's too (§13)
	if err != nil {
		return dialed{}, err
	}
	meta, _, err := connectTo(ctx, spec, true)
	if err != nil {
		main.Close()
		return dialed{}, err
	}
	return dialed{main, meta, info, spec}, nil
}

// connectTo is one connection of spec's, read only or not.
func connectTo(ctx context.Context, spec config.Connection, readOnly bool) (db.Conn, db.Info, error) {
	spec, err := spec.Expanded()
	if err != nil {
		return nil, db.Info{}, err
	}
	pw, cached := secrets.Load(spec.Name)
	if !cached {
		s, err := spec.Secret()
		if err != nil {
			return nil, db.Info{}, err
		}
		pw = s
		secrets.Store(spec.Name, s)
	}
	dsn := spec.DSN
	if spec.Database != "" { // a child of the connection in the tree: PG's database, Redis's db (F15.1, F16.2)
		withDB := postgres.WithDatabase
		switch spec.Kind() {
		case "redis":
			withDB = redis.WithDB
		case "mssql": // the DSN's database parameter (F18.6)
			withDB = mssql.WithDatabase
		}
		if dsn, err = withDB(dsn, spec.Database); err != nil {
			return nil, db.Info{}, err
		}
	}
	c, info, err := driverOf(spec).Connect(tunneled(ctx, spec), dsn, pw.(string), readOnly)
	if err != nil {
		secrets.Delete(spec.Name)
	}
	return c, info, err
}

// tunneled is ctx asking for the connection to go through spec's ssh, when it
// has one: a SQLite file has no network to go through (F15.2).
func tunneled(ctx context.Context, spec config.Connection) context.Context {
	if spec.SSH == "" || spec.Engine == "sqlite" {
		return ctx
	}
	return db.WithDialer(ctx, db.SSHDialer(spec.SSH))
}

// take makes c connected over d.
func (c *Conn) take(d dialed) {
	c.State, c.Main, c.Meta = connReady, c.worker(d.main, d.spec, false), c.worker(d.meta, d.spec, true)
	c.Addr, c.DB, c.startedPath = d.info.Addr, d.info.Database, d.info.Path
	if c.Spec.Database != "" { // two of the server's databases differ by the address alone in the status bar
		c.Addr += "/" + c.DB
	}
}

// Close closes the session's connections.
func (s *Session) Close() {
	for _, w := range s.workers() {
		w.Close()
	}
}

// workers is the session's connections connected, Main and Meta.
func (s *Session) workers() []*db.Worker {
	var ws []*db.Worker
	for _, c := range s.conns() {
		if c.State == connReady {
			ws = append(ws, c.Main, c.Meta)
		}
	}
	return ws
}

// openTable shows c's table t in openTarget's pane and focuses it (§7.8
// 「打开已有的表」, §12).
func (a *App) openTable(c *Conn, t db.Table) tea.Cmd { return a.openTableIn(a.openTarget(), c, t) }

// openTableIn shows c's table t in pane p and focuses it: p's tab of it, a
// pane having one at most (F3.37), else a new one. A landing tab current
// gives way to it, replaced, or closed for the tab there (§5「引导页」).
func (a *App) openTableIn(p *Pane, c *Conn, t db.Table) tea.Cmd {
	return a.openTableAs(p, c, t, nil, a.newTurned)
}

// openTableAs is openTableIn asking for r, when a new tab is made, in place
// of the first page as the table opens, and for it turned or not as turned
// says.
func (a *App) openTableAs(p *Pane, c *Conn, t db.Table, r *request, turned bool) tea.Cmd {
	a.showPane(p.ID)
	landing := p.tab().landing()
	if i := p.tableTab(c, t); i >= 0 {
		if closed := p.Cur; landing {
			if a.closeTab(p, closed); closed < i {
				i--
			}
		}
		selectTab(p, i)
		return nil
	}
	// ponytail: named by the table alone; two connections' t_order tabs read
	// the same, the status bar telling the focused one's (§7.8). Add the
	// connection, t_order [seed], if that confuses.
	tab := Tab{Name: c.tabName(t), Data: newDataTab(c, t)}
	tab.Data.transpose = turned
	if r == nil {
		first := tab.Data.request
		first.desc = a.newDesc
		tab.Data.setRequest(first)
	} else {
		tab.Data.setRequest(*r)
	}
	putTab(p, tab, landing)
	return a.fetch(tab.Data, true)
}

// showTab focuses pane p and switches to its tab i.
func (a *App) showTab(p *Pane, i int) {
	a.showPane(p.ID)
	selectTab(p, i)
}

// selectTab makes tab i pane p's current one; the one it was becomes the
// previous, the - (T-01). What was being typed in it goes, as when its pane
// loses focus.
func selectTab(p *Pane, i int) {
	if i == p.Cur || i < 0 || i >= len(p.Tabs) {
		return
	}
	if t := dataOf(p); t != nil {
		t.stopTyping()
	}
	p.Prev, p.Cur = p.Cur, i
}

// cycleTab is gt (d 1) and gT (d -1) on the focused pane, as in vim: the
// next or previous tab, around the ends; with a count gt goes to tab count
// and gT back count tabs.
func (a *App) cycleTab(d, count int) {
	p := a.focused()
	if n := len(p.Tabs); d > 0 && count > 0 {
		selectTab(p, count-1)
	} else if n > 0 {
		selectTab(p, ((p.Cur+d*max(count, 1))%n+n)%n)
	}
}

// newTab is tab.new, a tab bar's +: a landing tab after the last one,
// which it becomes, in the focused pane (§5「引导页」).
func (a *App) newTab() {
	p := a.openTarget()
	a.showPane(p.ID)
	putTab(p, landing(), false)
}

// newQuery is query.new (§5, §11): query_n, n the least no session
// has open, with what its file keeps, on conn, in its Schema, kept for the
// file, in place of a landing tab or in a new tab of openTarget's pane.
// ok is false when the file cannot be read: no query, a toast.
func (a *App) newQuery(conn *Conn) (cmd tea.Cmd, ok bool) {
	p, name := a.openTarget(), a.freeQuery()
	c, err := openQuery(conn, name)
	if err != nil {
		return a.showToast(err.Error()), false
	}
	a.showPane(p.ID)
	putTab(p, Tab{Name: name, Query: c}, p.tab().landing())
	return a.keepQuery(c), true
}

// restoreQuery puts query t, query_1 of the layout started with, on
// the connection and schema its file last ran on (§11): connected, now;
// not yet, once it is, t staying where it opened meanwhile. One gone from
// connections.toml leaves t as it is; a schema gone, gotCatalog's fallback
// takes it back. New queries go on the current connection (newQuery).
func (a *App) restoreQuery(t *queryTab) tea.Cmd {
	st, ok := a.state.Queries[t.name]
	i := slices.IndexFunc(a.sess.Conns, func(c *Conn) bool { return c.Spec.Name == st.Conn })
	if !ok || i < 0 {
		return nil
	}
	c := a.sess.Conns[i].inDB(st.DB)
	switch {
	case c.State == connReady, c == t.conn: // the one it is on is the start, connecting: a failed dial must not lose the schema (F12.5)
		t.conn, t.schema = c, cmp.Or(st.Schema, c.Schema)
		return nil
	}
	t.want, t.wantSchema = c, st.Schema
	return a.connect(t.want)
}

// pickConn is query t's connection dropdown taking c (§7.8「query
// 标题」): not while t's own is in a transaction, nor while t runs (a run
// may begin one); one not connected connects first, t going over once it
// is.
func (a *App) pickConn(t *queryTab, c *Conn) tea.Cmd {
	t.want = nil
	why := t.stayReason()
	switch {
	case c == t.conn:
		return nil
	case why != "":
		return a.showToast(why)
	case c.State == connReady:
		return a.switchConn(t, c, c.Schema)
	}
	t.want, t.wantSchema = c, ""
	return a.connect(c)
}

// stayReason is why query t stays on its connection, "" for none: its
// own in a transaction, or t running, a run that may begin one (§7.8).
func (t *queryTab) stayReason() string {
	switch {
	case t.running != nil:
		return i18n.T("正在执行，结束后再换连接")
	case t.conn.inTx():
		return i18n.T(txOpen)
	}
	return ""
}

// switchConn puts query t on connection c and schema: its next run runs
// there; its file keeps them (§11).
func (a *App) switchConn(t *queryTab, c *Conn, schema string) tea.Cmd {
	t.conn, t.schema, t.comp, t.asked = c, schema, nil, nil
	return a.keepQuery(t)
}

// keepQuery writes where query t runs to the state, by its file
// (§11, §14).
func (a *App) keepQuery(t *queryTab) tea.Cmd {
	if a.state.Queries == nil {
		a.state.Queries = map[string]config.QueryState{}
	}
	a.state.Queries[t.name] = config.QueryState{Conn: t.conn.Spec.Name, DB: t.conn.Spec.Database, Schema: t.schema}
	return a.saveState()
}

// queryOpen is whether a query of any session but except is open on the
// file name is: two on one file would write over each other's autosaves
// (§11). The file systems of macOS and Windows tell names apart by more
// than case no better than that, so neither does this.
func (a *App) queryOpen(name string, except *queryTab) bool {
	return slices.ContainsFunc(a.queries(), func(c *queryTab) bool { return c != except && strings.EqualFold(c.name, name) })
}

// sessionOf is the session c is a connection of: an answer may be for one
// not attached, or one closed, nil then (F5.1).
func (a *App) sessionOf(c *Conn) *Session {
	for _, s := range a.sessions {
		if slices.Contains(s.conns(), c) {
			return s
		}
	}
	return nil
}

// queries is every session's queries: an answer may be for a session not
// attached (F5.1).
func (a *App) queries() []*queryTab {
	var out []*queryTab
	for _, s := range a.sessions {
		out = append(out, s.queries()...)
	}
	return out
}

// freeQuery is the first of query_1, query_2… no session has open and
// no file keeps: a closed query stays in the Queries list till it is
// deleted there (§11, F8.21).
func (a *App) freeQuery() string {
	return freeQueryName(func(name string) bool { return a.queryOpen(name, nil) })
}

// freeQueryName is the first of query_1, query_2… taken says is not, and that has no file.
func freeQueryName(taken func(string) bool) string {
	for n := 1; ; n++ {
		if name := config.QueryName(n); !taken(name) && !config.QueryExists(name) {
			return name
		}
	}
}

// Close closes every session's connections, as the program ends.
func (a *App) Close() {
	for _, s := range a.sessions {
		s.Close()
	}
}

// panes is the session's panes but the sidebars, in every window.
func (s *Session) panes() []*Pane {
	var ps []*Pane
	for _, w := range s.Windows {
		if w.Root != nil {
			ps = append(ps, w.Root.Leaves()...)
		}
	}
	return ps
}

// queries is the session's queries, in every window.
func (s *Session) queries() []*queryTab { return queriesIn(s.panes()...) }

// queriesIn is the queries of panes.
func queriesIn(panes ...*Pane) []*queryTab {
	var out []*queryTab
	for _, p := range panes {
		for _, t := range p.Tabs {
			if t.Query != nil {
				out = append(out, t.Query)
			}
		}
	}
	return out
}

// paneShowing is the pane of the window whose current tab is t; nil when
// t is not on screen.
func (a *App) paneShowing(t *dataTab) *Pane {
	for _, p := range a.win().Root.Leaves() {
		if dataOf(p) == t {
			return p
		}
	}
	return nil
}

// moveTab is tab.move.next (d 1) and tab.move.prev (-1): the focused pane's
// current tab goes to the next or previous ordinary pane going clockwise
// (pane.focus.next's order) and becomes its current tab, the focus after it;
// the pane it leaves empty is a landing page. Nothing moves from the sidebar,
// the result area or a landing page, or with one ordinary pane on screen,
// as when one is zoomed; nor does a
// table to a pane that has it (F3.37), which says so: a merge would lose
// the changes of one (F12.5).
func (a *App) moveTab(d int) tea.Cmd {
	win, src := a.win(), a.focused()
	if src.tab().landing() {
		return nil
	}
	var ids []int
	for _, id := range a.clockwise() {
		if id != win.Tree.ID && (win.Result == nil || id != win.Result.ID) {
			ids = append(ids, id)
		}
	}
	i := slices.Index(ids, src.ID) // not the sidebar, nor the result area
	if i < 0 || len(ids) < 2 {
		return nil
	}
	dst := win.pane(ids[(i+d+len(ids))%len(ids)])
	tb := src.Tabs[src.Cur]
	if t := tb.Data; t != nil && dst.tableTab(t.conn, t.table) >= 0 {
		return a.showToast(i18n.T("目标 pane 里已有这张表"))
	}
	dropTab(src, src.Cur)
	putTab(dst, tb, dst.tab().landing()) // a landing tab there gives way to it (§5「引导页」)
	win.focus(dst.ID)
	return nil
}

// closeTab closes pane p's tab i: the current one (:q) gives way to the
// previous, as vim's alternate; another leaves the current and previous
// tabs as they were. Closing the last tab closes the pane too, except the
// sidebar and the window's only pane; the result area's log stays (§11).
func (a *App) closeTab(p *Pane, i int) {
	if p == a.win().Tree || i < 0 || i >= len(p.Tabs) || p.Tabs[i].Result != nil && p.Tabs[i].Result.run == nil {
		return
	}
	a.sess.recordClosed(a.win().ID, p.ID, p.Tabs[i])
	dropTab(p, i)
	if len(p.Tabs) == 0 {
		a.removePane(p.ID)
	}
}

// dropTab takes tab i out of p, the pane staying: when it was the current
// one, the one used before it gives way to take its place (vim's alternate),
// else the one that now stands where it did (T-01).
func dropTab(p *Pane, i int) {
	p.Tabs = slices.Delete(p.Tabs, i, i+1)
	switch {
	case i != p.Cur:
		if p.Cur > i {
			p.Cur--
		}
		if p.Prev == i {
			p.Prev = -1
		} else if p.Prev > i {
			p.Prev--
		}
	case p.Prev > i:
		p.Cur, p.Prev = p.Prev-1, -1
	case p.Prev >= 0 && p.Prev != i:
		p.Cur, p.Prev = p.Prev, -1
	default:
		p.Cur, p.Prev = max(min(i, len(p.Tabs)-1), 0), -1
	}
}

// closeTabAsking is x on pane p's tab i (F3.35): a query's is written
// first, asked about only when its connection's transaction would go with
// it (§10.5, §11); a table's with changes asks first.
func (a *App) closeTabAsking(p *Pane, i int) tea.Cmd {
	return a.closeTabThen(p, i, func() tea.Cmd { return nil })
}

// closeTabThen is closeTabAsking, then runs after once the tab is closed.
func (a *App) closeTabThen(p *Pane, i int, after func() tea.Cmd) tea.Cmd {
	if i < 0 || i >= len(p.Tabs) { // tab.close.at's i may come from a user's map
		return nil
	}
	if t := p.Tabs[i].Query; t != nil {
		return a.closeAsking(0, "", []*queryTab{t}, func() tea.Cmd {
			if err := t.flush(); err != nil {
				return a.saveFailed(err)
			}
			a.closeTab(p, i)
			return after()
		})
	}
	n, name := 0, ""
	if t := p.Tabs[i].Data; t != nil {
		n, name = t.changes(), t.table.Name+" "
	}
	return a.unlessLost(n, name, nil, verbClose, func() tea.Cmd { a.closeTab(p, i); return after() })
}

// removePane takes pane id out of the tree: its sibling gets the space and
// the focus, and any zoom ends. The window's last pane besides the result
// area stays. The result area goes with its tabs, pinned or not, its
// height kept for the next time (§11).
func (a *App) removePane(id int) { a.win().removePane(id) }

// removePane is App.removePane in window w, whichever it is.
func (w *Window) removePane(id int) {
	if w.onlyPane(id) {
		return
	}
	if res := w.Result; res != nil && id == res.ID {
		for _, tb := range res.Tabs { // what a run going on took the place of goes too
			if r := tb.Result.run; r != nil {
				r.prev = nil
			}
		}
		w.resultRatio, w.Result = w.Root.Ratio, nil // it is the root's lower half
	}
	if root, heir := w.Root.remove(id); root != nil {
		w.Root, w.Zoom = root, 0
		w.focus(heir.ID)
	}
}

// onlyPane reports whether pane id is the window's one pane besides the
// result area, which stays (§5).
func (w *Window) onlyPane(id int) bool {
	n := len(w.Root.Leaves())
	if w.Result != nil && !w.resultHidden {
		if id == w.Result.ID {
			return false
		}
		n--
	}
	return n == 1
}

// panesByNumber is ⟨n⟩ order: the sidebar is 0, then the tree's leaves.
func (a *App) panesByNumber() []*Pane {
	return append([]*Pane{a.win().Tree}, a.win().Root.Leaves()...)
}

// focusSide moves focus to the neighbouring pane on side, from the rects the
// last frame was drawn with (tech-design §5). A folded sidebar takes no focus.
func (a *App) focusSide(side string) {
	win := a.win()
	rects := a.layout()
	if !win.TreeOpen {
		delete(rects, win.Tree.ID)
	}
	if id, ok := neighbor(rects, win.Focus, side, a.recent); ok {
		win.focus(id)
	}
}

// focusNext is pane.focus.next: the pane after the focused one in
// clockwise, the last giving way to the first (§6.8, F5.16).
func (a *App) focusNext() {
	ids := a.clockwise()
	if i := slices.Index(ids, a.win().Focus); i >= 0 {
		a.win().focus(ids[(i+1)%len(ids)])
	}
}

// clockwise is the IDs of the window's panes on screen going round the
// middle of the screen, the status bar in it, as each pane's middle lies
// from 12 o'clock; a tie is by ⟨n⟩, tmux's order (§6.8, F5.16). Whole
// numbers, doubled so a middle is one, which keeps two on one ray equal.
func (a *App) clockwise() []int {
	win := a.win()
	rects := a.layout()
	if !win.TreeOpen {
		delete(rects, win.Tree.ID)
	}
	type vec struct{ x, y int } // from the middle, y up
	at := map[int]vec{}
	var ids []int
	for _, p := range a.panesByNumber() {
		if r, ok := rects[p.ID]; ok {
			at[p.ID] = vec{r.Min.X + r.Max.X - a.w, a.h - (r.Min.Y + r.Max.Y)}
			ids = append(ids, p.ID)
		}
	}
	half := func(v vec) int { // 0 from 12 o'clock to before 6, 1 from 6 to before 12
		switch {
		case v.x == 0 && v.y == 0: // on the middle: it has no direction, and is first
			return -1
		case v.x > 0 || v.x == 0 && v.y > 0:
			return 0
		}
		return 1
	}
	slices.SortStableFunc(ids, func(x, y int) int {
		u, v := at[x], at[y]
		if c := cmp.Compare(half(u), half(v)); c != 0 {
			return c
		}
		return cmp.Compare(u.x*v.y, u.y*v.x) // v clockwise of u: the cross product u × v is negative
	})
	return ids
}

// recent reports whether pane x beats y as the one focused most recently:
// moving by direction (§5) and opening a table from the tree (§12) pick by
// it. Of two never focused, the one first in ⟨n⟩ order (up / left) wins.
func (a *App) recent(x, y int) bool {
	win := a.win()
	if win.focusedAt[x] != win.focusedAt[y] {
		return win.focusedAt[x] > win.focusedAt[y]
	}
	ps := a.panesByNumber()
	at := func(id int) int { return slices.IndexFunc(ps, func(p *Pane) bool { return p.ID == id }) }
	return at(x) < at(y)
}

// splitPane divides the focused pane along d; the new, empty pane gets focus.
func (a *App) splitPane(d Dir) {
	win := a.win()
	p := a.focused()
	if p == win.Tree || p == win.Result { // one result area a window, at the bottom (§11)
		return
	}
	win.lastID++ // never reused, so pane IDs stay stable (§5)
	np := &Pane{ID: win.lastID, Prev: -1}
	win.Root, win.Zoom = win.Root.split(p.ID, d, np), 0
	win.focus(np.ID)
}

// closePane closes the focused pane; its sibling takes the space. The
// sidebar and the window's only pane stay.
func (a *App) closePane() {
	win := a.win()
	if win.Focus != win.Tree.ID {
		a.removePane(win.Focus)
	}
}

// resizeStep is how far one SPC H/J/K/L moves a border.
const resizeStep = 0.05

func (a *App) resizePane(d Dir, sign float64, count int) {
	win := a.win()
	if win.Focus == win.Tree.ID {
		return
	}
	win.Root = win.Root.resize(win.Focus, d, sign*resizeStep*float64(max(count, 1)))
}

func (a *App) toggleZoom() {
	win := a.win()
	switch {
	case win.Zoom != 0:
		win.Zoom = 0
	case win.Focus != win.Tree.ID:
		win.Zoom = win.Focus
	}
}

// toggleTree is SPC b, as DataGrip's tool windows: the sidebar open with the
// focus on it folds, the focus going back to the pane it came from; open with
// the focus elsewhere it is the focus that goes to it; folded it opens with the
// focus on it, its cursor on the node of the tab focused before (§7.8, F3.29,
// F19.5). A zoomed pane hides it: the zoom ends first.
func (a *App) toggleTree() {
	win := a.win()
	win.Zoom = 0
	if win.TreeOpen && win.Focus == win.Tree.ID {
		a.foldTree()
		return
	}
	win.TreeOpen = true
	a.revealTab(a.focused())
	win.focus(win.Tree.ID)
}

// foldTree folds the sidebar; the focus on it goes back to the pane it came
// from, the window's current one when that is closed (§7.8, F19.5).
func (a *App) foldTree() {
	win := a.win()
	win.TreeOpen = false
	if win.Focus != win.Tree.ID {
		return
	}
	if p := win.pane(win.beforeTree); win.beforeTree != win.Tree.ID && p != nil {
		win.focus(p.ID)
		return
	}
	win.focus(a.openTarget().ID)
}

// jumpToPane is SPC q's second key: a digit picks ⟨n⟩, anything else just
// closes the numbers.
func (a *App) jumpToPane(k keymap.Key) {
	a.paneNumbers = false
	n, err := strconv.Atoi(string(k))
	ps := a.panesByNumber()
	if err != nil || n < 0 || n >= len(ps) || n == 0 && !a.win().TreeOpen {
		return
	}
	a.win().Zoom = 0
	a.win().focus(ps[n].ID)
}

// showPane focuses pane id where the palette sends the user (§12). A zoom on
// another pane ends first, as a jump by number ends it (§5): the pane is on
// screen once it has focus.
func (a *App) showPane(id int) {
	if a.win().Zoom != id {
		a.win().Zoom = 0
	}
	a.focusPane(id)
}

// focusPane gives focus to pane id if it is on screen (a click).
func (a *App) focusPane(id int) {
	if _, ok := a.layout()[id]; ok && (id != a.win().Tree.ID || a.win().TreeOpen) {
		a.win().focus(id)
	}
}

// wheelStep is how many rows one wheel notch scrolls (§7.4).
const wheelStep = 3

// scrollPane scrolls pane id by down notches and right notches (negative:
// up, left), within its content.
func (a *App) scrollPane(id, down, right int) {
	switch p := a.win().pane(id); {
	case p == a.win().Tree:
		a.scrollTree(down)
	case p != nil && resultOf(p) != nil && resultOf(p).run == nil: // the log
		a.logMove(p, func(r, c, _, _ int) (int, int) { return r + down*wheelStep, c })
	case p != nil && queryOf(p) != nil: // not sideways: nowrap scrolls with the cursor (§11)
		c := queryOf(p)
		a.queryView(p, c)
		c.ed.Scroll(down * wheelStep)
		c.comp = nil
	case p != nil && jsonViewOf(p) != nil: // a JSON view scrolls as a text, not sideways (F14.5)
		jv := a.jsonSync(p)
		a.jsonSize(p, jv)
		jv.ed.Scroll(down * wheelStep)
	case p != nil:
		a.scrollGrid(p, down*wheelStep, right)
	}
}

// closedTab is a table or a query tab that was closed, as it was (F5.16).
type closedTab struct {
	win, pane int // the window and pane it was in, by ID
	conn      *Conn
	table     db.Table // a table's, with the request it had
	req       request
	turned    bool   // a table's: it was turned
	query     string // the query's name, "" for a table
	schema    string
}

// closedKept is how many closed tabs tab.reopen goes back through.
const closedKept = 20

// recordClosed notes tab, closed in pane of window, for tab.reopen: a table
// or a query, no landing tab or result.
func (s *Session) recordClosed(win, pane int, tab Tab) {
	var c closedTab
	switch {
	case tab.Data != nil:
		d := tab.Data
		c = closedTab{conn: d.conn, table: d.table, req: request{applied: d.applied, order: d.order, desc: d.desc, limit: d.limit}, turned: d.transpose}
	case tab.Query != nil:
		c = closedTab{conn: tab.Query.conn, query: tab.Query.name, schema: tab.Query.schema}
	default:
		return
	}
	c.win, c.pane = win, pane
	s.closed = append(s.closed, c)
	if len(s.closed) > closedKept {
		s.closed = s.closed[1:]
	}
}

// reopenTab is tab.reopen: the tab closed last, in its pane when it is
// still there, else in the current one, a table with the WHERE, ORDER and
// LIMIT it had, a query on its file, connection and schema. One that is
// open already is only shown (F3.37, §11); no tab to reopen is no change.
func (a *App) reopenTab() tea.Cmd {
	s := a.sess
	if len(s.closed) == 0 {
		return nil
	}
	c := s.closed[len(s.closed)-1]
	s.closed = s.closed[:len(s.closed)-1]
	if c.query != "" && a.focusQuery(c.query) {
		return nil
	}
	p := a.openTarget()
	if wi := s.windowByID(c.win); wi >= 0 && s.Windows[wi].pane(c.pane) != nil {
		a.selectWindow(wi)
		p = s.Windows[wi].pane(c.pane)
	}
	if c.query == "" {
		return a.openTableAs(p, c.conn, c.table, &c.req, c.turned)
	}
	t, err := openQuery(c.conn, c.query)
	if err != nil {
		return a.showToast(err.Error())
	}
	t.schema = c.schema
	a.showPane(p.ID)
	putTab(p, Tab{Name: c.query, Query: t}, p.tab().landing())
	return a.keepQuery(t)
}

// focusQuery goes to the tab of the query called name, in whichever
// window of whichever session has it open, that session attached; false when
// none has (F5.16, F8.21).
func (a *App) focusQuery(name string) bool {
	for _, s := range a.sessions {
		for wi, w := range s.Windows {
			for _, p := range w.Root.Leaves() {
				if i := slices.IndexFunc(p.Tabs, func(tb Tab) bool { return tb.Query != nil && strings.EqualFold(tb.Query.name, name) }); i >= 0 {
					if s != a.sess {
						a.attach(s)
					}
					a.selectWindow(wi)
					a.showTab(p, i)
					return true
				}
			}
		}
	}
	return false
}

// quitTab is tab.quit: the current tab closed as tab.close does, and when
// that leaves the window's only pane without a tab, the window too; an
// empty pane is closed as it is (F5.16). The session's last window stays.
func (a *App) quitTab() tea.Cmd {
	p, win := a.focused(), a.win()
	if p == win.Tree {
		return nil
	}
	sweep := func() tea.Cmd {
		if win != a.win() || win.pane(p.ID) != p || len(p.Tabs) > 0 {
			return nil
		}
		if win.onlyPane(p.ID) {
			return a.closeWindow()
		}
		a.removePane(p.ID)
		return nil
	}
	if len(p.Tabs) == 0 {
		return sweep()
	}
	return a.closeTabThen(p, p.Cur, sweep)
}

// pickedSchemas is the schemas of all that the connection's schemas setting names for
// database db (PG writes `db.schema`; MySQL's are plain names, pg false), nil for all
// when it names none: what the catalog's queries are limited to (F17.13).
// the schema keep, which names go to, is always one of them.
// ponytail: a database with none of its schemas picked reads as all of them; and with
// databases `my` and `my.db`, an entry `my.db.x` is read as `my`'s too
func pickedSchemas(setting []string, pg bool, db string, all []string, keep string) []string {
	var mine []string
	for _, e := range setting {
		if !pg {
			mine = append(mine, e)
		} else if s, ok := strings.CutPrefix(e, db+"."); ok {
			mine = append(mine, s)
		}
	}
	if len(mine) == 0 {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(all), func(s string) bool { return s != keep && !slices.Contains(mine, s) })
}

// pickedDatabases is the databases of all that the connection's databases setting names,
// with the one it is dialed to, which is always there; all when it names none (F17.13).
func pickedDatabases(setting []string, dsn string, all []string) []string {
	if len(setting) == 0 {
		return all
	}
	return slices.DeleteFunc(slices.Clone(all), func(d string) bool { return d != dsn && !slices.Contains(setting, d) })
}
