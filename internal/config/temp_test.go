package config

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestTempConnection(t *testing.T) {
	targets := map[string]Connection{
		"./x.db":                          {Name: "x", Engine: "sqlite", DSN: "./x.db", Temp: true},
		"/tmp/data/my.app.sqlite?mode=ro": {Name: "my.app", Engine: "sqlite", DSN: "/tmp/data/my.app.sqlite?mode=ro", Temp: true},
		"~/notes":                         {Name: "notes", Engine: "sqlite", DSN: "~/notes", Temp: true},
		"postgres://u:pw@db.example:5432/shop?x=1": {Name: "shop@db.example", Engine: "postgres", DSN: "postgres://u:pw@db.example:5432/shop?x=1", Temp: true},
		"postgresql://localhost/app":               {Name: "app@localhost", Engine: "postgres", DSN: "postgresql://localhost/app", Temp: true},
		"mysql://root@127.0.0.1:3306/":             {Name: "127.0.0.1", Engine: "mysql", DSN: "mysql://root@127.0.0.1:3306/", Temp: true},
		"mysql:///tmp/sock":                        {Name: "mysql", Engine: "mysql", DSN: "mysql:///tmp/sock", Temp: true},
		`D:/x.db`:                                  {Name: "x", Engine: "sqlite", DSN: `D:/x.db`, Temp: true}, // a windows drive, both separators taken
	}
	if runtime.GOOS == "windows" { // unix's filepath.Base does not cut at a backslash
		targets[`C:\data\a.db`] = Connection{Name: "a", Engine: "sqlite", DSN: `C:\data\a.db`, Temp: true}
	}
	for target, want := range targets {
		got, err := TempConnection(target, nil)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v %v, want %+v", target, got, err, want)
		}
		if f, p := got.Check(nil); p != "" {
			t.Errorf("%s: %s %s", target, f, p)
		}
	}
	if _, err := TempConnection("oracle://x/y", nil); err == nil {
		t.Error("an engine there is not")
	}
	// the password in a URL that does not parse is not in the error
	if _, err := TempConnection("postgres://u:secret@h:99999999999999/db%zz", nil); err != nil && strings.Contains(err.Error(), "secret") {
		t.Errorf("the error says the password: %v", err)
	}
}

func TestIsTarget(t *testing.T) {
	known := []string{"prod", "prod.eu"}
	for arg, want := range map[string]bool{"prod": false, "prod.eu": false, "x.db": true, "./prod": true, "a/b": true, "postgres://h/d": true, "other": false} {
		if got := IsTarget(arg, known); got != want {
			t.Errorf("%q: %v", arg, got)
		}
	}
}

// The name of a temporary connection is the file's without its extension, or
// <database>@<host>; one a connection has keeps the extension, then gets a
// number; one that is no connection's name, or none, is the engine's (F14.1).
func TestTempConnectionNames(t *testing.T) {
	for _, tc := range []struct {
		target string
		taken  []string
		want   string
	}{
		{"./app.db", []string{"app"}, "app.db"},
		{"./app.db", []string{"app", "app.db"}, "app.db-2"},
		{"./app.db", []string{"app", "app.db", "app.db-2"}, "app.db-3"},
		{"/a/.db", nil, "sqlite"},
		{"/a/.db", []string{"sqlite"}, "sqlite-2"},
		{".", nil, "sqlite"},
		{"/", nil, "sqlite"},
		{"postgres://h/a\\b", nil, "postgres"}, // a backslash is no name's
		{"mysql://", nil, "mysql"},
		{"postgres://h/d", []string{"d@h"}, "d@h-2"},
		{"./.hidden", nil, "sqlite"}, // no name starts with a dot
	} {
		got, err := TempConnection(tc.target, tc.taken)
		if err != nil || got.Name != tc.want {
			t.Errorf("%s %v: %q %v, want %q", tc.target, tc.taken, got.Name, err, tc.want)
		}
		if f, p := got.Check(nil); p != "" {
			t.Errorf("%s: %s %s", tc.target, f, p)
		}
	}
}
