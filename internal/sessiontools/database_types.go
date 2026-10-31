package sessiontools

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver-dev/sdk-go/model"
)

type DatabaseConfig struct {
	Protocol         string
	PlatformType     string
	TenantMode       string
	Host             string
	Port             int
	ServerName       string
	Username         string
	Password         string
	Database         string
	UseSSL           bool
	PGSSLMode        string
	CACert           string
	ClientCert       string
	ClientKey        string
	AllowInvalidCert bool
	Encrypt          bool
	DisableEncrypt   bool
	ClusterMode      bool
	AuthSource       string
	ConnectionOpts   string
	ProxyURL         string
	DataMaskingRules []model.DataMaskingRule
	// DialContext keeps database discovery traffic on the authorized gateway.
	DialContext func(context.Context, string, string) (net.Conn, error)
	agent       bool
}

type databaseDialer func(context.Context, string, string) (net.Conn, error)

func (d databaseDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d(ctx, network, address)
}

func databaseTLSDialer(dial databaseDialer, config *tls.Config) databaseDialer {
	if dial == nil || config == nil {
		return dial
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		secured := tls.Client(conn, config)
		if err := secured.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return secured, nil
	}
}

type SQLSchemaLookupRequest struct {
	Tables []string `json:"tables"`
	Query  string   `json:"query"`
}

type SQLSchemaColumn struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
	Ordinal  int     `json:"-"`
}

type SQLTableSchema struct {
	Database string            `json:"database"`
	Schema   string            `json:"schema,omitempty"`
	Table    string            `json:"table"`
	Columns  []SQLSchemaColumn `json:"columns"`
}

type SQLSchemaLookupResult struct {
	Database  string           `json:"database"`
	Matches   []string         `json:"matches,omitempty"`
	Tables    []SQLTableSchema `json:"tables"`
	Truncated bool             `json:"truncated,omitempty"`
}

func headTailPrompt(value string, limit int) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	const marker = "\n...[truncated]...\n"
	if limit <= len(marker) {
		value = value[len(value)-limit:]
		for len(value) > 0 && !utf8.ValidString(value) {
			value = value[1:]
		}
		return value
	}
	head := (limit - len(marker)) / 2
	tail := limit - len(marker) - head
	for head > 0 && !utf8.ValidString(value[:head]) {
		head--
	}
	start := len(value) - tail
	for start < len(value) && !utf8.ValidString(value[start:]) {
		start++
	}
	return value[:head] + marker + value[start:]
}
