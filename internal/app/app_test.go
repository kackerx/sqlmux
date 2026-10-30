package app

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image/color"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// testData is the data directory of the tests that set none of their own:
// sizedWith gives each App a fresh one, for the files of queries are listed
// and numbered after (F8.21).
var testData string

func TestMain(m *testing.M) {
	// feed runs the Cmds keys return, the ticks too: they must not hold it up
	whichKeyDelay, quitWindow, toastTTL, autosaveDelay, spinEvery, yankFlashFor, previewDelay = time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond
	i18n.Set("zh")  // the tests read the Chinese texts, not the default, English (F7.6)
	clipTools = nil // OSC 52: the user's clipboard stays theirs
	dir, _ := os.MkdirTemp("", "sqlmux-app-test")
	testData = dir
	os.Setenv("XDG_STATE_HOME", dir)  // saves the state from the Cmds tests run go here, not the user's
	os.Setenv("XDG_DATA_HOME", dir)   // and the queries
	os.Setenv("XDG_CONFIG_HOME", dir) // and a form's connection
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// noDB answers every query with nothing. The tests set their catalog up
// front and don't run Init's Cmd; run, its load would empty the catalog.
type noDB struct{}

func (noDB) Exec(context.Context, string, int) ([]db.Result, error)      { return nil, nil }
func (noDB) Query(context.Context, string, ...db.Val) (db.Result, error) { return db.Result{}, nil }
func (noDB) Close() error                                                { return nil }

// testSession is the default workspace over a catalog of 14 tables in
// public and one in agentable, with no database behind it.
func testSession() *Session {
	c := readyConn("doraemon")
	c.Schema, c.Schemas, c.home, c.DB = "public", []string{"agentable", "public"}, "public", "doraemon"
	s := newSession("doraemon", []*Conn{c}, c, config.QueryName(1))
	queryOf(s.Windows[0].Root.B.Pane).schema = "public" // as the catalog's coming sets it
	c.Tables = []db.Table{{Schema: "agentable", Name: "planner", Rows: 3}}
	for _, t := range []struct {
		name string
		rows float64
	}{
		{"agent", 124}, {"agent_version", 530}, {"goal", 57},
		{"mt_task", 812}, {"mt_task_log", 96e3}, {"schema_migrations", 88},
		{"t_order", 1.2e6}, {"t_order_item", 3.4e6}, {"t_payment", 410e3},
		{"t_refund", 12e3}, {"t_sku", 8.1e3}, {"t_user", 38e3},
		{"t_user_address", 52e3}, {"t_user_profile", 38e3},
	} {
		c.Tables = append(c.Tables, db.Table{Schema: "public", Name: t.name, Rows: t.rows})
	}
	return s
}

// readyConn is connection name, connected to no database.
func readyConn(name string) *Conn {
	c := newConn(config.Connection{Name: name, Engine: "postgres"})
	c.State, c.Main, c.Meta, c.Addr = connReady, db.NewWorker(noDB{}), db.NewWorker(noDB{}), "pg@localhost:5432"
	return c
}

// m0Layout puts M0's layout into a, for the tests of panes, tabs and windows:
// ⟨1⟩ data with the tabs t_order and t_user beside the default query_1
// (§5), and a second window.
func m0Layout(a *App) *App {
	data := a.win().Root.A.Pane
	data.Cur, data.Prev = 0, 1
	for _, name := range []string{"t_order", "t_user"} { // not fetched, nor in the catalog: opening public.t_order finds no tab of it
		data.Tabs = append(data.Tabs, Tab{Name: name, Data: newDataTab(a.sess.Conns[0], db.Table{Schema: "m0", Name: name})})
	}
	a.sess.winID++
	a.sess.Windows = append(a.sess.Windows, &Window{ID: a.sess.winID, Name: "report", Tree: &Pane{ID: 0}, Root: leaf(&Pane{ID: 1, Prev: -1}), lastID: 1})
	return a
}

// tabNames is p's tabs by name, space-separated.
func tabNames(p *Pane) string {
	var names []string
	for _, t := range p.Tabs {
		names = append(names, t.Name)
	}
	return strings.Join(names, " ")
}

// twoPanes is sized with m0Layout.
func twoPanes(w, h int) *App { return m0Layout(sized(w, h, "nerd")) }

// wide is sized with no query: M1's layout, the data pane to the edge,
// which the tests of the grid were written at.
func wide(w, h int) *App {
	a := sized(w, h, "nerd")
	a.removePane(2)
	return a
}

func sized(w, h int, icons string) *App {
	c := config.Default()
	c.Icons = ui.IconSet(icons)
	c.BaseIcons = c.Icons
	return sizedWith(w, h, c)
}

// tipsInTests is off in tests, though the default is on, so that the frames that
// are not about the tip are the same whichever one comes up; withTips turns it on
// (F15.4).
var tipsInTests = false

// newApp is New with the tips off unless tipsInTests has them on, and the save
// with no preview: the saves in the tests are not about what is asked first, the
// preview's own tests turn it on (F17.10).
func newApp(c *config.Config, keys *keymap.Map, sess *Session, st *config.State, warning string) *App {
	c.Tips = c.Tips && tipsInTests
	c.PreviewSave = false
	return New(c, keys, sess, st, warning)
}

func sizedWith(w, h int, c *config.Config) *App {
	if os.Getenv("XDG_DATA_HOME") == testData {
		dir, _ := os.MkdirTemp(testData, "data")
		os.Setenv("XDG_DATA_HOME", dir)
	}
	keys, _ := keymap.New(c.Leader, c.Vim, c.Timeoutlen, c.Bindings)
	a := newApp(c, keys, testSession(), &config.State{}, "")
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// configured is an App with toml as the user's keymaps.toml.
func configured(t *testing.T, w, h int, toml string) *App {
	t.Helper()
	return sizedWith(w, h, userConfig(t, "", toml))
}

// newDefaultKeys is the keymap of the defaults.
func newDefaultKeys() (*keymap.Map, []keymap.Problem) {
	c := config.Default()
	return keymap.New(c.Leader, c.Vim, c.Timeoutlen, c.Bindings)
}

// userConfig is the config from the user's config.toml conf and
// keymaps.toml keys.
func userConfig(t testing.TB, conf, keys string) *config.Config {
	t.Helper()
	c, err := config.Parse(conf)
	if err != nil {
		t.Fatal(err)
	}
	if c.Leader, c.Bindings, err = keymap.ParseKeys(keys); err != nil {
		t.Fatal(err)
	}
	return c
}

// bgs is row y's backgrounds from x0 to x1, a letter a column: " " the
// pane's, "." the screen's, "-" sep, "#" select, "?" any other.
func bgs(a *App, f *ui.Frame, y, x0, x1 int) string {
	th := a.theme
	letters := map[color.Color]string{th.PaneBg: " ", th.Bg: ".", th.Sep: "-", th.Select: "#"}
	var b strings.Builder
	for x := x0; x < x1; x++ {
		if c := f.Buf.CellAt(x, y); c.Width > 0 { // a wide one's letter for its columns, as the text has them
			b.WriteString(strings.Repeat(cmp.Or(letters[c.Style.Bg], "?"), c.Width))
		}
	}
	return b.String()
}

// A toast is one row: a driver's error of many lines, tabs too, is
// flattened (§7.8).
func TestToastOneRow(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.showToast("play：failed to connect\n\t127.0.0.1: refused")
	if a.toast != "play：failed to connect； 127.0.0.1: refused" {
		t.Errorf("toast %q", a.toast)
	}
}

// teaKey turns a key in vim notation back into the press a terminal sends.
func teaKey(k keymap.Key) tea.KeyPressMsg {
	switch k {
	case "<CR>":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "<Esc>":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "<BS>":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "<M-BS>":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	case "<Space>":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "<Tab>":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "<S-Tab>":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "<Up>":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "<Down>":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "<Left>":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "<Right>":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "<S-Right>":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
	}
	if s := string(k); len(s) == 5 && s[:3] == "<C-" {
		return tea.KeyPressMsg{Code: rune(s[3]), Mod: tea.ModCtrl}
	}
	r, _ := utf8.DecodeRuneInString(keymap.Text(k))
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// feed presses keys written in vim notation and reports whether any of them
// quit the program.
func feed(t testing.TB, a *App, s string) (quit bool) {
	t.Helper()
	ks, err := keymap.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range ks {
		_, cmd := a.Update(teaKey(k))
		quit = quits(cmd) || quit
	}
	return quit
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if quits(c) {
				return true
			}
		}
	}
	return false
}

func TestQuitCommand(t *testing.T) {
	if !feed(t, sized(160, 45, "nerd"), ":qa<CR>") {
		t.Fatal(":qa did not quit")
	}
	a := sized(160, 45, "nerd")
	if feed(t, a, ":zzzz<CR>") || a.palette == nil {
		t.Fatal("↵ with nothing matching does nothing")
	}
}

func TestPaletteCloses(t *testing.T) {
	for _, in := range []string{":ab<Esc>", "<C-p>ab<C-c>"} {
		a := sized(160, 45, "nerd")
		feed(t, a, in)
		if a.palette != nil || a.mode() != keymap.Normal || a.toast != "" {
			t.Errorf("%q: palette %v, toast %q", in, a.palette, a.toast)
		}
	}
}

func TestDoubleCtrlCQuits(t *testing.T) {
	a := sized(160, 45, "nerd")
	if feed(t, a, "<C-c>") || a.toast != "再按一次 C-c 退出" {
		t.Fatalf("first C-c: toast %q", a.toast)
	}
	if !feed(t, a, "<C-c>") {
		t.Fatal("second C-c within 2s did not quit")
	}

	a = sized(160, 45, "nerd")
	_, cmd := a.Update(teaKey("<C-c>"))
	a.Update(cmd()) // quitWindow passes: the toast goes
	if a.toast != "" {
		t.Fatal("the quit toast should last quitWindow")
	}
	if feed(t, a, "<C-c>") || a.toast == "" {
		t.Fatal("C-c after the toast is gone must count as a new first press")
	}
	if !feed(t, a, "<C-c>") {
		t.Fatal("…and the next one quits")
	}
}

// The quit hint stays quitWindow, not as long as a toast does (F13.17).
func TestQuitHintLastsQuitWindow(t *testing.T) {
	wasTTL, wasWindow := toastTTL, quitWindow
	toastTTL, quitWindow = time.Hour, time.Millisecond
	t.Cleanup(func() { toastTTL, quitWindow = wasTTL, wasWindow })
	a := sized(160, 45, "nerd")
	_, cmd := a.Update(teaKey("<C-c>"))
	done := make(chan struct{})
	go func() { cmd(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the hint to press C-c again is held as long as a toast")
	}
	b := sized(160, 45, "nerd") // and a toast is held toastTTL
	cmd = b.showToast("x")
	done = make(chan struct{})
	go func() { cmd(); close(done) }()
	select {
	case <-done:
		t.Error("a toast is held as short as the quit hint")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCloseTab(t *testing.T) {
	a := twoPanes(160, 45)
	data := a.focused()
	feed(t, a, ":q<CR>")
	if tabNames(data) != "t_user" || data.Cur != 0 {
		t.Fatalf("after :q: tabs %v cur %d", tabNames(data), data.Cur)
	}
	feed(t, a, ":q<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || queryOf(leaves[0]) == nil || a.win().Focus != leaves[0].ID {
		t.Fatalf("closing the last tab should close the pane and focus the query: %v focus %d", leaves, a.win().Focus)
	}
	feed(t, a, ":q<CR>")
	if leaves := a.win().Root.Leaves(); len(leaves) != 1 || len(leaves[0].Tabs) != 0 {
		t.Fatalf("the only pane stays, empty: %v", leaves)
	}
	a.win().Focus = a.win().Tree.ID
	feed(t, a, ":q<CR>") // the sidebar never closes
	if a.win().Focus != a.win().Tree.ID {
		t.Fatal("focus moved")
	}
	a.View()
}

// Every key goes through an action: running the action directly gives the
// same state as pressing its keys.
func TestKeysRunActions(t *testing.T) {
	pressed, direct := sized(160, 45, "nerd"), sized(160, 45, "nerd")
	feed(t, pressed, ":")
	direct.run("palette.open", 0)
	if !reflect.DeepEqual(pressed.palette, direct.palette) {
		t.Error(": and palette.open differ")
	}

	pressed, direct = sized(160, 45, "nerd"), sized(160, 45, "nerd")
	feed(t, pressed, ":q<CR>")
	direct.run("tab.close", 0)
	if !reflect.DeepEqual(pressed.win(), direct.win()) {
		t.Error(":q and tab.close differ")
	}

	if !quits(sized(160, 45, "nerd").run("quit", 0)) {
		t.Error("quit did not quit")
	}
}

// With the palette open, keys go to it, not to the focused pane's scope: ↵
// runs the command instead of query.run.
func TestPaletteShadowsPaneKeys(t *testing.T) {
	a := twoPanes(160, 45)
	a.win().Focus = 2 // query
	if ctx := a.context(); ctx.Mode != keymap.Normal || ctx.Focus[0] != "query" {
		t.Fatalf("query context %+v", ctx)
	}
	feed(t, a, "<C-p>")
	if ctx := a.context(); ctx.Overlay != "palette" || ctx.Focus != nil || ctx.Mode != keymap.Command {
		t.Fatalf("palette context %+v", ctx)
	}
	if !feed(t, a, ">qa<CR>") {
		t.Fatal(":qa typed over the query did not quit")
	}
}

// An ambiguous binding fires when its timeoutlen tick comes back.
func TestAmbiguousKeyTimesOut(t *testing.T) {
	a := configured(t, 160, 45, "[keys.normal]\ng = \"palette.open\"")
	if _, cmd := a.Update(teaKey("g")); cmd == nil || a.palette != nil {
		t.Fatal("g should wait for timeoutlen")
	}
	a.Update(keyTimeout{a.res.Seq() - 1}) // stale
	if a.palette != nil {
		t.Fatal("a stale timeout fired")
	}
	a.Update(keyTimeout{a.res.Seq()})
	if a.palette == nil {
		t.Fatal("the timeout did not run g")
	}
}

func TestResizeNoPanic(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-c>:x")
	for _, s := range [][2]int{{100, 30}, {160, 45}, {80, 24}, {1, 1}, {0, 0}} {
		a.Update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
		a.View()
	}
}

// Typed text reaches the palette whole: multi-rune clusters are one key.
func TestPaletteTakesGraphemes(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-p>")
	for _, s := range []string{"e\u0301", "👍🏽", "🇨🇳"} {
		a.Update(tea.KeyPressMsg{Code: tea.KeyExtended, Text: s})
	}
	if got := a.palette.input.Text; got != "e\u0301👍🏽🇨🇳" {
		t.Fatalf("input = %q", got)
	}
	for _, want := range []string{"e\u0301👍🏽", "e\u0301", ""} { // backspace takes whole clusters, as in nvim
		feed(t, a, "<BS>")
		if got := a.palette.input.Text; got != want {
			t.Fatalf("after <BS>: %q, want %q", got, want)
		}
	}
}

// The quit toast names whatever key cancel is bound to (§6.7).
func TestQuitToastFollowsKeymap(t *testing.T) {
	a := configured(t, 160, 45, "[keys.global]\n\"<C-c>\" = \"\"\n\"<C-q>\" = \"cancel\"")
	if feed(t, a, "<C-q>") || a.toast != "再按一次 C-q 退出" {
		t.Fatalf("toast %q", a.toast)
	}
	if !feed(t, a, "<C-q>") {
		t.Fatal("second C-q did not quit")
	}
}

func TestCloseTabPicksNext(t *testing.T) {
	for _, c := range []struct {
		tabs      []string
		cur, prev int
		want      []string
		wantCur   int
	}{
		{[]string{"a", "b", "c"}, 0, 2, []string{"b", "c"}, 1},  // prev after closed shifts left
		{[]string{"a", "b", "c"}, 2, 0, []string{"a", "b"}, 0},  // prev before closed stays
		{[]string{"a", "b", "c"}, 1, -1, []string{"a", "c"}, 1}, // no prev: the tab that slid in
		{[]string{"a", "b", "c"}, 2, -1, []string{"a", "b"}, 1}, // no prev, last one: its left neighbour
		{[]string{"a", "b"}, 1, 1, []string{"a"}, 0},            // prev == closed counts as none
	} {
		a := sized(160, 45, "nerd")
		p := a.focused()
		p.Tabs, p.Cur, p.Prev = nil, c.cur, c.prev
		for _, name := range c.tabs {
			p.Tabs = append(p.Tabs, Tab{Name: name})
		}
		a.closeTab(p, p.Cur)
		if got := tabNames(p); got != strings.Join(c.want, " ") || p.Cur != c.wantCur || p.Prev != -1 {
			t.Errorf("%v cur %d prev %d: got %v cur %d prev %d; want %v cur %d", c.tabs, c.cur, c.prev, got, p.Cur, p.Prev, c.want, c.wantCur)
		}
	}
}

// SPC w n and SPC w p move the current tab to the next and the previous
// ordinary pane going clockwise, to the end of its tabs, current, the focus
// with it; the pane left empty is a landing page. The sidebar, a landing
// page and a window of one pane move nothing; a table goes to no pane that
// has it (F12.5).
func TestTabMove(t *testing.T) {
	a := twoPanes(160, 45)
	one, two := a.win().pane(1), a.win().pane(2)
	a.win().focus(1)
	feed(t, a, "<Space>wn") // t_order: ⟨1⟩ → ⟨2⟩
	if tabNames(one) != "t_user" || tabNames(two) != "query_1 t_order" || two.Cur != 1 || a.win().Focus != 2 || one.Cur != 0 {
		t.Fatalf("next: %q / %q cur %d, focus %d", tabNames(one), tabNames(two), two.Cur, a.win().Focus)
	}
	feed(t, a, "<Space>wn") // round the end: ⟨2⟩ → ⟨1⟩
	if tabNames(one) != "t_user t_order" || one.Cur != 1 || a.win().Focus != 1 || tabNames(two) != "query_1" {
		t.Fatalf("round: %q / %q, focus %d", tabNames(one), tabNames(two), a.win().Focus)
	}
	feed(t, a, "<Space>wp") // back
	if tabNames(two) != "query_1 t_order" || a.win().Focus != 2 {
		t.Fatalf("prev: %q / %q", tabNames(one), tabNames(two))
	}
	a.win().Zoom = 2 // only it is on screen: there is no other to go to
	feed(t, a, "<Space>wp")
	if a.win().Focus != 2 || tabNames(two) != "query_1 t_order" {
		t.Errorf("zoomed: focus %d, %q", a.win().Focus, tabNames(two))
	}
	a.win().Zoom = 0
	feed(t, a, "<Space>wn") // t_order again, then query_1: its pane is left a landing page
	a.win().focus(2)
	feed(t, a, "<Space>wn")
	if len(two.Tabs) != 0 || !two.tab().landing() || tabNames(one) != "t_user t_order query_1" {
		t.Errorf("emptied: %q / %q", tabNames(one), tabNames(two))
	}
	a.win().focus(2)
	feed(t, a, "<Space>wn") // a landing page is not moved
	if tabNames(one) != "t_user t_order query_1" || a.win().Focus != 2 {
		t.Errorf("landing: %q", tabNames(one))
	}
	a.win().focus(a.win().Tree.ID)
	feed(t, a, "<Space>wn")
	if tabNames(one) != "t_user t_order query_1" || a.win().Focus != a.win().Tree.ID {
		t.Errorf("sidebar moved a tab")
	}
	res := a.showResult(a.win()) // its tabs stay where they are
	a.win().focus(res.ID)
	n := len(res.Tabs)
	feed(t, a, "<Space>wn")
	if len(res.Tabs) != n || a.win().Focus != res.ID {
		t.Errorf("the result area moved a tab: %d, focus %d", len(res.Tabs), a.win().Focus)
	}
	// a table is in a pane once (F3.37)
	putTab(two, Tab{Name: "t_user", Data: newDataTab(a.sess.Conns[0], db.Table{Schema: "m0", Name: "t_user"})}, false)
	a.win().focus(2)
	feed(t, a, "<Space>wn")
	if !strings.Contains(a.toast, "已有这张表") || tabNames(two) != "t_user" || len(one.Tabs) != 3 {
		t.Errorf("a table to a pane that has it: toast %q, %q / %q", a.toast, tabNames(one), tabNames(two))
	}
	// a landing tab where it goes gives way to it
	two.Tabs, two.Cur = []Tab{{Name: "t_sku", Data: newDataTab(a.sess.Conns[0], db.Table{Schema: "m0", Name: "t_sku"})}}, 0
	putTab(one, landing(), false)
	a.win().focus(2)
	feed(t, a, "<Space>wn")
	if tabNames(one) != "t_user t_order query_1 t_sku" || one.Cur != 3 {
		t.Errorf("onto a landing tab: %q cur %d", tabNames(one), one.Cur)
	}
	a.removePane(2)
	a.win().focus(1)
	selectTab(one, 2) // query_1: to itself it would go last
	feed(t, a, "<Space>wn")
	if tabNames(one) != "t_user t_order query_1 t_sku" {
		t.Errorf("one pane: %q", tabNames(one))
	}
}

// gt / gT go round the focused pane's tabs as in vim; with a count gt goes
// to that tab and gT back that many (§7.8「tab 栏」).
func TestTabCycle(t *testing.T) {
	a := sized(160, 45, "nerd")
	p := a.focused()
	feed(t, a, "gt")
	if p.Cur != 0 || p.Prev != -1 {
		t.Fatalf("an empty pane: cur %d prev %d", p.Cur, p.Prev)
	}
	for _, n := range []string{"a", "b", "c", "d"} {
		p.Tabs = append(p.Tabs, Tab{Name: n})
	}
	for _, c := range []struct {
		keys      string
		cur, prev int
	}{
		{"gt", 1, 0}, {"gt", 2, 1}, {"gt", 3, 2}, {"gt", 0, 3}, // around the end
		{"gT", 3, 0}, {"3gt", 2, 3}, {"9gt", 2, 3}, // past the last tab: no move
		{"2gT", 0, 2}, {"3gT", 1, 0}, // around the start
	} {
		if feed(t, a, c.keys); p.Cur != c.cur || p.Prev != c.prev {
			t.Errorf("%s: cur %d prev %d, want %d %d", c.keys, p.Cur, p.Prev, c.cur, c.prev)
		}
	}
	a.win().focus(0)
	if feed(t, a, "gt"); p.Cur != 1 {
		t.Error("gt in the tree moved the data pane's tabs")
	}
}

// Opening a table goes to the target pane's tab of it, fetching nothing,
// a pane having one at most; another pane opens its own, to compare them.
// C-t and the tree's t do as ↵. The target is the pane focused last, a
// query's too (§7.8「打开已有的表」, §12, F3.37).
func TestOpenExistingTab(t *testing.T) {
	a := sized(160, 45, "nerd")
	left := a.focused()
	feed(t, a, "<C-p>@t_user<CR><C-p>@t_sku<C-t><C-p>@t_user<CR>")
	if a.win().Focus != left.ID || tabNames(left) != "t_user t_sku" || left.Cur != 0 || left.Prev != 1 || a.sess.Conns[0].busy != 2 {
		t.Fatalf("one t_user: focus %d tabs %v cur %d prev %d, %d fetches", a.win().Focus, tabNames(left), left.Cur, left.Prev, a.sess.Conns[0].busy)
	}
	if feed(t, a, "<C-p>@t_sku<C-t>"); tabNames(left) != "t_user t_sku" || left.Cur != 1 || a.sess.Conns[0].busy != 2 {
		t.Fatalf("C-t as ↵: tabs %v cur %d, %d fetches", tabNames(left), left.Cur, a.sess.Conns[0].busy)
	}
	if feed(t, a, "<C-p>@t_user"); len(a.paletteView().Enter) != 1 { // ↵ 打开 alone
		t.Errorf("the footer: %v", a.paletteView().Enter)
	}
	feed(t, a, "<Esc><C-l><C-p>@t_user<CR>") // ⟨2⟩ is query_1's: it opens there too, its own
	right := a.focused()
	if tabNames(right) != "query_1 t_user" || a.sess.Conns[0].busy != 3 {
		t.Fatalf("another pane's: tabs %v, %d fetches", tabNames(right), a.sess.Conns[0].busy)
	}
	a.win().focus(0)
	treeTo(t, a, "t_user")
	if feed(t, a, "t"); a.win().Focus != right.ID || tabNames(right) != "query_1 t_user" || a.sess.Conns[0].busy != 3 {
		t.Errorf("the tree's t, in the pane focused last: tabs %v, %d fetches", tabNames(right), a.sess.Conns[0].busy)
	}
	a.sess.Conns[0].Tables = append(a.sess.Conns[0].Tables, db.Table{Schema: "agentable", Name: "t_user"})
	feed(t, a, "<C-p>@t_user agentable<CR>") // the same name in another schema is another table
	if tabNames(right) != "query_1 t_user t_user" || dataOf(right).table.Schema != "agentable" {
		t.Errorf("agentable.t_user: tabs %v, schema %s", tabNames(right), dataOf(right).table.Schema)
	}
}

// A click on a tab switches to it and focuses its pane; what was typed in
// the tab it leaves goes.
func TestClickTab(t *testing.T) {
	a := sized(160, 45, "nerd")
	p := a.focused()
	feed(t, a, "<C-p>@t_user<CR><C-p>@t_sku<C-t>/x")
	left := dataOf(p)
	click(a, find(t, a, ui.Target{Kind: ui.KindTab, Pane: p.ID, I: 0}).Min)
	if p.Cur != 0 || p.Prev != 1 || left.typing != "" || left.where.Text != "" {
		t.Fatalf("cur %d prev %d, left tab typing %q %q", p.Cur, p.Prev, left.typing, left.where.Text)
	}
	a.win().focus(0)
	click(a, find(t, a, ui.Target{Kind: ui.KindTab, Pane: p.ID, I: 1}).Min)
	if p.Cur != 1 || a.win().Focus != p.ID {
		t.Errorf("from the tree: cur %d focus %d", p.Cur, a.win().Focus)
	}
}

// + opens a landing tab and switches to it. Its 打开表 puts the table
// picked in its place, with ↵ or C-t, or, one this pane has open, goes to
// that tab, the landing tab closed (F3.37); its 新建 query a query,
// the least query_n not open (§5「引导页」). x closes it.
func TestLandingTab(t *testing.T) {
	a := sized(160, 45, "nerd")
	left := a.focused()
	if a.paneScope() != "landing" {
		t.Fatalf("an empty pane: scope %s", a.paneScope())
	}
	feed(t, a, "<C-p>@t_user<CR>")
	plus := func() {
		click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: left.ID, Action: "tab.new"}).Min)
		if left.Cur != len(left.Tabs)-1 || !left.tab().landing() || left.Object() != "新 tab" || a.paneScope() != "landing" {
			t.Fatalf("+: tabs %v cur %d scope %s", tabNames(left), left.Cur, a.paneScope())
		}
	}
	plus()
	if left.Prev != 0 {
		t.Errorf("+: prev %d, want the tab it was on", left.Prev)
	}
	feed(t, a, "t")
	if a.palette == nil || a.palette.into != left || a.palette.input.Text != "@" {
		t.Fatalf("t: palette %+v", a.palette)
	}
	feed(t, a, "t_user<CR>") // open in ⟨1⟩ already: to its tab, the landing tab gone
	if tabNames(left) != "t_user" || left.Cur != 0 || a.palette != nil {
		t.Fatalf("打开表 ↵ of one open: tabs %v cur %d", tabNames(left), left.Cur)
	}
	plus()
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: left.ID, Action: "tab.table"}).Min)
	feed(t, a, "t_sku<C-t>")
	if tabNames(left) != "t_user t_sku" || left.Cur != 1 {
		t.Fatalf("打开表 C-t: tabs %v cur %d", tabNames(left), left.Cur)
	}
	plus()
	feed(t, a, "c") // query_1 is open in ⟨2⟩
	if tabNames(left) != "t_user t_sku query_2" || queryOf(left) == nil || a.paneScope() != "query" {
		t.Fatalf("c: tabs %v scope %s", tabNames(left), a.paneScope())
	}
	plus()
	if feed(t, a, "x"); tabNames(left) != "t_user t_sku query_2" || a.confirm != nil {
		t.Errorf("x on a landing tab: tabs %v", tabNames(left))
	}
}

// The tree's t (C-t, a middle click) opens a table over the target's
// landing tab too, not beside it (§5「引导页」).
func TestTreeTabOverLanding(t *testing.T) {
	a := sized(160, 45, "nerd")
	left := a.focused()
	feed(t, a, "<C-p>@t_user<CR>")
	a.run("tab.new", 0)
	a.win().focus(0)
	treeTo(t, a, "t_sku")
	if feed(t, a, "t"); tabNames(left) != "t_user t_sku" || left.Cur != 1 {
		t.Fatalf("tabs %v cur %d", tabNames(left), left.Cur)
	}
}

// query.new from the palette opens a query in a new tab of the focused
// pane, or openTarget's from the tree.
func TestNewQuery(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	left := a.focused()
	feed(t, a, "<C-p>@t_user<CR>")
	a.run("query.new", 0)
	if tabNames(left) != "t_user query_2" || left.Cur != 1 {
		t.Fatalf("from ⟨1⟩: %v", tabNames(left))
	}
	a.win().focus(0)
	a.run("query.new", 0) // the tree: the pane focused last whose current tab is no query, else the last
	if tabNames(left) != "t_user query_2 query_3" || a.win().Focus != left.ID {
		t.Fatalf("from the tree: %v, focus %d", tabNames(left), a.win().Focus)
	}
}

// From the tree a table goes to the pane focused last, a query's too:
// beside query_1 in the default layout (§5, F3.37).
func TestOpenTargetQueries(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<C-l><C-h><C-h>") // ⟨2⟩, then ⟨1⟩, then the tree
	a.win().focus(2)
	a.win().focus(0)
	treeTo(t, a, "t_user")
	feed(t, a, "<CR>")
	if p := a.focused(); p.ID != 2 || tabNames(p) != "query_1 t_user" {
		t.Fatalf("opened in ⟨%d⟩: %v", p.ID, tabNames(p))
	}
}

// due delivers the which-key tick the last key press asked for.
func due(a *App) { a.Update(whichKeyDue{a.res.Seq()}) }

func TestWhichKeyShowsAfterDelay(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	if a.whichKey {
		t.Fatal("which-key must wait for its delay")
	}
	due(a)
	if !a.whichKey {
		t.Fatal("which-key did not show")
	}
	var keys []string
	for _, it := range a.whichKeyOverlay().Items {
		keys = append(keys, it.Key)
		if it.Title == "" || strings.Contains(it.Title, ".") {
			t.Errorf("%s has no registry title: %q", it.Key, it.Title)
		}
	}
	// §6.8's SPC keys, in default.toml order
	if got := strings.Join(keys, " "); got != `s ) ( L c n p l % " z x q b r ? w` {
		t.Errorf("SPC items: %s", got)
	}
	if row := strings.Split(a.render().String(), "\n")[a.h-2]; !strings.HasPrefix(row, "└") {
		t.Errorf("the overlay should sit right above the status bar: %q", row)
	}
}

func TestWhichKeyNotForQuickKeys(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	seq := a.res.Seq()
	feed(t, a, "s")
	a.Update(whichKeyDue{seq})
	if a.whichKey {
		t.Fatal("SPC s typed quickly must not show which-key")
	}
}

// Keys typed with the overlay up do what they do without it; esc closes it
// and clears the pending keys.
func TestWhichKeyKeys(t *testing.T) {
	a := configured(t, 160, 45, "[keys.normal]\n\"<Leader>:\" = \"palette.command\"")
	feed(t, a, "<Space>")
	due(a)
	feed(t, a, ":")
	if a.whichKey || a.palette == nil {
		t.Fatal("SPC : through which-key should open the palette and close the overlay")
	}

	a = sized(160, 45, "nerd")
	feed(t, a, "<Space>")
	due(a)
	feed(t, a, "<Esc>")
	if a.whichKey || len(a.res.Pending()) != 0 {
		t.Fatalf("esc: overlay %v, pending %v", a.whichKey, a.res.Pending())
	}
}

// Every default binding's action has a title, so which-key, the ? help and
// the palette never show a raw ID (§6.7「标题」); those that mean something
// only inside an overlay or while typing (cell, input) are Local, which
// the palette must not list (§12).
func TestEveryDefaultActionHasATitle(t *testing.T) {
	keys, _ := newDefaultKeys()
	anywhere := []string{"global", "normal", "grid", "tree", "query", "queryinsert", "landing", "result"}
	for _, id := range keys.Actions() {
		listed := slices.ContainsFunc(anywhere, func(s string) bool { return keys.Hint(id, s) != "" }) || id == "json.yank" // the JSON view's alone, and the palette lists it (F16.9)
		if actions[id].Title == "" || listed == actions[id].Local {
			t.Errorf("%s: title %q, local %v, bound outside overlays and inputs %v", id, actions[id].Title, actions[id].Local, listed)
		}
	}
	for alias, id := range exAliases {
		if actions[id].Title == "" {
			t.Errorf(":%s runs %s, which has no title to rank", alias, id)
		}
	}
}

func titles(a *App) []string {
	var out []string
	for i, p := range a.win().Root.Leaves() {
		_, word := a.tabIcon(p.tab())
		out = append(out, fmt.Sprintf("⟨%d⟩%s", i+1, word))
	}
	return out
}

func TestSplitAndClosePanes(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, `<Space>"`) // split ⟨1⟩ data below
	if got := strings.Join(titles(a), " "); got != "⟨1⟩table ⟨2⟩ ⟨3⟩query" {
		t.Fatalf("after SPC \": %s", got)
	}
	if p := a.focused(); len(p.Tabs) != 0 || a.win().Focus != 3 {
		t.Fatalf("the new, empty pane should have focus: %+v", p)
	}
	rects := a.layout()
	if top, bot := rects[1], rects[3]; top.Max.Y != bot.Min.Y || top.Dx() != bot.Dx() {
		t.Errorf("stacked halves: %v %v", top, bot)
	}
	feed(t, a, "<Space>%") // and the new one right
	if got := strings.Join(titles(a), " "); got != "⟨1⟩table ⟨2⟩ ⟨3⟩ ⟨4⟩query" {
		t.Fatalf("after SPC %%: %s", got)
	}

	feed(t, a, "<Space>x") // close the focused (newest) pane
	after := a.layout()
	if len(a.win().Root.Leaves()) != 3 || after[3] != rects[3] {
		t.Errorf("closing should give its sibling the whole space back: %v vs %v", after[3], rects[3])
	}
	feed(t, a, "<Space>x<Space>x<Space>x") // down to one pane, which stays
	if n := len(a.win().Root.Leaves()); n != 1 {
		t.Errorf("the last pane must stay, have %d", n)
	}
}

func TestFocusFollowsGeometry(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, `<Space>"`) // 1 over 3, query 2 on the right
	for _, c := range []struct {
		keys string
		want int
	}{
		{"<C-k>", 1},
		{"<C-l>", 2},
		{"<C-h>", 1}, // back where it came from, not just the first candidate
		{"<C-h>", 0}, // the sidebar
		{"<C-l>", 1},
		{"<C-j>", 3},
		{"<C-j>", 3}, // nothing below: stays, no wrapping round
		{"<C-l>", 2},
		{"<C-h>", 3}, // came from the lower half: back to it
	} {
		feed(t, a, c.keys)
		if a.win().Focus != c.want {
			t.Fatalf("%s: focus %d, want %d", c.keys, a.win().Focus, c.want)
		}
	}
}

func TestZoom(t *testing.T) {
	a := twoPanes(160, 45)
	before := a.layout()
	feed(t, a, "<Space>z")
	if r := a.layout(); len(r) != 1 || r[1] != uv.Rect(0, 0, 160, 44) {
		t.Fatalf("zoomed layout: %v", r)
	}
	if strings.Contains(a.render().String(), "query") {
		t.Error("other panes must not be drawn while zoomed")
	}
	feed(t, a, "<Space>z")
	if !reflect.DeepEqual(a.layout(), before) {
		t.Error("SPC z again should restore the layout")
	}
}

// Keys the defaults dropped come back through config.toml, which-key
// included (§6.8).
func TestUserLeaderKeys(t *testing.T) {
	a := configured(t, 160, 45, "[keys.normal]\n\"<Leader>h\" = \"pane.focus.left\"")
	feed(t, a, "<Space>")
	due(a)
	if !slices.Contains(a.whichKeyOverlay().Items, ui.WhichKeyItem{Key: "h", Title: "焦点移到左边", Group: "keys.normal"}) {
		t.Errorf("which-key lacks SPC h: %v", a.whichKeyOverlay().Items)
	}
	feed(t, a, "h")
	if a.win().Focus != 0 {
		t.Errorf("SPC h: focus %d, want the sidebar", a.win().Focus)
	}
}

func TestResizeKeys(t *testing.T) {
	a := m0Layout(configured(t, 160, 45, "[keys.normal]\n\"<Leader>H\" = \"pane.resize.left\"\n\"<Leader>L\" = \"pane.resize.right\""))
	r0 := a.win().Root.Ratio
	feed(t, a, "<Space>L")
	if got := a.win().Root.Ratio; math.Abs(got-r0-resizeStep) > 1e-9 {
		t.Fatalf("SPC L: ratio %v, want %v", got, r0+resizeStep)
	}
	feed(t, a, "3<Space>H") // the count scales the step
	if got := a.win().Root.Ratio; math.Abs(got-r0+2*resizeStep) > 1e-9 {
		t.Fatalf("3 SPC H: ratio %v", got)
	}
	if a.layout()[1].Dx() >= twoPanes(160, 45).layout()[1].Dx() {
		t.Error("the data pane should have narrowed")
	}
}

func TestPaneNumbers(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, "<Space>q")
	if !a.paneNumbers || !strings.Contains(a.render().String(), " 2 ") {
		t.Fatal("SPC q should show the numbers")
	}
	feed(t, a, "2")
	if a.paneNumbers || a.win().Focus != 2 {
		t.Fatalf("2 should jump to ⟨2⟩: focus %d", a.win().Focus)
	}
	feed(t, a, "<Space>q0")
	if a.win().Focus != 0 {
		t.Fatal("0 is the sidebar")
	}
	feed(t, a, "<Space>qj")
	if a.paneNumbers || a.win().Focus != 0 {
		t.Fatal("a non-digit just closes the numbers")
	}
	feed(t, a, "<Space>q9")
	if a.win().Focus != 0 {
		t.Fatal("no ⟨9⟩: nothing moves")
	}
}

// The « on the sidebar's title folds it, the » on the folded bar unfolds
// it, a click each; the bar says nothing else (§7.8, F10.9).
func TestFoldSidebar(t *testing.T) {
	a := sized(160, 45, "nerd")
	a.win().Focus = 0
	at := find(t, a, ui.Target{Kind: ui.KindHint, Pane: 0, Action: "tree.fold"}).Min
	if cell := a.render().Buf.CellAt(at.X, at.Y); cell == nil || cell.Content != "«" || cell.Style.Fg != a.theme.Dim {
		t.Errorf("the « on the title is %+v, want dim", cell)
	}
	click(a, at)
	if r := a.layout()[0]; r.Dx() != 3 {
		t.Fatalf("a click on «: bar %v", r)
	}
	click(a, uv.Pos(1, 1))
	if r := a.layout()[0]; r.Dx() != 32 {
		t.Fatalf("a click on »: bar %v", r)
	}
	a.win().Focus = 0
	feed(t, a, "<Space>b")
	if r := a.layout()[0]; r.Dx() != 3 || a.win().Focus == 0 {
		t.Fatalf("folded: bar %v, focus %d", r, a.win().Focus)
	}
	col := func(y int) string { return string([]rune(strings.Split(a.render().String(), "\n")[y])[1]) }
	var down string
	for y := 1; y <= 15; y++ {
		down += col(y)
	}
	if down != "»"+strings.Repeat(" ", 14) {
		t.Errorf("thin bar reads %q", down)
	}
	feed(t, a, "<C-h><C-h>")
	if a.win().Focus == 0 {
		t.Error("a folded sidebar takes no focus")
	}
	feed(t, a, "<Space>b")
	if r := a.layout()[0]; r.Dx() != 32 || a.win().Focus != 0 {
		t.Errorf("unfolded width %d, focus %d", r.Dx(), a.win().Focus)
	}
}

// tree.toggle as DataGrip's tool windows: open with the focus on the tree it
// folds and the focus goes back to the pane it came from, whichever way it
// came, or the window's current one when that pane is gone; open with the
// focus elsewhere it only takes the focus; folded it opens with the focus on
// it; a zoom ends first (§7.8, F19.5).
func TestTreeToggleFocus(t *testing.T) {
	state := func(a *App) (open bool, focus int) { return a.win().TreeOpen, a.win().Focus }
	a := sized(160, 45, "nerd")
	a.win().focus(2)
	a.run("tree.toggle", 0)
	if open, focus := state(a); !open || focus != 0 {
		t.Fatalf("open, focus on a pane: open %v focus %d", open, focus)
	}
	a.run("tree.toggle", 0)
	if open, focus := state(a); open || focus != 2 {
		t.Fatalf("open, focus on the tree: open %v focus %d, want folded and ② (not the first pane)", open, focus)
	}
	a.run("tree.toggle", 0)
	if open, focus := state(a); !open || focus != 0 {
		t.Fatalf("folded: open %v focus %d", open, focus)
	}

	a.win().focus(1) // by a click: the way back is the same
	click(a, uv.Pos(1, 1))
	a.win().focus(2)
	click(a, find(t, a, ui.Target{Kind: ui.KindTreeEdge}).Min.Sub(uv.Pos(10, 0))) // the tree's own area
	if a.win().Focus != 0 {
		t.Fatalf("a click on the tree: focus %d", a.win().Focus)
	}
	a.run("tree.toggle", 0)
	if open, focus := state(a); open || focus != 2 {
		t.Fatalf("after a click: open %v focus %d", open, focus)
	}

	a = sized(160, 45, "nerd") // the pane it came from is closed meanwhile: the one focused last of the others
	a.win().focus(2)
	a.splitPane(Horiz) // ③ right of ②, focused
	a.run("tree.toggle", 0)
	a.removePane(3) // focus goes to the heir, ②; the tree takes it back, as closing from its menu leaves it
	a.win().Focus = 0
	a.run("tree.toggle", 0)
	if open, focus := state(a); open || focus != 2 {
		t.Fatalf("its pane closed: open %v focus %d, want ②", open, focus)
	}

	a = sized(160, 45, "nerd") // the result area is a pane it can come from
	res := a.resultPane(a.win())
	a.win().focus(res.ID)
	a.run("tree.toggle", 0)
	a.run("tree.toggle", 0)
	if open, focus := state(a); open || focus != res.ID {
		t.Fatalf("from the result area: open %v focus %d, want %d", open, focus, res.ID)
	}

	a = sized(160, 45, "nerd") // a zoomed pane hides the sidebar: the zoom ends, then it is the focus that moves
	a.win().focus(2)
	a.run("pane.zoom", 0)
	a.run("tree.toggle", 0)
	if open, focus := state(a); !open || focus != 0 || a.win().Zoom != 0 {
		t.Fatalf("zoomed: open %v focus %d zoom %d", open, focus, a.win().Zoom)
	}

	a = sized(160, 45, "nerd") // « folds, wherever the focus is, and moves it only off the tree; » opens with the focus on it
	a.win().focus(2)
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: 0, Action: "tree.fold"}).Min)
	if open, focus := state(a); open || focus != 2 {
		t.Fatalf("« with the focus on ②: open %v focus %d", open, focus)
	}
	click(a, uv.Pos(1, 1))
	if open, focus := state(a); !open || focus != 0 {
		t.Fatalf("»: open %v focus %d", open, focus)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindHint, Pane: 0, Action: "tree.fold"}).Min)
	if open, focus := state(a); open || focus != 2 {
		t.Fatalf("« with the focus on the tree: open %v focus %d, want ②", open, focus)
	}
}

// SPC b unfolding the tree puts the focus on it, its cursor on the node of
// the tab focused before, the nodes above it opened: a table's under its
// schema, a query's in the workspace (F3.29). Filtered out, nothing
// opens.
func TestFoldSidebarReveals(t *testing.T) {
	a := sized(160, 45, "nerd")
	loadOrders(t, a, 3)
	a.sess.open[schemaNode(a.sess.Conns[0], "public")] = false // folded by hand
	a.win().tree.filter = ui.Input{Text: "planner"}
	if feed(t, a, "<Space>b"); a.sess.open[schemaNode(a.sess.Conns[0], "public")] {
		t.Fatal("filtered out, t_order's schema opens")
	}
	a.win().tree.filter = ui.Input{}
	feed(t, a, "<C-l><Space>b")
	if n, _ := a.treeNode(); a.win().Focus != 0 || n.kind != nodeTable || n.table.Name != "t_order" {
		t.Fatalf("t_order's: focus %d, node %+v", a.win().Focus, n.TreeNode)
	}
	feed(t, a, "<Space>b<C-l><Space>b") // query_1
	if n, _ := a.treeNode(); n.kind != nodeTab || n.Text != "query_1" {
		t.Errorf("query_1's: node %+v", n.TreeNode)
	}
}

// Closing a zoomed pane (here with :q) ends the zoom: the zoom must never
// point at a pane that is gone.
func TestCloseZoomedPane(t *testing.T) {
	a := twoPanes(160, 45)
	feed(t, a, "<Space>z:q<CR>:q<CR>")
	if a.win().Zoom != 0 || len(a.layout()) != 2 || !strings.Contains(a.render().String(), "query") {
		t.Fatalf("zoom %d, layout %v", a.win().Zoom, a.layout())
	}
}

// Split IDs are never handed out twice, even after the highest is closed.
func TestSplitIDsStayUnique(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>%")
	first := a.win().Focus
	feed(t, a, "<Space>x<Space>%")
	if a.win().Focus == first {
		t.Fatalf("pane ID %d reused", first)
	}
}

// quitOnMode stops the program once the mode report Init sends comes back.
type quitOnMode struct{ *App }

func (q quitOnMode) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.ModeReportMsg); ok {
		return q, tea.Quit
	}
	q.App.Update(msg)
	return q, nil
}

// Init's borrowed mode report must reach the renderer: with no terminal
// answering 2027, the output still switches to grapheme widths (§7.1).
func TestRendererUsesGraphemeWidths(t *testing.T) {
	var out bytes.Buffer
	c := config.Default()
	keys, _ := keymap.New(c.Leader, c.Vim, c.Timeoutlen, c.Bindings)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second) // Init stopped sending it
	defer cancel()
	p := tea.NewProgram(quitOnMode{newApp(c, keys, testSession(), &config.State{}, "")}, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(&out),
		tea.WithWindowSize(80, 24), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), ansi.SetModeUnicodeCore) {
		t.Fatal("the renderer never switched to grapheme widths")
	}
}

// A startup warning, such as a readable plain password (§13), is a toast.
func TestStartupWarningIsAToast(t *testing.T) {
	c := config.Default()
	keys, _ := keymap.New(c.Leader, c.Vim, c.Timeoutlen, c.Bindings)
	a := newApp(c, keys, testSession(), &config.State{}, "careful")
	a.Init()
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	if row := strings.Split(a.render().String(), "\n")[43]; !strings.HasSuffix(row, " careful ┘") {
		t.Fatalf("toast row %q", row)
	}
}

// With t and tc both bound, the key after t that is no tc is read once t has
// run, as nvim does: on a landing page t opens the palette on tables, and
// x, :, ; are then its text, not the page's keys (F8.15).
func TestKeyAfterBoundPrefixMeetsWhatItRan(t *testing.T) {
	for keys, want := range map[string]string{"tx": "@x", "t:": "@:", "t;": "@;"} {
		a := configured(t, 160, 45, "[keys.normal]\ntc = \"query.new\"")
		if feed(t, a, keys); a.palette == nil || a.palette.input.Text != want {
			t.Errorf("%s: palette %+v, want its input %q", keys, a.palette, want)
		}
	}
}
