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
	gen     int  // how many times a write changed it: what a list made of it goes by (F20.8)
	// deltas are what the writes since this one was begun made of the names, in order:
	// a SCAN that was past a key's place does not tell, so they go on over it when it is done
	deltas []keyDelta
	// last is what the pattern before matched of cands, in the order they matched:
	// what a longer pattern matches is among it, so typing on matches in that and
	// not in all of them (§9.8).
	last struct {
		pattern string
		cands   []candidate
	}
}

// keyDelta is the names a write made and the names it took away.
type keyDelta struct{ add, del []string }

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

// indexed is c's index when it can be matched in: built, or the tree's keys when
// it read them all; nil while there is none. A connection that is a command has
// none of its own built, a scan of it being a run of a command each time (§9.8).
func (c *Conn) indexed() *keyIndex {
	if c.index == nil && c.scan != nil && !c.scan.more {
		c.index = indexOfTree(c.scan)
	}
	return c.index
}

// indexOfTree is the index that the keys of a tree that read the whole db make.
func indexOfTree(s *keyScan) *keyIndex {
	ix := &keyIndex{total: s.total, done: true}
	for _, k := range s.keys {
		ix.names = append(ix.names, k.Name)
	}
	ix.makeCands()
	return ix
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
// after it look at what the one before matched, and with more than matchInline
// names the match goes on in the background (F20.8). Upgrade to a matcher of our
// own if the first one is still too long to wait for.

// narrowsTo is whether what query matches is among what last matched, query going on
// from it: so with a plain pattern, whose every added character asks more. Not so
// once last has a term with ! (a longer one excludes less) or $ (a$ ends with a, a$b
// is a fuzzy match) (§9.7, fzf's extended syntax).
func narrowsTo(last, query string) bool {
	return last != "" && strings.HasPrefix(query, last) && !strings.ContainsAny(last, "!$")
}

// narrowed is the candidates partial can match among: those the pattern typed
// before it matched, when it goes on from it, else every name.
func (ix *keyIndex) narrowed(partial string) []candidate {
	if narrowsTo(ix.last.pattern, strings.ToLower(partial)) {
		return ix.last.cands
	}
	return ix.cands
}

// narrowedOf is narrowed for an index that may be none: no names.
func (ix *keyIndex) narrowedOf(partial string) []candidate {
	if ix == nil {
		return nil
	}
	return ix.narrowed(partial)
}

// remember keeps what partial matched, the keys among items, for the next key typed:
// in the order of the names, as every match is of the names whole, so that what ties
// is ordered the same whichever way it was typed. src is what it was matched among.
func (ix *keyIndex) remember(partial string, items, src []candidate) {
	set := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it.kind == kindKey {
			set[it.label] = struct{}{}
		}
	}
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
	if c.building != nil || c.indexed() != nil {
		return nil
	}
	return a.rebuildIndex(c)
}

// rebuildIndex makes a new index of c's db in the background, a SCAN batch at a
// time: the one there is, if any, is used till the new one is done and takes its
// place; one being made before is let go. A connection that is a command or not
// connected makes none (F20.8).
func (a *App) rebuildIndex(c *Conn) tea.Cmd {
	if c.Spec.Engine == "exec" || c.State != connReady {
		return nil
	}
	c.building = &keyIndex{seen: map[string]bool{}}
	return a.indexBatch(c, c.building)
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

// gotIndex adds a batch to the index being built, and asks for the next until the
// cursor comes round or index_keys names are in, when it takes the place of the one
// there was; a batch of one that was let go is of no use. A failed batch drops the
// one being built: the next completion starts it again, the old index stays.
func (a *App) gotIndex(m indexMsg) tea.Cmd {
	c, ix := m.conn, m.ix
	if a.sessionOf(c) == nil || c.building != ix {
		return nil
	}
	if m.err != nil {
		c.building = nil
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
	for _, d := range ix.deltas { // the writes that came while it was made, in the order they came
		ix.update(d.add, d.del)
	}
	ix.deltas = nil
	c.index, c.building = ix, nil
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

// keySource is what a Redis db has read of its keys, for the palette's list of them:
// copies of the slices, which an update of the index replaces and does not change, so
// that a list made in the background goes on over what was there when it began.
type keySource struct {
	where, idPrefix string // <connection>/db<n>, and what the ids of its keys start with
	tree            []redis.KeyInfo
	names           []string
}

// keySources are the dbs the palette lists keys of: the connected Redis ones.
func (a *App) keySources() (srcs []keySource) {
	for _, c := range a.sess.conns() {
		if c.sql() || c.State != connReady {
			continue
		}
		src := keySource{where: c.Spec.Name + "/" + dbText(cmp.Or(c.Spec.Database, c.DB))}
		src.idPrefix = src.where + "/"
		if c.scan != nil {
			src.tree = c.scan.keys
		}
		if c.index != nil { // the one being made is none of its own yet
			src.names = c.index.names
		}
		srcs = append(srcs, src)
	}
	return srcs
}

// keyCount is how many names the dbs have read, those of the tree and of the index
// counted both: what decides whether matching them is done in place or in the
// background.
func (a *App) keyCount() (n int) {
	for _, c := range a.sess.conns() {
		if c.sql() || c.State != connReady {
			continue
		}
		if c.scan != nil {
			n += len(c.scan.keys)
		}
		if c.index != nil {
			n += len(c.index.names)
		}
	}
	return n
}

// keySig is what the dbs have read, in a line: the list of keys is that and nothing
// else, so it is made again when a db reads more.
func (a *App) keySig() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%p", a.icons)
	for _, c := range a.sess.conns() {
		if !c.sql() && c.State == connReady {
			fmt.Fprintf(&b, "|%s %p %p", c.tableKey(db.Table{Kind: "k"}), c.index, c.scan)
			if c.index != nil {
				fmt.Fprintf(&b, " %d %d", len(c.index.names), c.index.gen)
			}
			if c.scan != nil {
				fmt.Fprintf(&b, " %d", len(c.scan.keys))
			}
		}
	}
	return b.String()
}

// buildKeyList makes the list of the keys of srcs: the names the tree read and those of
// the index that it did not, as palette items with the icon of the type the tree
// knows. It touches nothing but its arguments (§12, F20.3).
func buildKeyList(sig string, srcs []keySource, icons *ui.Icons) *keyList {
	kl := &keyList{sig: sig}
	for _, src := range srcs {
		known := make(map[string]string, len(src.tree)) // the type of what the tree read
		add := func(n, typ string) {
			icon := icons.Key
			if typ != "" {
				icon = icons.KeyType(typ)
			}
			kl.items = append(kl.items, paletteItem{itemRedisKey, src.idPrefix + n, icon, n, src.where})
			kl.texts = append(kl.texts, n+" "+src.where)
		}
		for _, k := range src.tree {
			known[k.Name] = k.Type
			add(k.Name, k.Type)
		}
		for _, n := range src.names {
			if _, in := known[n]; !in {
				add(n, "")
			}
		}
	}
	return kl
}

// match makes kl the matches of query among its keys: among those the last query
// matched when this one goes on from it, the best keyListed of them kept, best
// first. It changes kl and nothing it shares with another list, so it may run in the
// background over a copy.
func (kl *keyList) match(query string) {
	narrow := narrowsTo(kl.query, query) // among what the last query matched
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
	slices.SortStableFunc(ms, func(x, y ui.Match) int { return cmp.Or(y.Score-x.Score, x.Index-y.Index) })
	kl.query, kl.top, kl.ms = query, []paletteItem{}, nil
	for _, m := range ms[:min(len(ms), keyListed)] {
		kl.top = append(kl.top, kl.items[m.Index])
		m.Index = len(kl.top) - 1
		kl.ms = append(kl.ms, m)
	}
}

// keyMatches are the best keys that query matches, with their matches, indexes into
// the items returned. With few keys the list is made and matched here, once for what
// the dbs have read and for each query; with many (matchInline) it is the one the
// background made last, which keyListCmd asks for, and the list stays as it was while
// the next is made (F20.8).
func (a *App) keyMatches(query string) ([]paletteItem, []ui.Match) {
	kl := a.palette.keys
	if a.keyCount() > matchInline {
		if kl == nil {
			return nil, nil
		}
		return kl.top, kl.ms
	}
	if sig := a.keySig(); kl == nil || kl.sig != sig {
		kl = buildKeyList(sig, a.keySources(), a.icons)
		a.palette.keys = kl
	}
	if kl.query != query {
		kl.match(query)
	}
	return kl.top, kl.ms
}

// matchInline is how many names are matched in place; more are matched in a Cmd, over
// a snapshot, and the answer comes back with the number of the ask (§9.8, F20.8).
// ponytail: a threshold, as a tab of a hundred names is no reason for a round trip
// through the loop for each key; a thousand names match in about a millisecond.
var matchInline = 2000

// keyListMsg is the answer of the background's making and matching of the keys.
type keyListMsg struct {
	p    *palette
	seq  int
	list *keyList
}

// keyListCmd asks for the keys the palette lists to be made and matched for what is
// typed, in the background, when the dbs have read more than matchInline names and
// the list is not the one for the query already or on its way: over a copy of the
// last one when the dbs are as they were (the next query goes on from its matches),
// else made anew. Run at the end of every message (F20.8).
func (a *App) keyListCmd() tea.Cmd {
	p := a.palette
	if p == nil {
		return nil
	}
	scope, query := a.paletteScope()
	if !slices.Contains(scopes[scope].kinds, itemRedisKey) || strings.TrimSpace(query) == "" || a.keyCount() <= matchInline {
		return nil
	}
	sig := a.keySig()
	if kl := p.keys; kl != nil && kl.sig == sig && kl.query == query || p.keyAsked == query && p.keyAskedSig == sig {
		return nil
	}
	p.keySeq++
	p.keyAsked, p.keyAskedSig = query, sig
	var base *keyList
	if kl := p.keys; kl != nil && kl.sig == sig {
		cp := *kl
		base = &cp
	}
	srcs, icons, seq := a.keySources(), a.icons, p.keySeq
	return func() tea.Msg {
		kl := base
		if kl == nil {
			kl = buildKeyList(sig, srcs, icons)
		}
		kl.match(query)
		return keyListMsg{p, seq, kl}
	}
}

// gotKeyListed takes the list the background made, unless a later ask was made or the
// palette is gone.
func (a *App) gotKeyListed(m keyListMsg) {
	if a.palette == m.p && m.seq == m.p.keySeq {
		m.p.keys = m.list
	}
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

// refreshIndex is what the tree's read of c's db (R, or the first) does to the
// index: the keys of a tree that read the whole db are the index at once, else an
// index there is, or one being made, is made again in the background, the old one
// used till then; none, none is made before the completion asks (§9.8, F20.8).
func (a *App) refreshIndex(c *Conn) tea.Cmd {
	if s := c.scan; s != nil && !s.more {
		c.index, c.building = indexOfTree(s), nil
		return a.recomplete(c)
	}
	if c.index != nil || c.building != nil {
		return a.rebuildIndex(c)
	}
	return nil
}

// wrote is what a command of a Redis connection that ran does to the key names:
// those it may have made or taken away are asked of the server in one go and the
// index follows; FLUSHDB empties the db's index and FLUSHALL every db's of the
// connection; SWAPDB, and a command whose keys are not known (not in the server's
// docs, EXEC and the ones that count their keys first), make the index again in the
// background. The
// names the console's completion was told of go too (§9.8, F20.8).
//
// ponytail: MOVE, COPY … DB n and SWAPDB change the keys of another db, whose index
// does not follow: R on that one. Upgrade when a db's keys are told of by other dbs'
// writes.
func (a *App) wrote(c *Conn, stmt string) tea.Cmd {
	words, err := redis.SplitLine(stmt)
	if c.sql() || err != nil || len(words) == 0 {
		return nil
	}
	name := strings.ToLower(words[0])
	if !c.lang().Writes(stmt) && name != "exec" {
		return nil
	}
	c.hints = nil
	switch name {
	case "flushdb":
		a.clearIndex(c)
		return nil
	case "flushall":
		for _, dc := range a.sess.conns() {
			if dc.root() == c.root() && !dc.sql() {
				a.clearIndex(dc)
			}
		}
		return nil
	case "swapdb": // no key in it, and every one of the db is another
		return a.rebuildIndex(c)
	}
	keys, known := c.writeKeys(words)
	if !known {
		return a.rebuildIndex(c)
	}
	return a.keysChanged(c, keys...)
}

// keysChanged follows keys that something made or took away: asked of the server in
// one go; none when the db has no index nor one being made to keep them in, it will
// be made from the server (§9.8, F20.8).
func (a *App) keysChanged(c *Conn, keys ...string) tea.Cmd {
	if len(keys) == 0 || c.indexed() == nil && c.building == nil {
		return nil
	}
	return a.askExists(c, keys)
}

// clearIndex empties c's index, a FLUSH having emptied the db; what was being made
// is let go.
func (a *App) clearIndex(c *Conn) {
	c.hints, c.building = nil, nil
	c.epoch++ // what was asked of the keys before is of the db as it was
	if c.index != nil || c.scan != nil {
		c.index = &keyIndex{done: true}
		c.index.makeCands()
	}
}

// writeKeys are the words of a command that the server's docs say are keys; known
// is false when they can not be told: the command is not in the docs, or counts its
// keys first (numkeys), or is EXEC, whose queued commands are not in the line.
func (c *Conn) writeKeys(words []string) (keys []string, known bool) {
	doc, _, rest, ok := c.docOf(words)
	if !ok || strings.EqualFold(words[0], "exec") || slices.ContainsFunc(doc.Args, func(a redis.Arg) bool { return a.Name == "numkeys" }) {
		return nil, false
	}
	for i := range rest {
		if p := doc.At(rest[:i]); p.Leaf != nil && p.Leaf.Type == "key" {
			keys = append(keys, rest[i])
		}
	}
	return keys, true
}

// indexDelta is what the server said of keys a write may have changed.
type indexDelta struct {
	conn  *Conn
	epoch int // of c.epoch when it was asked
	keys  []string
	found []bool
	err   error
}

// askExists asks whether keys are in c's db, on Meta, one round trip.
func (a *App) askExists(c *Conn, keys []string) tea.Cmd {
	meta, epoch := c.Meta, c.epoch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), countTimeout)
		defer cancel()
		m := indexDelta{conn: c, epoch: epoch, keys: keys}
		m.err = meta.RunSafe(ctx, func(ctx context.Context, conn db.Conn) (err error) {
			m.found, err = redis.Exists(ctx, conn, keys)
			return err
		})
		return m
	}
}

// gotDelta takes what the server said of the keys of a write into the index, and
// into the one being made, which has it when it is done; an answer it could not give
// makes the index again, one from before a FLUSH is of no use.
func (a *App) gotDelta(m indexDelta) tea.Cmd {
	c := m.conn
	if a.sessionOf(c) == nil || m.epoch != c.epoch {
		return nil
	}
	if m.err != nil {
		return a.rebuildIndex(c)
	}
	var d keyDelta
	for i, k := range m.keys {
		if m.found[i] {
			d.add = append(d.add, k)
		} else {
			d.del = append(d.del, k)
		}
	}
	if c.building != nil {
		c.building.deltas = append(c.building.deltas, d)
	}
	if c.index != nil {
		c.index.update(d.add, d.del)
	}
	return a.recomplete(c)
}

// update puts the names add in the index and takes del out. The slices are new, the
// old ones are not touched: a match going on over them in the background sees the
// index as it was (F20.8).
func (ix *keyIndex) update(add, del []string) {
	gone := make(map[string]bool, len(del))
	for _, n := range del {
		gone[n] = true
	}
	slices.Sort(add)
	add = slices.Compact(add)
	names := make([]string, 0, len(ix.names)+len(add))
	cands := make([]candidate, 0, cap(names))
	put := func(n string) {
		names = append(names, n)
		cands = append(cands, candidate{label: n, insert: redis.Quote(n), note: i18n.T("键"), space: true, kind: kindKey})
	}
	j, removed := 0, 0
	for i, n := range ix.names {
		for ; j < len(add) && add[j] < n; j++ {
			put(add[j])
		}
		if j < len(add) && add[j] == n {
			j++ // there already
		}
		if gone[n] {
			removed++
		} else {
			names, cands = append(names, n), append(cands, ix.cands[i])
		}
	}
	for ; j < len(add); j++ {
		put(add[j])
	}
	ix.total -= int64(removed)
	if !ix.capped { // else a key that was there and was not read is not a new one
		ix.total += int64(len(names) - len(ix.names) + removed)
	}
	ix.names, ix.cands, ix.gen = names, cands, ix.gen+1
	ix.last.pattern, ix.last.cands = "", nil
}
