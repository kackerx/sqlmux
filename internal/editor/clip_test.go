package editor

import (
	"strings"
	"testing"
)

// "+ and "* are the system clipboard (F3.38): a yank or a delete into it
// gives the query its text, the unnamed register too, as vim's; a plain
// one does not. "+p waits for the clipboard, then puts it as a register
// charwise, or linewise with a newline at its end, nothing when it is
// empty; the unnamed register is kept, but for one a "+ yank filled, which
// stands for "+ as in vim. With other registers the whole command goes;
// "" is the unnamed.
func TestClipboardRegister(t *testing.T) {
	e := New("one two\nthree")
	var eff Effect
	feed := func(ks string) {
		t.Helper()
		for _, k := range keys(t, ks) {
			if f := e.Feed(k); f.Clip != nil || f.Paste != nil {
				eff = f
			}
		}
	}
	if feed(`"+yy`); eff.Clip == nil || *eff.Clip != "one two\n" {
		t.Fatalf(`"+yy: %+v`, eff)
	}
	if feed(`wve"+y`); eff.Clip == nil || *eff.Clip != "two" || e.Mode() != Normal {
		t.Fatalf(`VISUAL "+y: %+v`, eff)
	}
	eff = Effect{}
	if feed("0yw"); eff.Clip != nil {
		t.Error("yw: the clipboard")
	}
	if feed(`w"*2x`); eff.Clip == nil || *eff.Clip != "tw" || e.Lines()[0] != "one o" {
		t.Fatalf(`"*2x: %+v %q`, eff, e.Lines())
	}
	eff = Effect{}
	if feed(`0yl"+p`); eff.Paste == nil || e.Lines()[0] != "one o" {
		t.Fatalf(`"+p: %+v`, eff)
	}
	if e.PutClip("CLIP", *eff.Paste); e.Lines()[0] != "oCLIPne o" {
		t.Errorf("charwise: %q", e.Lines())
	}
	eff = Effect{}
	feed(`"+P`)
	if e.PutClip("new\n", *eff.Paste); strings.Join(e.Lines(), "|") != "new|oCLIPne o|three" {
		t.Errorf("linewise: %q", e.Lines())
	}
	if feed("jp"); e.Lines()[1] != "ooCLIPne o" {
		t.Errorf("the unnamed register kept, yl's: %q", e.Lines())
	}
	eff = Effect{}
	feed(`"+p`)
	if f := e.PutClip("", *eff.Paste); f.Changed || e.Lines()[1] != "ooCLIPne o" {
		t.Errorf("an empty clipboard puts: %q", e.Lines())
	}
	before := strings.Join(e.Lines(), "|")
	if feed(`"add`); strings.Join(e.Lines(), "|") != before || e.Mode() != Normal {
		t.Errorf(`"a: %q in %v`, e.Lines(), e.Mode())
	}
	if feed(`""yyp`); len(e.Lines()) != 4 {
		t.Errorf(`"" is the unnamed: %q`, e.Lines())
	}
	e = New("one\ntwo")
	eff = Effect{}
	feed(`"+yy"+p`)
	e.PutClip("X", *eff.Paste)
	if feed("p"); strings.Join(e.Lines(), "|") != "oXXne|two" {
		t.Errorf(`the unnamed register after "+yy is "+'s: %q`, e.Lines())
	}
}

// Y in VISUAL yanks the lines, a block as y does (v_visop).
func TestVisualY(t *testing.T) {
	for ks, want := range map[string]string{`wvl"+Y`: "one two\n", `Vj"+Y`: "one two\nthree\n", `0<C-v>jl"+Y`: "on\nth\n"} {
		e := New("one two\nthree\nfour")
		var clip *string
		for _, k := range keys(t, ks) {
			if f := e.Feed(k); f.Clip != nil {
				clip = f.Clip
			}
		}
		if clip == nil || *clip != want || e.Mode() != Normal {
			t.Errorf("%s: %v, mode %v", ks, clip, e.Mode())
		}
	}
	for ks, mode := range map[string]Mode{"wvjY": Visual, "Vj\"+Y": VisualLine, "w<C-v>jlY": VisualBlock} { // gv comes back as it was
		e := New("one two\nthree\nfour")
		for _, k := range keys(t, ks+"gv") {
			e.Feed(k)
		}
		if e.Mode() != mode {
			t.Errorf("%sgv: mode %v, want %v", ks, e.Mode(), mode)
		}
	}
}

// With `clipboard = "unnamedplus"` a command with no register is "+'s: yank,
// delete and change give the clipboard their text, p and P ask for it, and an
// empty clipboard puts the unnamed register instead of nothing; a register
// typed ("a, "") is as before, and without it nothing is the clipboard's
// (F20.5).
func TestUnnamedPlus(t *testing.T) {
	defer func() { UnnamedPlus = false }()
	run := func(e *Editor, ks string) (eff Effect) {
		t.Helper()
		for _, k := range keys(t, ks) {
			if f := e.Feed(k); f.Clip != nil || f.Paste != nil {
				eff = f
			}
		}
		return eff
	}
	for _, name := range []string{"query", "one line", "read only"} {
		mk := func() *Editor {
			e := New("one two\nthree\nfour")
			e.OneLine, e.ReadOnly = name == "one line", name == "read only"
			return e
		}
		UnnamedPlus = false
		for _, ks := range []string{"yy", "yw", "x", "dd", "cwx<Esc>"} {
			if eff := run(mk(), ks); eff.Clip != nil {
				t.Errorf("%s, off, %s: the clipboard", name, ks)
			}
		}
		if eff := run(mk(), "yyp"); eff.Paste != nil {
			t.Errorf("%s, off: p asks the clipboard", name)
		}
		UnnamedPlus = true
		for ks, want := range map[string]string{"yy": "one two\n", "yw": "one ", "x": "o", "2x": "on", "dd": "one two\n", "cwx<Esc>": "one", "wD": "two", "vey": "one"} {
			eff := run(mk(), ks)
			switch {
			case name == "read only" && strings.ContainsAny(ks[:1], "x2dcw"): // changes the text: not run
				if eff.Clip != nil {
					t.Errorf("%s, on, %s: the clipboard", name, ks)
				}
			case eff.Clip == nil || *eff.Clip != want:
				t.Errorf("%s, on, %s: %+v, want %q", name, ks, eff.Clip, want)
			}
		}
		if eff := run(mk(), "p"); name == "read only" == (eff.Paste != nil) || eff.Paste != nil && !eff.Paste.Implicit() {
			t.Errorf("%s, on: p: %+v", name, eff)
		}
		for _, ks := range []string{`"ayy`, `""yy`} { // a named register, and the unnamed typed: not the clipboard's
			if run(mk(), ks).Clip != nil {
				t.Errorf("%s: %s", name, ks)
			}
		}
	}
	e := New("one two\nthree")
	eff := run(e, "ywP")
	if eff.Paste == nil || !eff.Paste.Implicit() {
		t.Fatalf("P: %+v", eff)
	}
	if e.PutClip("CLIP ", *eff.Paste); e.Lines()[0] != "CLIP one two" {
		t.Errorf("the clipboard's: %q", e.Lines())
	}
	eff = run(e, "jp")
	if f := e.PutClip("", *eff.Paste); f.Changed { // an empty clipboard is nothing to put, as nvim's E353
		t.Errorf("empty clipboard: %q", e.Lines())
	}
	if e.PutClip("", eff.Paste.Missing()); e.Lines()[1] != "threeone " { // no clipboard to read: the unnamed register, the yw's, the put of the clipboard's text left it
		t.Errorf("no clipboard: %q", e.Lines())
	}
	eff = run(e, `"+p`) // typed, it is still nothing when empty
	if f := e.PutClip("", *eff.Paste); f.Changed || eff.Paste.Implicit() {
		t.Errorf(`"+p: %+v`, f)
	}
}
