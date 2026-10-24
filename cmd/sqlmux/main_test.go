package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
	"unicode"

	"github.com/kackerx/sqlmux/internal/app"
	"github.com/kackerx/sqlmux/internal/config"
)

// writeConfig makes a config dir of the test's own with file name in it.
func writeConfig(t *testing.T, name, toml string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", t.TempDir()) // open() reads the queries there
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", name), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --check reads keymaps.toml; keys left in config.toml fail it as they
// fail the start (F5.9).
func TestKeysCheck(t *testing.T) {
	writeConfig(t, "keymaps.toml", "")
	var out, errb bytes.Buffer
	if code := keysCmd([]string{"--check"}, &out, &errb); code != 0 || errb.Len() > 0 {
		t.Fatalf("clean config: exit %d, stderr %q", code, errb.String())
	}

	writeConfig(t, "keymaps.toml", "[keys.normal]\nt = \"tab.next\"\ntc = \"tab.prev\"\n")
	if code := keysCmd([]string{"--check"}, &out, &errb); code != 0 || errb.Len() > 0 {
		t.Fatalf("a key and a sequence it starts: exit %d, stderr %q", code, errb.String())
	}

	writeConfig(t, "keymaps.toml", "[keys.normal]\n\"<C-x>\" = \"a\"\n\"<c-x>\" = \"b\"\n")
	errb.Reset()
	if code := keysCmd([]string{"--check"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "[keys.normal] <c-x>") {
		t.Fatalf("duplicate: exit %d, stderr %q", code, errb.String())
	}

	writeConfig(t, "config.toml", "[keys.normal]\nx = \"\"\n")
	errb.Reset()
	if code := keysCmd([]string{"--check"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "config.toml: [keys] / [map] belong in keymaps.toml") {
		t.Fatalf("keys in config.toml: exit %d, stderr %q", code, errb.String())
	}
}

// `sqlmux keys` says its headings in the language of config.toml, English
// when it does not say (§14, F7.6).
func TestKeysFollowLanguage(t *testing.T) {
	for toml, want := range map[string]string{"language = \"zh\"\n": "| 作用域 | 键 | 操作 |", "language = \"en\"\n": "| Scope | Key | Action |", "": "| Scope | Key | Action |"} {
		writeConfig(t, "config.toml", toml)
		var out, errb bytes.Buffer
		if code := keysCmd(nil, &out, &errb); code != 0 || !strings.Contains(out.String(), want) {
			t.Errorf("config.toml %q: exit %d, output %.60q", toml, code, out.String())
		}
	}
}

// With no language written the reference TOML, its group comments and the
// action titles, is English all through.
func TestKeysTOMLDefaultEnglish(t *testing.T) {
	writeConfig(t, "config.toml", "")
	var out, errb bytes.Buffer
	if code := keysCmd([]string{"--format", "toml"}, &out, &errb); code != 0 || strings.ContainsFunc(out.String(), func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
		t.Errorf("exit %d, output %.200q", code, out.String())
	}
}

// The reference's comments are for the user: they name no feature or section of
// the design, in either language (F17.4).
func TestKeysTOMLHasNoInternalRefs(t *testing.T) {
	refs := regexp.MustCompile(`[（(]\s*[FM]\d|§|[（(]\s*S-\d`)
	for _, lang := range []string{"en", "zh"} {
		writeConfig(t, "config.toml", "language = \""+lang+"\"\n")
		for _, format := range []string{"toml", "md"} {
			var out, errb bytes.Buffer
			if code := keysCmd([]string{"--format", format}, &out, &errb); code != 0 {
				t.Fatalf("%s %s: exit %d", lang, format, code)
			}
			if m := refs.FindString(out.String()); m != "" {
				i := strings.Index(out.String(), m)
				t.Errorf("%s %s: %q in %.80q", lang, format, m, out.String()[max(i-40, 0):])
			}
		}
	}
}

func TestKeysFormats(t *testing.T) {
	writeConfig(t, "keymaps.toml", "[keys.grid]\nx = \"\"\n")
	for format, want := range map[string]string{"md": "| global | `C-p` | palette.open |", "toml": "[keys.grid]"} {
		var out, errb bytes.Buffer
		if code := keysCmd([]string{"--format", format}, &out, &errb); code != 0 || !strings.Contains(out.String(), want) {
			t.Errorf("--format %s: exit %d, output lacks %q", format, code, want)
		}
	}
}

// The toml export is the full reference (§6.7): loaded back as keymaps.toml
// it changes nothing, and every titled action is in it, on its binding
// lines, defaults commented, or, bound nowhere, on exactly one commented
// "" line.
func TestKeysTOMLReference(t *testing.T) {
	keys := func(args ...string) string {
		var out, errb bytes.Buffer
		if code := keysCmd(args, &out, &errb); code != 0 || errb.Len() > 0 {
			t.Fatalf("keys %v: exit %d, stderr %q", args, code, errb.String())
		}
		return out.String()
	}
	writeConfig(t, "keymaps.toml", "")
	md, toml := keys(), keys("--format", "toml")
	writeConfig(t, "keymaps.toml", toml)
	if got := keys(); got != md {
		t.Errorf("loaded back, the keymap changed:\n%s", got)
	}
	lines := strings.Split(toml, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "[keys.") && !strings.HasPrefix(lines[i-1], "# ") {
			t.Errorf("%s: no note above it on when it applies (§6.7)", l)
		}
	}
	for id, title := range app.Titles() {
		bound := len(regexp.MustCompile(`(?m)^(# )?"[^"].*" = "`+regexp.QuoteMeta(id)+`( .*)?"  # `+regexp.QuoteMeta(title)+`$`).FindAllString(toml, -1))
		commented := strings.Count(toml, `# "" = "`+id+`"  # `+title+"\n")
		if !(bound > 0 && commented == 0 || bound == 0 && commented == 1) {
			t.Errorf("%s: %d binding lines, %d commented lines", id, bound, commented)
		}
	}
}

// Startup errors name the file to fix (§14).
func TestOpenWithoutConnections(t *testing.T) {
	writeConfig(t, "config.toml", "")
	_, err := open("")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sqlmux", "connections.toml")) {
		t.Fatalf("no connections.toml: %v", err)
	}
}

const oneSession = "version = 1\n[[session]]\nname = \"s\"\nconnection = \"c\"\n"

// The layout the program starts from: --layout's, which has to be there; a
// connection named starts the default one; else last.toml, kept aside when
// it does not read (§14「启动」).
func TestStartLayout(t *testing.T) {
	writeConfig(t, "config.toml", "")
	mine := filepath.Join(t.TempDir(), "mine.toml")
	if err := os.WriteFile(mine, []byte(oneSession), 0o600); err != nil {
		t.Fatal(err)
	}
	var warning string

	if l, err := startLayout("", mine, &warning); err != nil || l == nil || l.Sessions[0].Name != "s" {
		t.Errorf("--layout path: %v %v", l, err)
	}
	if _, err := startLayout("", "nope", &warning); !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "nope.toml") {
		t.Errorf("--layout that is not there: %v", err)
	}
	if l, err := startLayout("play", "", &warning); l != nil || err != nil {
		t.Errorf("a connection name starts the default: %v %v", l, err)
	}
	if l, err := startLayout("", "", &warning); l != nil || err != nil || warning != "" {
		t.Errorf("no last.toml: %v %v %q", l, err, warning)
	}

	if err := os.MkdirAll(config.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LastPath(), []byte(oneSession), 0o600); err != nil {
		t.Fatal(err)
	}
	if l, err := startLayout("", "", &warning); l == nil || err != nil {
		t.Errorf("last.toml: %v %v", l, err)
	}
	if l, err := startLayout("play", "", &warning); l != nil || err != nil {
		t.Errorf("a connection name does not restore last: %v %v", l, err)
	}
	if err := os.WriteFile(config.LastPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	warning = "config.toml: unknown keys x" // said before: the last.toml's joins it (F13.5)
	if l, err := startLayout("", "", &warning); l != nil || err != nil || !strings.Contains(warning, "last.toml") || !strings.Contains(warning, "unknown keys x") {
		t.Errorf("a last.toml that does not read: %v %v %q", l, err, warning)
	}
	if _, err := os.Stat(config.LastPath() + ".broken"); err != nil {
		t.Error("it is kept aside")
	} else if _, err := os.Stat(config.LastPath()); err == nil {
		t.Error("and not left to be written over by the next exit")
	}
}

// What is wrong with the arguments is said before anything is read.
func TestRunArguments(t *testing.T) {
	if err := run([]string{"--layout", "x", "play"}); err == nil || !strings.Contains(err.Error(), "--layout") {
		t.Errorf("a connection and --layout: %v", err)
	}
	if err := run([]string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("two connections: %v", err)
	}
}

// With no connections.toml, or none in it, the program starts with an empty
// session, also when last.toml is there; a connection asked for, or a
// --layout, is still an error naming the file (§14「启动时找不到连接」).
func TestStartSessionWithoutConnections(t *testing.T) {
	for _, file := range []string{"", "# nothing yet\n"} {
		writeConfig(t, "config.toml", "")
		if file != "" {
			writeConfig(t, "connections.toml", file)
		}
		path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sqlmux", "connections.toml")
		sess, restored, err := startSession("", "", nil)
		if err != nil || sess == nil || restored != nil || len(sess.Conns) != 0 {
			t.Fatalf("%q: %v %v %v", file, sess, restored, err)
		}
		l := &config.Layout{Version: 1, Sessions: []config.LayoutSession{{Name: "s", Connection: "c"}}}
		if sess, _, err := startSession("", "", l); err != nil || sess == nil || len(sess.Conns) != 0 {
			t.Errorf("%q with a last.toml: %v %v", file, sess, err)
		}
		if _, _, err := startSession("play", "", nil); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q, a connection asked for: %v", file, err)
		}
		if _, _, err := startSession("", "mine", l); err == nil {
			t.Errorf("%q, a --layout: no error", file)
		}
	}
}

// --version says what the build put in; without that the module's version Go
// recorded, which a `go install ...@v0.2.1` has; dev with neither (F11.1).
func TestVersionLine(t *testing.T) {
	defer func(v, c, d string) { version, commit, date = v, c, d }(version, commit, date)
	mod := func(v string, vcs ...string) *debug.BuildInfo {
		info := &debug.BuildInfo{Main: debug.Module{Version: v}}
		for i := 0; i+1 < len(vcs); i += 2 {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: vcs[i], Value: vcs[i+1]})
		}
		return info
	}
	rev, at := "3f2a1b9c0d4e5f6a7b8c9d0e1f2a3b4c5d6e7f80", "2026-10-11T00:06:40Z"
	for _, c := range []struct {
		info *debug.BuildInfo
		inj  string
		want string
	}{
		{nil, "dev", "sqlmux dev"}, // nothing known is not said as none, unknown
		{mod("(devel)"), "dev", "sqlmux dev"},
		{mod(""), "dev", "sqlmux dev"},
		{mod("v0.2.1"), "dev", "sqlmux v0.2.1"},    // go install
		{mod("v0.2.1"), "v0.3.0", "sqlmux v0.3.0"}, // ldflags win
		{mod("v0.5.0-0.2026-x-3f2a1b9c0d4e+dirty", "vcs.revision", rev, "vcs.time", at), "dev", "sqlmux v0.5.0-0.2026-x-3f2a1b9c0d4e+dirty (3f2a1b9, " + at + ")"},
		{mod("(devel)", "vcs.revision", rev), "dev", "sqlmux dev (3f2a1b9)"},
		{mod("(devel)", "vcs.time", at), "dev", "sqlmux dev (" + at + ")"},
	} {
		version, commit, date = c.inj, "none", "unknown"
		if got := versionLine(c.info); got != c.want {
			t.Errorf("%v with %q: %q, want %q", c.info, c.inj, got, c.want)
		}
	}
	version, commit, date = "v1.2.3", "abc1234", "2026-10-11T00:06:40Z"
	if got := versionLine(nil); got != "sqlmux v1.2.3 (abc1234, 2026-10-11T00:06:40Z)" {
		t.Errorf("injected: %q", got)
	}
	if got := versionLine(mod("(devel)", "vcs.revision", rev, "vcs.time", "other")); got != "sqlmux v1.2.3 (abc1234, 2026-10-11T00:06:40Z)" { // the build's own win
		t.Errorf("injected over vcs: %q", got)
	}
	if err := run([]string{"--version"}); err != nil {
		t.Errorf("--version: %v", err)
	}
}

// What the start says is every note joined: a misspelt key, and the queries
// not moved, none replacing the other (F13.5).
func TestStartNotes(t *testing.T) {
	writeConfig(t, "config.toml", "confirm_dngerous = false\n")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := startNotes(cfg); !strings.Contains(got, "confirm_dngerous") {
		t.Errorf("a misspelt key: %q", got)
	}
	data := os.Getenv("XDG_DATA_HOME")
	if err := os.MkdirAll(filepath.Join(data, "sqlmux", "consoles"), 0o755); err != nil {
		t.Fatal(err)
	}
	blockRename(t, filepath.Join(data, "sqlmux", "consoles")) // consoles cannot move to queries
	if got := startNotes(cfg); !strings.Contains(got, "confirm_dngerous") || !strings.Contains(got, "consoles") {
		t.Errorf("a key and a move that failed: %q", got)
	}
}

// The start cuts history.jsonl to [query] history_max, the newest kept (F14.2).
func TestStartTrimsHistory(t *testing.T) {
	writeConfig(t, "config.toml", "[query]\nhistory_max = 2\n")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"select 1", "select 2", "select 3"} {
		if err := config.AppendHistory(config.History{Conn: "c", SQL: sql}); err != nil {
			t.Fatal(err)
		}
	}
	if got := startNotes(cfg); got != "" {
		t.Fatalf("notes %q", got)
	}
	if hs, _ := config.LoadHistory(); len(hs) != 2 || hs[0].SQL != "select 2" {
		t.Fatalf("history %+v", hs)
	}
}

// The keys the ORDER list has are its own actions, in the reference as in the
// keymap: none of COLS's (F12.9).
func TestOrderKeysReference(t *testing.T) {
	writeConfig(t, "keymaps.toml", "")
	var out, errb bytes.Buffer
	if code := keysCmd([]string{"--format", "toml"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	_, after, ok := strings.Cut(out.String(), "[keys.order]")
	if !ok {
		t.Fatal("no [keys.order]")
	}
	section, _, _ := strings.Cut(after, "\n\n")
	for _, l := range strings.Split(section, "\n") {
		if strings.Contains(l, " = ") && !strings.Contains(l, `"order.`) || strings.Contains(l, "COLS") {
			t.Errorf("a line of [keys.order]: %q", l)
		}
	}
}

// A file or a URL is a connection of its own, kept nowhere; a name is a
// connection's. DATABASE_URL is taken with no name, no layout and no
// connection configured, the name winning over it (F14.1).
func TestTempConnection(t *testing.T) {
	configured := "[[connection]]\nname = \"prod\"\nengine = \"postgres\"\ndsn = \"postgres://h/d\"\n\n[[connection]]\nname = \"prod.eu\"\nengine = \"postgres\"\ndsn = \"postgres://h/e\"\n"
	for _, tc := range []struct {
		conns, name, layout, env string
		want                     string // the temporary connection's name; "" for none
	}{
		{configured, "x.db", "", "", "x"},
		{configured, "./data/x.sqlite", "", "postgres://u@h/other", "x"}, // the argument wins
		{configured, "postgres://u@h/shop", "", "", "shop@h"},
		{configured, "prod", "", "mysql://u@h/db", ""},      // a connection's name
		{configured, "prod.eu", "", "mysql://u@h/db", ""},   // with a dot in it
		{configured, "", "", "mysql://u@h/db", ""},          // a user with connections has last.toml back
		{"", "", "", "mysql://u@h/db", "db@h"},              // none configured
		{configured, "redis://u@h:6380/2", "", "", "db2@h"}, // F17.3
		{configured, "redis://h", "", "", "db0@h"},          // no db: db0
		{configured, "rediss://h/3", "", "", "db3@h"},
		{configured, "redis://h:6379?db=3", "", "", "db3@h"},   // the query's db is the one go-redis connects to
		{configured, "redis://h:6379/1?db=3", "", "", "db3@h"}, // and over the path's
		{configured, "redis://h/02", "", "", "db2@h"},          // as a number, as the tree names it
		{"", "", "", "", ""},
		{"", "", "lay", "mysql://u@h/db", ""}, // a layout asked for is the program's
	} {
		writeConfig(t, "connections.toml", tc.conns)
		c, err := tempConnection(tc.name, tc.layout, tc.env)
		got := ""
		if c != nil {
			got = c.Name
		}
		if err != nil || got != tc.want {
			t.Errorf("%+v: %q %v", tc, got, err)
		}
	}
	if _, err := tempConnection("oracle://h/d", "", ""); err == nil {
		t.Error("no such engine")
	}
	if c, err := tempConnection("rediss://:pw@h:6380/3", "", ""); err != nil || c.Engine != "redis" || c.DSN != "rediss://:pw@h:6380/3" || !c.Temp {
		t.Errorf("a Redis URL: %+v %v", c, err)
	}
	// a connections.toml that does not read is its error, not a reason to take a name for a file
	writeConfig(t, "connections.toml", configured+"\n[[connection]]\nname = \"prod\"\nengine = \"postgres\"\ndsn = \"postgres://h/d\"\n")
	if _, err := tempConnection("prod.eu", "", ""); err == nil || !strings.Contains(err.Error(), "prod") {
		t.Errorf("a name twice: %v", err)
	}
	// a name a connection has is taken: the temporary one keeps its extension
	writeConfig(t, "connections.toml", "[[connection]]\nname = \"app\"\nengine = \"postgres\"\ndsn = \"postgres://h/d\"\n")
	if c, err := tempConnection("./app.db", "", ""); err != nil || c.Name != "app.db" || !c.Temp {
		t.Errorf("a name taken: %+v %v", c, err)
	}
}
