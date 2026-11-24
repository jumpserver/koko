package db2

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-db2/go-db2/internal/validate"
)

const defaultAdminTimeout = 60 * time.Second

func validateSQLIdentifier(val, fieldName string) error {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return fmt.Errorf("db2: %s cannot be empty", fieldName)
	}
	if !validate.IsValidSQLIdentifier(trimmed) {
		return fmt.Errorf("db2: invalid %s identifier %q (must be alphanumeric or _, #, $)", fieldName, val)
	}
	return nil
}

// quoteIdentifier safely quotes an SQL identifier with double quotes, escaping any embedded double quotes.
// Optimization: Uses strings.IndexByte segment jumping and block sb.WriteString appends to eliminate character-by-character loops and strings.Count pre-scans when escaping embedded double quotes.
func quoteIdentifier(name string) string {
	idx := strings.IndexByte(name, '"')
	if idx == -1 {
		return `"` + name + `"`
	}
	var sb strings.Builder
	sb.Grow(len(name) + 6)
	sb.WriteByte('"')
	sb.WriteString(name[:idx])
	sb.WriteString(`""`)
	rest := name[idx+1:]
	for {
		nextIdx := strings.IndexByte(rest, '"')
		if nextIdx == -1 {
			sb.WriteString(rest)
			break
		}
		sb.WriteString(rest[:nextIdx])
		sb.WriteString(`""`)
		rest = rest[nextIdx+1:]
	}
	sb.WriteByte('"')
	return sb.String()
}

// buildCreateDbSQL constructs the CREATE DATABASE SQL DDL statement using quoted identifiers.
func buildCreateDbSQL(dbname, codeset, territory, mode string) string {
	var sb strings.Builder
	sb.WriteString("CREATE DATABASE ")
	sb.WriteString(quoteIdentifier(dbname))
	if codeset != "" {
		sb.WriteString(" CODESET ")
		sb.WriteString(quoteIdentifier(codeset))
	}
	if territory != "" {
		sb.WriteString(" TERRITORY ")
		sb.WriteString(quoteIdentifier(territory))
	}
	if mode != "" {
		sb.WriteString(" ")
		sb.WriteString(quoteIdentifier(mode))
	}
	return sb.String()
}

// buildDropDbSQL constructs the DROP DATABASE SQL DDL statement using quoted identifiers.
func buildDropDbSQL(dbname string) string {
	return "DROP DATABASE " + quoteIdentifier(dbname)
}

// CreateDB creates a new database on the IBM Db2 server specified in connStr.
// Optional options can be provided in "key=value" format (e.g. "codeset=UTF-8", "territory=US", "mode=...").
func CreateDB(ctx context.Context, dbname string, connStr string, options ...string) error {
	if err := validateSQLIdentifier(dbname, "database name"); err != nil {
		return err
	}
	if strings.TrimSpace(connStr) == "" {
		return fmt.Errorf("db2: connection string cannot be empty")
	}

	var codeset, territory, mode string
	for _, opt := range options {
		parts := strings.SplitN(opt, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("db2: invalid option format %q (expected key=value)", opt)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "codeset":
			if err := validateSQLIdentifier(val, "codeset"); err != nil {
				return err
			}
			codeset = val
		case "territory":
			if err := validateSQLIdentifier(val, "territory"); err != nil {
				return err
			}
			territory = val
		case "mode":
			if err := validateSQLIdentifier(val, "mode"); err != nil {
				return err
			}
			mode = val
		default:
			return fmt.Errorf("db2: unsupported option %q", key)
		}
	}

	db, err := sql.Open("db2", connStr)
	if err != nil {
		return fmt.Errorf("db2: failed to open connection: %w", err)
	}
	defer db.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAdminTimeout)
		defer cancel()
	}

	createSQL := buildCreateDbSQL(dbname, codeset, territory, mode)
	_, err = db.ExecContext(ctx, createSQL)
	if err != nil {
		// Also try via SYSPROC.ADMIN_CMD if direct DDL requires administrative routing
		if _, adminErr := ExecAdminCmd(ctx, db, createSQL); adminErr == nil {
			return nil
		}
		return fmt.Errorf("db2: failed to create database %q: %w", dbname, err)
	}

	return nil
}

// CreateDb is a backwards-compatible alias for CreateDB.
// Deprecated: Use CreateDB with context.Context.
func CreateDb(dbname string, connStr string, options ...string) (bool, error) {
	err := CreateDB(context.Background(), dbname, connStr, options...)
	return err == nil, err
}

// DropDB drops the specified database from the IBM Db2 server.
func DropDB(ctx context.Context, dbname string, connStr string) error {
	if err := validateSQLIdentifier(dbname, "database name"); err != nil {
		return err
	}
	if strings.TrimSpace(connStr) == "" {
		return fmt.Errorf("db2: connection string cannot be empty")
	}

	db, err := sql.Open("db2", connStr)
	if err != nil {
		return fmt.Errorf("db2: failed to open connection: %w", err)
	}
	defer db.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAdminTimeout)
		defer cancel()
	}

	dropSQL := buildDropDbSQL(dbname)
	_, err = db.ExecContext(ctx, dropSQL)
	if err != nil {
		if _, adminErr := ExecAdminCmd(ctx, db, dropSQL); adminErr == nil {
			return nil
		}
		return fmt.Errorf("db2: failed to drop database %q: %w", dbname, err)
	}

	return nil
}

// DropDb is a backwards-compatible alias for DropDB.
// Deprecated: Use DropDB with context.Context.
func DropDb(dbname string, connStr string) (bool, error) {
	err := DropDB(context.Background(), dbname, connStr)
	return err == nil, err
}

// ExecAdminCmd executes an administrative command via Db2's SYSPROC.ADMIN_CMD stored procedure.
// It enforces parameterized procedure execution to prevent SQL injection vulnerabilities and statement syntax errors.
func ExecAdminCmd(ctx context.Context, db *sql.DB, command string) (sql.Result, error) {
	// Sanitize null bytes before trimming and validating command non-emptiness
	command = strings.ReplaceAll(command, "\x00", "")
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil, fmt.Errorf("db2: admin command cannot be empty")
	}
	if db == nil {
		return nil, fmt.Errorf("db2: db cannot be nil")
	}
	// Enforce parameterized CALL SYSPROC.ADMIN_CMD(?) execution
	return db.ExecContext(ctx, "CALL SYSPROC.ADMIN_CMD(?)", trimmed)
}
