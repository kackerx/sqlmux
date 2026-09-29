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
			{Name: "active", Default: true, Old: text("f")},
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
		if failed, err := Save(ctx, w, "public", c.table, c.key, Changes{Updates: []Row{c.row}}); err != nil {
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
	if failed, err := Save(ctx, w, "public", "t_order", []string{"id"}, Changes{Updates: []Row{ok, stale}}); !errors.Is(err, ErrStale) || failed != 1 {
		t.Fatalf("changed behind: row %d, %v", failed, err)
	}
	gone := Row{Key: []string{"999999"}, Cols: ok.Cols}
	if failed, err := Save(ctx, w, "public", "t_order", []string{"id"}, Changes{Updates: []Row{ok, gone}}); !errors.Is(err, ErrStale) || failed != 1 {
		t.Fatalf("gone: row %d, %v", failed, err)
	}
	bad := Row{Key: []string{"3"}, Cols: []Change{{Name: "amount", Val: text("abc"), Old: text("3.99")}}}
	if failed, err := Save(ctx, w, "public", "t_order", []string{"id"}, Changes{Updates: []Row{ok, bad}}); sqlState(err) != "22P02" || failed != 1 {
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
		_, err := Save(ctx, w, "public", "t_order", []string{"id"}, Changes{Updates: []Row{row}})
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

// What a row was loaded as, each type's output text, is what the check
// finds in it: boolean's f, inet without a mask, char(n) padded, json,
// floats, times, bytea, arrays, enums, and NULL (§10.3).
func TestSaveChecksEveryType(t *testing.T) {
	ctx := context.Background()
	w, _, _ := saver(t)
	if _, err := w.Exec(ctx, `create table types (id int primary key, b boolean, ip inet, c char(5), j json, jb jsonb,
		n numeric(10, 2), r real, d double precision, ts timestamptz, iv interval, by bytea, arr int[], e order_status, nul text);
		insert into types values (1, false, '10.0.0.1', 'ab', '{"a" : 1}', '{"a": 1}', 1.50, 0.1, 0.1,
		'2026-09-28 10:00:00.5+08', '1 day 02:00', '\xdeadbeef', '{1,2}', 'done', null)`, 0); err != nil {
		t.Fatal(err)
	}
	r, err := w.Query(ctx, "select * from types")
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range r.Cols[1:] {
		old := r.Rows[0][i+1]
		row := Row{Key: []string{"1"}, Cols: []Change{{Name: c.Name, Val: db.Val{S: "{}"}, Old: old}}}
		if !old.Null {
			row.Cols[0].Val = old // written back as it was: only the check is under test
		}
		if _, err := Save(ctx, w, "public", "types", []string{"id"}, Changes{Updates: []Row{row}}); err != nil {
			t.Errorf("%s loaded as %+v: %v", c.Name, old, err)
		}
	}
}

// Deletes, updates and inserts in one save, in that order (§10.6): an
// INSERT writes the columns set, DEFAULT for the rest, DEFAULT VALUES with
// none; a composite key finds the row a DELETE takes.
func TestSaveInsertsAndDeletes(t *testing.T) {
	ctx := context.Background()
	w, q, _ := saver(t)
	if _, err := w.Exec(ctx, "create table all_default (id bigserial primary key, n int default 7)", 0); err != nil {
		t.Fatal(err)
	}
	ch := Changes{
		Deletes: [][]string{{"3", "1"}},
		Updates: []Row{{Key: []string{"3", "2"}, Cols: []Change{{Name: "qty", Val: text("9"), Old: text("1")}}}},
		Inserts: [][]Change{{{Name: "order_id", Val: text("3")}, {Name: "line_no", Val: text("99")}, {Name: "sku_code", Val: text("sku_1")}, {Name: "qty", Default: true}}},
	}
	if failed, err := Save(ctx, w, "public", "t_order_item", []string{"order_id", "line_no"}, ch); err != nil {
		t.Fatalf("statement %d: %v", failed, err)
	}
	if got := q("select (select count(*) from t_order_item where order_id = 3 and line_no = 1), (select qty from t_order_item where order_id = 3 and line_no = 2), (select qty from t_order_item where order_id = 3 and line_no = 99)"); got != "0|9|1" {
		t.Errorf("deleted, updated, inserted with qty's default: %s", got)
	}
	if _, err := Save(ctx, w, "public", "t_user", []string{"id"}, Changes{Inserts: [][]Change{{{Name: "name", Val: text("new one")}}}}); err != nil {
		t.Fatal(err)
	}
	if got := q("select active, created_at is not null from t_user where name = 'new one'"); got != "t|t" {
		t.Errorf("the other columns' defaults: %s", got)
	}
	if _, err := Save(ctx, w, "public", "all_default", []string{"id"}, Changes{Inserts: [][]Change{nil}}); err != nil {
		t.Fatal(err)
	}
	if got := q("select n from all_default"); got != "7" {
		t.Errorf("default values: %s", got)
	}
}

// One statement failing rolls all back; failed counts deletes, updates,
// then inserts. A row to delete that is gone is ErrGone (§10.6).
func TestSaveInsertsAndDeletesRollBack(t *testing.T) {
	ctx := context.Background()
	w, q, _ := saver(t)
	key := []string{"order_id", "line_no"}
	gone := Changes{Deletes: [][]string{{"3", "1"}, {"999999", "1"}}}
	if failed, err := Save(ctx, w, "public", "t_order_item", key, gone); !errors.Is(err, ErrGone) || failed != 1 {
		t.Fatalf("gone: statement %d, %v", failed, err)
	}
	bad := Changes{
		Deletes: [][]string{{"3", "1"}},
		Updates: []Row{{Key: []string{"3", "2"}, Cols: []Change{{Name: "qty", Val: text("9"), Old: text("1")}}}},
		Inserts: [][]Change{{{Name: "order_id", Val: text("3")}, {Name: "line_no", Val: text("98")}, {Name: "sku_code", Val: text("sku_1")}}, {{Name: "qty", Val: text("x")}}},
	}
	if failed, err := Save(ctx, w, "public", "t_order_item", key, bad); sqlState(err) == "" || failed != 3 {
		t.Fatalf("the second insert: statement %d, %v", failed, err)
	}
	if got := q("select (select count(*) from t_order_item where order_id = 3 and line_no in (1, 98)), (select qty from t_order_item where order_id = 3 and line_no = 2)"); got != "1|1" {
		t.Errorf("rolled back: %s", got)
	}
	if _, err := w.Exec(ctx, "select 1", 0); err != nil {
		t.Errorf("after: %v", err)
	}
}
