package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
	"github.com/kackerx/sqlmux/internal/ui"
)

// resultTab is a tab of the result area (§11): a result set of a run, the
// placeholder while the run goes on, or the log, which has no run.
type resultTab struct {
	gridState // the log's top is its first line on screen
	run       *run
	part      int // which of the run's result sets, from 1
	pinned    bool
	arrived   time.Time // when the run's rows came in, or a rerun's: what the cap goes by (§11)
	// the page (F10.4): which statement of the run it is of, how many rows
	// come before it, whether the statement can be paged, a page on its way
	stmt     int
	offset   int
	pageable bool
	paging   bool
	// mem is a Redis reply, whole, of which the grid shows the page at offset:
	// there is no cursor to ask for the next, so it pages in memory (§11)
	mem *db.Result
}

// memPage shows the rows of mem from offset, maxRows of them, as a page of a
// statement does: Truncated when more follow.
func (rt *resultTab) memPage(offset, maxRows int) {
	page := *rt.mem
	page.Rows = page.Rows[offset:min(offset+maxRows, len(page.Rows))]
	page.Truncated = offset+maxRows < len(rt.mem.Rows)
	rt.gridState = gridState{page: page, transpose: rt.transpose, jv: rt.jv}
	rt.offset = offset
}

// run is one run of a query's SQL (§11「执行」), or a quick SQL's rows
// C-t put in the result area, from no query (§12).
type run struct {
	from   *queryTab
	conn   *Conn  // where it ran, and runs again (§5)
	schema string // first on search_path as it ran
	name   string // the query's tab name as it ran: query_1
	sql    string // what ran, for 重跑
	base   int    // where sql is in the query's text, for the red ▶
	ver    int    // the query's version base is of: a ▶ goes red only while the text is still that
	seq    int    // Session.RunSeq: #42
	start  time.Time
	win    *Window
	stmts  []sqlkit.Stmt
	lang   lang // the connection's as it began: mainRun reads it off the UI goroutine
	// whether each statement was given its LIMIT and can be asked for the next page (F10.4)
	pageable []bool
	done     bool
	prev     []Tab // the tabs it took the place of, back when it has no result set
}

// label is the name of the run's part-th result tab: query_1 #42, then #42·2.
func (r *run) label(part int) string {
	s := fmt.Sprintf("%s #%d", r.name, r.seq)
	if part > 1 {
		s += fmt.Sprintf("·%d", part)
	}
	return s
}

func resultOf(p *Pane) *resultTab {
	if t := p.tab(); t != nil {
		return t.Result
	}
	return nil
}

// resultPane is window w's result area, made at its bottom when it has
// none: its old height, or result_height the first time (§11). One SPC r
// hid stays hidden.
func (a *App) resultPane(w *Window) *Pane {
	if w.Result == nil {
		w.lastID++
		w.Result = &Pane{ID: w.lastID, Tabs: []Tab{{Name: i18n.T("日志"), Result: &resultTab{}}}, Prev: -1}
		a.putResult(w)
	}
	return w.Result
}

// putResult puts w's result area at its bottom, the old height kept.
func (a *App) putResult(w *Window) {
	w.Root = &Node{Split: Vert, Ratio: cmp.Or(w.resultRatio, 1-a.resultHeight), A: w.Root, B: leaf(w.Result)}
}

// showResult is resultPane, one SPC r hid back as it was: a run shows it.
func (a *App) showResult(w *Window) *Pane {
	p := a.resultPane(w)
	if w.resultHidden {
		w.resultHidden = false
		a.putResult(w)
	}
	return p
}

// toggleResult is SPC r (§11): the result area out of the layout, its
// tabs kept, or back as it was; nothing without one. Hidden with the
// focus in it, the focus goes back to the pane focused before.
func (a *App) toggleResult() {
	switch w := a.win(); {
	case w.Result == nil:
	case w.resultHidden:
		a.showResult(w)
	default:
		w.resultRatio, w.Root, w.resultHidden = w.Root.Ratio, w.Root.A, true // it is the root's lower half
		if w.Zoom == w.Result.ID {
			w.Zoom = 0
		}
		if w.Focus == w.Result.ID {
			var prev *Pane
			for _, p := range a.panesByNumber() {
				if (p != w.Tree || w.TreeOpen) && (prev == nil || a.recent(p.ID, prev.ID)) {
					prev = p
				}
			}
			w.focus(prev.ID)
		}
	}
}

type (
	runDone struct {
		r   *run
		rs  []db.Result
		err error
		set string // the SET of search_path, MySQL's USE, when it is what failed
		tx  byte   // where Main's transaction was as the run ended (F5.11)
	}
)

// queryRun is ↵ in a query (§11「执行」), a click on a ▶ on line arg:
// what querySpan says.
func (a *App) queryRun(arg string) tea.Cmd {
	p := a.focused()
	t := queryOf(p)
	if t == nil || t.running != nil { // a run at a time a query
		return nil
	}
	text, from, to := querySpan(t, arg)
	if from < 0 {
		return nil
	}
	return a.runSQL(t, t.conn, t.schema, p.Object(), text[from:to], from, t.ver)
}

// querySpan is what ↵ takes of query t (§11): the statement under the
// cursor, the one a ▶ on line arg starts, or what VISUAL selects, a block
// by its whole lines as V-LINE; VISUAL then ends, the cursor where it is.
// from and to are offsets into text, the lines joined; from is -1 for
// nothing.
func querySpan(t *queryTab, arg string) (text string, from, to int) {
	ed := t.ed
	lines := ed.Lines()
	text = strings.Join(lines, "\n")
	at := func(pos editor.Pos) int { return offsetOf(lines, pos) }
	stmts := t.conn.lang().Stmts(text)
	from, to = -1, -1
	switch s, e, visual := ed.Selection(); {
	case arg != "":
		if n, _ := strconv.Atoi(arg); n >= 1 && n <= len(lines) {
			if i := t.conn.lang().At(text, stmts, at(editor.Pos{Line: n - 1})); i >= 0 {
				from, to = stmts[i].Start, stmts[i].End
			}
		}
	case visual && ed.Plain(): // no mode, and the span is what lies between the cursor places
		s, e, _ = ed.SelectionSpan()
		from, to = at(s), at(e)
	case visual && ed.Mode() == editor.Visual:
		from, to = at(s), at(e)
		if l := lines[e.Line]; e.Col < len(l) { // the last character is in
			gr, _ := ansi.FirstGraphemeCluster(l[e.Col:], ansi.GraphemeWidth)
			to += len(gr)
		} else {
			to = min(to+1, len(text)) // and so is the line break
		}
	case visual:
		from, to = at(editor.Pos{Line: s.Line}), at(editor.Pos{Line: e.Line, Col: len(lines[e.Line])})
	default:
		if i := t.conn.lang().At(text, stmts, at(ed.Cursor())); i >= 0 {
			from, to = stmts[i].Start, stmts[i].End
		}
	}
	if _, _, visual := ed.Selection(); visual {
		if ed.Plain() {
			ed.ClearSelection()
		} else {
			ed.Feed("<Esc>")
		}
	}
	return text, from, to
}

// runSQL runs sql of query t, named name, which is at base in its text
// at version ver: in conn's Main, schema first on search_path, a statement
// at a time, each committing on its own, stopping at the first that fails
// (§11); in the manual mode in a transaction, begun before a statement
// when there is none, as psql's AUTOCOMMIT off (F5.3). Reads get a LIMIT
// a row past max_rows, to tell there are more. The result area shows a
// placeholder meanwhile; the query's red ▶ goes until this run fails.
// On a read-only connection a statement it keeps back keeps the whole run
// back, the log saying so (§13); on any other, dangerous statements are
// asked about, once for the run, before it begins.
func (a *App) runSQL(t *queryTab, conn *Conn, schema, name, sql string, base, ver int) tea.Cmd {
	l := conn.lang()
	stmts := l.Stmts(sql)
	if len(stmts) == 0 || t.running != nil {
		return nil
	}
	if conn.State != connReady { // a layout's query, its connection not up yet (F6.5)
		return a.notReady(conn)
	}
	if !conn.sql() && !conn.cmdsReady { // what writes and what is dangerous is not known yet (§13)
		return a.showToast(i18n.T("还在读 %s 的命令列表", conn.label()))
	}
	if conn.readOnly {
		if i := slices.IndexFunc(stmts, func(s sqlkit.Stmt) bool { return l.Writes(sql[s.Start:s.End]) }); i >= 0 {
			p := a.showResult(a.win())
			a.log(&run{name: name, win: a.win()}, sql[stmts[i].Start:stmts[i].End], i18n.T("%s 是只读连接，整次都没有执行", conn.Spec.Name), true)
			selectTab(p, 0)
			return nil
		}
	}
	if a.confirmDangerous || conn.Spec.Production { // a production connection asks about every write (F17.9)
		var risky []string
		for _, s := range stmts {
			if text := sql[s.Start:s.End]; (a.confirmDangerous && l.Dangerous(text)) || (conn.Spec.Production && l.Writes(text)) {
				risky = append(risky, text)
			}
		}
		if len(risky) > 0 { // no run is begun, no log line made, till the answer is yes (§13)
			asked := t.conn // a connection that lands meanwhile may take the query over
			a.confirm = writeBox(conn, dangerText(risky, conn.Spec.Production), i18n.T("执行"), true, func() tea.Cmd {
				if t.conn != asked {
					return a.showToast(i18n.T("确认时 query 换了连接，没有执行"))
				}
				return a.startRun(t, conn, schema, name, sql, base, ver, stmts) // the overlay had the keys: nothing began a run meanwhile
			})
			return nil
		}
	}
	return a.startRun(t, conn, schema, name, sql, base, ver, stmts)
}

// What the confirm box names of dangerous statements: this many, each cut to
// this many cells.
const (
	dangerousListed = 8
	dangerousWidth  = 60
)

// dangerText is the confirm box over a run with the dangerous statements
// risky: how many, each with its white space (lines, tabs) squeezed to a
// space and cut to a width, and how many more there are than it lists (§13).
func dangerText(risky []string, production bool) string {
	head := i18n.T("这次执行有 %d 条危险语句：", len(risky))
	if production { // every write, not the dangerous ones alone (F17.9)
		head = i18n.T("这次执行有 %d 条写语句：", len(risky))
	}
	lines := []string{head}
	for _, s := range risky[:min(len(risky), dangerousListed)] {
		lines = append(lines, "  "+ui.Truncate(strings.Join(strings.Fields(s), " "), dangerousWidth))
	}
	if len(risky) > dangerousListed {
		lines = append(lines, i18n.T("… 另有 %d 条", len(risky)-dangerousListed))
	}
	return strings.Join(lines, "\n")
}

// startRun is runSQL once it is settled that the statements go: the run
// begun, its placeholder shown, the statements sent.
func (a *App) startRun(t *queryTab, conn *Conn, schema, name, sql string, base, ver int, stmts []sqlkit.Stmt) tea.Cmd {
	l := conn.lang()
	a.sess.RunSeq++
	r := &run{from: t, conn: conn, schema: schema, name: name, sql: sql, base: base, ver: ver, seq: a.sess.RunSeq, start: time.Now(), win: a.win(), stmts: stmts, lang: l}
	t.running, t.failed, t.bar = r, -1, nil
	p := a.showResult(r.win)
	at, old := a.placeRun(r, []Tab{{Name: r.label(1), Result: &resultTab{run: r}}})
	r.prev = old
	selectTab(p, at)
	a.capResults(p)
	r.conn.busy++
	texts := make([]string, len(stmts))
	r.pageable = make([]bool, len(stmts))
	// ponytail: select set_config('search_path', …) goes by unseen (its first
	// word is select): the next run then trusts mainPath still, and a page of
	// a statement after it is asked with the query's schema again, which may
	// be another table
	resets := false // what search_path, or MySQL's database, is past this run is not known (§8.6)
	for i, s := range stmts {
		texts[i], r.pageable[i] = l.Page(sql[s.Start:s.End], a.maxRows+1, 0)
		r.pageable[i] = r.pageable[i] && !resets // after a SET or USE of the run a page would ask for the schema again, and read another table
		resets = resets || l.Resets(texts[i])
	}
	maxRows, manual := a.maxRows, r.conn.Manual
	if !conn.sql() { // a reply is held whole, and paged in memory (§11)
		maxRows = 0
	}
	return func() tea.Msg { return mainRun(r, texts, maxRows, manual, resets) }
}

// mainRunner is the way a request of the statements texts goes on Main: one in
// which every statement is known to have no effect to repeat, a lost connection
// is dialed again and the request runs again, sent or not; any other is not run
// twice (§8.7).
func mainRunner(main *db.Worker, l lang, texts ...string) func(context.Context, func(context.Context, db.Conn) error) error {
	if slices.ContainsFunc(texts, func(text string) bool { return !l.Reruns(text) }) {
		return main.Run
	}
	return main.RunSafe
}

// mainRun runs texts in r's connection's Main, as one request: schema first
// on search_path, a statement at a time, each committing on its own, in the
// manual mode in a transaction begun before one when there is none. resets
// is whether what is run leaves search_path or the database not known. It
// is what a run and a page of a result are sent as (F10.4); it goes on the
// request's goroutine, so r and what it holds are read, not changed.
func mainRun(r *run, texts []string, maxRows int, manual, resets bool) runDone {
	s, l := r.conn, r.lang
	m := runDone{r: r}
	run := mainRunner(s.Main, l, texts...)
	m.err = run(context.Background(), func(ctx context.Context, c db.Conn) error {
		defer func() { m.tx = db.TxStatus(c) }()
		var err error
		if s.sql() {
			if m.set, err = s.mainSetup(ctx, c, r.schema); err != nil {
				return err
			}
		}
		var begin func(string) bool // after the SET, which is kept so
		if manual {
			begin = func(stmt string) bool { return !l.NoBegin(stmt) }
		}
		m.rs, err = db.ExecEach(ctx, c, texts, maxRows, begin)
		if resets || db.TxStatus(c) != 'I' {
			s.mainPath = ""
		}
		return err
	})
	if errors.As(m.err, new(db.TxLost)) { // it is the user's statement that was not run, not the SET
		m.set = ""
	}
	m.rs, m.err = runTxLost(m.rs, m.err, texts, l)
	return m
}

// mainSetup puts schema first on c's search_path (MySQL's database) before a
// statement of a query runs on Main, when it is not there; set is the
// statement that failed, as the log names it (§8.6). mainPath is read and
// written in Main's requests only, one at a time: runs of two queries may
// queue in either order. It is kept out of transactions only: in a failed one
// a SET fails too and the user's rollback would never run; in one going on, a
// rollback may take the SET back.
func (s *Conn) mainSetup(ctx context.Context, c db.Conn, schema string) (set string, err error) {
	set = "set search_path to " // as the log names it when it fails (§8.6)
	if s.eng.Dialect() == sqlkit.MySQL {
		set = "use "
	}
	if s.mainOn != c { // a connection dialed again has the search_path it started with (F8.19)
		s.mainPath, s.mainOn = "", c
	}
	switch tx := db.TxStatus(c); {
	case schema == "":
	case tx == 'E':
		s.mainPath = ""
	case s.mainPath != schema:
		if err := db.Setup(c, func() error { return s.eng.UseSchema(ctx, c, schema, s.startedPath) }); err != nil {
			s.mainPath = ""
			return set + schema, err
		}
		s.mainPath = ""
		if tx == 'I' { // set in a transaction, the run's rollback may take it back
			s.mainPath = schema
		}
	}
	return "", nil
}

// placeRun puts tabs in the result area of r's window in place of the
// tabs of r's query there not pinned, where the first of those was
// (§11); the tab that was current stays so, or the log. It returns where
// tabs went and the tabs taken out.
func (a *App) placeRun(r *run, tabs []Tab) (at int, old []Tab) {
	p := a.resultPane(r.win)
	cur := resultOf(p)
	at = -1
	var kept []Tab
	for _, tb := range p.Tabs {
		if rt := tb.Result; rt != nil && rt.run != nil && rt.run.from == r.from && !rt.pinned {
			if at < 0 {
				at = len(kept)
			}
			old = append(old, tb)
			continue
		}
		kept = append(kept, tb)
	}
	if at < 0 {
		at = len(kept)
	}
	p.Tabs = slices.Insert(kept, at, tabs...)
	p.Cur, p.Prev = max(slices.IndexFunc(p.Tabs, func(tb Tab) bool { return tb.Result == cur }), 0), -1
	return at, old
}

// maxResults is how many result tabs the area keeps besides the log (§11).
const maxResults = 10

// capResults closes the result tabs of p's area beyond maxResults, the one
// whose rows came in longest ago first, a pinned one too, the first of a
// run's sets when they came together (so its last stay); a run going on
// keeps its tab, even if that leaves more (F5.18).
func (a *App) capResults(p *Pane) {
	for len(p.Tabs)-1 > maxResults {
		oldest := -1
		for i := 1; i < len(p.Tabs); i++ {
			rt := p.Tabs[i].Result
			if rt == nil || rt.run == nil || !rt.run.done {
				continue
			}
			if oldest < 0 || rt.arrived.Before(p.Tabs[oldest].Result.arrived) {
				oldest = i
			}
		}
		if oldest < 0 {
			return
		}
		a.closeTab(p, oldest)
	}
}

// gotRun shows what run r did (§11): a line of the log a statement, a
// result tab a result set in place of the placeholder, or what was there
// before when there is none; a failure, logged, shows the log and turns
// its statement's ▶ red; a cancel says so. After DDL the catalog loads
// again.
func (a *App) gotRun(m runDone) tea.Cmd {
	r := m.r
	if a.sessionOf(r.conn) == nil { // its session closed, its connection with it: nothing to say (F5.1)
		return nil
	}
	r.conn.busy--
	if m.tx != 0 { // 0: it never got Main
		r.conn.tx = m.tx
	}
	r.done, r.from.running = true, nil
	var tabs []Tab
	var cmds []tea.Cmd
	ddl := false
	for i, res := range m.rs {
		stmt := r.sql[r.stmts[i].Start:r.stmts[i].End]
		a.log(r, stmt, resultText(res, 0), false)
		recordHistory(r, stmt, res, "")
		if res.Cols != nil {
			gs := gridState{page: res}
			if j := slices.IndexFunc(r.prev, func(tb Tab) bool { return tb.Result != nil && tb.Result.stmt == i }); j >= 0 { // the tab it replaces, turned or in JSON, stays so
				gs.transpose, gs.jv = r.prev[j].Result.transpose, r.prev[j].Result.jv
			}
			rt := &resultTab{gridState: gs, run: r, part: len(tabs) + 1, stmt: i, pageable: r.pageable[i]}
			if !r.conn.sql() && len(res.Rows) > a.maxRows {
				rt.mem = &res
				rt.memPage(0, a.maxRows)
			}
			tabs = append(tabs, Tab{Name: r.label(len(tabs) + 1), Result: rt})
		}
		ddl = ddl || r.conn.lang().DDL(stmt)
		r.conn.forgetHints(stmt)
	}
	if m.err != nil {
		s := r.stmts[len(m.rs)]
		stmt := r.sql[s.Start:s.End]
		if m.set != "" { // none of the run's ran: no ▶ is to blame (§8.6)
			stmt = m.set
		}
		if errors.Is(m.err, context.Canceled) {
			a.log(r, stmt, i18n.T("已取消"), false)
			if m.set == "" {
				recordHistory(r, stmt, db.Result{}, "canceled")
			}
			cmds = append(cmds, a.showToast(i18n.T("查询已取消")))
		} else {
			e, text := a.logError(r, stmt, m.err), strings.Join(r.from.ed.Lines(), "\n")
			if m.set == "" {
				recordHistory(r, stmt, db.Result{}, e.Message)
			}
			var at []string
			if r.from.ver == r.ver && m.set == "" { // the text is still what ran
				r.from.failed = strings.Count(text[:r.base+s.Start], "\n")
				if e.Position > 0 {
					at = append(at, errorAt(text, r.base+s.Start, stmt, e.Position))
				}
			}
			if slices.Contains(a.queries(), r.from) { // closed, the log has it alone
				r.from.bar = newErrorBar("run", e, "", "", at...)
			}
			if errors.Is(m.err, db.ErrCommitUnknown) { // over the connection's "transaction lost", which came first: the commit may have taken
				cmds = append(cmds, a.showToast(m.err.Error()))
			}
			cmds = append(cmds, a.deadlocked(r.conn, m.err))
		}
	}
	now := time.Now()
	for _, tb := range tabs {
		tb.Result.arrived = now
	}
	shown := tabs
	if len(tabs) == 0 {
		shown = r.prev
	}
	a.placeRun(r, shown)
	p := r.win.Result
	selectTab(p, 0) // the log, unless a result set shows
	a.capResults(p)
	if m.err == nil && len(tabs) > 0 { // the first set that is left, if the cap took some
		selectTab(p, slices.IndexFunc(p.Tabs, func(tb Tab) bool { return tb.Result != nil && tb.Result.run == r }))
	}
	if ddl {
		r.conn.dropCols() // as tree.refresh
		cmds = append(cmds, a.loadCatalog(r.conn))
	}
	return tea.Batch(cmds...)
}

// clock is the log's time; the tests stop it.
var clock = time.Now

// log adds a line for statement stmt of run r to its window's log, and
// the log follows it (§11): the time, the query, the statement's first
// line, what it did.
func (a *App) log(r *run, stmt, did string, failed bool) {
	first, _, _ := strings.Cut(stmt, "\n")
	w := r.win
	w.log = append(w.log, ui.LogLine{Head: fmt.Sprintf("%s  %s  %s  ", clock().Format("15:04:05"), r.name, first), Tail: did, Err: failed})
	if p := a.resultPane(w); len(p.Tabs) > 0 {
		p.Tabs[0].Result.top = len(w.log) // the bottom: LogTop keeps it to the last line
	}
}

// logError logs statement stmt of run r failing with err (§11): the
// server's severity and message, then DETAIL and HINT a line each.
func (a *App) logError(r *run, stmt string, err error) db.ServerError {
	e := r.conn.drv.ErrorOf(err)
	a.log(r, stmt, e.Severity+": "+e.Message, true)
	for _, l := range e.More {
		r.win.log = append(r.win.log, ui.LogLine{Tail: "    " + l, Err: true})
	}
	return e
}

// resultText is what a result says in the log and on the result area's
// title: its rows, or its tag, and the time it took (§11). A page that is
// not the first, offset rows in, says which rows it has: 501–1000+ for one
// with more after it, 1001–1203 for the last (F10.4).
func resultText(r db.Result, offset int) string {
	took := r.Took.Round(time.Millisecond).String()
	switch {
	case r.Cols == nil:
		return r.Tag + " · " + took
	case offset > 0 && r.Truncated: // there are rows then
		return fmt.Sprintf(i18n.T("%d–%d+ 行 · %s"), offset+1, offset+len(r.Rows), took)
	case offset > 0 && len(r.Rows) > 0:
		return fmt.Sprintf(i18n.T("%d–%d 行 · %s"), offset+1, offset+len(r.Rows), took)
	case r.Truncated:
		return fmt.Sprintf(i18n.T("%d+ 行 · %s"), len(r.Rows), took)
	}
	return fmt.Sprintf(i18n.T("%d 行 · %s"), len(r.Rows), took)
}

// rerun is 重跑: the SQL of the result tab's run, as a new run of its
// query, or as the quick SQL runs it for a quick #n.
func (a *App) rerun() tea.Cmd {
	rt := resultOf(a.focused())
	if rt == nil || rt.run == nil {
		return nil
	}
	r := rt.run
	if r.from == nil {
		return a.rerunQuick(rt)
	}
	return a.runSQL(r.from, r.conn, r.schema, r.name, r.sql, r.base, r.ver) // base holds only while the text is still r.ver
}
