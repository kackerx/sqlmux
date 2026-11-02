package app

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kackerx/sqlmux/internal/db"
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

// lateMsgs are the answers a run of commands asks for that come later: the matches in
// the background and the columns of a table, which the test gives a fixed set of.
func lateMsgs(cmd tea.Cmd) (out []tea.Msg) {
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, c := range m {
				walk(c)
			}
		case matchMsg:
			out = append(out, m)
		case colsMsg:
			m.cols, m.err = db.Columns{Cols: []db.Column{{Name: "id", Type: "bigint"}, {Name: "user_id", Type: "bigint"}, {Name: "status", Type: "text"}}}, nil
			out = append(out, m)
		}
	}
	walk(cmd)
	return out
}

// fuzzSeeds is how many random runs each of the completion tests makes: a few by
// default, SQLMUX_FUZZ_SEEDS for more (a hundred takes half a minute). Seed 27 of the
// Redis console is the one that found F20.10's panic, and is always run.
func fuzzSeeds() []int64 {
	n, _ := strconv.Atoi(os.Getenv("SQLMUX_FUZZ_SEEDS"))
	seeds := []int64{27, 0, 1, 2, 3}
	for s := int64(4); len(seeds) < n; s++ {
		if s != 27 {
			seeds = append(seeds, s)
		}
	}
	return seeds
}

// fuzzCompletion types keys of a random run into a query tab, mixed with the answers
// of what the typing asked for, which come late and in any order, and with what makes
// its list again (poke); after each step the list begins at or before the cursor, on
// its line, and a frame is drawn.
func fuzzCompletion(t *testing.T, a *App, c *queryTab, seed int64, keys []string, poke func() tea.Cmd) {
	rnd := rand.New(rand.NewSource(seed))
	var late []tea.Msg
	var log []string
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic %v after %s\n%q", r, strings.Join(log, " "), c.ed.Lines())
		}
	}()
	steps := 200
	if os.Getenv("SQLMUX_FUZZ_SEEDS") != "" {
		steps = 600
	}
	for range steps {
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
				late = append(late, lateMsgs(cmd)...)
			}
		case n < 9 && len(late) > 0: // an answer comes, not the last asked one first
			i := rnd.Intn(len(late))
			log = append(log, "<answer>")
			_, cmd = a.Update(late[i])
			late = append(late[:i], late[i+1:]...)
			late = append(late, lateMsgs(cmd)...)
		default: // what the list is made of changes under it
			log = append(log, "<again>")
			late = append(late, lateMsgs(poke())...)
		}
		if comp := c.comp; comp != nil {
			lines, cur := c.ed.Lines(), c.ed.Cursor()
			if cur.Line >= len(lines) || comp.start > cur.Col || cur.Col > len(lines[cur.Line]) {
				t.Fatalf("the list begins at %d, the cursor is at %v in %q, after %s", comp.start, cur, lines, strings.Join(log, " "))
			}
		}
		a.View()
	}
}

var fuzzControl = []string{"<BS>", "<BS>", "<BS>", "<CR>", "<CR>", "<C-n>", "<Esc>", "<Left>", "<Right>", "<Up>", "<Down>", "<C-w>", "<C-u>", "i", "a", "A", "o", "I", "<Home>", "<End>"}

// The list of candidates and the text under it stay together (F20.10): in a Redis
// console, its match going on in the background or at once, and in a SQL query tab,
// whose columns come when a table is named; typing, deleting, new lines, ↵, C-n, esc
// and the cursor's moves are mixed with the answers coming late and out of order.
func TestCompletionFollowsTheText(t *testing.T) {
	redisKeys := append([]string{"h", "g", "e", "t", " ", "k", "e", "y", "x", ":", "_", "1", "p", "i18n"}, fuzzControl...)
	sqlKeys := append([]string{"s", "e", "l", "*", " ", "f", "r", "o", "m", "t", "_", "u", "d", ".", ",", "(", ")", "w", "h", "=", "o", "t_order", "id"}, fuzzControl...)
	delay, hint := autosaveDelay, hintDelay
	autosaveDelay, hintDelay = 0, 0
	t.Cleanup(func() { autosaveDelay, hintDelay = delay, hint })
	for _, seed := range fuzzSeeds() {
		for _, inline := range []int{0, 2000} {
			t.Run(fmt.Sprintf("redis/inline=%d/seed=%d", inline, seed), func(t *testing.T) {
				was := matchInline
				matchInline = inline
				t.Cleanup(func() { matchInline = was })
				var names []string
				for i := range 30 {
					names = append(names, fmt.Sprintf("key%d:p:x%d", i%5, i), fmt.Sprintf("i18n_agent:expert:%d", i))
				}
				a, c, conn := keyConsole(t, names...)
				c.ed.Load("hget key xx")
				feed(t, a, "A")
				fuzzCompletion(t, a, c, seed, redisKeys, func() tea.Cmd { return a.recomplete(conn) })
			})
		}
		t.Run(fmt.Sprintf("sql/seed=%d", seed), func(t *testing.T) {
			a, c := inQuery(t, "", "")
			c.ed.Load("select * from t_order")
			feed(t, a, "A")
			fuzzCompletion(t, a, c, seed, sqlKeys, func() tea.Cmd { return a.queryComplete(c, false) })
		})
	}
}
