//go:build integration

package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestPage(t *testing.T) {
	c := connect(t, true)
	ctx := context.Background()
	orders := Query{Schema: "public", Table: "t_order", Key: []string{"id"}, Limit: 100}
	r, next, err := Page(ctx, c, orders)
	if err != nil || len(r.Rows) != 100 || !next || r.Rows[0][0].S != "1" || r.Rows[99][0].S != "100" {
		t.Fatalf("first page: %d rows, next %v, %v", len(r.Rows), next, err)
	}
	last := orders
	last.Offset = 5900
	if r, next, _ = Page(ctx, c, last); len(r.Rows) != 100 || next {
		t.Errorf("the last full page: %d rows, next %v", len(r.Rows), next)
	}
	// a composite key orders by its columns in key order
	r, _, err = Page(ctx, c, Query{Schema: "public", Table: "t_order_item", Key: []string{"order_id", "line_no"}, Limit: 3})
	if err != nil || r.Rows[1][0].S != "1" || r.Rows[1][1].S != "2" || r.Rows[2][0].S != "2" {
		t.Errorf("composite key order: %v %v", r.Rows, err)
	}
	// no key: no order, still paged; names needing quotes are quoted
	if r, next, err = Page(ctx, c, Query{Schema: "public", Table: "t_log", Limit: 2}); err != nil || len(r.Rows) != 2 || !next {
		t.Errorf("t_log: %d rows, next %v, %v", len(r.Rows), next, err)
	}
	if _, _, err := Page(ctx, c, Query{Schema: "public", Table: `no"such`, Limit: 1}); sqlState(err) != "42P01" {
		t.Errorf("a quoted missing table: %v", err)
	}
}

// WHERE goes in as typed (§9.6): its OR stays inside, a trailing comment
// comments nothing else out, and it can't smuggle in a second statement.
func TestPageWhere(t *testing.T) {
	c := connect(t, true)
	ctx := context.Background()
	q := Query{Schema: "public", Table: "t_order", Key: []string{"id"}, Limit: 5}
	for where, first := range map[string]string{
		"status = 'done' -- 备注":               "2",
		"id = 7 or id = 3":                    "3",
		"deleted_at is not null and id > 150": "200",
		"   ":                                 "1",
	} {
		q.Where = where
		r, _, err := Page(ctx, c, q)
		if err != nil || r.Rows[0][0].S != first {
			t.Errorf("%q: first id %v, want %s; %v", where, r.Rows, first, err)
		}
		if n, err := Count(ctx, c, q); err != nil || n == 0 {
			t.Errorf("%q: count %d %v", where, n, err)
		}
	}
	q.Where = "status = 'done'"
	if n, _ := Count(ctx, c, q); n != 1500 {
		t.Errorf("count done: %d", n)
	}
	q.Where = "1=1; drop table t_log"
	if _, _, err := Page(ctx, c, q); sqlState(err) != "42601" {
		t.Errorf("two statements: %v", err)
	}
	if _, _, err := Page(ctx, c, Query{Schema: "public", Table: "t_log", Where: "1=1); select (1", Limit: 1}); err == nil || !strings.Contains(err.Error(), "SQLSTATE") {
		t.Errorf("closing the parenthesis early: %v", err)
	}
	q.Where = "no_such_column = 1"
	if _, _, err := Page(ctx, c, q); sqlState(err) != "42703" {
		t.Errorf("a bad condition says why: %v", err)
	}
	// ordered by a column, the row identity breaks its ties
	r, _, _ := Page(ctx, c, Query{Schema: "public", Table: "t_order", Order: "status", Desc: true, Key: []string{"id"}, Limit: 2})
	if r.Rows[0][2].S != "failed" || r.Rows[0][0].S != "3" || r.Rows[1][0].S != "7" {
		t.Errorf("status desc, id: %v", r.Rows)
	}
}
