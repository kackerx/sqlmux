package postgres

import (
	"reflect"
	"testing"

	"sqlmux/internal/db"
)

// One UPDATE a row: DEFAULT as the keyword, the row identity with =, every
// changed column's loaded value as its output text, or IS NULL (§10.3).
func TestUpdateSQL(t *testing.T) {
	sql, args := update("public", "t_order_item", []string{"order_id", "line_no"}, Row{Key: []string{"3", "2"}, Cols: []Change{
		{Name: "qty", Val: db.Val{S: "9"}, Old: db.Val{S: "1"}},
		{Name: "sku code", Default: true, Old: db.Val{Null: true}},
	}})
	want := `update "public"."t_order_item" set "qty" = $1, "sku code" = DEFAULT where "order_id" = $2 and "line_no" = $3` +
		` and format('%s', "qty") = $4 and "sku code" is null`
	if sql != want {
		t.Errorf("sql:\n%s\nwant\n%s", sql, want)
	}
	if !reflect.DeepEqual(args, []db.Val{{S: "9"}, {S: "3"}, {S: "2"}, {S: "1"}}) {
		t.Errorf("args %v", args)
	}
}
