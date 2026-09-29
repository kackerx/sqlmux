package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"sqlmux/internal/config"
	"sqlmux/internal/editor"
	"sqlmux/internal/keymap"
	"sqlmux/internal/ui"
)

// inConsole is the default layout with focus on console_1 (⟨2⟩), whose
// file is in a directory of the test's own.
func inConsole(t *testing.T, toml string) (*App, *consoleTab) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a := configured(t, 160, 45, toml)
	feed(t, a, "<C-l>")
	c := consoleOf(a.focused())
	if c == nil {
		t.Fatal("C-l from ⟨1⟩ is not on the console")
	}
	return a, c
}

func text(c *consoleTab) string { return strings.Join(c.ed.Lines(), "\n") }

// Keys the keymap leaves go to the editor; one waiting for more takes the
// next key itself, so f<Space>x is no SPC x (§6.4). : and ; are its vim's.
func TestConsoleKeys(t *testing.T) {
	a, c := inConsole(t, "")
	feed(t, a, "ione two three<CR>four<Esc>gg0f<Space>x")
	if text(c) != "onetwo three\nfour" || len(a.win().Root.Leaves()) != 2 {
		t.Fatalf("f<Space>x: %q, panes %d", text(c), len(a.win().Root.Leaves()))
	}
	feed(t, a, "0fe;")
	if c.ed.Cursor() != (editor.Pos{Line: 0, Col: 10}) {
		t.Errorf("; repeats f: %v", c.ed.Cursor())
	}
	feed(t, a, ":")
	if a.palette != nil || c.ed.Mode() != editor.Command {
		t.Fatalf(": opens the editor's command line: palette %v, mode %v", a.palette, c.ed.Mode())
	}
	feed(t, a, "2<CR>")
	if c.ed.Cursor().Line != 1 {
		t.Errorf(":2 goes to line 2: %v", c.ed.Cursor())
	}
	feed(t, a, "d<C-c>") // C-c is esc: no quit toast
	if c.ed.Pending() != "" || a.toast != "" {
		t.Errorf("d then C-c: pending %q, toast %q", c.ed.Pending(), a.toast)
	}
	feed(t, a, ":foo<CR>")
	if a.toast != "不支持的命令：foo" {
		t.Errorf("toast %q", a.toast)
	}
}

// The user's maps for the console apply there, over the editor's own keys;
// the right-hand side goes in as typed, meeting the editor as it then is
// (§6.6): after i, a space is typed, no leader.
func TestConsoleMaps(t *testing.T) {
	a, c := inConsole(t, "tab_width = 4\n[map.console.normal]\nL = \"5l\"\nQ = \"iselect x<Esc>\"\nX = \"f<Space>\"\n[map.console.visual]\nH = \"2h\"\n")
	feed(t, a, "Q")
	if text(c) != "select x" || c.ed.Mode() != editor.Normal || len(a.win().Root.Leaves()) != 2 {
		t.Fatalf("Q = iselect x<Esc>: %q in %v, panes %d", text(c), c.ed.Mode(), len(a.win().Root.Leaves()))
	}
	feed(t, a, "0Xxx")
	if text(c) != "select" || len(a.win().Root.Leaves()) != 2 {
		t.Fatalf("X = f<Space>, x, x: %q, panes %d", text(c), len(a.win().Root.Leaves()))
	}
	feed(t, a, "dd")
	feed(t, a, "iabcdefgh<Esc>0L")
	if c.ed.Cursor().Col != 5 {
		t.Errorf("L = 5l: col %d", c.ed.Cursor().Col)
	}
	if feed(t, a, "vH"); c.ed.Cursor().Col != 3 || c.ed.Mode() != editor.Visual {
		t.Errorf("H = 2h in VISUAL: col %d in %v", c.ed.Cursor().Col, c.ed.Mode())
	}
	feed(t, a, "<Esc>")
	feed(t, a, "o<Tab>x<Esc>")
	if c.ed.Lines()[1] != "    x" {
		t.Errorf("tab_width 4: %q", c.ed.Lines()[1])
	}
}

// The file (§11): :w writes it, 0600; unchanged it is not written; :q
// writes and closes; a new session has it back. The autosave writes only
// for the latest change.
func TestConsoleFile(t *testing.T) {
	a, c := inConsole(t, "")
	if c.path != config.ConsolePath("doraemon", 1) {
		t.Fatalf("path %s", c.path)
	}
	feed(t, a, "iselect 1;<Esc>")
	a.Update(autosave{c, c.ver - 1})
	if _, err := os.Stat(c.path); err == nil {
		t.Fatal("a stale autosave wrote")
	}
	a.Update(autosave{c, c.ver})
	if data, _ := os.ReadFile(c.path); string(data) != "select 1;\n" {
		t.Fatalf("autosave: %q", data)
	}
	os.Remove(c.path)
	feed(t, a, ":w<CR>")
	if _, err := os.Stat(c.path); err == nil {
		t.Error("written with nothing changed")
	}
	feed(t, a, "A -- one<Esc>:w<CR>")
	info, err := os.Stat(c.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf(":w: %v %v", info, err)
	}
	feed(t, a, "o2<Esc>:wq<CR>")
	if data, _ := os.ReadFile(c.path); string(data) != "select 1; -- one\n2\n" || len(a.win().Root.Leaves()) != 1 {
		t.Fatalf(":wq: %q, panes %d", data, len(a.win().Root.Leaves()))
	}
	if s := newSession("doraemon", "", nil, nil); text(consoleOf(s.Windows[0].Root.B.Pane)) != "select 1; -- one\n2" || s.warning != "" {
		t.Fatalf("a new session: %q", s.warning)
	}
}

// A console file that cannot be read is not opened, empty or not: a toast
// says so and the rest starts (§11).
func TestConsoleUnreadable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	os.MkdirAll(config.ConsolePath("doraemon", 1), 0o700) // a directory where the file goes
	s := newSession("doraemon", "", nil, nil)
	if p := s.Windows[0].Root.B.Pane; len(p.Tabs) != 0 || !strings.HasPrefix(s.warning, "读取失败：") {
		t.Fatalf("tabs %v, warning %q", p.Tabs, s.warning)
	}
	c := config.Default()
	keys, _ := keymap.New(c)
	if a := New(c, keys, s, &config.State{}, "careful"); a.warning != "careful；"+s.warning {
		t.Errorf("warning %q", a.warning)
	}
}

// A console that cannot be written says so, and neither closes nor quits.
func TestConsoleSaveFails(t *testing.T) {
	a, c := inConsole(t, "")
	c.path = filepath.Join(os.DevNull, "console_1.sql")
	feed(t, a, "ix<Esc>:w<CR>")
	if !strings.HasPrefix(a.toast, "保存失败：") {
		t.Fatalf("toast %q", a.toast)
	}
	if feed(t, a, ":q<CR>"); consoleOf(a.focused()) != c {
		t.Error(":q closed what was not written")
	}
	if feed(t, a, "<C-p>>qa<CR>") {
		t.Error("quit with the console not written")
	}
	c.path = filepath.Join(t.TempDir(), "console_1.sql")
	if !feed(t, a, "<C-p>>qa<CR>") {
		t.Fatal("quit")
	}
	if data, _ := os.ReadFile(c.path); string(data) != "x\n" {
		t.Errorf("quitting writes the console: %q", data)
	}
}

// A paste is text in every mode, never keys (§11); a yank goes to the
// clipboard.
func TestConsolePasteAndYank(t *testing.T) {
	a, c := inConsole(t, "")
	feed(t, a, "iab<Esc>0")
	a.Update(tea.PasteMsg{Content: "dd\r\nx"})
	if text(c) != "add\nxb" || c.ed.Mode() != editor.Normal {
		t.Fatalf("pasted in NORMAL: %q %v", text(c), c.ed.Mode())
	}
	_, cmd := a.Update(teaKey("y"))
	if cmd != nil {
		t.Fatal("y alone yanks nothing")
	}
	_, cmd = a.Update(teaKey("y"))
	if cmd == nil || cmd() != tea.SetClipboard("xb\n")() {
		t.Error("yy does not reach the clipboard")
	}
}

// The status bar: the editor's mode by name in VISUAL's or INSERT's color,
// the selection's size with the key that runs it, what the editor waits
// on (§7.8).
func TestConsoleStatusBar(t *testing.T) {
	a, _ := inConsole(t, "")
	feed(t, a, "iabc<CR>def<Esc>gg")
	th := a.theme
	for _, c := range []struct {
		keys, mode, info string
		bg               any
	}{
		{"vl", "VISUAL", "2 字符 · ↵ run", th.Keyword},
		{"<Esc>Vj", "V-LINE", "2 行 · ↵ run", th.Keyword},
		{"<Esc>gg<C-v>jl", "V-BLOCK", "2 行 × 2 列 · ↵ run", th.Keyword},
		{"<Esc>R", "REPLACE", "", th.Warn},
		{"<Esc>:", "COMMAND", "", th.Info},
		{"<Esc>2d", "NORMAL", "", th.Focus},
	} {
		feed(t, a, c.keys)
		f := a.render()
		bar := strings.Split(f.String(), "\n")[44]
		if st := styleOf(t, f, " "+c.mode+" "); !strings.Contains(bar, " "+c.mode+" ") || st.Bg != c.bg {
			t.Errorf("%s: %q, bg %v", c.keys, bar, st.Bg)
		}
		if c.info != "" && !strings.Contains(bar, c.info) {
			t.Errorf("%s: no %q in %q", c.keys, c.info, bar)
		}
	}
	if bar := strings.Split(a.render().String(), "\n")[44]; !strings.Contains(bar, " 2d ") {
		t.Errorf("showcmd: %q", bar)
	}
}

// The terminal cursor is vim's: a block, a bar in INSERT, a line under in
// REPLACE (§11).
func TestConsoleCursorShape(t *testing.T) {
	a, _ := inConsole(t, "")
	for _, c := range []struct {
		keys string
		want tea.CursorShape
	}{{"", tea.CursorBlock}, {"i", tea.CursorBar}, {"<Esc>R", tea.CursorUnderline}, {"<Esc>:", tea.CursorBlock}} {
		feed(t, a, c.keys)
		if v := a.View(); v.Cursor == nil || v.Cursor.Shape != c.want {
			t.Errorf("%q: cursor %+v", c.keys, v.Cursor)
		}
	}
}

// A click puts the cursor there, a drag selects, the wheel scrolls the
// console under the pointer without taking the focus (§11).
func TestConsoleMouse(t *testing.T) {
	a, c := inConsole(t, "")
	feed(t, a, "i"+strings.Repeat("select 1;<CR>", 60)+"<Esc>gg")
	feed(t, a, "<C-h>") // focus back on ⟨1⟩
	text := c.ed.Top()
	wheel := func() {
		r := a.layout()[2]
		a.Update(tea.MouseWheelMsg{X: r.Min.X + 10, Y: r.Min.Y + 5, Button: tea.MouseWheelDown})
	}
	if wheel(); c.ed.Top() != text+wheelStep || a.win().Focus != 1 {
		t.Fatalf("wheel: top %d, focus %d", c.ed.Top(), a.win().Focus)
	}
	view, body := a.consoleView(a.win().pane(2), c)
	at := view.TextArea(body).Min
	click(a, uv.Pos(at.X+3, at.Y+1))
	if a.win().Focus != 2 || c.ed.Cursor() != (editor.Pos{Line: c.ed.Top() + 1, Col: 3}) {
		t.Fatalf("click: focus %d, cursor %v", a.win().Focus, c.ed.Cursor())
	}
	a.Update(tea.MouseClickMsg{X: at.X, Y: at.Y, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: at.X + 2, Y: at.Y + 1, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: at.X + 2, Y: at.Y + 1, Button: tea.MouseLeft})
	from, to, ok := c.ed.Selection()
	if !ok || from != (editor.Pos{Line: c.ed.Top(), Col: 0}) || to != (editor.Pos{Line: c.ed.Top() + 1, Col: 2}) {
		t.Errorf("drag: %v %v %v", from, to, ok)
	}
	if a.Update(tea.MouseMotionMsg{X: at.X + 5, Y: at.Y + 1}); c.ed.Cursor().Col != 2 {
		t.Error("moving after the release selects")
	}
}

// $EDITOR edits the file, written first; what it holds after is one undo
// step (§11).
func TestConsoleExternal(t *testing.T) {
	a, c := inConsole(t, "")
	feed(t, a, "iselect 1<Esc>")
	if a.run("console.external", 0) == nil {
		t.Fatal("no editor run")
	}
	if data, _ := os.ReadFile(c.path); string(data) != "select 1\n" {
		t.Fatalf("not written first: %q", data)
	}
	os.WriteFile(c.path, []byte("select 2;\nselect 3;\n"), 0o600)
	a.Update(externalDone{c, nil})
	if text(c) != "select 2;\nselect 3;" || c.saved != c.ver {
		t.Fatalf("read back: %q, saved %d of %d", text(c), c.saved, c.ver)
	}
	if feed(t, a, "u"); text(c) != "select 1" {
		t.Errorf("u: %q", text(c))
	}
}

// What the console draws is the editor's state: the selection by kind,
// the command line.
func TestConsoleView(t *testing.T) {
	a, c := inConsole(t, "")
	feed(t, a, "iab<CR>c\td<Esc>")
	view := func() ui.Console { v, _ := a.consoleView(a.focused(), c); return v }
	if v := view(); v.Cursor != (ui.TextPos{Line: 1, Col: 2}) || v.CursorCol != 2 || !v.Normal || v.Sel.Mode != ui.SelNone || v.Prompt != "" {
		t.Errorf("NORMAL: %+v", v)
	}
	for _, s := range []struct {
		keys string
		want ui.Sel
	}{
		{"kv", ui.Sel{Mode: ui.SelChars, From: ui.TextPos{Line: 0, Col: 1}, To: ui.TextPos{Line: 0, Col: 1}}},
		{"<Esc>Vj", ui.Sel{Mode: ui.SelLines, From: ui.TextPos{Line: 0, Col: 1}, To: ui.TextPos{Line: 1, Col: 2}}},
		{"<Esc>gg<C-v>j", ui.Sel{Mode: ui.SelBlock, From: ui.TextPos{Line: 0, Col: 1}, To: ui.TextPos{Line: 1, Col: 2}, Left: 1, Right: 2}}, // b to d, past the tab
	} {
		feed(t, a, s.keys)
		if v := view(); v.Sel != s.want || v.Normal {
			t.Errorf("%s: %+v", s.keys, v.Sel)
		}
	}
	feed(t, a, "<Esc>:%s")
	if v := view(); v.Prompt != ":" || v.Text != "%s" || v.Pos != 2 {
		t.Errorf("command line: %+v", v)
	}
}
