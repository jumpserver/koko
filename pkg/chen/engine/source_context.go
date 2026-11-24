package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"
)

// sourceReadContext temporarily selects the namespace captured by a result.
// It never changes the published console context, identity or session settings.
// The caller holds the console lock until restore has been verified.
type sourceReadContext struct {
	console                *console
	dialect                string
	before, source         []string
	beforeJSON, sourceJSON string
	attempted, restored    bool
	restoreErr             error
}

func prepareSourceReadContext(ctx context.Context, c *console, d, source string) (*sourceReadContext, error) {
	current, err := queryPageContext(ctx, c, d)
	if err != nil {
		return nil, err
	}
	if current == source {
		return nil, nil
	}
	g := &sourceReadContext{console: c, dialect: d, beforeJSON: current, sourceJSON: source}
	if json.Unmarshal([]byte(current), &g.before) != nil || json.Unmarshal([]byte(source), &g.source) != nil || len(g.before) != len(g.source) {
		return nil, sourceContextChanged()
	}
	var scope []int
	minimum := 0
	switch d {
	case "mysql", "mariadb", "oceanbase-mysql":
		scope, minimum = []int{0}, 6
	case "postgresql":
		scope, minimum = []int{1, 2}, 7
	case "sqlserver":
		scope, minimum = []int{0, 1}, 7
	case "oracle", "dameng", "oceanbase-oracle":
		scope, minimum = []int{1}, 6
	case "db2":
		scope, minimum = []int{1}, 6
	case "clickhouse":
		scope, minimum = []int{0}, 3
	default:
		return nil, sourceContextChanged()
	}
	if len(g.before) < minimum {
		return nil, sourceContextChanged()
	}
	if d == "sqlserver" && g.before[0] != g.source[0] {
		// USER_NAME, unlike SUSER_SNAME, is the database's mapping of the
		// login. Verify that mapping after USE, not in the other database.
		scope = append(scope, 3)
	}
	for i := range g.before {
		if !slices.Contains(scope, i) && g.before[i] != g.source[i] {
			return nil, sourceContextChanged()
		}
	}
	// USE cannot return a MySQL connection to having no default database.
	// Never reset or reconnect here: that would discard a transaction.
	if d != "postgresql" {
		index := scope[0]
		if g.before[index] == "" || g.source[index] == "" {
			return nil, &APIError{"query_context_not_restorable", "The result's original database context cannot be restored on this connection; run the query in a console with a selected database"}
		}
	}
	if d == "dameng" || d == "oceanbase-oracle" || d == "oceanbase-mysql" {
		state, err := editTransactionState(ctx, c, d)
		if err != nil || c.manual || state != editTxIdle {
			return nil, &APIError{"query_context_transaction_unsupported", "Restoring this result's original schema has unverified transaction behavior on this database while a transaction may be active; use a console already in that schema"}
		}
	}
	return g, nil
}

func sourceContextChanged() *APIError {
	return &APIError{"query_page_context_changed", "The result's session settings or identity changed; run the original SQL again before refreshing or paging"}
}

func sourceContextError(err error) *APIError {
	var api *APIError
	if errors.As(err, &api) {
		return api
	}
	return safeDatabaseError(err)
}

func (g *sourceReadContext) selectScope(ctx context.Context, values []string) error {
	c, d := g.console, g.dialect
	if d == "dameng" || d == "oceanbase-oracle" || d == "oceanbase-mysql" {
		// A SELECT or an explicitly confirmed function can start a transaction
		// after admission. Check again before the restoration setter as well.
		state, err := editTransactionState(ctx, c, d)
		if err != nil || c.manual || state != editTxIdle {
			return errors.New("The database context cannot change while transaction safety is unverified")
		}
	}
	var sql string
	switch d {
	case "mysql", "mariadb", "oceanbase-mysql", "sqlserver", "clickhouse":
		sql = "USE " + quoteDatabaseIdentifier(values[0], d)
	case "postgresql":
		// The complete search_path can contain quoted commas, $user and more
		// than one schema. A bound configuration value preserves it exactly.
		state, err := editTransactionState(ctx, c, d)
		if err != nil || state != editTxIdle && state != editTxActive {
			return errors.New("PostgreSQL transaction state is unavailable for context restoration")
		}
		var actual string
		return c.conn.QueryRowContext(ctx, "SELECT pg_catalog.set_config('search_path', $1, $2)", values[2], state == editTxActive).Scan(&actual)
	case "oracle", "oceanbase-oracle":
		sql = "ALTER SESSION SET CURRENT_SCHEMA = " + quoteDatabaseIdentifier(values[1], d)
	case "dameng":
		sql = "SET SCHEMA " + quoteDatabaseIdentifier(values[1], d)
	case "db2":
		sql = "SET CURRENT SCHEMA " + quoteDatabaseIdentifier(values[1], d)
	default:
		return sourceContextChanged()
	}
	if d == "oracle" {
		// The server's ALTER SESSION does not commit. Suppress the driver's
		// separate commit-on-execute flag as well, including raw BEGIN flows.
		boundary := &editBoundary{conn: c.conn, dialect: d}
		if err := boundary.preserveOracleTransaction(); err != nil {
			return err
		}
		defer boundary.restoreOracleTransaction()
	}
	_, err := c.conn.ExecContext(ctx, sql)
	return err
}

func (g *sourceReadContext) enter(ctx context.Context) error {
	// A failed driver call may still have changed the server's context.
	g.attempted = true
	if err := g.selectScope(ctx, g.source); err != nil {
		return err
	}
	actual, err := queryPageContext(ctx, g.console, g.dialect)
	if err == nil && actual != g.sourceJSON {
		err = sourceContextChanged()
	}
	return err
}

func (g *sourceReadContext) restore(s *session) error {
	if g.restored || !g.attempted {
		return g.restoreErr
	}
	g.restored = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if g.console.closed.Load() || g.console.lost.Load() || s.ctx.Err() != nil {
		err = errors.New("source context connection closed")
	} else {
		err = g.selectScope(ctx, g.before)
		if err == nil {
			var actual string
			actual, err = queryPageContext(ctx, g.console, g.dialect)
			if err == nil && actual != g.beforeJSON {
				err = errors.New("source context restoration did not match")
			}
		}
	}
	if err != nil {
		g.restoreErr = &APIError{"query_context_restore_failed", "The current console context could not be restored after selecting the result's source; the connection is invalid and the result was not published"}
		invalidateEditConnection(s, g.console)
	}
	return g.restoreErr
}

func (e *execution) restoreSourceReadContext() error {
	if e.restoreReadContext != nil {
		return e.restoreReadContext()
	}
	return nil
}

// A PostgreSQL context preflight may need an otherwise empty transaction for
// SET LOCAL. Rejection before user SQL must not stale all previously read rows.
func (v *Server) rollbackSourcePreflight(s *session, c *console, e *execution) error {
	tx := c.manualTx
	c.manualTx = nil
	if err := c.conn.Raw(func(any) error { return tx.Rollback() }); err != nil {
		invalidateEditConnection(s, c)
		v.publishTransaction(s, c, e, "unknown")
		return &APIError{"transaction_outcome_unknown", "The context preflight transaction could not be cleaned up; reconnect before retrying"}
	}
	v.publishTransaction(s, c, e, "")
	return nil
}
