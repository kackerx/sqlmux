package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, toml string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
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
