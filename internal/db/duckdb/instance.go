//go:build !linux || duckdb

package duckdb

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
)

// memory is the path of the in-memory database.
const memory = ":memory:"

// instance is a database the library has open. A second open of the same file
// in this process is no error: it is a second database that sees none of the
// first's, and the close of either lets go of the file's lock (§8.1). So the
// file is opened once, by the first connection, which the others share, and
// closed with the last.
type instance struct {
	h    uintptr
	key  string
	cfg  string // the settings it was opened with
	refs int
}

var (
	instMu sync.Mutex
	insts  = map[string]*instance{}
)

// target is what a spec names: where, the settings the library takes at open,
// and whether the file may be made.
type target struct {
	path     string
	settings url.Values
	create   bool
}

// parse reads spec as SQLite's rule has it (§8.1): a path with ~ and
// relative to where sqlmux started, :memory:, and after a ? the settings,
// but mode, which rwc makes the file, rw says what is the case, and ro is
// access_mode=read_only.
func parse(spec string) (t target, err error) {
	base, query, _ := strings.Cut(spec, "?")
	t.path = base
	if base != memory {
		if t.path, query, err = db.FilePath(spec); err != nil {
			return t, err
		}
	}
	if t.settings, err = url.ParseQuery(query); err != nil {
		return t, err
	}
	mode := t.settings.Get("mode")
	t.settings.Del("mode")
	switch mode {
	case "", "rw":
	case "rwc":
		t.create = true
	case "ro":
		t.settings.Set("access_mode", "read_only")
	default:
		return t, fmt.Errorf("%s", i18n.T("mode 只能是 rw、rwc 或 ro，这里是 %s", mode))
	}
	return t, nil
}

// config is the settings as one text, the same for the same settings.
func (t target) config() string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(t.settings)) {
		b.WriteString(k + "=" + t.settings.Get(k) + "&")
	}
	return b.String()
}

// acquire is the instance of t, opened if no connection has it open.
func acquire(t target) (*instance, error) {
	if t.path != memory && !t.create {
		if _, err := os.Stat(t.path); err != nil { // a file that is not there is no new database: a path mistyped
			return nil, err
		}
	}
	t.path = realPath(t.path)
	instMu.Lock()
	defer instMu.Unlock()
	if i, ok := insts[t.path]; ok {
		if i.cfg != t.config() {
			return nil, fmt.Errorf("%s", i18n.T("%s 已经用别的设置打开了：同一个文件在一个进程里只能开一次", t.path))
		}
		i.refs++
		return i, nil
	}
	var cfg uintptr
	if c.createConfig(&cfg) != 0 {
		return nil, errors.New("duckdb_create_config failed")
	}
	defer c.destroyCfg(&cfg)
	for k, v := range t.settings {
		if c.setConfig(cfg, k, v[0]) != 0 {
			return nil, fmt.Errorf("%s", i18n.T("DuckDB 不认这个设置：%s=%s", k, v[0]))
		}
	}
	var h, e uintptr
	if c.openExt(t.path, &h, cfg, &e) != 0 {
		return nil, errors.New(fromC(e))
	}
	i := &instance{h: h, key: t.path, cfg: t.config(), refs: 1}
	insts[t.path] = i
	return i, nil
}

// release is a connection done with i; the last one closes the database, and
// the file is the user's again.
func (i *instance) release() {
	instMu.Lock()
	defer instMu.Unlock()
	if i.refs--; i.refs == 0 {
		delete(insts, i.key)
		c.closeDB(&i.h)
	}
}

// realPath is path with its symbolic links followed, so that the instance of a
// file is one whichever way it is reached; a file that is not made yet has its
// directory followed, and is the same path once it is.
// shortcut: a hard link, or a path in other letters on a case-folding disk, is
// another file to this; compare os.SameFile of the open ones if one comes up.
func realPath(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}
