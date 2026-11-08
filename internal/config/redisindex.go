package config

import (
	"net/url"
	"os"
	"path/filepath"
)

// RedisIndexPath is the file that keeps the names of the keys of db number db
// of connection conn: $XDG_DATA_HOME/sqlmux/redis-index/<connection>/db<n>
// (§9.8, F21.4). A name with a / in it is one directory.
func RedisIndexPath(conn, db string) string {
	return filepath.Join(DataDir(), "redis-index", url.PathEscape(conn), "db"+db)
}

// ReadRedisIndex is that file's text; nil when it is not there or cannot be read.
func ReadRedisIndex(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

// WriteRedisIndex writes the file whole through the rename of writeFile, the
// directories made, readable by the user alone: key names may say a good deal
// about the business.
func WriteRedisIndex(path string, data []byte) error { return writeFile(path, data) }
