//go:build windows

package config

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// ShellCommand is how a command line of the config runs (§14): %COMSPEC% /c
// (cmd without the variable), the line given to cmd as written through
// SysProcAttr.CmdLine — os/exec's own quoting escapes " as \", which cmd
// does not read, so a quoted path would arrive mangled (§14).
func ShellCommand(ctx context.Context, line string) *exec.Cmd {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd"
	}
	c := exec.CommandContext(ctx, shell)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: shell + ` /c "` + line + `"`}
	return c
}
