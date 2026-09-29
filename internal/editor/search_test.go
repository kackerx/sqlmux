package editor

import (
	"strings"
	"testing"
)

// RE2 reads + ? | ( ) as vim's \v does, not as vim's default magic: these
// cannot be compared with nvim, so they are checked here.
func TestRE2Patterns(t *testing.T) {
	for _, c := range []struct{ keys, text, want string }{
		{":s/(\\w+) (\\w+)/\\2 \\1/<CR>", "select id", "id select"},
		{":s/a+/x/<CR>", "baaab", "bxb"},
		{":s/ab?c/x/g<CR>", "ac abc abbc", "x x abbc"},
		{":s/id|name/x/g<CR>", "id, name", "x, x"},
		{":s/(a)(b)/[&:\\2]/<CR>", "ab", "[ab:b]"},
		{":s/x/$1/<CR>", "x", "$1"},
		{"/name|id<CR>x", "select name", "select ame"},
		{"/a{2}<CR>x", "a aa", "a a"},
	} {
		e := New(c.text)
		for _, k := range keys(t, c.keys) {
			e.Feed(k)
		}
		if got := strings.Join(e.Lines(), "\n"); got != c.want {
			t.Errorf("%s on %q: %q, want %q", c.keys, c.text, got, c.want)
		}
	}
}

func TestSearchErrors(t *testing.T) {
	for _, c := range []struct{ keys, want string }{
		{"/zz<CR>", "找不到：zz"},
		{":s/zz/x/<CR>", "找不到：zz"},
		{"/a(<CR>", "正则有误：error parsing regexp: missing closing ): `a(`"},
		{":s//x/<CR>", "没有上一个模式"},
		{"n", "没有上一个模式"},
		{":'<,'>s/a/x/<CR>", "没有选区"},
		{":1,9s/e/x/<CR>", "范围无效"},
		{":-5<CR>", "范围无效"},
		{":9<CR>", ""},
		{":w<CR>", ""},
	} {
		e := New("select")
		var eff Effect
		for _, k := range keys(t, c.keys) {
			eff = e.Feed(k)
		}
		if eff.Error != c.want {
			t.Errorf("%s: %q, want %q", c.keys, eff.Error, c.want)
		}
	}
	e := New("select")
	var eff Effect
	for _, k := range keys(t, ":wq<CR>") {
		eff = e.Feed(k)
	}
	if eff.Ex != "wq" {
		t.Errorf(":wq: Ex %q, want wq", eff.Ex)
	}
}

// C-w in the command line and in every input (§7.9): back past the
// blanks, then over a run of one class; M-BS is C-w.
func TestWordStart(t *testing.T) {
	for s, want := range map[string]string{
		"select foo.bar": "select foo.", "a  ": "", "   ": "", "x = 'ab": "x = '", "id, ": "id", "": "", "a 中文": "a ",
	} {
		if got := s[:WordStart(s, len(s))]; got != want {
			t.Errorf("%q: %q, want %q", s, got, want)
		}
	}
	e := New("")
	feedAll(t, e, ":abc def<M-BS>")
	if _, text, _, _ := e.CmdLine(); text != "abc " {
		t.Errorf(":abc def M-BS: %q", text)
	}
	e = New("x")
	feedAll(t, e, "A select foo.bar<M-BS><Esc>")
	if e.Lines()[0] != "x select foo." {
		t.Fatalf("INSERT: %q", e.Lines())
	}
	if feedAll(t, e, "u"); e.Lines()[0] != "x select foo.bar" {
		t.Errorf("M-BS breaks the undo step as C-w: %q", e.Lines())
	}
	e = New("ab cd")
	if feedAll(t, e, "$<M-BS>v<M-BS>"); e.Lines()[0] != "ab cd" || e.Mode() != Visual {
		t.Errorf("NORMAL and VISUAL: %q %v", e.Lines(), e.Mode())
	}
	e = New("")
	if feedAll(t, e, "2ia b<M-BS><Esc>"); e.Lines()[0] != "a a " {
		t.Errorf("a count types C-w again: %q", e.Lines())
	}
}
