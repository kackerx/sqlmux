package db

import (
	"context"
	"sync"
)

// Worker owns one connection and runs one request on it at a time (§8.2).
// Requests come from tea.Cmds, each on its own goroutine already, so a lock
// is all it takes; which result is current is the caller's seq to decide.
type Worker struct {
	conn Conn

	run    sync.Mutex // held for the whole request
	mu     sync.Mutex // guards cancel
	cancel context.CancelFunc
}

func NewWorker(c Conn) *Worker { return &Worker{conn: c} }

func (w *Worker) Exec(ctx context.Context, sql string, maxRows int) (rs []Result, err error) {
	err = w.do(ctx, func(ctx context.Context) (err error) { rs, err = w.conn.Exec(ctx, sql, maxRows); return err })
	return rs, err
}

func (w *Worker) Query(ctx context.Context, sql string, args ...Val) (r Result, err error) {
	err = w.do(ctx, func(ctx context.Context) (err error) { r, err = w.conn.Query(ctx, sql, args...); return err })
	return r, err
}

// Run runs f on the connection as one request: nothing else runs on it in
// between, so f can keep a transaction open (§12 快速 SQL).
func (w *Worker) Run(ctx context.Context, f func(context.Context, Conn) error) error {
	return w.do(ctx, func(ctx context.Context) error { return f(ctx, w.conn) })
}

// ExecEach runs stmts one after the other as one request, each an Exec of
// its own and so, autocommit, a transaction of its own; it stops at the
// first that fails (§11「执行」). rs has one result for each statement
// that ran, the one that failed not among them.
func (w *Worker) ExecEach(ctx context.Context, stmts []string, maxRows int) (rs []Result, err error) {
	err = w.Run(ctx, func(ctx context.Context, c Conn) error {
		for _, s := range stmts {
			r, err := c.Exec(ctx, s, maxRows)
			if err != nil {
				return err
			}
			one := Result{} // one statement, one result
			if len(r) > 0 {
				one = r[0]
			}
			rs = append(rs, one)
		}
		return nil
	})
	return rs, err
}

// Cancel cancels the request running now, if any; queued ones still run.
func (w *Worker) Cancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *Worker) Close() error {
	w.Cancel()
	w.run.Lock()
	defer w.run.Unlock()
	return w.conn.Close()
}

func (w *Worker) do(ctx context.Context, f func(context.Context) error) error {
	w.run.Lock()
	defer w.run.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.setCancel(cancel)
	defer w.setCancel(nil)
	// A request stopped by Cancel or the caller's ctx fails with the ctx's
	// error, which says more than the server's reply to the cancel.
	if err := f(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (w *Worker) setCancel(c context.CancelFunc) {
	w.mu.Lock()
	w.cancel = c
	w.mu.Unlock()
}
