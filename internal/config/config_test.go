package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sqlmux/internal/ui"
)

func TestParseBindingsInFileOrder(t *testing.T) {
	c, err := Parse(`
icons = "ascii"
timeoutlen = 300
tab_width = 4
[keys]
leader = "<C-a>"
[keys.normal]
"<C-w>v" = "pane.split.right"
x = ""
[map.normal]
J = "5j"
[map.console.normal]
L = "5l"
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Binding{
		{"keys.normal", "<C-w>v", "pane.split.right"},
		{"keys.normal", "x", ""},
		{"map.normal", "J", "5j"},
		{"map.console.normal", "L", "5l"},
	}
	if c.Icons != ui.ASCIIIcons || c.Timeoutlen != 300 || c.TabWidth != 4 || c.Leader != "<C-a>" || !reflect.DeepEqual(c.Bindings, want) {
		t.Fatalf("got %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{
		`icons = "emoji"`,
		`timeoutlen = 0`,
		`tab_width = 0`,
		"[keys.normal]\nx = 1",
		"[keys]\nx = \"a\"",
		"[keys.normal\n",
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q: expected an error", s)
		}
	}
}

func TestLoadUsesXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if c, err := Load(); err != nil || c.Icons != ui.NerdIcons {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o755)
	os.WriteFile(filepath.Join(dir, "sqlmux", "config.toml"), []byte(`icons = "ascii"`), 0o644)
	if c, err := Load(); err != nil || c.Icons != ui.ASCIIIcons {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestThemeFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	themes := filepath.Join(dir, "sqlmux", "themes")
	os.MkdirAll(themes, 0o755)
	os.WriteFile(filepath.Join(themes, "ristretto.toml"), []byte("pane_bg = \"#393333\"\n[icon]\nconsole = { text = \"C\" }"), 0o644)
	os.WriteFile(filepath.Join(themes, "typo.toml"), []byte(`rwo = "#393333"`), 0o644)
	load := func(config string) (*Config, error) {
		t.Helper()
		os.WriteFile(filepath.Join(dir, "sqlmux", "config.toml"), []byte(config), 0o644)
		return Load()
	}

	// [icon] lands on the set `icons` picks, wherever `icons` is written
	c, err := load("theme = \"ristretto\"\nicons = \"ascii\"")
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme.PaneBg == ui.TokyonightStorm.PaneBg || c.Theme.Fg != ui.TokyonightStorm.Fg {
		t.Errorf("pane_bg %v fg %v", c.Theme.PaneBg, c.Theme.Fg)
	}
	if c.Icons.Console.Text != "C" || c.Icons.Data != ui.ASCIIIcons.Data {
		t.Errorf("icons %+v", c.Icons)
	}
	if c, err := load(`theme = "tokyonight-storm"`); err != nil || c.Theme != ui.TokyonightStorm {
		t.Errorf("built-in: %v", err)
	}
	for name, want := range map[string]string{"nope": "nope.toml", "typo": "rwo"} {
		if _, err := load(`theme = "` + name + `"`); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("theme %s: error %v should name %s", name, err, want)
		}
	}
	if c, err := Parse(`theme = "nope"`); err != nil || c.Theme != ui.TokyonightStorm {
		t.Errorf("Parse reads no theme file: %v", err)
	}
}
