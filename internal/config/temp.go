package config

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/kackerx/sqlmux/internal/i18n"
)

// IsTarget is whether arg, what the command line gives, is a file or a URL
// to open, not the name of a connection: it has a /, or a . that no connection
// of connections.toml, which are known, is called so (F14.1).
func IsTarget(arg string, known []string) bool {
	return strings.Contains(arg, "/") || strings.Contains(arg, ".") && !slices.Contains(known, arg)
}

// TempConnection is the connection a file or a URL on the command line, or
// DATABASE_URL, stands for (F14.1): postgres://, mysql://, redis://,
// sqlserver://, clickhouse:// or trino://, else a SQLite file. It is nowhere written, not connections.toml, not the keychain: the
// password a URL holds stays in it. Its name is the file's without its
// extension, or <database>@<host>, as a connection's name has to be (no /);
// one that is taken by a connection of taken, by what the state, the history and
// the queries go by, keeps the extension, then gets a number; none that is
// valid is the engine's. Temp says it is not for the manager to list.
func TempConnection(target string, taken []string) (Connection, error) {
	scheme, _, isURL := strings.Cut(target, "://")
	c := Connection{Temp: true}
	if !isURL {
		path, _, _ := strings.Cut(target, "?")
		base := filepath.Base(path)
		c.Engine, c.DSN = "sqlite", target
		return named(c, strings.TrimSuffix(base, filepath.Ext(base)), base, taken), nil
	}
	engine, ok := map[string]string{"postgres": "postgres", "postgresql": "postgres", "mysql": "mysql", "redis": "redis", "rediss": "redis",
		"sqlserver": "mssql", "mssql": "mssql", "clickhouse": "clickhouse", "trino": "trino"}[scheme]
	if !ok {
		return Connection{}, fmt.Errorf(i18n.T("不认识的 URL %s://：只有 postgres://、mysql://、redis://、sqlserver://、clickhouse://、trino:// 和 SQLite 的文件"), scheme)
	}
	u, err := url.Parse(target)
	if ue := (*url.Error)(nil); errors.As(err, &ue) {
		return Connection{}, fmt.Errorf("%s: %w", scheme, ue.Err) // not the URL: it holds the password
	}
	c.Engine, c.DSN = engine, target
	database, host := strings.TrimPrefix(u.Path, "/"), u.Hostname()
	switch engine {
	case "trino": // catalog/schema: the catalog names it
		database, _, _ = strings.Cut(database, "/")
	case "mssql": // the driver's URL has the database as a parameter
		database = u.Query().Get("database")
	}
	if engine == "redis" { // the db it connects to, as the tree names it: the query's over the path's, as go-redis reads them (F17.3)
		n := cmp.Or(u.Query().Get("db"), database)
		if v, err := strconv.Atoi(n); err == nil {
			n = strconv.Itoa(v)
		}
		database = "db" + cmp.Or(n, "0")
	}
	name := strings.Join(slices.DeleteFunc([]string{database, host}, func(s string) bool { return s == "" }), "@")
	return named(c, name, name, taken), nil
}

// named is c with the first of name, full, and numbered ones that is a name
// of a connection and not taken; the engine's when none is.
func named(c Connection, name, full string, taken []string) Connection {
	valid := func(n string) bool { return n != "" && checkFileName(n) == nil }
	base := full
	if !valid(base) {
		base = c.Engine
	}
	for _, n := range []string{name, full, base} {
		if valid(n) && !slices.Contains(taken, n) {
			c.Name = n
			return c
		}
	}
	for i := 2; ; i++ {
		if n := base + "-" + strconv.Itoa(i); !slices.Contains(taken, n) {
			c.Name = n
			return c
		}
	}
}
