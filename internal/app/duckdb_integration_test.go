//go:build integration && (!linux || duckdb)

package app

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/config"
	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/duckdb"
)

// openDuck is an App over a DuckDB file of the seed, with table name open.
func openDuck(t *testing.T, name string) (*App, *Conn, *dataTab) {
	t.Helper()
	if os.Getenv("SQLMUX_TEST_DUCKDB") == "" {
		t.Skip("SQLMUX_TEST_DUCKDB is not set")
	}
	a := opened(t, config.Connection{Name: "duck", Engine: "duckdb", DSN: duckdb.OwnDB(t)})
	duck := a.sess.Conns[0]
	settle(a, a.openTable(duck, duck.Tables[slices.IndexFunc(duck.Tables, func(tb db.Table) bool { return tb.Schema == "main" && tb.Name == name })]))
	tab := dataOf(a.focused())
	if tab == nil {
		t.Fatalf("%s did not open", name)
	}
	return a, duck, tab
}

// A DuckDB table reads in the library's own text, a table with no key is read
// only and says why, a cell is saved to the row by its key (§8.1).
func TestDuckDBTables(t *testing.T) {
	_, _, tab := openDuck(t, "t_types")
	if tab.page.Cols[1].Name != "tags" || tab.page.Rows[0][1].S != "[1, 2]" || tab.page.Rows[0][2].S != "{'a': 1, 'b': x}" || tab.page.Rows[0][3].S != "{k=1}" || tab.page.Rows[0][8].S != "<binary 2 bytes>" {
		t.Errorf("t_types: %+v", tab.page.Rows[0])
	}
	if !tab.page.Rows[1][1].Null || tab.page.Rows[1][1].S != "" {
		t.Errorf("a NULL list: %+v", tab.page.Rows[1][1])
	}
	tab.col = len(tab.page.Cols) - 1 // area
	if s, ok := tab.cellAtCursor(); !ok || s.why == "" {
		t.Errorf("a generated column's cell takes an edit: %+v", s)
	}
	_, _, log := openDuck(t, "t_log")
	if why := log.whyNoEdit(); !strings.Contains(why, "t_log") || !strings.Contains(why, "只读") {
		t.Errorf("t_log: %q", why)
	}
	a2, duck, user := openDuck(t, "t_user")
	email := slices.IndexFunc(user.page.Cols, func(c db.Col) bool { return c.Name == "email" })
	if !user.page.Rows[4][email].Null {
		t.Fatalf("user_5 has no email: %+v", user.page.Rows[4])
	}
	user.edits = map[editKey]edit{{user.rowKey(4), "email"}: {val: db.Val{S: "five@example.com"}, orig: user.page.Rows[4][email]}}
	settle(a2, a2.save())
	if got := user.page.Rows[4][email].S; got != "five@example.com" || user.bar != nil {
		t.Fatalf("saved: %q, bar %v", got, user.bar)
	}
	// another connection changed it since it was read: the row has changed
	if _, err := duck.Meta.Query(t.Context(), "select 1"); err != nil {
		t.Fatal(err)
	}
	if err := duck.Main.Run(t.Context(), func(ctx context.Context, c db.Conn) error {
		_, err := c.Query(ctx, "update t_user set email = 'other' where id = 5")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	user.edits = map[editKey]edit{{user.rowKey(4), "email"}: {val: db.Val{S: "six@example.com"}, orig: user.page.Rows[4][email]}}
	settle(a2, a2.save())
	if user.bar == nil {
		t.Errorf("a row that changed was saved over")
	}
}
