// Package db is how the app talks to a database (tech-design §8): every
// value is text, and each connection runs one request at a time.
package db

import (
	"context"
	"time"
)

// Val is one value as the database's text, or NULL (§8.1).
type Val struct {
	S    string
	Null bool
}

type Col struct {
	Name string
	Type string // the database's type name; "" when the engine can't tell (e.g. a PG enum)
}

type Result struct {
	Cols      []Col
	Rows      [][]Val
	Tag       string // e.g. "UPDATE 3"
	Truncated bool   // more rows came back than Exec's maxRows
	Took      time.Duration
}

// Conn is one engine's connection (§8.1). It sits behind a Worker, which
// runs one request on it at a time; cancelling a request's ctx cancels it on
// the server.
type Conn interface {
	// Exec runs sql, which may hold several statements, over the simple
	// protocol; each result keeps at most maxRows rows (0: all).
	Exec(ctx context.Context, sql string, maxRows int) ([]Result, error)
	// Query runs one statement over the extended protocol, args as text.
	Query(ctx context.Context, sql string, args ...Val) (Result, error)
	Close() error
}
