package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// The SQL parser and result paging must see the same physical session settings.
type mysqlSessionState struct {
	scope       sqlservice.Context
	options     sqlservice.AnalyzeOptions
	fingerprint string
	backendID   int64
}

func readMySQLSession(ctx context.Context, conn *sql.Conn) (mysqlSessionState, error) {
	return scanMySQLSession(conn.QueryRowContext(ctx, "SELECT "+sqlservice.MySQLSessionFields))
}

func scanMySQLSession(row interface{ Scan(...any) error }, suffix ...any) (mysqlSessionState, error) {
	var values [6]string
	var version string
	var backend int64
	dest := []any{&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &version, &backend}
	err := row.Scan(append(dest, suffix...)...)
	if err != nil {
		return mysqlSessionState{}, err
	}
	fingerprint, _ := json.Marshal(values)
	return mysqlSessionState{
		scope:       sqlservice.Context{Database: values[0], Schema: values[0]},
		options:     mysqlAnalyzeOptions(values[2], version),
		fingerprint: string(fingerprint),
		backendID:   backend,
	}, nil
}

func readMySQLStarSession(ctx context.Context, conn *sql.Conn, hint sqlservice.QueryPageRelation) (mysqlSessionState, *resultStarSource, error) {
	rows, err := sqlservice.MySQLStarSessionRows(ctx, conn, hint)
	if err != nil {
		return mysqlSessionState{}, nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return mysqlSessionState{}, nil, err
		}
		return mysqlSessionState{}, nil, sql.ErrNoRows
	}
	var discard any
	var ordinal int
	state, err := scanMySQLSession(rows, &discard, &discard, &ordinal)
	if err != nil || ordinal != 0 {
		if err == nil {
			err = errors.New("missing MySQL session row")
		}
		return mysqlSessionState{}, nil, err
	}
	schema := hint.Schema
	if schema == "" {
		schema = state.scope.Database
	}
	star := &resultStarSource{source: sqlservice.EditableSource{Schema: schema, Table: hint.Name}}
	for rows.Next() {
		var name string
		var safe bool
		if err := rows.Scan(&discard, &discard, &discard, &discard, &discard, &discard, &discard, &discard, &name, &safe, &ordinal); err != nil {
			return mysqlSessionState{}, nil, err
		}
		if !safe {
			star.columns = nil
			return state, star, nil
		}
		star.columns = append(star.columns, name)
	}
	return state, star, rows.Err()
}

func mysqlAnalyzeOptions(mode, version string) sqlservice.AnalyzeOptions {
	return sqlservice.AnalyzeOptions{MySQLSQLMode: mode, MySQLVersion: dialect.MySQLCompatibilityNumber(version)}
}

func closeConsoleConnection(conn *sql.Conn) {
	// Java Chen gives each query a fresh physical connection. Driver pool
	// resets do not consistently restore USE, search_path, CURRENT_SCHEMA or
	// other session settings, so closed consoles must never be reused.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func connectionContext(ctx context.Context, conn *sql.Conn, dialect string, configured sqlservice.Context) (sqlservice.Context, error) {
	current := configured
	var err error
	switch {
	case core.IsMySQLDialect(dialect):
		err = conn.QueryRowContext(ctx, "SELECT COALESCE(DATABASE(),'')").Scan(&current.Database)
		current.Schema = current.Database
	case dialect == "postgresql":
		err = conn.QueryRowContext(ctx, "SELECT current_database(), COALESCE(current_schema(),'')").Scan(&current.Database, &current.Schema)
	case dialect == "sqlserver":
		err = conn.QueryRowContext(ctx, "SELECT DB_NAME(), COALESCE(SCHEMA_NAME(),'dbo')").Scan(&current.Database, &current.Schema)
	case dialect == "clickhouse":
		err = conn.QueryRowContext(ctx, "SELECT currentDatabase()").Scan(&current.Database)
		current.Schema = current.Database
	case dialect == "db2":
		err = conn.QueryRowContext(ctx, "VALUES (CURRENT SERVER, CURRENT SCHEMA)").Scan(&current.Database, &current.Schema)
	case sqlservice.Dialect(dialect).UsesOracleSyntax():
		err = conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') FROM DUAL").Scan(&current.Schema)
		if current.Database == "" {
			current.Database = current.Schema
		}
	default:
		return current, errors.New("unsupported database context")
	}
	return current, err
}

func initializeConsole(ctx context.Context, c *console, cfg core.ConnectionConfig, requested sqlservice.Context) (sqlservice.Context, int64, error) {
	conn := c.conn
	var err error
	if cfg.Dialect == "dameng" && cfg.Database == "" {
		// Dameng's catalog uses the login schema as its database label when
		// Core omits a database. A pooled console may have changed CURRENT_SCHEMA.
		if err = conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV','SESSION_USER') FROM DUAL").Scan(&cfg.Database); err != nil {
			return requested, 0, err
		}
	}
	if requested.Database == "" {
		requested.Database = cfg.Database
	}
	if requested.Schema == "" {
		requested.Schema = cfg.Schema
	}
	if cfg.Dialect == "mysql" {
		// The driver already selects its configured database during authentication.
		// Read its effective settings and cancellation ID together. These
		// session-local settings remain valid until the first user execution.
		selectDatabase := func() error {
			_, err := conn.ExecContext(ctx, "USE "+quoteDatabaseIdentifier(requested.Database, cfg.Dialect))
			return err
		}
		if requested.Database != "" && requested.Database != cfg.Database {
			if err := selectDatabase(); err != nil {
				return requested, 0, err
			}
		}
		state, err := readMySQLSession(ctx, conn)
		if err == nil && requested.Database != "" && requested.Database == cfg.Database && requested.Database != state.scope.Database {
			if err = selectDatabase(); err == nil {
				state, err = readMySQLSession(ctx, conn)
			}
		}
		if err == nil {
			c.initialMySQL = &state
			return state.scope, state.backendID, nil
		}
		// Some MySQL-compatible proxies expose the database and connection ID
		// but omit optional paging settings. Keep their original initialization.
	}
	if requested.Database != "" {
		switch {
		case core.IsMySQLDialect(cfg.Dialect) || cfg.Dialect == "sqlserver" || cfg.Dialect == "clickhouse":
			_, err = conn.ExecContext(ctx, "USE "+quoteDatabaseIdentifier(requested.Database, cfg.Dialect))
		case cfg.Dialect != "postgresql" && requested.Database != cfg.Database:
			// Catalogs return the server's canonical name, which need not equal
			// the configured alias (Db2 folds names; Dameng may omit a database).
			// Verify against this connection before treating it as a switch.
			current, contextErr := connectionContext(ctx, conn, cfg.Dialect, sqlservice.Context{Database: cfg.Database, Schema: cfg.Schema})
			if contextErr != nil {
				return requested, 0, contextErr
			}
			if requested.Database != current.Database {
				return requested, 0, unsupportedContext(cfg.Dialect)
			}
		}
	}
	if err == nil && requested.Schema != "" {
		quoted := quoteDatabaseIdentifier(requested.Schema, cfg.Dialect)
		switch cfg.Dialect {
		case "postgresql":
			_, err = conn.ExecContext(ctx, "SET search_path TO "+quoted)
		case "oracle", "oceanbase-oracle":
			_, err = conn.ExecContext(ctx, "ALTER SESSION SET CURRENT_SCHEMA = "+quoted)
		case "dameng":
			_, err = conn.ExecContext(ctx, "SET SCHEMA "+quoted)
		case "db2":
			_, err = conn.ExecContext(ctx, "SET CURRENT SCHEMA "+quoted)
			// SQL Server's default schema belongs to the database principal. A UI
			// context is not allowed to ALTER USER merely to browse another schema.
		}
	}
	if err != nil {
		return requested, 0, err
	}
	// Read the effective namespace and cancellation ID from the same session.
	current := requested
	var backend int64
	switch {
	case core.IsMySQLDialect(cfg.Dialect):
		err = conn.QueryRowContext(ctx, "SELECT COALESCE(DATABASE(),''), CONNECTION_ID()").Scan(&current.Database, &backend)
		current.Schema = current.Database
	case cfg.Dialect == "postgresql":
		err = conn.QueryRowContext(ctx, "SELECT pg_catalog.current_database(), COALESCE(pg_catalog.current_schema(),''), pg_catalog.pg_backend_pid()").Scan(&current.Database, &current.Schema, &backend)
	case cfg.Dialect == "oceanbase-oracle":
		err = conn.QueryRowContext(ctx, "SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA'), USERENV('SID') FROM DUAL").Scan(&current.Schema, &backend)
		if current.Database == "" {
			current.Database = current.Schema
		}
	default:
		current, err = connectionContext(ctx, conn, cfg.Dialect, requested)
	}
	return current, backend, err
}
