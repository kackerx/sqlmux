package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseBindingsInFileOrder(t *testing.T) {
	c, err := Parse(`
icons = "ascii"
timeoutlen = 300
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
	if c.Icons != "ascii" || c.Timeoutlen != 300 || c.Leader != "<C-a>" || !reflect.DeepEqual(c.Bindings, want) {
		t.Fatalf("got %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{
		`icons = "emoji"`,
		`timeoutlen = 0`,
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
	if c, err := Load(); err != nil || c.Icons != "nerd" {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o755)
	os.WriteFile(filepath.Join(dir, "sqlmux", "config.toml"), []byte(`icons = "ascii"`), 0o644)
	if c, err := Load(); err != nil || c.Icons != "ascii" {
		t.Fatalf("got %+v %v", c, err)
	}
}
