package sessiontools

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/dbconn"
)

const (
	maxMySQLRows   = 1000
	maxMySQLOutput = 100 * 1024
)

type MySQLConfig = DatabaseConfig

type MySQLExecutor struct {
	protocol  string
	db        *sql.DB
	sanitizer *MySQLSanitizer
	metadata  *sqlMetadataTool
	closeOnce sync.Once
}

func NewMySQLExecutor(ctx context.Context, config MySQLConfig) (*MySQLExecutor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.PlatformType == "oceanbase" {
		return newOceanBaseExecutor(config)
	}
	protocol := config.Protocol
	if protocol == "" {
		protocol = "mysql"
	}
	executor := &MySQLExecutor{protocol: protocol, sanitizer: NewMySQLSanitizer(config.DataMaskingRules)}
	options := dbconn.MySQLOptions{ParseTime: true, Autocommit: "true"}
	if config.UseSSL {
		var err error
		options.TLS, err = buildMySQLTLSConfig(config)
		if err != nil {
			return nil, err
		}
	}
	connector, err := dbconn.MySQLConnector(config.connectionConfig(), options)
	if err != nil {
		return nil, fmt.Errorf("create MySQL connector: %w", err)
	}
	executor.db = sql.OpenDB(connector)
	executor.metadata = newSQLMetadataTool(executor.db, config.Protocol, config.Database)
	executor.db.SetMaxOpenConns(1)
	executor.db.SetMaxIdleConns(1)
	executor.db.SetConnMaxLifetime(30 * time.Minute)
	// Keep the pool available across transient outages; connect on execution.
	return executor, nil
}

func newOceanBaseExecutor(config MySQLConfig) (*MySQLExecutor, error) {
	protocol, err := DatabaseDialect(config)
	if err != nil {
		return nil, err
	}
	db, err := newOceanBaseDatabase(config)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &MySQLExecutor{
		protocol:  protocol,
		db:        db,
		sanitizer: NewMySQLSanitizer(config.DataMaskingRules),
		metadata:  newOceanBaseSQLMetadataTool(db, protocol, config.Database),
	}, nil
}

func buildMySQLTLSConfig(config MySQLConfig) (*tls.Config, error) {
	c := config.connectionConfig()
	c.Dialect, c.TLS.Enabled = "mysql", true
	return dbconn.MakeTLS(c, true)
}

func (e *MySQLExecutor) Execute(
	ctx context.Context, command string, onOutput func(string),
) (string, *int, error) {
	analysis, err := analyzeSQL(command, e.protocol)
	if err != nil {
		return "", nil, err
	}
	if !analysis.BackgroundEligible() {
		return "", nil, errors.New(analysis.PTYReason())
	}
	var output string
	if analysis.kind == sqlRead {
		output, err = e.query(ctx, command)
	} else {
		output, err = e.exec(ctx, command)
	}
	if err == nil && isSchemaChangingSQL(analysis) {
		e.metadata.Invalidate()
	}
	if output != "" && onOutput != nil {
		onOutput(output)
	}
	if err != nil && ctx.Err() == nil {
		healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if healthErr := e.db.PingContext(healthCtx); healthErr != nil {
			return output, nil, &ExecutionUnavailableError{Cause: e.sanitizer.diagnostic(healthErr)}
		}
	}
	return output, nil, e.sanitizer.diagnostic(err)
}

func (e *MySQLExecutor) SQLMetadataScope() string {
	return e.metadata.Scope()
}

func (e *MySQLExecutor) LookupSQLSchema(
	ctx context.Context, request SQLSchemaLookupRequest,
) (SQLSchemaLookupResult, error) {
	return e.metadata.Lookup(ctx, request, e.sanitizer)
}

func (e *MySQLExecutor) InvalidateSQLMetadata() {
	e.metadata.Invalidate()
}

func (e *MySQLExecutor) query(ctx context.Context, command string) (string, error) {
	rows, err := e.db.QueryContext(ctx, command)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	output := newMySQLOutput()
	if !output.addLine(strings.Join(columns, "\t")) {
		return output.String(), nil
	}
	values := make([]any, len(columns))
	destinations := make([]any, len(columns))
	for index := range values {
		destinations[index] = &values[index]
	}
	count := 0
	for rows.Next() {
		if count >= maxMySQLRows {
			output.truncated = true
			break
		}
		if err = rows.Scan(destinations...); err != nil {
			return output.String(), err
		}
		fields := make([]string, len(columns))
		for index, value := range values {
			fields[index] = e.formatValue(columns[index], value)
		}
		if !output.addLine(strings.Join(fields, "\t")) {
			break
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return output.String(), err
	}
	return output.String(), nil
}

func (e *MySQLExecutor) exec(ctx context.Context, command string) (string, error) {
	result, err := e.db.ExecContext(ctx, command)
	if err != nil {
		return "", err
	}
	affected, affectedErr := result.RowsAffected()
	insertID, insertErr := result.LastInsertId()
	var lines []string
	if affectedErr == nil {
		lines = append(lines, fmt.Sprintf("Rows affected: %d", affected))
	}
	if insertErr == nil && insertID != 0 {
		lines = append(lines, fmt.Sprintf("Last insert ID: %d", insertID))
	}
	if len(lines) == 0 {
		return "Statement completed", nil
	}
	return strings.Join(lines, "\n"), nil
}

func (e *MySQLExecutor) formatValue(column string, value any) string {
	if value == nil {
		return "NULL"
	}
	var text string
	switch typed := value.(type) {
	case []byte:
		text = string(typed)
	case time.Time:
		text = typed.Format(time.RFC3339Nano)
	default:
		text = fmt.Sprint(value)
	}
	text = e.sanitizer.Sanitize(column, text)
	text = strings.ToValidUTF8(text, "\uFFFD")
	text = strings.ReplaceAll(text, "\t", `\t`)
	text = strings.ReplaceAll(text, "\r", `\r`)
	return strings.ReplaceAll(text, "\n", `\n`)
}

func (e *MySQLExecutor) Close() error {
	var err error
	e.closeOnce.Do(func() {
		if e.db != nil {
			err = e.db.Close()
		}
	})
	return err
}

type mysqlOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func newMySQLOutput() *mysqlOutput {
	return &mysqlOutput{}
}

func (o *mysqlOutput) addLine(line string) bool {
	suffix := ""
	if o.buffer.Len() > 0 {
		suffix = "\n"
	}
	if o.buffer.Len()+len(suffix)+len(line) > maxMySQLOutput {
		o.truncated = true
		return false
	}
	o.buffer.WriteString(suffix)
	o.buffer.WriteString(line)
	return true
}

func (o *mysqlOutput) String() string {
	if !o.truncated {
		return o.buffer.String()
	}
	const marker = "\n[output truncated at 1000 rows or 100 KiB]"
	available := maxMySQLOutput - len(marker)
	value := o.buffer.Bytes()
	if len(value) > available {
		value = value[:available]
		for len(value) > 0 && !utf8.Valid(value) {
			value = value[:len(value)-1]
		}
	}
	return string(value) + marker
}
