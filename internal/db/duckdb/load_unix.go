//go:build !windows

package duckdb

import "github.com/ebitengine/purego"

func openLib(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
