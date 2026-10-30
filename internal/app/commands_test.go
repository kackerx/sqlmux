package app

import (
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// pane.focus.next goes clockwise round the middle of the screen, the
// status bar in it, and the sidebar when it is open (§6.8, F5.16).
func TestFocusNext(t *testing.T) {
	a := sized(160, 45, "nerd")
	win := a.win()
	order := func(from, n int) (ids []int) {
		win.focus(from)
		for range n {
			a.run("pane.focus.next", 0)
			ids = append(ids, win.Focus)
		}
		return ids
	}
	// a row of three full-height panes, the sidebar a fourth: 树 → 1 → 2 → 3 → 树
	win.focus(2)
	a.splitPane(Horiz) // 3 at the right
	if got := order(0, 5); !slices.Equal(got, []int{1, 2, 3, 0, 1}) {
		t.Errorf("a row: %v", got)
	}
	a.foldTree()
	if got := order(1, 4); !slices.Equal(got, []int{2, 3, 1, 2}) {
		t.Errorf("sidebar folded: %v", got)
	}
	a.toggleTree()
	// the default layout and the result area below the panes: 树 → data → query → 结果区 → 树
	b := sized(160, 45, "nerd")
	bw := b.win()
	res := b.resultPane(bw)
	step := func(from int) int {
		bw.focus(from)
		b.run("pane.focus.next", 0)
		return bw.Focus
	}
	if got := []int{step(0), step(1), step(2), step(res.ID)}; !slices.Equal(got, []int{1, 2, res.ID, 0}) {
		t.Errorf("with the result area: %v, area %d", got, res.ID)
	}
	bw.resultHidden = false
	b.toggleResult() // hidden: it counts no more
	if got := []int{step(0), step(1), step(2)}; !slices.Equal(got, []int{1, 2, 0}) {
		t.Errorf("result area hidden: %v", got)
	}
	// the data pane split, 1 above 3: 树 → 1 → 2 → 3 → 树, a clockwise turn, not ⟨n⟩ order
	c := sized(160, 45, "nerd")
	cw := c.win()
	cw.focus(1)
	c.splitPane(Vert) // the new pane, 3, is below 1
	cstep := func(from int) int {
		cw.focus(from)
		c.run("pane.focus.next", 0)
		return cw.Focus
	}
	if got := []int{cstep(0), cstep(1), cstep(2), cstep(3)}; !slices.Equal(got, []int{1, 2, 3, 0}) {
		t.Errorf("1 above 3: %v", got)
	}
	// a pane split, the sidebar and the result area: all five, round once
	b.toggleResult()
	bw.focus(1)
	b.splitPane(Vert)
	start, seen := bw.Focus, map[int]bool{}
	for range 5 {
		b.run("pane.focus.next", 0)
		seen[bw.Focus] = true
	}
	if len(seen) != 5 || bw.Focus != start {
		t.Errorf("five panes: visited %v, back on %d, began on %d", seen, bw.Focus, start)
	}
}

// query.format lays out every statement, as one undo step, the cursor
// where it was, line and column, clamped when the text got shorter; not in
// INSERT (F5.16, F8.3).
func TestQueryFormatAll(t *testing.T) {
	if _, err := exec.LookPath("printf"); err != nil {
		t.Skip("the fake formatprg of a segment is printf, and it is not on the PATH")
	}
	a, c := inQuery(t, "", "")
	format := func() {
		t.Helper()
		cmd := a.run("query.format", 0)
		if cmd == nil {
			t.Fatal("formats nothing")
		}
		a.Update(cmd())
	}
	a.queryDid(c, c.ed.Load("select 1;\nselect a,b from t;"))
	feed(t, a, "jfb")
	format()
	if got := text(c); got != "select\n  1;\n\nselect\n  a,\n  b\nfrom\n  t;" || c.ed.Cursor() != (editor.Pos{Line: 1, Col: 3}) {
		t.Fatalf("both statements, cursor kept (line 1, the column clamped to the ;): %q, cursor %v", got, c.ed.Cursor())
	}
	if feed(t, a, "u"); text(c) != "select 1;\nselect a,b from t;" {
		t.Errorf("u: %q", text(c))
	}
	a.queryDid(c, c.ed.Load("select 1;\n\n\n\nselect 2 from t;"))
	feed(t, a, "G$")
	a.formatPrg = "printf 'select 1;\\nselect 2'" // fewer lines than the cursor's; the last statement's ; is not part of it
	format()
	if got := c.ed.Cursor(); got != (editor.Pos{Line: 1, Col: 8}) {
		t.Errorf("past the end: cursor %v, want the last line, the nearest character", got)
	}
	a.formatPrg = ""
	a.queryDid(c, c.ed.Load("select 1;\n\n\nselect 2;"))
	feed(t, a, "3G")
	format()
	if got := c.ed.Cursor(); got != (editor.Pos{Line: 2, Col: 0}) {
		t.Errorf("a line that is there still: cursor %v", got)
	}
	feed(t, a, "i")
	if a.run("query.format", 0) != nil {
		t.Error("formats in INSERT")
	}
}

// What the formatter brings back for a query closed meanwhile is dropped,
// the file's text is the one reopened (F5.16).
func TestFormatAllClosedMeanwhile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, c := inQuery(t, "", "")
	a.queryDid(c, c.ed.Load("select 1;"))
	cmd := a.run("query.format", 0)
	a.win().focus(2)
	a.run("tab.close", 0)
	a.run("tab.reopen", 0)
	a.Update(cmd())
	if c2 := a.focusedQuery(); c2 == nil || c2 == c || text(c) != "select 1;" {
		t.Errorf("the closed query %q was formatted", text(c))
	}
}

// tab.quit closes the tab, then the pane when it is left empty, then the
// window when it is its only pane; the session's last window stays; what
// would be lost is asked about first (F5.16).
func TestTabQuit(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>c")
	win := a.win()
	tab := loadOrders(t, a, 3)
	win.focus(2)
	a.run("tab.quit", 0)
	if len(a.sess.Windows) != 2 || win.pane(2) != nil || len(win.Root.Leaves()) != 1 {
		t.Fatalf("a query's tab, its pane left empty: %d windows, panes %d", len(a.sess.Windows), len(win.Root.Leaves()))
	}
	win.focus(1)
	if dataOf(a.focused()) != tab {
		t.Fatal("the table is not in the pane")
	}
	tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}}
	if a.run("tab.quit", 0); a.confirm == nil || len(a.sess.Windows) != 2 || len(a.focused().Tabs) != 1 {
		t.Fatalf("with changes: %+v", a.confirm)
	}
	if feed(t, a, "n"); len(a.sess.Windows) != 2 || len(a.focused().Tabs) != 1 {
		t.Fatalf("n: %d windows, %d tabs", len(a.sess.Windows), len(a.focused().Tabs))
	}
	a.run("tab.quit", 0)
	if feed(t, a, "y"); len(a.sess.Windows) != 1 {
		t.Fatalf("y: the table and the window go together: %d windows", len(a.sess.Windows))
	}
	// the last window stays: its one pane, no tab left, is the landing page
	a.win().focus(2)
	a.run("tab.quit", 0)
	a.win().focus(1)
	if a.run("tab.quit", 0); len(a.sess.Windows) != 1 || a.win().pane(1) == nil || len(a.win().pane(1).Tabs) != 0 {
		t.Errorf("the last window: %d windows", len(a.sess.Windows))
	}
	// a pane with another tab only loses the one
	feed(t, a, "<Space>c")
	win = a.win()
	loadOrders(t, a, 3)
	a.newTab()
	a.run("tab.quit", 0)
	if p := win.pane(1); len(a.sess.Windows) != 2 || len(win.Root.Leaves()) != 1 || len(p.Tabs) != 1 {
		t.Errorf("a pane that has a tab left: %d windows, %d panes, %d tabs", len(a.sess.Windows), len(win.Root.Leaves()), len(p.Tabs))
	}
}

// tab.reopen brings back the tabs closed last, newest first, a table with
// its WHERE, ORDER and LIMIT, a query on its file and schema; a table
// open already is only shown; 20 are kept; landing tabs and results are
// not recorded (F5.16).
func TestTabReopen(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	if a.run("tab.reopen", 0) != nil || len(a.sess.closed) != 0 {
		t.Fatal("nothing closed, something reopened")
	}
	tab := loadOrders(t, a, 3)
	tab.applied, tab.order, tab.limit = "id > 1", orderOf("amount-"), 500
	win := a.win()
	win.focus(2)
	queryOf(a.focused()).schema = "agentable"
	a.run("tab.close", 0) // the query, its pane gone
	win.focus(1)
	a.run("tab.close", 0) // the table, the pane the window's only one
	if win.pane(2) != nil || len(a.sess.closed) != 2 {
		t.Fatalf("closed: pane %v, %d recorded", win.pane(2), len(a.sess.closed))
	}
	a.run("tab.reopen", 0) // the table, in its own pane
	d := dataOf(a.focused())
	if d == nil || d.table.Name != "t_order" || d.applied != "id > 1" || !slices.Equal(d.order, orderOf("amount-")) || d.limit != 500 || d.where.Text != "id > 1" {
		t.Fatalf("table: %+v", d)
	}
	a.run("tab.reopen", 0) // the query: its pane is gone, so the current one
	p := a.focused()
	if c := queryOf(p); c == nil || c.name != "query_1" || c.schema != "agentable" || len(p.Tabs) != 2 {
		t.Fatalf("query: %+v in pane %d", c, p.ID)
	}
	selectTab(p, 0)
	a.run("tab.close", 0) // the table
	loadOrders(t, a, 3)
	a.run("tab.reopen", 0) // already open: shown, not twice
	if n := len(p.Tabs); n != 2 || dataOf(p) == nil {
		t.Errorf("a table open already: %d tabs", n)
	}

	s := a.sess
	s.closed = nil
	table := db.Table{Schema: "public", Name: "t_order"}
	for i := range 21 {
		s.recordClosed(1, 1, Tab{Name: "t", Data: &dataTab{conn: s.Conns[0], table: table, request: request{limit: 100 + i}}})
	}
	s.recordClosed(1, 1, landing())
	s.recordClosed(1, 1, Tab{Name: "log", Result: &resultTab{}})
	if len(s.closed) != closedKept || s.closed[0].req.limit != 101 || s.closed[len(s.closed)-1].req.limit != 120 {
		t.Errorf("kept %d, the first limit %d: the 21st takes the oldest out, a landing tab and a result are none", len(s.closed), s.closed[0].req.limit)
	}
}

// A query open in another session is shown there: that session
// attached, its tab current, the record gone (§6.8, F5.16).
func TestTabReopenQueryElsewhere(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	first := a.sess
	second := newSession("2", []*Conn{first.Conns[0]}, first.Conns[0], config.QueryName(2))
	a.sessions = append(a.sessions, second)
	first.recordClosed(1, 2, Tab{Name: "query_2", Query: queryOf(second.Windows[0].pane(2))})
	if a.run("tab.reopen", 0); a.sess != second || a.focused().ID != 2 || len(first.closed) != 0 {
		t.Errorf("session %q, focus %d, %d recorded", a.sess.Name, a.focused().ID, len(first.closed))
	}
	if n := len(first.queries()); n != 1 {
		t.Errorf("opened again in the first: %d queries", n)
	}
}

// A key held with Cmd types nothing and does nothing the plain key would,
// in NORMAL or INSERT; bound in [keys.global] it runs (F5.17).
func TestSuperKeys(t *testing.T) {
	a, c := inQuery(t, "", "[keys.global]\n\"<D-w>\" = \"tree.toggle\"\n")
	feed(t, a, "iselect abc<Esc>")
	press := func(code rune) { a.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModSuper}) }
	press('x')
	feed(t, a, "i")
	press('y')
	feed(t, a, "<Esc>")
	if text(c) != "select abc" {
		t.Errorf("typed: %q", text(c))
	}
	if press('w'); a.win().Focus != a.win().Tree.ID {
		t.Error("<D-w> did not run")
	}
}

// Lines of keymaps.toml that did not take are listed in a notice at the
// start, one a line, closed by ↵; the program is as usual after it (§6.7,
// F6.1).
func TestKeyProblemsNotice(t *testing.T) {
	c := userConfig(t, "", "[keys.normal]\n\"<C-q>\" = \"tab.next\"\n\"<c-q>\" = \"tab.prev\"\n\"<C-p>\" = \"tab.next\"\n\"<c-p>\" = \"tab.prev\"\n")
	keys, problems := keymap.New(c.Leader, c.Vim, c.Timeoutlen, c.Bindings)
	if len(problems) < 2 {
		t.Fatalf("problems: %v", problems)
	}
	a := newApp(c, keys, testSession(), &config.State{}, "")
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	a.KeyProblems(problems)
	if a.confirm == nil || !strings.HasPrefix(a.confirm.text, "keymaps.toml 有 2 处问题：\n") || strings.Count(a.confirm.text, "\n") != len(problems) {
		t.Fatalf("notice: %+v", a.confirm)
	}
	screen := a.render().String()
	for _, p := range problems {
		if !strings.Contains(screen, p.String()) {
			t.Errorf("%q is not on screen", p)
		}
	}
	if !strings.Contains(screen, "知道了") || strings.Contains(screen, "取消") {
		t.Error("the one button: 知道了")
	}
	if feed(t, a, "<CR>"); a.confirm != nil {
		t.Error("↵ keeps it")
	}
	a.KeyProblems(nil)
	if a.confirm != nil {
		t.Error("none: a notice")
	}
	if feed(t, a, "<Space>c"); len(a.sess.Windows) != 2 {
		t.Error("the program is as usual")
	}
}

// theme.switch lists the themes there are, the one in use marked; the pick
// is on the screen at once, a token the file leaves out as tokyonight-storm
// has it, the icons chosen kept with the theme's [icon] on them; a theme
// that cannot be read says why and changes nothing; nothing is written
// (F6.2).
func TestThemeSwitch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	themes := filepath.Join(dir, "sqlmux", "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "mine.toml"), []byte("bg = \"#101010\"\n[icon.table]\ntext = \"T\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "bad.toml"), []byte("bg = \"red\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := sized(160, 45, "ascii")
	a.run("theme.switch", 0)
	items, current := a.dropItems()
	if a.drop == nil || a.drop.kind != dropTheme || !slices.Equal(items, []string{"bad", "mine", "tokyonight-storm"}) || current != 2 {
		t.Fatalf("list: %+v %v current %d", a.drop, items, current)
	}
	feed(t, a, "mi<CR>")
	if a.drop != nil || a.themeName != "mine" || ansiHex(a.theme.Bg) != "#101010" || a.theme.Fg != ui.TokyonightStorm.Fg || a.icons.Table.Text != "T" || a.icons.Schema.Text != "#" {
		t.Fatalf("mine: bg %v fg %v table %q schema %q", a.theme.Bg, a.theme.Fg, a.icons.Table.Text, a.icons.Schema.Text)
	}
	if !strings.Contains(a.toast, "config.toml") {
		t.Errorf("toast %q", a.toast)
	}
	a.run("theme.switch", 0)
	feed(t, a, "bad<CR>")
	if a.themeName != "mine" || !strings.Contains(a.toast, "bad.toml") {
		t.Errorf("a theme that cannot be read: %q, toast %q", a.themeName, a.toast)
	}
	a.run("theme.switch", 0)
	feed(t, a, "tokyo<CR>")
	if a.theme != ui.TokyonightStorm || a.icons.Table.Text != "+" {
		t.Error("back to tokyonight-storm and the icons chosen")
	}
	// the file is read as the program runs: what it says goes with the language
	i18n.Set("en")
	defer i18n.Set("zh")
	a.run("theme.switch", 0)
	feed(t, a, "bad<CR>")
	if !strings.Contains(a.toast, "write a color as #rrggbb") || strings.Contains(a.toast, "颜色") {
		t.Errorf("in English: toast %q", a.toast)
	}
}

func ansiHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// With language = "en" the screen is in English: the landing page, the
// palette, the confirm box over a table with changes, the query bar, the
// result area's log, a table's saved note (§14「界面语言」, F6.4).
func TestGoldenEnglish(t *testing.T) {
	i18n.Set("en")
	defer i18n.Set("zh")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	scenes := map[string]func() *App{
		"landing": func() *App { return wide(160, 45) },
		"palette": func() *App {
			a := wide(160, 45)
			feed(t, a, "<C-p>")
			return a
		},
		"confirm": func() *App {
			a := wide(160, 45)
			tab := loadOrders(t, a, 60)
			tab.edits = map[editKey]edit{{joinKey([]string{"1"}), "status"}: {val: db.Val{S: "done"}, orig: db.Val{S: "running"}}}
			feed(t, a, "x")
			return a
		},
		// 80 columns: the scopes of the palette all fit
		"palette80": func() *App {
			a := wide(80, 24)
			feed(t, a, "<C-p>")
			return a
		},
		"danger": func() *App { return dangerScene(t) },
		// both questions of a quit, a line each
		"confirm2": func() *App {
			a := wide(100, 30)
			tab := loadOrders(t, a, 60)
			tab.edits = map[editKey]edit{{joinKey([]string{"1"}), "status"}: {val: db.Val{S: "done"}, orig: db.Val{S: "running"}}}
			a.sess.Conns[0].tx = 'T'
			feed(t, a, ":qa<CR>")
			return a
		},
		"table": func() *App {
			a := wide(160, 45)
			tab := loadOrders(t, a, 60)
			tab.note = ui.Note{Head: i18n.T("已保存 %d 行%s · %s", 2, "", "3ms")}
			return a
		},
		"result": func() *App {
			a, _ := inRun(t, &execDB{res: map[string]db.Result{"select 1": rows(3)}}, "select 1;")
			press(t, a, "<CR>")
			return a
		},
	}
	for name, scene := range scenes {
		t.Run(name, func(t *testing.T) {
			golden.RequireEqual(t, scene().render().String())
		})
	}
}

// shiftArrow presses an arrow with Shift, as a terminal sends it.
func shiftArrow(a *App, code rune, n int) {
	for range n {
		a.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModShift})
	}
}

// vim = false: a query is typed into at once, ↵ makes a line, Esc and the
// vim keys are text or nothing, there is no mode to name, and the pane focus
// keys still go (§6.9, F6.6a).
func TestPlainQuery(t *testing.T) {
	a, c := inQuery(t, "vim = false", "")
	feed(t, a, "select 1<CR>gt<Space>x<Esc>")
	if text(c) != "select 1\ngt x" || c.ed.Mode() != editor.Insert {
		t.Fatalf("typed: %q in %v", text(c), c.ed.Mode())
	}
	if row := statusRow(a); strings.Contains(row, "INSERT") || strings.Contains(row, "NORMAL") {
		t.Errorf("a mode block: %q", row)
	}
	feed(t, a, "ab<C-a>X")
	if got := c.ed.Lines()[1]; got != "Xgt xab" {
		t.Errorf("C-a: %q", got)
	}
}

// vim = false: the WHERE is typed into, C-a and C-e go to the ends of the
// line, Esc closes the list and then leaves it (§6.9, F6.6a); ↵ and C-r are
// TestPlainWhereKeys'.
func TestPlainWhere(t *testing.T) {
	c := userConfig(t, "vim = false", "")
	a := sizedWith(160, 45, c)
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	a.win().focus(1)
	tab := loadOrders(t, a, 3)
	feed(t, a, "/")
	if tab.typing != "where" || tab.ed == nil || !tab.ed.Plain() || tab.ed.Mode() != editor.Insert {
		t.Fatalf("typing %q, %+v", tab.typing, tab.ed)
	}
	feed(t, a, "id = 1")
	if tab.where.Text != "id = 1" {
		t.Fatalf("where %q", tab.where.Text)
	}
	feed(t, a, "<C-a>x<C-e>y")
	if tab.where.Text != "xid = 1y" {
		t.Fatalf("C-a, C-e: %q", tab.where.Text)
	}
	feed(t, a, "<Esc>")
	if tab.where.Text != "" || tab.typing != "" { // what was not run goes, as the vim's second esc
		t.Errorf("esc leaves: %q, typing %q", tab.where.Text, tab.typing)
	}
	feed(t, a, "/ and st")
	if tab.comp == nil {
		t.Fatalf("no list")
	}
	feed(t, a, "<Esc>")
	if tab.comp != nil || tab.typing != "where" {
		t.Errorf("the first esc closes the list only: %v %q", tab.comp, tab.typing)
	}
}

// A theme with a key of a later version switches, and the toast says the key
// in place of "written to config.toml" (F14.7).
func TestThemeSwitchUnknownKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	themes := filepath.Join(dir, "sqlmux", "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "later.toml"), []byte("bg = \"#202020\"\nfuture_token = \"#ffffff\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := sized(160, 45, "ascii")
	a.run("theme.switch", 0)
	feed(t, a, "later<CR>")
	if a.themeName != "later" || ansiHex(a.theme.Bg) != "#202020" || a.toast != "themes/later.toml: 不认识的键 future_token；写进 config.toml 的 theme 才会保留" {
		t.Errorf("theme %q bg %v toast %q", a.themeName, a.theme.Bg, a.toast)
	}
}
