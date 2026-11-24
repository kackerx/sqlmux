package duckdb

// The download of libduckdb (§8.1「库从哪来」, F24.3), asked for when there is
// none to load. Built on every platform, with no purego: the Linux build
// without DuckDB has no library to look for, so it is never offered there.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// PinnedVersion is the libduckdb the downloads are of and the tests run on;
// the library is looked for under this version in the data directory.
const PinnedVersion = "2.0.1"

// minVersion is the oldest libduckdb the catalog queries run on: 1.4.4 has no
// duckdb_columns().is_generated (tried on both).
var minVersion = "2.0.0" // a var for the tests

// archive is a platform's archive of the shared library in DuckDB's Release:
// its name, size in bytes, and sha256 (the digests GitHub's release API gives,
// each the same as the archive downloaded hashes to).
type archive struct {
	name   string
	size   int64
	sha256 string
}

// archives are the platforms the Release has one for, by GOOS/GOARCH; Linux's
// are glibc's, as the duckdb build of sqlmux is (§15).
var archives = map[string]archive{
	"linux/amd64":   {"duckdb-shared-libs-linux-amd64.tar.gz", 28207066, "e25502612fed533a243b61f23a2a574daad030e7e88906222a5f0ab30d06769e"},
	"linux/arm64":   {"duckdb-shared-libs-linux-arm64.tar.gz", 26068127, "811e8b4d0226f5e531f583aadc798070ffb33b350e3823be563a2ebeb2c68593"},
	"darwin/amd64":  {"duckdb-shared-libs-osx-amd64.tar.gz", 21632950, "e12f174ef01575590ec6e27a5d475308680a9e15292691623d9848c0fc5a9ba8"},
	"darwin/arm64":  {"duckdb-shared-libs-osx-arm64.tar.gz", 19317344, "d71e4800350f1135bc3f80a2e5cbcc01e199ca03759edf5a376db308b514b341"},
	"windows/amd64": {"duckdb-shared-libs-windows-amd64.tar.gz", 20136584, "d51f23831423b82418ee1d5bae324484f9fbe4a2c2424fa5b6be3392453c62ce"},
	"windows/arm64": {"duckdb-shared-libs-windows-arm64.tar.gz", 18730993, "8b5a2b23f78f96bcf8b22204629f02f6b49e08383717338c6b5f951874d1f09e"},
}

// releaseURL is where the archives are; a var for the tests' server.
var releaseURL = "https://github.com/duckdb/duckdb/releases/download/v" + PinnedVersion + "/"

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

// downloadPath is where a download puts the library, under data directory data.
func downloadPath(goos, data string) string {
	return filepath.Join(data, "duckdb", PinnedVersion, libName(goos))
}

// NotFound is no libduckdb to use where it is looked for, with none
// configured: none there, or one too old, Old its version. Into is where a
// download would put one, "" with no data directory.
type NotFound struct {
	Tried     []string
	Old, Into string
}

func (e *NotFound) Error() string {
	if e.Old != "" {
		return i18n.T("libduckdb 至少要 v%s，这个是 %s。", minVersion, e.Old) + Manual()
	}
	return i18n.T("找不到 libduckdb（找过：%s）。", strings.Join(e.Tried, ", ")) + Manual()
}

// Manual is how to put a library there oneself (§8.1).
func Manual() string {
	if runtime.GOOS == "windows" {
		return i18n.T("可以从 %s 下载 duckdb.dll，在 config.toml 的 [duckdb] library 写上它的路径", "https://github.com/duckdb/duckdb/releases")
	}
	return i18n.T("可以 brew install duckdb，或从 %s 下载 libduckdb，在 config.toml 的 [duckdb] library 写上它的路径", "https://github.com/duckdb/duckdb/releases")
}

// Download is the library a download gives: what the question before it says
// (Old the version of the one too old, "" for none found), and what Fetch takes.
type Download struct {
	URL, Path, Old string
	Size           int64 // of the archive
	sum            string
}

// Offer is the download that gives a library in place of err's none; false
// when err is no NotFound, there is no data directory to put it in, or no
// archive for this platform (NotFound says the manual way).
func Offer(err error) (Download, bool) {
	var nf *NotFound
	a, ok := archives[runtime.GOOS+"/"+runtime.GOARCH]
	if !errors.As(err, &nf) || nf.Into == "" || !ok {
		return Download{}, false
	}
	return Download{URL: releaseURL + a.name, Path: nf.Into, Old: nf.Old, Size: a.size, sum: a.sha256}, true
}

// counter counts the bytes written through it.
type counter struct{ n *atomic.Int64 }

func (c counter) Write(p []byte) (int, error) { c.n.Add(int64(len(p))); return len(p), nil }

// Fetch downloads d's archive to a file beside d.Path, checks its sha256, and
// puts the library in it at d.Path; got counts the bytes downloaded. Nothing
// of it is left when it fails, ctx ending too.
func Fetch(ctx context.Context, d Download, got *atomic.Int64) error {
	dir := filepath.Dir(d.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// what a download left when sqlmux quit halfway through it
	// shortcut: one another sqlmux is making at the same time goes too, and that one fails
	for _, pattern := range []string{"download-*", filepath.Base(d.Path) + ".*"} {
		left, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, p := range left {
			_ = os.Remove(p)
		}
	}
	tmp, err := os.CreateTemp(dir, "download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // after the Close below: windows removes no open file
	defer tmp.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return err
	}
	// shortcut: no timeout, a stalled connection stays at its MB till C-c; a read deadline if that is seen
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", d.URL, resp.Status)
	}
	h := sha256.New()
	// no more than the archive is: a longer one is not the table's, and its end is not waited for
	if _, err := io.Copy(io.MultiWriter(tmp, h, counter{got}), io.LimitReader(resp.Body, d.Size)); err != nil {
		return err
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != d.sum {
		return fmt.Errorf("%s", i18n.T("下载的 %s 校验不对（sha256 是 %s，应为 %s）", d.URL, sum, d.sum))
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return unpack(tmp, d.Path)
}

// unpack puts the file of the tar.gz r named as the end of to at to, by way of
// a file beside it renamed: no half of a library is ever there to be loaded.
func unpack(r io.Reader, to string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	name := filepath.Base(to)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s", i18n.T("下载的包里没有 %s", name))
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == name {
			break
		}
	}
	f, err := os.CreateTemp(filepath.Dir(to), name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // renamed when it is whole, and then there is nothing to remove
	_, err = io.Copy(f, tr)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), to)
}
