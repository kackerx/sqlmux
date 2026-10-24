//go:build !windows

package config

import (
	"context"
	"os/exec"
)

// ShellCommand is how a command line of the config runs (§14): sh -c.
func ShellCommand(ctx context.Context, line string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", line)
}
