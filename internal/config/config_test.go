package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

func TestParse(t *testing.T) {
	c, err := Parse(`
icons = "ascii"
timeoutlen = 300
tab_width = 4
result_height = 0.3
keyword_case = "upper"
formatprg = "pg_format -"
autopairs = false
confirm_dangerous = false
[query]
max_rows = 50
[grid]
full_width = 12
`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Icons != ui.ASCIIIcons || c.Timeoutlen != 300 || c.TabWidth != 4 || c.ResultHeight != 0.3 || c.MaxRows != 50 || c.GridFullWidth != 12 || c.KeywordCase != "upper" || c.FormatPrg != "pg_format -" || c.AutoPairs || c.ConfirmDangerous {
		t.Fatalf("got %+v", c)
	}
}

// [grid] order and transpose are what a table opened anew starts with
// (F8.8); order is "asc" or "desc".
func TestGridNewTableDefaults(t *testing.T) {
	if c, err := Parse(""); err != nil || c.GridDesc || c.GridTranspose {
		t.Errorf("default: %+v %v", c, err)
	}
	if c, err := Parse("[grid]\norder = \"desc\"\ntranspose = true"); err != nil || !c.GridDesc || !c.GridTranspose {
		t.Errorf("desc, transpose: %+v %v", c, err)
	}
	if c, err := Parse("[grid]\norder = \"asc\""); err != nil || c.GridDesc {
		t.Errorf("asc: %+v %v", c, err)
	}
	for _, s := range []string{"[grid]\norder = \"down\"", "[grid]\ntranspose = \"yes\""} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q: expected an error", s)
		}
	}
}

// border names the boxes' lines; another name is an error that lists them
// (F8.10).
func TestBorder(t *testing.T) {
	single, _ := ui.BorderOf("single")
	if c, err := Parse(""); err != nil || !reflect.DeepEqual(c.Border, single) {
		t.Errorf("default: %+v %v", c.Border, err)
	}
	for _, b := range []string{"single", "rounded", "double", "bold", "hidden"} {
		want, _ := ui.BorderOf(b)
		if c, err := Parse(`border = "` + b + `"`); err != nil || !reflect.DeepEqual(c.Border, want) {
			t.Errorf("%s: %v", b, err)
		}
	}
	if _, err := Parse(`border = "dotted"`); err == nil || err.Error() != `border = "dotted": must be "single", "rounded", "double", "bold" or "hidden"` {
		t.Errorf("dotted: %v", err)
	}
}

func TestGridFullWidthDefault(t *testing.T) {
	if c, err := Parse(""); err != nil || c.GridFullWidth != 22 {
		t.Errorf("default: %d %v", c.GridFullWidth, err)
	}
}

// Keys belong in keymaps.toml, and only keys (F5.9).
func TestParseErrors(t *testing.T) {
	for _, s := range []string{
		`icons = "emoji"`,
		`timeoutlen = 0`,
		`tab_width = 0`,
		`result_height = 1`,
		`keyword_case = "title"`,
		`autopairs = "yes"`,
		`confirm_dangerous = "no"`,
		"[query]\nmax_rows = 0",
		"[grid]\nfull_width = 0",
		"[keys]\nleader = \"<C-a>\"",
		"[map.normal]\nJ = \"5j\"",
		"[keys.normal\n",
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("config.toml %q: expected an error", s)
		}
	}
}

// Load reads config.toml and keymaps.toml, each optional; an error names
// its file, and one with keys in config.toml says where they go (F5.9).
func TestLoadUsesXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if c, err := Load(); err != nil || c.Icons != ui.NerdIcons {
		t.Fatalf("missing files: %+v %v", c, err)
	}
	conf, keys := filepath.Join(dir, "sqlmux", "config.toml"), filepath.Join(dir, "sqlmux", "keymaps.toml")
	if err := os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keys, []byte("[keys.grid]\nx = \"\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err != nil || len(c.Bindings) != 1 {
		t.Fatalf("keymaps.toml alone: %+v %v", c, err)
	}
	if err := os.WriteFile(conf, []byte(`icons = "ascii"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err != nil || c.Icons != ui.ASCIIIcons || len(c.Bindings) != 1 {
		t.Fatalf("got %+v %v", c, err)
	}
	if err := os.WriteFile(conf, []byte("[keys.grid]\nx = \"\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), conf+": ") || !strings.Contains(err.Error(), "keymaps.toml") {
		t.Errorf("keys in config.toml: %v", err)
	}
	if err := os.WriteFile(conf, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keys, []byte(`theme = "x"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.HasPrefix(err.Error(), keys+": theme: ") {
		t.Errorf("theme in keymaps.toml: %v", err)
	}
}

// ristretto is built in: with no themes/ristretto.toml `theme = "ristretto"` is
// the one in the binary, its [icon] on the icon set `icons` picks, and the
// theme list has it; a file of the same name is read instead, not over it (F20.7).
func TestBuiltinRistretto(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sqlmux", "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	load := func() *Config {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "sqlmux", "config.toml"), []byte("theme = \"ristretto\"\nicons = \"ascii\""), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := load()
	th := c.Theme
	if names := ThemeNames(); !slices.Equal(names, []string{"ristretto", "tokyonight-storm"}) || c.ThemeName != "ristretto" || len(c.ThemeUnknown) != 0 {
		t.Errorf("themes %v, in use %q, unknown %q", names, c.ThemeName, c.ThemeUnknown)
	}
	if th.Bg != ansi.XParseColor("#312b2b") || th.PaneBg != ansi.XParseColor("#393333") || th.Border != ansi.XParseColor("#7a7272") || th.Fg == ui.TokyonightStorm.Fg {
		t.Errorf("bg %v pane_bg %v border %v fg %v", th.Bg, th.PaneBg, th.Border, th.Fg)
	}
	if th.Gap != th.PaneBg || th.BorderFocus != th.Focus || th.Focus == ui.TokyonightStorm.Focus { // none written: its own pane_bg and focus
		t.Errorf("gap %v border_focus %v", th.Gap, th.BorderFocus)
	}
	// the theme's [icon] glyphs are Nerd Font's: on the ascii set the text is not taken, the fg is
	if c.Icons.Query.Text != ui.ASCIIIcons.Query.Text || c.Icons.Query.Fg != ansi.XParseColor("#78dce8") || c.Icons.Table.Fg != ansi.XParseColor("#a9dc76") || c.Icons.Table.Text != ui.ASCIIIcons.Table.Text {
		t.Errorf("icons: query %+v table %+v", c.Icons.Query, c.Icons.Table)
	}
	if _, ic, _, err := LoadTheme("ristretto", ui.NerdIcons); err != nil || ic.Query.Text != "\uf489" {
		t.Errorf("on the nerd set the glyph is the theme's: %q %v", ic.Query.Text, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", "themes", "ristretto.toml"), []byte(`pane_bg = "#010203"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := load(); c.Theme.PaneBg != ansi.XParseColor("#010203") || c.Theme.Bg != ui.TokyonightStorm.Bg || c.Icons.Query.Fg == ansi.XParseColor("#78dce8") {
		t.Errorf("the user's file is the theme: pane_bg %v bg %v", c.Theme.PaneBg, c.Theme.Bg)
	}
	if names := ThemeNames(); !slices.Equal(names, []string{"ristretto", "tokyonight-storm"}) {
		t.Errorf("themes with a file of the same name: %v", names)
	}
}

func TestThemeFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	themes := filepath.Join(dir, "sqlmux", "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "ristretto.toml"), []byte("pane_bg = \"#393333\"\n[icon]\nquery = { text = \"C\" }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "typo.toml"), []byte(`rwo = "#393333"`), 0o644); err != nil {
		t.Fatal(err)
	}
	load := func(config string) (*Config, error) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "sqlmux", "config.toml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load()
	}

	// [icon] lands on the set `icons` picks, wherever `icons` is written
	c, err := load("theme = \"ristretto\"\nicons = \"ascii\"")
	if err != nil {
		t.Fatal(err)
	}
	if names := ThemeNames(); !slices.Equal(names, []string{"ristretto", "tokyonight-storm", "typo"}) || c.ThemeName != "ristretto" || c.BaseIcons == c.Icons {
		t.Errorf("themes %v, in use %q, base icons kept apart: %v", names, c.ThemeName, c.BaseIcons != c.Icons)
	}
	if c.Theme.PaneBg == ui.TokyonightStorm.PaneBg || c.Theme.Fg != ui.TokyonightStorm.Fg {
		t.Errorf("pane_bg %v fg %v", c.Theme.PaneBg, c.Theme.Fg)
	}
	if c.Icons.Query.Text != "C" || c.Icons.Table != ui.ASCIIIcons.Table {
		t.Errorf("icons %+v", c.Icons)
	}
	if c, err := load(`theme = "tokyonight-storm"`); err != nil || c.Theme != ui.TokyonightStorm {
		t.Errorf("built-in: %v", err)
	}
	if _, err := load(`theme = "nope"`); err == nil || !strings.Contains(err.Error(), "nope.toml") {
		t.Errorf("a theme there is not: %v", err)
	}
	// a key the theme file has of a later version is a word at the start, not an error (F14.7)
	c, err = load(`theme = "typo"`)
	if err != nil || c.Theme.PaneBg != ui.TokyonightStorm.PaneBg {
		t.Fatalf("unknown key: %v", err)
	}
	if want := "themes/typo.toml: 不认识的键 rwo"; !slices.Equal(c.ThemeUnknown, []string{"rwo"}) || !slices.Contains(UnknownKeys(c), want) {
		t.Errorf("unknown %q, start says %q", c.ThemeUnknown, UnknownKeys(c))
	}
	i18n.Set("en") // the sentence is made when the language is known, not when the file is read
	if got := UnknownKeys(c); !slices.Contains(got, "themes/typo.toml: unknown key(s) rwo") {
		t.Errorf("in English: %q", got)
	}
	i18n.Set("zh")
	if th, _, unknown, err := LoadTheme("ristretto", ui.NerdIcons); err != nil || len(unknown) != 0 || th == nil {
		t.Errorf("a theme with no unknown key says nothing: %q %v", unknown, err)
	}
	if c, err := Parse(`theme = "nope"`); err != nil || c.Theme != ui.TokyonightStorm {
		t.Errorf("Parse reads no theme file: %v", err)
	}
}

// language is "en" or "zh", en when not written; another one is an error
// that says, in English, what is allowed (§14「界面语言」, F7.6).
func TestLanguage(t *testing.T) {
	if c, err := Parse(""); err != nil || c.Language != "en" {
		t.Errorf("default: %q %v", c.Language, err)
	}
	if c, err := Parse(`language = "zh"`); err != nil || c.Language != "zh" {
		t.Errorf("zh: %v", err)
	}
	if _, err := Parse(`language = "fr"`); err == nil || err.Error() != `language = "fr": must be "en" or "zh"` {
		t.Errorf("fr: %v", err)
	}
}

// vim is true unless it is written false (§6.9).
func TestVimOption(t *testing.T) {
	if c, err := Parse(""); err != nil || !c.Vim {
		t.Errorf("default: %v %v", c.Vim, err)
	}
	if c, err := Parse("vim = false"); err != nil || c.Vim {
		t.Errorf("false: %v %v", c.Vim, err)
	}
	if _, err := Parse(`vim = "no"`); err == nil {
		t.Error("a string is not a bool")
	}
}

// The confirmation of dangerous statements is on unless config.toml turns it
// off (§13, F6.7).
func TestConfirmDangerousDefault(t *testing.T) {
	if c, err := Parse(""); err != nil || !c.ConfirmDangerous {
		t.Errorf("default: %v %v", c, err)
	}
	if c, err := Parse("confirm_dangerous = false"); err != nil || c.ConfirmDangerous {
		t.Errorf("off: %v %v", c, err)
	}
}

func TestTreeWidth(t *testing.T) {
	if c, err := Parse(""); err != nil || c.TreeWidth != 32 {
		t.Errorf("default: %d %v", c.TreeWidth, err)
	}
	if c, err := Parse("[tree]\nwidth = 38"); err != nil || c.TreeWidth != 38 {
		t.Errorf("38: %d %v", c.TreeWidth, err)
	}
	if c, err := Parse("[tree]\nwidth = -5"); err != nil || c.TreeWidth != -5 { // no error: the sidebar holds it to 16
		t.Errorf("-5: %d %v", c.TreeWidth, err)
	}
}

func TestTreeFollow(t *testing.T) {
	for in, want := range map[string]bool{"": false, "[tree]\nfollow = true": true, "[tree]\nfollow = false": false} {
		if c, err := Parse(in); err != nil || c.TreeFollow != want {
			t.Errorf("%q: %v %v", in, c.TreeFollow, err)
		}
	}
}

// clipboard is "" or "unnamedplus"; another value is told like a key nothing
// reads and leaves the default (F20.5).
func TestClipboard(t *testing.T) {
	for in, want := range map[string]string{"": "", `clipboard = "unnamedplus"`: "unnamedplus", `clipboard = ""`: "", `clipboard = "unnamed"`: ""} {
		c, err := Parse(in)
		if err != nil || c.Clipboard != want {
			t.Errorf("%q: %q %v", in, c.Clipboard, err)
		}
		if bad := strings.Contains(in, `"unnamed"`); bad != (len(c.Unknown) == 1 && c.Unknown[0] == `clipboard = "unnamed"`) {
			t.Errorf("%q: unknown %q", in, c.Unknown)
		}
	}
}

// A key nothing reads, misspelt, is said at the start, with the connection it
// is in: it is no error (F13.5).
func TestUnknownKeys(t *testing.T) {
	c, err := Parse("confirm_dngerous = false\nmax_rows = 10\n[query]\nmax_rows = 20\n[tree]\nwidht = 30\n")
	if err != nil || c.MaxRows != 20 {
		t.Fatalf("an unknown key is no error: %v, %v", err, c)
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o700); err != nil {
		t.Fatal(err)
	}
	conns := "[[connection]]\nname = \"ok\"\nengine = \"postgres\"\ndsn = \"postgres://u@h/d\"\nread_only = true\n\n[[connection]]\nname = \"D\"\nengine = \"postgres\"\ndsn = \"postgres://u@h/d\"\nreadonly = true\nnote = \"x\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", "connections.toml"), []byte(conns), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"config.toml: 不认识的键 confirm_dngerous, max_rows, tree.widht",
		`connections.toml: 不认识的键 note, readonly（connection "D"）`,
	}
	if got := UnknownKeys(c); !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
	clean, _ := Parse("max_rows_x = 1\n")
	if len(clean.Unknown) != 1 {
		t.Errorf("%v", clean.Unknown)
	}
	ok, _ := Parse("[query]\nmax_rows = 20\n")
	if got := UnknownKeys(ok); len(got) != 1 || strings.HasPrefix(got[0], "config.toml") {
		t.Errorf("only the connection: %q", got)
	}
	// outside a [[connection]]: a key of the top, a block misspelt, said once
	stray := "default = \"prod\"\n[[servers]]\nname = \"x\"\n[[servers]]\nname = \"y\"\n[[connection]]\nname = \"z\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", "connections.toml"), []byte(stray), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := UnknownKeys(ok); !slices.Equal(got, []string{"connections.toml: 不认识的键 default, servers"}) {
		t.Errorf("outside the connections: %q", got)
	}
	twice, _ := Parse("[[foo]]\na = 1\n[[foo]]\na = 2\n")
	if !slices.Equal(twice.Unknown, []string{"foo", "foo.a"}) {
		t.Errorf("a table twice: %v", twice.Unknown)
	}
	// the decoder reads a key whatever its case: not unknown
	upper := "[[connection]]\nName = \"x\"\nDSN = \"postgres://u@h/d\"\nEngine = \"postgres\"\nRead_Only = true\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", "connections.toml"), []byte(upper), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := UnknownKeys(ok); len(got) != 0 {
		t.Errorf("keys of another case: %q", got)
	}
}

// The line of the field a query's value goes in is on, `[query] hints = false`
// turns it off (F15.3).
func TestParseHints(t *testing.T) {
	if c, err := Parse(""); err != nil || !c.Hints {
		t.Fatalf("default: %v %v", c, err)
	}
	if c, err := Parse("[query]\nhints = false\n"); err != nil || c.Hints {
		t.Fatalf("false: %v %v", c, err)
	}
}

// The status bar's tips are on, `[ui] tips = false` turns them off (F15.4).
func TestParseTips(t *testing.T) {
	if c, err := Parse(""); err != nil || !c.Tips {
		t.Fatalf("default: %v %v", c, err)
	}
	if c, err := Parse("[ui]\ntips = false\n"); err != nil || c.Tips {
		t.Fatalf("false: %v %v", c, err)
	}
}

// [redis] has the delimiter, the scan budget and the length a key is read whole
// up to, with the defaults of the design (§14, F16.1).
func TestParseRedis(t *testing.T) {
	if c, err := Parse(""); err != nil || c.RedisDelimiter != ":" || c.RedisScanKeys != 10000 || c.RedisLoadAll != 5000 || c.RedisIndexKeys != 100000 {
		t.Fatalf("default: %+v %v", c, err)
	}
	c, err := Parse("[redis]\ndelimiter = \"/\"\nscan_keys = 500\nload_all = 20\nindex_keys = 7\n")
	if err != nil || c.RedisDelimiter != "/" || c.RedisScanKeys != 500 || c.RedisLoadAll != 20 || c.RedisIndexKeys != 7 || len(c.Unknown) != 0 {
		t.Fatalf("set: %+v %v", c, err)
	}
	for _, bad := range []string{"delimiter = \"\"", "scan_keys = 0", "load_all = -1", "index_keys = 0"} {
		if _, err := Parse("[redis]\n" + bad + "\n"); err == nil {
			t.Errorf("%s is accepted", bad)
		}
	}
	if c, _ := Parse("[redis]\ndelimeter = \":\"\n"); len(c.Unknown) != 1 {
		t.Errorf("a misspelt key: %v", c.Unknown)
	}
}

// A save shows its statements first unless [grid] preview_save says it does not (F17.10).
func TestPreviewSave(t *testing.T) {
	for toml, want := range map[string]bool{"": true, "[grid]\npreview_save = true\n": true, "[grid]\npreview_save = false\n": false} {
		if c, err := Parse(toml); err != nil || c.PreviewSave != want {
			t.Errorf("%q: %v %v", toml, c != nil && c.PreviewSave, err)
		}
	}
}

// default_connection is a name, empty when not written; DefaultStart finds it,
// and takes the first for an empty or an unknown one (F21.2).
func TestDefaultConnection(t *testing.T) {
	if c, err := Parse(""); err != nil || c.DefaultConnection != "" {
		t.Errorf("not written: %q, %v", c.DefaultConnection, err)
	}
	c, err := Parse("default_connection = \"b\"\n")
	if err != nil || c.DefaultConnection != "b" || len(c.Unknown) != 0 {
		t.Errorf("written: %q %v %v", c.DefaultConnection, c.Unknown, err)
	}
	conns := []Connection{{Name: "a"}, {Name: "b"}}
	for def, want := range map[string]int{"": 0, "a": 0, "b": 1, "nope": 0} {
		if got := DefaultStart(conns, def); got != want {
			t.Errorf("DefaultStart(%q) = %d, want %d", def, got, want)
		}
	}
}
