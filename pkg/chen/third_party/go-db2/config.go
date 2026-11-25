package db2

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-db2/go-db2/internal/drda"
)

var (
	dsnPasswordURLRegex = regexp.MustCompile(`(://[^:]+:)([^/\?\s#]+)(@([a-zA-Z0-9.\-_\[\]]*)(:[^/\?\s"']+)?(/|\?|#|\s|"|'|$))`)
	dsnPasswordKVRegex  = regexp.MustCompile(`(?i)\b(password|pwd)\s*=\s*("[^"]*"|'[^']*'|[^;&\s"']+)`)
)

type redactedError struct {
	err error
}

func (e *redactedError) Error() string {
	return redactDSNPassword(e.err.Error())
}

func (e *redactedError) Unwrap() error {
	return e.err
}

func redactDSNPassword(s string) string {
	s = dsnPasswordURLRegex.ReplaceAllString(s, "${1}******${3}")
	s = dsnPasswordKVRegex.ReplaceAllString(s, "${1}=******")
	return s
}

// Config holds the configuration options parsed from a connection string (DSN or URL).
type Config struct {
	Host              string
	Port              int
	Database          string
	User              string
	Password          string
	UseSSL            bool
	SSLRootCAPath     string
	SSLClientCertPath string
	SSLClientKeyPath  string
	// TLSConfig, when set together with UseSSL, is used for the TLS handshake
	// instead of the SSLRootCAPath, SSLClientCertPath and SSLClientKeyPath
	// files. It lets a caller that holds certificates in memory, not on disk,
	// build the root CA pool or client certificate itself. It cannot be set
	// from a DSN.
	TLSConfig *tls.Config
	Timeout   time.Duration
	// DialContext optionally opens the TCP transport before TLS and DRDA negotiation.
	DialContext        func(context.Context, string, string) (net.Conn, error)
	BlockSize          int
	SecurityMechanism  uint16
	AllowPlaintextAuth bool
	KerberosSPN        string
	KerberosRealm      string
	Krb5ConfigFile     string
	Krb5KeytabFile     string
	Krb5CCacheFile     string
	ClientApplName     string
	ClientWrkstnName   string
	ClientUserid       string
	ClientAcctng       string
	ClientCorrToken    string
	Params             map[string]string
	portExplicitlySet  bool
}

// sanitizeLogString removes ASCII control characters (< 32 and 127) for log sanitization.
// Optimization: Fast-path byte scan returns clean strings directly (0 B/op, 0 allocs/op) and uses a single pre-allocated strings.Builder pass for modified strings.
func sanitizeLogString(s string) string {
	idx := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 32 || c == 127 {
			idx = i
			break
		}
	}
	if idx == -1 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	sb.WriteString(s[:idx])
	for i := idx; i < len(s); i++ {
		c := s[i]
		if c >= 32 && c != 127 {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// String implements fmt.Stringer to ensure sensitive credentials (Password) are redacted in logs.
func (c Config) String() string {
	return fmt.Sprintf("Config{Host:%s, Port:%d, Database:%s, User:%s, Password:\"******\", UseSSL:%t, Timeout:%v}",
		sanitizeLogString(c.Host), c.Port, sanitizeLogString(c.Database), sanitizeLogString(c.User), c.UseSSL, c.Timeout)
}

// GoString implements fmt.GoStringer to ensure sensitive credentials (Password) are redacted in logs.
func (c Config) GoString() string {
	return c.String()
}

// NewConfig returns a default Config with standard Db2 settings.
func NewConfig() *Config {
	return &Config{
		Host:      "localhost",
		Port:      50000,
		Timeout:   30 * time.Second,
		BlockSize: 65535,
		Params:    make(map[string]string),
	}
}

// ParseDSN parses a Db2 connection string in either URL format or Key-Value DSN format.
//
// Examples:
//
//	db2://user:password@localhost:50000/sample?ssl=true&timeout=30s
//	host=localhost;port=50000;database=sample;user=db2inst1;password=password;ssl=false;
func ParseDSN(dsn string) (*Config, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, ErrInvalidConnectionStr
	}

	var cfg *Config
	var err error
	if strings.HasPrefix(dsn, "db2://") || strings.HasPrefix(dsn, "db2s://") {
		cfg, err = parseURL(dsn)
	} else {
		cfg, err = parseKeyValue(dsn)
	}

	if err != nil {
		return nil, &redactedError{err: err}
	}

	if !cfg.portExplicitlySet {
		if cfg.UseSSL {
			cfg.Port = 50001
		} else {
			cfg.Port = 50000
		}
	}

	if (cfg.SecurityMechanism == 0 || cfg.SecurityMechanism == 3) && !cfg.UseSSL && !cfg.AllowPlaintextAuth && cfg.Password != "" {
		return nil, &redactedError{err: errors.New("db2: plaintext authentication over unencrypted connection is disabled by default; set allow_plaintext_auth=true to override")}
	}

	return cfg, nil
}

func parseURL(dsn string) (*Config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConnectionStr, redactDSNPassword(err.Error()))
	}

	if u.Scheme != "db2" && u.Scheme != "db2s" {
		return nil, fmt.Errorf("%w: expected scheme 'db2' or 'db2s'", ErrInvalidConnectionStr)
	}

	cfg := NewConfig()
	if u.Scheme == "db2s" {
		cfg.UseSSL = true
	}

	// Host and Port
	if u.Hostname() != "" {
		cfg.Host = u.Hostname()
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("%w: invalid port %s", ErrInvalidConnectionStr, u.Port())
		}
		cfg.Port = port
		cfg.portExplicitlySet = true
	}

	// Database name (Path)
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, fmt.Errorf("%w: missing database name", ErrInvalidConnectionStr)
	}
	cfg.Database = dbName

	// User and Password
	if u.User != nil {
		cfg.User = u.User.Username()
		if pwd, set := u.User.Password(); set {
			cfg.Password = pwd
		}
	}

	// Query parameters
	query := u.Query()
	for key, values := range query {
		if len(values) > 0 {
			if err := applyParam(cfg, key, values[0]); err != nil {
				return nil, err
			}
		}
	}

	return cfg, nil
}

func parseKeyValue(dsn string) (*Config, error) {
	cfg := NewConfig()
	pairs := strings.Split(dsn, ";")

	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("db2: malformed DSN parameter: %q", pair)
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if err := applyParam(cfg, key, val); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func parseBoolValue(key, val string) (bool, error) {
	switch strings.ToLower(val) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("db2: invalid boolean value for %s: %q", key, val)
	}
}

func applyParam(cfg *Config, key, val string) error {
	lowerKey := strings.ToLower(key)
	switch lowerKey {
	case "host", "server", "hostname":
		if h, p, err := net.SplitHostPort(val); err == nil {
			cfg.Host = h
			if port, err := strconv.Atoi(p); err == nil && port > 0 && port <= 65535 {
				cfg.Port = port
				cfg.portExplicitlySet = true
			}
		} else {
			cfg.Host = val
		}
	case "port":
		port, err := strconv.Atoi(val)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("db2: invalid port: %q", val)
		}
		cfg.Port = port
		cfg.portExplicitlySet = true
	case "database", "dbname", "db":
		cfg.Database = val
	case "user", "uid", "username":
		cfg.User = val
	case "password", "pwd":
		cfg.Password = val
	case "ssl", "use_ssl", "usessl", "tls":
		b, err := parseBoolValue(key, val)
		if err != nil {
			return err
		}
		cfg.UseSSL = b
	case "ssl_root_ca", "ssl_ca", "sslrootcapath", "ssl_ca_path":
		cfg.SSLRootCAPath = val
		cfg.UseSSL = true
	case "ssl_client_cert_path", "ssl_client_cert", "sslcert", "cert":
		cfg.SSLClientCertPath = val
		cfg.UseSSL = true
	case "ssl_client_key_path", "ssl_client_key", "sslkey", "key":
		cfg.SSLClientKeyPath = val
		cfg.UseSSL = true
	case "timeout", "connect_timeout":
		if d, err := time.ParseDuration(val); err == nil {
			if d < 0 {
				return fmt.Errorf("db2: invalid timeout value: %q", val)
			}
			cfg.Timeout = d
		} else if sec, err := strconv.Atoi(val); err == nil && sec >= 0 {
			cfg.Timeout = time.Duration(sec) * time.Second
		} else {
			return fmt.Errorf("db2: invalid timeout value: %q", val)
		}
	case "block_size", "blocksize", "qryblksz":
		sz, err := strconv.Atoi(val)
		if err != nil || sz < 1024 || sz > 1048576 {
			return fmt.Errorf("db2: invalid block_size value: %q", val)
		}
		cfg.BlockSize = sz
	case "allow_plaintext_auth":
		b, err := parseBoolValue(key, val)
		if err != nil {
			return err
		}
		cfg.AllowPlaintextAuth = b
	case "security_mechanism", "secmec", "auth_mechanism", "auth":
		lowerVal := strings.ToLower(val)
		switch lowerVal {
		case "dh_encrypted_password", "encrypted_password", "encrypted-password", "secmec9", "9":
			cfg.SecurityMechanism = 9
		case "plain", "plaintext", "secmec3", "3":
			cfg.SecurityMechanism = 3
		case "kerberos", "gssapi", "7":
			cfg.SecurityMechanism = 7
		case "encrypted_kerberos", "11":
			cfg.SecurityMechanism = 11
		case "user_only", "secmec4", "4":
			cfg.SecurityMechanism = 4
		case "secmec6", "6":
			cfg.SecurityMechanism = 6
		default:
			if code, err := strconv.Atoi(val); err == nil && code > 0 {
				cfg.SecurityMechanism = uint16(code)
			} else {
				return fmt.Errorf("db2: invalid security mechanism: %q", val)
			}
		}
	case "spn", "service_principal", "server_principal":
		cfg.KerberosSPN = val
	case "realm", "krb5_realm":
		cfg.KerberosRealm = val
	case "krb5_config", "krb5_conf":
		cfg.Krb5ConfigFile = val
	case "krb5_keytab", "keytab":
		cfg.Krb5KeytabFile = val
	case "krb5_ccache", "ccache":
		cfg.Krb5CCacheFile = val
	case "client_applname", "applname", "application_name", "appname":
		cfg.ClientApplName = val
	case "client_wrkstnname", "wrkstnname", "workstation_name", "workstation":
		cfg.ClientWrkstnName = val
	case "client_userid", "client_user", "end_user":
		cfg.ClientUserid = val
	case "client_acctng", "acctng", "accounting":
		cfg.ClientAcctng = val
	case "client_corr_token", "corr_token", "correlation_token":
		cfg.ClientCorrToken = val
	default:
		return fmt.Errorf("db2: unknown DSN parameter: %q", key)
	}
	return nil
}

// toSessionConfig converts Config to the drda.SessionConfig structure.
func (c *Config) toSessionConfig() drda.SessionConfig {
	return drda.SessionConfig{
		Host:               c.Host,
		Port:               c.Port,
		Database:           c.Database,
		User:               c.User,
		Password:           c.Password,
		UseSSL:             c.UseSSL,
		SSLRootCAPath:      c.SSLRootCAPath,
		SSLClientCertPath:  c.SSLClientCertPath,
		SSLClientKeyPath:   c.SSLClientKeyPath,
		TLSConfig:          c.TLSConfig,
		Timeout:            c.Timeout,
		DialContext:        c.DialContext,
		BlockSize:          c.BlockSize,
		SecurityMechanism:  c.SecurityMechanism,
		AllowPlaintextAuth: c.AllowPlaintextAuth,
		KerberosSPN:        c.KerberosSPN,
		KerberosRealm:      c.KerberosRealm,
		Krb5ConfigFile:     c.Krb5ConfigFile,
		Krb5KeytabFile:     c.Krb5KeytabFile,
		Krb5CCacheFile:     c.Krb5CCacheFile,
		ClientApplName:     c.ClientApplName,
		ClientWrkstnName:   c.ClientWrkstnName,
		ClientUserid:       c.ClientUserid,
		ClientAcctng:       c.ClientAcctng,
		ClientCorrToken:    c.ClientCorrToken,
	}
}
