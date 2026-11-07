package keymap

import (
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/kackerx/sqlmux/internal/i18n"
)

//go:embed default.toml
var defaultTOML string

// noVimTOML is what vim = false adds over it (§6.9).
//
//go:embed default-novim.toml
var noVimTOML string

// table is a [keys.*] / [map.*] table a config may use, with the note above its
// section of the export: when its keys apply (§6.4).
type table struct{ name, when string }

// tables is every one of them (§6.4, §6.6), in resolution priority order for
// the export. which-key is no scope: keys pressed over it resolve as without
// it (§6.5).
var tables = []table{
	{"keys.confirm", i18n.N("确认框打开时")},
	{"keys.value", i18n.N("值编辑器浮窗打开时：C-s 写回并关闭，其余是它的 vim")},
	{"keys.menu", i18n.N("右键菜单打开时")},
	{"keys.ddl", i18n.N("表的 DDL 浮窗打开时")},
	{"keys.palette", i18n.N("命令面板打开时")},
	{"keys.palettepreview", i18n.N("命令面板的预览区有焦点时：其余键是它的只读编辑器的")},
	{"keys.conns", i18n.N("连接管理的列表有焦点时")},
	{"keys.groups", i18n.N("连接表单的 group 字段弹出分组列表时，表单的键在它之后")},
	{"keys.engines", i18n.N("连接表单的 engine 字段弹出引擎列表时，表单的键在它之后")},
	{"keys.form", i18n.N("连接表单打开时")},
	{"keys.jump", i18n.N("flash 式跳转进行中：其余可打印字符进模式，标签字母跳转")},
	{"keys.where", i18n.N("WHERE 的历史 / 收藏下拉打开时")},
	{"keys.order", i18n.N("ORDER 列表打开时")},
	{"keys.cols", i18n.N("COLS 列表打开时")},
	{"keys.dropdown", i18n.N("LIMIT 等下拉框打开时")},
	{"keys.sessions", i18n.N("session 列表打开时")},
	{"keys.complete", i18n.N("补全列表弹出时（↵、esc 由输入框处理）")},
	{"keys.newrow", i18n.N("编辑还没保存的新行的格时，叠在选项 / 分段浮层之上")},
	{"keys.options", i18n.N("非时间列的单元格，选项浮层展开、而且有选项时")},
	{"keys.segments", i18n.N("时间列的单元格，分段浮层展开时")},
	{"keys.keyhelp", i18n.N("? 键位帮助打开时")},
	{"keys.cell", i18n.N("编辑单元格时")},
	{"keys.input", i18n.N("任何输入框里")},
	{"keys.wherenormal", i18n.N("WHERE 输入框在 NORMAL 下")},
	{"keys.select", i18n.N("表格有选中的记录、或在 VISUAL 扩选时，叠在 grid / result 之上；它的键是完整绑定，不等 timeoutlen")},
	{"keys.result", i18n.N("结果区，叠在 grid 之上")},
	{"keys.grid", i18n.N("焦点在表格（NORMAL）")},
	{"keys.tree", i18n.N("焦点在 schema 树（NORMAL）")},
	{"keys.jsonview", i18n.N("grid 的 JSON 视图：其余键是它的只读编辑器的")},
	{"keys.queryinsert", i18n.N("query 在 INSERT 下")},
	{"keys.query", i18n.N("query 的 NORMAL / VISUAL")},
	{"keys.landing", i18n.N("引导页：新 tab、没有 tab 的 pane")},
	{"keys.normal", i18n.N("NORMAL 模式，焦点在任何 pane")},
	{"keys.global", i18n.N("任何时候，输入框里也生效")},
	{"map.query.normal", ""},
	{"map.query.visual", ""},
	{"map.grid.normal", ""},
	{"map.grid.visual", ""},
	{"map.tree.normal", ""},
	{"map.tree.visual", ""},
	{"map.normal", ""},
	{"map.visual", ""},
}

// Line is one `key = "value"` line of keymaps.toml under [keys.<scope>] or
// [map.<...>].
type Line struct {
	Table string // "keys.normal", "map.query.normal"
	Key   string // as written, e.g. "<C-p>"
	Value string // action for keys.*, key sequence for map.*; "" unbinds
}

// ParseKeys reads keymaps.toml's TOML (§14): [keys]' leader, and the
// [keys.*] and [map.*] lines in file order, so "first binding" questions
// have a stable answer.
func ParseKeys(data string) (leader string, lines []Line, err error) {
	var raw map[string]any
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return "", nil, err
	}
	for _, k := range md.Keys() {
		if k[0] != "keys" && k[0] != "map" {
			return "", nil, fmt.Errorf("%s: only [keys] and [map.*] go here", k[0])
		}
		if md.Type(k...) == "Hash" {
			continue
		}
		v, ok := lookup(raw, k).(string)
		if !ok {
			return "", nil, fmt.Errorf("%s: the value must be a string", k)
		}
		if k.String() == "keys.leader" {
			leader = v
			continue
		}
		if len(k) < 3 {
			return "", nil, fmt.Errorf("%s: a key goes in a table such as [keys.normal] or [map.normal]", k)
		}
		lines = append(lines, Line{strings.Join(k[:len(k)-1], "."), k[len(k)-1], v})
	}
	return leader, lines, nil
}

func lookup(m map[string]any, path []string) any {
	var v any = m
	for _, p := range path {
		v = v.(map[string]any)[p]
	}
	return v
}

// Binding is one effective line of a table.
type Binding struct {
	Keys   []Key  // as written, <Leader> not expanded
	Action string // keys.* tables: action with args, e.g. "window.select 3"
	RHS    []Key  // map.* tables: the key sequence it stands for (noremap)
}

// Problem is a config line the keymap rejected or warns about.
type Problem struct {
	Table, Key, Msg string
}

func (p Problem) String() string { return fmt.Sprintf("[%s] %s: %s", p.Table, p.Key, p.Msg) }

// Map is the effective keymap: defaults with the user's config on top.
type Map struct {
	Leader  []Key
	Timeout time.Duration

	tables        map[string][]Binding
	novim         map[string]bool      // table + key of what default-novim.toml bound: the hints name these first (§6.9)
	defaults      map[string][]Binding // for the export: what the user changed
	defaultLeader []Key
	tries         map[string]*node // merged per context, see trie
}

// New builds the keymap from the embedded defaults and the user's
// keymaps.toml, its leader and its lines, with the program's vim and
// timeoutlen. Lines with problems are skipped; the rest still
// apply.
func New(userLeader string, vim bool, timeoutlen int, user []Line) (*Map, []Problem) {
	leader, def, err := ParseKeys(defaultTOML)
	if err != nil {
		panic("default.toml: " + err.Error())
	}
	m := &Map{tables: map[string][]Binding{}, tries: map[string]*node{}}
	m.Leader, _ = Parse(leader)
	m.defaultLeader = m.Leader
	var problems []Problem
	if userLeader != "" {
		if ks, err := Parse(userLeader); err != nil || len(ks) == 0 || slices.Contains(ks, Leader) {
			problems = append(problems, Problem{"keys", "leader", i18n.T("leader 写法不对：%q", userLeader)})
		} else {
			m.Leader = ks
		}
	}
	// The leader is final before any binding is merged: "same key" is decided
	// on expanded sequences, so a user's "<Space>s" rebinds the default "<Leader>s".
	if ps := m.apply(def); len(ps) > 0 {
		panic(fmt.Sprint("default.toml: ", ps))
	}
	if !vim { // the same defaults, for the export and the check, with the arrow keys on
		_, nv, err := ParseKeys(noVimTOML)
		if err != nil {
			panic("default-novim.toml: " + err.Error())
		}
		if ps := m.apply(nv); len(ps) > 0 {
			panic(fmt.Sprint("default-novim.toml: ", ps))
		}
		m.novim = map[string]bool{}
		for _, b := range nv {
			ks, _ := Parse(b.Key)
			m.novim[b.Table+" "+String(ks)] = true
		}
		for _, t := range []string{"keys.grid", "keys.tree"} { // the arrow keys are in default.toml since F16.6, the hints still name them first
			for _, b := range m.tables[t] {
				if len(b.Keys) == 1 && slices.Contains([]Key{"<Up>", "<Down>", "<Left>", "<Right>"}, b.Keys[0]) {
					m.novim[t+" "+String(b.Keys)] = true
				}
			}
		}
	}
	m.defaults = map[string][]Binding{}
	for t, bs := range m.tables {
		m.defaults[t] = slices.Clone(bs)
	}
	m.Timeout = time.Duration(timeoutlen) * time.Millisecond
	problems = append(problems, m.apply(user)...)
	return m, problems
}

// apply merges one file's bindings: a key already bound in the same table
// is rebound in place (an ordinary remap, not a conflict), "" unbinds.
func (m *Map) apply(bs []Line) []Problem {
	var ps []Problem
	seen := map[string]string{} // table + normalized key → spelling, within this file
	for _, b := range bs {
		bad := func(msg string) { ps = append(ps, Problem{b.Table, b.Key, msg}) }
		if !slices.ContainsFunc(tables, func(t table) bool { return t.name == b.Table }) {
			bad(i18n.T("没有这个表"))
			continue
		}
		keys, err := Parse(b.Key)
		if err != nil || len(keys) == 0 {
			bad(i18n.T("键位写法不对：%v", err))
			continue
		}
		id := b.Table + " " + String(m.expand(keys))
		if prev, dup := seen[id]; dup {
			bad(i18n.T("与 %q 是同一个键", prev))
			continue
		}
		seen[id] = b.Key
		nb := Binding{Keys: keys, Action: b.Value}
		if strings.HasPrefix(b.Table, "map.") {
			if nb.RHS, err = Parse(b.Value); err != nil {
				bad(i18n.T("映射写法不对：%v", err))
				continue
			}
			nb.Action = ""
		}
		m.set(b.Table, nb, b.Value == "")
	}
	return ps
}

func (m *Map) set(table string, b Binding, unbind bool) {
	bs := m.tables[table]
	i := slices.IndexFunc(bs, func(o Binding) bool { return m.same(o.Keys, b.Keys) })
	switch {
	case unbind && i >= 0:
		bs = slices.Delete(bs, i, i+1)
	case unbind:
	case i >= 0:
		bs[i] = b
	default:
		bs = append(bs, b)
	}
	m.tables[table] = bs
}

// same reports whether two bindings are the same key once <Leader> is expanded.
func (m *Map) same(a, b []Key) bool { return slices.Equal(m.expand(a), m.expand(b)) }

func (m *Map) expand(ks []Key) []Key {
	if !slices.Contains(ks, Leader) {
		return ks
	}
	var out []Key
	for _, k := range ks {
		if k == Leader {
			out = append(out, m.Leader...)
		} else {
			out = append(out, k)
		}
	}
	return out
}

// Hint is how the UI shows the first key bound to action in scope, or "" when
// unbound (§6.7). All key text on screen goes through here. With vim = false
// a key default-novim.toml bound comes first: ↓ for the grid, not j.
func (m *Map) Hint(action, scope string) string {
	first := ""
	for _, b := range m.tables["keys."+scope] {
		if b.Action != action {
			continue
		}
		if m.novim["keys."+scope+" "+String(b.Keys)] {
			return Display(m.expand(b.Keys))
		}
		if first == "" {
			first = Display(m.expand(b.Keys))
		}
	}
	return first
}

// LeaderHint is Hint of the first key that starts with the leader: the one
// typed where no pane has the keys, in an overlay, with a Ctrl leader (§6.4).
func (m *Map) LeaderHint(action, scope string) string {
	for _, b := range m.tables["keys."+scope] {
		if b.Action == action && len(b.Keys) > 0 && b.Keys[0] == Leader {
			return Display(m.expand(b.Keys))
		}
	}
	return ""
}

// Actions lists every bound action ID, args stripped, once each: app's
// tests check it against the registry's titles.
func (m *Map) Actions() []string {
	var ids []string
	for _, t := range tables {
		for _, b := range m.tables[t.name] {
			if id, _, _ := strings.Cut(b.Action, " "); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// Markdown renders the effective keymap as a table (`sqlmux keys`).
func (m *Map) Markdown() string {
	var b strings.Builder
	b.WriteString(i18n.T("| 作用域 | 键 | 操作 |\n|---|---|---|\n"))
	for _, t := range tables {
		for _, bd := range m.tables[t.name] {
			op := bd.Action
			if bd.RHS != nil {
				op = "→ " + String(bd.RHS)
			}
			fmt.Fprintf(&b, "| %s | `%s` | %s |\n", strings.TrimPrefix(t.name, "keys."), mdEscape(Display(m.expand(bd.Keys))), mdEscape(op))
		}
	}
	return b.String()
}

func mdEscape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// whenPlainQuery is keys.query's note with vim = false, where the query
// has no NORMAL and the Ctrl keys reach its editor (§6.9).
var whenPlainQuery = i18n.N("query 的编辑器，只有 Ctrl 开头的键生效，C-h 除外，它是退格；NORMAL / VISUAL 是 vim 模式的")

// TOML renders the effective keymap as a whole keymaps.toml (`sqlmux keys
// --format toml`, §6.7): every scope, with its defaults commented out, so
// an old export never pins a new version's defaults, and only what the
// user changed live, in the default's place: a rebinding, an unbinding
// `"key" = ""`, then keys of their own. Loaded back it gives the same
// keymap. titles come from app's registry, which keymap cannot import;
// titled actions bound nowhere come as a commented `"" = "<action>"` under
// the scope their ID prefix names.
func (m *Map) TOML(titles map[string]string) string {
	bound := m.Actions()
	unbound := map[string][]string{}
	for _, id := range slices.Sorted(maps.Keys(titles)) {
		if slices.Contains(bound, id) {
			continue
		}
		t := "keys.normal"
		if p, _, _ := strings.Cut(id, "."); slices.Contains([]string{"grid", "tree", "query", "result"}, p) {
			t = "keys." + p
		}
		unbound[t] = append(unbound[t], fmt.Sprintf(`# "" = %s  # %s`, strconv.Quote(id), titles[id]))
	}
	var b strings.Builder
	leader := "leader = " + strconv.Quote(String(m.Leader))
	if slices.Equal(m.Leader, m.defaultLeader) {
		leader = "# " + leader
	}
	fmt.Fprintf(&b, "[keys]\n%s\n", leader)
	for _, tb := range tables {
		t := tb.name
		lines := []string{}
		for _, d := range m.defaults[t] {
			i := slices.IndexFunc(m.tables[t], func(o Binding) bool { return m.same(o.Keys, d.Keys) })
			switch {
			case i < 0:
				lines = append(lines, strconv.Quote(String(d.Keys))+` = ""`)
			case m.tables[t][i].Action == d.Action && slices.Equal(m.tables[t][i].RHS, d.RHS):
				lines = append(lines, "# "+tomlLine(d, titles))
			default:
				lines = append(lines, tomlLine(m.tables[t][i], titles))
			}
		}
		for _, bd := range m.tables[t] {
			if !slices.ContainsFunc(m.defaults[t], func(d Binding) bool { return m.same(d.Keys, bd.Keys) }) {
				lines = append(lines, tomlLine(bd, titles))
			}
		}
		lines = append(lines, unbound[t]...)
		if len(lines) > 0 {
			b.WriteString("\n")
			note := tb.when
			if t == "keys.query" && m.novim != nil {
				note = whenPlainQuery
			}
			if note != "" {
				fmt.Fprintf(&b, "# %s\n", i18n.T(note))
			}
			fmt.Fprintf(&b, "[%s]\n%s\n", t, strings.Join(lines, "\n"))
		}
	}
	b.WriteString(i18n.T("\n# 用户映射：键 → 一串键（noremap）。[map.normal] 所有 pane 通用，下面这种只在表格里生效\n# [map.grid.normal]\n# \"J\" = \"5j\"\n"))
	return b.String()
}

// tomlLine is b as a keymaps.toml line, with its action's title.
func tomlLine(b Binding, titles map[string]string) string {
	line := strconv.Quote(String(b.Keys)) + " = "
	if b.RHS != nil {
		return line + strconv.Quote(String(b.RHS))
	}
	line += strconv.Quote(b.Action)
	if id, _, _ := strings.Cut(b.Action, " "); titles[id] != "" {
		line += "  # " + titles[id]
	}
	return line
}
