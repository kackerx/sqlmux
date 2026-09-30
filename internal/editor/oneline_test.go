package editor

import "testing"

// A WHERE's editor keeps one line (F3.39): o, O, J, r<CR> and the / ? :
// lines do nothing, j and k don't move, a line yanked is put as
// characters after or before the cursor, and the clipboard's lines too,
// joined by spaces; VISUAL's o still swaps its ends.
func TestOneLine(t *testing.T) {
	e := New("a = 1")
	e.OneLine = true
	feed := func(ks string) {
		t.Helper()
		for _, k := range keys(t, ks) {
			e.Feed(k)
		}
	}
	for _, ks := range []string{"o", "O", "J", "r<CR>", "/", "?", ":", "d/", "j", "k", "dj"} {
		if feed("$" + ks); len(e.Lines()) != 1 || e.Lines()[0] != "a = 1" || e.Mode() != Normal || e.Cursor().Col != 4 {
			t.Errorf("%s: %q in %v at %d", ks, e.Lines(), e.Mode(), e.Cursor().Col)
		}
		feed("<Esc>")
	}
	if feed("0yyp"); e.Lines()[0] != "aa = 1 = 1" || e.Cursor().Col != 5 {
		t.Errorf("yyp: %q at %d", e.Lines(), e.Cursor().Col)
	}
	if feed("uyy$P"); e.Lines()[0] != "a = a = 11" {
		t.Errorf("yyP: %q", e.Lines())
	}
	if feed("u0vlloy"); e.Cursor().Col != 0 || e.Mode() != Normal {
		t.Errorf("VISUAL o: at %d in %v", e.Cursor().Col, e.Mode())
	}
	var eff Effect
	for _, k := range keys(t, `"+P`) {
		eff = e.Feed(k)
	}
	if e.PutClip("x\ny\n", *eff.Paste); e.Lines()[0] != "x ya = 1" || len(e.Lines()) != 1 {
		t.Errorf(`"+P of lines: %q`, e.Lines())
	}
}
