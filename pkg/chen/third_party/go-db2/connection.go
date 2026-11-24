package db2

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-db2/go-db2/internal/drda"
)

// Conn implements the database/sql/driver.Conn interface for IBM Db2.
type Conn struct {
	session      *drda.Session
	cfg          *Config
	sections     *sectionManager
	hasCustomIso bool
	mu           sync.Mutex
	closed       bool
}

// newConn wraps a connected network session into a database/sql driver.Conn.
func newConn(session *drda.Session, cfg *Config) *Conn {
	return &Conn{
		session:  session,
		cfg:      cfg,
		sections: newSectionManager(maxDrdaSections),
	}
}

func (c *Conn) getSections() *sectionManager {
	if c.sections == nil {
		c.sections = newSectionManager(maxDrdaSections)
	}
	return c.sections
}

// Prepare returns a prepared statement, bound to this connection.
func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// applyContextMetadata applies session-level user switching (WithUser) and client info
// audit registers (WithClientInfo) propagated in ctx to ensure proper multi-tenant
// identity isolation and auditability across all execution paths.
func (c *Conn) applyContextMetadata(ctx context.Context) error {
	if targetUser := UserFromContext(ctx); targetUser != "" {
		if err := validateSQLIdentifier(targetUser, "context user"); err != nil {
			return err
		}
		if targetUser != c.session.CurrentUser() {
			if err := c.session.SwitchUser(ctx, targetUser); err != nil {
				return err
			}
		}
	}

	if clientInfo := ClientInfoFromContext(ctx); !clientInfo.IsEmpty() {
		if err := c.session.SetClientInfo(ctx, clientInfo.ApplicationName, clientInfo.WorkstationName, clientInfo.UserID, clientInfo.Accounting, clientInfo.CorrelationToken); err != nil {
			return err
		}
	}
	return nil
}

// PrepareContext returns a prepared statement, bound to this connection with context support.
func (c *Conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return nil, ErrConnectionClosed
	}

	if err := c.applyContextMetadata(ctx); err != nil {
		return nil, err
	}

	sec, err := c.getSections().Acquire()
	if err != nil {
		return nil, err
	}

	outCols, paramCols, err := c.session.PrepareAndDescribe(ctx, query, sec)
	if err != nil {
		c.sections.Release(sec) // Invariante: liberação imediata em caso de falha
		return nil, err
	}

	return newStmt(c, query, outCols, paramCols, sec), nil
}

// Exec executes a query that doesn't return rows (legacy interface).
func (c *Conn) Exec(query string, args []driver.Value) (driver.Result, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, v := range args {
		namedArgs[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return c.ExecContext(context.Background(), query, namedArgs)
}

// ExecContext executes a query without returning rows.
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return nil, ErrConnectionClosed
	}

	if err := c.applyContextMetadata(ctx); err != nil {
		return nil, err
	}

	if len(args) == 0 {
		affected, err := c.session.ExecDirect(ctx, query)
		if err != nil {
			return nil, err
		}
		return newResult(affected, 0), nil
	}

	// Prepare and execute with parameters
	sec, err := c.getSections().Acquire()
	if err != nil {
		return nil, err
	}

	outCols, paramCols, err := c.session.PrepareAndDescribe(ctx, query, sec)
	if err != nil {
		c.sections.Release(sec)
		return nil, err
	}

	stmt := newStmt(c, query, outCols, paramCols, sec)
	defer stmt.Close()
	return stmt.execContextLocked(ctx, args)
}

// Query executes a query that may return rows (legacy interface).
func (c *Conn) Query(query string, args []driver.Value) (driver.Rows, error) {
	namedArgs := make([]driver.NamedValue, len(args))
	for i, v := range args {
		namedArgs[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return c.QueryContext(context.Background(), query, namedArgs)
}

// QueryContext executes a query that may return rows.
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return nil, ErrConnectionClosed
	}

	if err := c.applyContextMetadata(ctx); err != nil {
		return nil, err
	}

	if len(args) == 0 {
		sQuery := stripLeadingCommentsAndSpaces(query)
		if hasPrefixFold(sQuery, "CALL") {
			sets, err := c.session.QueryCallDirect(ctx, query)
			if err != nil {
				return nil, err
			}
			if len(sets) == 0 {
				return newRows(nil, nil), nil
			}
			resultSets := make([]resultSet, len(sets))
			for i, s := range sets {
				resultSets[i] = resultSet{columns: s.Columns, data: s.Data}
			}
			return newMultiRows(resultSets), nil
		}

		if !isQueryStatement(query) {
			_, err := c.session.ExecDirect(ctx, query)
			if err != nil {
				return nil, err
			}
			return newRows(nil, nil), nil
		}

		cols, rowsData, stream, err := c.session.QueryStreamDirect(ctx, query)
		if err != nil {
			return nil, err
		}

		return newStreamingRows(cols, rowsData, stream, nil), nil
	}

	// Prepare and query with parameters
	sec, err := c.getSections().Acquire()
	if err != nil {
		return nil, err
	}

	outCols, paramCols, err := c.session.PrepareAndDescribe(ctx, query, sec)
	if err != nil {
		c.sections.Release(sec)
		return nil, err
	}

	stmt := newStmt(c, query, outCols, paramCols, sec)
	stmt.isTemp = true
	return stmt.queryContextLocked(ctx, args)
}

func stripLeadingCommentsAndSpaces(sql string) string {
	s := strings.TrimSpace(sql)
	for {
		if strings.HasPrefix(s, "--") {
			idx := strings.IndexByte(s, '\n')
			if idx == -1 {
				return ""
			}
			s = strings.TrimSpace(s[idx+1:])
			continue
		}
		if strings.HasPrefix(s, "/*") {
			idx := strings.Index(s, "*/")
			if idx == -1 {
				return ""
			}
			s = strings.TrimSpace(s[idx+2:])
			continue
		}
		if strings.HasPrefix(s, "(") {
			s = strings.TrimSpace(s[1:])
			continue
		}
		break
	}
	return s
}

// hasPrefixFold reports whether s begins with prefix (expected to be uppercase ASCII),
// ignoring ASCII case differences.
// Optimization: Checks bytes inline without converting s to upper case via strings.ToUpper,
// eliminating heap allocations and full-string scanning overhead (0 allocs/op, O(1) prefix time).
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

func isQueryStatement(sql string) bool {
	s := stripLeadingCommentsAndSpaces(sql)
	return hasPrefixFold(s, "SELECT") ||
		hasPrefixFold(s, "WITH") ||
		hasPrefixFold(s, "VALUES") ||
		hasPrefixFold(s, "TABLE")
}

// CurrentUser returns the currently active user on this connection.
func (c *Conn) CurrentUser() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.session == nil {
		return ""
	}
	return c.session.CurrentUser()
}

// SwitchUser transitions the active user identity on this connection within a trusted context or session.
func (c *Conn) SwitchUser(ctx context.Context, newUser string, password ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return ErrConnectionClosed
	}

	return c.session.SwitchUser(ctx, newUser, password...)
}

// SetClientInfo updates client info registers on this connection.
func (c *Conn) SetClientInfo(ctx context.Context, info ClientInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return ErrConnectionClosed
	}

	return c.session.SetClientInfo(ctx, info.ApplicationName, info.WorkstationName, info.UserID, info.Accounting, info.CorrelationToken)
}

// Close invalidates and closes the database connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

// Begin starts and returns a new transaction (legacy interface).
func (c *Conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx starts and returns a new transaction with the provided context and options.
func (c *Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return nil, ErrConnectionClosed
	}

	if opts.ReadOnly {
		return nil, errors.New("db2: read-only transactions are not supported")
	}

	var isoClause string
	switch opts.Isolation {
	case driver.IsolationLevel(sql.LevelDefault), driver.IsolationLevel(sql.LevelReadCommitted):
		isoClause = "CS"
	case driver.IsolationLevel(sql.LevelReadUncommitted):
		isoClause = "UR"
	case driver.IsolationLevel(sql.LevelRepeatableRead):
		isoClause = "RS"
	case driver.IsolationLevel(sql.LevelSerializable):
		isoClause = "RR"
	default:
		return nil, fmt.Errorf("db2: unsupported isolation level: %v", opts.Isolation)
	}

	// Executa comando para configurar isolamento se diferente do padrão CS
	if isoClause != "CS" {
		if _, err := c.session.ExecDirect(ctx, "SET CURRENT ISOLATION = "+isoClause); err != nil {
			return nil, fmt.Errorf("db2: failed to set transaction isolation %s: %w", isoClause, err)
		}
		c.hasCustomIso = true
	}

	c.session.SetAutoCommit(false)

	return &Tx{
		conn:      c,
		ctx:       ctx,
		customIso: isoClause != "CS",
	}, nil
}

// Ping implements driver.Pinger interface to verify connection liveness.
func (c *Conn) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil {
		return ErrConnectionClosed
	}

	return c.session.Ping(ctx)
}

// IsValid implements driver.Validator. database/sql calls it before returning
// the connection to the pool, and discards the connection when it is false.
func (c *Conn) IsValid() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.session != nil && !c.session.Broken()
}

// ResetSession implements driver.SessionResetter. It is called before a pooled
// connection is reused; driver.ErrBadConn makes database/sql discard it.
func (c *Conn) ResetSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.session == nil || c.session.Broken() {
		return driver.ErrBadConn
	}

	// Garante que o isolamento foi restaurado ao padrão (CS) caso tenha permanecido alterado
	if c.hasCustomIso {
		if _, err := c.session.ExecDirect(ctx, "SET CURRENT ISOLATION = CS"); err != nil {
			return driver.ErrBadConn
		}
		c.hasCustomIso = false
	}

	if c.cfg != nil && c.cfg.User != "" && c.session.CurrentUser() != c.cfg.User {
		if err := c.session.SwitchUser(ctx, c.cfg.User); err != nil {
			return driver.ErrBadConn
		}
	}

	if c.cfg != nil && !c.session.ClientInfoMatches(c.cfg.ClientApplName, c.cfg.ClientWrkstnName, c.cfg.ClientUserid, c.cfg.ClientAcctng, c.cfg.ClientCorrToken) {
		if err := c.session.ResetClientInfo(ctx, c.cfg.ClientApplName, c.cfg.ClientWrkstnName, c.cfg.ClientUserid, c.cfg.ClientAcctng, c.cfg.ClientCorrToken); err != nil {
			return driver.ErrBadConn
		}
	}

	c.session.SetAutoCommit(true)
	return nil
}

// Interface assertions
// CheckNamedValue implements driver.NamedValueChecker interface.
func (c *Conn) CheckNamedValue(nv *driver.NamedValue) error {
	return checkNamedValueCommon(nv)
}

func checkNamedValueCommon(nv *driver.NamedValue) error {
	if nv == nil {
		return nil
	}

	// 1. Desreferenciação de driver.Valuer
	if vr, ok := nv.Value.(driver.Valuer); ok {
		val, err := vr.Value()
		if err != nil {
			return err
		}
		nv.Value = val
		if nv.Value == nil {
			return nil
		}
	}

	// 2. Normalização de arrays de bytes fixos (ex: [16]byte / uuid.UUID) para []byte escalar
	switch v := nv.Value.(type) {
	case [16]byte:
		b := make([]byte, 16)
		copy(b, v[:])
		nv.Value = b
		return nil
	case *[16]byte:
		if v == nil {
			nv.Value = nil
			return nil
		}
		b := make([]byte, 16)
		copy(b, v[:])
		nv.Value = b
		return nil
	case sql.Out, *sql.Out:
		return nil
	case io.Reader:
		return nil
	}

	// 3. Preservação de fatias homogêneas para modo Batch
	// Retornar nil para que o database/sql não invoque DefaultParameterConverter
	// (que rejeitaria slices como []int, []string com "unsupported type, a slice").
	switch nv.Value.(type) {
	case []int, []int64, []int32, []int16, []int8,
		[]uint, []uint64, []uint32, []uint16, []uint8,
		[]string, []float64, []float32, []bool, []time.Time,
		[]any, []driver.Value:
		return nil
	}

	// 4. Delegar conversão de tipos escalares básicos e ponteiros para o database/sql
	return driver.ErrSkip
}

var (
	_ driver.Conn               = (*Conn)(nil)
	_ driver.ConnPrepareContext = (*Conn)(nil)
	_ driver.ConnBeginTx        = (*Conn)(nil)
	_ driver.Pinger             = (*Conn)(nil)
	_ driver.ExecerContext      = (*Conn)(nil)
	_ driver.QueryerContext     = (*Conn)(nil)
	_ driver.NamedValueChecker  = (*Conn)(nil)
	_ driver.Validator          = (*Conn)(nil)
	_ driver.SessionResetter    = (*Conn)(nil)
)
