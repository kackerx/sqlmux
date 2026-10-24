//go:build windows

package config

import (
	"context"
	"testing"
)

// ShellCommand's whole line goes to %COMSPEC% /c as written (os/exec's
// quoting of a " is \" which cmd does not read), cmd without the variable
// (§14).
func TestShellCommand(t *testing.T) {
	t.Setenv("COMSPEC", `C:\windows\system32\cmd.exe`)
	c := ShellCommand(context.Background(), "echo hi")
	if want := `C:\windows\system32\cmd.exe /c "echo hi"`; c.SysProcAttr.CmdLine != want {
		t.Errorf("cmdline %q, want %q", c.SysProcAttr.CmdLine, want)
	}
	t.Setenv("COMSPEC", "")
	if c := ShellCommand(context.Background(), "echo hi"); c.SysProcAttr.CmdLine != `cmd /c "echo hi"` {
		t.Errorf("without COMSPEC: cmdline %q", c.SysProcAttr.CmdLine)
	}
}
