package keymap

import (
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"sqlmux/internal/config"
)

//go:embed default.toml
var defaultTOML string

// tables is every [keys.*] / [map.*] table a config may use (§6.4, §6.6),
// in resolution priority order for the export. which-key is no scope: keys
// pressed over it resolve as without it (§6.5).
var tables = []string{
	"keys.confirm", "keys.palette", "keys.where", "keys.cols", "keys.dropdown", "keys.sessions",
	"keys.complete", "keys.options", "keys.cell", "keys.input",
	"keys.result", "keys.grid", "keys.tree", "keys.console", "keys.normal", "keys.global",
	"map.console.normal", "map.console.visual", "map.grid.normal", "map.grid.visual",
	"map.tree.normal", "map.tree.visual", "map.normal", "map.visual",
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

	tables   map[string][]Binding
	defaults map[string][]Binding // for the export's unbind lines
	tries    map[string]*node     // merged per context, see trie
}

// New builds the keymap from the embedded defaults and the user's config.
// Lines with problems are skipped; the rest still apply.
func New(user *config.Config) (*Map, []Problem) {
	def, err := config.Parse(defaultTOML)
	if err != nil {
		panic("default.toml: " + err.Error())
	}
	m := &Map{tables: map[string][]Binding{}, tries: map[string]*node{}}
	m.Leader, _ = Parse(def.Leader)
	var problems []Problem
	if user.Leader != "" {
		if ks, err := Parse(user.Leader); err != nil || len(ks) == 0 || slices.Contains(ks, Leader) {
			problems = append(problems, Problem{"keys", "leader", fmt.Sprintf("leader 写法不对：%q", user.Leader)})
		} else {
			m.Leader = ks
		}
	}
	// The leader is final before any binding is merged: "same key" is decided
	// on expanded sequences, so a user's "<Space>s" rebinds the default "<Leader>s".
	if ps := m.apply(def); len(ps) > 0 {
		panic(fmt.Sprint("default.toml: ", ps))
	}
	m.defaults = map[string][]Binding{}
	for t, bs := range m.tables {
		m.defaults[t] = slices.Clone(bs)
	}
	m.Timeout = time.Duration(user.Timeoutlen) * time.Millisecond
	problems = append(problems, m.apply(user)...)
	return m, append(problems, m.ambiguities()...)
}

// apply merges one config's bindings: a key already bound in the same table
// is rebound in place (an ordinary remap, not a conflict), "" unbinds.
func (m *Map) apply(c *config.Config) []Problem {
	var ps []Problem
	seen := map[string]string{} // table + normalized key → spelling, within this file
	for _, b := range c.Bindings {
		bad := func(format string, a ...any) { ps = append(ps, Problem{b.Table, b.Key, fmt.Sprintf(format, a...)}) }
		if !slices.Contains(tables, b.Table) {
			bad("没有这个表")
			continue
		}
		keys, err := Parse(b.Key)
		if err != nil || len(keys) == 0 {
			bad("键位写法不对：%v", err)
			continue
		}
		id := b.Table + " " + String(m.expand(keys))
		if prev, dup := seen[id]; dup {
			bad("与 %q 是同一个键", prev)
			continue
		}
		seen[id] = b.Key
		nb := Binding{Keys: keys, Action: b.Value}
		if strings.HasPrefix(b.Table, "map.") {
			if nb.RHS, err = Parse(b.Value); err != nil {
				bad("映射写法不对：%v", err)
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

// ambiguities warns where one binding is a prefix of another in the same
// table: the shorter one then fires only after timeoutlen (§6.7).
// ponytail: same table only; a grid "g" against normal's "gt" also makes an
// ambiguous node once merged. Check per context if that bites.
func (m *Map) ambiguities() []Problem {
	var ps []Problem
	for _, t := range tables {
		for _, a := range m.tables[t] {
			for _, b := range m.tables[t] {
				ka, kb := m.expand(a.Keys), m.expand(b.Keys)
				if len(ka) < len(kb) && slices.Equal(ka, kb[:len(ka)]) {
					ps = append(ps, Problem{t, String(a.Keys), fmt.Sprintf("是 %s 的前缀，要等 timeoutlen 之后才执行", String(b.Keys))})
					break
				}
			}
		}
	}
	return ps
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
// unbound (§6.7). All key text on screen goes through here.
func (m *Map) Hint(action, scope string) string {
	for _, b := range m.tables["keys."+scope] {
		if b.Action == action {
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
		for _, b := range m.tables[t] {
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
	b.WriteString("| 作用域 | 键 | 操作 |\n|---|---|---|\n")
	for _, t := range tables {
		for _, bd := range m.tables[t] {
			op := bd.Action
			if bd.RHS != nil {
				op = "→ " + String(bd.RHS)
			}
			fmt.Fprintf(&b, "| %s | `%s` | %s |\n", strings.TrimPrefix(t, "keys."), mdEscape(Display(m.expand(bd.Keys))), mdEscape(op))
		}
	}
	return b.String()
}

func mdEscape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// when is the note above each section of the export: when its keys apply (§6.4).
var when = map[string]string{
	"keys.global":   "任何时候，输入框里也生效",
	"keys.normal":   "NORMAL 模式，焦点在任何 pane",
	"keys.grid":     "焦点在表格（NORMAL）",
	"keys.tree":     "焦点在 schema 树（NORMAL）",
	"keys.console":  "console 的 NORMAL / VISUAL（M3）",
	"keys.result":   "结果区，叠在 grid 之上（M3）",
	"keys.palette":  "命令面板打开时",
	"keys.dropdown": "ORDER / LIMIT 下拉框打开时",
	"keys.complete": "补全列表弹出时（↵、esc 由输入框处理）",
	"keys.where":    "WHERE 的历史 / 收藏下拉打开时",
	"keys.input":    "任何输入框里",
	"keys.cols":     "COLS 列表打开时",
	"keys.cell":     "编辑单元格时（M2）",
	"keys.sessions": "session 列表打开时（M5）",
	"keys.confirm":  "确认框打开时",
	"keys.options":  "单元格的选项浮层展开、而且有选项时",
}

// TOML renders the effective keymap as a config that, loaded back, gives the
// same keymap (`sqlmux keys --format toml`), commented to read as the full
// reference (§6.7). titles come from app's registry, which keymap cannot
// import. Defaults the user unbound come out as `"key" = ""`; titled actions
// bound nowhere as a commented `"" = "<action>"` under the scope their ID
// prefix names.
func (m *Map) TOML(titles map[string]string) string {
	bound := m.Actions()
	unbound := map[string][]string{}
	for _, id := range slices.Sorted(maps.Keys(titles)) {
		if slices.Contains(bound, id) {
			continue
		}
		t := "keys.normal"
		if p, _, _ := strings.Cut(id, "."); slices.Contains([]string{"grid", "tree", "console", "result"}, p) {
			t = "keys." + p
		}
		unbound[t] = append(unbound[t], fmt.Sprintf(`# "" = %s  # %s`, strconv.Quote(id), titles[id]))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[keys]\nleader = %s\n", strconv.Quote(String(m.Leader)))
	for _, t := range tables {
		lines := []string{}
		for _, bd := range m.tables[t] {
			line := strconv.Quote(String(bd.Keys)) + " = "
			if bd.RHS != nil {
				line += strconv.Quote(String(bd.RHS))
			} else {
				line += strconv.Quote(bd.Action)
				if id, _, _ := strings.Cut(bd.Action, " "); titles[id] != "" {
					line += "  # " + titles[id]
				}
			}
			lines = append(lines, line)
		}
		for _, d := range m.defaults[t] {
			if !slices.ContainsFunc(m.tables[t], func(o Binding) bool { return m.same(o.Keys, d.Keys) }) {
				lines = append(lines, strconv.Quote(String(d.Keys))+` = ""`)
			}
		}
		lines = append(lines, unbound[t]...)
		if len(lines) > 0 {
			b.WriteString("\n")
			if when[t] != "" {
				fmt.Fprintf(&b, "# %s\n", when[t])
			}
			fmt.Fprintf(&b, "[%s]\n%s\n", t, strings.Join(lines, "\n"))
		}
	}
	b.WriteString("\n# 用户映射（§6.6）：键 → 一串键（noremap）。[map.normal] 所有 pane 通用，下面这种只在表格里生效\n# [map.grid.normal]\n# \"J\" = \"5j\"\n")
	return b.String()
}
