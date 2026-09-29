package editor

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"sqlmux/internal/keymap"
)

// The editor is checked against nvim (tech-design §15). testdata/cases.txt
// holds the cases: "## title", then the keys in vim notation, then the
// text with █ where the cursor starts ({1..100} stands for the lines 1 to
// 100); blank lines before the next case separate it. `go generate` (go test -update) runs each in nvim and keeps
// what nvim ends with in testdata/cases.golden, so the test itself needs
// no nvim.

var update = flag.Bool("update", false, "run testdata/cases.txt in nvim and rewrite testdata/cases.golden")

const cursorMark = "█"

// nvimRows is the window nvim's 24-line screen leaves: less the status
// line and the command line; nvimCols is as wide as the screen.
const nvimRows, nvimCols = 22, 80

type nvimCase struct {
	title, keys string
	text        []string
	cur         Pos
}

func readCases(t *testing.T) []nvimCase {
	data, err := os.ReadFile("testdata/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	var cases []nvimCase
	seen := map[string]bool{}
	for _, block := range strings.Split("\n"+string(data), "\n## ")[1:] {
		lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
		if len(lines) < 3 {
			t.Fatalf("case %q: wants a title, keys and text", lines[0])
		}
		c := nvimCase{title: lines[0], keys: lines[1], cur: Pos{-1, -1}}
		for _, l := range lines[2:] { // {1..100} is the lines 1 to 100
			var a, b int
			if n, _ := fmt.Sscanf(l, "{%d..%d}", &a, &b); n == 2 && strings.HasSuffix(l, "}") {
				for i := a; i <= b; i++ {
					c.text = append(c.text, strconv.Itoa(i))
				}
				continue
			}
			c.text = append(c.text, l)
		}
		if seen[c.title] {
			t.Fatalf("case %q twice", c.title)
		}
		seen[c.title] = true
		for i, l := range c.text {
			if j := strings.Index(l, cursorMark); j >= 0 {
				c.cur, c.text[i] = Pos{i, j}, l[:j]+l[j+len(cursorMark):]
			}
		}
		if c.cur.Line < 0 {
			t.Fatalf("case %q: no %s in the text", c.title, cursorMark)
		}
		cases = append(cases, c)
	}
	return cases
}

// result is how a case ends, the same for nvim and the editor: the text
// with the cursor marked, the unnamed register, the first line and, when
// scrolled sideways, the first column on screen, and the mode.
func result(lines []string, cur Pos, regType, reg string, top, left int, mode string) string {
	var b strings.Builder
	for i, l := range lines {
		if i == cur.Line {
			l = l[:cur.Col] + cursorMark + l[cur.Col:]
		}
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "reg: %q %q\ntop: %d\n", regType, reg, top+1)
	if left > 0 {
		fmt.Fprintf(&b, "left: %d\n", left)
	}
	fmt.Fprintf(&b, "mode: %s", mode)
	return b.String()
}

// splitPaste cuts keys at <Paste>, which a Go-quoted string follows: what
// the terminal pastes there. ok is false when keys paste nothing.
func splitPaste(t *testing.T, keys string) (before, paste, after string, ok bool) {
	before, rest, ok := strings.Cut(keys, "<Paste>")
	if !ok {
		return keys, "", "", false
	}
	q, err := strconv.QuotedPrefix(rest)
	if err != nil {
		t.Fatalf("%s: <Paste> wants a quoted string after it: %v", keys, err)
	}
	paste, _ = strconv.Unquote(q)
	return before, paste, rest[len(q):], true
}

func TestNvim(t *testing.T) {
	cases := readCases(t)
	if *update {
		writeGolden(t, cases)
	}
	golden := readGolden(t)
	for _, c := range cases {
		want, ok := golden[c.title]
		if !ok {
			t.Errorf("%s: not in cases.golden; run go generate ./internal/editor", c.title)
			continue
		}
		if got := run(t, c); got != want {
			t.Errorf("## %s\n%s\n%s\nnvim:\n%s\neditor:\n%s", c.title, c.keys, strings.Join(c.text, "\n"), want, got)
		}
	}
}

// keys splits vim notation into keys as the keymap sends them.
func keys(t *testing.T, s string) []string {
	ks, err := keymap.Parse(s)
	if err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}

func run(t *testing.T, c nvimCase) string {
	e := New("")
	e.lines, e.cur = append([]string(nil), c.text...), c.cur
	e.clampCursor() // as nvim's cursor() does
	e.SetHeight(nvimRows)
	e.SetWidth(nvimCols)
	before, paste, after, ok := splitPaste(t, c.keys)
	for _, k := range keys(t, before) {
		e.Feed(k)
	}
	if ok {
		e.Paste(paste)
		for _, k := range keys(t, after) {
			e.Feed(k)
		}
	}
	kind := ""
	switch e.reg.kind {
	case 0:
	case blockKind:
		kind = string(rune(blockKind)) + strconv.Itoa(e.reg.width)
	default:
		kind = string(e.reg.kind)
	}
	return result(e.lines, e.cur, kind, e.reg.text, e.top, e.left, map[Mode]string{Normal: "n", Insert: "i", Replace: "R", Visual: "v", VisualLine: "V", VisualBlock: "\x16", Command: "c"}[e.mode])
}

func readGolden(t *testing.T) map[string]string {
	data, err := os.ReadFile("testdata/cases.golden")
	if err != nil {
		t.Fatal(err)
	}
	golden := map[string]string{}
	for _, block := range strings.Split(string(data), "\n## ")[1:] {
		title, body, _ := strings.Cut(block, "\n")
		golden[title] = strings.TrimRight(body, "\n")
	}
	return golden
}

// writeGolden runs every case in nvim.
func writeGolden(t *testing.T, cases []nvimCase) {
	version, err := exec.Command("nvim", "--version").Output()
	if err != nil {
		t.Fatal("go generate needs nvim: ", err)
	}
	dir := t.TempDir()
	out := make([]string, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, c := range cases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = nvim(t, filepath.Join(dir, strconv.Itoa(i)), c)
		})
	}
	wg.Wait()
	var b bytes.Buffer
	fmt.Fprintf(&b, "# nvim %s, by go generate from cases.txt\n", strings.TrimSpace(strings.SplitN(string(version), "\n", 2)[0]))
	for i, c := range cases {
		fmt.Fprintf(&b, "\n## %s\n%s\n", c.title, out[i])
	}
	if err := os.WriteFile("testdata/cases.golden", b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// nvim runs case c with the options of tech-design §15 and feedkeys(…,
// 'xt'), so each command undoes on its own as when typed. A paste is
// nvim_paste() as the terminal's, run from a <Cmd> mapping so the mode
// stays what the keys before it left. wincol() scrolls sideways as a
// redraw would, which nothing after the last command did.
func nvim(t *testing.T, path string, c nvimCase) string {
	var text []string
	for _, l := range c.text {
		text = append(text, "'"+strings.ReplaceAll(l, "'", "''")+"'")
	}
	keys := vimKeys(c.keys)
	before, paste, after, ok := splitPaste(t, c.keys)
	if ok {
		keys = vimKeys(before) + `\<F12>` + vimKeys(after)
	}
	script := fmt.Sprintf(`set expandtab tabstop=2 shiftwidth=2 nowrap ignorecase smartcase
set commentstring=--\ %%s formatoptions-=j
set lines=24 columns=%d
set undolevels=-1
call setline(1, [%s])
set undolevels=1000
call cursor(%d, %d)
let g:paste = %s
noremap <F12> <Cmd>call nvim_paste(g:paste, v:true, -1)<CR>
noremap! <F12> <Cmd>call nvim_paste(g:paste, v:true, -1)<CR>
silent! call feedkeys("%s", 'xt')
let p = getpos('.')
let _ = wincol()
call writefile([p[1], p[2], getregtype('"'), line('w0'), winsaveview().leftcol, mode(), line('$')] + getline(1, '$') + split(getreg('"'), "\n", 1), %q)
qa!
`, nvimCols, strings.Join(text, ", "), c.cur.Line+1, c.cur.Col+1, strconv.Quote(paste), keys, path+".out")
	if err := os.WriteFile(path+".vim", []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("nvim", "--headless", "--clean", "--cmd", "set noloadplugins", "-S", path+".vim").CombinedOutput(); err != nil {
		t.Fatalf("%s: nvim: %v\n%s", c.title, err, out)
	}
	data, err := os.ReadFile(path + ".out")
	if err != nil {
		t.Fatalf("%s: %v", c.title, err)
	}
	ls := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	f := func(i int) int { n, _ := strconv.Atoi(ls[i]); return n }
	n := 7 + f(6)
	return result(ls[7:n], Pos{f(0) - 1, f(1) - 1}, ls[2], strings.Join(ls[n:], "\n"), f(3)-1, f(4), ls[5])
}

// vimKeys is keys as the inside of a vim "string": <Esc> becomes \<Esc>.
// A <…> the keymap reads as characters (\<ab\> in a pattern) stays
// characters, so nvim and the editor get the same keys.
func vimKeys(keys string) string {
	var b strings.Builder
	for keys != "" {
		if keys[0] == '<' {
			if end := strings.IndexByte(keys, '>'); end > 1 {
				if ks, err := keymap.Parse(keys[:end+1]); err == nil && len(ks) == 1 {
					b.WriteString(`\` + keys[:end+1])
					keys = keys[end+1:]
					continue
				}
			}
		}
		if keys[0] == '\\' || keys[0] == '"' {
			b.WriteByte('\\')
		}
		b.WriteByte(keys[0])
		keys = keys[1:]
	}
	return b.String()
}
