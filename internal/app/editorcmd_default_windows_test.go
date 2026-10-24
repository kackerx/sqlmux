//go:build windows

package app

import (
	"strings"
	"testing"
)

// Without $VISUAL and $EDITOR the editor is notepad, the path quoted at
// its side (§14).
func TestEditorCmdDefault(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if want := `notepad "x.sql"`; !strings.Contains(editorCmd("x.sql").SysProcAttr.CmdLine, want) {
		t.Errorf("cmdline %q, want %q in it", editorCmd("x.sql").SysProcAttr.CmdLine, want)
	}
}
