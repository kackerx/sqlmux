package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/keymap"
)

var layoutSpecs = []config.Connection{{Name: "doraemon", Engine: "postgres"}, {Name: "play", Engine: "postgres"}}

// restoredFrom is l restored over doraemon and play, the first session's
// start connection made ready as a dial brings it, the rest waiting.
func restoredFrom(t *testing.T, l *config.Layout) *Restored {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	return readyStart(Restore(layoutSpecs, l))
}

func readyStart(r *Restored) *Restored {
	c := r.Sessions[0].start
	c.State, c.Main, c.Meta = connReady, db.NewWorker(noDB{}), db.NewWorker(noDB{})
	return r
}

// layoutApp is the program over r, as main makes it.
func layoutApp(r *Restored) *App {
	keys, _ := newDefaultKeys()
	a := newApp(config.Default(), keys, r.Sessions[0], &config.State{}, "")
	a.Adopt(r)
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	return a
}

func leafNode(id int) *config.LayoutNode { return &config.LayoutNode{Pane: id} }

// oneWindow is a layout of one session, doraemon's, with window lw.
func oneWindow(lw config.LayoutWindow) *config.Layout {
	return &config.Layout{Version: 1, Sessions: []config.LayoutSession{{Name: "1", Connection: "doraemon", Windows: []config.LayoutWindow{lw}}}}
}

func table(conn, schema, name string) config.LayoutTab {
	return config.LayoutTab{Kind: "table", Connection: conn, Schema: schema, Table: name}
}

// A layout is the sessions as they were: snapshot, file, read back, restore,
// snapshot again, and the two snapshots are the same, the first being what
// the layout said (§14「布局文件」, F6.5). A query waiting for a connection
// is kept as it is going there; a session keeps the connection it started on,
// not the one the focus last named.
func TestLayoutRoundTrip(t *testing.T) {
	order := config.LayoutTab{Kind: "table", Connection: "doraemon", Schema: "public", Table: "t_order", Where: "id > 5", Order: config.LayoutOrder{{Col: "amount", Desc: true}, {Col: "id"}},
		Limit: 500, Page: 2, Cols: []string{"meta", "note"}, OrderCols: []string{"note", "id", "amount"}, ColWidths: map[string]int{"note": 30, "id": 4}, TransposeWidths: map[string]int{"1": 12, "3": 5}, Transpose: true}
	want := &config.Layout{Version: 1, Active: 1, Sessions: []config.LayoutSession{
		{Name: "one", Connection: "doraemon", Active: 1, Windows: []config.LayoutWindow{
			{
				Name: "a", Focus: 3, Zoom: 3, SidebarOpen: true, SidebarWidth: 40, ResultHeight: 0.25,
				Layout: &config.LayoutNode{Split: "h", Ratio: 0.25, A: leafNode(1), B: &config.LayoutNode{Split: "v", Ratio: 0.75, A: leafNode(2), B: leafNode(3)}},
				Panes: []config.LayoutPane{
					{ID: 1, Current: 1, Tabs: []config.LayoutTab{order, {Kind: "query", Connection: "play", Schema: "s_play", Name: "query_1"}}},
					{ID: 2, Tabs: []config.LayoutTab{{Kind: "table", Connection: "doraemon", Schema: "agentable", Table: "planner", Limit: 100, Desc: true}}}, // the default order turned
					{ID: 3},
				},
			},
			{Name: "b", Focus: 1, Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1}}},
		}},
		{Name: "two", Connection: "play", Windows: []config.LayoutWindow{{
			Name: "c", Focus: 1, Layout: leafNode(1),
			Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{{Kind: "query", Connection: "doraemon", Schema: "agentable", Name: "query_2"}}}},
		}}},
	}}
	a := layoutApp(restoredFrom(t, want))
	first := a.layoutOf()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("snapshot of what was restored:\n got %+v\nwant %+v", first, want)
	}
	a.sessions[0].last = a.sessions[0].Conns[1]
	if got := a.layoutOf().Sessions[0].Connection; got != "doraemon" {
		t.Errorf("the start connection is kept, not the current one: %q", got)
	}

	path := filepath.Join(t.TempDir(), "x.toml")
	if err := config.SaveLayout(path, first); err != nil {
		t.Fatal(err)
	}
	back, err := config.LoadLayout(path)
	if err != nil {
		t.Fatal(err)
	}
	if second := layoutApp(restoredFrom(t, back)).layoutOf(); !reflect.DeepEqual(first, second) {
		t.Fatalf("after the file:\n got %+v\nwant %+v", second, first)
	}
}

// A direction beside a sorting is the old format's, one column and desc: what
// is written has desc only for the default order, or the file would read back
// the other way (F12.1).
func TestLayoutDescOnlyForDefaultOrder(t *testing.T) {
	tab := table("doraemon", "public", "t_order")
	tab.Order, tab.Desc = config.LayoutOrder{{Col: "amount"}}, true
	l := oneWindow(config.LayoutWindow{Name: "a", Focus: 1, Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{tab}}}})
	got := layoutApp(restoredFrom(t, l)).layoutOf().Sessions[0].Windows[0].Panes[0].Tabs[0]
	if got.Desc || !reflect.DeepEqual(got.Order, config.LayoutOrder{{Col: "amount"}}) {
		t.Errorf("written with %+v desc %v", got.Order, got.Desc)
	}
}

// The result area is not saved, its height is: from the area as it is, or
// as it was left hidden or closed; a focus or zoom in it is the pane focused
// before it, and none.
func TestWindowLayoutResult(t *testing.T) {
	a := sized(160, 45, "nerd")
	win := a.win()
	if got := windowLayout(win); got.ResultHeight != 0 {
		t.Errorf("never opened: %v", got.ResultHeight)
	}
	a.resultPane(win)
	win.Root.Ratio = 0.75
	win.focus(1)
	win.focus(2)
	win.focus(win.Result.ID)
	win.Zoom = win.Result.ID
	got := windowLayout(win)
	if got.ResultHeight != 0.25 || got.Focus != 2 || got.Zoom != 0 || got.Layout.Split != "h" || len(got.Panes) != 2 {
		t.Errorf("shown: %+v", got)
	}
	win.focus(1)
	a.toggleResult()
	if got := windowLayout(win); got.ResultHeight != 0.25 || got.Layout.Split != "h" || len(got.Panes) != 2 {
		t.Errorf("hidden: %+v", got)
	}
	win.Result = nil
	if got := windowLayout(win); got.ResultHeight != 0.25 {
		t.Errorf("closed: %+v", got)
	}
}

// What cannot be built is left out and said; the rest is built (§14「恢复」).
func TestLayoutRestoreGaps(t *testing.T) {
	query := func(n int, conn string) config.LayoutTab {
		return config.LayoutTab{Kind: "query", N: n, Connection: conn}
	}
	queries := func(s *Session) (ns []string) {
		for _, c := range s.queries() {
			ns = append(ns, c.name)
		}
		slices.Sort(ns)
		return ns
	}

	t.Run("tree and panes", func(t *testing.T) {
		r := restoredFrom(t, oneWindow(config.LayoutWindow{
			Name: "a", Layout: &config.LayoutNode{Split: "h", Ratio: 3, A: leafNode(1), B: &config.LayoutNode{Split: "v", A: leafNode(0), B: leafNode(7)}},
			Panes: []config.LayoutPane{
				{ID: 0, Tabs: []config.LayoutTab{query(8, "")}}, // not a pane
				{ID: 9, Tabs: []config.LayoutTab{query(1, "")}}, // not in the tree: no tab of it is made, nor a number taken
				{ID: 1, Tabs: []config.LayoutTab{query(1, ""), table("doraemon", "public", "t"), table("doraemon", "public", "t"), table("gone", "public", "u"), table("play", "public", "t")}},
				{ID: 1, Tabs: []config.LayoutTab{query(6, "")}}, // a second entry is not read
			},
		}))
		w := r.Sessions[0].Windows[0]
		if len(w.Root.Leaves()) != 2 || w.pane(7) == nil || w.pane(9) != nil || len(w.pane(1).Tabs) != 3 || w.lastID != 7 || w.Root.Ratio != 0.5 {
			t.Errorf("panes %d, tabs %d, last %d, ratio %v", len(w.Root.Leaves()), len(w.pane(1).Tabs), w.lastID, w.Root.Ratio)
		}
		if c := queryOf(w.pane(1)); c == nil || c.name != "query_1" {
			t.Errorf("the pane the tree has gets query_1: %+v", c)
		}
		if tb := w.pane(1).Tabs[2]; tb.Data == nil || tb.Data.conn.Spec.Name != "play" {
			t.Errorf("the same table on another connection stays: %+v", tb)
		}
		if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "gone") {
			t.Errorf("warnings %q", r.Warnings)
		}
	})

	t.Run("a pane twice in the tree", func(t *testing.T) {
		r := restoredFrom(t, oneWindow(config.LayoutWindow{Name: "a", Layout: &config.LayoutNode{Split: "h", A: leafNode(1), B: leafNode(1)}, Panes: []config.LayoutPane{{ID: 1}}}))
		if n := len(r.Sessions[0].Windows[0].Root.Leaves()); n != 1 {
			t.Errorf("%d leaves", n)
		}
	})

	t.Run("no tree", func(t *testing.T) {
		r := restoredFrom(t, oneWindow(config.LayoutWindow{Name: "a", Panes: []config.LayoutPane{{ID: 0}, {ID: 4, Tabs: []config.LayoutTab{query(1, "")}}, {ID: 5, Tabs: []config.LayoutTab{query(2, "")}}}}))
		w := r.Sessions[0].Windows[0]
		if leaves := w.Root.Leaves(); len(leaves) != 1 || leaves[0].ID != 4 || len(leaves[0].Tabs) != 1 || !slices.Equal(queries(r.Sessions[0]), []string{"query_1"}) {
			t.Errorf("the first pane there is: %+v", w.Root.Leaves())
		}
	})

	t.Run("query numbers", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		if err := os.MkdirAll(filepath.Dir(config.QueryPath("query_1")), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.QueryPath("query_1"), []byte("one\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.QueryPath("query_2"), []byte("two\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		win := func(n int) []config.LayoutWindow {
			return []config.LayoutWindow{{Name: "w", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{query(n, "")}}}}}
		}
		r := readyStart(Restore(layoutSpecs, &config.Layout{Version: 1, Sessions: []config.LayoutSession{
			{Name: "1", Connection: "doraemon", Windows: win(1)},
			{Name: "2", Connection: "nowhere", Windows: win(1)}, // the number is taken: the next with no file, which query_2 has (F8.21)
			{Name: "3", Connection: "play"},                     // no window: the default, on the next free number
			{Name: "4", Connection: "play"},                     // and the next one's the one after
		}}))
		text := func(s *Session) string { return strings.Join(s.queries()[0].ed.Lines(), "\n") }
		if text(r.Sessions[0]) != "one" || r.Sessions[1].queries()[0].name != "query_3" || text(r.Sessions[1]) != "" {
			t.Errorf("queries %v %v", r.Sessions[0].queries(), r.Sessions[1].queries())
		}
		if got := queries(r.Sessions[2]); !slices.Equal(got, []string{"query_4"}) || r.Sessions[2].start.Spec.Name != "play" || len(r.Sessions[2].Windows) != 1 {
			t.Errorf("the default window: queries %v", got)
		}
		if r.Sessions[1].start.Spec.Name != "doraemon" || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "nowhere") {
			t.Errorf("a connection that is not there: starts on %s, warnings %q", r.Sessions[1].start.Spec.Name, r.Warnings)
		}
		if got := queries(r.Sessions[3]); !slices.Equal(got, []string{"query_5"}) {
			t.Errorf("a second default window: queries %v", got)
		}
	})

	t.Run("query file unreadable", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		// a file that does not read
		if err := os.MkdirAll(config.QueryPath("query_1"), 0o755); err != nil {
			t.Fatal(err)
		}
		r := Restore(layoutSpecs, &config.Layout{Version: 1, Sessions: []config.LayoutSession{
			{Name: "2", Connection: "doraemon", Windows: []config.LayoutWindow{{Name: "w", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{query(1, "")}}}}}},
		}})
		if len(r.Sessions) != 1 || len(r.Warnings) != 1 || len(r.Sessions[0].Windows) != 1 || len(queries(r.Sessions[0])) != 0 {
			t.Errorf("sessions %d, warnings %q", len(r.Sessions), r.Warnings)
		}
	})

	t.Run("fields a hand left out", func(t *testing.T) {
		r := restoredFrom(t, oneWindow(config.LayoutWindow{Name: "a", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{
			{Kind: "chart"}, query(0, ""),
			{Kind: "table", Table: "a", Limit: -5}, {Kind: "table", Table: "b", Limit: 5_000_000}, {Kind: "table", Table: "c"}, {Kind: "table", Table: "d", Limit: 500},
		}}}}))
		s := r.Sessions[0]
		tabs := s.Windows[0].pane(1).Tabs
		if len(r.Warnings) != 1 || r.Warnings[0] != "不认识的 tab" || len(tabs) != 5 {
			t.Fatalf("warnings %q, %d tabs", r.Warnings, len(tabs))
		}
		if c := tabs[0].Query; c == nil || c.conn != s.start || c.want != nil {
			t.Errorf("a query with no connection is on the start: %+v", c)
		}
		for i, want := range []int{100, maxLimit, 100, 500} {
			if got := tabs[i+1].Data.limit; got != want || tabs[i+1].Data.shown.limit != want {
				t.Errorf("tab %d limit %d, want %d", i+1, got, want)
			}
		}
	})

	t.Run("focus and zoom", func(t *testing.T) {
		two := &config.LayoutNode{Split: "h", A: leafNode(1), B: leafNode(2)}
		for _, c := range []struct {
			open         bool
			focus, zoom  int
			wantF, wantZ int
		}{
			{false, 0, 0, 1, 0}, // the sidebar is folded: nothing to focus there
			{true, 0, 0, 0, 0},  // the sidebar
			{false, 9, 0, 1, 0}, // a pane that is not there
			{false, 2, 1, 2, 0}, // a zoom on another pane than the focused one would hide it
			{false, 2, 2, 2, 2},
		} {
			w := restoredFrom(t, oneWindow(config.LayoutWindow{Name: "a", SidebarOpen: c.open, Focus: c.focus, Zoom: c.zoom, Layout: two,
				Panes: []config.LayoutPane{{ID: 1}, {ID: 2}}})).Sessions[0].Windows[0]
			if w.Focus != c.wantF || w.Zoom != c.wantZ {
				t.Errorf("%+v: focus %d, zoom %d", c, w.Focus, w.Zoom)
			}
		}
	})
}

// A table tab of a layout waits for its connection's catalog: with the table
// there it fetches (a schema it did not name is the connection's home),
// without it the tab is closed and a toast says so; a connection that fails
// leaves its waiting tabs, showing why, and R dials it again. Until then nothing goes
// out for it. A query waiting for another connection keeps where it is
// going (§14「恢复」, F6.5).
func TestLayoutPendingTabs(t *testing.T) {
	l := oneWindow(config.LayoutWindow{Name: "a", Layout: &config.LayoutNode{Split: "h", A: leafNode(1), B: leafNode(2)}, Panes: []config.LayoutPane{
		{ID: 1, Tabs: []config.LayoutTab{table("doraemon", "public", "t_order"), table("doraemon", "public", "dropped"), table("play", "public", "x"), table("", "", "t_user"), table("doraemon", "public", "t_user")}},
		{ID: 2, Tabs: []config.LayoutTab{{Kind: "query", N: 3, Connection: "play", Schema: "s_play"}}},
	}})
	r := restoredFrom(t, l)
	a := layoutApp(r)
	cmds := slices.DeleteFunc(a.connectsNow(), func(c tea.Cmd) bool { return c == nil })
	if len(cmds) != 2 { // the first's catalog, play's connect, once
		t.Errorf("at start: %d commands", len(cmds))
	}
	doraemon, play := r.Sessions[0].Conns[0], r.Sessions[0].Conns[1]
	p := r.Sessions[0].Windows[0].pane(1)
	if len(p.Tabs) != 5 {
		t.Fatalf("tabs %d", len(p.Tabs))
	}
	if c := queryOf(r.Sessions[0].Windows[0].pane(2)); c.want != play || c.wantSchema != "s_play" || c.schema != "" {
		t.Errorf("a query on the start waiting for play: %+v", c)
	}
	for _, i := range []int{0, 2} { // pending: not before the catalog, the connection ready or not
		if d := p.Tabs[i].Data; a.fetch(d, true) != nil || d.seq != 0 || d.conn.busy != 0 {
			t.Errorf("tab %d fetched while pending", i)
		}
	}
	a.Update(catalogMsg{conn: doraemon, schemas: []string{"public"}, current: "public", tables: []db.Table{{Schema: "public", Name: "t_order", Rows: 5}, {Schema: "public", Name: "t_user"}}})
	if len(p.Tabs) != 3 || p.Tabs[0].Data.pending || p.Tabs[0].Data.table.Rows != 5 || p.Tabs[0].Data.seq == 0 || !strings.Contains(a.toast, "dropped") {
		t.Fatalf("after the catalog: %d tabs, %+v, toast %q", len(p.Tabs), p.Tabs[0].Data, a.toast)
	}
	if d := p.Tabs[2].Data; d.pending || d.table.Schema != "public" || d.seq == 0 {
		t.Errorf("home schema: %+v", d.table)
	}
	if !p.Tabs[1].Data.pending {
		t.Error("play's waits")
	}
	if _, ok := a.state.Queries["query_3"]; ok {
		t.Error("a query on its way to play is not put on the start's in the state")
	}
	if c := queryOf(r.Sessions[0].Windows[0].pane(2)); c.schema != "" {
		t.Errorf("nor is the start's schema its: %q", c.schema)
	}
	a.Update(connected{conn: play, err: errors.New("refused")})
	if len(p.Tabs) != 3 || !strings.Contains(a.toast, "play：refused") || play.State != connFailed || !p.Tabs[1].Data.pending {
		t.Errorf("play failed: %d tabs, toast %q", len(p.Tabs), a.toast)
	}
	if cmd := a.fetch(p.Tabs[1].Data, true); cmd == nil || play.State != connConnecting { // R on its error page dials again
		t.Errorf("R on a failed connection's tab: %v, %v", cmd != nil, play.State)
	}
}

// The start connection that cannot be connected is no reason to end: the
// program starts connecting it like any other, its node turns failed, the
// toast says why, and the table tab that waits for it stays, with the error
// in the middle of its content; once it connects R fetches (F12.5).
func TestStartConnectionFails(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	l := oneWindow(config.LayoutWindow{Name: "a", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{table("doraemon", "public", "t_order")}}}})
	r := Restore(layoutSpecs, l)
	a := layoutApp(r)
	doraemon := r.Sessions[0].Conns[0]
	if cmds := a.connectsNow(); doraemon.State != connConnecting || len(cmds) == 0 {
		t.Fatalf("the start connects as the program runs: %v", doraemon.State)
	}
	a.Update(connected{conn: doraemon, err: errors.New("failed to connect: context deadline exceeded")})
	p := r.Sessions[0].Windows[0].pane(1)
	if doraemon.State != connFailed || !strings.Contains(a.toast, "doraemon：failed to connect") || len(p.Tabs) != 1 {
		t.Fatalf("after the failure: %v, toast %q, %d tabs", doraemon.State, a.toast, len(p.Tabs))
	}
	screen := a.render().String()
	for _, want := range []string{"doraemon", "failed to connect: context deadline exceeded"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the error page lacks %q:\n%s", want, screen)
		}
	}
	golden.RequireEqual(t, []byte(screen))
	for _, state := range []string{"failed", "dialing"} { // the DDL of a table needs a Meta: none, and no panic
		a.toast = ""
		if cmd := a.run("grid.ddl.table", 0); cmd == nil || a.toast != "连接还没连上" || a.ddl != nil {
			t.Errorf("%s: DDL asked: toast %q, popup %v", state, a.toast, a.ddl)
		}
		if state == "failed" {
			cmd := a.run("grid.refresh", 0)
			if doraemon.State != connConnecting || cmd == nil {
				t.Errorf("R tries again: %v", doraemon.State)
			}
		}
	}
}

// A tab of a connection that is not up, closed and reopened, waits for its
// catalog as a layout's does, and fetches then (§14「恢复」).
func TestReopenPending(t *testing.T) {
	r := restoredFrom(t, &config.Layout{Version: 1, Sessions: []config.LayoutSession{{Name: "1", Connection: "doraemon", Windows: []config.LayoutWindow{{
		Name: "a", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{table("play", "public", "x"), table("doraemon", "public", "y")}}},
	}}}}})
	a := layoutApp(r)
	play := r.Sessions[0].Conns[1]
	p := r.Sessions[0].Windows[0].pane(1)
	a.closeTab(p, 0)
	a.reopenTab()
	at := func() *dataTab {
		i := slices.IndexFunc(p.Tabs, func(tb Tab) bool { return tb.Data != nil && tb.Data.table.Name == "x" })
		if i < 0 {
			t.Fatalf("no x among %d tabs", len(p.Tabs))
		}
		return p.Tabs[i].Data
	}
	if d := at(); !d.pending || d.seq != 0 {
		t.Fatalf("reopened with play not up: pending %v, seq %d", d.pending, d.seq)
	}
	play.State, play.Main, play.Meta = connReady, db.NewWorker(noDB{}), db.NewWorker(noDB{})
	a.Update(catalogMsg{conn: play, schemas: []string{"public"}, current: "public", tables: []db.Table{{Schema: "public", Name: "x"}}})
	if d := at(); d.pending || d.seq == 0 {
		t.Errorf("play's catalog came: pending %v, seq %d", d.pending, d.seq)
	}
}

// A connection that is not up is not run on, and a tab on one is not the
// current connection: a layout's other sessions connect as the program runs.
func TestUnreadyConnection(t *testing.T) {
	r := restoredFrom(t, &config.Layout{Version: 1, Sessions: []config.LayoutSession{
		{Name: "1", Connection: "doraemon", Windows: []config.LayoutWindow{{Name: "a", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{table("play", "public", "x")}}}}}},
		{Name: "2", Connection: "play"},
	}})
	a := layoutApp(r)
	doraemon, play := r.Sessions[0].Conns[0], r.Sessions[1].start
	a.win().focus(1)
	if got := a.conn(); got != doraemon {
		t.Errorf("a pending tab on play: %s", got.Spec.Name)
	}
	a.attach(r.Sessions[1])
	a.win().focus(0)
	ns, _ := a.treeNodes() // a connection's node would connect it instead
	a.treeGo(slices.IndexFunc(ns, func(n node) bool { return n.kind == nodeWorkspace }))
	q := queryOf(a.win().pane(2))
	for name, run := range map[string]func() tea.Cmd{
		"R on the tree": func() tea.Cmd { return a.refreshTree() },
		"quick SQL":     func() tea.Cmd { a.palette = &palette{conn: play}; return a.runQuick("select 1") },
		"a query's run": func() tea.Cmd { return a.runSQL(q, play, "", "query_1", "select 1", 0, 0) },
		"cancel":        func() tea.Cmd { play.cancel(); return nil },
	} {
		play.State = connConnecting // its dial is on: it is said so
		a.toast = ""
		if run(); name != "cancel" && !strings.Contains(a.toast, "连接还没连上") {
			t.Errorf("%s: toast %q", name, a.toast)
		}
		play.State = connFailed // after a failure it is dialed again, nothing said
		a.toast = ""
		if cmd := run(); name != "cancel" && (cmd == nil || play.State != connConnecting || a.toast != "") {
			t.Errorf("%s after a failure: state %v, toast %q", name, play.State, a.toast)
		}
	}
}

// The working set is written as the program quits, once it is told where:
// not by the tests, which are never told (F6.5).
func TestLayoutSavedOnQuit(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	path := filepath.Join(t.TempDir(), "last.toml")
	if feed(t, a, ":qa<CR>"); !quits(a.quit()) {
		t.Fatal("quit")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("written with nowhere to write")
	}
	a.SaveLayoutOnExit(path)
	a.quit()
	l, err := config.LoadLayout(path)
	if err != nil || len(l.Sessions) != 1 || len(l.Sessions[0].Windows[0].Panes) != 2 {
		t.Fatalf("last: %+v %v", l, err)
	}
}

// layout.save asks the name in the status bar: ↵ writes layouts/<name>.toml,
// esc drops it, a name that cannot be one is said; layout.open lists them,
// last first when there is one (F6.5).
func TestLayoutSaveAndList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	a.run("layout.save", 0)
	if a.askName == nil || a.mode() != keymap.Insert || a.render().Cursor == nil {
		t.Fatal("no prompt")
	}
	for _, bad := range []string{"my/x", "last", ".x"} {
		a.run("layout.save", 0)
		if feed(t, a, bad+"<CR>"); a.askName != nil || a.toast == "" || len(config.LayoutNames()) != 0 {
			t.Errorf("%q: toast %q, layouts %v", bad, a.toast, config.LayoutNames())
		}
		a.toast = ""
	}
	a.run("layout.save", 0)
	feed(t, a, "<Esc>")
	if a.askName != nil || len(config.LayoutNames()) != 0 {
		t.Error("esc")
	}
	a.run("layout.save", 0)
	feed(t, a, "work<CR>")
	if l, err := config.LoadLayout(config.LayoutPath("work")); err != nil || len(l.Sessions) != 1 || !strings.Contains(a.toast, "work") {
		t.Fatalf("saved: %v, toast %q", err, a.toast)
	}
	a.run("layout.save", 0) // the name is the file's, whatever it ends in
	feed(t, a, "x.toml<CR>")
	if _, err := os.Stat(filepath.Join(config.LayoutsDir(), "x.toml.toml")); err != nil {
		t.Errorf("x.toml: %v", err)
	}
	if _, err := os.Stat("x.toml"); err == nil {
		os.Remove("x.toml")
		t.Error("written in the working directory")
	}
	if err := config.SaveLayout(config.LastPath(), a.layoutOf()); err != nil {
		t.Fatal(err)
	}
	a.run("layout.open", 0)
	if items, _ := a.dropItems(); !slices.Equal(items, []string{"last", "work", "x.toml"}) {
		t.Fatalf("layouts: %v", items)
	}
}

// A pick of layout.open is done in the one Update: what is typed in the
// queries is written, the layout read and the sessions replaced, so what the
// new queries read is what was typed, and nothing is left to arrive late.
// Asked first when a transaction would go; an error changes nothing (§14).
func TestPickLayout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := sized(160, 45, "nerd")
	closed := &atomic.Int32{}
	old := a.sess.Conns[0]
	old.Main, old.Meta = db.NewWorker(closeDB{n: closed}), db.NewWorker(closeDB{n: closed})
	old.Spec.DSN = "postgres://u@127.0.0.1:1/db?connect_timeout=1" // the new sessions' connections: refused at once
	next := &config.Layout{Version: 1, Active: 1, Sessions: []config.LayoutSession{
		{Name: "x", Connection: "doraemon", Windows: []config.LayoutWindow{{Name: "w", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{{Kind: "query", N: 1}}}}}}},
		{Name: "y", Connection: "doraemon"},
		{Name: "z", Connection: "gone"},
	}}
	for _, path := range []string{config.LayoutPath("two"), config.LastPath()} {
		if err := config.SaveLayout(path, next); err != nil {
			t.Fatal(err)
		}
	}
	a.win().focus(2)
	feed(t, a, "iabc<Esc>")

	conn := a.sess.Conns[0]
	conn.tx = 'T'
	if cmd := a.pickLayout("two"); cmd != nil || a.confirm == nil || len(a.sessions) != 1 {
		t.Fatalf("a transaction is asked about: %v %+v", cmd, a.confirm)
	}
	conn.tx = 0
	a.confirm = nil

	query := queryOf(a.win().pane(2))
	if err := os.MkdirAll(query.path+"d", 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := query.path
	query.path = oldPath + "d" // a query that cannot be written: nothing is replaced
	a.pickLayout("two")
	if len(a.sessions) != 1 {
		t.Fatal("replaced with a query unwritten")
	}
	query.path = oldPath

	a.toast = ""
	if a.pickLayout("nope"); len(a.sessions) != 1 || !strings.Contains(a.toast, "nope.toml") {
		t.Errorf("a layout that is not there: %d sessions, toast %q", len(a.sessions), a.toast)
	}
	a.toast = ""
	cmd := a.pickLayout("last")
	if cmd == nil || len(a.sessions) != 3 || a.sess.Name != "y" {
		t.Fatalf("replaced: %d sessions, on %q", len(a.sessions), a.sess.Name)
	}
	if got := strings.Join(a.sessions[0].queries()[0].ed.Lines(), "\n"); got != "abc" {
		t.Errorf("the new query reads %q, what was typed", got)
	}
	for _, s := range a.sessions {
		if s.start.State != connConnecting {
			t.Errorf("session %s: its start is %v, to be dialed in the background", s.Name, s.start.State)
		}
	}
	if !strings.Contains(a.toast, "gone") {
		t.Errorf("the layout's warnings are a toast: %q", a.toast)
	}
	for _, c := range cmd().(tea.BatchMsg) { // the old connections are closed, the new ones dialed (refused)
		go c()
	}
	for i := 0; closed.Load() != 2; i++ {
		if i > 200 {
			t.Fatalf("the old session's workers closed: %d of 2", closed.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// a pick is by name: x.toml is the file x.toml.toml of layouts/, not ./x.toml
	t.Chdir(t.TempDir())
	byName := &config.Layout{Version: 1, Sessions: []config.LayoutSession{{Name: "viaName", Connection: "doraemon"}}}
	byPath := &config.Layout{Version: 1, Sessions: []config.LayoutSession{{Name: "viaPath", Connection: "doraemon"}}}
	if err := errors.Join(config.SaveLayout(config.LayoutPath("x.toml"), byName), config.SaveLayout("x.toml", byPath)); err != nil {
		t.Fatal(err)
	}
	a.pickLayout("x.toml")
	if a.sess.Name != "viaName" {
		t.Errorf("x.toml: %q", a.sess.Name)
	}
}

// closeDB counts its closes.
type closeDB struct {
	noDB
	n *atomic.Int32
}

func (d closeDB) Close() error { d.n.Add(1); return nil }

// A session keeps the connection it was started on (§14).
func TestNewSessionStart(t *testing.T) {
	a := sized(160, 45, "nerd")
	conns := []*Conn{readyConn("a"), readyConn("b")}
	s := newSession("x", conns, conns[1], config.QueryName(1))
	a.sessions, a.sess = []*Session{s}, s
	if got := a.layoutOf().Sessions[0].Connection; got != "b" {
		t.Errorf("started on b, saved %q", got)
	}
}

// A ${NAME} in the DSN is dialed as the variable's value, one not set is
// the error, the connection's name first (§13, F7.3).
func TestDialExpandsDSN(t *testing.T) {
	spec := config.Connection{Name: "far", Engine: "postgres", DSN: "${SQLMUX_TEST_FAR_DSN}"}
	if _, err := dial(context.Background(), spec); err == nil || err.Error() != "${SQLMUX_TEST_FAR_DSN}：这个环境变量没有设置" {
		t.Errorf("not set: %v", err)
	}
	t.Setenv("SQLMUX_TEST_FAR_DSN", "postgres://u@127.0.0.1:1/db?connect_timeout=1")
	if _, err := dial(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("set: %v", err)
	}
}

// A tab dropped leaves the one before it or the one that was current
// before showing (T-01).
func TestDropTab(t *testing.T) {
	for _, c := range []struct {
		cur, prev, drop   int
		wantCur, wantPrev int
	}{
		{2, -1, 0, 1, -1}, // before the current one
		{1, 2, 2, 1, -1},  // after it, the previous one gone
		{1, 3, 3, 1, -1},
		{0, 2, 0, 1, -1},  // the current one: the previous, a place to the left
		{1, 0, 1, 0, -1},  // the current one: the previous, on its left
		{3, -1, 3, 2, -1}, // the last one, no previous
		{1, 2, 0, 0, 1},   // before both: both move
	} {
		p := &Pane{Tabs: []Tab{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}, Cur: c.cur, Prev: c.prev}
		dropTab(p, c.drop)
		if len(p.Tabs) != 3 || p.Cur != c.wantCur || p.Prev != c.wantPrev {
			t.Errorf("%+v: cur %d prev %d", c, p.Cur, p.Prev)
		}
	}
}

// A query tab's file is its name, or query_<n> in a layout of before
// the names; a name taken (files differing in case are one on some file
// systems) or no file's (a hand-written ../../x would leave the queries'
// directory) is replaced by the next free one, with a file of its own
// (§11, F8.11).
func TestLayoutQueryNames(t *testing.T) {
	tabs := []config.LayoutTab{{Kind: "query", Name: "Query_3"}, {Kind: "query", N: 3}, {Kind: "query", Name: "../../evil"}, {Kind: "query", Name: "keep"}, {Kind: "query", N: 5}, {Kind: "query", Name: "QUERY_5"}}
	r := restoredFrom(t, oneWindow(config.LayoutWindow{Name: "a", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: tabs}}}))
	var got []string
	for _, tb := range r.Sessions[0].Windows[0].pane(1).Tabs {
		if want := filepath.Join(config.DataDir(), "queries"); filepath.Dir(tb.Query.path) != want {
			t.Errorf("%s is at %s, not under %s", tb.Query.name, tb.Query.path, want)
		}
		got = append(got, tb.Query.name)
	}
	if want := []string{"Query_3", "query_1", "query_2", "keep", "query_5", "query_4"}; !slices.Equal(got, want) {
		t.Errorf("names %v, want %v", got, want)
	}
}

// The query that last ran in a schema of the start connection, which is still
// connecting as the program starts, is on that schema from the first frame:
// a dial that fails does not take it back (F12.5).
func TestRestoreQueryOnStart(t *testing.T) {
	a := sized(160, 45, "nerd")
	start := a.sess.start
	start.State = connIdle
	q := a.sess.queries()[0]
	a.state.Queries = map[string]config.QueryState{q.name: {Conn: start.Spec.Name, Schema: "sales"}}
	if cmd := a.restoreQuery(q); cmd != nil || q.schema != "sales" || q.want != nil || q.conn != start {
		t.Errorf("schema %q, want %v, conn %v", q.schema, q.want, q.conn == start)
	}
}

// Two halves make a split: with none written it is h, with one nothing reads it
// is said once and read as h, and a leaf that has a split written too is still a
// leaf; the panes and the tabs they hold are none of them dropped (F13.12, §14).
func TestLayoutUnknownSplit(t *testing.T) {
	body := func(layout string) string {
		return `version = 1
[[session]]
name = "s"
connection = "doraemon"
[[session.window]]
name = "w"
layout = ` + layout + `
[[session.window.pane]]
id = 1
[[session.window.pane.tab]]
kind = "table"
table = "users"
[[session.window.pane]]
id = 2
[[session.window.pane.tab]]
kind = "table"
table = "orders"
[[session.window.pane]]
id = 3
[[session.window.pane.tab]]
kind = "table"
table = "items"
`
	}
	for _, c := range []struct {
		name, layout string
		panes, warns int
		dir          Dir
	}{
		{"h", `{split = "h", a = {pane = 1}, b = {pane = 2}}`, 2, 0, Horiz},
		{"v", `{split = "v", a = {pane = 1}, b = {pane = 2}}`, 2, 0, Vert},
		{"none", `{a = {pane = 1}, b = {pane = 2}}`, 2, 0, Horiz},
		{"empty", `{split = "", a = {pane = 1}, b = {pane = 2}}`, 2, 0, Horiz},
		{"capital", `{split = "H", a = {pane = 1}, b = {pane = 2}}`, 2, 1, Horiz},
		{"twice", `{split = "x", a = {split = "x", a = {pane = 1}, b = {pane = 3}}, b = {pane = 2}}`, 3, 1, Horiz},
		{"leaf", `{split = "h", a = {split = "H", pane = 1}, b = {pane = 2}}`, 2, 0, Horiz},
		{"only a", `{a = {pane = 2}}`, 1, 0, Horiz},
		{"only b", `{split = "v", b = {pane = 2}}`, 1, 0, Horiz},
		{"deep half", `{split = "v", a = {pane = 1}, b = {a = {pane = 2}}}`, 2, 0, Vert},
		{"spaced", `{split = " h ", a = {pane = 1}, b = {pane = 2}}`, 2, 1, Horiz},
	} {
		p := filepath.Join(t.TempDir(), "a.toml")
		if err := os.WriteFile(p, []byte(body(c.layout)), 0o600); err != nil {
			t.Fatal(err)
		}
		l, err := config.LoadLayout(p)
		if err != nil {
			t.Fatal(err)
		}
		r := restoredFrom(t, l)
		w := r.Sessions[0].Windows[0]
		tabs := 0
		for _, p := range w.Root.Leaves() {
			tabs += len(p.Tabs)
		}
		said := 0
		for _, s := range r.Warnings {
			if strings.Contains(s, "split") {
				said++
			}
		}
		if c.name == "spaced" && !slices.ContainsFunc(r.Warnings, func(s string) bool { return strings.Contains(s, `" h "`) }) {
			t.Errorf("the warning does not show the spaces: %q", r.Warnings)
		}
		if len(w.Root.Leaves()) != c.panes || tabs != c.panes || said != c.warns || c.panes > 1 && w.Root.Split != c.dir {
			t.Errorf("%s: %d panes with %d tabs, %d said (%q), split %v", c.name, len(w.Root.Leaves()), tabs, said, r.Warnings, w.Root.Split)
		}
	}
}

// A width in the file that is none, 0 or less, is not one, nor a record that is no number
// (F17.14, F17.15).
func TestLayoutRestoresOnlyWidths(t *testing.T) {
	tab := table("doraemon", "public", "t_order")
	tab.ColWidths = map[string]int{"id": 0, "note": -3, "amount": 7}
	tab.TransposeWidths = map[string]int{"0": 5, "x": 6, "2": -2, "3": 8} // a number from 1, and a width
	r := restoredFrom(t, oneWindow(config.LayoutWindow{Name: "a", Layout: leafNode(1), Panes: []config.LayoutPane{{ID: 1, Tabs: []config.LayoutTab{tab}}}}))
	d := r.Sessions[0].Windows[0].pane(1).Tabs[0].Data
	if d == nil || !maps.Equal(d.colWidth, map[string]int{"amount": 7}) || !maps.Equal(d.transposeWidths, map[int]int{2: 8}) {
		t.Errorf("widths %v, records %v", d.colWidth, d.transposeWidths)
	}
}

// The table of the command line opens in the data pane once the catalog is
// in, with its WHERE, in the connection's schema or the schema it names, and
// leaves the layout as it was when there is none (F19.1).
func TestOpenAtStart(t *testing.T) {
	catalog := func(a *App) tea.Msg {
		c := a.sess.start
		return catalogMsg{conn: c, schemas: c.Schemas, current: "public", tables: c.Tables}
	}
	data := func(a *App) *Pane { return a.win().Root.Leaves()[0] }
	for _, tc := range []struct {
		name, table, where string
		want               string // the data pane's tab, "" for none
		schema             string
		toast              string
	}{
		{"default schema", "t_order", "id < 11", "t_order", "public", ""},
		{"schema.table", "agentable.planner", "", "planner", "agentable", ""},
		{"not in the default schema", "planner", "", "", "", "doraemon：没有表 planner"},
		{"exact name", "T_ORDER", "", "", "", "doraemon：没有表 T_ORDER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := sized(160, 45, "nerd")
			if err := a.OpenAtStart(tc.table, tc.where); err != nil {
				t.Fatal(err)
			}
			if len(data(a).Tabs) != 0 {
				t.Fatal("opened before the catalog")
			}
			a.win().focus(2) // typed on the way: it still opens in the data pane
			a.Update(catalog(a))
			p := data(a)
			if tc.want == "" {
				if len(p.Tabs) != 0 || a.toast != tc.toast || a.want != nil {
					t.Errorf("tabs %d, toast %q", len(p.Tabs), a.toast)
				}
				return
			}
			d := p.tab().Data
			if len(p.Tabs) != 1 || d == nil || p.tab().Name != a.sess.start.tabName(d.table) || d.table.Name != tc.want || d.table.Schema != tc.schema || d.applied != tc.where || d.where.Text != tc.where || d.seq == 0 || d.pending {
				t.Fatalf("tabs %d, %+v", len(p.Tabs), d)
			}
			if a.focused() != p || a.want != nil {
				t.Errorf("focus %v, want %v", a.focused(), a.want)
			}
			if h := a.tableState(d).History; tc.where == "" && len(h) != 0 || tc.where != "" && (len(h) != 1 || h[0].Where != tc.where) {
				t.Errorf("the WHERE history: %+v, after --where %q", h, tc.where)
			}
			a.Update(catalog(a)) // a refresh does not open it again
			if len(p.Tabs) != 1 {
				t.Errorf("tabs after a refresh: %d", len(p.Tabs))
			}
		})
	}

	t.Run("the catalog fails", func(t *testing.T) {
		a := sized(160, 45, "nerd")
		_ = a.OpenAtStart("t_order", "")
		a.Update(catalogMsg{conn: a.sess.start, err: errors.New("boom")})
		a.Update(catalog(a))
		if len(data(a).Tabs) != 0 {
			t.Error("opened by a later catalog")
		}
	})

	t.Run("redis", func(t *testing.T) {
		c := newConn(config.Connection{Name: "cache", Engine: "redis", DSN: "redis://localhost"})
		c.State, c.Main, c.Meta = connReady, db.NewWorker(noDB{}), db.NewWorker(noDB{})
		a := sized(160, 45, "nerd")
		a.sess = newSession("cache", []*Conn{c}, c, config.QueryName(1))
		if err := a.OpenAtStart("hash:user", "x = 1"); err == nil {
			t.Error("--where on a Redis connection is no usage")
		}
		if err := a.OpenAtStart("hash:user", ""); err != nil {
			t.Fatal(err)
		}
		_ = a.resolvePending(c)
		d := a.sess.Windows[0].Root.Leaves()[0].tab().Data
		if d == nil || d.table.Name != "hash:user" || d.table.Kind != "k" {
			t.Errorf("key tab: %+v", d)
		}
	})

	t.Run("no connection", func(t *testing.T) {
		a := emptyApp(t)
		if err := a.OpenAtStart("t", ""); err == nil {
			t.Error("a table of no connection")
		}
	})
}
