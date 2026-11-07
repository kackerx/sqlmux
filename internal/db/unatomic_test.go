package db_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/db/postgres"
)

// unatomic is PG's SQL with an Unatomic engine's words for UPDATE and DELETE
// and a Typer's arguments, as ClickHouse has them (§10.3).
type unatomic struct{ postgres.Engine }

func (unatomic) Write(table, set, where string) string {
	if set == "" {
		return "alter table " + table + " delete where " + where
	}
	return "alter table " + table + " update " + set + " where " + where
}

func (unatomic) Typed(arg, typ string) string { return "cast(" + arg + " as " + typ + ")" }

// fakeConn answers a count with counts[the statement], a write with affected
// (-1 unless set); it keeps what was sent.
type fakeConn struct {
	sent     []string
	counts   map[string]string
	affected map[string]int64
	fail     string // a statement that fails
}

func (c *fakeConn) Exec(context.Context, string, int) ([]db.Result, error) { return nil, nil }
func (c *fakeConn) Close() error                                           { return nil }
func (c *fakeConn) Query(_ context.Context, sql string, args ...db.Val) (db.Result, error) {
	c.sent = append(c.sent, sql)
	if sql == c.fail {
		return db.Result{}, errors.New("server says no")
	}
	if n, ok := c.counts[sql]; ok {
		return db.Result{Rows: [][]db.Val{{{S: n}}}}, nil
	}
	n, ok := c.affected[sql]
	if !ok {
		n = -1
	}
	return db.Result{Affected: n}, nil
}

var unatomicChanges = db.Changes{
	Deletes: [][]string{{"1"}},
	Updates: []db.Row{{Key: []string{"2"}, Cols: []db.Change{{Name: "name", Val: db.Val{S: "b2"}, Old: db.Val{S: "b"}}}}},
	Inserts: [][]db.Change{{{Name: "id", Val: db.Val{S: "9"}}}, nil},
	Types:   map[string]string{"id": "UInt64", "name": "String"},
}

// An Unatomic engine's statements are its own words, each argument in its
// column's type, and each UPDATE and DELETE comes with the count of its rows.
func TestUnatomicStatements(t *testing.T) {
	stmts := db.Statements(unatomic{}, "s", "t", []string{"id"}, unatomicChanges)
	var got []string
	for _, s := range stmts {
		got = append(got, s.SQL+" | "+s.Count)
	}
	want := []string{
		`alter table "s"."t" delete where "id" = cast($1 as UInt64) | select count(*) from "s"."t" where "id" = cast($1 as UInt64)`,
		`alter table "s"."t" update "name" = cast($1 as String) where "id" = cast($2 as UInt64) and format('%s', "name") = cast($3 as String)` +
			` | select count(*) from "s"."t" where "id" = cast($1 as UInt64) and format('%s', "name") = cast($2 as String)`,
		`insert into "s"."t" ("id") values (cast($1 as UInt64)) | `,
		`insert into "s"."t" ("id") values (DEFAULT) | `, // no DEFAULT VALUES: the key's DEFAULT
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statements:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !reflect.DeepEqual(stmts[1].Args, []db.Val{{S: "b2"}, {S: "2"}, {S: "b"}}) || !reflect.DeepEqual(stmts[1].CountArgs, []db.Val{{S: "2"}, {S: "b"}}) {
		t.Errorf("args %v, count's %v", stmts[1].Args, stmts[1].CountArgs)
	}
	if p := db.Preview(unatomic{}, "s", "t", []string{"id"}, unatomicChanges); p[0] != `alter table "s"."t" delete where "id" = cast('1' as UInt64)` {
		t.Errorf("preview: %v", p)
	}
}

// Each statement goes alone, an UPDATE or a DELETE only after its count says
// one row; what fails stops the rest, and the error says how many were
// written before it, none of which is taken back (§10.3).
func TestUnatomicSave(t *testing.T) {
	stmts := db.Statements(unatomic{}, "s", "t", []string{"id"}, unatomicChanges)
	counts := func(del, upd string) map[string]string {
		return map[string]string{stmts[0].Count: del, stmts[1].Count: upd}
	}
	for _, c := range []struct {
		name   string
		conn   *fakeConn
		failed int
		is     error
		sent   int // statements written, counts aside
	}{
		{"all", &fakeConn{counts: counts("1", "1"), affected: map[string]int64{stmts[2].SQL: 1, stmts[3].SQL: 1}}, -1, nil, 4},
		{"first gone", &fakeConn{counts: counts("0", "1")}, 0, db.ErrGone, 0},
		{"second changed", &fakeConn{counts: counts("1", "0")}, 1, db.ErrStale, 1},
		{"two match", &fakeConn{counts: counts("1", "2")}, 1, db.ErrStale, 1},
		{"insert none", &fakeConn{counts: counts("1", "1"), affected: map[string]int64{stmts[2].SQL: 0}}, 2, db.ErrNotInserted, 3},
		{"server error", &fakeConn{counts: counts("1", "1"), fail: stmts[1].SQL}, 1, nil, 2},
	} {
		w := db.NewWorker(c.conn)
		failed, _, err := db.Save(context.Background(), w, unatomic{}, "s", "t", []string{"id"}, unatomicChanges, false)
		if failed != c.failed || c.is != nil && !errors.Is(err, c.is) || c.failed >= 0 && err == nil {
			t.Errorf("%s: failed %d, err %v", c.name, failed, err)
		}
		var written db.Written
		switch {
		case c.failed > 0 && (!errors.As(err, &written) || written.N != c.failed):
			t.Errorf("%s: %v does not say %d were written", c.name, err, c.failed)
		case c.failed == 0 && errors.As(err, &written):
			t.Errorf("%s: nothing was written, it says %v", c.name, err)
		}
		writes := 0
		for _, s := range c.conn.sent {
			if !strings.HasPrefix(s, "select count") {
				writes++
			}
		}
		if writes != c.sent {
			t.Errorf("%s: %d sent, want %d: %s", c.name, writes, c.sent, fmt.Sprint(c.conn.sent))
		}
		for _, s := range c.conn.sent {
			if s == "begin" || s == "commit" || s == "rollback" {
				t.Errorf("%s: %s sent to an engine with no transactions", c.name, s)
			}
		}
	}
}
