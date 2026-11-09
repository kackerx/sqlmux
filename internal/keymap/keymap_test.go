package keymap

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// The tests below read the Chinese texts: the language they run in is not the
// default, English (F7.6).
func init() { i18n.Set("zh") }

// load builds the keymap with toml as the user's keymaps.toml.
func load(t *testing.T, toml string) (*Map, []Problem) {
	t.Helper()
	leader, lines, err := ParseKeys(toml)
	if err != nil {
		t.Fatal(err)
	}
	return New(leader, true, 1000, lines)
}

func mustLoad(t *testing.T, toml string) *Map {
	t.Helper()
	m, ps := load(t, toml)
	if len(ps) > 0 {
		t.Fatalf("unexpected problems: %v", ps)
	}
	return m
}

// loadNoVim is mustLoad with vim = false, default-novim.toml over the defaults.
func loadNoVim(t *testing.T, toml string) *Map {
	t.Helper()
	leader, lines, err := ParseKeys(toml)
	if err != nil {
		t.Fatal(err)
	}
	m, ps := New(leader, false, 1000, lines)
	if len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	return m
}

var (
	grid  = Context{Focus: []string{"grid"}, Pane: "grid"}
	tree  = Context{Focus: []string{"tree"}, Pane: "tree"}
	query = Context{Focus: []string{"query"}, Pane: "query"}
	input = Context{Focus: []string{"input"}, Mode: Insert}
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
			if x.Again != "" {
				feed(x.Again, maps)
			} else if x.Map == nil {
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
		{query, "gg", []string{"keys gg"}}, // whole buffered prefix goes to vim
		{query, "gq", []string{"keys gq"}}, // the editor's operator (§9.5)
		{query, "<CR>", []string{"query.run"}},
		{Context{Focus: []string{"result", "grid"}, Pane: "grid"}, "qj", []string{"result.close", "grid.down"}},
		{Context{Overlay: "palette", Focus: []string{"grid"}}, "<C-t>", []string{"palette.open.tab"}},
		{input, "j", []string{"keys j"}},
		{input, "<Space>", []string{"keys <Space>"}}, // SPC leader is NORMAL only
		{input, "<C-p>", []string{"palette.open"}},
		{Context{Focus: []string{"query"}, Pane: "query", Mode: Insert}, "<CR>gq", []string{"keys <CR>", "keys g", "keys q"}},
		{Context{Focus: []string{"cell"}, Mode: Insert}, "<Tab>", []string{"keys <Tab>"}}, // the floats' keys (§10.2)
		{Context{Overlay: "segments", Focus: []string{"cell"}, Mode: Insert}, "<Tab>", []string{"cell.segment.next"}},
		{Context{Overlay: "options", Focus: []string{"cell"}, Mode: Insert}, "<Tab>", []string{"cell.option.next"}},
		{Context{Overlay: "newrow", Under: "options", Focus: []string{"cell"}, Mode: Insert}, "<Tab><Down>", []string{"cell.field.next", "cell.option.next"}},
		// the modeless editor (§6.9): Ctrl keys of the pane and NORMAL apply, the keys that are text do not
		{Context{Focus: []string{"query"}, Pane: "query", Mode: Insert, Plain: true}, "<C-h>", []string{"keys <C-h>"}}, // Backspace there
		{Context{Focus: []string{"query"}, Pane: "query", Mode: Insert, Plain: true}, "<C-j>", []string{"pane.focus.down"}},
		{Context{Focus: []string{"query"}, Pane: "query", Mode: Insert, Plain: true}, "<C-p>", []string{"palette.open"}},
		{Context{Focus: []string{"query"}, Pane: "query", Mode: Insert, Plain: true}, "<CR>", []string{"keys <CR>"}},
		{Context{Focus: []string{"query"}, Pane: "query", Mode: Insert, Plain: true}, "<Space>sgt", []string{"keys <Space>", "keys s", "keys g", "keys t"}},
		{Context{Focus: []string{"input"}, Mode: Insert, Plain: true}, "<C-l>?", []string{"pane.focus.right", "keys ?"}},
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
	if got := r.Timeout(r.Seq()); len(got) != 0 {
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

// A Firm scope's complete binding does not wait for a longer sequence of a
// lower scope that starts with it: select's d beside grid's dd, y beside yy,
// yl, yi. Without the scope as Firm they wait, as any shorter binding does;
// what the Firm scope does not bind is as it was (F10.7).
func TestFirmScope(t *testing.T) {
	m := mustLoad(t, "")
	sel := Context{Focus: []string{"select", "grid"}, Pane: "grid"}
	firm := sel
	firm.Firm = "select"
	for _, c := range []struct{ key, want string }{{"d", "grid.select.delete"}, {"y", "grid.select.yank"}} {
		if out, wait := press(t, NewResolver(m), firm, c.key); wait || !reflect.DeepEqual(actions(out), []string{c.want}) {
			t.Errorf("firm %s: %v wait %v", c.key, actions(out), wait)
		}
		if out, wait := press(t, NewResolver(m), sel, c.key); len(out) != 0 || !wait {
			t.Errorf("not firm %s: %v wait %v", c.key, actions(out), wait)
		}
	}
	for in, want := range map[string][]string{"gg": {"grid.top"}, "j": {"grid.down"}, "<Esc>": {"grid.select.clear"}} {
		if out, _ := press(t, NewResolver(m), firm, in); !reflect.DeepEqual(actions(out), want) {
			t.Errorf("firm %s: %v, want %v", in, actions(out), want)
		}
	}
	// a key of a lower scope that does not start with a firm one stays
	if out, _ := press(t, NewResolver(m), firm, "T"); !reflect.DeepEqual(actions(out), []string{"grid.transpose"}) {
		t.Errorf("T: %v", actions(out))
	}
}

// Only the Firm scope's bindings stay complete: what the user adds to the
// select scope itself is a longer sequence as any, d and dd ambiguous, yj
// instead of y and then j; a longer one a lower scope adds under a binding of
// another scope hangs, T waiting for Tx (F10.7).
func TestFirmOnlyOverLowerScopes(t *testing.T) {
	m := mustLoad(t, "[keys.select]\n\"dd\" = \"grid.select.delete\"\n\"yj\" = \"grid.down\"\n[keys.normal]\n\"Tx\" = \"grid.up\"\n")
	firm := Context{Focus: []string{"select", "grid"}, Pane: "grid", Firm: "select"}
	if out, wait := press(t, NewResolver(m), firm, "d"); len(out) != 0 || !wait {
		t.Errorf("d with dd beside it: %v wait %v", actions(out), wait)
	}
	if out, _ := press(t, NewResolver(m), firm, "dd"); !reflect.DeepEqual(actions(out), []string{"grid.select.delete"}) {
		t.Errorf("dd: %v", actions(out))
	}
	if out, _ := press(t, NewResolver(m), firm, "yj"); !reflect.DeepEqual(actions(out), []string{"grid.down"}) {
		t.Errorf("yj: %v", actions(out))
	}
	if out, wait := press(t, NewResolver(m), firm, "T"); len(out) != 0 || !wait {
		t.Errorf("T with Tx under it: %v wait %v", actions(out), wait)
	}
}

func TestAmbiguousNodeTimesOut(t *testing.T) {
	m, ps := load(t, "[keys.normal]\ng = \"my.g\"")
	if len(ps) != 0 {
		t.Fatalf("g is the prefix of gt: no warning, got %v", ps)
	}
	r := NewResolver(m)
	out, wait := press(t, r, grid, "g")
	if len(out) != 0 || !wait {
		t.Fatalf("g: out %v wait %v", out, wait)
	}
	seq := r.Seq()
	if got := r.Timeout(seq - 1); len(got) != 0 {
		t.Fatalf("stale timeout fired: %v", got)
	}
	if got := r.Timeout(seq); !reflect.DeepEqual(actions(got), []string{"my.g"}) {
		t.Fatalf("timeout: %v", actions(got))
	}
	if out, _ := press(t, r, grid, "gt"); !reflect.DeepEqual(actions(out), []string{"tab.next"}) {
		t.Fatalf("gt before the timeout: %v", actions(out))
	}
	if out, _ := press(t, r, grid, "gk"); !reflect.DeepEqual(actions(out), []string{"my.g", "grid.up"}) {
		t.Fatalf("g then an unrelated key: %v", actions(out))
	}
}

// A key bound and the first of longer sequences too, t with tc and tt: the
// next key that continues none of them runs t first and is read afresh, as
// nvim does, esc too, so t needs no timeout to run; one that continues one is that
// sequence; a pause runs t (F8.15, §6.5).
func TestBoundPrefixRunsWhenTheNextKeyDoesNotContinue(t *testing.T) {
	m, ps := load(t, "[keys.normal]\nt = \"my.t\"\ntc = \"my.tc\"\ntt = \"my.tt\"")
	if len(ps) != 0 { // vim's ordinary case: no notice at start, nothing for keys --check
		t.Fatalf("a key and a sequence it starts: %v", ps)
	}
	for _, c := range []struct {
		keys string
		want []string
	}{
		{"tz", []string{"my.t", "keys z"}},    // z binds nothing there: the widget gets it
		{"tx", []string{"my.t", "tab.close"}}, // x binds something: t first, then it
		{"tc", []string{"my.tc"}},
		{"tt", []string{"my.tt"}},
		{"tj", []string{"my.t", "grid.down"}},
		{"t<Esc>", []string{"my.t", "pane.error.close"}}, // esc continues nothing either: t runs, esc is read afresh
	} {
		r := NewResolver(m)
		out, _ := press(t, r, grid, c.keys)
		if got := actions(out); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.keys, got, c.want)
		}
	}
	r := NewResolver(m)
	out, wait := press(t, r, grid, "t")
	if len(out) != 0 || !wait {
		t.Fatalf("t waits: %v %v", out, wait)
	}
	if got := r.Timeout(r.Seq()); !reflect.DeepEqual(actions(got), []string{"my.t"}) {
		t.Errorf("a pause: %v", actions(got))
	}
	// the key that ends it is not read in the trie t was found in: Feed
	// hands it back, for the caller to read once t has run
	r = NewResolver(m)
	r.Feed(grid, "t", true)
	if out, _ := r.Feed(grid, "x", true); len(out) != 2 || out[0].Action != "my.t" || out[1].Again != "x" {
		t.Errorf("tx: %+v", out)
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
		{query, "5j", []string{"keys 5j"}}, // the editor does its own counting
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
	// a dropdown opened by a click meanwhile: its esc is its own, typing
	drop := Context{Overlay: "dropdown", Focus: []string{"grid"}, Pane: "grid", Mode: Command}
	if got, _ := press(t, r, drop, "<Esc>"); len(got) != 1 || got[0].Action != "dropdown.close" {
		t.Errorf("esc in a dropdown, a count typed before: %v", actions(got))
	}
}

func TestMappingPriority(t *testing.T) {
	m := mustLoad(t, `
[map.normal]
J = "5j"
[map.grid.normal]
J = "3j"
j = "jj"
[map.query.normal]
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
		{query, "L", []string{"keys 5l"}},
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
	m := mustLoad(t, "[keys.query]\n\"<CR>\" = \"\"\n[keys.normal]\n\"<C-b>\" = \"tree.toggle\"")
	for _, c := range []struct{ action, scope, want string }{
		{"tree.toggle", "normal", "SPC b"}, // first binding in file order wins
		{"palette.open", "global", "C-p"},
		{"tab.next", "normal", "gt"},
		{"query.run", "query", ""}, // unbound: no hint
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

// The export keeps the user's changes live, each in its default's place,
// and gives the same keymap loaded back (§6.7).
func TestTOMLRoundTrip(t *testing.T) {
	m := mustLoad(t, `
[keys]
leader = "<C-a>"
[keys.grid]
x = ""
"<C-w>v" = "pane.split.right"
R = "grid.page.prev"
[map.query.normal]
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
		"[keys]\nleader = \"<C-a>\"\n",
		"# 焦点在表格（NORMAL）\n[keys.grid]\n",
		`# "gt" = "tab.next"  # 下一个 tab` + "\n",
		`# "<C-p>" = "palette.open"` + "\n", // untitled: no comment
		// a rebinding and an unbinding in the default's place, the user's
		// own keys after the defaults, then a titled action bound nowhere,
		// commented, in the scope its prefix names
		`# "dd" = "grid.row.delete"` + "\n" + `"R" = "grid.page.prev"` + "\n" + `# "T" = "grid.transpose"` + "\n" + `"x" = ""` + "\n",
		`"<C-w>v" = "pane.split.right"  # 左右分割` + "\n" + `# "" = "grid.nope"  # 没绑的` + "\n",
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

// With nothing changed the export is all comments but its table headers,
// so loaded back it is the defaults; a line taken out of its comment and
// given another action is a rebinding, the rest staying comments when
// exported again (§6.7, F5.9).
func TestTOMLDefaultsCommented(t *testing.T) {
	m := mustLoad(t, "")
	out := m.TOML(map[string]string{"tab.next": "下一个 tab"})
	for _, l := range strings.Split(out, "\n") {
		if l != "" && !strings.HasPrefix(l, "# ") && !regexp.MustCompile(`^\[[a-z.]+\]$`).MatchString(l) {
			t.Errorf("a live line: %q", l)
		}
	}
	if !reflect.DeepEqual(mustLoad(t, out).tables, m.tables) {
		t.Error("loaded back, the defaults changed")
	}
	mine := strings.Replace(out, `# "gt" = "tab.next"`, `"gt" = "tab.prev"`, 1)
	again := mustLoad(t, mine)
	if again.Hint("tab.prev", "normal") != "gt" || again.Hint("tab.next", "normal") != "" {
		t.Errorf("gt rebound: tab.prev %q, tab.next %q", again.Hint("tab.prev", "normal"), again.Hint("tab.next", "normal"))
	}
	if live := regexp.MustCompile(`(?m)^"`).FindAllString(again.TOML(nil), -1); len(live) != 1 {
		t.Errorf("%d live lines, want gt's", len(live))
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
//
// With vim = false the layer default-novim.toml is among them, and may use
// PgUp, PgDn and Delete, which are what someone who does not use vim has to
// hand (§6.9); only the layer has them.
func TestDefaultsArePortable(t *testing.T) {
	ctrl := regexp.MustCompile(`^<C-([a-z])>$`)
	named := regexp.MustCompile(`^<(S-Tab|Space|CR|Esc|Tab|BS|Up|Down|Left|Right|lt|Leader)>$`)
	layer := regexp.MustCompile(`^<(PageUp|PageDown|Del)>$`)
	for _, vim := range []bool{true, false} {
		m := mustLoad(t, "")
		if !vim {
			m = loadNoVim(t, "")
		}
		for table, bs := range m.tables {
			for _, b := range bs {
				for _, k := range b.Keys {
					s := string(k)
					switch sub := ctrl.FindStringSubmatch(s); {
					case len([]rune(s)) == 1 || named.MatchString(s):
					case !vim && layer.MatchString(s):
					case len(sub) > 1 && sub[1] != "i" && sub[1] != "m":
						if sub[1] == "h" && table != "keys.normal" {
							t.Errorf("[%s] %s: C-h is Backspace in some terminals, NORMAL only", table, String(b.Keys))
						}
					default:
						t.Errorf("vim = %v [%s] %s: %s is not portable", vim, table, String(b.Keys), s)
					}
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
	if strings.Join(keys, "") != "y"+"s)(Lcnpl%\"zxqb><lt>r?wtoe" {
		t.Errorf("SPC next keys in binding order: %q", strings.Join(keys, ""))
	}
	if next[0].RHS == nil || next[0].Table != "map.normal" || next[1].Action != "session.list" || next[1].Table != "keys.normal" {
		t.Errorf("entries: %+v %+v", next[0], next[1])
	}
	press(t, r, grid, "g") // not bound after SPC: the sequence ends
	if len(r.Next()) != 0 {
		t.Error("Next after the sequence ended")
	}
	press(t, r, grid, "g")
	if n := r.Next(); len(n) != 12 || n[0].Key != "g" || n[10].Key != "t" { // grid's g-keys first, then normal's
		t.Errorf("g next: %+v", n)
	}
}

// Map.Next is a level of a context's keys for the ? help: grouped by the
// table each comes from in scope order, a prefix in the highest table
// that has it, nothing a higher table hides repeated (§6.5).
func TestMapNext(t *testing.T) {
	m := mustLoad(t, "[map.grid.normal]\nJ = \"5j\"\n[keys.normal]\ngz = \"pane.zoom\"\nj = \"pane.close\"")
	var got []string
	for _, n := range m.Next(grid, nil) {
		if n.Key == "J" || n.Key == "j" || n.Key == "g" || n.Key == "<C-h>" || n.Key == "<C-p>" {
			got = append(got, string(n.Key)+" "+n.Table)
		}
	}
	if want := []string{"J map.grid.normal", "j keys.grid", "g keys.grid", "<C-h> keys.normal", "<C-p> keys.global"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top level: %v", got)
	}
	got = nil
	for _, n := range m.Next(grid, []Key{"g"}) {
		got = append(got, string(n.Key)+" "+n.Table)
	}
	if want := []string{"g keys.grid", "o keys.grid", "l keys.grid", "p keys.grid", "c keys.grid", "e keys.grid", "s keys.grid", "S keys.grid", "j keys.grid", "i keys.grid", "t keys.normal", "T keys.normal", "z keys.normal"}; !reflect.DeepEqual(got, want) {
		t.Errorf("g: %v", got)
	}
	if m.Next(grid, []Key{"x", "y"}) != nil {
		t.Error("past what it binds: nothing")
	}
}

// vim = false lays default-novim.toml over the defaults: PgUp, PgDn and Delete
// in the grid, ↵ unbound in a query and C-e running it; with vim on none of it
// is there. The arrow keys of the grid and the tree are in both (§6.9, F6.6b,
// F16.6).
func TestNoVimLayer(t *testing.T) {
	plainQuery := Context{Focus: []string{"query"}, Pane: "query", Mode: Insert, Plain: true}
	off, on := mustLoad(t, ""), loadNoVim(t, "")
	for _, c := range []struct {
		ctx     Context
		in      string
		off, on []string
	}{
		{grid, "<Down>", []string{"grid.down"}, []string{"grid.down"}},
		{grid, "<Right>", []string{"grid.right"}, []string{"grid.right"}},
		{grid, "<Left>", []string{"grid.left"}, []string{"grid.left"}},
		{grid, "<Up>", []string{"grid.up"}, []string{"grid.up"}},
		{grid, "<PageDown>", []string{"keys <PageDown>"}, []string{"grid.page.next"}},
		{grid, "<PageUp>", []string{"keys <PageUp>"}, []string{"grid.page.prev"}},
		{grid, "<Del>", []string{"keys <Del>"}, []string{"grid.row.delete"}},
		{grid, "j", []string{"grid.down"}, []string{"grid.down"}}, // the letters stay
		{tree, "<Up>", []string{"tree.up"}, []string{"tree.up"}},
		{tree, "<Left>", []string{"tree.collapse"}, []string{"tree.collapse"}},
		{tree, "<Down>", []string{"tree.down"}, []string{"tree.down"}},
		{tree, "<Right>", []string{"tree.expand"}, []string{"tree.expand"}},
		{plainQuery, "<C-e>", []string{"keys <C-e>"}, []string{"query.run"}},
		{plainQuery, "<CR>", []string{"keys <CR>"}, []string{"keys <CR>"}},
		{query, "<CR>", []string{"query.run"}, []string{"keys <CR>"}},
	} {
		if got, _ := press(t, NewResolver(off), c.ctx, c.in); !reflect.DeepEqual(actions(got), c.off) {
			t.Errorf("vim on, %v %q: got %v, want %v", c.ctx.Focus, c.in, actions(got), c.off)
		}
		if got, _ := press(t, NewResolver(on), c.ctx, c.in); !reflect.DeepEqual(actions(got), c.on) {
			t.Errorf("vim off, %v %q: got %v, want %v", c.ctx.Focus, c.in, actions(got), c.on)
		}
	}
	// the hints name the layer's keys and the arrows first, and the vim ones without it
	for _, c := range []struct{ action, scope, off, on string }{
		{"grid.down", "grid", "j", "↓"}, {"tree.up", "tree", "k", "↑"}, {"grid.page.next", "grid", "]", "PageDown"}, {"grid.row.delete", "grid", "dd", "Del"},
	} {
		if got := off.Hint(c.action, c.scope); got != c.off {
			t.Errorf("vim on, hint of %s: %q, want %q", c.action, got, c.off)
		}
		if got := on.Hint(c.action, c.scope); got != c.on {
			t.Errorf("vim off, hint of %s: %q, want %q", c.action, got, c.on)
		}
	}
	// one of them rebound is an ordinary remap, no conflict; the export has them as defaults
	m := loadNoVim(t, "[keys.grid]\n\"<PageDown>\" = \"grid.bottom\"\n")
	if got, _ := press(t, NewResolver(m), grid, "<PageDown>"); !reflect.DeepEqual(actions(got), []string{"grid.bottom"}) {
		t.Errorf("rebound: %v", actions(got))
	}
	if toml := on.TOML(map[string]string{}); !strings.Contains(toml, "# \"<Down>\" = \"grid.down\"") || !strings.Contains(toml, "# \"<C-e>\" = \"query.run\"") || !strings.Contains(off.TOML(map[string]string{}), "# \"<Down>\" = \"grid.down\"") {
		t.Errorf("the export has them as defaults:\n%s", toml)
	}
	if !strings.Contains(on.TOML(nil), "query 的编辑器，只有 Ctrl 开头的键生效，C-h 除外") || strings.Contains(off.TOML(nil), "query 的编辑器") {
		t.Error("the query's note tells of the editor with vim = false only")
	}
}

// The tables of before F10.11 are no tables now, as any misspelt one is: no
// alias (F10.11).
func TestOldConsoleTables(t *testing.T) {
	for _, toml := range []string{"[keys.console]\n\"x\" = \"query.run\"\n", "[map.console.normal]\n\"x\" = \"j\"\n"} {
		if _, ps := load(t, toml); len(ps) != 1 || !strings.Contains(ps[0].String(), "没有这个表") {
			t.Errorf("%q: %v", toml, ps)
		}
	}
}

func TestParseKeysInFileOrder(t *testing.T) {
	leader, bs, err := ParseKeys(`
[keys]
leader = "<C-a>"
[keys.normal]
"<C-w>v" = "pane.split.right"
x = ""
[map.normal]
J = "5j"
[map.query.normal]
L = "5l"
`)
	want := []Line{
		{"keys.normal", "<C-w>v", "pane.split.right"},
		{"keys.normal", "x", ""},
		{"map.normal", "J", "5j"},
		{"map.query.normal", "L", "5l"},
	}
	if err != nil || leader != "<C-a>" || !reflect.DeepEqual(bs, want) {
		t.Fatalf("leader %q, bindings %+v, %v", leader, bs, err)
	}
}

// A keymaps.toml with a value that is no string, a key outside a table, or
// what belongs in config.toml is an error (§14).
func TestParseKeysErrors(t *testing.T) {
	for _, s := range []string{
		"[keys.normal]\nx = 1",
		"[keys]\nx = \"a\"",
		`theme = "x"`,
		"[query]\nmax_rows = 50",
		"[keys.normal\n",
	} {
		if _, _, err := ParseKeys(s); err == nil {
			t.Errorf("keymaps.toml %q: expected an error", s)
		}
	}
}

// LeaderHint is the key that starts with the leader, where Hint is the first
// key: the ? help is opened by <Leader>? where a pane has no keys (§6.5).
func TestLeaderHint(t *testing.T) {
	m := mustLoad(t, "[keys]\nleader = \"<C-a>\"")
	if got, first := m.LeaderHint("keyhelp.open", "normal"), m.Hint("keyhelp.open", "normal"); got != "C-a ?" || first != "?" {
		t.Errorf("leader's %q, first %q", got, first)
	}
	if got := m.LeaderHint("grid.edit", "grid"); got != "" {
		t.Errorf("no key with the leader: %q", got)
	}
}

// Only limits the other Focus scopes of a read only editor to some actions,
// whatever their keys are, in the modeless editor too: the user's own paging
// key works, and the table's other keys are not bound (F15.9).
func TestContextOnly(t *testing.T) {
	m := mustLoad(t, "[keys.grid]\n\">\" = \"grid.page.next\"\n")
	for _, plain := range []bool{false, true} {
		c := Context{Focus: []string{"jsonview", "grid"}, ReadOnly: "jsonview", Plain: plain, Only: []string{"grid.page.next", "grid.page.prev", "grid.refresh"}}
		for keys, want := range map[string]string{">": "grid.page.next", "]": "grid.page.next", "R": "grid.refresh", "x": "keys x", "G": "keys G"} { // "keys x": no binding took it, the editor does
			if out, _ := press(t, NewResolver(m), c, keys); !reflect.DeepEqual(actions(out), []string{want}) {
				t.Errorf("plain %v, %s: %v, want %v", plain, keys, actions(out), want)
			}
		}
	}
}
