//go:build !linux || duckdb

package duckdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

func TestParse(t *testing.T) {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	abs := filepath.Join(t.TempDir(), "x.duckdb") // an absolute path of the system's own kind
	for _, tc := range []struct {
		spec, path, config string
		create             bool
	}{
		{"~/x.duckdb", filepath.Join(home, "x.duckdb"), "", false},
		{"rel/x.duckdb", filepath.Join(wd, "rel/x.duckdb"), "", false},
		{abs + "?mode=rwc", abs, "", true},
		{abs + "?mode=rw", abs, "", false},
		{abs + "?mode=ro", abs, "access_mode=read_only&", false},
		{abs + "?threads=4&memory_limit=4GB&mode=rwc", abs, "memory_limit=4GB&threads=4&", true},
		{":memory:", ":memory:", "", false},
		{":memory:?threads=2", ":memory:", "threads=2&", false},
	} {
		got, err := parse(tc.spec)
		if err != nil || got.path != tc.path || got.config() != tc.config || got.create != tc.create {
			t.Errorf("parse(%q) = %q %q %v, %v; want %q %q %v", tc.spec, got.path, got.config(), got.create, err, tc.path, tc.config, tc.create)
		}
	}
	if _, err := parse(abs + "?mode=rwx"); err == nil {
		t.Error("a mode that is none")
	}
}

func TestWrapSQL(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		max  int
		want string
	}{
		{"select 1", 0, "select cast(columns(*) as varchar) from (\nselect 1\n)"},
		{"select 1;\n", 0, "select cast(columns(*) as varchar) from (\nselect 1\n)"},
		{"select 1 -- c", 5, "select cast(columns(*) as varchar) from (\nselect 1 -- c\n) limit 6"},
	} {
		if got := wrapSQL(tc.sql, tc.max); got != tc.want {
			t.Errorf("wrapSQL(%q, %d) = %q", tc.sql, tc.max, got)
		}
	}
}

func TestTxAfter(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		tx       byte
		sql      string
		prepared bool // the library took it as a statement
		err      error
		want     byte
	}{
		{'I', "begin", true, nil, 'T'},
		{'I', "start transaction", true, nil, 'T'},
		{'T', "select 1", true, nil, 'T'},
		{'T', "select 1", true, boom, 'E'},
		{'T', "selec 1", false, boom, 'E'}, // not even prepared: it fails the transaction
		{'E', "select 1", true, boom, 'E'},
		{'I', "select nope", true, boom, 'I'},
		{'T', "commit", true, nil, 'I'},
		{'T', "end", true, nil, 'I'},
		{'E', "rollback", true, nil, 'I'},
		{'E', "commit", true, nil, 'I'},
		{'T', "commit", true, boom, 'I'},               // it ran, and failed: the library has rolled back
		{'T', "commit and chain", false, boom, 'E'},    // it was not taken: the transaction failed and is to be rolled back
		{'T', "select 1", true, context.Canceled, 'T'}, // not asked of the library
		{'T', "use x", true, refusal{"no"}, 'T'},
	} {
		word, _, _ := strings.Cut(tc.sql, " ")
		if got := txAfter(tc.tx, word, tc.sql, tc.prepared, tc.err); got != tc.want {
			t.Errorf("txAfter(%c, %q, %v, %v) = %c, want %c", tc.tx, tc.sql, tc.prepared, tc.err, got, tc.want)
		}
	}
}

func TestBlobText(t *testing.T) {
	for in, want := range map[string]string{
		`\xAA\xBB`:     "<binary 2 bytes>",
		`\x00Aabc\x0A`: "<binary 6 bytes>",
		``:             "<binary 0 bytes>",
		`abc`:          "<binary 3 bytes>",
		`\x5C`:         "<binary 1 bytes>",
	} {
		if got := blobText(in, false); got != want {
			t.Errorf("blobText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := blobText(`\xAA`, true); got != `'\xAA'::BLOB` {
		t.Errorf("as a literal: %q", got)
	}
	if !db.IsBinaryShown(blobText(`\xAA`, false)) {
		t.Error("the grid's word for bytes")
	}
}

func TestErrorOf(t *testing.T) {
	for in, want := range map[string]db.ServerError{
		"Catalog Error: Table with name x does not exist!\n\nLINE 1: select * from x": {Severity: "ERROR", Code: "Catalog", Message: "Table with name x does not exist!\n\nLINE 1: select * from x"},
		"INTERRUPT Error: Interrupted!":                                               {Severity: "ERROR", Code: "INTERRUPT", Message: "Interrupted!"},
		"TransactionContext Error: Current transaction is aborted (please ROLLBACK)":  {Severity: "ERROR", Code: "TransactionContext", Message: "Current transaction is aborted (please ROLLBACK)"},
		"Invalid Input Error: Cannot execute: x\nsecond line Error: y":                {Severity: "ERROR", Code: "Invalid Input", Message: "Cannot execute: x\nsecond line Error: y"},
		"Out of Memory Error: failed to allocate":                                     {Severity: "ERROR", Code: "Out of Memory", Message: "failed to allocate"},
		"context canceled": {Severity: "ERROR", Message: "context canceled"},
	} {
		got := (Engine{}).ErrorOf(errors.New(in))
		if got.Severity != want.Severity || got.Code != want.Code || got.Message != want.Message {
			t.Errorf("ErrorOf(%q) = %+v", in, got)
		}
	}
	if (Engine{}).Broken(errors.New("any")) != db.NotLost {
		t.Error("a file is no connection to lose")
	}
}

func TestCandidates(t *testing.T) {
	if got := candidates("linux", "/my/lib.so", "/data", "/exe"); len(got) != 1 || got[0] != "/my/lib.so" {
		t.Errorf("the configured path alone: %q", got)
	}
	got := candidates("linux", "", "/data", "/exe")
	if got[0] != filepath.Join("/data", "duckdb", PinnedVersion, "libduckdb.so") || got[len(got)-1] != "libduckdb.so" || !strings.Contains(strings.Join(got, " "), "/opt/homebrew/lib/libduckdb.so") {
		t.Errorf("linux: %q", got)
	}
	got = candidates("windows", "", "", `C:\app`) // no bare name: the system would look in the working directory
	if len(got) != 1 || !strings.HasSuffix(got[0], "duckdb.dll") || got[0] == "duckdb.dll" {
		t.Errorf("windows: %q", got)
	}
	if got = candidates("windows", "", "", ""); len(got) != 0 {
		t.Errorf("windows with no place known: %q", got)
	}
	if got := candidates("darwin", "", "", ""); got[len(got)-1] != "libduckdb.dylib" {
		t.Errorf("darwin: %q", got)
	}
}

func TestReadable(t *testing.T) {
	for typ, want := range map[int32]bool{1: true, 17: true, 18: true, 19: true, 32: true, 20: false, 23: false, 24: false, 27: false, 31: false} {
		if readable(typ) != want {
			t.Errorf("readable(%d) = %v", typ, !want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	loadMu.Lock()
	was := loaded
	loaded = false
	loadMu.Unlock()
	defer func() {
		loadMu.Lock()
		loaded = was
		loadMu.Unlock()
		Configure("", "")
	}()
	Configure(filepath.Join(t.TempDir(), "no.so"), "")
	if err := Load(); err == nil || !strings.Contains(err.Error(), "no.so") {
		t.Errorf("a configured library that is not there: %v", err)
	}
	notLib := filepath.Join(t.TempDir(), "x.so")
	if err := os.WriteFile(notLib, []byte("not a library"), 0o644); err != nil {
		t.Fatal(err)
	}
	Configure(notLib, "")
	if err := Load(); err == nil || !strings.Contains(err.Error(), notLib) {
		t.Errorf("a file that is no library: %v", err)
	}
	data := t.TempDir()
	Configure("", data)
	var nf *NotFound
	if err := Load(); err != nil && (!errors.As(err, &nf) || nf.Into != downloadPath(runtime.GOOS, data)) { // a machine with the library in a place it looks has it loaded
		t.Errorf("no library in the data directory: %v", err)
	}
}

// A library that has not the functions is said to be too old, not a panic.
func TestBindMissing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("libc.so.6")
	}
	h, err := openLib("libc.so.6")
	if err != nil {
		t.Skip(err)
	}
	if err := bind(h); err == nil || !strings.Contains(err.Error(), "duckdb_open_ext") {
		t.Errorf("bind: %v", err)
	}
}

func TestEngineSQL(t *testing.T) {
	e := Engine{}
	for got, want := range map[string]string{
		e.Quote("t"):                `"t"`,
		e.Quote("main", "my table"): `"main"."my table"`,
		e.Quote(`a"b`):              `"a""b"`,
		e.Arg(3):                    "$3",
		e.Same("c", "$2"):           "(c is not distinct from $2)",
		e.Eq(`"k"`, "$1"):           `("k" is not distinct from $1)`,
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if e.Dialect() != sqlkit.DuckDB {
		t.Error("dialect")
	}
	// no savepoints: setting one does nothing; taking back a save's fails the transaction, a read's or an export's does nothing
	set, rollback, release := (*Conn)(nil).Savepoint(db.SaveSavepoint)
	if set != "select 1" || !strings.HasPrefix(rollback, "select cast(") || release != "" || (*Conn)(nil).Begin() != "begin" {
		t.Errorf("tx words %q %q %q", set, rollback, release)
	}
	for _, name := range []string{"sqlmux_page", "sqlmux_export"} {
		if set, rollback, release := (*Conn)(nil).Savepoint(name); set != "select 1" || rollback != "select 1" || release != "" {
			t.Errorf("%s: %q %q %q", name, set, rollback, release)
		}
	}
}

func TestAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"v2.0.1": true, "v2.0.0": true, "v2.1.0-dev123": true, "v10.0.0": true, "v1.4.4": false, "v1.9.9": false, "v1.5": false, "": false} {
		if atLeast(v, "2.0.0") != want {
			t.Errorf("atLeast(%q) = %v", v, !want)
		}
	}
}
