//go:build integration

package oracle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

// IntegrationDSN is the seeded Oracle of docker-compose.yml the integration
// tests run against (SQLMUX_TEST_ORACLE); unset, the test skips.
func IntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMUX_TEST_ORACLE")
	if dsn == "" {
		t.Skip("SQLMUX_TEST_ORACLE is not set")
	}
	return dsn
}

// OwnTable is a table of the test's own in the sqlmux schema, made by ddl with
// %s for its name and seeded by inserts, dropped when the test ends: for the
// tests that write (AGENTS.md「集成测试环境」). The user may make no other
// schema, so it is a table, not a schema. It returns the table's name.
func OwnTable(t *testing.T, ddl string, inserts ...string) string {
	t.Helper()
	ctx := context.Background()
	c, _, err := Connect(ctx, IntegrationDSN(t), "", false)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	name := strings.ToUpper(fmt.Sprintf("ZZ_%d_%s", os.Getpid(), hex.EncodeToString(b)))
	for _, s := range append([]string{fmt.Sprintf(ddl, name)}, inserts...) {
		if _, err := c.Exec(ctx, strings.ReplaceAll(s, "%s", name), 0); err != nil {
			t.Fatal(s, err)
		}
	}
	t.Cleanup(func() {
		defer c.Close()
		if _, err := c.Exec(context.Background(), "drop table "+name+" purge", 0); err != nil {
			t.Error(err)
		}
	})
	return name
}
