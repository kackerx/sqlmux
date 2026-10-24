//go:build !windows

package config

import (
	"os"
	"syscall"
)

// flockExclusive is the advisory lock lockFile takes: one writer of
// state.json at a time, held until funlockExclusive or the file closes.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func funlockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
