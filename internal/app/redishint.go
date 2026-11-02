package app

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/redis"
	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/ui"
)

// docOf is the documentation of the command words start with, the subcommand's
// when it has one that the second word names: the name as a line shows it, and the
// words after it (§9.8).
func (c *Conn) docOf(words []string) (doc redis.Doc, name string, rest []string, ok bool) {
	if len(words) == 0 {
		return doc, "", nil, false
	}
	doc, ok = c.docs[strings.ToLower(words[0])]
	if !ok {
		return doc, "", nil, false
	}
	if len(words) > 1 {
		if sub, found := doc.Subs[strings.ToLower(words[1])]; found {
			return sub, words[0] + " " + words[1], words[2:], true
		}
	}
	return doc, words[0], words[1:], true
}

// redisSignature is the line of a command's arguments with the one the cursor is
// at lit (§9.8, F16.5): none where no command is typed, or the server has no docs of
// it; with everything given the name is lit.
func (a *App) redisSignature(t *queryTab) ui.Signature {
	lines, cur := t.ed.Lines(), t.ed.Cursor()
	words, _, _ := redis.Typed(lines[cur.Line][:cur.Col])
	doc, name, rest, ok := t.conn.docOf(words)
	if !ok || len(doc.Args) == 0 {
		return ui.Signature{}
	}
	return ui.Signature{Fields: doc.Fields(name), Cur: max(doc.At(rest).Field, 0), Sep: " "}
}

// hintDelay is how long a key or field completion waits for the next key before
// it asks the server (§9.8); hintWant how many names it asks for.
var hintDelay = 150 * time.Millisecond

const hintWant = 50

// hashCommands are those whose argument named field is a field of the hash that
// the key before it names.
var hashCommands = []string{"hget", "hset", "hsetnx", "hdel", "hexists", "hmget", "hincrby", "hincrbyfloat", "hstrlen", "hgetdel", "hgetex", "hsetex"}

// redisComplete is the candidates for the word before the cursor of a console's
// line, before being the line up to it, their start a column of it (§9.8, F16.5):
// command names, then a command's subcommands, then by what its docs say goes
// there: the tokens of its arguments, the keys of the db, the fields of a hash.
// With nothing typed it opens for C-n only. The names the server has to give come
// later: the list is made again when they are in.
func (a *App) redisComplete(t *queryTab, before string, manual bool) (*completion, []tea.Cmd) {
	words, partial, open := redis.Typed(before)
	if open || strings.HasPrefix(strings.TrimLeft(before, " \t"), "#") || partial == "" && !manual {
		return nil, nil
	}
	c := t.conn
	var cmds []tea.Cmd
	if start := a.startIndex(c); start != nil { // from the first word typed, the key's is some words on: the round trips of a far server are over by then (F20.11)
		cmds = append(cmds, start)
	}
	text := before // the candidates are ranked against the line up to the cursor
	start := len(before) - len(partial)
	t.matchSeq++ // a match going on in the background for an earlier word is of no use now (F20.8)
	var groups [][]candidate
	var foot string       // what the list says under it (F20.1)
	var indexed *keyIndex // the index the keys came from, if one did
	var big []candidate   // the names of an index too long to be matched here (F20.8)
	switch doc, _, rest, ok := c.docOf(words); {
	case len(words) == 0:
		groups = [][]candidate{c.commandNames()}
	case len(words) == 1 && ok && len(doc.Subs) > 0:
		var subs []candidate
		for _, s := range slices.Sorted(maps.Keys(doc.Subs)) {
			subs = append(subs, candidate{label: strings.ToUpper(s), insert: strings.ToUpper(s), note: doc.Subs[s].Summary, space: true, kind: kindKeyword})
		}
		groups = [][]candidate{subs}
	case ok:
		place := doc.At(rest)
		var toks []candidate
		for _, tk := range place.Tokens {
			toks = append(toks, candidate{label: tk, insert: tk, note: i18n.T("参数"), space: true, kind: kindKeyword})
		}
		groups = [][]candidate{toks}
		if place.Leaf != nil && !strings.ContainsAny(partial[:min(len(partial), 1)], `"'`) {
			var names []string
			var kind, arg string
			switch {
			case place.Leaf.Type == "key":
				kind, arg = "keys", partial
			case place.Leaf.Name == "field" && len(words) >= 2 && slices.Contains(hashCommands, strings.ToLower(words[0])):
				kind, arg = "fields", words[1]
			}
			switch kind {
			case "keys":
				found, f, ask := a.keyCandidates(t, partial)
				foot, cmds, indexed = f, append(cmds, ask...), c.indexed()
				if indexed != nil && len(found) > matchInline {
					big = found
				} else {
					groups = append(groups, found)
				}
			case "fields":
				var have bool
				if names, have = c.hints[kind+"\x00"+arg]; !have {
					cmds = append(cmds, a.askHints(t, kind, arg))
				}
				var found []candidate
				for _, n := range names {
					found = append(found, candidate{label: n, insert: redis.Quote(n), note: i18n.T("字段"), space: true, kind: kindColumn})
				}
				groups = append(groups, found)
			}
		}
	}
	if big != nil { // matched over the names as they are now, which an update does not change, and answered with the number of the ask (F20.8)
		t.matching = true
		seq, gen := t.matchSeq, indexed.gen
		groups = slices.Clone(groups)
		cmds = append(cmds, func() tea.Msg {
			comp := ranked(text, len(text), partial, start, append(groups, big)...)
			return matchMsg{t: t, seq: seq, comp: comp, ix: indexed, gen: gen, partial: partial, src: big, foot: foot}
		})
		return nil, cmds
	}
	return a.finishMatch(ranked(text, len(text), partial, start, groups...), indexed, partial, indexed.narrowedOf(partial), foot), cmds
}

// commandNames are the commands of the server as candidates: those its docs
// list, else those COMMAND does (a server before 7), a container's subcommands not.
func (c *Conn) commandNames() (out []candidate) {
	if c.docs != nil {
		for _, n := range slices.Sorted(maps.Keys(c.docs)) {
			out = append(out, candidate{label: strings.ToUpper(n), insert: strings.ToUpper(n), note: c.docs[n].Summary, space: true, kind: kindCommand})
		}
		return out
	}
	for _, n := range slices.Sorted(maps.Keys(c.cmds)) {
		if !strings.Contains(n, "|") {
			out = append(out, candidate{label: strings.ToUpper(n), insert: strings.ToUpper(n), space: true, kind: kindCommand})
		}
	}
	return out
}

// hintDue is a completion that waited for the typing to stop (§9.8): it asks if
// it is still the last, hintMsg is what the server answered.
type hintDue struct {
	t         *queryTab
	seq       int
	kind, arg string
}

type hintMsg struct {
	conn      *Conn
	kind, arg string
	names     []string
	err       error
}

// askHints waits hintDelay, then asks the server for the names of kind ("keys" that
// start with arg, "fields" of the hash arg) unless a later key was typed.
func (a *App) askHints(t *queryTab, kind, arg string) tea.Cmd {
	t.hintSeq++
	due := hintDue{t, t.hintSeq, kind, arg}
	return tea.Tick(hintDelay, func(time.Time) tea.Msg { return due })
}

func (a *App) gotHintDue(m hintDue) tea.Cmd {
	t := m.t
	c := t.conn
	if m.seq != t.hintSeq || c.State != connReady || !slices.Contains(a.queries(), t) && (a.palette == nil || a.palette.hint != t) {
		return nil
	}
	meta, limit := c.Meta, a.loadAll
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), countTimeout)
		defer cancel()
		r := hintMsg{conn: c, kind: m.kind, arg: m.arg}
		r.err = meta.RunSafe(ctx, func(ctx context.Context, conn db.Conn) (err error) {
			if m.kind == "keys" {
				r.names, err = redis.KeyHints(ctx, conn, m.arg, hintWant)
			} else {
				r.names, err = redis.HashFields(ctx, conn, m.arg, limit)
			}
			return err
		})
		return r
	}
}

// gotHints keeps the names, and makes the lists of the consoles of its connection
// again, where one is being typed in (a failed ask is none).
func (a *App) gotHints(m hintMsg) tea.Cmd {
	if m.err != nil || a.sessionOf(m.conn) == nil {
		return nil
	}
	if m.conn.hints == nil {
		m.conn.hints = map[string][]string{}
	}
	m.conn.hints[m.kind+"\x00"+m.arg] = m.names
	return a.recomplete(m.conn)
}

// finishMatch is comp, the match of partial, kept: what it matched is where the next
// key typed looks, the best redisShown are the list, with the foot of the index (F20.1).
func (a *App) finishMatch(comp *completion, ix *keyIndex, partial string, src []candidate, foot string) *completion {
	if ix != nil && partial != "" {
		var items []candidate
		if comp != nil {
			items = comp.items
		}
		ix.remember(partial, items, src)
	}
	if comp != nil {
		comp.items, comp.foot = comp.items[:min(len(comp.items), redisShown)], foot
	}
	return comp
}

// matchCtx is where a match in the background was asked for, to see that the
// answer still has a place: a console's cursor and text, or the quick SQL's input
// and where its text begins in it.
type matchCtx struct {
	cur    editor.Pos
	ver    int
	manual bool
	p      *palette
	input  string
	shift  int
}

// matchMsg is a key completion that was matched in the background.
type matchMsg struct {
	t       *queryTab
	seq     int
	comp    *completion
	ix      *keyIndex
	gen     int // the index's as it was matched: an update since makes what it matched out of date
	partial string
	src     []candidate
	foot    string
}

// gotMatch makes the list of a match that came back, if it is the last one asked and
// what it was asked for is as it was: the typing went on, a mode left, a palette
// closed, and it is of no use (§9.8, F20.8). A ↵ that came meanwhile takes the
// selected one of the list, when what it was asked for is as it was.
func (a *App) gotMatch(m matchMsg) tea.Cmd {
	t := m.t
	if m.seq != t.matchSeq || !t.matching {
		return nil
	}
	t.matching = false
	enter := t.enter
	t.enter = false
	ix := m.ix
	if ix != nil && ix.gen != m.gen {
		ix = nil // the names changed since: what it matched is not where the next key looks
	}
	comp := a.finishMatch(m.comp, ix, m.partial, m.src, m.foot)
	x := t.matchFor
	if p := x.p; p != nil { // the quick SQL's
		if a.palette != p || p.input.Text != x.input {
			return nil
		}
		if comp != nil {
			comp.start += x.shift
		}
		p.comp = comp
		if enter && comp != nil && comp.sel >= 0 {
			return a.acceptCompletion()
		}
		return nil
	}
	if t.ed.Mode() != editor.Insert || t.ver != x.ver || t.ed.Cursor() != x.cur {
		return nil
	}
	t.comp, t.wait = comp, &compWait{x.cur, x.ver, x.manual}
	if enter && comp != nil && comp.sel >= 0 {
		return a.queryAccept(t)
	}
	return nil
}
