//go:build !windows && (!linux || duckdb)

package duckdb

import "github.com/ebitengine/purego"

// openLib loads the library with its symbols kept to itself: a too-old one
// closed stays mapped, and global symbols of it would be what the one loaded
// next calls into, and crash (v1.4.4, then v2.0.1, tried on linux; DuckDB's
// extensions, httpfs and spatial, load as well).
func openLib(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
}

func closeLib(h uintptr) { _ = purego.Dlclose(h) }
