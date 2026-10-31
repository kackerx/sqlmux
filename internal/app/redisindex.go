package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// keyIndex is the names of the keys of a db, kept here for the completion of
// key names to match in with the matcher the tables' names are, and for the
// palette's @ to list (§9.8, F20.1). It is built a SCAN at a time the first time a
// key name is asked for, each batch a request of its own so that others get in
// between; the tree's keys are it when the tree read them all.
type keyIndex struct {
	names   []string
	seen    map[string]bool // SCAN may give a name twice
	cands   []candidate     // names, made when it is done
	total   int64           // DBSIZE
	cursor  uint64
	started bool // the first batch is asked for: it asks DBSIZE too
	done    bool // names is all of the db, or all that index_keys keeps room for
	capped  bool // the db has more
	// last is what the pattern before matched of cands, in the order they matched:
	// what a longer pattern matches is among it, so typing on matches in that and
	// not in all of them (§9.8).
	last struct {
		pattern string
		cands   []candidate
	}
}

// indexBatchCount is the COUNT of each SCAN that builds the index (§9.8).
const indexBatchCount = redis.ScanCount

// redisShown is how many candidates a key completion keeps: the best of what an
// index of a hundred thousand names matches, as the list shows eight at a time.
const redisShown = 200

// indexMsg is a batch of the index's names that the server gave.
type indexMsg struct {
	conn  *Conn
	ix    *keyIndex
	names []string
	next  uint64
	total int64 // DBSIZE, in the first batch
	err   error
}

// forgetNames drops what the completion was told of c's db: the names it was
// handed and the index, which may be others now (§9.8).
func (c *Conn) forgetNames() { c.hints, c.index = nil, nil }

// indexed is c's index when it can be matched in: built, or the tree's keys when
// it read them all; nil while there is none. A connection that is a command has
// none of its own built, a scan of it being a run of a command each time (§9.8).
func (c *Conn) indexed() *keyIndex {
	if c.index == nil && c.scan != nil && !c.scan.more && !c.treeStale {
		ix := &keyIndex{total: c.scan.total, done: true}
		for _, k := range c.scan.keys {
			ix.names = append(ix.names, k.Name)
		}
		ix.makeCands()
		c.index = ix
	}
	if c.index != nil && c.index.done {
		return c.index
	}
	return nil
}

// makeCands sorts the names, as the list of one match orders those that match alike,
// and makes the candidates once.
func (ix *keyIndex) makeCands() {
	slices.Sort(ix.names)
	ix.cands = make([]candidate, len(ix.names))
	for i, n := range ix.names {
		ix.cands[i] = candidate{label: n, insert: redis.Quote(n), note: i18n.T("键"), space: true, kind: kindKey}
	}
}

// ponytail: the first key typed looks at every name, 85ms of the fzf matcher when
// nearly all of a hundred thousand match (BenchmarkKeyIndexComplete); the ones
// after it look at what the one before matched. Match off the main loop if that is
// still felt.

// narrowed is the candidates partial can match among: those the pattern typed
// before it matched, when it goes on from it, else every name.
func (ix *keyIndex) narrowed(partial string) []candidate {
	if p := strings.ToLower(partial); ix.last.pattern != "" && strings.HasPrefix(p, ix.last.pattern) {
		return ix.last.cands
	}
	return ix.cands
}

// remember keeps what partial matched, the keys among items, for the next key typed:
// in the order of the names, as every match is of the names whole, so that what ties
// is ordered the same whichever way it was typed.
func (ix *keyIndex) remember(partial string, items []candidate) {
	set := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it.kind == kindKey {
			set[it.label] = struct{}{}
		}
	}
	src := ix.narrowed(partial) // what it was matched among, in name order
	kept := make([]candidate, 0, len(set))
	for _, cd := range src {
		if _, ok := set[cd.label]; ok {
			kept = append(kept, cd)
		}
	}
	ix.last.pattern, ix.last.cands = strings.ToLower(partial), kept
}

// startIndex starts the index of c's db, once, unless there is one or will not
// be: the completion goes on with what it has till it is built.
func (a *App) startIndex(c *Conn) tea.Cmd {
	if c.index != nil || c.indexed() != nil || c.Spec.Engine == "exec" || c.State != connReady {
		return nil
	}
	c.index = &keyIndex{seen: map[string]bool{}}
	return a.indexBatch(c, c.index)
}

// indexBatch asks for the next batch of ix's names.
func (a *App) indexBatch(c *Conn, ix *keyIndex) tea.Cmd {
	meta, cursor, first := c.Meta, ix.cursor, !ix.started
	ix.started = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), countTimeout)
		defer cancel()
		m := indexMsg{conn: c, ix: ix}
		m.err = meta.RunSafe(ctx, func(ctx context.Context, conn db.Conn) (err error) {
			if first {
				if m.total, err = redis.DBSize(ctx, conn); err != nil {
					return err
				}
			}
			m.names, m.next, err = redis.ScanBatch(ctx, conn, cursor, indexBatchCount)
			return err
		})
		return m
	}
}

// gotIndex adds a batch to the index, and asks for the next until the cursor
// comes round or index_keys names are in; one that came after the index was
// dropped (a write, a refresh) is of no use. A failed batch drops the index: the
// next completion starts it again.
func (a *App) gotIndex(m indexMsg) tea.Cmd {
	c, ix := m.conn, m.ix
	if a.sessionOf(c) == nil || c.index != ix {
		return nil
	}
	if m.err != nil {
		c.index = nil
		return nil
	}
	if ix.cursor == 0 && len(ix.names) == 0 {
		ix.total = m.total
	}
	for _, n := range m.names {
		if !ix.seen[n] && len(ix.names) < a.indexKeys {
			ix.seen[n] = true
			ix.names = append(ix.names, n)
		}
	}
	ix.cursor = m.next
	if ix.cursor != 0 && len(ix.names) < a.indexKeys {
		return a.indexBatch(c, ix)
	}
	ix.done, ix.capped, ix.seen = true, ix.cursor != 0, nil
	ix.makeCands()
	return a.recomplete(c)
}

// recomplete makes the lists of the consoles of c again, where one is being
// typed in: names came that they were not told of.
func (a *App) recomplete(c *Conn) tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range a.queries() {
		if t.conn == c && t.ed.Mode() == editor.Insert {
			cmds = append(cmds, a.queryComplete(t, t.wait != nil && t.wait.manual))
		}
	}
	if p := a.palette; p != nil && p.conn == c && p.hint != nil { // the quick SQL's
		cmds = append(cmds, a.completeSQL())
	}
	return tea.Batch(cmds...)
}

// keyCandidates are the keys the completion offers for a name partly typed:
// those of the index, matched as every completion is; while there is none,
// those the server gave for the prefix, which are asked for. foot is what the
// list says under it when the index stops short of the db.
func (a *App) keyCandidates(t *queryTab, partial string) (found []candidate, foot string, cmds []tea.Cmd) {
	c := t.conn
	if ix := c.indexed(); ix != nil {
		if ix.capped {
			foot = i18n.T("已索引 %d / 共 %d 键", len(ix.names), ix.total)
		}
		return ix.narrowed(partial), foot, nil
	}
	if start := a.startIndex(c); start != nil {
		cmds = append(cmds, start)
	}
	names, have := c.hints["keys\x00"+partial]
	if !have {
		cmds = append(cmds, a.askHints(t, "keys", partial))
	}
	for _, n := range names {
		found = append(found, candidate{label: n, insert: redis.Quote(n), note: i18n.T("键"), space: true, kind: kindKey})
	}
	return found, "", cmds
}

// keyList is the keys of the Redis dbs the palette lists, made once for what the dbs
// have read, and what the last query matched of them, which the next one that goes
// on from it matches among (F20.3).
type keyList struct {
	sig   string // what the dbs had read: the list is made again when it changes
	items []paletteItem
	texts []string
	query string // cand are the items it matches, in their order
	cand  []int
	top   []paletteItem // the best keyListed of them, best first
	ms    []ui.Match    // of top
}

// keyListed is how many matching keys the palette keeps: no list shows more than the
// best of a hundred thousand.
const keyListed = 500

// keySig is what the dbs have read, in a line: the list of keys is that and nothing
// else, so it is made again when a db reads more.
func (a *App) keySig() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%p", a.icons)
	for _, c := range a.sess.conns() {
		if !c.sql() && c.State == connReady {
			fmt.Fprintf(&b, "|%s %p %p", c.tableKey(db.Table{Kind: "k"}), c.index, c.scan)
			if c.index != nil {
				fmt.Fprintf(&b, " %v %d", c.index.done, len(c.index.names))
			}
			if c.scan != nil {
				fmt.Fprintf(&b, " %d", len(c.scan.keys))
			}
		}
	}
	return b.String()
}

// keyItems are the keys of the Redis dbs the palette lists with the tables and the
// queries: the names the index of a db has and the keys the tree read, whichever the
// db has; none for a db of neither. It never asks the server (§12, F20.3).
func (a *App) keyItems() (items []paletteItem) {
	for _, c := range a.sess.conns() {
		if c.sql() || c.State != connReady {
			continue
		}
		where := c.Spec.Name + "/" + dbText(cmp.Or(c.Spec.Database, c.DB))
		known := map[string]string{} // the type of what the tree read
		var names []string
		if c.scan != nil {
			for _, k := range c.scan.keys {
				known[k.Name] = k.Type
				names = append(names, k.Name)
			}
		}
		if c.index != nil && c.index.done {
			for _, n := range c.index.names {
				if _, in := known[n]; !in {
					names = append(names, n)
				}
			}
		}
		for _, n := range names {
			icon := a.icons.Key
			if typ := known[n]; typ != "" {
				icon = a.icons.KeyType(typ)
			}
			items = append(items, paletteItem{itemRedisKey, c.tableKey(db.Table{Name: n, Kind: "k"}), icon, n, where})
		}
	}
	return items
}

// keyMatches are the best keys that query matches, with their matches, indexes into
// the items returned: the list of keys is made again only when the dbs read more,
// and a query that goes on from the last one looks among what that matched.
func (a *App) keyMatches(query string) ([]paletteItem, []ui.Match) {
	kl := a.palette.keys
	if sig := a.keySig(); kl == nil || kl.sig != sig {
		kl = &keyList{sig: sig, items: a.keyItems()}
		for _, it := range kl.items {
			kl.texts = append(kl.texts, it.name+" "+it.where)
		}
		a.palette.keys = kl
	}
	if kl.query != query {
		narrow := kl.query != "" && strings.HasPrefix(query, kl.query) // among what the last query matched
		texts := kl.texts
		if narrow {
			texts = make([]string, len(kl.cand))
			for i, ix := range kl.cand {
				texts[i] = kl.texts[ix]
			}
		}
		ms, from := ui.Filter(query, texts), kl.cand
		kl.cand = make([]int, len(ms))
		for i, m := range ms {
			if narrow {
				m.Index = from[m.Index]
			}
			ms[i], kl.cand[i] = m, m.Index
		}
		slices.Sort(kl.cand)
		slices.SortStableFunc(ms, func(x, y ui.Match) int { return cmp.Or(y.Score-x.Score, x.Index-y.Index) })
		kl.query, kl.top, kl.ms = query, []paletteItem{}, nil
		for _, m := range ms[:min(len(ms), keyListed)] {
			kl.top = append(kl.top, kl.items[m.Index])
			m.Index = len(kl.top) - 1
			kl.ms = append(kl.ms, m)
		}
	}
	return kl.top, kl.ms
}

// keyByID is the db and the key of a palette item's id, <connection>/db<n>/<key>:
// a name has no /, so the first two parts tell the db.
func (a *App) keyByID(id string) (*Conn, string, bool) {
	for _, c := range a.sess.conns() {
		if c.sql() {
			continue
		}
		if key, ok := strings.CutPrefix(id, c.Spec.Name+"/"+dbText(cmp.Or(c.Spec.Database, c.DB))+"/"); ok {
			return c, key, true
		}
	}
	return nil, "", false
}
