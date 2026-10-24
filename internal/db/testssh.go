package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// FakeSSH puts an `ssh` on the PATH of the test that runs script, which is
// given the arguments the program gave ssh; they are written to the file that
// is returned (F15.2). The script is a /bin/sh one: no windows.
func FakeSSH(t *testing.T, script string) (args string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a /bin/sh script")
	}
	dir := t.TempDir()
	args = filepath.Join(dir, "args")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >" + args + "\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return args
}

// NCSSH is a FakeSSH that does what `ssh -W host:port` does for a server on
// this machine: its stdin and stdout go to the same port of localhost, whatever
// the host, which may be a name only the other end of a real ssh knows; the
// test skips with no nc.
func NCSSH(t *testing.T) (args string) {
	t.Helper()
	if _, err := exec.LookPath("nc"); err != nil {
		t.Skip("no nc")
	}
	return FakeSSH(t, `exec nc localhost "${2##*:}"`)
}
