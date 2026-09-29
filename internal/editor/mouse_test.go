package editor

import (
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
