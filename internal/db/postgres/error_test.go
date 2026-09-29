package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestErrorLines(t *testing.T) {
	pe := &pgconn.PgError{Severity: "ERROR", Message: "duplicate key", Detail: "Key (id)=(1) already exists.", Code: "23505"}
	if got := ErrorLines(pe); !slices.Equal(got, []string{"ERROR: duplicate key", "DETAIL: Key (id)=(1) already exists."}) {
		t.Errorf("%q", got)
	}
	if got := ErrorLines(errors.New("conn closed")); !slices.Equal(got, []string{"ERROR: conn closed"}) {
		t.Errorf("%q", got)
	}
}
