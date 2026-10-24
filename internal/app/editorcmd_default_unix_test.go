//go:build !windows

package app

import "testing"

// Without $VISUAL and $EDITOR the editor is vi, the path as $1 out of the
// shell's parsing (§14).
func TestEditorCmdDefault(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	c := editorCmd("x.sql")
	if len(c.Args) != 5 || c.Args[0] != "sh" || c.Args[2] != `vi "$1"` || c.Args[4] != "x.sql" {
		t.Errorf("argv %v", c.Args)
	}
}
