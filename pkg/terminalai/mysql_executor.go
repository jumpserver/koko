package terminalai

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const (
	maxMySQLRows   = 1000
	maxMySQLOutput = 100 * 1024
)

type MySQLConfig = DatabaseConfig

type MySQLExecutor struct {
	db            *sql.DB
	sanitizer     ValueSanitizer
	metadata      *sqlMetadataTool
	tlsConfigName string
	closeOnce     sync.Once
}

var mysqlTLSSequence atomic.Uint64

func NewMySQLExecutor(ctx context.Context, config MySQLConfig) (*MySQLExecutor, error) {
	driverConfig := mysqlDriver.NewConfig()
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	driverConfig.User = config.Username
	driverConfig.Passwd = config.Password
	driverConfig.DBName = config.Database
	driverConfig.ParseTime = true
	driverConfig.MultiStatements = false
	driverConfig.Params = map[string]string{"autocommit": "true"}

	executor := &MySQLExecutor{sanitizer: NewMySQLSanitizer(config.DataMaskingRules)}
	if config.UseSSL {
		tlsName, err := registerMySQLTLS(config)
		if err != nil {
			return nil, err
		}
		executor.tlsConfigName = tlsName
		driverConfig.TLSConfig = tlsName
	}
	connector, err := mysqlDriver.NewConnector(driverConfig)
	if err != nil {
		executor.deregisterTLS()
		return nil, fmt.Errorf("create MySQL connector: %w", err)
	}
	executor.db = sql.OpenDB(connector)
	executor.metadata = newSQLMetadataTool(executor.db, config.Protocol, config.Database)
	executor.db.SetMaxOpenConns(1)
	executor.db.SetMaxIdleConns(1)
	executor.db.SetConnMaxLifetime(30 * time.Minute)
	if err = executor.db.PingContext(ctx); err != nil {
		_ = executor.Close()
		return nil, fmt.Errorf("initialize MySQL background connection: %w", err)
	}
	return executor, nil
}

func registerMySQLTLS(config MySQLConfig) (string, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.AllowInvalidCert,
	}
	if config.CACert != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(config.CACert)) {
			return "", fmt.Errorf("parse MySQL CA certificate")
		}
		tlsConfig.RootCAs = pool
	}
	if config.ClientCert != "" || config.ClientKey != "" {
		certificate, err := tls.X509KeyPair(
			[]byte(config.ClientCert), []byte(config.ClientKey),
		)
		if err != nil {
			return "", fmt.Errorf("parse MySQL client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	name := fmt.Sprintf("koko-terminal-ai-%d", mysqlTLSSequence.Add(1))
	if err := mysqlDriver.RegisterTLSConfig(name, tlsConfig); err != nil {
		return "", fmt.Errorf("register MySQL TLS config: %w", err)
	}
	return name, nil
}

func (e *MySQLExecutor) Execute(
	ctx context.Context, command string, onOutput func(string),
) (string, *int, error) {
	analysis, err := analyzeSQL(command)
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
			return output, nil, &BackgroundUnavailableError{Cause: healthErr}
		}
	}
	return output, nil, err
}

func (e *MySQLExecutor) SQLMetadataScope() string {
	return e.metadata.Scope()
}

func (e *MySQLExecutor) LookupSQLSchema(
	ctx context.Context, request SQLSchemaLookupRequest,
) (SQLSchemaLookupResult, error) {
	return e.metadata.Lookup(ctx, request)
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
		e.deregisterTLS()
	})
	return err
}

func (e *MySQLExecutor) deregisterTLS() {
	if e.tlsConfigName != "" {
		mysqlDriver.DeregisterTLSConfig(e.tlsConfigName)
		e.tlsConfigName = ""
	}
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
