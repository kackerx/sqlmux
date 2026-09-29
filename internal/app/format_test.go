package app

import (
	"strings"
	"testing"

	"sqlmux/internal/editor"
)

// gqIn presses gq in console c and delivers what the Cmd brings back.
func gqIn(t *testing.T, a *App, c *consoleTab, keys string) {
	t.Helper()
	feed(t, a, keys+"g")
	_, cmd := a.Update(teaKey("q"))
	if cmd == nil {
		t.Fatal("gq formats nothing")
	}
	a.Update(cmd())
}

// gq lays out the statement under the cursor, its ; left where it was, as
// one undo step, the cursor on its last line as vim's gq; a selection
// formats what it holds and VISUAL ends (§9.5).
func TestFormatStatement(t *testing.T) {
	a, c := inConsole(t, "")
	a.consoleDid(c, c.ed.Load("x;\nselect a,b from t;"))
	gqIn(t, a, c, "j")
	if got := text(c); got != "x;\nselect\n  a,\n  b\nfrom\n  t;" || c.ed.Cursor() != (editor.Pos{Line: 5, Col: 2}) {
		t.Fatalf("%q, cursor %v", got, c.ed.Cursor())
	}
	if feed(t, a, "u"); text(c) != "x;\nselect a,b from t;" {
		t.Fatalf("u: %q", text(c))
	}
	a.consoleDid(c, c.ed.Load("select 1 union select 2\nfrom t"))
	gqIn(t, a, c, "gg0v$")
	if got := text(c); got != "select\n  1\nunion\nselect\n  2\nfrom t" || c.ed.Mode() != editor.Normal {
		t.Errorf("a charwise selection: %q in %v", got, c.ed.Mode())
	}
}

// What comes back after the text changed is dropped; a failure says why
// and changes nothing (§9.5).
func TestFormatDropsAndFails(t *testing.T) {
	a, c := inConsole(t, "")
	a.consoleDid(c, c.ed.Load("select a from t"))
	feed(t, a, "g")
	_, cmd := a.Update(teaKey("q"))
	msg := cmd()
	feed(t, a, "x")
	a.Update(msg)
	if text(c) != "elect a from t" {
		t.Errorf("a stale format landed: %q", text(c))
	}
	a.consoleDid(c, c.ed.Load("select 'a"))
	gqIn(t, a, c, "")
	if text(c) != "select 'a" || !strings.HasPrefix(a.toast, "格式化失败：Parse error") {
		t.Errorf("%q, toast %q", text(c), a.toast)
	}
}

// formatprg formats instead: stdin in, stdout out; its stderr's first
// line when it fails (§9.5).
func TestFormatPrg(t *testing.T) {
	a, c := inConsole(t, `formatprg = "tr a-z A-Z"`)
	a.consoleDid(c, c.ed.Load("select a from t"))
	gqIn(t, a, c, "")
	if text(c) != "SELECT A FROM T" {
		t.Errorf("%q", text(c))
	}
	a, c = inConsole(t, `formatprg = "echo 'no good' >&2; echo second >&2; exit 3"`)
	a.consoleDid(c, c.ed.Load("select a from t"))
	gqIn(t, a, c, "")
	if text(c) != "select a from t" || a.toast != "格式化失败：no good" {
		t.Errorf("%q, toast %q", text(c), a.toast)
	}
}
