package config

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DataDir is $XDG_DATA_HOME/sqlmux, falling back to ~/.local/share/sqlmux,
// %LOCALAPPDATA%\sqlmux on windows (§14).
func DataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "sqlmux")
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserCacheDir(); err == nil {
			return filepath.Join(d, "sqlmux")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "sqlmux")
}

// QueryName is what the nth query is called until it is renamed (§11).
func QueryName(n int) string { return fmt.Sprintf("query_%d", n) }

// QueryPath is the file of the query called name (§11), whatever
// connection it is on.
func QueryPath(name string) string {
	return filepath.Join(DataDir(), "queries", name+".sql")
}

// CheckQueryName is why name is no name for a query: it is a file's,
// so the rule is a connection's (F8.11). The caller has trimmed it, and not
// asked for a blank one.
func CheckQueryName(name string) error { return checkFileName(name) }

// ReadQuery is the query file at path; "" when there is none yet.
func ReadQuery(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// WriteQuery writes a query's text to path, the user's only (§11, §13).
func WriteQuery(path, text string) error { return writeFile(path, []byte(text)) }

// QueryFiles is the names of the query files (the .sql files of
// queries/, without it), query_<n> by n first, the others by name (§7.8
// 「Queries 节点」); and when the directory last changed, for a cache of it
// to know. None, no time, when there is no directory yet.
func QueryFiles() (names []string, changed time.Time, err error) {
	dir := filepath.Dir(QueryPath("x"))
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, time.Time{}, err
	}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".sql"); ok && !e.IsDir() {
			names = append(names, name)
		}
	}
	SortQueries(names)
	return names, fi.ModTime(), nil
}

// SortQueries puts query_<n> by n first, the renamed ones by name after.
func SortQueries(names []string) {
	number := func(name string) int {
		if n, ok := strings.CutPrefix(name, "query_"); ok {
			if i, err := strconv.Atoi(n); err == nil && i >= 0 && strconv.Itoa(i) == n {
				return i
			}
		}
		return -1
	}
	slices.SortFunc(names, func(a, b string) int {
		na, nb := number(a), number(b)
		switch {
		case na >= 0 && nb >= 0:
			return cmp.Compare(na, nb)
		case na >= 0:
			return -1
		case nb >= 0:
			return 1
		}
		return cmp.Or(strings.Compare(strings.ToLower(a), strings.ToLower(b)), strings.Compare(a, b))
	})
}

// QueryExists is whether the query called name has a file: it is
// written once there is a word in it, and not deleted by closing it (§11).
func QueryExists(name string) bool {
	_, err := os.Stat(QueryPath(name))
	return !errors.Is(err, fs.ErrNotExist) // a file we cannot stat is there, its number not free
}

// DeleteQuery deletes the query file called name; none is no error.
func DeleteQuery(name string) error {
	if err := os.Remove(QueryPath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
