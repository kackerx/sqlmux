//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sqlmux/internal/db"
)

// saver is a Worker on a database of the test's own, a second session on
// it that reads, and changes rows behind the saver's back, and its DSN.
func saver(t *testing.T) (*db.Worker, func(sql string) string, string) {
	t.Helper()
	ctx := context.Background()
	dsn := ownDB(t)
	main, err := Connect(ctx, dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	w := db.NewWorker(main)
	t.Cleanup(func() { w.Close() })
	other, err := Connect(ctx, dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	// the first row's values as text, | between, NULL as <null>
	return w, func(sql string) string {
		t.Helper()
		r, err := other.Query(ctx, sql)
		if err != nil {
			t.Fatal(err)
		}
		var vs []string
		for _, v := range r.Rows[0] {
			if v.Null {
				v.S = "<null>"
			}
			vs = append(vs, v.S)
		}
		return strings.Join(vs, "|")
	}, dsn
}

func text(s string) db.Val { return db.Val{S: s} }

// Rows found by a primary key, a composite one or a unique index take text,
// NULL and DEFAULT; a json column's loaded value compares as text (§10.3).
func TestSave(t *testing.T) {
	ctx := context.Background()
	w, q, _ := saver(t)
	q("update t_user set active = false where id = 1 returning 1")
	rows := []struct {
		table string
		key   []string
		row   Row
		after string
	}{
		{"t_user", []string{"id"}, Row{Key: []string{"1"}, Cols: []Change{
			{Name: "name", Val: text("renamed"), Old: text("user_1")},
			{Name: "email", Val: db.Val{Null: true}, Old: text("user_1@example.com")},
			{Name: "active", Default: true, Old: text("false")},
		}}, "select name, email, active from t_user where id = 1"},
		{"t_order_item", []string{"order_id", "line_no"}, Row{Key: []string{"3", "2"}, Cols: []Change{
			{Name: "qty", Val: text("9"), Old: text("1")},
		}}, "select qty from t_order_item where order_id = 3 and line_no = 2"},
		{"t_sku", []string{"code"}, Row{Key: []string{"sku_1"}, Cols: []Change{
			{Name: "title", Val: text("SKU one"), Old: text("SKU 1")},
		}}, "select title from t_sku where code = 'sku_1'"},
		{"t_order", []string{"id"}, Row{Key: []string{"4"}, Cols: []Change{
			{Name: "raw", Val: text(`{"n": 40}`), Old: text(`{"n" : 4}`)},
		}}, "select raw from t_order where id = 4"},
	}
	want := []string{`renamed|<null>|t`, `9`, `SKU one`, `{"n": 40}`}
	for i, c := range rows {
		if failed, err := Save(ctx, w, "public", c.table, c.key, []Row{c.row}); err != nil {
			t.Fatalf("%s: row %d: %v", c.table, failed, err)
		}
		if got := q(c.after); got != want[i] {
			t.Errorf("%s: %s, want %s", c.table, got, want[i])
		}
	}
}

// A row changed or gone since it was loaded fails the whole save: nothing
// is written, and failed says which row (§10.3). So does a database error.
func TestSaveRollsBack(t *testing.T) {
	ctx := context.Background()
	w, q, _ := saver(t)
	ok := Row{Key: []string{"1"}, Cols: []Change{{Name: "note", Val: text("saved"), Old: db.Val{Null: true}}}}
	stale := Row{Key: []string{"2"}, Cols: []Change{{Name: "note", Val: text("mine"), Old: db.Val{Null: true}}}}
	q("update t_order set note = 'theirs' where id = 2 returning 1")
	if failed, err := Save(ctx, w, "public", "t_order", []string{"id"}, []Row{ok, stale}); !errors.Is(err, ErrStale) || failed != 1 {
		t.Fatalf("changed behind: row %d, %v", failed, err)
	}
	gone := Row{Key: []string{"999999"}, Cols: ok.Cols}
	if failed, err := Save(ctx, w, "public", "t_order", []string{"id"}, []Row{ok, gone}); !errors.Is(err, ErrStale) || failed != 1 {
		t.Fatalf("gone: row %d, %v", failed, err)
	}
	bad := Row{Key: []string{"3"}, Cols: []Change{{Name: "amount", Val: text("abc"), Old: text("3.99")}}}
	if failed, err := Save(ctx, w, "public", "t_order", []string{"id"}, []Row{ok, bad}); sqlState(err) != "22P02" || failed != 1 {
		t.Fatalf("bad input: row %d, %v", failed, err)
	}
	if got := q("select coalesce(note, '<null>') from t_order where id = 1"); got != "<null>" {
		t.Errorf("row 1 was written: %s", got)
	}
}

// Cancelling stops the UPDATEs, rolls back and leaves the connection in
// use; COMMIT and ROLLBACK themselves are out of its reach (§10.3).
func TestSaveCancel(t *testing.T) {
	ctx := context.Background()
	w, q, dsn := saver(t)
	lock, err := Connect(ctx, dsn, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := lock.Exec(ctx, "begin; select 1 from t_order where id = 5 for update", 0); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		row := Row{Key: []string{"5"}, Cols: []Change{{Name: "note", Val: text("x"), Old: db.Val{Null: true}}}}
		_, err := Save(ctx, w, "public", "t_order", []string{"id"}, []Row{row})
		done <- err
	}()
	for q("select count(*) from pg_stat_activity where wait_event_type = 'Lock' and datname = current_database()") != "1" {
		time.Sleep(20 * time.Millisecond)
	}
	w.Cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	lock.Exec(ctx, "rollback", 0)
	if got := q("select coalesce(note, '<null>') from t_order where id = 5"); got != "<null>" {
		t.Errorf("written: %s", got)
	}
	if r, err := w.Query(ctx, "select 1"); err != nil || r.Rows[0][0].S != "1" {
		t.Errorf("after the cancel: %v", err)
	}
}
