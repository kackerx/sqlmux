package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/keymap"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// prgOf is the formatprg config line: the unix program, or its windows
// equivalent where the line has no sh syntax; a " of the program is
// escaped, the line sits inside TOML's.
func prgOf(t *testing.T, unix, windows string) string {
	t.Helper()
	prg := unix
	if runtime.GOOS == "windows" {
		prg = windows
	}
	return `formatprg = "` + strings.ReplaceAll(prg, `"`, `\"`) + `"`
}

// upperPS is a powershell file uppercasing what is on its stdin byte for
// byte: windows's tr a-z A-Z.
func upperPS(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upper.ps1")
	if err := os.WriteFile(path, []byte("$t = [Console]::In.ReadToEnd().ToUpper(); [Console]::Out.Write($t)"), 0o600); err != nil {
		t.Fatal(err)
	}
	return `powershell -NoProfile -ExecutionPolicy Bypass -File "` + filepath.ToSlash(path) + `"` // " quotes: -File does not read '
}

// gqIn presses keys in query c, the last starting a format, and
// delivers what the Cmd brings back.
func gqIn(t *testing.T, a *App, keys string) {
	t.Helper()
	ks, _ := keymap.Parse(keys)
	for _, k := range ks[:len(ks)-1] {
		a.Update(teaKey(k))
	}
	_, cmd := a.Update(teaKey(ks[len(ks)-1]))
	if cmd == nil {
		t.Fatal("formats nothing")
	}
	a.Update(cmd())
}

// gq is the editor's operator (§9.5): gqq lays out the statement under the
// cursor, its ; left where it was, as one undo step, the cursor on its
// last line as vim's gq; gq and a motion the statements it touches, first
// to last; a selection what it holds, and VISUAL ends.
func TestFormatStatement(t *testing.T) {
	a, c := inQuery(t, "", "")
	a.queryDid(c, c.ed.Load("x;\nselect a,b from t;"))
	gqIn(t, a, "jgqq")
	if got := text(c); got != "x;\nselect\n  a,\n  b\nfrom\n  t;" || c.ed.Cursor() != (editor.Pos{Line: 5, Col: 2}) {
		t.Fatalf("%q, cursor %v", got, c.ed.Cursor())
	}
	if feed(t, a, "u"); text(c) != "x;\nselect a,b from t;" {
		t.Fatalf("u: %q", text(c))
	}
	a.queryDid(c, c.ed.Load("select 1 union select 2\nfrom t"))
	gqIn(t, a, "gg0v$gq")
	if got := text(c); got != "select\n  1\nunion\nselect\n  2\nfrom t" || c.ed.Mode() != editor.Normal {
		t.Errorf("a charwise selection: %q in %v", got, c.ed.Mode())
	}
	a.queryDid(c, c.ed.Load("select 1; select 2;\n\nselect 3"))
	gqIn(t, a, "gggqap")
	if got := text(c); got != "select\n  1;\n\nselect\n  2;\n\nselect 3" || c.ed.Mode() != editor.Normal {
		t.Errorf("gqap: %q in %v", got, c.ed.Mode())
	}
}

// What comes back after the text changed, or into INSERT, is dropped; a
// failure, no output or a timeout says so and changes nothing (§9.5).
func TestFormatDropsAndFails(t *testing.T) {
	a, c := inQuery(t, "", "")
	for _, k := range []string{"x", "A"} {
		a.queryDid(c, c.ed.Load("select a from t"))
		feed(t, a, "gg0gq")
		_, cmd := a.Update(teaKey("q"))
		msg := cmd()
		feed(t, a, k)
		a.Update(msg)
		if text(c) != map[string]string{"x": "elect a from t", "A": "select a from t"}[k] {
			t.Errorf("a stale format landed after %s: %q", k, text(c))
		}
		feed(t, a, "<Esc>")
	}
	a.queryDid(c, c.ed.Load("select 'a"))
	gqIn(t, a, "gqq")
	if text(c) != "select 'a" || !strings.HasPrefix(a.toast, "格式化失败：Parse error") {
		t.Errorf("%q, toast %q", text(c), a.toast)
	}
	a, c = inQuery(t, prgOf(t, "true", "rem"), "") // no output: the toast says so
	a.queryDid(c, c.ed.Load("select a from t;"))
	if gqIn(t, a, "gqq"); text(c) != "select a from t;" || a.toast != "格式化失败：没有输出" {
		t.Errorf("no output: %q, toast %q", text(c), a.toast)
	}
	defer func(d time.Duration) { sqlkit.FormatTimeout = d }(sqlkit.FormatTimeout)
	sqlkit.FormatTimeout = 200 * time.Millisecond
	a, c = inQuery(t, prgOf(t, "sleep 10; echo x", "ping -n 10 127.0.0.1 >nul & echo x"), "")
	a.queryDid(c, c.ed.Load("select a from t"))
	start := time.Now()
	if gqIn(t, a, "gqq"); text(c) != "select a from t" || a.toast != "格式化超时（200ms）" || time.Since(start) > 2*time.Second {
		t.Errorf("formatprg's child past the timeout: %q, toast %q, after %v", text(c), a.toast, time.Since(start))
	}
}

// formatprg formats instead: stdin in, stdout out; its stderr's first
// line when it fails (§9.5).
func TestFormatPrg(t *testing.T) {
	a, c := inQuery(t, prgOf(t, "tr a-z A-Z", upperPS(t)), "")
	a.queryDid(c, c.ed.Load("select a from t"))
	gqIn(t, a, "gqq")
	if text(c) != "SELECT A FROM T" {
		t.Errorf("%q", text(c))
	}
	a, c = inQuery(t, prgOf(t, "echo 'no good' >&2; echo second >&2; exit 3", "echo no good 1>&2&exit 3"), "")
	a.queryDid(c, c.ed.Load("select a from t"))
	gqIn(t, a, "gqq")
	if text(c) != "select a from t" || a.toast != "格式化失败：no good" {
		t.Errorf("%q, toast %q", text(c), a.toast)
	}
}

// A line long enough to scroll the view sideways, gqq'd into lines that need no
// scroll: the text shows from its first column (F14.8).
func TestFormatViewFromTheLeft(t *testing.T) {
	a, c := inQuery(t, "", "")
	long := "select id, name, email, created_at, deleted_at from t_user where id = 1 and name = 'a' order by id"
	a.queryDid(c, c.ed.Load(long))
	feed(t, a, "$")
	a.render()
	if c.ed.Left() == 0 {
		t.Fatalf("the long line did not scroll the view: cursor %v, left %d", c.ed.Cursor(), c.ed.Left())
	}
	gqIn(t, a, "gqq")
	a.render()
	if c.ed.Left() != 0 || strings.Contains(text(c), long) {
		t.Errorf("after gqq: left %d, text %q", c.ed.Left(), text(c))
	}
}
