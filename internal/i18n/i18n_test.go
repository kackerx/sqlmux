package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// text is what a string needing translation is made of: Han, and the
// punctuation of a Chinese text, ideographic and full-width, which shows in
// English as it is (、 ： ；).
var text = regexp.MustCompile(`[\p{Han}\x{3000}-\x{303F}\x{FF00}-\x{FFEF}]`)

// literal is a string or character of the program's code with a place.
type literal struct{ rel, fn, text string }

// scan is every key of i18n.T and i18n.N, with how many arguments the
// formatting takes for it, and the literals needing translation that are
// not one: only a bare string as the first argument of T or N is a key, any
// other argument of theirs is as outside as anything.
func scan(t *testing.T) (keys map[string][]int, outside []literal) {
	t.Helper()
	keys = map[string][]int{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel) // the prefix checks below compare slashed paths on every GOOS
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasPrefix(rel, "internal/i18n") || strings.HasPrefix(rel, "e2e") {
			return nil //nolint:nilerr // an entry that cannot be read is skipped, the walk goes on
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		scanFile(rel, f, keys, &outside)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys, outside
}

// scanFile adds what file f, at rel, holds to keys and outside.
func scanFile(rel string, f *ast.File, keys map[string][]int, outside *[]literal) {
	isCall := func(c *ast.CallExpr, pkg string, names ...string) bool {
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, _ := sel.X.(*ast.Ident)
		return id != nil && id.Name == pkg && slices.Contains(names, sel.Sel.Name)
	}
	for _, decl := range f.Decls {
		fn := ""
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		// fmt.Sprintf(T(k), args...) and fmt.Errorf(T(k), args...): the arguments are the outer call's
		outer := map[*ast.CallExpr]int{}
		ast.Inspect(decl, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isCall(c, "fmt", "Sprintf", "Errorf") && len(c.Args) > 0 {
				if in, ok := c.Args[0].(*ast.CallExpr); ok && isCall(in, "i18n", "T") {
					outer[in] = len(c.Args) - 1
					if c.Ellipsis.IsValid() { // fmt.Sprintf(T(k), args...): not counted
						outer[in] = -1
					}
				}
			}
			return true
		})
		key := map[*ast.BasicLit]bool{} // the first argument of a T or N, when it is a bare string
		ast.Inspect(decl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if lit, ok := firstArg(x).(*ast.BasicLit); ok && isCall(x, "i18n", "T", "N") && lit.Kind == token.STRING {
					key[lit] = true
					if s, _ := strconv.Unquote(lit.Value); text.MatchString(s) {
						n := len(x.Args) - 1
						if o, ok := outer[x]; ok {
							n = o
						}
						if x.Ellipsis.IsValid() { // T(k, args...): not counted
							n = -1
						}
						keys[s] = append(keys[s], n)
					}
				}
			case *ast.BasicLit:
				if s, _ := strconv.Unquote(x.Value); !key[x] && (x.Kind == token.STRING || x.Kind == token.CHAR) && text.MatchString(s) {
					*outside = append(*outside, literal{rel, fn, s})
				}
			}
			return true
		})
	}
}

func firstArg(c *ast.CallExpr) ast.Expr {
	if len(c.Args) == 0 {
		return nil
	}
	return c.Args[0]
}

// Every Chinese text the program shows goes through T or N (§14「界面语言」).
func TestNoChineseOutsideT(t *testing.T) {
	_, outside := scan(t)
	for _, l := range outside {
		t.Errorf("not translated: %s %s: %q", l.rel, l.fn, l.text)
	}
}

// The English table has every text T and N are given, and nothing else,
// each one taking the arguments its Chinese does; and a call gives T as many
// arguments as its text takes.
func TestEnglishTableCoversAll(t *testing.T) {
	keys, _ := scan(t)
	for k, uses := range keys {
		if en[k] == "" {
			t.Errorf("no English for %q", k)
		}
		for _, n := range uses {
			if n >= 0 && n != len(verbs(k)) {
				t.Errorf("%q takes %d arguments and is given %d", k, len(verbs(k)), n)
			}
		}
	}
	for k, v := range en {
		if len(keys[k]) == 0 {
			t.Errorf("English for %q, which nothing shows", k)
		}
		if !slices.Equal(verbs(k), verbs(v)) {
			t.Errorf("%q: verbs %v, English %v", k, verbs(k), verbs(v))
		}
		if text.MatchString(v) {
			t.Errorf("%q: the English has Chinese or full-width marks in it: %q", k, v)
		}
	}
}

// verbs is the formatting verbs of s in order.
func verbs(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(s) && strings.IndexByte("+-# 0123456789.*[]", s[j]) >= 0 {
			j++
		}
		if j < len(s) && s[j] != '%' {
			out = append(out, s[i:j+1])
		}
		i = j
	}
	return out
}

func TestT(t *testing.T) {
	en["测试 %d"] = "test %d"
	defer delete(en, "测试 %d")
	defer Set("en")
	if got := T("测试 %d", 3); got != "test 3" { // before any Set: F7.6
		t.Errorf("default: %q", got)
	}
	Set("zh")
	if got := T("测试 %d", 3); got != "测试 3" {
		t.Errorf("zh: %q", got)
	}
	Set("en")
	if got := T("测试 %d", 3); got != "test 3" {
		t.Errorf("en: %q", got)
	}
	if got := T("没有这条"); got != "没有这条" {
		t.Errorf("missing: %q", got)
	}
	if got := verbs("%s 有 %d %% 个 %-5.2f %v"); !slices.Equal(got, []string{"%s", "%d", "%-5.2f", "%v"}) {
		t.Errorf("verbs: %v", got)
	}
}

// The scan sees what a text is made of: a key is a bare string first
// argument; a prefix, a Sprintf, a second argument, a rune and a full-width
// mark are outside it.
func TestScanSeesWhatItShould(t *testing.T) {
	src := `package x
import ("fmt"; "github.com/kackerx/sqlmux/internal/i18n")
func f(x string) {
	_ = i18n.T("前缀" + x)
	_ = i18n.T(fmt.Sprintf("中文 %s", x))
	_ = i18n.T("%s 的", "参数里的中文")
	_ = i18n.N("x" + "：")
	_ = '汉'
	_ = "，"
	_ = i18n.T("是 key")
	_ = fmt.Sprintf(i18n.T("两个 %s %s"), x)
}
func g() {
	_ = "这句"
	_ = fmt.Sprintf(i18n.T("展开 %s %s"), []any{1, 2}...)
}
`
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string][]int{}
	var outside []literal
	scanFile("x.go", f, keys, &outside)
	var got []string
	for _, l := range outside {
		got = append(got, l.text)
	}
	if want := []string{"前缀", "中文 %s", "参数里的中文", "：", "汉", "，", "这句"}; !slices.Equal(got, want) {
		t.Errorf("outside %q, want %q", got, want)
	}
	if len(keys["展开 %s %s"]) != 1 || keys["展开 %s %s"][0] != -1 {
		t.Errorf("a spread is not counted: %v", keys["展开 %s %s"])
	}
	if !slices.Equal(keys["是 key"], []int{0}) || !slices.Equal(keys["%s 的"], []int{1}) || !slices.Equal(keys["两个 %s %s"], []int{1}) {
		t.Errorf("keys %v", keys)
	}
}
