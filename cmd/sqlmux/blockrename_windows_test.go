//go:build windows

package main

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// blockRename holds a file of dir open without FILE_SHARE_DELETE: a rename
// of the directory fails while it is.
func blockRename(t *testing.T, dir string) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(filepath.Join(dir, "held"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_ALWAYS, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
}
