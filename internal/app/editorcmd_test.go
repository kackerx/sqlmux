package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// editorCmd runs the editor on the query file as written: cat (type on
// windows) reads the file back whatever the query is called — a space in
// the name included, a $ or a backtick on unix, where the path would go
// through the shell's parsing if it were quoted into the line (§14).
func TestEditorCmd(t *testing.T) {
	ed := "cat"
	if runtime.GOOS == "windows" {
		ed = "type"
	}
	t.Setenv("VISUAL", ed)
	t.Setenv("EDITOR", "")
	names := []string{"plain", "a b"}
	if runtime.GOOS != "windows" {
		names = append(names, "cost$HOME", "x`echo INJECTED`y")
	}
	for _, name := range names {
		path := filepath.Join(t.TempDir(), name+".sql")
		if err := os.WriteFile(path, []byte("select '"+name+"';\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := editorCmd(path).Output()
		if string(out) != "select '"+name+"';\n" || err != nil {
			t.Errorf("%s: %q %v", name, out, err)
		}
	}
}
