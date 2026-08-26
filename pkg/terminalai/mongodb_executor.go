package terminalai

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var mongoDBRunCommandPattern = regexp.MustCompile(
	`(?s)^\s*db\s*\.\s*runCommand\s*\((.*)\)\s*;?\s*$`,
)

type MongoDBExecutor struct {
	client    *mongo.Client
	database  *mongo.Database
	sanitizer ValueSanitizer
	closeOnce sync.Once
}

func NewMongoDBExecutor(
	ctx context.Context,
	config DatabaseConfig,
) (*MongoDBExecutor, error) {
	clientOptions, err := mongoDBClientOptions(config)
	if err != nil {
		return nil, err
	}
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("initialize MongoDB background connection: %w", err)
	}
	database := strings.TrimSpace(config.Database)
	if database == "" {
		database = "test"
	}
	executor := &MongoDBExecutor{
		client: client, database: client.Database(database),
		sanitizer: NewMySQLSanitizer(config.DataMaskingRules),
	}
	if err = client.Ping(ctx, nil); err != nil {
		_ = executor.Close()
		return nil, fmt.Errorf("initialize MongoDB background connection: %w", err)
	}
	return executor, nil
}

func (e *MongoDBExecutor) Execute(
	ctx context.Context,
	command string,
	onOutput func(string),
) (string, *int, error) {
	document, err := parseMongoDBCommand(command)
	if err != nil {
		return "", nil, err
	}
	if !mongoDBBackgroundEligible(document) {
		return "", nil, fmt.Errorf("session-dependent MongoDB commands require the active PTY")
	}
	var result bson.D
	err = e.database.RunCommand(ctx, document).Decode(&result)
	if err != nil {
		if ctx.Err() == nil {
			healthCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if healthErr := e.client.Ping(healthCtx, nil); healthErr != nil {
				return "", nil, &BackgroundUnavailableError{Cause: healthErr}
			}
		}
		return "", nil, err
	}
	result = sanitizeMongoDBDocument(result, e.sanitizer)
	encoded, err := bson.MarshalExtJSON(result, false, false)
	if err != nil {
		return "", nil, fmt.Errorf("encode MongoDB result: %w", err)
	}
	outputWriter := &boundedDatabaseOutput{}
	_, _ = outputWriter.Write(encoded)
	output := outputWriter.String()
	if output != "" && onOutput != nil {
		onOutput(output)
	}
	exitCode := 0
	return output, &exitCode, nil
}

func (e *MongoDBExecutor) Close() error {
	var err error
	e.closeOnce.Do(func() {
		if e.client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = e.client.Disconnect(ctx)
		}
	})
	return err
}

func parseMongoDBCommand(command string) (bson.D, error) {
	matches := mongoDBRunCommandPattern.FindStringSubmatch(command)
	if len(matches) != 2 {
		return nil, fmt.Errorf(
			"MongoDB background execution requires db.runCommand with strict Extended JSON",
		)
	}
	var document bson.D
	if err := bson.UnmarshalExtJSON([]byte(strings.TrimSpace(matches[1])), false, &document); err != nil {
		return nil, fmt.Errorf("invalid MongoDB db.runCommand document: %w", err)
	}
	if len(document) == 0 {
		return nil, fmt.Errorf("MongoDB db.runCommand document is empty")
	}
	return document, nil
}

func classifyMongoDBProposal(document bson.D, proposal *CommandProposal) {
	command := strings.ToLower(document[0].Key)
	switch command {
	case "find", "count", "distinct", "listcollections", "listdatabases",
		"listindexes", "collstats", "dbstats", "explain", "ping", "hello",
		"ismaster", "buildinfo", "serverstatus", "currentop", "getmore":
		return
	case "drop", "dropdatabase", "shutdown", "renamecollection", "killop",
		"compact", "repairdatabase", "fsync", "fsyncunlock",
		"createuser", "updateuser", "dropuser", "grantrolestouser",
		"revokerolesfromuser", "createrole", "updaterole", "droprole",
		"grantprivilegestorole", "revokeprivilegesfromrole", "setparameter",
		"replsetreconfig", "replsetstepdown":
		proposal.RiskLevel, proposal.RiskReason = raiseRisk(
			proposal.RiskLevel, proposal.RiskReason, 4,
			"backend rule detected a destructive or administrative MongoDB command",
		)
	default:
		proposal.RiskLevel, proposal.RiskReason = raiseRisk(
			proposal.RiskLevel, proposal.RiskReason, 2,
			"backend rule detected a potentially data-changing MongoDB command",
		)
	}
	proposal.ApprovalRequired = true
}

func mongoDBBackgroundEligible(document bson.D) bool {
	if len(document) == 0 {
		return false
	}
	switch strings.ToLower(document[0].Key) {
	case "getmore", "committransaction", "aborttransaction", "authenticate", "logout",
		"saslstart", "saslcontinue":
		return false
	default:
		return true
	}
}

func mongoDBClientOptions(config DatabaseConfig) (*options.ClientOptions, error) {
	host := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	connectionURL := url.URL{Scheme: "mongodb", Host: host, Path: config.Database}
	if config.Username != "" || config.Password != "" {
		connectionURL.User = url.UserPassword(config.Username, config.Password)
	}
	params, err := url.ParseQuery(config.ConnectionOpts)
	if err != nil {
		return nil, fmt.Errorf("parse MongoDB connection options: %w", err)
	}
	authSource := strings.TrimSpace(config.AuthSource)
	if authSource == "" {
		authSource = "admin"
	}
	params.Set("authSource", authSource)
	if config.UseSSL {
		params.Set("tls", "true")
	}
	connectionURL.RawQuery = params.Encode()
	clientOptions := options.Client().ApplyURI(connectionURL.String()).SetMaxPoolSize(1)
	if config.UseSSL {
		tlsConfig, err := databaseTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("configure MongoDB TLS: %w", err)
		}
		clientOptions.SetTLSConfig(tlsConfig)
	}
	if config.ProxyURL != "" {
		dialer, err := newMongoDBProxyDialer(config.ProxyURL)
		if err != nil {
			return nil, err
		}
		clientOptions.SetDialer(dialer)
	}
	return clientOptions, nil
}

func databaseTLSConfig(config DatabaseConfig) (*tls.Config, error) {
	result := &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: config.ServerName,
		InsecureSkipVerify: config.AllowInvalidCert,
	}
	if config.CACert != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(config.CACert)) {
			return nil, fmt.Errorf("parse CA certificate")
		}
		result.RootCAs = pool
	}
	if config.ClientCert != "" || config.ClientKey != "" {
		certificate, err := tls.X509KeyPair(
			[]byte(config.ClientCert), []byte(config.ClientKey),
		)
		if err != nil {
			return nil, fmt.Errorf("parse client certificate: %w", err)
		}
		result.Certificates = []tls.Certificate{certificate}
	}
	return result, nil
}

func sanitizeMongoDBDocument(document bson.D, sanitizer ValueSanitizer) bson.D {
	result := make(bson.D, len(document))
	for index, element := range document {
		result[index] = bson.E{
			Key:   element.Key,
			Value: sanitizeMongoDBValue(element.Key, element.Value, sanitizer),
		}
	}
	return result
}

func sanitizeMongoDBValue(field string, value any, sanitizer ValueSanitizer) any {
	switch typed := value.(type) {
	case bson.D:
		return sanitizeMongoDBDocument(typed, sanitizer)
	case bson.A:
		result := make(bson.A, len(typed))
		for index, item := range typed {
			result[index] = sanitizeMongoDBValue(field, item, sanitizer)
		}
		return result
	default:
		text := fmt.Sprint(value)
		if masked := sanitizer.Sanitize(field, text); masked != text {
			return masked
		}
		return value
	}
}

type mongoDBProxyDialer struct {
	proxyAddress string
	dialer       net.Dialer
}

func newMongoDBProxyDialer(proxyURL string) (*mongoDBProxyDialer, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid MongoDB HTTP proxy URL %q", proxyURL)
	}
	return &mongoDBProxyDialer{proxyAddress: parsed.Host}, nil
}

func (d *mongoDBProxyDialer) DialContext(
	ctx context.Context,
	network, address string,
) (net.Conn, error) {
	connection, err := d.dialer.DialContext(ctx, network, d.proxyAddress)
	if err != nil {
		return nil, err
	}
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: address}, Host: address,
		Header: make(http.Header),
	}
	if err = request.Write(connection); err != nil {
		_ = connection.Close()
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), request)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_ = connection.Close()
		return nil, fmt.Errorf(
			"MongoDB proxy connect %s via %s failed: %s",
			address, d.proxyAddress, response.Status,
		)
	}
	return connection, nil
}
