//go:build windows

package duckdb

import "syscall"

func openLib(path string) (uintptr, error) {
	h, err := syscall.LoadLibrary(path)
	return uintptr(h), err
}
