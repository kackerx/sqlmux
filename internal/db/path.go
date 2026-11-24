package db

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// FilePath is the file spec names, as the file engines (SQLite, DuckDB) take
// it: ~ at its start is the home directory, a relative path is relative to
// where sqlmux started; what follows a ? is the engine's own parameters, which
// the caller passes on. Both separators are taken on windows.
func FilePath(spec string) (path, query string, err error) {
	path, query, _ = strings.Cut(spec, "?")
	if rest, ok := strings.CutPrefix(path, "~"); ok && (rest == "" || rest[0] == '/' || rest[0] == '\\' && runtime.GOOS == "windows") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		path = home + rest
	}
	path, err = filepath.Abs(path)
	return path, query, err
}
