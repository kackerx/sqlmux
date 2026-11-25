//go:build windows

package duckdb

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// openLib loads the library at path, by its absolute path: the search of a
// bare name goes to the working directory, where a project's own duckdb.dll
// would be run; the DLLs it needs are looked for in its directory and the
// system's only.
func openLib(path string) (uintptr, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	h, err := windows.LoadLibraryEx(abs, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
	return uintptr(h), err
}

func closeLib(h uintptr) { _ = windows.FreeLibrary(windows.Handle(h)) }
