package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/jackc/pgx/v5/pgconn"

	"sqlmux/internal/db"
	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
)

// execDB answers console runs: a statement's result by its SQL, the
// LIMIT a read gets left off; fail says where one fails.
type execDB struct {
	noDB
	res  map[string]db.Result
	fail map[string]error
	ran  []string
}

func (d *execDB) Exec(_ context.Context, sql string, _ int) ([]db.Result, error) {
	d.ran = append(d.ran, sql)
	sql, _, _ = strings.Cut(sql, "\nLIMIT ")
	if err := d.fail[sql]; err != nil {
		return nil, err
	}
	return []db.Result{d.res[sql]}, nil
}

func rows(n int) db.Result {
	r := db.Result{Cols: []db.Col{{Name: "n", Type: "int4"}}}
	for i := range n {
		r.Rows = append(r.Rows, []db.Val{{S: string(rune('a' + i%26))}})
	}
	return r
}

// press presses keys and delivers what a run they start does.
func press(t *testing.T, a *App, keys string) {
	t.Helper()
	ks, err := keymap.Parse(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ks {
		_, cmd := a.Update(teaKey(k))
		deliver(a, cmd)
	}
}

func deliver(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			deliver(a, c)
		}
	case runDone:
		a.Update(m)
	}
}

// runOf is the runDone cmd would deliver, not delivered.
func runOf(t *testing.T, cmd tea.Cmd) runDone {
	t.Helper()
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			if done, ok := c().(runDone); ok {
				return done
			}
		}
	case runDone:
		return m
	}
	t.Fatal("no run")
	return runDone{}
}

// inRun is inConsole with Main answering from d and the console holding text.
func inRun(t *testing.T, d *execDB, text string) (*App, *consoleTab) {
	t.Helper()
	a, c := inConsole(t, "")
	a.sess.Main = db.NewWorker(d)
	c.ed.Load(text)
	return a, c
}

func logText(a *App) string {
	var out []string
	for _, l := range a.win().log {
		_, stmt, _ := strings.Cut(l.Head, "  ") // after the time
		out = append(out, strings.TrimSpace(stmt+l.Tail))
	}
	return strings.Join(out, "\n")
}

// The first run makes the result area at the bottom; the statement under
// the cursor runs, a read with a LIMIT past max_rows; its result tab
// comes after the log, the focus stays in the console (§11).
func TestRunFirst(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}}
	a, c := inRun(t, d, "select 1;\nselect 2;")
	console := a.focused()
	press(t, a, "<CR>")
	res := a.win().Result
	if res == nil || a.win().Root.Split != Vert || a.win().Root.Ratio != 0.6 || a.win().Root.B.Pane != res {
		t.Fatalf("no result area at the bottom: %+v", a.win().Root)
	}
	if tabNames(res) != "日志 console_1 #1" || res.Cur != 1 || a.focused() != console || c.running != nil {
		t.Fatalf("tabs %v cur %d, focus %d", tabNames(res), res.Cur, a.win().Focus)
	}
	if len(d.ran) != 1 || d.ran[0] != "select 1\nLIMIT 1001" {
		t.Errorf("ran %q", d.ran)
	}
	if got := logText(a); got != "console_1  select 1  3 行 · 0s" {
		t.Errorf("log %q", got)
	}
	if top := strings.Split(a.render().String(), "\n")[a.layout()[res.ID].Min.Y]; !strings.Contains(top, "console_1 #1") || !strings.Contains(top, "3 行 · 0s") {
		t.Errorf("title %q", top)
	}
}

// Running again takes the place of the console's tabs, the number going
// up; a pinned tab stays and the next run opens beside it. Several result
// sets get a tab each; a write only a line in the log (§11).
func TestRunReplaces(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(1), "select 2": rows(2), "update t set a = 1": {Tag: "UPDATE 3"}}}
	a, c := inRun(t, d, "select 1;\nselect 2;\nupdate t set a = 1;")
	press(t, a, "<CR><CR>")
	res := a.win().Result
	if tabNames(res) != "日志 console_1 #2" {
		t.Fatalf("again: %v", tabNames(res))
	}
	a.win().focus(res.ID)
	press(t, a, "P")
	a.win().focus(2)
	press(t, a, "ggVG<CR>")
	if tabNames(res) != "日志 console_1 #2 console_1 #3 console_1 #3·2" || res.Cur != 2 || c.ed.Mode() != 0 {
		t.Fatalf("pinned, then three statements: %v cur %d mode %v", tabNames(res), res.Cur, c.ed.Mode())
	}
	if !strings.HasSuffix(logText(a), "console_1  update t set a = 1  UPDATE 3 · 0s") {
		t.Errorf("log %q", logText(a))
	}
	press(t, a, "G<CR>") // nothing to show: the last tabs stay, the log shows
	if tabNames(res) != "日志 console_1 #2 console_1 #3 console_1 #3·2" || res.Cur != 0 {
		t.Errorf("a write alone: %v cur %d", tabNames(res), res.Cur)
	}
}

// A statement that fails stops the run: those before it show, the log
// has the error and shows, its ▶ goes red; a cancel says so and no ▶ does (§11).
func TestRunFails(t *testing.T) {
	fail := &pgconn.PgError{Severity: "ERROR", Message: "relation \"nope\" does not exist", Hint: "look again"}
	d := &execDB{res: map[string]db.Result{"select 1": rows(1)}, fail: map[string]error{"select * from nope": fail, "select pg_sleep(9)": context.Canceled}}
	a, c := inRun(t, d, "select 1;\n\nselect * from nope;\nselect 3;")
	press(t, a, "ggVG<CR>")
	res := a.win().Result
	if tabNames(res) != "日志 console_1 #1" || res.Cur != 0 || len(d.ran) != 2 {
		t.Fatalf("tabs %v cur %d ran %q", tabNames(res), res.Cur, d.ran)
	}
	if got := logText(a); got != "console_1  select 1  1 行 · 0s\nconsole_1  select * from nope  ERROR: relation \"nope\" does not exist\nHINT: look again" {
		t.Errorf("log %q", got)
	}
	if c.failed != 2 {
		t.Errorf("red ▶ on line %d, want 2", c.failed)
	}
	if press(t, a, "gg<CR>"); c.failed != -1 { // the next run starts with none
		t.Errorf("red ▶ on line %d after select 1 ran", c.failed)
	}
	a.consoleDid(c, c.ed.Load("select pg_sleep(9);"))
	press(t, a, "<CR>")
	if a.toast != "查询已取消" || c.failed != -1 || !strings.HasSuffix(logText(a), "已取消") {
		t.Errorf("cancel: toast %q, red ▶ %d, log %q", a.toast, c.failed, logText(a))
	}
}

// More rows than max_rows read "1000+ 行" (§11).
func TestRunTruncated(t *testing.T) {
	r := rows(1000)
	r.Truncated = true
	a, _ := inRun(t, &execDB{res: map[string]db.Result{"select n": r}}, "select n")
	press(t, a, "<CR>")
	if got := logText(a); !strings.HasSuffix(got, "1000+ 行 · 0s") {
		t.Errorf("log %q", got)
	}
}

// DDL loads the catalog again; a run going on takes no second ↵ (§11).
func TestRunDDLAndBusy(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"create table x (a int)": {Tag: "CREATE TABLE"}}}
	a, c := inRun(t, d, "create table x (a int)")
	a.sess.cols[tableID{"public", "t_order"}] = db.Columns{}
	_, cmd := a.Update(teaKey("<CR>"))
	if c.running == nil || a.busy != 1 {
		t.Fatal("not running")
	}
	if _, again := a.Update(teaKey("<CR>")); again != nil {
		t.Error("a second ↵ while it runs")
	}
	if _, cmd := a.Update(runOf(t, cmd)); cmd == nil || len(a.sess.cols) != 0 || a.busy != 0 {
		t.Errorf("after create table: the catalog loads again, the columns go: %v", a.sess.cols)
	}
}

// q and x close a result tab, never the log; P pins; R runs its SQL
// again; the export writes <console>-<n>.csv here (§11).
func TestResultKeys(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(2)}}
	a, _ := inRun(t, d, "select 1")
	press(t, a, "<CR>")
	res := a.win().Result
	a.win().focus(res.ID)
	press(t, a, "R")
	if tabNames(res) != "日志 console_1 #2" || len(d.ran) != 2 {
		t.Fatalf("R: %v", tabNames(res))
	}
	dir := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(wd)
	a.run("result.export", 0)
	if data, err := os.ReadFile(filepath.Join(dir, "console_1-2.csv")); err != nil || string(data) != "n\na\nb\n" || !strings.HasPrefix(a.toast, "已导出 ") {
		t.Errorf("export: %q %v, toast %q", data, err, a.toast)
	}
	press(t, a, "x")
	if tabNames(res) != "日志" {
		t.Fatalf("x: %v", tabNames(res))
	}
	press(t, a, "q")
	if a.run("tab.close", 0); tabNames(res) != "日志" {
		t.Error("q or tab.close (:q) closed the log")
	}
}

// ↵ runs the statement under the cursor; a click on a ▶ the one starting
// on its line, the cursor staying; VISUAL what it selects, to the end of
// the last character or the line break (§11「执行」).
func TestRunWhat(t *testing.T) {
	d := &execDB{}
	a, c := inRun(t, d, "select '中', 2;\nselect 3;\nselect 4;")
	deliver(a, a.run("console.run 3", 0))
	if len(d.ran) != 1 || d.ran[0] != "select 4\nLIMIT 1001" || c.ed.Cursor() != (editor.Pos{}) {
		t.Fatalf("▶ 3: ran %q, cursor %v", d.ran, c.ed.Cursor())
	}
	for _, k := range []struct{ keys, span string }{
		{"f'v", "'"},
		{"f'vl", "'中"},                       // a character, not a byte
		{"wv$", "'中', 2;\n"},                 // with the line break
		{"$vj", ";\nselect 3;\n"},            // $ aims j at the end: the line break too
		{"jlV", "select 3;"},                 // whole lines
		{"jl<C-v>j", "select 3;\nselect 4;"}, // a block by its whole lines
	} {
		press(t, a, "<Esc>gg0"+k.keys)
		if text, from, to := consoleSpan(c, ""); text[from:to] != k.span || c.ed.Mode() != editor.Normal {
			t.Errorf("%s: %q, mode %v", k.keys, text[from:to], c.ed.Mode())
		}
	}
	d.ran = nil
	if press(t, a, "gg0wvj<CR>"); strings.Join(d.ran, ";") != "'中', 2;select 3\nLIMIT 1001" || c.ed.Cursor() != (editor.Pos{Line: 1, Col: 7}) {
		t.Errorf("v then ↵: ran %q, cursor %v", d.ran, c.ed.Cursor())
	}
}

// 重跑 runs the tab's SQL, which the console's text may no longer be: no
// ▶ goes red then (§11).
func TestRerunAfterEdit(t *testing.T) {
	nope := &pgconn.PgError{Severity: "ERROR", Message: "relation \"nope\" does not exist"}
	d := &execDB{res: map[string]db.Result{"select 1": rows(1)}, fail: map[string]error{"select * from nope": nope}}
	for _, edited := range []string{"x", "select 5;\nselect 6;\nselect 1;\nselect * from nope;"} {
		a, c := inRun(t, d, "select 1;\nselect * from nope;")
		press(t, a, "ggVG<CR>")
		a.consoleDid(c, c.ed.Load(edited))
		res := a.win().Result
		a.win().focus(res.ID)
		selectTab(res, 1)
		if press(t, a, "R"); c.failed != -1 || !strings.HasSuffix(logText(a), "does not exist") {
			t.Errorf("%q: red ▶ %d, log %q", edited, c.failed, logText(a))
		}
	}
}

// The result area closed while a run goes on takes the tabs it took the
// place of along: back with no result set, the run brings none back (§11).
func TestResultAreaClosedWhileRunning(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(1), "update t set a = 1": {Tag: "UPDATE 3"}}}
	a, c := inRun(t, d, "select 1")
	press(t, a, "<CR>")
	a.consoleDid(c, c.ed.Load("update t set a = 1"))
	_, cmd := a.Update(teaKey("<CR>"))
	win := a.win()
	a.removePane(win.Result.ID)
	if a.Update(runOf(t, cmd)); tabNames(win.Result) != "日志" {
		t.Errorf("tabs %v", tabNames(win.Result))
	}
}

// Closing the result area keeps its height and the log for the next run;
// its tabs go, pinned or not (§11). Its keys move the log.
func TestResultAreaCloses(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(1)}}
	a, _ := inRun(t, d, "select 1")
	press(t, a, "<CR>")
	win := a.win()
	res := win.Result
	win.focus(res.ID)
	press(t, a, "P")
	win.Root.Ratio = 0.5 // its border dragged
	ratio := win.Root.Ratio
	press(t, a, "<Space>x")
	if win.Result != nil || len(win.log) != 1 || win.resultRatio != ratio {
		t.Fatalf("closed: result %v, log %d, ratio %v of %v", win.Result, len(win.log), win.resultRatio, ratio)
	}
	press(t, a, "<C-l><CR>")
	if win.Result == nil || win.Root.Ratio != ratio || tabNames(win.Result) != "日志 console_1 #2" || len(win.log) != 2 {
		t.Fatalf("again: %v, ratio %v", tabNames(win.Result), win.Root.Ratio)
	}
	win.focus(win.Result.ID)
	selectTab(win.Result, 0)
	press(t, a, "gg")
	if win.Result.Tabs[0].Result.top != 0 {
		t.Error("gg on the log")
	}
}

// The result area's title as the window narrows: the words go first,
// then export, transpose, pin, rerun, strictly; close stays longest
// (§11「工具行」). Then a run's placeholder, and the log tab with an error
// in it.
func TestGoldenResult(t *testing.T) {
	d := &execDB{res: map[string]db.Result{"select 1": rows(3)}, fail: map[string]error{"select x": &pgconn.PgError{Severity: "ERROR", Message: "column \"x\" does not exist"}}}
	clock = func() time.Time { return time.Date(2026, 9, 29, 14, 5, 12, 0, time.Local) }
	defer func() { clock = time.Now }()
	var titles []string
	for _, w := range []int{160, 90, 70, 62, 56, 50, 46, 42} { // ascii: the buttons can be read
		a, c := inConsole(t, `icons = "ascii"`)
		a.sess.Main = db.NewWorker(d)
		c.ed.Load("select 1")
		a.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		press(t, a, "<CR>")
		r := a.layout()[a.win().Result.ID]
		titles = append(titles, ansi.Cut(strings.Split(a.render().String(), "\n")[r.Min.Y], r.Min.X, r.Max.X))
	}
	a, c := inRun(t, d, "select 1;\nselect x;")
	_, cmd := a.Update(teaKey("<CR>"))
	r := a.layout()[a.win().Result.ID]
	running := strings.Split(a.render().String(), "\n")[r.Min.Y+1]
	a.Update(runOf(t, cmd))
	a.consoleDid(c, c.ed.Feed("j"))
	press(t, a, "<CR>")
	golden.RequireEqual(t, strings.Join(titles, "\n")+"\n\n"+ansi.Cut(running, r.Min.X, r.Max.X)+"\n\n"+a.render().String())
}
