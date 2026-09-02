package sessiontools

import (
	"strings"
	"unicode/utf8"

	"github.com/jumpserver-dev/sdk-go/model"
)

type DatabaseConfig struct {
	Protocol         string
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

type commandProposal struct {
	RiskLevel          int
	RiskReason         string
	ApprovalRequired   bool
	BackgroundEligible bool
}

func normalizeRisk(level int, reason string) (int, string) {
	if level < 1 || level > 4 {
		level = 2
	}
	if strings.TrimSpace(reason) == "" {
		reason = "risk classified by the connection executor"
	}
	return level, reason
}

func raiseRisk(level int, reason string, minimum int, cause string) (int, string) {
	if level < minimum {
		return minimum, cause
	}
	return level, reason
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
