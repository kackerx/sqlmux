package app

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/editor"
	"github.com/kackerx/sqlmux/internal/keymap"
)

// With the match in the background the list's start is a column of the cursor's line,
// as the one made at once is, on a later line of the console too (F20.10): the user's
// hget key xx under other lines panicked drawing it.
func TestKeyListStartOnALaterLine(t *testing.T) {
	async(t)
	a, c, _ := keyConsole(t, "key:1", "key:2", "user:1")
	c.ed.Load("get user:1\nget key:1\nhget key")
	feed(t, a, "GA")
	_, cmd := a.Update(teaKey(":"))
	ms := matched(cmd)
	if len(ms) == 0 {
		t.Fatal("no match asked")
	}
	a.Update(ms[len(ms)-1])
	if c.comp == nil || c.comp.start != 5 {
		t.Fatalf("the list: %+v, want it to begin at column 5 of the line", c.comp)
	}
	a.View()
}

// The list of candidates and the text under it stay together (F20.10): whatever the
// keys and the answers of the background matches come in, the list begins at or before
// the cursor, on its line, and a frame is drawn. Typing, deleting, new lines, ↵, C-n,
// esc and the cursor's moves are mixed with the answers coming late and out of order.
func TestCompletionFollowsTheText(t *testing.T) {
	keys := []string{"h", "g", "e", "t", " ", "k", "e", "y", "x", ":", "_", "1", "p", "i18n", "<BS>", "<BS>", "<BS>", "<CR>", "<CR>",
		"<C-n>", "<Esc>", "<Left>", "<Right>", "<Up>", "<Down>", "<C-w>", "<C-u>", "i", "a", "A", "o", "I", "<Home>", "<End>"}
	for _, inline := range []int{0, 2000} {
		for seed := range int64(40) {
			t.Run(fmt.Sprintf("inline=%d/seed=%d", inline, seed), func(t *testing.T) {
				was, delay, hint := matchInline, autosaveDelay, hintDelay
				matchInline, autosaveDelay, hintDelay = inline, 0, 0
				t.Cleanup(func() { matchInline, autosaveDelay, hintDelay = was, delay, hint })
				var names []string
				for i := range 30 {
					names = append(names, fmt.Sprintf("key%d:p:x%d", i%5, i), fmt.Sprintf("i18n_agent:expert:%d", i))
				}
				a, c, conn := keyConsole(t, names...)
				c.ed.Load("hget key xx")
				feed(t, a, "A")
				rnd := rand.New(rand.NewSource(seed))
				var late []matchMsg
				var log []string
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic %v after %s\n%q", r, strings.Join(log, " "), c.ed.Lines())
					}
				}()
				for range 600 {
					var cmd tea.Cmd
					switch n := rnd.Intn(10); {
					case n < 7:
						k := keys[rnd.Intn(len(keys))]
						if k == "<CR>" && c.ed.Mode() != editor.Insert {
							continue // runs the line: no database here
						}
						log = append(log, k)
						ks, err := keymap.Parse(k)
						if err != nil {
							t.Fatal(err)
						}
						for _, k := range ks {
							_, cmd = a.Update(teaKey(k))
							late = append(late, matched(cmd)...)
						}
					case n < 9 && len(late) > 0: // an answer comes, not the last asked one first
						i := rnd.Intn(len(late))
						log = append(log, "<answer>")
						_, cmd = a.Update(late[i])
						late = append(late[:i], late[i+1:]...)
						late = append(late, matched(cmd)...)
					default: // the names change under it
						log = append(log, "<recomplete>")
						late = append(late, matched(a.recomplete(conn))...)
					}
					if comp := c.comp; comp != nil {
						lines, cur := c.ed.Lines(), c.ed.Cursor()
						if cur.Line >= len(lines) || comp.start > cur.Col || cur.Col > len(lines[cur.Line]) {
							t.Fatalf("the list begins at %d, the cursor is at %v in %q, after %s", comp.start, cur, lines, strings.Join(log, " "))
						}
					}
					a.View()
				}
			})
		}
	}
}
