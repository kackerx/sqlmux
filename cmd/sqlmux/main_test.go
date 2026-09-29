package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sqlmux/internal/app"
)

func writeConfig(t *testing.T, toml string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", t.TempDir()) // open() reads the consoles there
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	os.MkdirAll(filepath.Join(dir, "sqlmux"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "sqlmux", "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKeysCheck(t *testing.T) {
	writeConfig(t, "")
	var out, errb bytes.Buffer
	if code := keysCmd([]string{"--check"}, &out, &errb); code != 0 || errb.Len() > 0 {
		t.Fatalf("clean config: exit %d, stderr %q", code, errb.String())
	}

	writeConfig(t, "[keys.normal]\n\"<C-x>\" = \"a\"\n\"<c-x>\" = \"b\"\n")
	errb.Reset()
	if code := keysCmd([]string{"--check"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "[keys.normal] <c-x>") {
		t.Fatalf("duplicate: exit %d, stderr %q", code, errb.String())
	}
}

func TestKeysFormats(t *testing.T) {
	writeConfig(t, "[keys.grid]\nx = \"\"\n")
	for format, want := range map[string]string{"md": "| global | `C-p` | palette.open |", "toml": "[keys.grid]"} {
		var out, errb bytes.Buffer
		if code := keysCmd([]string{"--format", format}, &out, &errb); code != 0 || !strings.Contains(out.String(), want) {
			t.Errorf("--format %s: exit %d, output lacks %q", format, code, want)
		}
	}
}

// The toml export is the full reference (§6.7): loaded back as config.toml it
// changes nothing, and every titled action is in it, on its binding lines or,
// bound nowhere, on exactly one commented line.
func TestKeysTOMLReference(t *testing.T) {
	keys := func(args ...string) string {
		var out, errb bytes.Buffer
		if code := keysCmd(args, &out, &errb); code != 0 || errb.Len() > 0 {
			t.Fatalf("keys %v: exit %d, stderr %q", args, code, errb.String())
		}
		return out.String()
	}
	writeConfig(t, "")
	md, toml := keys(), keys("--format", "toml")
	writeConfig(t, toml)
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
		bound := len(regexp.MustCompile(`(?m)^".*" = "`+regexp.QuoteMeta(id)+`( .*)?"  # `+regexp.QuoteMeta(title)+`$`).FindAllString(toml, -1))
		commented := strings.Count(toml, `# "" = "`+id+`"  # `+title+"\n")
		if !(bound > 0 && commented == 0 || bound == 0 && commented == 1) {
			t.Errorf("%s: %d binding lines, %d commented lines", id, bound, commented)
		}
	}
}

// Startup errors name the file to fix (§14).
func TestOpenWithoutConnections(t *testing.T) {
	writeConfig(t, "")
	_, _, err := open("")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sqlmux", "connections.toml")) {
		t.Fatalf("no connections.toml: %v", err)
	}
}
