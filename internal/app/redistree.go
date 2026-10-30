package app

import (
	"cmp"
	"context"
	"errors"
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// The tree of a Redis connection (§7.8「Redis 的树」, §8.7「树」): a connection's
// dbs, each a connection of its own like a PG database, and under a db the
// namespaces its keys' names make by the delimiter, then the keys, each with
// the icon of its type. What the tree shows is what SCAN read, up to a budget;
// the tree's filter asks the server for the keys that match it.

// keyScan is what a scan of a db read (§8.7).
type keyScan struct {
	keys  []redis.KeyInfo
	total int64 // the db's size, DBSIZE
	more  bool  // the budget stopped the scan: there are keys that were not read
	busy  bool  // a filter's scan is going on: keys is what the one before found (F18.11)
}

// delimiter is what splits the names of c's keys into namespaces: the
// connection's own, else `[redis] delimiter`.
func (a *App) delimiter(c *Conn) string { return cmp.Or(c.root().Spec.Delimiter, a.redisDelim) }

// keyTree is the keys of a db laid out by their namespaces: the keys that are
// in it, the namespaces below it by name.
type keyTree struct {
	spaces map[string]*keyTree
	keys   []redis.KeyInfo // the keys whose name ends here, in the order they are read
	n      int             // how many keys it and what is under it hold
}

// buildKeyTree splits each key's name by delim: all but the last part are
// namespaces, the last the key's. A name with two delimiters in a row has a
// namespace with no name, one that ends in a delimiter a key with none.
func buildKeyTree(keys []redis.KeyInfo, delim string) *keyTree {
	root := &keyTree{}
	for _, k := range keys {
		parts := strings.Split(k.Name, delim)
		t := root
		for _, p := range parts[:len(parts)-1] {
			if t.spaces == nil {
				t.spaces = map[string]*keyTree{}
			}
			if t.spaces[p] == nil {
				t.spaces[p] = &keyTree{}
			}
			t = t.spaces[p]
			t.n++
		}
		t.keys = append(t.keys, k)
	}
	return root
}

// nameText is a part of a key's name as the tree shows it: bytes that are no
// text are replaced, a part with nothing in it is said so.
func nameText(s string) string {
	if s == "" {
		return i18n.T("(空)")
	}
	return strings.ToValidUTF8(s, "�")
}

// keyColor is the color of the icon of a key of type typ (§7.7).
func (a *App) keyColor(typ string) (c color.Color) {
	th := a.theme
	switch typ {
	case "string":
		return th.SQLString
	case "hash":
		return th.Keyword
	case "list":
		return th.Number
	case "set":
		return th.Func
	case "zset":
		return th.Warn
	case "stream":
		return th.Info
	case "ReJSON-RL", "json":
		return th.SQLOperator
	}
	return th.Dim
}

// dbText is the name of a db as its node has it.
func dbText(name string) string { return "db" + name }

// keyNodes adds the nodes under the db node of dc, named name: its namespaces,
// a namespace's namespaces and keys, and a note when the scan stopped at its
// budget. A filter shows what the server found for it, every namespace open.
func (a *App) keyNodes(dc *Conn, name string, add func(node) bool, open func(string, bool) bool, filtering bool) {
	s := dc.scan
	if filtering {
		s = a.win().tree.found[dc]
	}
	if s == nil {
		return
	}
	delim := a.delimiter(dc)
	lit := a.win().tree.filter.Text // what the filter asked for, in the names it found (F20.2)
	if strings.ContainsAny(lit, "*?[") {
		lit = ""
	}
	dbID := dbNode(dc.root(), name)
	ic, th := a.icons, a.theme
	var walk func(t *keyTree, path []string, depth int)
	walk = func(t *keyTree, path []string, depth int) {
		for _, p := range slices.Sorted(maps.Keys(t.spaces)) {
			child, parts := t.spaces[p], append(slices.Clone(path), p)
			id := dbID + "/ns:" + strings.Join(parts, delim)
			n := node{id: id, kind: nodeNamespace, conn: dc, prefix: strings.Join(parts, delim) + delim, TreeNode: ui.TreeNode{
				Depth: depth, Branch: true, Open: open(id, false), Icon: ic.Schema, IconFg: th.PK, Text: nameText(p), Pos: substringPos(p, lit), Note: strconv.Itoa(child.n), NoteFg: th.Border,
			}}
			if add(n) {
				walk(child, parts, depth+1)
			}
		}
		keys := slices.Clone(t.keys)
		slices.SortFunc(keys, func(x, y redis.KeyInfo) int { return strings.Compare(x.Name, y.Name) })
		for _, k := range keys {
			_, leaf := cutLast(k.Name, delim)
			add(node{id: dbID + "/key:" + k.Name, kind: nodeRedisKey, conn: dc, redisKey: k, TreeNode: ui.TreeNode{
				Depth: depth, Icon: ic.KeyType(k.Type), IconFg: a.keyColor(k.Type), Text: nameText(leaf), Pos: substringPos(leaf, lit),
			}})
		}
	}
	walk(buildKeyTree(s.keys, delim), nil, 2)
	if s.more {
		add(node{id: dbID + "/more", kind: nodeNote, conn: dc, TreeNode: ui.TreeNode{
			Depth: 2, Text: i18n.T("已扫描 %d / %s，用 / 过滤", len(s.keys), ui.Magnitude(float64(s.total))), TextFg: th.Dim,
		}})
	}
}

// substringPos are the runes of s that lit covers, each place it is in s, as the
// server's *lit* matched it (case counts): where a part of a key's name is lit
// when the filter is typed (F20.2). A hit that crosses the delimiter is in no part
// whole, so none is found.
// ponytail: a filter with glob characters is not lit, as what it matches is not
// a place in a name, nor is a hit across the delimiter; upgrade to a match of the
// glob per part if either is missed.
func substringPos(s, lit string) (pos []int) {
	if lit == "" {
		return nil
	}
	s = strings.ToValidUTF8(s, "�") // as the node shows it
	n, from := utf8.RuneCountInString(lit), 0
	for {
		i := strings.Index(s[from:], lit)
		if i < 0 {
			return pos
		}
		start := utf8.RuneCountInString(s[:from+i])
		for k := range n {
			pos = append(pos, start+k)
		}
		from += i + len(lit)
	}
}

// cutLast is s before and after its last sep; the whole of it after when it has none.
func cutLast(s, sep string) (before, after string) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):]
	}
	return "", s
}

// keysMsg is what loadKeys read.
type keysMsg struct {
	conn *Conn
	dbs  []string // the server's dbs with keys, none from a db's own connection
	scan *keyScan
	err  error
}

// cmdsMsg is the server's commands, and their docs when it has them; err is why it
// would not say (§9.8).
type cmdsMsg struct {
	conn *Conn
	cmds redis.Commands
	docs redis.Docs
	err  error
}

// loadCommands asks c's server for its commands, once, as the connection comes up,
// on Meta ahead of the scan of the keys, which may take long: what writes and what
// is dangerous is not known till it answers, and the console does not run before
// (§9.8, §13).
func (a *App) loadCommands(c *Conn) tea.Cmd {
	if c.cmdsAsked {
		return nil
	}
	c.cmdsAsked = true
	meta := c.Meta
	return func() tea.Msg {
		m := cmdsMsg{conn: c}
		m.err = meta.RunSafe(context.Background(), func(ctx context.Context, conn db.Conn) (err error) {
			m.docs, _ = redis.LoadDocs(ctx, conn) // a server before 7 has none: the names alone are completed
			m.cmds, err = redis.LoadCommands(ctx, conn)
			return err
		})
		return m
	}
}

// gotCommands keeps the commands; a server that would not say has every command
// taken for a write and none for dangerous (§13).
func (a *App) gotCommands(m cmdsMsg) tea.Cmd {
	c := m.conn
	if a.sessionOf(c) == nil {
		return nil
	}
	c.cmds, c.docs, c.cmdsReady = m.cmds, m.docs, true
	if m.err != nil {
		return a.showToast(i18n.T("%s：读不到命令列表（%s），命令都按写命令处理", c.label(), m.err))
	}
	return nil
}

// loadKeys reads c's db on its Meta: with the connection of the DSN, which dbs
// the server has; then its keys, up to the scan budget (§8.7). It is
// loadCatalog of a Redis connection.
func (a *App) loadKeys(c *Conn) tea.Cmd {
	meta, budget, root := c.Meta, a.scanKeys, c.Spec.Database == ""
	return func() tea.Msg {
		ctx := context.Background()
		m := keysMsg{conn: c}
		var s redis.Scanned
		err := meta.RunSafe(ctx, func(ctx context.Context, conn db.Conn) (err error) {
			if root {
				if m.dbs, err = redis.Databases(ctx, conn); err != nil {
					return err
				}
			}
			s, err = redis.Scan(ctx, conn, "*", budget, redis.ScanCount)
			return err
		})
		m.scan, m.err = &keyScan{keys: s.Keys, total: s.Total, more: !s.Complete}, err
		return m
	}
}

// gotKeys takes a scan in: the dbs the server has, with the one the connection
// is on among them, and the keys of the db; the filter, if there is one, asks
// again for what it matches.
func (a *App) gotKeys(m keysMsg) tea.Cmd {
	c := m.conn
	if a.sessionOf(c) == nil {
		return nil
	}
	c.forgetNames() // the keys are not the ones the completion was told of
	c.treeStale = false
	if m.err != nil {
		a.dropPending(c)
		return a.showToast(i18n.T("%s：%s", c.label(), m.err))
	}
	if c.Spec.Database == "" {
		c.Databases = m.dbs
		if !slices.Contains(c.Databases, c.DB) { // the DSN's db is there to open, empty as it may be
			c.Databases = append(c.Databases, c.DB)
			slices.SortFunc(c.Databases, func(x, y string) int { return cmp.Compare(atoiOr(x), atoiOr(y)) })
		}
	}
	c.scan = m.scan
	a.clampTree()
	return tea.Batch(append(a.resolvePending(c), a.findKeys(c))...)
}

func atoiOr(s string) int { n, _ := strconv.Atoi(s); return n }

// filterPattern is the glob the tree's filter asks the server for: what is typed,
// with a * at each end unless it has one (§7.8).
func filterPattern(typed string) string {
	if typed == "" {
		return ""
	}
	if strings.Contains(typed, "*") {
		return typed
	}
	return "*" + typed + "*"
}

// foundMsg is what a scan for the filter's pattern found in a db.
type foundMsg struct {
	win     *Window // the one whose filter it is
	conn    *Conn
	pattern string
	scan    *keyScan
	err     error
}

// findKeys asks c's db for the keys the tree's filter matches, when it has one
// and c is a connected db of Redis; the keys it showed before stay till then.
func (a *App) findKeys(c *Conn) tea.Cmd {
	w := a.win()
	pattern := filterPattern(w.tree.filter.Text)
	if pattern == "" || c.sql() || c.State != connReady {
		delete(w.tree.found, c)
		return nil
	}
	meta, budget, ctx := c.Meta, a.scanKeys, w.tree.findContext()
	busy := &keyScan{busy: true}
	if old := w.tree.found[c]; old != nil { // what it showed stays, searching
		busy.keys, busy.total = old.keys, old.total
	}
	w.setFound(c, busy)
	return func() tea.Msg {
		var s redis.Scanned
		err := meta.RunSafe(ctx, func(ctx context.Context, conn db.Conn) (err error) {
			s, err = redis.Scan(ctx, conn, pattern, budget, redis.FilterCount)
			return err
		})
		return foundMsg{w, c, pattern, &keyScan{keys: s.Keys, total: s.Total, more: !s.Complete}, err}
	}
}

// setFound keeps s as what the server said of the window's filter in c's db.
func (w *Window) setFound(c *Conn, s *keyScan) {
	if w.tree.found == nil {
		w.tree.found = map[*Conn]*keyScan{}
	}
	w.tree.found[c] = s
}

// gotFound shows what was found in the window that asked, unless its filter has
// changed since.
func (a *App) gotFound(m foundMsg) tea.Cmd {
	if a.sessionOf(m.conn) == nil || m.pattern != filterPattern(m.win.tree.filter.Text) {
		return nil
	}
	if errors.Is(m.err, context.Canceled) { // a newer filter's scan is the one that answers
		return nil
	}
	if m.err != nil {
		delete(m.win.tree.found, m.conn) // not searching any more
		return a.showToast(i18n.T("%s：%s", m.conn.label(), m.err))
	}
	m.win.setFound(m.conn, m.scan)
	a.clampTree()
	return nil
}

// findContext is the context of the scans for the window's filter as it is: a
// change of it, esc included, cancels it with stopFind (F18.11).
func (t *treeState) findContext() context.Context {
	if t.findCtx == nil {
		t.findCtx, t.findStop = context.WithCancel(context.Background())
	}
	return t.findCtx
}

// stopFind cancels the scans out for the filter: the scans of the filter before
// are not wanted, they stop and do not run out. Another window's go on.
func (t *treeState) stopFind() {
	if t.findStop != nil {
		t.findStop()
		t.findCtx, t.findStop = nil, nil
	}
}

// filterDue is the filter left alone long enough to ask the servers for its
// keys: one typed over it is the one asked about.
type filterDue struct{ seq int }

// filterDelay is how long the filter waits for the next key before it asks.
var filterDelay = 200 * time.Millisecond

// askFilter is the tree's filter changed: every connected db of Redis is asked
// for its keys once the typing stops, none when there are none to ask.
func (a *App) askFilter() tea.Cmd {
	if !slices.ContainsFunc(a.sess.conns(), func(c *Conn) bool { return !c.sql() && c.State == connReady }) {
		return nil
	}
	a.filterSeq++
	a.win().tree.stopFind()
	seq := a.filterSeq
	return tea.Tick(filterDelay, func(time.Time) tea.Msg { return filterDue{seq} })
}

func (a *App) gotFilterDue(m filterDue) tea.Cmd {
	if m.seq != a.filterSeq {
		return nil
	}
	var cmds []tea.Cmd
	for _, c := range a.sess.conns() {
		if !c.sql() && c.State == connReady {
			cmds = append(cmds, a.findKeys(c))
		}
	}
	return tea.Batch(cmds...)
}

// onKey is op on the key of the focused pane's tab, as it is on a node of the tree;
// nothing when the tab is not a key's.
func (a *App) onKey(op func(node) tea.Cmd) tea.Cmd {
	if t := dataOf(a.focused()); t != nil && t.table.Kind == "k" {
		return op(node{conn: t.conn, redisKey: redis.KeyInfo{Name: t.table.Name}})
	}
	return nil
}

// keyDone answers an operation on keys: what to say, and the db's keys to read
// again; key is the one it was done to and to the name it has now, "" when it
// kept it: the tabs of it read it again, following the name.
type keyDone struct {
	conn    *Conn
	say     string
	err     error
	key, to string
}

func (a *App) gotKeyDone(m keyDone) tea.Cmd {
	c := m.conn
	c.busy--
	if a.sessionOf(c) == nil {
		return nil
	}
	say := m.say
	if m.err != nil {
		e := c.drv.ErrorOf(m.err)
		if say = e.Message; e.Code != "" {
			say = e.Code + " " + e.Message
		}
		if m.say != "" { // what was done before it failed, a cancel included
			say = m.say + " · " + say
		}
	}
	cmds := []tea.Cmd{a.showToast(say), a.loadKeys(c)}
	if m.key != "" {
		for _, p := range a.sessionOf(c).panes() {
			for i, tb := range p.Tabs {
				if t := tb.Data; t != nil && t.conn == c && t.table.Kind == "k" && t.table.Name == m.key {
					if m.err == nil && m.to != "" {
						t.table.Name = m.to
						p.Tabs[i].Name = c.tabName(t.table)
					}
					cmds = append(cmds, a.fetch(t, true))
				}
			}
		}
	}
	return tea.Batch(cmds...)
}

// writable is whether c takes a change of keys; else it says why.
func (a *App) writable(c *Conn) (ok bool, why tea.Cmd) {
	if c.readOnly {
		return false, a.showToast(i18n.T("%s 是只读连接", c.Spec.Name))
	}
	if c.State != connReady {
		return false, a.notReady(c)
	}
	return true, nil
}

// deleteRedisKey is the menu's 删除… on a key, and dd: asked first unless
// confirm_dangerous is off, a key being a piece of data as a table is (§13);
// then UNLINK.
func (a *App) deleteRedisKey(n node) tea.Cmd {
	c, key := n.conn, n.redisKey.Name
	if ok, why := a.writable(c); !ok {
		return why
	}
	del := func() tea.Cmd {
		main := c.Main
		c.busy++
		return func() tea.Msg {
			err := main.Run(context.Background(), func(ctx context.Context, conn db.Conn) error {
				_, err := redis.Unlink(ctx, conn, key)
				return err
			})
			return keyDone{c, i18n.T("已删除键 %s", nameText(key)), err, key, ""}
		}
	}
	if !a.confirmDangerous && !c.Spec.Production {
		return del()
	}
	a.confirm = writeBox(c, i18n.T("删除键 %s？", nameText(key)), i18n.T("删除"), true, del)
	return nil
}

// renameKey is the menu's 改名: the name asked in the status bar, RENAME on
// Main, and a name that is taken asked about once more (§7.8).
func (a *App) renameRedisKey(n node) tea.Cmd {
	c, key := n.conn, n.redisKey.Name
	if ok, why := a.writable(c); !ok {
		return why
	}
	a.askName = &nameAsk{label: i18n.T("新名字："), in: ui.Input{Text: key, Pos: len(key)}, done: func(a *App, to string) (tea.Cmd, bool) {
		if to == "" || to == key {
			return nil, false
		}
		if c.Spec.Production {
			a.confirm = writeBox(c, i18n.T("把键 %s 改名为 %s？", nameText(key), nameText(to)), i18n.T("改名"), true, func() tea.Cmd { return a.renameTo(c, key, to, false) })
			return nil, false
		}
		return a.renameTo(c, key, to, false), false
	}}
	return nil
}

// renameTo is RENAME of key to to on c's Main; overwrite says the question of a
// name that is taken has been answered yes.
func (a *App) renameTo(c *Conn, key, to string, overwrite bool) tea.Cmd {
	main := c.Main
	c.busy++
	return func() tea.Msg {
		exists := false
		err := main.Run(context.Background(), func(ctx context.Context, conn db.Conn) (err error) {
			exists, err = redis.Rename(ctx, conn, key, to, overwrite)
			return err
		})
		if exists {
			return keyExists{c, key, to}
		}
		return keyDone{c, i18n.T("%s 改名为 %s", nameText(key), nameText(to)), err, key, to}
	}
}

// keyExists is a rename onto a key that is there.
type keyExists struct {
	conn    *Conn
	key, to string
}

func (a *App) gotKeyExists(m keyExists) {
	m.conn.busy--
	a.confirm = writeBox(m.conn, i18n.T("%s 已经有了，覆盖它？", nameText(m.to)), i18n.T("覆盖"), true, func() tea.Cmd {
		return a.renameTo(m.conn, m.key, m.to, true)
	})
}

// ttlKey is the menu's 设 TTL: seconds, or a duration, or nothing to have it
// not expire, asked in the status bar.
func (a *App) ttlKey(n node) tea.Cmd {
	c, key := n.conn, n.redisKey.Name
	if ok, why := a.writable(c); !ok {
		return why
	}
	a.askName = &nameAsk{label: i18n.T("TTL（秒，或 90m；空 = 不过期）："), blank: "0", done: func(a *App, text string) (tea.Cmd, bool) {
		ttl, ok := redis.ParseTTL(text)
		if !ok {
			return a.showToast(i18n.T("TTL 要是秒数或 90m、1h30m 这样的时长")), true
		}
		set := func() tea.Cmd {
			main := c.Main
			c.busy++
			return func() tea.Msg {
				err := main.Run(context.Background(), func(ctx context.Context, conn db.Conn) error { return redis.SetTTL(ctx, conn, key, ttl) })
				return keyDone{c, i18n.T("%s 的 TTL 已设", nameText(key)), err, key, ""}
			}
		}
		if c.Spec.Production {
			if ttl == 0 { // a blank is given as "0"; EXPIRE 0 would delete the key, sqlmux's 0 is no expiry
				text = i18n.T("不过期")
			}
			a.confirm = writeBox(c, i18n.T("设置键 %s 的 TTL：%s？", nameText(key), text), i18n.T("设置"), true, set)
			return nil, false
		}
		return set(), false
	}}
	return nil
}

// namespaceFound is the keys under a namespace that a scan of the whole db found.
type namespaceFound struct {
	conn  *Conn
	space string
	keys  []string
	err   error
}

// deleteNamespace is the menu's 删除 on a namespace: every key under it is found
// first, with no budget, so that the number asked about is the true one, then,
// asked, UNLINKed 500 at a time (§8.7). C-c stops either.
func (a *App) deleteNamespace(n node) tea.Cmd {
	c, prefix := n.conn, n.prefix
	if ok, why := a.writable(c); !ok {
		return why
	}
	meta := c.Meta
	c.busy++
	return func() tea.Msg {
		var keys []string
		err := meta.RunSafe(context.Background(), func(ctx context.Context, conn db.Conn) error {
			keys = nil
			return redis.Each(ctx, conn, redis.Glob(prefix)+"*", func(ks []string) error { keys = append(keys, ks...); return nil })
		})
		return namespaceFound{c, prefix, keys, err}
	}
}

// unlinkBatch is how many keys an UNLINK takes at a time.
const unlinkBatch = 500

// unlinkAll deletes keys unlinkBatch at a time by unlink, which says how many of a
// batch were there; deleted is how many went, those of the batches before an error
// counted.
func unlinkAll(keys []string, unlink func([]string) (int64, error)) (deleted int, err error) {
	for batch := range slices.Chunk(keys, unlinkBatch) {
		n, err := unlink(batch)
		deleted += int(n)
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (a *App) gotNamespaceFound(m namespaceFound) tea.Cmd {
	c := m.conn
	if a.sessionOf(c) == nil {
		c.busy--
		return nil
	}
	if m.err != nil || len(m.keys) == 0 {
		c.busy--
		say := i18n.T("%s 下面没有键", nameText(m.space))
		if m.err != nil {
			say = c.drv.ErrorOf(m.err).Message
			if errors.Is(m.err, context.Canceled) {
				say = i18n.T("已取消")
			}
		}
		return a.showToast(say)
	}
	c.busy-- // the question is not a request out
	del := func() tea.Cmd {
		main := c.Main
		c.busy++
		return func() tea.Msg {
			deleted := 0
			err := main.Run(context.Background(), func(ctx context.Context, conn db.Conn) (err error) {
				deleted, err = unlinkAll(m.keys, func(batch []string) (int64, error) { return redis.Unlink(ctx, conn, batch...) })
				return err
			})
			say := i18n.T("已删除 %d 个键", deleted)
			if err != nil && deleted < len(m.keys) {
				say = i18n.T("已删除 %d 个键，没有删完", deleted)
				if errors.Is(err, context.Canceled) {
					err = nil
					say = i18n.T("已取消，已删除 %d 个键", deleted)
				}
			}
			return keyDone{conn: c, say: say, err: err}
		}
	}
	if !a.confirmDangerous && !c.Spec.Production {
		return del()
	}
	a.confirm = writeBox(c, i18n.T("删除 %d 个键？", len(m.keys))+"\n  "+nameText(m.space)+"*", i18n.T("删除"), true, del)
	return nil
}
