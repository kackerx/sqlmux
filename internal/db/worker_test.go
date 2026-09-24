package db

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockConn's Query waits for its ctx, counting how many run at once.
type blockConn struct{ running, most atomic.Int32 }

func (c *blockConn) Query(ctx context.Context, sql string, args ...Val) (Result, error) {
	n := c.running.Add(1)
	defer c.running.Add(-1)
	if n > c.most.Load() {
		c.most.Store(n)
	}
	if sql == "wait" {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}
	time.Sleep(time.Millisecond)
	return Result{Tag: sql}, nil
}

func (c *blockConn) Exec(context.Context, string, int) ([]Result, error) { return nil, nil }
func (c *blockConn) Close() error                                        { return nil }

func TestWorkerRunsOneRequestAtATime(t *testing.T) {
	c := &blockConn{}
	w := NewWorker(c)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { w.Query(context.Background(), "x") })
	}
	wg.Wait()
	if c.most.Load() != 1 {
		t.Fatalf("%d requests ran at once", c.most.Load())
	}
}

func TestWorkerCancelsOnlyTheRunningRequest(t *testing.T) {
	w := NewWorker(&blockConn{})
	w.Cancel() // idle: nothing to cancel, and the next request isn't hit
	if r, err := w.Query(context.Background(), "x"); err != nil || r.Tag != "x" {
		t.Fatalf("after an idle cancel: %v %v", r, err)
	}
	done := make(chan error)
	go func() { _, err := w.Query(context.Background(), "wait"); done <- err }()
	for {
		if w.mu.Lock(); w.cancel != nil {
			w.mu.Unlock()
			break
		}
		w.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	w.Cancel()
	if err := <-done; !errors.Is(err, ErrCanceled) {
		t.Fatalf("cancelled request: %v", err)
	}
}
