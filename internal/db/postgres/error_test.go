package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestServerErrorOf(t *testing.T) {
	if e := ServerErrorOf(errors.New("conn closed")); e.Severity != "ERROR" || e.Message != "conn closed" || e.Code != "" || e.More != nil {
		t.Errorf("no server's: %+v", e)
	}
	pe := &pgconn.PgError{Severity: "ERROR", Code: "42703", Message: "column \"x\" does not exist", Detail: "one\ntwo", Hint: "try y", Position: 8}
	if e := ServerErrorOf(pe); e.Code != "42703" || e.Position != 8 || !slices.Equal(e.More, []string{"DETAIL: one", "two", "HINT: try y"}) {
		t.Errorf("%+v", e)
	}
}
