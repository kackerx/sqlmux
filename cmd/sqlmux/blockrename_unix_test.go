//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// blockRename makes a rename of dir fail: its parent chmod 0o500, back to
// 0o700 when the test ends.
func blockRename(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(filepath.Dir(dir), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(dir), 0o700) })
}
