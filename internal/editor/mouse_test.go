package editor

import (
	"fmt"
	"strings"
	"testing"
)

func feedAll(t *testing.T, e *Editor, ks string) {
	t.Helper()
	for _, k := range keys(t, ks) {
		e.Feed(k)
	}
}

// A click puts the cursor on the character there and j, k aim for the
// clicked column; it ends VISUAL, and INSERT goes on, a new undo step.
func TestClick(t *testing.T) {
	e := New("abc\nabcdef")
	e.Click(0, 10)
	if e.Cursor() != (Pos{0, 2}) {
		t.Errorf("past the end: %v", e.Cursor())
	}
	if e.Feed("j"); e.Cursor() != (Pos{1, 5}) {
		t.Errorf("j after the click: %v", e.Cursor())
	}
	feedAll(t, e, "v")
	if e.Click(0, 1); e.Mode() != Normal || e.Cursor() != (Pos{0, 1}) {
		t.Errorf("in VISUAL: %v %v", e.Mode(), e.Cursor())
	}
	feedAll(t, e, "ix")
	e.Click(1, 0)
	feedAll(t, e, "y<Esc>u")
	if got := strings.Join(e.Lines(), "\n"); e.Mode() != Normal || got != "axbc\nabcdef" {
		t.Errorf("INSERT, a click, typing, u: %q in %v", got, e.Mode())
	}
}

// In INSERT a click or the wheel that moves the cursor ends the change so
// far, as an arrow key: what the probes of nvim_input_mouse gave.
func TestMouseInInsert(t *testing.T) {
	lines := func() *Editor {
		var ls []string
		for i := 1; i <= 30; i++ {
			ls = append(ls, fmt.Sprintf("line%d", i))
		}
		e := New(strings.Join(ls, "\n"))
		e.SetHeight(22)
		return e
	}
	for _, c := range []struct {
		name string
		e    *Editor
		do   func(e *Editor)
		want string // the first lines and the cursor
	}{
		{"wheel, then u keeps what came before", lines(), func(e *Editor) {
			feedAll(t, e, "Ax")
			e.Scroll(3)
			feedAll(t, e, "y<Esc>u")
		}, "line1x line2 line3 line4 {3 4}"},
		{"wheel drops the count", lines(), func(e *Editor) {
			feedAll(t, e, "3ix")
			e.Scroll(3)
			feedAll(t, e, "<Esc>")
		}, "xline1 line2 line3 line4 {3 0}"},
		{"wheel leaves an autoindent", New("  abc\n" + strings.Repeat("x\n", 29)), func(e *Editor) {
			e.SetHeight(22)
			feedAll(t, e, "o")
			e.Scroll(3)
			feedAll(t, e, "<Esc>")
		}, "  abc  x x {3 0}"},
		{"a click where the cursor is keeps the undo step", New("abc\ndef"), func(e *Editor) {
			feedAll(t, e, "ix")
			e.Click(0, 1)
			feedAll(t, e, "y<Esc>u")
		}, "abc def {0 0}"},
		{"a click along the line ends it", New("abcdef\ndef"), func(e *Editor) {
			feedAll(t, e, "ix")
			e.Click(0, 4)
			feedAll(t, e, "y<Esc>u")
		}, "xabcdef def {0 4}"},
		{"a click off an autoindent", New("  abc\ndef"), func(e *Editor) {
			feedAll(t, e, "o")
			e.Click(2, 1)
			feedAll(t, e, "<Esc>")
		}, "  abc  def {2 0}"},
		{"the wheel drops d and stays", lines(), func(e *Editor) {
			feedAll(t, e, "d")
			e.Scroll(3)
			feedAll(t, e, "w")
		}, "line1 line2 line3 line4 {1 0}"},
		{"a click drops d", New("abc def ghi"), func(e *Editor) { // nvim deletes to the click
			feedAll(t, e, "d")
			e.Click(0, 6)
		}, "abc def ghi {0 6}"},
	} {
		c.do(c.e)
		ls := c.e.Lines()[:min(4, len(c.e.Lines()))]
		if got := fmt.Sprintf("%s %v", strings.Join(ls, " "), c.e.Cursor()); got != c.want || c.e.Pending() != "" {
			t.Errorf("%s: %q pending %q, want %q", c.name, got, c.e.Pending(), c.want)
		}
	}
	e := lines()
	feedAll(t, e, "d")
	if e.Scroll(3); e.Top() != 0 {
		t.Errorf("d, the wheel: top %d", e.Top())
	}
}

// A drag selects from the click to the pointer, in VISUAL, ending an INSERT.
func TestDrag(t *testing.T) {
	e := New("abc\ndef")
	feedAll(t, e, "A")
	e.Click(0, 1)
	e.Drag(1, 1)
	from, to, ok := e.Selection()
	if !ok || e.Mode() != Visual || from != (Pos{0, 1}) || to != (Pos{1, 1}) {
		t.Errorf("%v %v %v in %v", from, to, ok, e.Mode())
	}
	if e.Drag(1, 9); e.Cursor() != (Pos{1, 3}) {
		t.Errorf("past the end in VISUAL: %v", e.Cursor())
	}
}

// The wheel scrolls until the last line is at the top, the cursor kept in view.
func TestScroll(t *testing.T) {
	e := New(strings.Repeat("x\n", 99) + "x")
	e.SetHeight(10)
	if e.Scroll(3); e.Top() != 3 || e.Cursor().Line != 3 {
		t.Errorf("down 3: top %d, cursor %v", e.Top(), e.Cursor())
	}
	if e.Scroll(500); e.Top() != 99 {
		t.Errorf("down to the end: top %d", e.Top())
	}
	if e.Scroll(-500); e.Top() != 0 || e.Cursor().Line != 9 {
		t.Errorf("back up: top %d, cursor %v", e.Top(), e.Cursor())
	}
}

// A completion taken replaces the word before the cursor as typing would:
// one undo step with what was typed before it.
func TestComplete(t *testing.T) {
	e := New("x")
	feedAll(t, e, "Aselect * from t_o")
	if eff := e.Complete(15, "t_order"); !eff.Changed || e.Lines()[0] != "xselect * from t_order" || e.Cursor().Col != 22 {
		t.Fatalf("%v %q %v", eff, e.Lines(), e.Cursor())
	}
	feedAll(t, e, " o<Esc>u")
	if e.Lines()[0] != "x" {
		t.Errorf("u: %q", e.Lines())
	}
	if eff := e.Complete(0, "y"); eff.Changed {
		t.Error("in NORMAL")
	}
	e = New("")
	feedAll(t, e, "2it_o")
	e.Complete(0, "t_order")
	if feedAll(t, e, "<Esc>"); e.Lines()[0] != "t_ordert_order" { // nvim's 2it_o<C-n><Esc>
		t.Errorf("a count types what was taken: %q", e.Lines())
	}
}

// gq is an operator: it asks the console to lay out what its motion
// covers, the statement for gqq and gqgq, the selection in VISUAL (a
// block's lines); the text stays as it is (§9.5).
func TestFormatOperator(t *testing.T) {
	for _, c := range []struct {
		keys string
		want FormatSpan
	}{
		{"gqap", FormatSpan{From: Pos{0, 0}, To: Pos{1, 0}}}, // the paragraph and the blank after it
		{"jgqj", FormatSpan{From: Pos{1, 0}, To: Pos{2, 8}}},
		{"gqq", FormatSpan{Current: true}},
		{"jgqgq", FormatSpan{From: Pos{1, 0}, To: Pos{1, 0}, Current: true}},
		{"lvlgq", FormatSpan{From: Pos{0, 1}, To: Pos{0, 3}, Selected: true}},
		{"Vjgq", FormatSpan{From: Pos{0, 0}, To: Pos{1, 0}, Selected: true}},
		{"l<C-v>jgq", FormatSpan{From: Pos{0, 0}, To: Pos{1, 0}, Selected: true}},
	} {
		e := New("select 1;\n\nselect 2")
		var got *FormatSpan
		for _, k := range keys(t, c.keys) {
			if eff := e.Feed(k); eff.Format != nil {
				got = eff.Format
			}
		}
		if got == nil || *got != c.want || e.Mode() != Normal || strings.Join(e.Lines(), "\n") != "select 1;\n\nselect 2" {
			t.Errorf("%s: %+v in %v", c.keys, got, e.Mode())
		}
	}
}

// Loading new text is one undo step; the same text changes nothing.
func TestLoad(t *testing.T) {
	e := New("ab\ncd")
	feedAll(t, e, "jA")
	if eff := e.Load("ab\nxy\n"); !eff.Changed || e.Mode() != Normal || strings.Join(e.Lines(), "\n") != "ab\nxy" {
		t.Errorf("%v %v %q", eff, e.Mode(), e.Lines())
	}
	if eff := e.Load("ab\nxy"); eff.Changed {
		t.Error("the same text is no change")
	}
	if e.Feed("u"); strings.Join(e.Lines(), "\n") != "ab\ncd" {
		t.Errorf("u: %q", e.Lines())
	}
}

// Size is the selection as nvim's showcmd counts it.
func TestSize(t *testing.T) {
	for _, c := range []struct {
		keys         string
		lines, chars int
	}{
		{"vl", 1, 2},
		{"v$", 1, 4}, // the end of the line is one
		{"lvj", 2, 0},
		{"V", 1, 0},
		{"<C-v>jl", 2, 3}, // the tab is two columns, d the third
		{"j<C-v>kl", 2, 2},
		{"", 0, 0},
	} {
		e := New("abc\n\tdef")
		feedAll(t, e, c.keys)
		if lines, chars := e.Size(); lines != c.lines || chars != c.chars {
			t.Errorf("%s: %d %d, want %d %d", c.keys, lines, chars, c.lines, c.chars)
		}
	}
}

// Replace is one undo step, the cursor on the first non-blank of its last
// line, as gq leaves it; the same text is none.
func TestReplace(t *testing.T) {
	e := New("x select a, b from t; y\nz")
	if eff := e.Replace(Pos{0, 2}, Pos{0, 20}, "select\n  a,\n  b\nfrom\n  t"); !eff.Changed || e.Cursor() != (Pos{4, 2}) {
		t.Fatalf("%v, cursor %v", eff, e.Cursor())
	}
	if got := strings.Join(e.Lines(), "\n"); got != "x select\n  a,\n  b\nfrom\n  t; y\nz" {
		t.Fatalf("%q", got)
	}
	if eff := e.Replace(Pos{1, 2}, Pos{1, 4}, "a,"); eff.Changed {
		t.Error("the same text")
	}
	if e.Feed("u"); strings.Join(e.Lines(), "\n") != "x select a, b from t; y\nz" {
		t.Errorf("u: %q", e.Lines())
	}
}
