// Package exec is the engine "exec" (§8.8, F17.7): a server that only a command
// reaches, one the user's own tool runs the statements of. Each statement is
// one run of the command, with {sql} in its arguments replaced by the text; what
// it prints on stdout is a JSON array with an item for each statement; what it
// says on stderr when it fails is the error. It is read only: no transaction
// goes from one run to the next, and so no write, no transaction and no cursor.
package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kackerx/sqlmux/internal/db"
	"github.com/kackerx/sqlmux/internal/i18n"
	"github.com/kackerx/sqlmux/internal/sqlkit"
)

// Placeholder is what a command's argument holds where the statement goes.
const Placeholder = "{sql}"

// Error is what the command said was wrong: the err of an item of its output, or
// what it wrote on stderr when it exited with an error.
type Error struct{ Msg string }

func (e Error) Error() string { return e.Msg }

// Conn runs statements with a command. It has no state: the statements of two
// requests go in two runs, and cancelling a request kills its run.
type Conn struct {
	argv    []string
	dialect sqlkit.Dialect
}

// New is the connection a command makes, Placeholder in at least one of its
// arguments; d is the dialect its arguments are inlined in.
func New(argv []string, d sqlkit.Dialect) (*Conn, error) {
	if len(argv) == 0 || !hasPlaceholder(argv) {
		return nil, errors.New(i18n.T("command 里要有 {sql}"))
	}
	return &Conn{argv: argv, dialect: d}, nil
}

func hasPlaceholder(argv []string) bool {
	for _, a := range argv {
		if strings.Contains(a, Placeholder) {
			return true
		}
	}
	return false
}

// item is a statement's answer in the command's output.
type item struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	N       *int64   `json:"n"`
	Err     string   `json:"err"`
	// Truncated is that the command cut the rows: the result says so, as one cut at maxRows does.
	Truncated bool `json:"truncated"`
}

// tail keeps the last 4 KB written to it: what the command says on stderr is short
// but not bounded.
type tail struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if over := t.buf.Len() - 4096; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

// runCommand is one run of argv: what it printed on stdout. A run that ends in
// error is what it said on stderr, or how it ended; a cancel kills it and is the
// error whatever it said.
func runCommand(ctx context.Context, argv []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out bytes.Buffer
	var errOut tail
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.WaitDelay = 500 * time.Millisecond // a child of the command may hold its pipes open after it is gone
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if msg := strings.TrimSpace(errOut.buf.String()); msg != "" {
			return nil, Error{msg}
		}
		return nil, Error{fmt.Sprintf("%s: %v", argv[0], err)}
	}
	return out.Bytes(), nil
}

// run is one run of the command for statement sql, its output parsed.
func (c *Conn) run(ctx context.Context, sql string) ([]item, error) {
	args := make([]string, len(c.argv))
	for i, a := range c.argv {
		args[i] = strings.ReplaceAll(a, Placeholder, sql)
	}
	out, err := runCommand(ctx, args)
	if err != nil {
		return nil, err
	}
	return parse(out)
}

// parse reads the output of a run: an array of items, or one item alone.
func parse(out []byte) ([]item, error) {
	d := json.NewDecoder(bytes.NewReader(out))
	d.UseNumber() // a number is the text it was written as
	var items []item
	var err error
	if bytes.HasPrefix(bytes.TrimSpace(out), []byte("{")) {
		var one item
		err = d.Decode(&one)
		items = []item{one}
	} else {
		err = d.Decode(&items)
	}
	if err != nil {
		return nil, Error{fmt.Sprintf(i18n.T("命令的输出不是约定的 JSON：%s"), err)}
	}
	return items, nil
}

// val is a JSON value as the text protocol has it: a string as it is, a number as it
// was written, a boolean t or f (as PG's), null NULL, what is nested the JSON text of
// it.
func val(v any) db.Val {
	switch v := v.(type) {
	case nil:
		return db.Val{Null: true}
	case string:
		return db.Val{S: v}
	case json.Number:
		return db.Val{S: v.String()}
	case bool:
		return db.Val{S: map[bool]string{true: "t", false: "f"}[v]}
	}
	b, _ := json.Marshal(v)
	return db.Val{S: string(b)}
}

// result is item it as a db.Result, at most maxRows rows of it (0: all).
func result(it item, maxRows int, took time.Duration) db.Result {
	r := db.Result{Took: took, Truncated: it.Truncated}
	for _, c := range it.Columns {
		r.Cols = append(r.Cols, db.Col{Name: c})
	}
	for _, row := range it.Rows {
		if maxRows > 0 && len(r.Rows) == maxRows {
			r.Truncated = true
			break
		}
		vals := make([]db.Val, len(r.Cols)) // as many as the columns: a row the code reads by place is not read past its end
		for i := range vals {
			vals[i] = db.Val{Null: true}
			if i < len(row) {
				vals[i] = val(row[i])
			}
		}
		r.Rows = append(r.Rows, vals)
	}
	if it.N != nil {
		r.Affected = *it.N
	}
	if len(r.Cols) == 0 {
		r.Tag = strconv.FormatInt(r.Affected, 10)
	}
	return r
}

// Exec runs each statement of sql in a run of its own, and stops at the first that
// fails: the results before it come back with its error.
func (c *Conn) Exec(ctx context.Context, sql string, maxRows int) ([]db.Result, error) {
	var out []db.Result
	for _, s := range sqlkit.Statements(sql, c.dialect) {
		start := time.Now()
		items, err := c.run(ctx, sql[s.Start:s.End])
		if err != nil {
			return out, err
		}
		for _, it := range items {
			if it.Err != "" {
				return out, Error{it.Err}
			}
			out = append(out, result(it, maxRows, time.Since(start)))
		}
	}
	return out, nil
}

// Query runs one statement, its $n written in as literals: the command has no way
// to take arguments apart from the text.
func (c *Conn) Query(ctx context.Context, sql string, args ...db.Val) (db.Result, error) {
	text, err := db.Inline(sql, args, c.dialect)
	if err != nil {
		return db.Result{}, err
	}
	start := time.Now()
	items, err := c.run(ctx, text)
	if err != nil {
		return db.Result{}, err
	}
	if len(items) == 0 {
		return db.Result{Took: time.Since(start)}, nil
	}
	if items[0].Err != "" {
		return db.Result{}, Error{items[0].Err}
	}
	it := items[0]
	if ctx.Value(noPaging{}) == nil {
		if it, err = c.whole(ctx, text, it); err != nil {
			return db.Result{}, err
		}
	}
	return result(it, 0, time.Since(start)), nil
}

// noPaging is a context value: the statement of this Query is a user's, which stays cut.
type noPaging struct{}

// maxPaged is how many rows whole reads before it leaves a statement truncated.
var maxPaged = 100000

// whole is the item of statement sql, which the command cut at some rows, with the rest
// of the rows: the statement is run again as a page of itself, the size of the first cut
// from the rows read, for as long as a page is as long as the first: a short page or an
// empty one is the end, whatever it says of being cut, for the command cannot tell its
// wall from the end (staging's pgAdmin, 1000 rows). At maxPaged rows it stays truncated.
// Only the statements sqlmux sends (the catalog, a page, a count) come here: a user's
// query stays cut, and says so (§8.8, F17.11).
// ponytail: the rows come in the order the subselect gives them, which PG keeps for an
// ORDER BY inside it in practice and does not promise
func (c *Conn) whole(ctx context.Context, sql string, it item) (item, error) {
	size := len(it.Rows)
	for it.Truncated && size > 0 && len(it.Rows) < maxPaged {
		page := fmt.Sprintf("select * from (%s) as sqlmux_page limit %d offset %d", strings.TrimRight(sql, "; \t\r\n"), size, len(it.Rows))
		items, err := c.run(ctx, page)
		if err != nil {
			return item{}, err
		}
		if len(items) == 0 {
			it.Truncated = false
			break
		}
		if items[0].Err != "" {
			return item{}, Error{items[0].Err}
		}
		it.Rows = append(it.Rows, items[0].Rows...)
		it.Truncated = len(items[0].Rows) == size
	}
	return it, nil
}

func (c *Conn) Close() error { return nil }
