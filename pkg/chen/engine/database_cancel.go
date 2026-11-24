package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jumpserver/koko/pkg/chen/core"
)

func supportsActiveCancellation(dialect string) bool {
	switch dialect {
	case "mysql", "mariadb", "postgresql", "sqlserver", "oracle", "clickhouse", "oceanbase-mysql", "oceanbase-oracle", "dameng", "db2":
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
func driverConfirmedCancellation(dialect string, err error) bool {
	return dialect == "sqlserver" && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func executionContext(ctx context.Context, dialect, id string) context.Context {
	if dialect == "clickhouse" {
		return clickhouse.Context(ctx, clickhouse.WithQueryID(id))
	}
	return ctx
}

// Only these drivers have a server-side interrupt independent of the original
// query context. Other drivers need context cancellation to start the interrupt.
func supportsBackendInterrupt(dialect string) bool {
	return core.IsMySQLWire(dialect) || dialect == "postgresql" || dialect == "clickhouse"
}

func (e *execution) beginDatabaseWork() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelRequested.Load() {
		return context.Canceled
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	e.databaseDone = make(chan struct{})
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
	pending := e.cancelRequested.Load()
	e.mu.Unlock()
	if pending {
		<-e.cancelDone
	}
}

func interruptDatabaseWork(s *session, c *console, e *execution, databaseDone <-chan struct{}) {
	defer close(e.cancelDone)
	start := time.Now()
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	err := cancelBackend(ctx, s, c, e)
	stop()
	if err != nil {
		slog.Warn("database_interrupt_failed", "dialect", s.grant.Config.Dialect, "execution", e.view.ID, "elapsed", time.Since(start), "error", err)
	}
	// A successful control command does not mean the original connection has
	// finished reading its reply. Canceling its context early can close it.
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-databaseDone:
	case <-e.ctx.Done():
	case <-timer.C:
		slog.Warn("database_interrupt_grace_expired", "dialect", s.grant.Config.Dialect, "execution", e.view.ID, "elapsed", time.Since(start))
		e.cancel()
	}
}

func cancelBackend(ctx context.Context, s *session, c *console, e *execution) error {
	if s.db == nil {
		return nil
	}
	dialect := s.grant.Config.Dialect
	switch {
	case core.IsMySQLWire(dialect):
		_, err := s.db.ExecContext(ctx, "KILL QUERY "+strconv.FormatInt(c.backendID, 10))
		return err
	case dialect == "postgresql":
		var sent bool
		if err := s.db.QueryRowContext(ctx, "SELECT pg_cancel_backend($1)", c.backendID).Scan(&sent); err != nil {
			return err
		}
		if !sent {
			return errors.New("PostgreSQL did not signal the backend")
		}
	case dialect == "clickhouse":
		return cancelClickHouseExecution(ctx, s.db, e.view.ID)
	}
	return nil
}

func cancelClickHouseExecution(ctx context.Context, db *sql.DB, id string) error {
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
