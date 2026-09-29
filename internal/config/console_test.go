package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Console files go under XDG_DATA_HOME, in directories and files only the
// user can read (§11, §13).
func TestConsoleFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	path := ConsolePath("seed", 1)
	if want := filepath.Join(dir, "sqlmux", "consoles", "seed", "console_1.sql"); path != want {
		t.Fatalf("path %s, want %s", path, want)
	}
	if text, err := ReadConsole(path); text != "" || err != nil {
		t.Errorf("no file yet: %q %v", text, err)
	}
	if err := WriteConsole(path, "select 1;\n"); err != nil {
		t.Fatal(err)
	}
	if text, err := ReadConsole(path); text != "select 1;\n" || err != nil {
		t.Errorf("read back: %q %v", text, err)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700, filepath.Join(dir, "sqlmux"): 0o700} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %v", p, info.Mode().Perm(), err, want)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*-*")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}
