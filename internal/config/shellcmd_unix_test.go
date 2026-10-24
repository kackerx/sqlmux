//go:build !windows

package config

import (
	"context"
	"slices"
	"testing"
)

// ShellCommand's argv is sh -c (§14).
func TestShellCommand(t *testing.T) {
	if want := []string{"sh", "-c", "echo hi"}; !slices.Equal(ShellCommand(context.Background(), "echo hi").Args, want) {
		t.Errorf("argv %v, want %v", ShellCommand(context.Background(), "echo hi").Args, want)
	}
}
