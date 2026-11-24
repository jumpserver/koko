package db2

import (
	"context"
	"database/sql/driver"
)

// Tx represents an in-progress transaction on an IBM Db2 database.
type Tx struct {
	conn      *Conn
	ctx       context.Context
	customIso bool
	closed    bool
}

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	if tx.conn == nil || tx.conn.session == nil {
		return ErrConnectionClosed
	}

	tx.conn.mu.Lock()
	defer tx.conn.mu.Unlock()

	if tx.closed {
		return ErrConnectionClosed
	}
	tx.closed = true

	commitErr := tx.conn.session.Commit(tx.ctx)
	cleanupErr := tx.cleanup(tx.ctx)
	if commitErr != nil {
		return commitErr
	}
	return cleanupErr
}

// Rollback aborts the transaction.
func (tx *Tx) Rollback() error {
	if tx.conn == nil || tx.conn.session == nil {
		return ErrConnectionClosed
	}

	tx.conn.mu.Lock()
	defer tx.conn.mu.Unlock()

	if tx.closed {
		return ErrConnectionClosed
	}
	tx.closed = true

	rollbackErr := tx.conn.session.Rollback(tx.ctx)
	cleanupErr := tx.cleanup(tx.ctx)
	if rollbackErr != nil {
		return rollbackErr
	}
	return cleanupErr
}

func (tx *Tx) cleanup(ctx context.Context) error {
	var firstErr error
	if tx.customIso && tx.conn != nil && tx.conn.session != nil && !tx.conn.session.Broken() {
		// Restaura o isolamento padrão (CS)
		if _, err := tx.conn.session.ExecDirect(ctx, "SET CURRENT ISOLATION = CS"); err != nil {
			firstErr = err
		} else {
			tx.conn.hasCustomIso = false
		}
	}
	if tx.conn != nil && tx.conn.session != nil {
		tx.conn.session.SetAutoCommit(true)
	}
	return firstErr
}

var _ driver.Tx = (*Tx)(nil)
