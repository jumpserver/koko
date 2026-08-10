package terminalai

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jumpserver/koko/pkg/srvconn"
	mssql "github.com/microsoft/go-mssqldb"
	go_ora "github.com/sijms/go-ora/v2"
)

const maxNativeSQLRows = 1000

type NativeSQLExecutor struct {
	db        *sql.DB
	sanitizer ValueSanitizer
	closeOnce sync.Once
}

func NewNativeSQLExecutor(
	ctx context.Context,
	config DatabaseConfig,
) (*NativeSQLExecutor, error) {
	db, err := newNativeSQLDatabase(config)
	if err != nil {
		return nil, err
	}
	executor := &NativeSQLExecutor{
		db: db, sanitizer: NewMySQLSanitizer(config.DataMaskingRules),
	}
	if err = db.PingContext(ctx); err != nil {
		_ = executor.Close()
		return nil, fmt.Errorf("initialize %s background connection: %w", config.Protocol, err)
	}
	return executor, nil
}

func (e *NativeSQLExecutor) Execute(
	ctx context.Context,
	command string,
	onOutput func(string),
) (string, *int, error) {
	analysis, err := analyzeSQL(command)
	if err != nil {
		return "", nil, err
	}
	if !analysis.BackgroundEligible() {
		return "", nil, fmt.Errorf("%s", analysis.PTYReason())
	}
	var output string
	if analysis.kind == sqlRead {
		output, err = e.query(ctx, command)
	} else {
		output, err = e.exec(ctx, command)
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

func (e *NativeSQLExecutor) query(ctx context.Context, command string) (string, error) {
	rows, err := e.db.QueryContext(ctx, command)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	output := &boundedDatabaseOutput{}
	firstLine := true
	appendLine := func(line string) bool {
		if !firstLine {
			line = "\n" + line
		}
		firstLine = false
		_, _ = output.Write([]byte(line))
		return !output.Truncated()
	}
	if !appendLine(strings.Join(columns, "\t")) {
		return output.String(), nil
	}
	values := make([]any, len(columns))
	destinations := make([]any, len(columns))
	for index := range values {
		destinations[index] = &values[index]
	}
	count := 0
	for rows.Next() {
		if count >= maxNativeSQLRows {
			appendLine("[output truncated at 1000 rows]")
			break
		}
		if err = rows.Scan(destinations...); err != nil {
			return output.String(), err
		}
		fields := make([]string, len(columns))
		for index, value := range values {
			fields[index] = formatNativeSQLValue(e.sanitizer, columns[index], value)
		}
		if !appendLine(strings.Join(fields, "\t")) {
			break
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return output.String(), err
	}
	return output.String(), nil
}

func (e *NativeSQLExecutor) exec(ctx context.Context, command string) (string, error) {
	result, err := e.db.ExecContext(ctx, command)
	if err != nil {
		return "", err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr == nil {
		return fmt.Sprintf("Rows affected: %d", affected), nil
	}
	return "Statement completed", nil
}

func (e *NativeSQLExecutor) Close() error {
	var err error
	e.closeOnce.Do(func() {
		if e.db != nil {
			err = e.db.Close()
		}
	})
	return err
}

func formatNativeSQLValue(sanitizer ValueSanitizer, column string, value any) string {
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
	text = sanitizer.Sanitize(column, text)
	text = strings.ToValidUTF8(text, "\uFFFD")
	text = strings.ReplaceAll(text, "\t", `\t`)
	text = strings.ReplaceAll(text, "\r", `\r`)
	return strings.ReplaceAll(text, "\n", `\n`)
}

func newNativeSQLDatabase(config DatabaseConfig) (*sql.DB, error) {
	var (
		db  *sql.DB
		err error
	)
	switch config.Protocol {
	case srvconn.ProtocolPostgresql:
		db, err = newPostgreSQLDatabase(config)
	case srvconn.ProtocolSQLServer:
		db, err = newSQLServerDatabase(config)
	case srvconn.ProtocolOracle:
		db, err = newOracleDatabase(config)
	case srvconn.ProtocolClickHouse:
		db, err = newClickHouseDatabase(config)
	default:
		err = fmt.Errorf("unsupported native SQL background protocol %s", config.Protocol)
	}
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

func newPostgreSQLDatabase(config DatabaseConfig) (*sql.DB, error) {
	dsn, cleanup, err := buildPostgreSQLDSN(config)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	pgConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("configure PostgreSQL background connection: %w", err)
	}
	configurePostgreSQLTLS(pgConfig, config)
	return stdlib.OpenDB(*pgConfig), nil
}

func buildPostgreSQLDSN(config DatabaseConfig) (string, func(), error) {
	dsn := url.URL{
		Scheme: "postgres", Host: net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		User: url.UserPassword(config.Username, config.Password), Path: config.Database,
	}
	params := url.Values{}
	paths := make([]string, 0, 3)
	cleanup := func() { removeBackgroundSecrets(paths...) }
	if !config.UseSSL {
		params.Set("sslmode", "disable")
	} else {
		sslMode := strings.TrimSpace(config.PGSSLMode)
		if sslMode == "" {
			sslMode = "require"
		}
		params.Set("sslmode", sslMode)
		certificates := []struct{ parameter, name, value string }{
			{"sslrootcert", "postgres-ca", config.CACert},
			{"sslcert", "postgres-cert", config.ClientCert},
			{"sslkey", "postgres-key", config.ClientKey},
		}
		for _, certificate := range certificates {
			if certificate.value == "" {
				continue
			}
			path, writeErr := writeBackgroundSecret(certificate.name, certificate.value)
			if writeErr != nil {
				cleanup()
				return "", func() {}, writeErr
			}
			paths = append(paths, path)
			params.Set(certificate.parameter, path)
		}
	}
	dsn.RawQuery = params.Encode()
	return dsn.String(), cleanup, nil
}

func configurePostgreSQLTLS(config *pgx.ConnConfig, database DatabaseConfig) {
	configure := func(tlsConfig *tls.Config) {
		if tlsConfig == nil {
			return
		}
		if database.ServerName != "" {
			tlsConfig.ServerName = database.ServerName
		}
		if database.AllowInvalidCert {
			tlsConfig.InsecureSkipVerify = true
			tlsConfig.VerifyPeerCertificate = nil
		}
	}
	configure(config.TLSConfig)
	for _, fallback := range config.Fallbacks {
		configure(fallback.TLSConfig)
	}
}

func newSQLServerDatabase(config DatabaseConfig) (*sql.DB, error) {
	dsn, cleanup, err := buildSQLServerDSN(config)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	connector, err := mssql.NewConnector(dsn)
	if err != nil {
		return nil, fmt.Errorf("configure SQL Server background connection: %w", err)
	}
	return sql.OpenDB(connector), nil
}

func buildSQLServerDSN(config DatabaseConfig) (string, func(), error) {
	dsn := url.URL{
		Scheme: "sqlserver", Host: net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		User: url.UserPassword(config.Username, config.Password),
	}
	params := url.Values{}
	if config.Database != "" {
		params.Set("database", config.Database)
	}
	if config.DisableEncrypt {
		params.Set("encrypt", "disable")
	} else if config.Encrypt || config.UseSSL {
		params.Set("encrypt", "true")
		params.Set("TrustServerCertificate", strconv.FormatBool(config.AllowInvalidCert))
		if config.ServerName != "" {
			params.Set("hostNameInCertificate", config.ServerName)
		}
	}
	cleanup := func() {}
	if config.CACert != "" && !config.DisableEncrypt && (config.Encrypt || config.UseSSL) {
		path, err := writeBackgroundSecret("sqlserver-ca", config.CACert)
		if err != nil {
			return "", cleanup, err
		}
		cleanup = func() { removeBackgroundSecrets(path) }
		params.Set("certificate", path)
	}
	dsn.RawQuery = params.Encode()
	return dsn.String(), cleanup, nil
}

func newOracleDatabase(config DatabaseConfig) (*sql.DB, error) {
	host := config.Host
	var dialer *fixedAddressDialer
	if config.ServerName != "" && config.ServerName != config.Host {
		host = config.ServerName
		dialer = &fixedAddressDialer{
			address: net.JoinHostPort(config.Host, strconv.Itoa(config.Port)),
		}
	}
	options := map[string]string{}
	if config.UseSSL {
		options["SSL"] = "true"
	}
	dsn := go_ora.BuildUrl(
		host, config.Port, config.Database, config.Username, config.Password, options,
	)
	connector, ok := go_ora.NewConnector(dsn).(*go_ora.OracleConnector)
	if !ok {
		return nil, fmt.Errorf("configure Oracle background connection")
	}
	if dialer != nil {
		connector.Dialer(dialer)
	}
	if config.UseSSL {
		tlsConfig, err := databaseTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("configure Oracle TLS: %w", err)
		}
		connector.WithTLSConfig(tlsConfig)
	}
	return sql.OpenDB(connector), nil
}

func newClickHouseDatabase(config DatabaseConfig) (*sql.DB, error) {
	options := &clickhouse.Options{
		Protocol: clickhouse.Native,
		Addr:     []string{net.JoinHostPort(config.Host, strconv.Itoa(config.Port))},
		Auth: clickhouse.Auth{
			Database: config.Database, Username: config.Username, Password: config.Password,
		},
		MaxOpenConns: 1, MaxIdleConns: 1, ConnMaxLifetime: 30 * time.Minute,
	}
	if config.UseSSL {
		tlsConfig, err := databaseTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("configure ClickHouse TLS: %w", err)
		}
		options.TLS = tlsConfig
	}
	return clickhouse.OpenDB(options), nil
}

type fixedAddressDialer struct {
	address string
	dialer  net.Dialer
}

func (d *fixedAddressDialer) DialContext(
	ctx context.Context,
	network, _ string,
) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, d.address)
}
