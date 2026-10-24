package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const editFile = `# my databases

# the one for play
[[connection]]
name = "play"        # not a seed
engine = "postgres"
dsn = "postgres://localhost/play?sslmode=disable"

# staging, read only
[[connection]]
name = "stage"
engine = "mysql"
dsn = "u:p@tcp(h:3306)/stage"
read_only = true

[[connection]]
name = "last"
engine = "postgres"
dsn = "postgres://localhost/last"`

const oneConn = "[[connection]]\nname = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\n"

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func connA(dsn string, readOnly bool) Connection {
	return Connection{Name: "a", Engine: "postgres", DSN: dsn, ReadOnly: readOnly}
}

// An edit rewrites the lines of its block that changed, adds a new key after
// the block's last, takes a cleared one out, and leaves every other byte of
// the file and its permissions as they were; what comes back is the block as
// the file says it now (F8.16).
func TestUpdateConnection(t *testing.T) {
	path := connectionsFile(t, editFile)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	was := Connection{Name: "stage", Engine: "mysql", DSN: "u:p@tcp(h:3306)/stage", ReadOnly: true}
	c := was
	c.DSN = "u:p@tcp(other:3306)/stage"
	got, err := UpdateConnection("stage", was, c)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("a dsn: %+v %v", got, err)
	}
	if got, want := readFile(t, path), strings.Replace(editFile, "tcp(h:3306)", "tcp(other:3306)", 1); got != want {
		t.Errorf("a dsn:\n%s", got)
	}
	was = c
	c.ReadOnly, c.PasswordCmd, c.Name = false, "pass show stage", "staging"
	if _, err := UpdateConnection("stage", was, c); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editFile, "tcp(h:3306)", "tcp(other:3306)", 1)
	want = strings.Replace(want, `name = "stage"`, `name = "staging"`, 1)
	want = strings.Replace(want, "read_only = true\n", `password_cmd = "pass show stage"`+"\n", 1)
	if got := readFile(t, path); got != want {
		t.Errorf("a rename, a key added after the last, one cleared:\n%s", got)
	}
	last := Connection{Name: "last", Engine: "postgres", DSN: "postgres://localhost/last"}
	withRO := last
	withRO.ReadOnly = true
	if _, err := UpdateConnection("last", last, withRO); err != nil { // the last line has no break
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.HasSuffix(got, "dsn = \"postgres://localhost/last\"\nread_only = true\n") || !strings.HasPrefix(got, "# my databases\n\n# the one for play\n") {
		t.Errorf("the last block:\n%s", got)
	}
	if fi, err := os.Stat(path); err != nil || (fi.Mode().Perm() != 0o644 && runtime.GOOS != "windows") { // windows has no group/other bits to strip
		t.Errorf("permissions: %v %v", fi.Mode(), err)
	}
	if _, err := UpdateConnection("nobody", was, c); err == nil {
		t.Error("a connection that is not there")
	}
}

// Only what the caller changed is written, in the file as it is now: a key
// the user changed in an editor meanwhile stays, one they added too (F8.16).
func TestUpdateConnectionKeepsEditsMadeMeanwhile(t *testing.T) {
	path := connectionsFile(t, oneConn+"read_only = true\n")
	was := connA("postgres://h/a", true)
	disk := oneConn + "password_env = \"PW\"\nread_only = true\n"
	disk = strings.Replace(disk, "h/a", "h/edited", 1)
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	c := was
	c.ReadOnly = false
	got, err := UpdateConnection("a", was, c)
	if want := (Connection{Name: "a", Engine: "postgres", DSN: "postgres://h/edited", PasswordEnv: "PW"}); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("read back %+v %v, want %+v", got, err, want)
	}
	if got, want := readFile(t, path), strings.Replace(disk, "read_only = true\n", "", 1); got != want {
		t.Errorf("file:\n%s", got)
	}
}

// What the edit would write is read back and compared with what was asked:
// a key written in a way the lines cannot take (in quotes, in a value of
// several lines) is refused, the file untouched, while the ways that only
// look odd (dotted keys, inline tables, arrays, values of several lines
// left alone) are edited as any (F8.16).
func TestUpdateConnectionReadsBack(t *testing.T) {
	multi := "[[connection]]\nname = \"a\"\nengine = \"postgres\"\ndsn = \"\"\"\npostgres://h/a\"\"\"\n"
	for _, c := range []struct {
		label, body string
		to          Connection
		odd         bool
	}{
		{"no spaces", "[[connection]]\nname=\"a\"\nengine=\"postgres\"\ndsn=\"postgres://h/a\"\n", connA("postgres://h/b", false), false},
		{"wide spaces", "[[connection]]\nname   =   \"a\"\nengine = \"postgres\"\ndsn   =   \"postgres://h/a\"\n", connA("postgres://h/b", false), false},
		{"single quotes", "[[connection]]\nname = 'a'\nengine = 'postgres'\ndsn = 'postgres://h/a'\n", connA("postgres://h/b", false), false},
		{"a # in a password", oneConn + "password = \"a#b\"\n", Connection{Name: "a", Engine: "postgres", DSN: "postgres://h/b", Password: "a#b"}, false},
		{"a dotted key beside", oneConn + "options.sslmode = \"require\"\n", connA("postgres://h/b", false), false},
		{"an inline table beside", oneConn + "opts = { a = 1 }\n", connA("postgres://h/b", false), false},
		{"an array beside", oneConn + "m = [\n  [1, 2],\n]\n", connA("postgres://h/b", false), false},
		{"a key after a value of lines", multi, connA("postgres://h/a", true), false},
		{"a key after a string of lines", oneConn + "note = '''\nline one\n'''\n", connA("postgres://h/a", true), false},
		{"a key after an array of lines", oneConn + "tags = [\n  \"x\",\n]\n", connA("postgres://h/a", true), false},
		{"a quoted key cleared", oneConn + "\"read_only\" = true\n", connA("postgres://h/a", false), true},
		{"a quoted key changed", "[[connection]]\nname = \"a\"\nengine = \"postgres\"\n\"dsn\" = \"postgres://h/a\"\n", connA("postgres://h/b", false), true},
		{"a quoted name changed", "[[connection]]\n\"name\" = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\n", Connection{Name: "z", Engine: "postgres", DSN: "postgres://h/a"}, true},
		{"a value of lines changed", multi, connA("postgres://h/b", false), true},
	} {
		path := connectionsFile(t, c.body)
		was := connA("postgres://h/a", strings.Contains(c.body, "read_only"))
		got, err := UpdateConnection("a", was, c.to)
		switch {
		case c.odd && (err == nil || err.Error() != errOdd().Error()):
			t.Errorf("%s: %v", c.label, err)
		case c.odd && readFile(t, path) != c.body:
			t.Errorf("%s: the file changed:\n%s", c.label, readFile(t, path))
		case !c.odd && err != nil:
			t.Errorf("%s: %v", c.label, err)
		case !c.odd && !reflect.DeepEqual(got, c.to):
			t.Errorf("%s: read back %+v, want %+v", c.label, got, c.to)
		}
	}
}

// Nothing is written when the edit gives a file that no longer reads, a name
// that is taken or an engine that is none; CheckUpdate says so without
// writing it (F8.16).
func TestCheckUpdate(t *testing.T) {
	body := oneConn + "\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"
	path := connectionsFile(t, body)
	was := connA("postgres://h/a", false)
	for label, c := range map[string]Connection{
		"an engine that is none": {Name: "a", Engine: "oracle", DSN: "x"},
		"a name taken":           {Name: "b", Engine: "postgres", DSN: "postgres://h/a"},
		"a name with a /":        {Name: "x/y", Engine: "postgres", DSN: "postgres://h/a"},
	} {
		if err := CheckUpdate("a", was, c); err == nil {
			t.Errorf("%s: no error", label)
		}
		if _, err := UpdateConnection("a", was, c); err == nil || readFile(t, path) != body {
			t.Errorf("%s: %v, written %v", label, err, readFile(t, path) != body)
		}
	}
	if err := CheckUpdate("a", was, connA("postgres://h/other", true)); err != nil || readFile(t, path) != body {
		t.Errorf("a good edit: %v, written %v", err, readFile(t, path) != body)
	}
}

// The file's own ways: CRLF breaks, tabs, a last line with no break, a block
// right below another, and the [[ connection ]] spelled with spaces
// (F8.16).
func TestUpdateConnectionFileForms(t *testing.T) {
	two := oneConn + "\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"
	for _, c := range []struct{ label, body, want string }{
		{"CRLF", strings.ReplaceAll("# top\n"+two, "\n", "\r\n"), strings.ReplaceAll("# top\n"+strings.Replace(oneConn, "h/a", "h/b", 1)+"read_only = true\n"+strings.TrimPrefix(two, oneConn), "\n", "\r\n")},
		{"tabs", "[[connection]]\n\tname = \"a\"\n\tengine = \"postgres\"\n\tdsn = \"postgres://h/a\"\n", "[[connection]]\n\tname = \"a\"\n\tengine = \"postgres\"\n\tdsn = \"postgres://h/b\"\n\tread_only = true\n"},
		{"no break at the end", strings.TrimSuffix(oneConn, "\n"), strings.Replace(oneConn, "h/a", "h/b", 1)[:len(oneConn)-1] + "\nread_only = true\n"},
		{"no blank between blocks", strings.Replace(two, "\n\n[[", "\n[[", 1), strings.Replace(strings.Replace(two, "\n\n[[", "\n[[", 1), "dsn = \"postgres://h/a\"\n", "dsn = \"postgres://h/b\"\nread_only = true\n", 1)},
		{"spaced header", "[[ connection ]]  # first\nname = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\n", "[[ connection ]]  # first\nname = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/b\"\nread_only = true\n"},
		{"[defaults] before", "[defaults]\ntimeout = 5\n\n" + oneConn, "[defaults]\ntimeout = 5\n\n" + strings.Replace(oneConn, "h/a", "h/b", 1) + "read_only = true\n"},
	} {
		path := connectionsFile(t, c.body)
		if c.label == "no break at the end" { // the dsn is the last line, rewritten with no break either
			if _, err := UpdateConnection("a", connA("postgres://h/a", false), connA("postgres://h/b", false)); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, path); got != strings.Replace(c.body, "h/a", "h/b", 1) {
				t.Errorf("rewritten, still no break: %q", got)
			}
			path = connectionsFile(t, c.body)
		}
		if _, err := UpdateConnection("a", connA("postgres://h/a", false), connA("postgres://h/b", true)); err != nil {
			t.Errorf("%s: %v", c.label, err)
		} else if got := readFile(t, path); got != c.want {
			t.Errorf("%s:\n%q\nwant\n%q", c.label, got, c.want)
		}
	}
}

// A block ends at its last key and the blank lines after it: a comment on a
// paragraph of its own below is the user's, whichever block it follows, and
// the comment lines glued to the next header are that block's (F8.16).
func TestConnectionBlockEnds(t *testing.T) {
	two := oneConn + "\n# ---- legacy below ----\n\n# old b\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"
	path := connectionsFile(t, two)
	if err := DeleteConnection("a"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, path), "# ---- legacy below ----\n\n# old b\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"; got != want {
		t.Errorf("delete a:\n%s", got)
	}
	connectionsFile(t, two)
	if err := DeleteConnection("b"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, connectionsPath()), oneConn+"\n# ---- legacy below ----\n\n"; got != want {
		t.Errorf("delete b, the comment above it kept with it:\n%q", got)
	}
	connectionsFile(t, two)
	if _, err := UpdateConnection("a", connA("postgres://h/a", false), connA("postgres://h/a", true)); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, connectionsPath()), strings.Replace(two, "dsn = \"postgres://h/a\"\n", "dsn = \"postgres://h/a\"\nread_only = true\n", 1); got != want {
		t.Errorf("a key added before the paragraph:\n%s", got)
	}
	commented := "# keep\n\n[[connection]]\nname = \"z\"\nengine = \"postgres\"\ndsn = \"postgres://h/z\"\n\n" + oneConn + "\n# prod, off for now\n# [[connection]]\n# name = \"prod\"\n"
	connectionsFile(t, commented)
	if err := DeleteConnection("a"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, connectionsPath()), strings.Replace(commented, oneConn+"\n", "", 1); got != want {
		t.Errorf("a commented-out block at the end:\n%s", got)
	}
	defaults := two + "\n# my defaults\n[defaults]\ntimeout = 5\n"
	connectionsFile(t, defaults)
	if err := DeleteConnection("b"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, connectionsPath()); !strings.HasSuffix(got, "\n# my defaults\n[defaults]\ntimeout = 5\n") || strings.Contains(got, "old b") {
		t.Errorf("[defaults] after:\n%s", got)
	}
}

// A deletion takes the block, the comment lines right above it and the
// blank lines after it; nothing else (F8.16).
func TestDeleteConnection(t *testing.T) {
	path := connectionsFile(t, editFile)
	if err := DeleteConnection("stage"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editFile, "# staging, read only\n[[connection]]\nname = \"stage\"\nengine = \"mysql\"\ndsn = \"u:p@tcp(h:3306)/stage\"\nread_only = true\n\n", "", 1)
	if got := readFile(t, path); got != want {
		t.Errorf("the middle one:\n%s", got)
	}
	if err := DeleteConnection("last"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.HasSuffix(got, "dsn = \"postgres://localhost/play?sslmode=disable\"\n\n") {
		t.Errorf("the last one:\n%q", got)
	}
	if err := DeleteConnection("play"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Contains(got, "[[connection]]") || !strings.HasPrefix(got, "# my databases\n") {
		t.Errorf("the only one left, then none:\n%q", got)
	}
	if err := DeleteConnection("play"); err == nil {
		t.Error("deleted twice")
	}
	crlf := strings.ReplaceAll("# top\n\n"+oneConn+"\n"+strings.Replace(oneConn, `"a"`, `"b"`, 1), "\n", "\r\n")
	connectionsFile(t, crlf)
	if err := DeleteConnection("a"); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, connectionsPath()), strings.ReplaceAll("# top\n\n"+strings.Replace(oneConn, `"a"`, `"b"`, 1), "\n", "\r\n"); got != want {
		t.Errorf("CRLF:\n%q", got)
	}
}

// What is left must read: a block's own sub-table does not survive it, and
// the file is not written (F8.16).
func TestDeleteConnectionRefusesWhatLeavesUnreadable(t *testing.T) {
	body := oneConn + "[connection.opts]\nx = 1\n\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"
	path := connectionsFile(t, body)
	if err := DeleteConnection("a"); err == nil || readFile(t, path) != body {
		t.Errorf("deleted anyway: %v\n%s", err, readFile(t, path))
	}
}

// The file is replaced whole, never cut and written over: a failure on the
// way leaves what the user wrote; its permissions stay, a link stays a link
// with the target written, and one that cannot be written is refused with
// the file as it was (F8.16).
func TestConnectionsWrite(t *testing.T) {
	path := connectionsFile(t, oneConn)
	was, to := connA("postgres://h/a", false), connA("postgres://h/b", false)
	swaps := 0
	beforeRename = func() {
		swaps++
		if got := readFile(t, path); got != oneConn {
			t.Errorf("the file was touched before the swap: %q", got)
		}
	}
	defer func() { beforeRename = func() {} }()
	if _, err := UpdateConnection("a", was, to); err != nil || swaps != 1 {
		t.Fatalf("%v, %d swaps", err, swaps)
	}
	beforeRename = func() {}

	target := filepath.Join(t.TempDir(), "dotfiles.toml")
	if err := os.WriteFile(target, []byte(oneConn), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConnection("a", was, to); err != nil {
		t.Fatal(err)
	}
	if li, err := os.Lstat(path); err != nil || li.Mode()&os.ModeSymlink == 0 {
		t.Errorf("no longer a link: %v %v", li, err)
	}
	if fi, err := os.Stat(target); err != nil || (fi.Mode().Perm() != 0o640 && runtime.GOOS != "windows") || !strings.Contains(readFile(t, target), "h/b") { // windows has no group/other bits to strip
		t.Errorf("the target: %v %v\n%s", fi, err, readFile(t, target))
	}

	path = connectionsFile(t, oneConn)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateConnection("a", was, to); err == nil || readFile(t, path) != oneConn {
		t.Errorf("a read-only file: %v\n%s", err, readFile(t, path))
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConnection("a"); err == nil || readFile(t, path) != oneConn {
		t.Errorf("a read-only file, a delete: %v", err)
	}
}

// The file can be replaced is part of the check before anything is
// stored: a read-only file, or a directory a file cannot be made in, is
// refused by CheckUpdate as UpdateConnection would (F8.16).
func TestCheckUpdateWritable(t *testing.T) {
	was, to := connA("postgres://h/a", false), connA("postgres://h/b", false)
	path := connectionsFile(t, oneConn)
	if err := CheckUpdate("a", was, to); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := CheckUpdate("a", was, to); err == nil {
		t.Error("a read-only file")
	}
	path = connectionsFile(t, oneConn)
	if os.Geteuid() == 0 {
		return // root makes files anywhere
	}
	if runtime.GOOS == "windows" { // the readonly attribute is what blocks there, not a directory's mode
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
		if err := CheckUpdate("a", was, to); err == nil {
			t.Error("a file that cannot be written")
		}
		return
	}
	if err := os.Chmod(filepath.Dir(path), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o700) })
	if err := CheckUpdate("a", was, to); err == nil {
		t.Error("a directory no file can be made in")
	}
}

// A table header is a TOML key, dotted or in quotes, and nothing else on the
// line but a comment: a quoted name with a comma or an = is another table,
// which a deletion leaves; the lines of an array are no header (F8.16).
func TestConnectionBlockEndsAtTables(t *testing.T) {
	for _, rest := range []string{"\n[\"x,y\"]\nk = 1\n\n['a=b'.c]\nk = 2\n", "\n['a=b'.c]\nk = 2\n\n[\"x,y\"]\nk = 1\n", "\n[\"a\\\"b\"]\nk = 1\n"} {
		connectionsFile(t, oneConn+"\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"+rest)
		if err := DeleteConnection("b"); err != nil {
			t.Fatal(err)
		}
		if got, want := readFile(t, connectionsPath()), oneConn+rest; got != want {
			t.Errorf("tables after the block:\n%s", got)
		}
	}
	array := oneConn + "m = [\n  [1, 2],\n  [3, 4]\n]\nn = [\n  [\"a\"], [\"b\"]\n]\n"
	connectionsFile(t, array+"\n[x]\nk = 1\n")
	if _, err := UpdateConnection("a", connA("postgres://h/a", false), connA("postgres://h/a", true)); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, connectionsPath()), array+"read_only = true\n\n[x]\nk = 1\n"; got != want {
		t.Errorf("the array's lines are the block's:\n%s", got)
	}
}

// A value over lines with a # line, a [x] or a [[connection]] in it is no
// block we can find whole: it is said to be written in a way to edit by
// hand, not that the connection is not there (F8.16).
func TestConnectionInAValueOfLines(t *testing.T) {
	head := "[[connection]]\nname = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\n"
	for label, body := range map[string]string{
		"a closing line that starts with #": head + "note = \"\"\"\nabc\n#tail\"\"\"\n",
		"a table":                           head + "note = '''\n[x]\n'''\n",
		"a block":                           head + "note = '''\n[[connection]]\nx\n'''\n",
	} {
		path := connectionsFile(t, body)
		if _, err := ReadConnection("a"); err == nil || err.Error() != errOdd().Error() {
			t.Errorf("%s, read: %v", label, err)
		}
		if _, err := UpdateConnection("a", connA("postgres://h/a", false), connA("postgres://h/a", true)); err == nil || err.Error() != errOdd().Error() {
			t.Errorf("%s, update: %v", label, err)
		}
		if err := DeleteConnection("a"); err == nil || err.Error() != errOdd().Error() || readFile(t, path) != body {
			t.Errorf("%s, delete: %v", label, err)
		}
	}
	connectionsFile(t, oneConn)
	if _, err := ReadConnection("none"); err == nil || err.Error() == errOdd().Error() {
		t.Errorf("a connection not there: %v", err)
	}
}

// A file that does not read as it is says so itself, not that the edit was
// too odd (F8.16).
func TestUpdateConnectionOfAFileThatDoesNotRead(t *testing.T) {
	body := oneConn + "\n[other]\nx = = 1\n"
	path := connectionsFile(t, body)
	_, err := UpdateConnection("a", connA("postgres://h/a", false), connA("postgres://h/a", true))
	if err == nil || err.Error() == errOdd().Error() || !strings.Contains(err.Error(), path) || readFile(t, path) != body {
		t.Errorf("%v", err)
	}
}

// Several keys changed in one save: a cleared one goes and a new one goes
// after the block's last line, with the next block a blank line away or
// right below; two new ones keep the order they are written in (F8.16).
func TestUpdateConnectionClearsAndAdds(t *testing.T) {
	for label, next := range map[string]string{"a blank between": "\n", "no blank": ""} {
		body := oneConn + "password_cmd = \"pass show a\"\n" + next + "[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"
		connectionsFile(t, body)
		was := connA("postgres://h/a", false)
		was.PasswordCmd = "pass show a"
		to := connA("postgres://h/a", true)
		to.Password = "plain"
		if _, err := UpdateConnection("a", was, to); err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		want := oneConn + "password = \"plain\"\nread_only = true\n" + next + "[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n"
		if got := readFile(t, connectionsPath()); got != want {
			t.Errorf("%s:\n%s", label, got)
		}
	}
}

// A group is a line of the block like any other key: added after the block's
// last line, rewritten, and taken out when cleared (F10.6).
func TestUpdateConnectionGroup(t *testing.T) {
	connectionsFile(t, oneConn+"\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h:3306)/b\"\n")
	was := connA("postgres://h/a", false)
	to := was
	to.Group = "prod"
	got, err := UpdateConnection("a", was, to)
	if err != nil || got.Group != "prod" {
		t.Fatalf("added: %+v %v", got, err)
	}
	if want := "[[connection]]\nname = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\ngroup = \"prod\"\n\n[[connection]]\nname = \"b\""; !strings.HasPrefix(readFile(t, connectionsPath()), want) {
		t.Errorf("file:\n%s", readFile(t, connectionsPath()))
	}
	was = to
	to.Group = "dev"
	if _, err := UpdateConnection("a", was, to); err != nil || !strings.Contains(readFile(t, connectionsPath()), "group = \"dev\"\n\n[[connection]]") {
		t.Fatalf("rewritten: %v\n%s", err, readFile(t, connectionsPath()))
	}
	was = to
	to.Group = ""
	if _, err := UpdateConnection("a", was, to); err != nil || strings.Contains(readFile(t, connectionsPath()), "group") {
		t.Fatalf("cleared: %v\n%s", err, readFile(t, connectionsPath()))
	}
}

// A group has the characters of a name; a new connection's is written under
// its name (F10.6).
func TestConnectionGroupCheck(t *testing.T) {
	c := Connection{Name: "a", Group: "prod/eu", Engine: "postgres", DSN: "x"}
	if field, problem := c.Check(nil); field != "group" || !strings.Contains(problem, "group") {
		t.Errorf("a slash: %q %q", field, problem)
	}
	c.Group = ".hidden"
	if field, _ := c.Check(nil); field != "group" {
		t.Errorf("a dot first: %q", field)
	}
	c.Group = "prod eu"
	if field, problem := c.Check(nil); problem != "" {
		t.Errorf("a space is fine: %q %q", field, problem)
	}
	connectionsFile(t, oneConn)
	c.Name, c.DSN = "b", "postgres://h/b"
	if err := AppendConnection(c); err != nil || !strings.Contains(readFile(t, connectionsPath()), "name = \"b\"\ngroup = \"prod eu\"\nengine") {
		t.Errorf("appended: %v\n%s", err, readFile(t, connectionsPath()))
	}
	got, _, err := LoadConnections("b")
	if err != nil || got[1].Group != "prod eu" {
		t.Errorf("read back: %+v %v", got, err)
	}
}

// The checks before a write leave nothing in the directory (F8.16).
func TestChecksLeaveNothing(t *testing.T) {
	connectionsFile(t, oneConn)
	if err := CheckAppend(Connection{Name: "n", Engine: "postgres", DSN: "postgres://h/n"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckUpdate("a", connA("postgres://h/a", false), connA("postgres://h/b", false)); err != nil {
		t.Fatal(err)
	}
	if es, _ := os.ReadDir(Dir()); len(es) != 1 {
		t.Errorf("left in the directory: %v", es)
	}
}

// ssh is a line of the block like the others: read, added after the last,
// rewritten, taken out when cleared, appended with a new connection; and a
// key of connections.toml that is known (F15.2).
func TestConnectionSSH(t *testing.T) {
	connectionsFile(t, oneConn+"ssh = \"me@jump\"\n")
	conns, _, err := LoadConnections("")
	if err != nil || conns[0].SSH != "me@jump" {
		t.Fatalf("read: %+v %v", conns, err)
	}
	if lines := UnknownKeys(&Config{}); len(lines) != 0 {
		t.Fatalf("ssh is known: %v", lines)
	}
	was := connA("postgres://h/a", false)
	was.SSH = "me@jump"
	to := was
	to.SSH = "you@other"
	if _, err := UpdateConnection("a", was, to); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, connectionsPath()); got != oneConn+"ssh = \"you@other\"\n" {
		t.Fatalf("rewritten:\n%s", got)
	}
	was, to.SSH = to, ""
	if _, err := UpdateConnection("a", was, to); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, connectionsPath()); got != oneConn {
		t.Fatalf("cleared:\n%s", got)
	}
	was, to.SSH = to, "me@jump"
	if _, err := UpdateConnection("a", was, to); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, connectionsPath()); got != oneConn+"ssh = \"me@jump\"\n" {
		t.Fatalf("added:\n%s", got)
	}
	connectionsFile(t, "")
	if err := AppendConnection(Connection{Name: "n", Engine: "postgres", DSN: "postgres://h/n", SSH: "me@jump"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, connectionsPath()); !strings.Contains(got, "ssh = \"me@jump\"\n") {
		t.Fatalf("appended:\n%s", got)
	}
}

// A production connection that does not say read_only is read only; read_only =
// false, written, makes it writable; what the form does with the two switches is
// written so that it reads back, and a block of the file is never encoded again
// (F17.9, §13).
func TestProductionConnection(t *testing.T) {
	path := connectionsFile(t, oneConn+"production = true\n\n[[connection]]\nname = \"b\"\nengine = \"mysql\"\ndsn = \"u@tcp(h)/b\"\nproduction = true\nread_only = false\n\n[[connection]]\nname = \"c\"\nengine = \"postgres\"\ndsn = \"postgres://h/c\"\n")
	cs, _, err := LoadConnections("")
	if err != nil || len(cs) != 3 {
		t.Fatalf("%+v %v", cs, err)
	}
	if !cs[0].Production || !cs[0].ReadOnly || !cs[1].Production || cs[1].ReadOnly || cs[2].Production || cs[2].ReadOnly {
		t.Errorf("a: %+v, b: %+v, c: %+v", cs[0], cs[1], cs[2])
	}
	if c, err := ReadConnection("a"); err != nil || !c.ReadOnly {
		t.Errorf("ReadConnection a: %+v %v", c, err)
	}
	// a made writable: read_only = false is written
	was := cs[0]
	c := was
	c.ReadOnly = false
	if got, err := UpdateConnection("a", was, c); err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("a writable: %+v %v", got, err)
	}
	if got := readFile(t, path); !strings.Contains(got, "production = true\nread_only = false\n\n[[connection]]\nname = \"b\"") {
		t.Errorf("a writable:\n%s", got)
	}
	// and read only again: its line goes, the default says it
	was = c
	c.ReadOnly = true
	if got, err := UpdateConnection("a", was, c); err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("a read only: %+v %v", got, err)
	}
	if got := readFile(t, path); strings.Contains(got, "name = \"a\"\nengine = \"postgres\"\ndsn = \"postgres://h/a\"\nproduction = true\nread_only") {
		t.Errorf("a read only:\n%s", got)
	}
	// c is production now, the form's switch for read only still on: read only
	was = cs[2]
	c = was
	c.Production, c.ReadOnly = true, true
	if got, err := UpdateConnection("c", was, c); err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("c made production: %+v %v", got, err)
	}
	// production off again with the box for read only still ticked: that is said
	was = c
	c.Production = false
	if got, err := UpdateConnection("c", was, c); err != nil || !reflect.DeepEqual(got, c) || !got.ReadOnly {
		t.Fatalf("c not production, read only: %+v %v", got, err)
	}
	if got := readFile(t, path); !strings.HasSuffix(got, "dsn = \"postgres://h/c\"\nread_only = true\n") {
		t.Errorf("c:\n%s", got)
	}
	// b: production off, and read_only = false stays what it was said to be
	was = cs[1]
	c = was
	c.Production = false
	if got, err := UpdateConnection("b", was, c); err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("b not production: %+v %v", got, err)
	}
}

// A production connection added, writable, says so, and reads back as it was made.
func TestAppendProductionConnection(t *testing.T) {
	connectionsFile(t, oneConn)
	for _, ro := range []bool{false, true} {
		c := Connection{Name: map[bool]string{false: "w", true: "r"}[ro], Engine: "postgres", DSN: "postgres://h/x", Production: true, ReadOnly: ro}
		if err := AppendConnection(c); err != nil {
			t.Fatal(err)
		}
	}
	cs, _, err := LoadConnections("")
	if err != nil || len(cs) != 3 || cs[1].ReadOnly || !cs[2].ReadOnly || !cs[1].Production || !cs[2].Production {
		t.Errorf("%+v %v", cs, err)
	}
}

// databases and schemas are written as lists in the block, the rest of the file as it
// was, a comment above it included; read back they are what was written; cleared, their
// lines go (F17.13).
func TestUpdateConnectionPicks(t *testing.T) {
	path := connectionsFile(t, editFile)
	was := Connection{Name: "play", Engine: "postgres", DSN: "postgres://localhost/play?sslmode=disable"}
	c := was
	c.Databases, c.Schemas = []string{"play", "other"}, []string{"play.public", "play.sales"}
	got, err := UpdateConnection("play", was, c)
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("add: %+v %v", got, err)
	}
	text := readFile(t, path)
	if !strings.Contains(text, "databases = [\"play\", \"other\"]\n") || !strings.Contains(text, "schemas = [\"play.public\", \"play.sales\"]\n") || !strings.Contains(text, "# the one for play\n[[connection]]\nname = \"play\"        # not a seed\n") || !strings.Contains(text, "# staging, read only\n[[connection]]\nname = \"stage\"") {
		t.Errorf("the file:\n%s", text)
	}
	if r, err := ReadConnection("play"); err != nil || !reflect.DeepEqual(r.Schemas, c.Schemas) || !reflect.DeepEqual(r.Databases, c.Databases) {
		t.Errorf("read back: %+v %v", r, err)
	}
	c2 := c
	c2.Schemas = []string{"play.public"} // one rewritten in place, the other as it was
	if _, err := UpdateConnection("play", c, c2); err != nil || strings.Count(readFile(t, path), "schemas = ") != 1 || !strings.Contains(readFile(t, path), "databases = [\"play\", \"other\"]\n") {
		t.Errorf("change: %v\n%s", err, readFile(t, path))
	}
	empty := c2
	empty.Schemas = []string{} // a list emptied, not nil: the line goes as it does for nil
	if _, err := UpdateConnection("play", c2, empty); err != nil || strings.Count(readFile(t, path), "schemas = ") != 0 {
		t.Errorf("emptied: %v\n%s", err, readFile(t, path))
	}
	if _, err := UpdateConnection("play", empty, was); err != nil || readFile(t, path) != editFile {
		t.Errorf("cleared: %v\n%s", err, readFile(t, path))
	}
}

// databases is for a postgres connection, schemas for it and for MySQL; the others have no
// such level.
func TestPicksCheck(t *testing.T) {
	for _, tc := range []struct {
		c    Connection
		want string
	}{
		{Connection{Name: "a", Engine: "postgres", DSN: "x", Databases: []string{"a"}, Schemas: []string{"a.public"}}, ""},
		{Connection{Name: "a", Engine: "mysql", DSN: "x", Schemas: []string{"a"}}, ""},
		{Connection{Name: "a", Engine: "mysql", DSN: "x", Databases: []string{"a"}}, "databases"},
		{Connection{Name: "a", Engine: "sqlite", DSN: "x", Schemas: []string{"a"}}, "schemas"},
		{Connection{Name: "a", Engine: "redis", DSN: "x", Schemas: []string{"a"}}, "schemas"},
		{Connection{Name: "a", Engine: "exec", Dialect: "postgres", Command: []string{"x", "{sql}"}, Schemas: []string{"a.public"}}, ""},
	} {
		if field, _ := tc.c.Check(nil); field != tc.want {
			t.Errorf("%+v: field %q, want %q", tc.c, field, tc.want)
		}
	}
}
