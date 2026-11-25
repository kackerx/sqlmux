package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/ui"
)

// withSessions is sized with a second session, report, of two windows, as
// gotSessionDialed would make one.
func withSessions() (*App, *Session) {
	a := sized(160, 45, "nerd")
	c := readyConn("doraemon")
	s := newSession("report", []*Conn{c}, c, a.freeQuery())
	s.Windows = append(s.Windows, &Window{Name: "logs"})
	a.sessions = append(a.sessions, s)
	return a, s
}

// session.next and session.prev go round the list; session.last goes back to
// the one attached before; with one session they do nothing (F9.10).
func TestSessionNextPrevLast(t *testing.T) {
	a, report := withSessions()
	third := newSession("third", []*Conn{readyConn("doraemon")}, nil, a.freeQuery())
	a.sessions = append(a.sessions, third)
	doraemon := a.sess
	if a.sessions[0] != doraemon {
		t.Fatal("the first is not the one shown")
	}
	for _, c := range []struct {
		keys string
		want *Session
	}{
		{"<Space>)", report}, {"<Space>)", third}, {"<Space>)", doraemon}, // round the end
		{"<Space>(", third}, {"<Space>(", report}, {"<Space>(", doraemon}, // and the start
		{"<Space>L", report}, {"<Space>L", doraemon}, {"<Space>L", report}, // between the last two
	} {
		if feed(t, a, c.keys); a.sess != c.want {
			t.Fatalf("%s: on %s, want %s", c.keys, a.sess.Name, c.want.Name)
		}
	}
	// a session closed is not gone back to
	a.sessions = []*Session{doraemon, third}
	a.attach(doraemon)
	a.lastSess = report
	if feed(t, a, "<Space>L"); a.sess != doraemon {
		t.Errorf("last, closed: on %s", a.sess.Name)
	}
	a.sessions = []*Session{doraemon}
	for _, keys := range []string{"<Space>)", "<Space>(", "<Space>L"} {
		if feed(t, a, keys); a.sess != doraemon {
			t.Errorf("%s with one session: on %s", keys, a.sess.Name)
		}
	}
}

// ↵ in the session list on the session shown attaches it to itself, which is
// no switch: SPC L still goes back to the one before (F9.10).
func TestSessionLastAfterAttachingItself(t *testing.T) {
	a, report := withSessions()
	doraemon := a.sess
	feed(t, a, "<Space>)")
	if a.sess != report {
		t.Fatal("not on report")
	}
	feed(t, a, "<Space>s<CR>") // the list opens on the session shown
	if a.sess != report || a.sessList != nil {
		t.Fatalf("↵: on %s", a.sess.Name)
	}
	if feed(t, a, "<Space>L"); a.sess != doraemon {
		t.Errorf("SPC L: on %s, want %s", a.sess.Name, doraemon.Name)
	}
}

// The session list (§7.8): open on the session attached; l shows a
// session's windows, h hides them; ↵ attaches, on a window switching to
// it too; a click on a row is ↵ on it; the status bar's session block and
// the palette's $ scope get there as well (§12).
func TestSessionList(t *testing.T) {
	a, report := withSessions()
	doraemon := a.sess
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "session.list"}).Min)
	if a.sessList == nil || a.sessList.sel != 0 || a.mode().String() != "command" {
		t.Fatalf("SPC s: %+v", a.sessList)
	}
	feed(t, a, "jl")
	golden.RequireEqual(t, a.render().String())
	feed(t, a, "jjh")
	if a.sessList.sel != 1 || len(a.sessionRows()) != 2 {
		t.Fatalf("h on a window: row %d of %d", a.sessList.sel, len(a.sessionRows()))
	}
	feed(t, a, "lj<CR>")
	if a.sess != report || report.Active != 0 || a.sessList != nil {
		t.Fatalf("↵ on 0: data: session %s window %d", a.sess.Name, report.Active)
	}
	feed(t, a, "<Space>s")
	click(a, find(t, a, ui.Target{Kind: ui.KindRow, I: 0}).Min)
	if a.sess != doraemon || a.sessList != nil {
		t.Errorf("a click on doraemon: %s", a.sess.Name)
	}
	if feed(t, a, "<C-p>$rep<CR>"); a.sess != report || a.palette != nil {
		t.Errorf("$rep: %s", a.sess.Name)
	}
}

// $ renames in place: nothing typed, or another session's name, keeps the
// name (§7.8).
func TestSessionRename(t *testing.T) {
	a, report := withSessions()
	feed(t, a, "<Space>sj$")
	if a.mode().String() != "insert" {
		t.Fatalf("mode %v", a.mode())
	}
	feed(t, a, "<C-u>doraemon<CR>")
	if report.Name != "report" || a.toast != "已有同名 session" {
		t.Errorf("a name taken: %s, toast %q", report.Name, a.toast)
	}
	feed(t, a, "$<C-u> <CR>$<C-u>logs<Esc>")
	if report.Name != "report" || a.sessList.rename != nil {
		t.Errorf("blank, then esc: %s", report.Name)
	}
	a.toast = ""
	if feed(t, a, "$<CR>"); report.Name != "report" || a.toast != "" {
		t.Errorf("its own name: %s, toast %q", report.Name, a.toast)
	}
	if feed(t, a, "$<BS><BS>rt2<CR>"); report.Name != "report2" {
		t.Errorf("renamed: %s", report.Name)
	}
}

// x closes a session: asked first with changes or a transaction in it;
// the one attached gives way to the one before it; the list stays; the
// last one is quitting. A window's row takes no x (§7.8).
func TestSessionKill(t *testing.T) {
	a, report := withSessions()
	doraemon := a.sess
	a.attach(report)
	report.Conns[0].tx = 'T'
	tab := loadOrders(t, a, 3)
	tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}}
	feed(t, a, "<Space>sjlj")
	if feed(t, a, "x"); len(a.sessions) != 2 || a.confirm != nil {
		t.Fatal("x on a window's row")
	}
	feed(t, a, "kx")
	if a.confirm == nil || a.confirm.text != "report 里有 1 处修改未保存，关闭会丢弃。\ndoraemon 有未提交的事务，关闭会回滚。" {
		t.Fatalf("confirm %+v", a.confirm)
	}
	if feed(t, a, "y"); len(a.sessions) != 1 || a.sess != doraemon || a.sessList == nil || a.sessList.sel != 0 {
		t.Fatalf("killed: %d left, on %s, list %+v", len(a.sessions), a.sess.Name, a.sessList)
	}
	tab = loadOrders(t, a, 3)
	tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}}
	feed(t, a, "<Space>sx")
	if a.confirm == nil || !strings.Contains(a.confirm.text, "退出会丢弃") {
		t.Errorf("the last one quits: %+v", a.confirm)
	}
}

// n makes a session once its connection is connected; not connecting, it
// makes none (§7.8「新建 session」).
func TestNewSessionFails(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>s")
	feed(t, a, "n")
	_, cmd := a.Update(teaKey("<CR>"))
	if cmd == nil || a.sessList != nil || a.toast != "正在连接 doraemon…" {
		t.Fatalf("n: list %v, toast %q", a.sessList, a.toast)
	}
	if a.Update(sessionDialed{name: "1", spec: a.sess.Conns[0].Spec, err: errors.New("refused")}); len(a.sessions) != 1 || a.toast != "doraemon：refused" {
		t.Errorf("failed: %d sessions, toast %q", len(a.sessions), a.toast)
	}
}

// closing is a connection that counts its Close.
type closing struct {
	noDB
	n *int
}

func (c closing) Close() error { *c.n++; return nil }

// session.new and n in the list ask for the name first, with the least free
// number in the box, which a blank ↵ takes; a name another session has is
// refused and the input stays, esc makes none; one taken while the
// connection dials makes none (F10.2).
func TestNewSessionAsksName(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, _ := withSessions() // doraemon and report
	answer := func(name string) sessionDialed {
		spec := a.sess.Conns[0].Spec
		return sessionDialed{name: name, spec: spec, d: dialed{main: noDB{}, meta: noDB{}, spec: spec}}
	}
	feed(t, a, "<C-p>session.new<CR>")
	if a.askName == nil || a.askName.in.Text != "1" || a.sessList != nil {
		t.Fatalf("the input: %+v", a.askName)
	}
	if feed(t, a, "<C-u>report<CR>"); a.askName == nil || a.toast != "已有同名 session" || len(a.sessions) != 2 {
		t.Errorf("a name taken: input %v, toast %q", a.askName, a.toast)
	}
	toast := a.toast
	if _, cmd := a.Update(teaKey("<Esc>")); a.askName != nil || len(a.sessions) != 2 || cmd != nil || a.toast != toast {
		t.Errorf("esc makes none, not even a dial: input %v, %d sessions, toast %q", a.askName, len(a.sessions), a.toast)
	}
	feed(t, a, "<Space>sn<C-u>")
	_, cmd := a.Update(teaKey("<CR>")) // blank: the number in the box to begin with
	if a.askName != nil || cmd == nil || a.toast != "正在连接 doraemon…" {
		t.Fatalf("blank ↵: input %v, toast %q", a.askName, a.toast)
	}
	if a.Update(answer("1")); a.sess.Name != "1" || len(a.sessions) != 3 {
		t.Errorf("made %q, %d sessions", a.sess.Name, len(a.sessions))
	}
	feed(t, a, "<C-p>session.new<CR><C-u>ops<CR>")
	if a.Update(answer("ops")); a.sess.Name != "ops" || len(a.sessions) != 4 {
		t.Errorf("made %q, %d sessions", a.sess.Name, len(a.sessions))
	}
	// "ops" taken while this one dialed: none is made, the two connections dialed close
	closed := 0
	late := answer("ops")
	late.d.main, late.d.meta = closing{n: &closed}, closing{n: &closed}
	_, cmd = a.Update(late)
	deliver(a, cmd)
	if len(a.sessions) != 4 || a.toast != "已有同名 session" || closed != 2 {
		t.Errorf("a name taken meanwhile: %d sessions, toast %q, %d closed", len(a.sessions), a.toast, closed)
	}
}

// The pane of a new window is pane 1, the numbers go on from it: a split
// makes pane 2, not 3 (F8.18).
func TestNewWindowSplitNumbering(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>c<Space>%")
	var ids []int
	for _, p := range a.win().Root.Leaves() {
		ids = append(ids, p.ID)
	}
	if !slices.Equal(ids, []int{1, 2}) {
		t.Errorf("panes %v", ids)
	}
}

// A session made is one window of one empty pane, on the connection that was
// dialed for it, and no query, nor a file for one (F8.18).
func TestNewSessionIsOneEmptyPane(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	_, cmd := a.Update(sessionDialed{name: "1", spec: a.sess.Conns[0].Spec, d: dialed{main: noDB{}, meta: noDB{}, spec: a.sess.Conns[0].Spec}})
	s := a.sess
	if m, ok := cmd().(catalogMsg); !ok || m.conn != s.start { // its connections are its own: the tree needs the catalog of this one
		t.Errorf("the session loads the catalog of its start: %T", cmd())
	}
	if len(a.sessions) != 2 || s.Name != "1" || len(s.Windows) != 1 {
		t.Fatalf("%d sessions, attached %q with %d windows", len(a.sessions), s.Name, len(s.Windows))
	}
	if ps := s.Windows[0].Root.Leaves(); len(ps) != 1 || len(ps[0].Tabs) != 0 || len(s.queries()) != 0 {
		t.Errorf("%d panes, %d queries", len(s.Windows[0].Root.Leaves()), len(s.queries()))
	}
	if es, _ := os.ReadDir(filepath.Join(config.DataDir(), "queries")); len(es) != 0 {
		t.Errorf("files made for it: %v", es)
	}
	if s.start.State != connReady || s.last != s.start {
		t.Error("the connection dialed is its start")
	}
}

// conn.new opens the form; ↵ checks the connection as connections.toml is
// read, the field at fault getting the keys; one that checks goes at the
// end of the file and into every session's tree, not connected (§7.8).
func TestConnForm(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, report := withSessions()
	feed(t, a, "<C-p>conn.new<CR><Tab><Tab><Space><Tab>db.example<Tab><C-u>3307<Tab>app<Tab><Tab>shop<Tab> <CR>")
	if a.form == nil || a.form.sel != 0 || a.form.err != "缺少 name" || a.form.fields[fieldReadOnly].On != 1 {
		t.Fatalf("no name: %+v", a.form)
	}
	feed(t, a, "doraemon<CR>")
	golden.RequireEqual(t, a.render().String())
	feed(t, a, "<C-u>play<CR>")
	if a.form != nil || len(a.sess.Conns) != 2 || len(report.Conns) != 2 || a.sess.Conns[1].State != connIdle {
		t.Fatalf("saved: form %+v, conns %d %d", a.form, len(a.sess.Conns), len(report.Conns))
	}
	data, _ := os.ReadFile(filepath.Join(config.Dir(), "connections.toml"))
	if string(data) != "[[connection]]\nname = \"play\"\nengine = \"mysql\"\ndsn = \"mysql://app@db.example:3307/shop\"\nread_only = true\n" {
		t.Errorf("file:\n%s", data)
	}
}

// SPC c makes a window of one empty pane at the end, shown (F8.18); SPC n / SPC p go round; SPC l goes back to the
// one used before, by its ID, gone with it; a click on the status bar's
// window switches; the tree's window node too (§5, F5.2).
func TestWindows(t *testing.T) {
	a := sized(160, 45, "nerd")
	data := a.win()
	feed(t, a, "<Space>c")
	w := a.win()
	if len(a.sess.Windows) != 2 || a.sess.Active != 1 || w.Name != "data-2" || w.ID != 2 {
		t.Fatalf("SPC c: %d windows, on %d %q", len(a.sess.Windows), a.sess.Active, w.Name)
	}
	// one empty pane, the landing page, focused; no query, no file for it (F8.18)
	if ps := w.Root.Leaves(); len(ps) != 1 || len(ps[0].Tabs) != 0 || w.Focus != ps[0].ID || len(a.sess.queries()) != 1 {
		t.Errorf("a new window: %d panes, %d queries in the session", len(w.Root.Leaves()), len(a.sess.queries()))
	}
	feed(t, a, "<Space>c<Space>n")
	if a.sess.Active != 0 {
		t.Errorf("SPC n round the end: %d", a.sess.Active)
	}
	if feed(t, a, "<Space>p"); a.sess.Active != 2 {
		t.Errorf("SPC p round the start: %d", a.sess.Active)
	}
	feed(t, a, "<Space>p<Space>l")
	if a.sess.Active != 2 {
		t.Errorf("SPC l: %d", a.sess.Active)
	}
	click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "window.select 0"}).Min)
	if a.win() != data {
		t.Errorf("a click on 0: data: %d", a.sess.Active)
	}
	a.win().focus(0)
	ns, _ := a.treeNodes()
	a.treeGo(slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeWindow && n.win == 1 }))
	if feed(t, a, "<CR>"); a.sess.Active != 1 {
		t.Errorf("↵ on a window node: %d", a.sess.Active)
	}
}

// window.close: asked first with changes, its queries written; the one
// used before shown, else the one before; the last one stays. window.rename
// types the name in the status bar, in its place: ↵ takes it but a blank
// one, esc drops it (§5, F5.2).
func TestWindowCloseRename(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>c<Space>c<Space>p") // on 1, 2 used before
	if a.run("window.close", 0); len(a.sess.Windows) != 2 || a.sess.Active != 1 || a.win().ID != 3 {
		t.Fatalf("closed 1: %d left, on %d (ID %d)", len(a.sess.Windows), a.sess.Active, a.win().ID)
	}
	tab := loadOrders(t, a, 3)
	tab.edits = map[editKey]edit{{"1", "note"}: {val: db.Val{S: "x"}}}
	if a.run("window.close", 0); a.confirm == nil || a.confirm.text != "这个 window 里有 1 处修改未保存，关闭会丢弃。" || len(a.sess.Windows) != 2 {
		t.Fatalf("with changes: %+v", a.confirm)
	}
	if feed(t, a, "y"); len(a.sess.Windows) != 1 || a.sess.Active != 0 {
		t.Fatalf("closed 2: %d left, on %d", len(a.sess.Windows), a.sess.Active)
	}
	if a.run("window.close", 0); len(a.sess.Windows) != 1 || a.confirm != nil {
		t.Error("the last window closed")
	}
	a.run("window.rename", 0)
	if a.mode().String() != "insert" || a.render().Cursor == nil || !strings.Contains(statusRow(a), " 0: data  ") {
		t.Fatalf("rename: mode %v, bar %q", a.mode(), statusRow(a))
	}
	feed(t, a, "<C-u>main<CR>")
	if a.win().Name != "main" || a.winRename != nil {
		t.Errorf("↵: %q", a.win().Name)
	}
	a.run("window.rename", 0)
	feed(t, a, "<C-u> <CR>")
	a.run("window.rename", 0)
	if feed(t, a, "x<Esc>"); a.win().Name != "main" || !strings.Contains(statusRow(a), " 0: main* ") {
		t.Errorf("blank, then esc: %q", a.win().Name)
	}
}

// Attaching a session closes every overlay, a question or a name typed in
// the one left among them; a session in the background still refreshes
// its tables in turn, and a catalog of its own opens its own tree's tables
// again (F5.1).
func TestAttachAcrossSessions(t *testing.T) {
	a, report := withSessions()
	tab := loadOrders(t, a, 3)
	tab.auto, tab.autoGen, tab.out = 2*time.Second, 1, 0
	a.confirm, a.winRename, a.form = &confirmBox{}, &ui.Input{}, &connForm{}
	a.openPalette("")
	if a.attach(report); a.confirm != nil || a.winRename != nil || a.form != nil || a.palette != nil {
		t.Errorf("overlays stay: %v %v %v %v", a.confirm, a.winRename, a.form, a.palette)
	}
	if a.gotAuto(autoMsg{tab, 1}) == nil {
		t.Error("a background table's auto refresh stopped")
	}
	c, only := report.Conns[0], db.Table{Schema: "public", Name: "only_in_report"}
	report.open[tableNode(c, only)] = true
	a.attach(a.sessions[0])
	if !asksCols(a.gotCatalog(catalogMsg{conn: c, schemas: []string{"public"}, current: "public", tables: []db.Table{only}})) {
		t.Error("report's open table: no columns asked")
	}
}

// asksCols reports whether cmd asks for a table's columns.
func asksCols(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		return slices.ContainsFunc(m, asksCols)
	case colsMsg:
		return true
	}
	return false
}

// The list's ↵ on a window, as a palette's window, by its ID, a recent one
// too, keeps SPC l's way back; a name being typed in the status bar goes
// with a switch, or a click off it, which does nothing more (F5.2).
func TestWindowSwitches(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>c<Space>c<Space>l") // on 1, 2 before
	feed(t, a, "<Space>s")
	a.sessionsFold(true)
	a.sessionsAttach(1) // 0: data
	if feed(t, a, "<Space>l"); a.sess.Active != 1 {
		t.Errorf("SPC l after the list's 0: %d", a.sess.Active)
	}
	a.run("window.rename", 0)
	if click(a, find(t, a, ui.Target{Kind: ui.KindButton, Action: "window.select 2"}).Min); a.winRename != nil || a.sess.Active != 1 {
		t.Errorf("a click off the name: renaming %v, on %d", a.winRename != nil, a.sess.Active)
	}
	a.run("window.rename", 0)
	if a.selectWindow(0); a.winRename != nil {
		t.Error("the name typed went with the switch")
	}
	a.openPalette("%")
	items, ms := a.paletteMatches()
	at := slices.IndexFunc(ms, func(m ui.Match) bool { return items[m.Index].kind == itemWindow && items[m.Index].name == "2: data-3" })
	if a.paletteRun(at, false); a.sess.Active != 2 || a.state.Recent[0] != (config.Recent{Kind: "window", ID: strconv.Itoa(a.win().ID)}) {
		t.Errorf("the palette's 2: data-3: on %d, recent %+v", a.sess.Active, a.state.Recent)
	}
}

// Each session's own window shows as its current one in the list, as
// tmux's (F5.1).
func TestSessionListWindows(t *testing.T) {
	a, report := withSessions()
	report.Active = 1
	feed(t, a, "<Space>s")
	a.sessList.open[a.sess], a.sessList.open[report] = true, true
	var cur []string
	for _, r := range a.sessionsView().Rows {
		if r.Window && r.Current {
			cur = append(cur, r.Name)
		}
	}
	if strings.Join(cur, "|") != "0: data|1: logs" {
		t.Errorf("current windows: %q", cur)
	}
}

// C-p over the session list or the form: the palette on top, the keys its;
// C-s in the form is its own save, not the table's behind it (F5.1).
func TestPaletteOverLists(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>s<C-p>j")
	if a.palette == nil || a.palette.input.Text != "j" || a.sessList.sel != 0 || strings.Contains(a.render().String(), " session ") {
		t.Errorf("over the list: %+v, list title shows %v", a.palette, strings.Contains(a.render().String(), " session "))
	}
	b := sized(160, 45, "nerd")
	b.run("conn.new", 0)
	if feed(t, b, "<C-s>"); b.form == nil || b.form.err != "缺少 name" {
		t.Errorf("C-s in the form: %+v", b.form)
	}
}

// A new window is data, else data-2, data-3: the first name the session
// has none of; a window cannot be renamed to another's, the name typed
// staying (§5, F8.11).
func TestWindowNames(t *testing.T) {
	a := sized(160, 45, "nerd")
	feed(t, a, "<Space>c<Space>c")
	names := func() (out []string) {
		for _, w := range a.sess.Windows {
			out = append(out, w.Name)
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"data", "data-2", "data-3"}) {
		t.Fatalf("three windows: %v", got)
	}
	a.selectWindow(1)
	a.run("window.close", 0)
	feed(t, a, "<Space>c")
	if got := names(); !slices.Equal(got, []string{"data", "data-3", "data-2"}) {
		t.Errorf("the least free number: %v", got)
	}
	a.run("window.rename", 0)
	feed(t, a, "<C-u>data<CR>")
	if a.winRename == nil || a.toast == "" || a.win().Name != "data-2" {
		t.Errorf("another's name: input %v, toast %q, name %q", a.winRename != nil, a.toast, a.win().Name)
	}
	feed(t, a, "<C-u>mine<CR>")
	if a.winRename != nil || a.win().Name != "mine" {
		t.Errorf("a name of its own: %q", a.win().Name)
	}
	a.run("window.rename", 0) // its own name again
	if feed(t, a, "<CR>"); a.winRename != nil || a.win().Name != "mine" {
		t.Errorf("unchanged: %q", a.win().Name)
	}
}

// session.rename types this session's name in the status bar: another
// session's is refused, the input staying; esc drops it (F8.11).
func TestSessionRenameCommand(t *testing.T) {
	a, report := withSessions()
	a.attach(report)
	a.run("session.rename", 0)
	if a.askName == nil || a.mode() != keymap.Insert || a.askName.in.Text != "report" {
		t.Fatalf("not asking: %+v", a.askName)
	}
	feed(t, a, "<C-u>doraemon<CR>")
	if a.askName == nil || a.toast == "" || report.Name != "report" {
		t.Errorf("another's name: asking %v, toast %q, %q", a.askName != nil, a.toast, report.Name)
	}
	a.toast = ""
	if feed(t, a, "<C-u>report<CR>"); a.askName != nil || a.toast != "" || report.Name != "report" {
		t.Errorf("its own name: asking %v, toast %q, %q", a.askName != nil, a.toast, report.Name)
	}
	a.run("session.rename", 0)
	feed(t, a, "<C-u>weekly<CR>")
	if a.askName != nil || report.Name != "weekly" {
		t.Errorf("a name of its own: %q", report.Name)
	}
	a.run("session.rename", 0)
	if feed(t, a, "<C-u>x<Esc>"); a.askName != nil || report.Name != "weekly" {
		t.Errorf("esc: %q", report.Name)
	}
}

// session.close closes the session attached wherever it is run, asking
// first as x on its row does; the one before it, or after, is attached;
// the only one is quitting (F8.12).
func TestSessionCloseCommand(t *testing.T) {
	a, report := withSessions()
	doraemon := a.sess
	a.attach(report)
	a.run("session.close", 0)
	if len(a.sessions) != 1 || a.sess != doraemon || a.sessList != nil {
		t.Fatalf("closed: %d sessions, attached %q", len(a.sessions), a.sess.Name)
	}
	if !quits(a.run("session.close", 0)) {
		t.Error("the only session closing is quitting")
	}

	a, report = withSessions()
	a.attach(report)
	a.sess.Conns[0].tx = 'T' // a transaction would go: asked first
	if a.run("session.close", 0); a.confirm == nil || len(a.sessions) != 2 {
		t.Fatalf("not asked: %d sessions", len(a.sessions))
	}
	feed(t, a, "y")
	if len(a.sessions) != 1 || a.sess.Name != "doraemon" {
		t.Errorf("confirmed: %d sessions", len(a.sessions))
	}
}

// The names asked in the status bar, layout.save, session.rename and
// query.rename, take the left of it as window.rename does: the mode block
// stays at the right, INSERT, the input not over it, on a narrow bar too
// (§7.8, F8.11).
func TestAskNameKeepsModeBlock(t *testing.T) {
	for _, w := range []int{160, 50} {
		for _, cmd := range []string{"layout.save", "session.rename", "query.rename"} {
			a, _ := inQuery(t, "", "")
			a.Update(tea.WindowSizeMsg{Width: w, Height: 20})
			a.run(cmd, 0)
			if a.askName == nil {
				t.Fatalf("%s: not asking", cmd)
			}
			feed(t, a, "x")
			row := statusRow(a)
			if !strings.HasSuffix(row, " INSERT ") || !strings.Contains(row, a.askName.label+a.askName.in.Text) {
				t.Errorf("%s at %d: %q", cmd, w, row)
			}
			if c := a.render().Cursor; c == nil || c.X >= ui.Width(row)-ui.Width(" INSERT ") || c.Y != a.h-1 {
				t.Errorf("%s at %d: the cursor %v is not in the left", cmd, w, c)
			}
			if bg := a.render().Buf.CellAt(1+ui.Width(a.askName.label)+3, a.h-1).Style.Bg; bg != a.theme.Border {
				t.Errorf("%s at %d: past the name the input is %v, not the bar's %v", cmd, w, bg, a.theme.Border)
			}
			// a name longer than the input is its tail: nothing of its start is left
			// beside it, and the mode block is not covered
			feed(t, a, "αβγδεζηθικλμν"+strings.Repeat("z", 200))
			row = statusRow(a)
			if !strings.HasSuffix(row, " INSERT ") || strings.ContainsAny(row, "αβγδεζηθικλμν") {
				t.Errorf("%s at %d, a long name: %q", cmd, w, row)
			}
			if c := a.render().Cursor; c == nil || c.X >= ui.Width(row)-ui.Width(" INSERT ") || c.Y != a.h-1 {
				t.Errorf("%s at %d, a long name: the cursor %v", cmd, w, c)
			}
		}
	}
}

// The toast that says a session is being connected goes when it is, and another
// toast that came meanwhile stays (F17.4).
func TestNewSessionToastGoesWhenConnected(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	spec := a.sess.Conns[0].Spec
	answer := func() {
		a.Update(sessionDialed{name: "1", spec: spec, d: dialed{main: noDB{}, meta: noDB{}, spec: spec}})
	}
	a.dialSession("1", spec)
	if a.toast != "正在连接 doraemon…" {
		t.Fatalf("toast %q", a.toast)
	}
	if answer(); a.toast != "" || a.sess.Name != "1" {
		t.Errorf("connected: toast %q", a.toast)
	}
	a.dialSession("2", spec)
	a.showToast("something else")
	spec2 := spec
	a.Update(sessionDialed{name: "2", spec: spec2, d: dialed{main: noDB{}, meta: noDB{}, spec: spec2}})
	if a.toast != "something else" {
		t.Errorf("another toast: %q", a.toast)
	}
}

func TestWindowCount(t *testing.T) {
	for n, want := range map[int]string{0: "0 个窗口", 1: "1 个窗口", 2: "2 个窗口", 11: "11 个窗口"} {
		if got := windowCount(n); got != want {
			t.Errorf("%d: %q", n, got)
		}
	}
}
