//go:build integration

package clickhouse

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
)

// IntegrationDSN is the seeded server of docker-compose.yml the ClickHouse
// integration tests run against (SQLMUX_TEST_CLICKHOUSE); unset, the test
// skips.
func IntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("SQLMUX_TEST_CLICKHOUSE")
	if dsn == "" {
		t.Skip("SQLMUX_TEST_CLICKHOUSE is not set")
	}
	return dsn
}

// OwnDB is a database of the test's own on the test server, with a copy of
// sqlmux.t_user and sqlmux.t_order in it, dropped when the test ends: for the
// tests that write (AGENTS.md「集成测试环境」). It returns its DSN.
func OwnDB(t *testing.T) string {
	t.Helper()
	base := IntegrationDSN(t)
	ctx := context.Background()
	c, _, err := Connect(ctx, base, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	name := fmt.Sprintf("sqlmux_test_%d_%s", os.Getpid(), newID()[:8])
	for _, s := range []string{
		"create database " + name,
		"create table " + name + ".t_user as sqlmux.t_user",
		"insert into " + name + ".t_user select * from sqlmux.t_user",
		"create table " + name + ".t_order as sqlmux.t_order",
		"insert into " + name + ".t_order select * from sqlmux.t_order where id <= 20",
	} {
		if _, err := c.Exec(ctx, s, 0); err != nil {
			t.Fatal(s, err)
		}
	}
	t.Cleanup(func() {
		if _, err := c.Exec(context.Background(), "drop database if exists "+name, 0); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
