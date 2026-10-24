package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hostPath is the PATH the tests were started with, where the few tools a
// fake keychain tool needs come from.
var hostPath = os.Getenv("PATH")

// isolated makes PATH a directory of its own with the named tools linked in
// and returns it: /usr/bin/security is not on it, so no test can reach the
// real keychain, whatever hostOS says. A tool the machine lacks skips the
// test. hostOS is put back when the test ends.
func isolated(t *testing.T, tools ...string) (bin string) {
	t.Helper()
	old := hostOS
	t.Cleanup(func() { hostOS = old })
	bin = t.TempDir()
	for _, tool := range tools {
		var path string
		for _, dir := range filepath.SplitList(hostPath) {
			if p := filepath.Join(dir, tool); fileExists(p) {
				path = p
				break
			}
		}
		if path == "" {
			t.Skipf("no %s here", tool)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return bin
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// fakeTool is a script called name in bin; it sees the directory it keeps
// its notes in as $FAKE.
func fakeTool(t *testing.T, bin, name, script string) {
	t.Helper()
	notes := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE", notes)
}

// The fake security keeps a password under its -s and answers find with it,
// in hex when a byte is not printable, as the real one does; the fake
// secret-tool keeps it under the last argument of the attributes.
const (
	fakeSecurity = `cmd=$1; svc=; pw=
while [ $# -gt 0 ]; do case $1 in -s) svc=$2 ;; -w) pw=$2 ;; esac; shift; done
case $cmd in
add-generic-password) printf %s "$pw" >"$FAKE/svc-$svc" ;;
delete-generic-password) [ -f "$FAKE/svc-$svc" ] || exit 44; rm -f "$FAKE/svc-$svc" ;;
find-generic-password)
	[ -f "$FAKE/svc-$svc" ] || exit 44
	if LC_ALL=C grep -q '[^ -~]' "$FAKE/svc-$svc"; then od -An -tx1 "$FAKE/svc-$svc" | tr -d ' \n'; else cat "$FAKE/svc-$svc"; fi ;;
esac
`
	fakeSecretTool = `cmd=$1; for a; do last=$a; done
case $cmd in
store) cat >"$FAKE/svc-$last" ;;
clear) rm -f "$FAKE/svc-$last" ;;
lookup) [ -f "$FAKE/svc-$last" ] && cat "$FAKE/svc-$last" ;;
esac
`
)

// The password goes in with the tool of the system, and the password_cmd
// that comes back reads it out as it was, a name with a quote and a space
// and a password that is not ASCII included (§13): the real security would
// write that in hex, so the command ends in xxd.
func TestStorePassword(t *testing.T) {
	const name = "my 'db'"
	for _, c := range []struct{ os, tool, script string }{{"darwin", "security", fakeSecurity}, {"linux", "secret-tool", fakeSecretTool}} {
		for _, pw := range []string{`p@ss w'rd $x`, "密码123", "pässwörd", "tab\there"} {
			bin := isolated(t, "sh", "cat", "grep", "od", "tr", "xxd")
			hostOS = c.os
			fakeTool(t, bin, c.tool, c.script)
			cmd, stored, err := StorePassword(name, pw)
			if err != nil || !stored {
				t.Fatalf("%s %q: stored %v, err %v", c.os, pw, stored, err)
			}
			if want := c.os == "darwin" && !printable(pw); strings.Contains(cmd, "xxd") != want {
				t.Errorf("%s %q: %q", c.os, pw, cmd)
			}
			if got, err := (Connection{PasswordCmd: cmd}).Secret(); err != nil || got != pw {
				t.Errorf("%s: %q reads %q, %v", c.os, cmd, got, err)
			}
		}
	}
}

// Another name is another entry: the one stored is not read back by a
// connection of a name that was not.
func TestStorePasswordByName(t *testing.T) {
	bin := isolated(t, "sh", "cat", "grep", "od", "tr")
	hostOS = "darwin"
	fakeTool(t, bin, "security", fakeSecurity)
	cmd, _, err := StorePassword("a", "one")
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := StorePassword("b", "two")
	if got, _ := (Connection{PasswordCmd: cmd}).Secret(); got != "one" {
		t.Errorf("a: %q", got)
	}
	if got, _ := (Connection{PasswordCmd: other}).Secret(); got != "two" {
		t.Errorf("b: %q", got)
	}
}

// No tool is no keychain, not an error; one that fails is the error, its
// first line of complaint said.
func TestStorePasswordNoToolOrFailing(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		isolated(t)
		hostOS = goos
		if cmd, stored, err := StorePassword("x", "pw"); cmd != "" || stored || err != nil {
			t.Errorf("%s with no tool: %q %v %v", goos, cmd, stored, err)
		}
	}
	if runtime.GOOS == "windows" { // the fake tool is a /bin/sh script; the keychain is not windows' (§14)
		return
	}
	bin := isolated(t)
	hostOS = "darwin"
	fakeTool(t, bin, "security", "echo 'keychain is locked' >&2\necho second >&2\nexit 1\n")
	if _, stored, err := StorePassword("x", "pw"); stored || err == nil || err.Error() != "security: keychain is locked" {
		t.Errorf("failing: %v %v", stored, err)
	}
}

// ${NAME} in dsn and password is the variable's value; $NAME is not
// touched; one not set, or empty, names itself (§13).
func TestExpanded(t *testing.T) {
	t.Setenv("PGURL", "postgres://u@h/db")
	t.Setenv("PGPW", "s3cret")
	t.Setenv("EMPTY", "")
	c, err := Connection{Name: "a", DSN: "${PGURL}?x=${PGPW}", Password: "pre${PGPW}$PGPW$"}.Expanded()
	if err != nil || c.DSN != "postgres://u@h/db?x=s3cret" || c.Password != "pres3cret$PGPW$" {
		t.Errorf("expanded: %+v %v", c, err)
	}
	for _, in := range []Connection{{DSN: "${NOT_SET_ANYWHERE}"}, {DSN: "x", Password: "${EMPTY}"}} {
		if _, err := in.Expanded(); err == nil || !strings.Contains(err.Error(), "${") || !strings.Contains(err.Error(), "这个环境变量没有设置") {
			t.Errorf("%+v: %v", in, err)
		}
	}
	if _, err := (Connection{DSN: "${NOT_SET_ANYWHERE}"}).Expanded(); err == nil || !strings.HasPrefix(err.Error(), "${NOT_SET_ANYWHERE}") {
		t.Errorf("names the variable: %v", err)
	}
	for _, src := range []Connection{{PasswordCmd: "x"}, {PasswordEnv: "X"}} { // password is not the source: left alone
		src.DSN, src.Password = "x", "${NOT_SET_ANYWHERE}"
		if c, err := src.Expanded(); err != nil || c.Password != "${NOT_SET_ANYWHERE}" {
			t.Errorf("%+v: %q %v", src, c.Password, err)
		}
	}
	if c, err := (Connection{DSN: "x${}y"}).Expanded(); err != nil || c.DSN != "x${}y" { // no name: not a reference
		t.Errorf("an empty ${}: %+v %v", c, err)
	}
	if c, err := (Connection{DSN: "postgres://u:p$w@h/db"}).Expanded(); err != nil || c.DSN != "postgres://u:p$w@h/db" {
		t.Errorf("a $ alone: %+v %v", c, err)
	}
}

// What StorePassword wrote is taken out again by the command alone, a name
// with a quote included; a command of the user's own, and a keychain with no
// tool, are left alone (F8.16).
func TestDeletePassword(t *testing.T) {
	for _, c := range []struct{ os, tool, script string }{{"darwin", "security", fakeSecurity}, {"linux", "secret-tool", fakeSecretTool}} {
		bin := isolated(t, "sh", "cat", "grep", "od", "tr", "xxd", "rm")
		hostOS = c.os
		fakeTool(t, bin, c.tool, c.script)
		cmd, _, err := StorePassword("my 'db'", "pw")
		if err != nil {
			t.Fatal(err)
		}
		other, _, _ := StorePassword("other", "pw2")
		if got, _ := (Connection{PasswordCmd: cmd}).Secret(); got != "pw" {
			t.Fatalf("%s: stored %q", c.os, got)
		}
		if err := DeletePassword(cmd); err != nil {
			t.Fatalf("%s: %v", c.os, err)
		}
		if got, _ := (Connection{PasswordCmd: cmd}).Secret(); got != "" {
			t.Errorf("%s: still there: %q", c.os, got)
		}
		if got, _ := (Connection{PasswordCmd: other}).Secret(); got != "pw2" {
			t.Errorf("%s: another's went too: %q", c.os, got)
		}
		if err := DeletePassword("pass show db"); err != nil {
			t.Errorf("%s: a command of the user's: %v", c.os, err)
		}
	}
	isolated(t, "sh")
	if err := DeletePassword("security find-generic-password -a sqlmux -s 'sqlmux-a' -w"); err != nil {
		t.Errorf("no tool: %v", err)
	}
}

// Only the command StorePassword wrote is ours: anything else a shell would
// run, an item of the user's own under -a sqlmux, another way of quoting,
// goes nowhere near the tool, and the service is read as it was quoted
// (F8.16).
func TestDeletePasswordMatchesOnlyWhatWeWrote(t *testing.T) {
	bin := isolated(t, "sh", "cat", "grep", "od", "tr", "rm")
	hostOS = "darwin"
	fakeTool(t, bin, "security", "echo \"$*\" >>\"$FAKE/log\"\n"+fakeSecurity)
	calls := func(cmd string) string {
		_ = os.Remove(filepath.Join(os.Getenv("FAKE"), "log"))
		if err := DeletePassword(cmd); err != nil {
			t.Errorf("%q: %v", cmd, err)
		}
		log, _ := os.ReadFile(filepath.Join(os.Getenv("FAKE"), "log"))
		return strings.TrimSpace(string(log))
	}
	for cmd, want := range map[string]string{
		"security find-generic-password -a sqlmux -s 'sqlmux-x' -w":                  "delete-generic-password -a sqlmux -s sqlmux-x",
		"security find-generic-password -a sqlmux -s 'sqlmux-x' -w | xxd -r -p":      "delete-generic-password -a sqlmux -s sqlmux-x",
		"security find-generic-password -a sqlmux -s 'sqlmux-it'\\''s 库 x' -w":       "delete-generic-password -a sqlmux -s sqlmux-it's 库 x",
		"security find-generic-password -a sqlmux -s 'sqlmux-x' -w; rm -rf ~":        "",
		"security find-generic-password -a sqlmux -s 'sqlmux-x' -w\n":                "",
		"security find-generic-password -a sqlmux -s 'sqlmux-x' -w | xxd -r -p | tr": "",
		"security find-generic-password -a other -s 'sqlmux-x' -w":                   "",
		"security  find-generic-password -a sqlmux -s 'sqlmux-x' -w":                 "",
		" security find-generic-password -a sqlmux -s 'sqlmux-x' -w":                 "",
		"echo; security find-generic-password -a sqlmux -s 'sqlmux-x' -w":            "",
		"security find-generic-password -a sqlmux -s \"sqlmux-x\" -w":                "",
		"security find-generic-password -a sqlmux -s sqlmux-x -w":                    "",
		"security find-generic-password -a sqlmux -s 'my-own-item' -w":               "", // the user's, under the same account
		"security find-generic-password -a sqlmux -s 'sqlmux-a' 'b' -w":              "",
	} {
		if got := calls(cmd); got != want {
			t.Errorf("%q: calls %q, want %q", cmd, got, want)
		}
	}
	hostOS = "linux"
	fakeTool(t, bin, "secret-tool", "echo \"$*\" >>\"$FAKE/log\"\n"+fakeSecretTool)
	for cmd, want := range map[string]string{
		"secret-tool lookup sqlmux 'x'":                 "clear sqlmux x",
		"secret-tool lookup sqlmux 'it'\\''s'":          "clear sqlmux it's",
		"secret-tool lookup sqlmux 'x'; rm -rf ~":       "",
		"secret-tool lookup service sqlmux account 'x'": "",
		"echo; secret-tool lookup sqlmux 'x'":           "",
		"secret-tool lookup sqlmux x":                   "",
	} {
		if got := calls(cmd); got != want {
			t.Errorf("%q: calls %q, want %q", cmd, got, want)
		}
	}
}

// An item that is not there is not a failure to delete; one the tool
// refuses to is (F8.16).
func TestDeletePasswordNotFound(t *testing.T) {
	bin := isolated(t, "sh", "cat", "grep", "od", "tr", "rm")
	hostOS = "darwin"
	fakeTool(t, bin, "security", fakeSecurity)
	if err := DeletePassword("security find-generic-password -a sqlmux -s 'sqlmux-none' -w"); err != nil {
		t.Errorf("not there: %v", err)
	}
	fakeTool(t, bin, "security", "echo 'keychain is locked' >&2\nexit 1\n")
	if err := DeletePassword("security find-generic-password -a sqlmux -s 'sqlmux-a' -w"); err == nil || err.Error() != "security: keychain is locked" {
		t.Errorf("failing: %v", err)
	}
}

// Two commands read one item when they name the same service: a password
// that is not printable makes the command longer, not the item another
// (F8.16).
func TestSamePassword(t *testing.T) {
	hostOS = "darwin"
	plain, hex := KeychainCmd("a", "pw"), KeychainCmd("a", "pä")
	if plain == hex || !SamePassword(plain, hex) {
		t.Errorf("the same item, two commands: %q %q", plain, hex)
	}
	if SamePassword(plain, KeychainCmd("b", "pw")) || SamePassword("pass show a", "pass show a") || SamePassword(plain, "pass show a") {
		t.Error("another item, or not ours")
	}
	if !SamePassword(KeychainCmd("Db", "x"), KeychainCmd("db", "x")) {
		t.Error("a name in another case is the item a keychain finds by its service")
	}
	hostOS = "linux"
	if !SamePassword(KeychainCmd("a", "x"), KeychainCmd("a", "y")) || SamePassword(KeychainCmd("a", "x"), plain) {
		t.Error("secret-tool: a name is an item; a tool's items are not another's")
	}
}

// KeychainCmd is what StorePassword answers, known before anything is
// stored.
func TestKeychainCmdIsWhatStoreWrites(t *testing.T) {
	for _, c := range []struct{ os, tool, script string }{{"darwin", "security", fakeSecurity}, {"linux", "secret-tool", fakeSecretTool}} {
		bin := isolated(t, "sh", "cat", "grep", "od", "tr", "rm")
		hostOS = c.os
		fakeTool(t, bin, c.tool, c.script)
		for _, pw := range []string{"pw", "pä"} {
			cmd, _, err := StorePassword("my 'db'", pw)
			if err != nil || cmd != KeychainCmd("my 'db'", pw) {
				t.Errorf("%s %q: %q %v, want %q", c.os, pw, cmd, err, KeychainCmd("my 'db'", pw))
			}
		}
	}
}
