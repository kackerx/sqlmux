package config

import (
	"runtime"
	"testing"
)

// On windows the directories without XDG_* are %APPDATA%\sqlmux for the
// config and %LOCALAPPDATA%\sqlmux for data and state (§14); XDG_* set wins
// as anywhere, which every other test of the directories covers.
func TestDirsWindowsDefault(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the windows defaults")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("APPDATA", `C:\users\u\AppData\Roaming`)
	t.Setenv("LOCALAPPDATA", `C:\users\u\AppData\Local`)
	if got := Dir(); got != `C:\users\u\AppData\Roaming\sqlmux` {
		t.Errorf("Dir() = %q", got)
	}
	if got := DataDir(); got != `C:\users\u\AppData\Local\sqlmux` {
		t.Errorf("DataDir() = %q", got)
	}
	if got := StateDir(); got != `C:\users\u\AppData\Local\sqlmux` {
		t.Errorf("StateDir() = %q", got)
	}
}
