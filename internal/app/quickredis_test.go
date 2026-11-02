package app

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
)

// answerQuickCmds runs what a key returned and gives the palette the quick SQL's
// answer, whatever batch it is in.
func answerQuickCmds(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			answerQuickCmds(a, c)
		}
	case quickMsg:
		a.Update(m)
	}
}

// In the quick SQL of a Redis connection a command goes to Main as the console's
// line does, a reply is the rows, a write on a read-only connection is kept back
// and a dangerous one is asked about; blank lines and comments are none, and a
// password is kept out of the history (F17.2).
func TestQuickRedis(t *testing.T) {
	d := &execDB{res: map[string]db.Result{
		"HGETALL h": {Cols: []db.Col{{Name: "field"}, {Name: "value"}}, Rows: [][]db.Val{{{S: "a"}, {S: "1"}}}},
	}}
	a, _ := redisConsole(t, d, someCommands(), "")
	c := a.sess.Conns[0]
	a.confirmDangerous = true
	typeLine := func(line string) { // the list of candidates, which ↵ would take, put away
		t.Helper()
		feed(t, a, "<C-p>;"+line)
		if a.palette.comp != nil {
			feed(t, a, "<Esc>")
		}
	}
	run := func(line string) {
		t.Helper()
		typeLine(line)
		answerQuickCmds(a, keys(t, a, "<CR>"))
	}
	run("HGETALL h")
	if q := a.palette.quick; q.err != "" || len(q.res.Rows) != 1 || q.res.Rows[0][1].S != "1" || !slices.Equal(d.ran, []string{"HGETALL h"}) {
		t.Fatalf("a reply: %+v, ran %v", a.palette.quick, d.ran)
	}
	if !slices.Equal(a.state.SQL["doraemon"], []string{"HGETALL h"}) {
		t.Errorf("history %v", a.state.SQL)
	}
	if hs, _ := config.LoadHistory(); len(hs) != 1 || hs[0].SQL != "HGETALL h" || hs[0].Conn != "doraemon" {
		t.Errorf("history.jsonl: %+v", hs)
	}
	feed(t, a, "<Esc>")
	// dangerous: asked, and sent on a yes only
	typeLine("FLUSHALL")
	keys(t, a, "<CR>")
	if a.confirm == nil || len(d.ran) != 1 || a.palette.quick.running != "" {
		t.Fatalf("FLUSHALL: confirm %v, ran %v", a.confirm != nil, d.ran)
	}
	feed(t, a, "n")
	if a.confirm != nil || len(d.ran) != 1 || a.palette == nil || a.palette.quick.running != "" {
		t.Fatalf("no: ran %v", d.ran)
	}
	feed(t, a, "<CR>")
	answerQuickCmds(a, keys(t, a, "y"))
	c.building = &keyIndex{} // FLUSHALL took the index being made: none starts on the fake's server
	if len(d.ran) != 2 || d.ran[1] != "FLUSHALL" {
		t.Errorf("yes: ran %v", d.ran)
	}
	feed(t, a, "<Esc>")
	// a write on a read-only connection is not sent; a read is
	c.readOnly = true
	run("SET k 1")
	if q := a.palette.quick; q.err != "doraemon 是只读连接" || len(d.ran) != 2 || q.running != "" {
		t.Errorf("SET on a read-only one: %q, ran %v", q.err, d.ran)
	}
	feed(t, a, "<Esc>")
	run("GET k")
	if len(d.ran) != 3 {
		t.Errorf("a read: ran %v", d.ran)
	}
	feed(t, a, "<Esc>")
	c.readOnly = false
	// nothing to send, and a password is not kept
	run("# only a comment")
	feed(t, a, "<Esc>")
	run("AUTH secret")
	if len(d.ran) != 4 || d.ran[3] != "AUTH secret" || slices.Contains(a.state.SQL["doraemon"], "AUTH secret") {
		t.Errorf("ran %v, history %v", d.ran, a.state.SQL["doraemon"])
	}
	if hs, _ := config.LoadHistory(); slices.ContainsFunc(hs, func(h config.History) bool { return h.SQL == "AUTH secret" }) {
		t.Error("AUTH in history.jsonl")
	}
}

// The quick SQL completes a Redis command as the console does: names, and a
// key the server was asked for (F17.2).
func TestQuickRedisCompletion(t *testing.T) {
	a, _ := redisConsole(t, &execDB{}, someCommands(), "")
	conn := a.sess.Conns[0]
	conn.docs, conn.hints = someDocs(), map[string][]string{"keys\x00us": {"user:1"}}
	conn.building = &keyIndex{} // being built: the completion goes on with the names it was told of
	feed(t, a, "<C-p>;se")
	if c := a.palette.comp; c == nil || c.items[0].label != "SET" || c.items[0].kind != kindCommand || c.start != 1 {
		t.Fatalf("se: %+v", c)
	}
	feed(t, a, "<Esc><Esc>")
	feed(t, a, "<C-p>;get us")
	if c := a.palette.comp; c == nil || c.items[0].label != "user:1" || c.items[0].kind != kindKey {
		t.Fatalf("get us: %+v", c)
	}
	// a key not known yet is asked for, and the list comes up when it is in
	feed(t, a, "<Esc><Esc>")
	feed(t, a, "<C-p>;get zz")
	seq := a.palette.hint.hintSeq
	if seq == 0 {
		t.Fatal("not asked")
	}
	if a.gotHintDue(hintDue{a.palette.hint, seq, "keys", "zz"}) == nil {
		t.Error("the quick SQL's ask is dropped")
	}
	a.Update(hintMsg{conn: conn, kind: "keys", arg: "zz", names: []string{"zz:1"}})
	if c := a.palette.comp; c == nil || c.items[0].label != "zz:1" {
		t.Errorf("after the names: %+v", c)
	}
}

// R on a quick tab of a Redis connection sends a read again, and a command with an
// effect not at all (F17.2).
func TestQuickRedisRerun(t *testing.T) {
	d := &execDB{}
	a, _ := redisConsole(t, d, someCommands(), "")
	c := a.sess.Conns[0]
	for sql, want := range map[string]bool{"GET k": true, "SET k 1": false} {
		r := &run{conn: c, sql: sql, done: true, win: a.win()}
		a.rerunQuick(&resultTab{run: r})
		if r.done == want { // sent: not done
			t.Errorf("%s: done %v", sql, r.done)
		}
		if !want && a.toast != "这条命令不是只读的，不重跑" {
			t.Errorf("%s: toast %q", sql, a.toast)
		}
		c.busy = 0
	}
}

// A reply of more rows than the quick SQL shows is cut to them, and says so (F17.2).
func TestQuickRedisRowsCut(t *testing.T) {
	res := db.Result{Cols: []db.Col{{Name: "key"}}}
	for range quickRows + 50 {
		res.Rows = append(res.Rows, []db.Val{{S: "k"}})
	}
	a, _ := redisConsole(t, &execDB{res: map[string]db.Result{"KEYS *": res}}, someCommands(), "")
	feed(t, a, "<C-p>;KEYS *")
	answerQuickCmds(a, keys(t, a, "<CR>"))
	if q := a.palette.quick; len(q.res.Rows) != quickRows || !q.res.Truncated {
		t.Errorf("%d rows, truncated %v", len(q.res.Rows), q.res.Truncated)
	}
}

// A command the quick SQL sent is in the history and has the completion forget the
// names it was told of, whether or not the palette is still there when it answers:
// it ran (F17.2, §9.8).
func TestQuickRedisAnsweredAfterTheClose(t *testing.T) {
	a, _ := redisConsole(t, &execDB{}, someCommands(), "")
	c := a.sess.Conns[0]
	c.hints = map[string][]string{"keys\x00us": {"user:1"}}
	c.building = &keyIndex{}
	a.confirmDangerous = false
	feed(t, a, "<C-p>;SET newkey 1")
	if a.palette.comp != nil {
		feed(t, a, "<Esc>")
	}
	cmd := keys(t, a, "<CR>")
	feed(t, a, "<Esc>") // the palette goes before the answer comes
	if a.palette != nil {
		t.Fatal("the palette is still there")
	}
	answerQuickCmds(a, cmd)
	if hs, _ := config.LoadHistory(); len(hs) != 1 || hs[0].SQL != "SET newkey 1" {
		t.Errorf("history.jsonl: %+v", hs)
	}
	if c.hints != nil {
		t.Errorf("hints: %v", c.hints)
	}
}
