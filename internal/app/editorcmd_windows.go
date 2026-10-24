//go:build windows

package app

import (
	"cmp"
	"context"
	"os"
	"os/exec"

	"github.com/kackerx/sqlmux/internal/config"
)

// editorCmd is the command opening the query file at path: the editor of
// $VISUAL / $EDITOR, notepad without them, the path quoted into the line —
// a path holding a quote or %…% is a known limit, cmd expands %…% inside
// quotes too (§14).
func editorCmd(path string) *exec.Cmd {
	ed := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "notepad")
	return config.ShellCommand(context.Background(), ed+` "`+path+`"`)
}
