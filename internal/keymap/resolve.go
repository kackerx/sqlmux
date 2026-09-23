package keymap

import (
	"fmt"
	"strconv"
	"strings"
)

type Mode uint8

const (
	Normal Mode = iota
	Visual
	Insert
)

func (m Mode) String() string { return [...]string{"normal", "visual", "insert"}[m] }

// Context is where a key press lands; it picks and orders the scopes (§6.4).
type Context struct {
	Overlay string   // topmost overlay scope, if one is open
	Focus   []string // focused widget scopes, most specific first: {"result", "grid"}
	Pane    string   // pane type for [map.<pane>.<mode>]: console | grid | tree
	Mode    Mode
}

// scopes lists the tables for c, highest priority first: overlay → user maps
// (NORMAL/VISUAL only) → focused widget → normal (NORMAL only) → global.
// A Ctrl leader works in every mode, so leader bindings from normal come last
// when normal itself is not in play.
func (m *Map) scopes(c Context, maps bool) []string {
	var ts []string
	if c.Overlay != "" {
		ts = append(ts, "keys."+c.Overlay)
	}
	if maps && c.Mode != Insert {
		if c.Pane != "" {
			ts = append(ts, "map."+c.Pane+"."+c.Mode.String())
		}
		ts = append(ts, "map."+c.Mode.String())
	}
	for _, f := range c.Focus {
		// grid/tree/console/result keys are NORMAL/VISUAL keys: in INSERT only
		// the input being typed into has bindings (↵ in console inserts a newline).
		if c.Mode != Insert || f == "cell" || f == "input" {
			ts = append(ts, "keys."+f)
		}
	}
	if c.Mode == Normal {
		ts = append(ts, "keys.normal")
	}
	ts = append(ts, "keys.global")
	if c.Mode != Normal && strings.HasPrefix(string(m.Leader[0]), "<C-") {
		ts = append(ts, "leader")
	}
	return ts
}

type node struct {
	next   map[Key]*node
	bound  bool
	action string
	rhs    []Key
}

// trie merges the scopes of c into one trie, cached per context. The first
// scope to bind an exact sequence wins; a longer sequence from a lower scope
// still hangs under it, which is what makes a node ambiguous.
func (m *Map) trie(c Context, maps bool) *node {
	id := fmt.Sprint(c, maps)
	if t, ok := m.tries[id]; ok {
		return t
	}
	root := &node{}
	for _, t := range m.scopes(c, maps) {
		bs := m.tables[t]
		if t == "leader" {
			bs = nil
			for _, b := range m.tables["keys.normal"] {
				if b.Keys[0] == Leader {
					bs = append(bs, b)
				}
			}
		}
		for _, b := range bs {
			n := root
			for _, k := range m.expand(b.Keys) {
				if n.next == nil {
					n.next = map[Key]*node{}
				}
				if n.next[k] == nil {
					n.next[k] = &node{}
				}
				n = n.next[k]
			}
			if !n.bound {
				n.bound, n.action, n.rhs = true, b.Action, b.RHS
			}
		}
	}
	m.tries[id] = root
	return root
}

// Result is one thing a key press produced.
type Result struct {
	Action string // bound action, with args
	Count  int    // count typed before it; 0 when none
	Keys   []Key  // keys no binding claimed, for the focused widget (count digits included)
}

// Resolver turns key presses into actions: counts, sequences, timeouts and
// user mappings (§6.5, §6.6).
type Resolver struct {
	m     *Map
	count string
	keys  []Key // pending sequence, after the count
	node  *node
	seq   int
}

func NewResolver(m *Map) *Resolver { return &Resolver{m: m} }

// Feed consumes one key press. wait reports an ambiguous pending sequence:
// the caller should call Timeout(seq) after Map.Timeout unless another key
// comes first.
func (r *Resolver) Feed(c Context, k Key) (out []Result, wait bool) {
	r.seq++
	r.feed(c, k, r.m.trie(c, true), &out)
	return out, r.node != nil && r.node.bound
}

// Timeout fires the pending ambiguous binding, if seq is still current.
func (r *Resolver) Timeout(c Context, seq int) []Result {
	var out []Result
	if seq == r.seq && r.node != nil && r.node.bound {
		r.fire(c, &out)
	}
	return out
}

func (r *Resolver) Seq() int { return r.seq }

// Pending is the count and keys typed so far, for the status bar (B-03).
func (r *Resolver) Pending() []Key { return append(digits(r.count), r.keys...) }

func (r *Resolver) Reset() { r.count, r.keys, r.node = "", nil, nil }

func (r *Resolver) feed(c Context, k Key, root *node, out *[]Result) {
	if r.node == nil {
		if c.Mode != Insert && len(k) == 1 && (k >= "1" && k <= "9" || k == "0" && r.count != "") {
			r.count += string(k) // a lone 0 is a key (grid.first), not a count
			return
		}
		r.node = root
	}
	next := r.node.next[k]
	switch {
	case next != nil:
		r.keys = append(r.keys, k)
		r.node = next
		if len(next.next) == 0 {
			r.fire(c, out)
		}
	case r.node != root && r.node.bound:
		// The shorter binding of an ambiguous node fires; k starts afresh.
		r.fire(c, out)
		r.feed(c, k, root, out)
	case k == Esc && (r.node != root || r.count != ""):
		r.Reset() // esc cancels a pending sequence or count
	default:
		*out = append(*out, Result{Keys: append(r.Pending(), k)})
		r.Reset()
	}
}

func (r *Resolver) fire(c Context, out *[]Result) {
	n, count := r.node, r.count
	r.Reset()
	if n.rhs == nil {
		cnt, _ := strconv.Atoi(count)
		*out = append(*out, Result{Action: n.action, Count: cnt})
		return
	}
	// noremap: the right-hand side runs against the bindings without user
	// maps; a count typed before the mapping goes in front, as in vim.
	base := r.m.trie(c, false)
	for _, k := range append(digits(count), r.m.expand(n.rhs)...) {
		r.feed(c, k, base, out)
	}
}

func digits(s string) []Key {
	var ks []Key
	for _, r := range s {
		ks = append(ks, Key(string(r)))
	}
	return ks
}
