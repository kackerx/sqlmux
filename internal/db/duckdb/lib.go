// Package duckdb is the DuckDB engine (§8.1, F24.1): the C API of libduckdb,
// loaded as the program runs (purego), so the build stays CGO_ENABLED=0 and
// the binary the size it was. Reference: duckdb.h of the library; the probes
// of specs/m24-engines/eval.md.
package duckdb

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// result is duckdb_result, which the library fills in: six words, only ever
// handed over by pointer.
type result struct{ _ [6]uintptr }

// The C functions of duckdb.h that are used; a handle (database, connection,
// prepared statement) is the one pointer its struct holds, and the ones the
// library makes are given back through a pointer to it.
var c struct {
	openExt      func(path string, out *uintptr, cfg uintptr, errp *uintptr) int32
	connect      func(db uintptr, out *uintptr) int32
	disconnect   func(conn *uintptr)
	closeDB      func(db *uintptr)
	interrupt    func(conn uintptr)
	query        func(conn uintptr, q string, out *result) int32
	prepare      func(conn uintptr, q string, out *uintptr) int32
	prepareError func(st uintptr) uintptr
	destroyPrep  func(st *uintptr)
	stmtType     func(st uintptr) int32
	bindVarchar  func(st uintptr, idx uint64, v string, n uint64) int32
	bindNull     func(st uintptr, idx uint64) int32
	execPrepared func(st uintptr, out *result) int32
	destroy      func(r *result)
	resultError  func(r *result) uintptr
	rowCount     func(r *result) uint64
	rowsChanged  func(r *result) uint64
	colCount     func(r *result) uint64
	colName      func(r *result, i uint64) uintptr
	colType      func(r *result, i uint64) int32
	valVarchar   func(r *result, col, row uint64) uintptr
	isNull       func(r *result, col, row uint64) bool
	free         func(p uintptr)
	version      func() uintptr
	createConfig func(out *uintptr) int32
	setConfig    func(cfg uintptr, k, v string) int32
	destroyCfg   func(cfg *uintptr)
}

var symbols = []struct {
	fn   any
	name string
}{
	{&c.openExt, "duckdb_open_ext"}, {&c.connect, "duckdb_connect"}, {&c.disconnect, "duckdb_disconnect"},
	{&c.closeDB, "duckdb_close"}, {&c.interrupt, "duckdb_interrupt"}, {&c.query, "duckdb_query"},
	{&c.prepare, "duckdb_prepare"}, {&c.prepareError, "duckdb_prepare_error"},
	{&c.destroyPrep, "duckdb_destroy_prepare"}, {&c.stmtType, "duckdb_prepared_statement_type"},
	{&c.bindVarchar, "duckdb_bind_varchar_length"}, {&c.bindNull, "duckdb_bind_null"},
	{&c.execPrepared, "duckdb_execute_prepared"}, {&c.destroy, "duckdb_destroy_result"},
	{&c.resultError, "duckdb_result_error"}, {&c.rowCount, "duckdb_row_count"}, {&c.rowsChanged, "duckdb_rows_changed"},
	{&c.colCount, "duckdb_column_count"}, {&c.colName, "duckdb_column_name"},
	{&c.colType, "duckdb_column_type"}, {&c.valVarchar, "duckdb_value_varchar"},
	{&c.isNull, "duckdb_value_is_null"}, {&c.free, "duckdb_free"}, {&c.version, "duckdb_library_version"},
	{&c.createConfig, "duckdb_create_config"}, {&c.setConfig, "duckdb_set_config"},
	{&c.destroyCfg, "duckdb_destroy_config"},
}

// goString is the C string at p, "" for none.
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&p)) // the pointer the library gave, not Go memory
	n := 0
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n))
}

// fromC is a string the library allocated, taken and freed.
func fromC(p uintptr) string {
	s := goString(p)
	if p != 0 {
		c.free(p)
	}
	return s
}

// Version is the library's, as it says: v2.0.1.
func Version() string { return goString(c.version()) }

// PinnedVersion is the libduckdb the downloads are of and the tests run on;
// the library is looked for under this version in the data directory.
const PinnedVersion = "2.0.1"

// minVersion is the oldest libduckdb the catalog queries run on: 1.4.4 has no
// duckdb_columns().is_generated (tried on both).
const minVersion = "2.0.0"

// atLeast is whether version v, "v2.0.1" or "v2.1.0-dev123", is minimum or
// later, by its numbers.
func atLeast(v, minimum string) bool {
	nums := func(s string) (n [3]int) {
		s, _, _ = strings.Cut(strings.TrimPrefix(s, "v"), "-")
		for i, f := range strings.SplitN(s, ".", 3) {
			n[i], _ = strconv.Atoi(f)
		}
		return n
	}
	a, b := nums(v), nums(minimum)
	return slices.Compare(a[:], b[:]) >= 0
}

var (
	loadMu           sync.Mutex
	loaded           bool
	library, dataDir string
)

// Configure says where to look for the library (§8.1「库从哪来」): the path
// config.toml's [duckdb] library gives, "" for none, and the data directory,
// whose duckdb/<version>/ has the one that was downloaded.
func Configure(libraryPath, data string) {
	loadMu.Lock()
	defer loadMu.Unlock()
	library, dataDir = libraryPath, data
}

// NotFound is no libduckdb in any of the places there are to look.
type NotFound struct{ Tried []string }

func (e *NotFound) Error() string {
	return i18n.T("找不到 libduckdb（找过：%s）。装好后在 config.toml 的 [duckdb] library 写上它的路径", strings.Join(e.Tried, ", "))
}

// libName is the file the library is, and what the system finds it by.
func libName(goos string) string {
	switch goos {
	case "windows":
		return "duckdb.dll"
	case "darwin":
		return "libduckdb.dylib"
	}
	return "libduckdb.so"
}

// candidates are the places to look for the library, in order (§8.1): the
// configured path alone when there is one; else the data directory's, the
// package managers' directories, the program's own on windows, then the bare
// name, which the system's loader looks up as it does; on windows none, whose
// search would take a duckdb.dll of the working directory.
func candidates(goos, configured, data, exeDir string) []string {
	if configured != "" {
		return []string{configured}
	}
	name := libName(goos)
	var out []string
	if data != "" {
		out = append(out, filepath.Join(data, "duckdb", PinnedVersion, name))
	}
	if goos == "windows" {
		if exeDir != "" {
			out = append(out, filepath.Join(exeDir, name))
		}
	} else {
		for _, d := range []string{"/opt/homebrew/lib", "/usr/local/lib", "/home/linuxbrew/.linuxbrew/lib"} {
			out = append(out, d+"/"+name)
		}
	}
	if goos == "windows" {
		return out
	}
	return append(out, name)
}

// Load loads the library once; a failure is tried again by the next call, so
// a library put there later is found.
func Load() error {
	loadMu.Lock()
	defer loadMu.Unlock()
	if loaded {
		return nil
	}
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	var tried []string
	for _, p := range candidates(runtime.GOOS, library, dataDir, exeDir) {
		_, statErr := os.Stat(p)
		bare := p == filepath.Base(p) // the loader's to find
		if statErr != nil && !bare {
			if library != "" {
				return fmt.Errorf("%s", i18n.T("[duckdb] library 的 %s 不存在", p))
			}
			tried = append(tried, p)
			continue
		}
		h, err := openLib(p)
		if err != nil {
			if bare && library == "" {
				tried = append(tried, p)
				continue
			}
			return loadError(p, err)
		}
		if err := bind(h); err != nil {
			closeLib(h) // not kept: the user's next try is of another library
			return err
		}
		loaded = true
		return nil
	}
	return &NotFound{Tried: tried}
}

// loadError is a library that is there and does not load: wrong architecture,
// a file that is no library, on windows the runtime it needs.
func loadError(path string, err error) error {
	hint := ""
	if runtime.GOOS == "windows" {
		hint = i18n.T("（缺 Microsoft Visual C++ 运行库时会这样，装 VC++ Redistributable 再试）")
	}
	return fmt.Errorf("%s", i18n.T("加载不了 %s：%s%s", path, err.Error(), hint))
}

// bind points the functions at the library's, or says which one it lacks: an
// older library than the ones the symbols were taken from.
func bind(h uintptr) (err error) {
	var name string
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%s", i18n.T("libduckdb 版本太旧，缺 %s", name))
		}
	}()
	for _, s := range symbols {
		name = s.name
		purego.RegisterLibFunc(s.fn, h, s.name)
	}
	return nil
}
