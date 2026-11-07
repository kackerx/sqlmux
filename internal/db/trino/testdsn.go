//go:build integration

package trino

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"

	"github.com/kackerx/sqlmux/internal/db/postgres"
)

// IntegrationDSN is the Trino of docker-compose.yml the integration tests run
// against (SQLMUX_TEST_TRINO), its catalog postgresql: the seeded PG, which
// takes writes. Unset, the test skips.
func IntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMUX_TEST_TRINO")
	if dsn == "" {
		t.Skip("SQLMUX_TEST_TRINO is not set")
	}
	return dsn
}

// OwnSchema is a schema of the test's own in the PG behind the postgresql
// catalog, made there with a table t_item that has a primary key, as a user's
// table has: the connector updates no table without one. Its NOT NULL columns
// are the row identity Trino's saves go by; it is dropped when the test ends
// (AGENTS.md「集成测试环境」). It returns the schema's name.
func OwnSchema(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	pg, _, err := postgres.Connect(ctx, postgres.IntegrationDSN(t), "", false)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := fmt.Sprintf("sqlmux_test_%d_%s", os.Getpid(), hex.EncodeToString(b))
	for _, s := range []string{
		"create schema " + name,
		"create table " + name + ".t_item (id bigint primary key, name text not null, price numeric(10, 2) not null, at timestamptz not null, note text, meta jsonb, ok boolean)",
		"insert into " + name + ".t_item values " +
			"(1, 'a', 1.50, '2026-01-01 00:00:00 UTC', null, '{\"k\":1}', true), " +
			"(2, 'b', 2.25, '2026-01-02 03:04:05.678 UTC', 'it''s', null, false), " +
			"(3, 'c', 3.00, '2026-01-03 00:00:00 UTC', null, null, null)",
	} {
		if _, err := pg.Exec(ctx, s, 0); err != nil {
			t.Fatal(s, err)
		}
	}
	t.Cleanup(func() {
		defer pg.Close()
		if _, err := pg.Exec(context.Background(), "drop schema if exists "+name+" cascade", 0); err != nil {
			t.Error(err)
		}
	})
	return name
}
