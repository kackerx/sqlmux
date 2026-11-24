package duckdb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

// Every platform sqlmux is released for has an archive of the Release.
func TestArchives(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			a, ok := archives[goos+"/"+arch]
			if sum, err := hex.DecodeString(a.sha256); !ok || err != nil || len(sum) != sha256.Size || a.size <= 0 || a.name == "" {
				t.Errorf("%s/%s: %+v", goos, arch, a)
			}
		}
	}
}

func TestOffer(t *testing.T) {
	into := downloadPath(runtime.GOOS, "/data")
	d, ok := Offer(fmt.Errorf("connect: %w", &NotFound{Into: into, Old: "v1.4.4"}))
	if _, there := archives[runtime.GOOS+"/"+runtime.GOARCH]; ok != there || ok && (d.Path != into || d.Old != "v1.4.4" || d.URL != releaseURL+archives[runtime.GOOS+"/"+runtime.GOARCH].name) {
		t.Errorf("Offer = %+v, %v", d, ok)
	}
	if _, ok := Offer(&NotFound{}); ok {
		t.Error("no data directory to put it in")
	}
	if _, ok := Offer(errors.New("加载不了")); ok {
		t.Error("a library that is there and does not load is not downloaded again")
	}
}

// targz is a tar.gz of the files.
func targz(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// release is a Release of one archive, body, served; the download of it into
// a data directory of its own.
func release(t *testing.T, body []byte) Download {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	return Download{URL: srv.URL + "/a.tar.gz", Path: downloadPath(runtime.GOOS, t.TempDir()), Size: int64(len(body)), sum: hex.EncodeToString(sum[:])}
}

// left is the files in the directory of the download's library.
func left(t *testing.T, d Download) []string {
	es, err := os.ReadDir(filepath.Dir(d.Path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestFetch(t *testing.T) {
	name := libName(runtime.GOOS)
	d := release(t, targz(t, map[string]string{"duckdb.h": "header", name: "the library"}))
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, half := range []string{partial + "123", partial + "456"} { // of a download sqlmux quit during
		if err := os.WriteFile(filepath.Join(filepath.Dir(d.Path), half), []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(d.Path), name+".bak"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got atomic.Int64
	if err := Fetch(context.Background(), d, &got); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(d.Path); err != nil || string(b) != "the library" || got.Load() != d.Size {
		t.Errorf("%q %v, %d bytes", b, err, got.Load())
	}
	if l := left(t, d); len(l) != 2 { // and the user's own backup
		t.Errorf("left %q", l)
	}
}

// A sha256 that is not the table's puts nothing there.
func TestFetchWrongSum(t *testing.T) {
	d := release(t, targz(t, map[string]string{libName(runtime.GOOS): "not the library"}))
	d.sum = hex.EncodeToString(make([]byte, sha256.Size))
	if err := Fetch(context.Background(), d, new(atomic.Int64)); err == nil {
		t.Fatal("no error")
	}
	if l := left(t, d); len(l) != 0 {
		t.Errorf("left %q", l)
	}
}

func TestFetchNoLibrary(t *testing.T) {
	d := release(t, targz(t, map[string]string{"duckdb.h": "header"}))
	if err := Fetch(context.Background(), d, new(atomic.Int64)); err == nil {
		t.Fatal("no error")
	}
	if l := left(t, d); len(l) != 0 {
		t.Errorf("left %q", l)
	}
}

// A download cancelled halfway leaves nothing of it.
func TestFetchCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 1000))
		w.(http.Flusher).Flush()
		cancel() // the user's C-c, with half of it here
		<-r.Context().Done()
	}))
	defer srv.Close()
	d := Download{URL: srv.URL, Path: downloadPath(runtime.GOOS, t.TempDir()), Size: 2000, sum: "x"}
	if err := Fetch(ctx, d, new(atomic.Int64)); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if l := left(t, d); len(l) != 0 {
		t.Errorf("left %q", l)
	}
}
