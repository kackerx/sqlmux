package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A command line holding a quoted path with a space runs as written (§14):
// a password_cmd reading a file under a directory with a space in its name.
func TestShellCommandQuotedPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "pw.txt")
	if err := os.WriteFile(f, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := `cat "` + f + `"`
	if runtime.GOOS == "windows" {
		line = `type "` + f + `"`
	}
	if out, err := ShellCommand(context.Background(), line).Output(); string(out) != "secret" || err != nil {
		t.Errorf("%s: %q %v", line, out, err)
	}
}
