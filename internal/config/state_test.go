package config

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
)

// state.json round-trips, is 0600, and an older snapshot never overwrites
// a newer one (§14); a table picked before its id named the connection is
// dropped (F5.10).
func TestState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if s, err := LoadState(); err != nil || len(s.Tables) != 0 {
		t.Fatalf("no file: %+v %v", s, err)
	}
	at := time.Date(2026, 9, 24, 14, 5, 0, 0, time.UTC)
	want := &State{
		Recent:  []Recent{{Kind: "table", ID: "doraemon/public.t_order"}, {Kind: "table", ID: "public.t_order"}, {Kind: "command", ID: "quit"}},
		SQL:     map[string][]string{"doraemon": {"select 2", "select 1"}},
		Queries: map[string]QueryState{"query_1": {Conn: "doraemon", Schema: "agentable"}, "query_2": {Conn: "play", DB: "alpha"}}, // a query's database when not the DSN's (F15.1)
		Tables: map[string]*TableState{"doraemon/public.t_order": {
			History:   []Query{{Where: "id > 5", At: at}},
			Favorites: []Query{{Where: "status = 'done'", Order: []db.OrderCol{{Col: "amount", Desc: true}}, Limit: 500, At: at}},
		}},
	}
	older := Snapshot(&State{})
	if err := Snapshot(want)(); err != nil {
		t.Fatal(err)
	}
	if err := older(); err != nil { // an older snapshot, written late
		t.Fatal(err)
	}
	got, err := LoadState()
	want.Recent = slices.Delete(want.Recent, 1, 2)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v %v", got, err)
	}
	info, _ := os.Stat(filepath.Join(StateDir(), "state.json"))
	if info.Mode().Perm() != 0o600 && runtime.GOOS != "windows" { // windows has no group/other bits to strip
		t.Errorf("mode %v", info.Mode().Perm())
	}
	path := filepath.Join(StateDir(), "state.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadState(); err == nil || len(s.Tables) != 0 {
		t.Error("a broken file is an error, and an empty state")
	}
	if b, err := os.ReadFile(path + ".broken"); err != nil || string(b) != "{" {
		t.Errorf("the broken file is kept aside: %q %v", b, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("and out of the next save's way")
	}
}

// A process killed after the temp file is written and before it is renamed
// leaves the state.json there was whole (§14, F6.3).
func TestStateKilledMidWrite(t *testing.T) {
	if os.Getenv("SQLMUX_KILL_HELPER") != "" { // the child: write, and stop at the rename
		beforeRename = func() {
			os.Stdout.WriteString("ready\n")
			select {}
		}
		_ = Snapshot(&State{Recent: []Recent{{Kind: "command", ID: "new"}}})()
		return
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	old := &State{Recent: []Recent{{Kind: "command", ID: "old"}}}
	if err := Snapshot(old)(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStateKilledMidWrite$")
	cmd.Env = append(os.Environ(), "SQLMUX_KILL_HELPER=1")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		t.Fatalf("the child said %q", line)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait() // a killed child reports it
	got, err := LoadState()
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatalf("after the kill: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(StateDir(), "state.json.broken")); err == nil {
		t.Error("the file was moved aside")
	}
}

// A layout is written and read back whole; a hand-written one with inline
// tables reads the same; another version, an empty file and a bad name are
// refused; layouts are listed by name (§14「布局文件」, F6.5).
func TestLayoutFile(t *testing.T) {
	cfg, st := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", st)
	want := &Layout{Version: 1, Active: 1, Sessions: []LayoutSession{
		{Name: "1", Connection: "doraemon", Active: 0, Windows: []LayoutWindow{{
			Name: "data", Focus: 2, Zoom: 0, SidebarOpen: true, SidebarWidth: 40, ResultHeight: 0.5,
			Layout: &LayoutNode{Split: "h", Ratio: 0.55, A: &LayoutNode{Pane: 1}, B: &LayoutNode{Split: "v", Ratio: 0.5, A: &LayoutNode{Pane: 2}, B: &LayoutNode{Pane: 3}}},
			Panes: []LayoutPane{
				{ID: 1, Current: 1, Tabs: []LayoutTab{
					{Kind: "table", Connection: "doraemon", Schema: "public", Table: "t_order", Where: "id > 5", Order: LayoutOrder{{Col: "amount", Desc: true}}, Limit: 500, Page: 2, Cols: []string{"meta", "note"}, Transpose: true},
					{Kind: "query", Name: "query_1", Connection: "doraemon", Schema: "agentable"},
					{Kind: "table", Connection: "doraemon", DB: "alpha", Schema: "public", Table: "t_x"}, // not the DSN's database (F15.1)
				}},
				{ID: 2}, {ID: 3},
			},
		}}},
		{Name: "2", Connection: "play"},
	}}
	if err := SaveLayout(LastPath(), want); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(LastPath()); info.Mode().Perm() != 0o600 && runtime.GOOS != "windows" { // windows has no group/other bits to strip
		t.Errorf("mode %v", info.Mode().Perm())
	}
	got, err := LoadLayout(LastPath())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	hand := `version = 1
[[session]]
name = "s"
connection = "doraemon"
[[session.window]]
name = "w"
layout = { split = "h", ratio = 0.5, a = { pane = 1 }, b = { pane = 2 } }
[[session.window.pane]]
id = 1
[[session.window.pane]]
id = 2
`
	if err := os.MkdirAll(LayoutsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(LayoutsDir(), "mine.toml"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(LayoutsDir(), "a.toml"), []byte("version = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(LayoutsDir(), "empty.toml"), []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := LoadLayout(LayoutPath("mine"))
	if err != nil || l.Sessions[0].Windows[0].Layout.B.Pane != 2 || l.Sessions[0].Windows[0].Layout.Split != "h" {
		t.Fatalf("by name, inline tables: %+v %v", l, err)
	}
	if _, err := LoadLayout(LayoutPath("a")); err == nil || !strings.Contains(err.Error(), "version = 2") {
		t.Errorf("version: %v", err)
	}
	if _, err := LoadLayout(LayoutPath("empty")); err == nil || !strings.Contains(err.Error(), "[[session]]") {
		t.Errorf("no session: %v", err)
	}
	if _, err := LoadLayout(LayoutPath("nope")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	if names := LayoutNames(); !slices.Equal(names, []string{"a", "empty", "mine"}) {
		t.Errorf("names %v", names)
	}
	for _, bad := range []string{"a/b", ".x", `a\b`, "last"} {
		if CheckLayoutName(bad) == nil {
			t.Errorf("%q is a name", bad)
		}
	}
	if CheckLayoutName("x.toml") != nil || LayoutPath("x.toml") != filepath.Join(LayoutsDir(), "x.toml.toml") {
		t.Error("a name is a name, whatever it ends in")
	}
	for arg, want := range map[string]string{"work": LayoutPath("work"), "a/b.toml": "a/b.toml", "x.toml": "x.toml", "./w": "./w", "last": LastPath()} {
		if got := LayoutArg(arg); got != want {
			t.Errorf("--layout %q is %q", arg, got)
		}
	}
	if err := os.WriteFile(LastPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := BrokenLast(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LastPath() + ".broken"); err != nil {
		t.Error("the broken file is kept aside")
	}
	if BrokenLast() != nil {
		t.Error("none to move is no error")
	}
}

// A connection renamed carries its picks, table histories, quick SQL and the
// queries that run on it with it; another's stay (F8.16).
func TestStateRenameConnection(t *testing.T) {
	s := &State{
		Recent:  []Recent{{"table", "play/public.t"}, {"table", "playground/public.t"}, {"command", "play/x"}, {"table", "play/alpha/public.t"}},
		Tables:  map[string]*TableState{"play/public.t": {}, "playground/public.t": {}, "play/alpha/public.t": {}},
		SQL:     map[string][]string{"play": {"select 1"}, "other": {"select 2"}},
		Queries: map[string]QueryState{"query_1": {Conn: "play"}, "query_2": {Conn: "other"}},
	}
	s.RenameConnection("play", "dev")
	if s.Recent[0].ID != "dev/public.t" || s.Recent[1].ID != "playground/public.t" || s.Recent[2].ID != "play/x" || s.Recent[3].ID != "dev/alpha/public.t" { // a database's table with its connection (F15.1)
		t.Errorf("recent %+v", s.Recent)
	}
	if s.Tables["dev/alpha/public.t"] == nil || s.Tables["play/alpha/public.t"] != nil { // a database's table with its connection (F15.1)
		t.Errorf("tables of a database %v", s.Tables)
	}
	if _, ok := s.Tables["dev/public.t"]; !ok || s.Tables["play/public.t"] != nil || s.Tables["playground/public.t"] == nil {
		t.Errorf("tables %v", s.Tables)
	}
	if s.SQL["dev"][0] != "select 1" || s.SQL["play"] != nil || s.SQL["other"][0] != "select 2" {
		t.Errorf("sql %v", s.SQL)
	}
	if s.Queries["query_1"].Conn != "dev" || s.Queries["query_2"].Conn != "other" {
		t.Errorf("queries %v", s.Queries)
	}
}

// A name that was a connection's, and has data left from it, gives way to
// the connection renamed to it: its tables and quick SQL are written over, a
// pick in both is kept once (F8.16).
func TestStateRenameOverOldData(t *testing.T) {
	s := &State{
		Recent: []Recent{{"table", "dev/public.t"}, {"table", "play/public.t"}, {"table", "dev/public.u"}},
		Tables: map[string]*TableState{"dev/public.t": {History: []Query{{Where: "dev's"}}}, "play/public.t": {History: []Query{{Where: "play's"}}}},
		SQL:    map[string][]string{"dev": {"dev's sql"}, "play": {"play's sql"}},
	}
	s.RenameConnection("play", "dev")
	if want := []Recent{{"table", "dev/public.t"}, {"table", "dev/public.u"}}; !slices.Equal(s.Recent, want) {
		t.Errorf("recent %v, want %v", s.Recent, want)
	}
	if h := s.Tables["dev/public.t"].History; len(h) != 1 || h[0].Where != "play's" {
		t.Errorf("tables %v", h)
	}
	if s.SQL["dev"][0] != "play's sql" || len(s.SQL) != 1 {
		t.Errorf("sql %v", s.SQL)
	}
}
