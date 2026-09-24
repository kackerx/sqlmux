package db

import (
	"context"
	"errors"
	"sync"
)

// ErrCanceled is what a request fails with once its ctx is cancelled, by
// Worker.Cancel or the caller: the server's own error for it says less.
var ErrCanceled = errors.New("canceled")

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
	if err := f(ctx); err != nil {
		if ctx.Err() != nil {
			return ErrCanceled
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
