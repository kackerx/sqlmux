//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"testing"
)

// blockDelete makes the directory of the query file at path one nothing
// can be deleted in: chmod 0o500, back to 0o700 when the test ends.
func blockDelete(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}
