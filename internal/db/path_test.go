package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	for spec, want := range map[string][2]string{
		"~/x.db?mode=ro": {filepath.Join(home, "x.db"), "mode=ro"},
		"~":              {home, ""},
		"~other/x.db":    {filepath.Join(wd, "~other/x.db"), ""},
		"rel/x.db?a=b":   {filepath.Join(wd, "rel/x.db"), "a=b"},
	} {
		path, query, err := FilePath(spec)
		if err != nil || path != want[0] || query != want[1] {
			t.Errorf("FilePath(%q) = %q, %q, %v; want %q, %q", spec, path, query, err, want[0], want[1])
		}
	}
}
