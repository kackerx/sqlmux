//go:build integration

package app

import (
	"context"
	"testing"

	"sqlmux/internal/db"
	"sqlmux/internal/db/postgres"
)

// PG 17 takes what cellCases say it takes: cellCheck follows its input
// functions, but where a case says otherwise (§10.7).
func TestCellCasesAgainstPG(t *testing.T) {
	ctx := context.Background()
	c, err := postgres.Connect(ctx, postgres.IntegrationDSN(t), "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cc := range cellCases {
		_, err := c.Query(ctx, "select $1::"+cc.typ, db.Val{S: cc.text})
		if (err == nil) != cc.pg {
			t.Errorf("%s %q: PG takes it %v, the case says %v (%v)", cc.typ, cc.text, err == nil, cc.pg, err)
		}
	}
}
