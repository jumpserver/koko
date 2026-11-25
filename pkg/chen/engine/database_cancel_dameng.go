package engine

import (
	"context"
	"io"
	"sync"
)

// interruptDamengTransport closes a pre-operation socket snapshot on context
// cancellation. The vendor driver is never touched by this goroutine. Completion
// joins the watcher before a handle can be cleaned up or the next SQL can start.
// No server acknowledgement is implied: a dispatched outcome remains unknown.
func interruptDamengTransport(ctx context.Context, transport io.Closer, run func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if transport == nil || ctx.Done() == nil {
		return run()
	}
	done, stopped := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	active := true
	go func() {
		defer close(stopped)
		select {
		case <-done:
			return
		case <-ctx.Done():
			mu.Lock()
			defer mu.Unlock()
			if active {
				_ = transport.Close()
			}
		}
	}()
	defer func() { mu.Lock(); active = false; mu.Unlock(); close(done); <-stopped }()
	return run()
}
func (c *damengConn) withTransportCancel(ctx context.Context, run func() error) error {
	var transport io.Closer
	if snapshot, ok := c.damengContextConn.(interface{ ChenTransportCloser() io.Closer }); ok {
		transport = snapshot.ChenTransportCloser()
	}
	return interruptDamengTransport(ctx, transport, run)
}
