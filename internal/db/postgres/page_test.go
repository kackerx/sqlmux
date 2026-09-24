//go:build integration

package postgres

import (
	"context"
	"testing"
)

func TestPage(t *testing.T) {
	c := connect(t, true)
	ctx := context.Background()
	r, next, err := Page(ctx, c, "public", "t_order", []string{"id"}, 100, 0)
	if err != nil || len(r.Rows) != 100 || !next || r.Rows[0][0].S != "1" || r.Rows[99][0].S != "100" {
		t.Fatalf("first page: %d rows, next %v, %v", len(r.Rows), next, err)
	}
	if r, next, _ = Page(ctx, c, "public", "t_order", []string{"id"}, 100, 5900); len(r.Rows) != 100 || next {
		t.Errorf("the last full page: %d rows, next %v", len(r.Rows), next)
	}
	// a composite key orders by its columns in key order
	r, _, err = Page(ctx, c, "public", "t_order_item", []string{"order_id", "line_no"}, 3, 0)
	if err != nil || r.Rows[1][0].S != "1" || r.Rows[1][1].S != "2" || r.Rows[2][0].S != "2" {
		t.Errorf("composite key order: %v %v", r.Rows, err)
	}
	// no key: no order, still paged; names needing quotes are quoted
	if r, next, err = Page(ctx, c, "public", "t_log", nil, 2, 0); err != nil || len(r.Rows) != 2 || !next {
		t.Errorf("t_log: %d rows, next %v, %v", len(r.Rows), next, err)
	}
	if _, _, err := Page(ctx, c, "public", `no"such`, nil, 1, 0); sqlState(err) != "42P01" {
		t.Errorf("a quoted missing table: %v", err)
	}
}
