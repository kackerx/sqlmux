package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

// While the index of a db is made the first time, with no file to start from, a key typed has its
// box at once with one dim row, the index being built; the names of the prefix go in when they come and
// the row stays; the new index takes the row away; one that could not be made says why instead
// (F21.7).
func TestKeyIndexFeedback(t *testing.T) {
	a, c := redisConsole(t, &execDB{}, someCommands(), "")
	conn := a.sess.Conns[0]
	conn.docs, conn.State = someDocs(), connReady
	conn.building = &keyIndex{started: true, seen: map[string]bool{}}
	pane := func() string {
		r := a.layout()[a.focused().ID]
		var lines []string
		for _, l := range strings.Split(a.render().String(), "\n")[r.Min.Y : r.Min.Y+7] {
			lines = append(lines, ansi.Cut(l, r.Min.X, r.Max.X))
		}
		return strings.Join(lines, "\n")
	}
	c.ed.Load("get us")
	feed(t, a, "A")
	a.queryComplete(c, false)
	if c.comp != nil || c.foot != "索引建立中" || c.footStart != len("get ") {
		t.Fatalf("a box of the row alone: comp %v, foot %q at %d", c.comp, c.foot, c.footStart)
	}
	t.Run("building", func(t *testing.T) { golden.RequireEqual(t, pane()) })

	// what the server gave for the prefix comes in: the names are there, the row too
	conn.hints = map[string][]string{"keys\x00us": {"user:1", "user:2"}}
	a.recomplete(conn)
	if c.comp == nil || len(c.comp.items) != 2 || c.comp.foot != "索引建立中" || c.foot != "" {
		t.Fatalf("the prefix's names: %+v, row %q", c.comp, c.foot)
	}

	// the index takes its place: no row
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, names: []string{"user:1", "user:2", "other"}, total: 3})
	if c.comp == nil || c.comp.foot != "" || c.foot != "" || len(c.comp.items) != 2 {
		t.Errorf("the index is made: %+v", c.comp)
	}

	// one that fails: the row says why, a key typed does not try again, R does
	conn.index, conn.hints = nil, nil
	a.rebuildIndex(conn)
	a.gotIndex(indexMsg{conn: conn, ix: conn.building, err: errBoom})
	if conn.building != nil || c.comp != nil || c.foot != "索引建立失败：boom" {
		t.Fatalf("it failed: building %v, comp %v, row %q", conn.building, c.comp, c.foot)
	}
	t.Run("failed", func(t *testing.T) { golden.RequireEqual(t, pane()) })
	a.queryComplete(c, false)
	if conn.building != nil || c.foot != "索引建立失败：boom" {
		t.Errorf("tried again by a key typed: building %v, row %q", conn.building, c.foot)
	}
	a.refreshIndex(conn) // R in the tree
	a.queryComplete(c, false)
	if conn.building == nil || conn.indexErr != "" || c.foot != "索引建立中" {
		t.Errorf("tried again by R: building %v, error %q, row %q", conn.building, conn.indexErr, c.foot)
	}

	// the cursor left INSERT: the box is not drawn
	feed(t, a, "<Esc>")
	if strings.Contains(pane(), "索引建立中") {
		t.Error("the row is drawn in NORMAL")
	}
}
