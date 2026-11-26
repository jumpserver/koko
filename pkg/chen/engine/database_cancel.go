package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const (
	databaseCancelTimeout = 2 * time.Second
	databaseCancelGrace   = time.Second
)

func supportsActiveCancellation(d dialect.ID) bool {
	switch d {
	case dialect.MySQL, dialect.MariaDB, dialect.PostgreSQL, dialect.SQLServer, dialect.Oracle, dialect.ClickHouse, dialect.OceanBaseMySQL, dialect.OceanBaseOracle, dialect.Dameng, dialect.DB2:
		return true
	default:
		return false
	}
}

// go-mssqldb v1.11.2 tokenProcessor.next sends TDS attention and drains
// the server acknowledgement before returning ctx.Err. Failed acknowledgements
// return a distinct cancelDrainError; other drivers do not share this guarantee.
// Only pass the direct QueryContext/ExecContext error: database/sql can replace
// Rows.Err with the context error independently of a driver's acknowledgement.
func driverConfirmedCancellation(d dialect.ID, err error) bool {
	return d == dialect.SQLServer && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func executionContext(ctx context.Context, d dialect.ID, id string) context.Context {
	if d == dialect.ClickHouse {
		return clickhouse.Context(ctx, clickhouse.WithQueryID(id))
	}
	return ctx
}

// Only these drivers have a server-side interrupt independent of the original
// query context. Other drivers need context cancellation to start the interrupt.
func supportsBackendInterrupt(d dialect.ID) bool {
	return d.UsesMySQLWire() || d == dialect.PostgreSQL || d == dialect.ClickHouse
}

type cancellationResult int

const (
	cancelAccepted cancellationResult = iota
	cancelFinished
	cancelUnsupported
	cancelTransactionBoundary
)

// The first request owns both the reason and the single interrupt. Synchronize
// with databaseDone so a late KILL cannot reach cleanup or the next statement.
func (e *execution) requestCancellation(s *session, reason string) cancellationResult {
	e.mu.Lock()
	if terminal(e.view.Status) {
		e.mu.Unlock()
		return cancelFinished
	}
	if e.cancelRequested.Load() || e.ctx.Err() != nil || (reason == "timeout" && e.deadlineTimer == nil) {
		e.mu.Unlock()
		return cancelAccepted
	}
	active := e.view.Status != "queued" && e.view.Status != "waiting_review"
	if reason == "user" && active {
		if !supportsActiveCancellation(s.grant.Dialect()) {
			e.mu.Unlock()
			return cancelUnsupported
		}
		if e.transactionAction != "" || e.transactionBoundary {
			e.mu.Unlock()
			return cancelTransactionBoundary
		}
	}
	e.cancelRequested.Store(true)
	e.deadlineExpired.Store(reason == "timeout")
	e.view.CancelReason = reason
	e.view.Status = "cancel_requested"
	e.view.Revision++
	if e.interruptBackend != nil {
		go interruptDatabaseWork(s, e, e.databaseDone, e.interruptBackend)
	} else {
		e.cancel()
		close(e.cancelDone)
	}
	e.mu.Unlock()
	s.emit("execution.updated", e.snapshot())
	return cancelAccepted
}

func (e *execution) beginDatabaseWork(s *session, c *console) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelRequested.Load() {
		return context.Canceled
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	e.databaseDone = make(chan struct{})
	if s.db != nil && supportsBackendInterrupt(s.grant.Dialect()) {
		e.interruptBackend = sync.OnceFunc(func() { interruptExecutionBackend(s, c, e) })
	}
	e.stopDatabaseWatch = e.watchDatabaseCancellation(s, c, e.databaseDone, e.interruptBackend)
	return nil
}

// Called by the worker after driver calls and row draining, before cleanup SQL
// or another statement can reuse the connection targeted by the interrupt.
func (e *execution) finishDatabaseWork() {
	e.mu.Lock()
	if e.databaseDone != nil {
		close(e.databaseDone)
		e.databaseDone = nil
	}
	stop := e.stopDatabaseWatch
	e.stopDatabaseWatch = nil
	e.interruptBackend = nil
	pending := e.cancelRequested.Load()
	e.mu.Unlock()
	if stop != nil {
		stop()
	}
	if pending {
		<-e.cancelDone
	}
}

// Session shutdown cancels the driver context immediately. MySQL and
// ClickHouse can still need a separate server interrupt. In particular, a
// driver draining rows can stop watching its context before its last read.
func (e *execution) watchDatabaseCancellation(s *session, c *console, work <-chan struct{}, interruptBackend func()) func() {
	interrupt := sync.OnceFunc(func() {
		d := s.grant.Dialect()
		if interruptBackend != nil && !e.cancelRequested.Load() && (d.UsesMySQLWire() || d == dialect.ClickHouse) {
			interruptBackend()
		}
		if c.sockets == nil {
			return
		}
		timer := time.NewTimer(databaseCleanupTimeout)
		defer timer.Stop()
		select {
		case <-work:
		case <-timer.C:
			e.mu.Lock()
			if e.databaseDone == work {
				c.sockets.close()
			}
			e.mu.Unlock()
		}
	})
	stop := context.AfterFunc(e.ctx, interrupt)
	return func() {
		// If the worker saw cancellation first, still issue and join the
		// server interrupt before rollback, metadata or the next statement.
		if !stop() || e.ctx.Err() != nil {
			interrupt()
		}
	}
}

func interruptDatabaseWork(s *session, e *execution, databaseDone <-chan struct{}, interruptBackend func()) {
	defer close(e.cancelDone)
	start := time.Now()
	interruptBackend()
	// A successful control command does not mean the original connection has
	// finished reading its reply. Canceling its context early can close it.
	timer := time.NewTimer(databaseCancelGrace)
	defer timer.Stop()
	select {
	case <-databaseDone:
	case <-e.ctx.Done():
	case <-timer.C:
		slog.Warn("database_interrupt_grace_expired", "dialect", s.grant.Dialect(), "execution", e.view.ID, "elapsed", time.Since(start))
		e.cancel()
	}
}

func cancelBackend(ctx context.Context, s *session, c *console, e *execution) error {
	if s.db == nil {
		return nil
	}
	// The control connection can itself stall. Close only its own sockets
	// on expiry and join that watcher before returning it to the pool.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var stop func()
	if err := conn.Raw(func(raw any) error {
		if c, ok := raw.(interface{ managedConn() *databaseConn }); ok {
			stop = c.managedConn().sockets.watch(ctx)
		}
		return nil
	}); err != nil {
		return err
	}
	if stop != nil {
		defer stop()
	}
	d := s.grant.Dialect()
	switch {
	case d.UsesMySQLWire():
		_, err := conn.ExecContext(ctx, "KILL QUERY "+strconv.FormatInt(c.backendID, 10))
		return err
	case d == dialect.PostgreSQL:
		var sent bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_cancel_backend($1)", c.backendID).Scan(&sent); err != nil {
			return err
		}
		if !sent {
			return errors.New("PostgreSQL did not signal the backend")
		}
	case d == dialect.ClickHouse:
		return cancelClickHouseExecution(ctx, conn, e.view.ID)
	}
	return nil
}

func cancelClickHouseExecution(ctx context.Context, db *sql.Conn, id string) error {
	rows, err := db.QueryContext(ctx, "KILL QUERY WHERE query_id = ? AND user = currentUser() SYNC", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	values := make([]any, len(columns))
	targets := make([]any, len(columns))
	for i := range values {
		targets[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(targets...); err != nil {
			return err
		}
		for i, name := range columns {
			if name == "kill_status" && fmt.Sprint(values[i]) != "finished" {
				return fmt.Errorf("ClickHouse cancellation unconfirmed: %v", values[i])
			}
		}
	}
	return rows.Err()
}
