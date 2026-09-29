package app

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
	"sqlmux/internal/editor"
	"sqlmux/internal/sqlkit"
	"sqlmux/internal/ui"
)

// resultTab is a tab of the result area (§11): a result set of a run, the
// placeholder while the run goes on, or the log, which has no run.
type resultTab struct {
	gridState // the log's top is its first line on screen
	run       *run
	part      int // which of the run's result sets, from 1
	pinned    bool
}

// run is one run of a console's SQL (§11「执行」).
type run struct {
	from  *consoleTab
	name  string // the console's tab name as it ran: console_1
	sql   string // what ran, for 重跑
	base  int    // where sql is in the console's text, for the red ▶
	ver   int    // the console's version as it ran: a ▶ goes red only on the text that ran
	seq   int    // Session.RunSeq: #42
	start time.Time
	win   *Window
	stmts []sqlkit.Stmt
	done  bool
	prev  []Tab // the tabs it took the place of, back when it has no result set
}

// label is the name of the run's part-th result tab: console_1 #42, then #42·2.
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
// none: its old height, or result_height the first time (§11).
func (a *App) resultPane(w *Window) *Pane {
	if w.Result == nil {
		w.lastID++
		w.Result = &Pane{ID: w.lastID, Tabs: []Tab{{Name: "日志", Result: &resultTab{}}}, Prev: -1}
		w.Root = &Node{Split: Vert, Ratio: cmp.Or(w.resultRatio, 1-a.resultHeight), A: w.Root, B: leaf(w.Result)}
	}
	return w.Result
}

type (
	runDone struct {
		r   *run
		rs  []db.Result
		err error
	}
	runTick struct{ r *run }
)

// consoleRun is ↵ in a console (§11「执行」), a click on a ▶ on line arg:
// what consoleSpan says.
func (a *App) consoleRun(arg string) tea.Cmd {
	p := a.focused()
	t := consoleOf(p)
	if t == nil || t.running != nil { // a run at a time a console
		return nil
	}
	text, from, to := consoleSpan(t, arg)
	if from < 0 {
		return nil
	}
	return a.runSQL(t, p.Object(), text[from:to], from)
}

// consoleSpan is what ↵ and gq take of console t (§9.5, §11): the
// statement under the cursor, the one a ▶ on line arg starts, or what
// VISUAL selects, a block by its whole lines as V-LINE; VISUAL then ends,
// the cursor where it is. from and to are offsets into text, the lines
// joined; from is -1 for nothing.
func consoleSpan(t *consoleTab, arg string) (text string, from, to int) {
	ed := t.ed
	lines := ed.Lines()
	text = strings.Join(lines, "\n")
	at := func(pos editor.Pos) int { // pos's offset in text
		off := pos.Col
		for _, l := range lines[:pos.Line] {
			off += len(l) + 1
		}
		return off
	}
	stmts := sqlkit.Statements(text, sqlkit.PG)
	from, to = -1, -1
	switch s, e, visual := ed.Selection(); {
	case arg != "":
		if n, _ := strconv.Atoi(arg); n >= 1 && n <= len(lines) {
			if i := sqlkit.StmtAt(text, stmts, at(editor.Pos{Line: n - 1})); i >= 0 {
				from, to = stmts[i].Start, stmts[i].End
			}
		}
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
		if i := sqlkit.StmtAt(text, stmts, at(ed.Cursor())); i >= 0 {
			from, to = stmts[i].Start, stmts[i].End
		}
	}
	if _, _, visual := ed.Selection(); visual {
		ed.Feed("<Esc>")
	}
	return text, from, to
}

// runSQL runs sql of console t, named name, which is at base in its text:
// in Main, a statement at a time, each committing on its own, stopping at
// the first that fails (§11). Reads get a LIMIT a row past max_rows, to
// tell there are more. The result area shows a placeholder meanwhile.
func (a *App) runSQL(t *consoleTab, name, sql string, base int) tea.Cmd {
	stmts := sqlkit.Statements(sql, sqlkit.PG)
	if len(stmts) == 0 || t.running != nil {
		return nil
	}
	a.sess.RunSeq++
	r := &run{from: t, name: name, sql: sql, base: base, ver: t.ver, seq: a.sess.RunSeq, start: time.Now(), win: a.win(), stmts: stmts}
	t.running = r
	p := a.resultPane(r.win)
	at, old := a.placeRun(r, []Tab{{Name: r.label(1), Result: &resultTab{run: r}}})
	r.prev = old
	selectTab(p, at)
	a.busy++
	texts := make([]string, len(stmts))
	for i, s := range stmts {
		texts[i] = sqlkit.AutoLimit(sql[s.Start:s.End], sqlkit.PG, a.maxRows+1)
	}
	main, maxRows := a.sess.Main, a.maxRows
	return tea.Batch(func() tea.Msg {
		rs, err := main.ExecEach(context.Background(), texts, maxRows)
		return runDone{r, rs, err}
	}, a.runTick(r))
}

// runTickEvery is how often a placeholder's time is redrawn while its run
// goes on (§11).
var runTickEvery = time.Second

func (a *App) runTick(r *run) tea.Cmd {
	return tea.Tick(runTickEvery, func(time.Time) tea.Msg { return runTick{r} })
}

// placeRun puts tabs in the result area of r's window in place of the
// tabs of r's console there not pinned, where the first of those was
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

// gotRun shows what run r did (§11): a line of the log a statement, a
// result tab a result set in place of the placeholder, or what was there
// before when there is none; a failure, logged, shows the log and turns
// its statement's ▶ red; a cancel says so. After DDL the catalog loads
// again.
func (a *App) gotRun(m runDone) tea.Cmd {
	r := m.r
	a.busy--
	r.done, r.from.running = true, nil
	var tabs []Tab
	var cmds []tea.Cmd
	ddl := false
	for i, res := range m.rs {
		stmt := r.sql[r.stmts[i].Start:r.stmts[i].End]
		a.log(r, stmt, resultText(res), false)
		if res.Cols != nil {
			tabs = append(tabs, Tab{Name: r.label(len(tabs) + 1), Result: &resultTab{gridState: gridState{page: res}, run: r, part: len(tabs) + 1}})
		}
		switch sqlkit.FirstWord(stmt, sqlkit.PG) {
		case "create", "alter", "drop":
			ddl = true
		}
	}
	if m.err != nil {
		s := r.stmts[len(m.rs)]
		stmt := r.sql[s.Start:s.End]
		if errors.Is(m.err, context.Canceled) {
			a.log(r, stmt, "已取消", false)
			cmds = append(cmds, a.showToast("查询已取消", toastTTL))
		} else {
			lines := postgres.ErrorLines(m.err)
			a.log(r, stmt, lines[0], true)
			for _, l := range lines[1:] {
				r.win.log = append(r.win.log, ui.LogLine{Tail: "    " + l, Err: true})
			}
			if r.from.ver == r.ver { // the text is still what ran
				r.from.failed = strings.Count(strings.Join(r.from.ed.Lines(), "\n")[:r.base+s.Start], "\n")
			}
		}
	}
	shown := tabs
	if len(tabs) == 0 {
		shown = r.prev
	}
	at, _ := a.placeRun(r, shown)
	p := r.win.Result
	selectTab(p, 0) // the log, unless a result set shows
	if m.err == nil && len(tabs) > 0 {
		selectTab(p, at)
	}
	if ddl {
		clear(a.sess.cols) // as tree.refresh
		cmds = append(cmds, a.loadCatalog())
	}
	return tea.Batch(cmds...)
}

// clock is the log's time; the tests stop it.
var clock = time.Now

// log adds a line for statement stmt of run r to its window's log, and
// the log follows it (§11): the time, the console, the statement's first
// line, what it did.
func (a *App) log(r *run, stmt, did string, failed bool) {
	first, _, _ := strings.Cut(stmt, "\n")
	w := r.win
	w.log = append(w.log, ui.LogLine{Head: fmt.Sprintf("%s  %s  %s  ", clock().Format("15:04:05"), r.name, first), Tail: did, Err: failed})
	if p := a.resultPane(w); len(p.Tabs) > 0 {
		p.Tabs[0].Result.top = len(w.log) // the bottom: LogTop keeps it to the last line
	}
}

// resultText is what a result says in the log and on the result area's
// title: its rows, or its tag, and the time it took (§11).
func resultText(r db.Result) string {
	took := r.Took.Round(time.Millisecond).String()
	switch {
	case r.Cols == nil:
		return r.Tag + " · " + took
	case r.Truncated:
		return fmt.Sprintf("%d+ 行 · %s", len(r.Rows), took)
	}
	return fmt.Sprintf("%d 行 · %s", len(r.Rows), took)
}

// rerun is 重跑: the SQL of the result tab's run, as a new run of its console.
func (a *App) rerun() tea.Cmd {
	rt := resultOf(a.focused())
	if rt == nil || rt.run == nil {
		return nil
	}
	r := rt.run
	return a.runSQL(r.from, r.name, r.sql, r.base)
}

// exportResult writes the result tab to <console>-<n>.csv in the current
// directory (§11): the header and every row, NULL as nothing.
func (a *App) exportResult() tea.Cmd {
	rt := resultOf(a.focused())
	if rt == nil || rt.run == nil || rt.page.Cols == nil {
		return nil
	}
	name := fmt.Sprintf("%s-%d", rt.run.name, rt.run.seq)
	if rt.part > 1 {
		name += fmt.Sprintf("-%d", rt.part)
	}
	path, err := filepath.Abs(name + ".csv")
	if err == nil {
		err = os.WriteFile(path, []byte(csvOf(rt.page)), 0o600)
	}
	if err != nil {
		return a.showToast(err.Error(), toastTTL)
	}
	return a.showToast("已导出 "+path, toastTTL)
}

// csvOf is r as CSV: the header, the rows, NULL as nothing, as lazysql's
// helpers/csv.go writes it.
func csvOf(r db.Result) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	rec := make([]string, len(r.Cols))
	for i, c := range r.Cols {
		rec[i] = c.Name
	}
	w.Write(rec)
	for _, row := range r.Rows {
		for i, v := range row {
			rec[i] = v.S // "" when NULL
		}
		w.Write(rec)
	}
	w.Flush()
	return b.String()
}
