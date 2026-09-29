package keymap

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"sqlmux/internal/config"
)

func load(t *testing.T, toml string) (*Map, []Problem) {
	t.Helper()
	c, err := config.Parse(toml)
	if err != nil {
		t.Fatal(err)
	}
	return New(c)
}

func mustLoad(t *testing.T, toml string) *Map {
	t.Helper()
	m, ps := load(t, toml)
	if len(ps) > 0 {
		t.Fatalf("unexpected problems: %v", ps)
	}
	return m
}

var (
	grid    = Context{Focus: []string{"grid"}, Pane: "grid"}
	tree    = Context{Focus: []string{"tree"}, Pane: "tree"}
	console = Context{Focus: []string{"console"}, Pane: "console"}
	input   = Context{Focus: []string{"input"}, Mode: Insert}
)

// press feeds a vim-notation string and flattens what came out, feeding a
// mapping's right-hand side back without maps, as the app does.
func press(t *testing.T, r *Resolver, c Context, s string) (out []Result, wait bool) {
	t.Helper()
	ks, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	var feed func(k Key, maps bool)
	feed = func(k Key, maps bool) {
		res, w := r.Feed(c, k, maps)
		wait = w
		for _, x := range res {
			if x.Map == nil {
				out = append(out, x)
			}
			for _, k := range x.Map {
				feed(k, false)
			}
		}
	}
	for _, k := range ks {
		feed(k, true)
	}
	return out, wait
}

func actions(rs []Result) []string {
	var out []string
	for _, r := range rs {
		switch {
		case r.Action != "" && r.Count > 0:
			out = append(out, r.Action+" ×"+strconv.Itoa(r.Count))
		case r.Action != "":
			out = append(out, r.Action)
		default:
			out = append(out, "keys "+String(r.Keys))
		}
	}
	return out
}

func TestDefaultsLoadClean(t *testing.T) {
	mustLoad(t, "")
}

func TestSequences(t *testing.T) {
	m := mustLoad(t, "[keys.normal]\n\"<Leader>0\" = \"window.select 0\"")
	for _, c := range []struct {
		ctx  Context
		in   string
		want []string
	}{
		{grid, "j", []string{"grid.down"}},
		{grid, "gg", []string{"grid.top"}},
		{grid, "gt", []string{"tab.next"}}, // normal scope under grid
		{grid, "<Space>s", []string{"session.list"}},
		{grid, "<Space>0", []string{"window.select 0"}}, // digits after a prefix are keys
		{grid, "gz", []string{"keys gz"}},
		{grid, "<C-p>", []string{"palette.open"}},
		{console, "gg", []string{"keys gg"}}, // whole buffered prefix goes to vim
		{console, "gq", []string{"console.format"}},
		{console, "<CR>", []string{"console.run"}},
		{Context{Focus: []string{"result", "grid"}, Pane: "grid"}, "qj", []string{"result.close", "grid.down"}},
		{Context{Overlay: "palette", Focus: []string{"grid"}}, "<C-t>", []string{"palette.open.tab"}},
		{input, "j", []string{"keys j"}},
		{input, "<Space>", []string{"keys <Space>"}}, // SPC leader is NORMAL only
		{input, "<C-p>", []string{"palette.open"}},
		{Context{Focus: []string{"console"}, Pane: "console", Mode: Insert}, "<CR>gq", []string{"keys <CR>", "keys g", "keys q"}},
		{Context{Focus: []string{"cell"}, Mode: Insert}, "<Tab>", []string{"cell.segment.next"}},
		// COMMAND is typing too: no counts, no widget keys, no user maps
		{Context{Overlay: "palette", Mode: Command}, "5<CR>", []string{"keys 5", "palette.run"}},
	} {
		got, _ := press(t, NewResolver(m), c.ctx, c.in)
		if !reflect.DeepEqual(actions(got), c.want) {
			t.Errorf("%v %q: got %v, want %v", c.ctx.Focus, c.in, actions(got), c.want)
		}
	}
}

func TestPurePrefixWaits(t *testing.T) {
	r := NewResolver(mustLoad(t, ""))
	out, wait := press(t, r, grid, "<Space>")
	if len(out) != 0 || wait || Display(r.Pending()) != "SPC" {
		t.Fatalf("SPC: out %v wait %v pending %q", out, wait, Display(r.Pending()))
	}
	if got := r.Timeout(grid, r.Seq()); len(got) != 0 {
		t.Fatalf("a pure prefix must not time out: %v", got)
	}
	out, _ = press(t, r, grid, "%")
	if !reflect.DeepEqual(actions(out), []string{"pane.split.right"}) || len(r.Pending()) != 0 {
		t.Fatalf("SPC %%: %v, pending %v", actions(out), r.Pending())
	}
	press(t, r, grid, "<Space>")
	if out, _ := press(t, r, grid, "<Esc>"); len(out) != 0 || len(r.Pending()) != 0 {
		t.Fatalf("esc should cancel silently: %v %v", out, r.Pending())
	}
}

func TestAmbiguousNodeTimesOut(t *testing.T) {
	m, ps := load(t, "[keys.normal]\ng = \"my.g\"")
	if len(ps) != 1 || ps[0].Table != "keys.normal" || ps[0].Key != "g" {
		t.Fatalf("expected one prefix warning on g, got %v", ps)
	}
	r := NewResolver(m)
	out, wait := press(t, r, grid, "g")
	if len(out) != 0 || !wait {
		t.Fatalf("g: out %v wait %v", out, wait)
	}
	seq := r.Seq()
	if got := r.Timeout(grid, seq-1); len(got) != 0 {
		t.Fatalf("stale timeout fired: %v", got)
	}
	if got := r.Timeout(grid, seq); !reflect.DeepEqual(actions(got), []string{"my.g"}) {
		t.Fatalf("timeout: %v", actions(got))
	}
	if out, _ := press(t, r, grid, "gt"); !reflect.DeepEqual(actions(out), []string{"tab.next"}) {
		t.Fatalf("gt before the timeout: %v", actions(out))
	}
	if out, _ := press(t, r, grid, "gj"); !reflect.DeepEqual(actions(out), []string{"my.g", "grid.down"}) {
		t.Fatalf("g then an unrelated key: %v", actions(out))
	}
}

func TestCounts(t *testing.T) {
	m := mustLoad(t, "")
	for _, c := range []struct {
		ctx  Context
		in   string
		want []string
	}{
		{grid, "5j", []string{"grid.down ×5"}},
		{grid, "12k", []string{"grid.up ×12"}},
		{grid, "0", []string{"grid.first"}},
		{grid, "10j", []string{"grid.down ×10"}},
		{tree, "3j", []string{"tree.down ×3"}},
		{grid, "5<Esc>j", []string{"grid.down"}},
		{console, "5j", []string{"keys 5j"}}, // the editor does its own counting
		{input, "5", []string{"keys 5"}},
	} {
		got, _ := press(t, NewResolver(m), c.ctx, c.in)
		if !reflect.DeepEqual(actions(got), c.want) {
			t.Errorf("%v %q: got %v, want %v", c.ctx.Focus, c.in, actions(got), c.want)
		}
	}
	r := NewResolver(m)
	press(t, r, grid, "4")
	if Display(r.Pending()) != "4" {
		t.Errorf("pending count shows %q", Display(r.Pending()))
	}
}

func TestMappingPriority(t *testing.T) {
	m := mustLoad(t, `
[map.normal]
J = "5j"
[map.grid.normal]
J = "3j"
j = "jj"
[map.console.normal]
L = "5l"
`)
	for _, c := range []struct {
		ctx  Context
		in   string
		want []string
	}{
		{grid, "J", []string{"grid.down ×3"}},           // pane type beats generic
		{tree, "J", []string{"tree.down ×5"}},           // generic beats default
		{grid, "j", []string{"grid.down", "grid.down"}}, // noremap: rhs j is not remapped
		{grid, "2J", []string{"grid.down ×23"}},         // count goes in front, as in vim
		{console, "L", []string{"keys 5l"}},
		{input, "J", []string{"keys J"}},                                      // no maps in INSERT
		{Context{Overlay: "palette", Mode: Command}, "J", []string{"keys J"}}, // nor in COMMAND
	} {
		got, _ := press(t, NewResolver(m), c.ctx, c.in)
		if !reflect.DeepEqual(actions(got), c.want) {
			t.Errorf("%v %q: got %v, want %v", c.ctx.Focus, c.in, actions(got), c.want)
		}
	}
}

func TestCtrlLeaderWorksInInsert(t *testing.T) {
	m := mustLoad(t, "[keys]\nleader = \"<C-a>\"")
	for _, ctx := range []Context{input, grid, {Overlay: "palette", Mode: Command}} {
		got, _ := press(t, NewResolver(m), ctx, "<C-a>%")
		if !reflect.DeepEqual(actions(got), []string{"pane.split.right"}) {
			t.Errorf("%+v: got %v", ctx, actions(got))
		}
	}
	if got, _ := press(t, NewResolver(m), grid, "<Space>%"); actions(got)[0] == "pane.split.right" {
		t.Error("space must no longer be the leader")
	}
	if h := m.Hint("tree.toggle", "normal"); h != "C-a b" {
		t.Errorf("hint = %q", h)
	}
}

func TestHint(t *testing.T) {
	m := mustLoad(t, "[keys.console]\n\"<CR>\" = \"\"\n[keys.normal]\n\"<C-b>\" = \"tree.toggle\"")
	for _, c := range []struct{ action, scope, want string }{
		{"tree.toggle", "normal", "SPC b"}, // first binding in file order wins
		{"palette.open", "global", "C-p"},
		{"tab.next", "normal", "gt"},
		{"console.run", "console", ""}, // unbound: no hint
		{"nope", "normal", ""},
	} {
		if got := m.Hint(c.action, c.scope); got != c.want {
			t.Errorf("Hint(%s, %s) = %q, want %q", c.action, c.scope, got, c.want)
		}
	}
}

func TestProblems(t *testing.T) {
	_, ps := load(t, `
[keys.normal]
"<C-x>" = "a"
"<c-X>" = "b"
"<Nope>" = "c"
[keys.nowhere]
x = "d"
[keys.whichkey]
y = "e"
[keys]
leader = "<Bad>"
`)
	var got []string
	for _, p := range ps {
		got = append(got, p.Table+" "+p.Key)
	}
	want := []string{"keys leader", "keys.normal <c-X>", "keys.normal <Nope>", "keys.nowhere x", "keys.whichkey y"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !strings.Contains(ps[1].Msg, `"<C-x>"`) {
		t.Errorf("duplicate should name the other spelling: %s", ps[1])
	}
}

func TestTOMLRoundTrip(t *testing.T) {
	m := mustLoad(t, `
[keys]
leader = "<C-a>"
[keys.grid]
x = ""
"<C-w>v" = "pane.split.right"
[map.console.normal]
L = "5l"
`)
	titles := map[string]string{"tab.next": "下一个 tab", "pane.split.right": "左右分割", "grid.nope": "没绑的", "window.rename": "重命名 window"}
	out := m.TOML(titles)
	again := mustLoad(t, out)
	if again.TOML(titles) != out || !reflect.DeepEqual(again.tables, m.tables) {
		t.Fatalf("round trip changed the keymap:\n%s\n---\n%s", out, again.TOML(titles))
	}
	normal := out[strings.Index(out, "[keys.normal]"):strings.Index(out, "[keys.global]")]
	for _, want := range []string{
		"# 焦点在表格（NORMAL）\n[keys.grid]\n",
		`"gt" = "tab.next"  # 下一个 tab` + "\n",
		`"<C-w>v" = "pane.split.right"  # 左右分割` + "\n",
		`"<C-p>" = "palette.open"` + "\n", // untitled: no comment
		// an unbound default is exported as an unbind; a titled action bound
		// nowhere comes after, commented, in the scope its prefix names
		`"x" = ""` + "\n" + `# "" = "grid.nope"  # 没绑的` + "\n",
		`"L" = "5l"` + "\n",
		"# [map.grid.normal]\n# \"J\" = \"5j\"\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("toml lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(normal, `# "" = "window.rename"  # 重命名 window`) {
		t.Errorf("window.rename should be commented under [keys.normal]:\n%s", normal)
	}
	if !strings.Contains(m.Markdown(), "| normal | `C-a %` | pane.split.right |\n") {
		t.Errorf("markdown lacks the leader binding:\n%s", m.Markdown())
	}
}

// One key can mean one thing per pane type (§6.7), in keys and in maps alike.
func TestSameKeyPerPaneType(t *testing.T) {
	m := mustLoad(t, `
[keys.grid]
X = "grid.refresh"
[keys.tree]
X = "tree.refresh"
[map.grid.normal]
Z = "5j"
[map.tree.normal]
Z = "3j"
`)
	for _, c := range []struct {
		ctx  Context
		in   string
		want []string
	}{
		{grid, "X", []string{"grid.refresh"}},
		{tree, "X", []string{"tree.refresh"}},
		{grid, "Z", []string{"grid.down ×5"}},
		{tree, "Z", []string{"tree.down ×3"}},
	} {
		got, _ := press(t, NewResolver(m), c.ctx, c.in)
		if !reflect.DeepEqual(actions(got), c.want) {
			t.Errorf("%v %q: got %v, want %v", c.ctx.Focus, c.in, actions(got), c.want)
		}
	}
}

// §6.2: default keys must work in any terminal — no Alt, no Ctrl+Shift, no
// modified Enter, no Ctrl+digit, and no C-i/C-m/C-[ (they are Tab/CR/Esc).
func TestDefaultsArePortable(t *testing.T) {
	ctrl := regexp.MustCompile(`^<C-([a-z])>$`)
	named := regexp.MustCompile(`^<(S-Tab|Space|CR|Esc|Tab|BS|Up|Down|Left|Right|lt|Leader)>$`)
	m := mustLoad(t, "")
	for table, bs := range m.tables {
		for _, b := range bs {
			for _, k := range b.Keys {
				s := string(k)
				switch sub := ctrl.FindStringSubmatch(s); {
				case len([]rune(s)) == 1 || named.MatchString(s):
				case sub != nil && sub[1] != "i" && sub[1] != "m":
					if sub[1] == "h" && table != "keys.normal" {
						t.Errorf("[%s] %s: C-h is Backspace in some terminals, NORMAL only", table, String(b.Keys))
					}
				default:
					t.Errorf("[%s] %s: %s is not portable", table, String(b.Keys), s)
				}
			}
		}
	}
}

// The UI shows "SPC s", so users write "<Space>s": that must rebind and
// unbind the default "<Leader>s" like any other key.
func TestLeaderSpelledOut(t *testing.T) {
	m := mustLoad(t, "[keys.normal]\n\"<Space>s\" = \"foo\"\n\"<Space>b\" = \"\"")
	for in, want := range map[string][]string{"<Space>s": {"foo"}, "<Space>b": {"keys <Space>b"}} {
		if got, _ := press(t, NewResolver(m), grid, in); !reflect.DeepEqual(actions(got), want) {
			t.Errorf("%s: got %v, want %v", in, actions(got), want)
		}
	}
	if h := m.Hint("session.list", "normal"); h != "" {
		t.Errorf("rebound session.list still hints %q", h)
	}
	again := mustLoad(t, m.TOML(nil))
	if !reflect.DeepEqual(again.tables, m.tables) {
		t.Errorf("round trip lost the rebinding:\n%s", m.TOML(nil))
	}
	if _, ps := load(t, "[keys.normal]\n\"<Leader>s\" = \"a\"\n\"<Space>s\" = \"b\""); len(ps) != 1 {
		t.Errorf("<Leader>s and <Space>s are the same key: %v", ps)
	}
}

func TestLiteralSpaceLeader(t *testing.T) {
	m := mustLoad(t, "[keys]\nleader = \" \"")
	if got, _ := press(t, NewResolver(m), grid, "<Space>s"); !reflect.DeepEqual(actions(got), []string{"session.list"}) {
		t.Errorf("got %v", actions(got))
	}
}

func TestMappingExpandsLeader(t *testing.T) {
	m := mustLoad(t, "[map.normal]\nx = \"<Leader>s\"")
	if got, _ := press(t, NewResolver(m), grid, "x"); !reflect.DeepEqual(actions(got), []string{"session.list"}) {
		t.Errorf("got %v", actions(got))
	}
}

func TestNext(t *testing.T) {
	m := mustLoad(t, "[map.normal]\n\"<Space>y\" = \"5j\"")
	r := NewResolver(m)
	if len(r.Next()) != 0 {
		t.Fatal("nothing pending, nothing next")
	}
	press(t, r, grid, "<Space>")
	next := r.Next()
	var keys []string
	for _, n := range next {
		keys = append(keys, string(n.Key))
	}
	if strings.Join(keys, "") != "y"+"scnpl%\"zxqb" {
		t.Errorf("SPC next keys in binding order: %q", strings.Join(keys, ""))
	}
	if next[0].RHS == nil || next[1].Action != "session.list" {
		t.Errorf("entries: %+v %+v", next[0], next[1])
	}
	press(t, r, grid, "g") // not bound after SPC: the sequence ends
	if len(r.Next()) != 0 {
		t.Error("Next after the sequence ended")
	}
	press(t, r, grid, "g")
	if n := r.Next(); len(n) != 7 || n[0].Key != "g" || n[5].Key != "t" { // grid's g-keys first, then normal's
		t.Errorf("g next: %+v", n)
	}
}
