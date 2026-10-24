//go:build !windows

package app

import (
	"cmp"
	"os"
	"os/exec"
)

// editorCmd is the command opening the query file at path: the editor of
// $VISUAL / $EDITOR, vi without them, gets the path as $1 — it stays out of
// the shell's parsing, so a name holding $ or ` is opened as it is (§11,
// §14).
func editorCmd(path string) *exec.Cmd {
	ed := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	return exec.Command("sh", "-c", ed+` "$1"`, "sh", path)
}
