// Package standalone resolves public URL selectors to private operator-managed
// database configurations.
package standalone

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/jumpserver/koko/pkg/chen/core"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

const maxConfigFileSize = 1 << 20

type LoadOptions struct{ AllowInsecurePermissions bool }

type Config struct {
	Version  int             `yaml:"version" json:"version"`
	Backends []BackendConfig `yaml:"backends" json:"-"`
}
type BackendConfig struct {
	Selector         string `yaml:"selector" json:"selector"`
	Name             string `yaml:"name" json:"name"`
	Protocol         string `yaml:"protocol" json:"protocol"`
	Host             string `yaml:"host" json:"-"`
	Port             int    `yaml:"port" json:"-"`
	User             string `yaml:"user" json:"-"`
	Password         string `yaml:"password" json:"-"`
	Database         string `yaml:"database" json:"-"`
	Schema           string `yaml:"schema" json:"-"`
	UseSYSDBA        bool   `yaml:"use_sysdba" json:"-"`
	TenantMode       string `yaml:"tenant_mode" json:"-"`
	Transport        string `yaml:"transport" json:"-"`
	UseSSL           bool   `yaml:"use_ssl" json:"-"`
	CACert           string `yaml:"ca_cert" json:"-"`
	ClientCert       string `yaml:"client_cert" json:"-"`
	ClientKey        string `yaml:"client_key" json:"-"`
	ServerName       string `yaml:"server_name" json:"-"`
	SSLMode          string `yaml:"ssl_mode" json:"-"`
	AllowInvalidCert bool   `yaml:"allow_invalid_cert" json:"-"`
}

func (c Config) String() string          { return "standalone config (credentials redacted)" }
func (c Config) GoString() string        { return c.String() }
func (b BackendConfig) String() string   { return "standalone backend (credentials redacted)" }
func (b BackendConfig) GoString() string { return b.String() }

// Load rejects ambiguous or exposed configuration before a listener starts.
// YAML diagnostics intentionally omit raw values: invalid YAML can contain
// passwords in the wrong field, so its decoder error must not reach logs.
func Load(path string, options LoadOptions) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("standalone_config_path_required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("standalone_config_unreadable")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("standalone_config_must_be_regular_file")
	}
	if info.Size() > maxConfigFileSize {
		return nil, errors.New("standalone_config_exceeds_1_mib")
	}
	if !options.AllowInsecurePermissions && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("standalone_config_permissions_require_0600")
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, errors.New("standalone_config_unreadable")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("standalone_config_changed_while_opening")
	}
	if !options.AllowInsecurePermissions && opened.Mode().Perm()&0077 != 0 {
		return nil, errors.New("standalone_config_permissions_require_0600")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigFileSize+1))
	if err != nil {
		return nil, errors.New("standalone_config_unreadable")
	}
	if len(data) > maxConfigFileSize {
		return nil, errors.New("standalone_config_exceeds_1_mib")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if decoder.Decode(&cfg) != nil {
		return nil, errors.New("standalone_config_invalid_yaml")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("standalone_config_requires_one_yaml_document")
	}
	return normalize(cfg)
}

func normalize(cfg Config) (*Config, error) {
	if cfg.Version != 1 {
		return nil, errors.New("standalone_config_version_must_be_1")
	}
	if len(cfg.Backends) == 0 || len(cfg.Backends) > 256 {
		return nil, errors.New("standalone_config_requires_1_to_256_backends")
	}
	result := &Config{Version: 1, Backends: make([]BackendConfig, 0, len(cfg.Backends))}
	selectors := make(map[string]bool)
	for i, item := range cfg.Backends {
		invalid := func(field, reason string) (*Config, error) {
			return nil, fmt.Errorf("backends[%d].%s: %s", i, field, reason)
		}
		if !validSelector(item.Selector) {
			return invalid("selector", "must contain 1 to 128 URL-safe letters, digits, dots, underscores or hyphens")
		}
		if selectors[item.Selector] {
			return invalid("selector", "must be globally unique")
		}
		selectors[item.Selector] = true
		if item.Name == "" {
			item.Name = item.Selector
		}
		if len(item.Name) > 256 || strings.ContainsFunc(item.Name, unicode.IsControl) {
			return invalid("name", "must be a short display name without control characters")
		}
		dialect, err := core.CanonicalDialect(item.Protocol, item.TenantMode)
		if err != nil {
			return invalid("protocol", err.Error())
		}
		item.Protocol = dialect
		if item.UseSYSDBA && dialect != "oracle" {
			return invalid("use_sysdba", "is supported only for oracle")
		}
		item.Transport = strings.ToLower(strings.TrimSpace(item.Transport))
		if item.Transport != "" && (dialect != "clickhouse" || item.Transport != "http" && item.Transport != "native") {
			return invalid("transport", "must be http or native and is supported only for clickhouse")
		}
		if strings.TrimSpace(item.Host) != item.Host || item.Host == "" || strings.ContainsAny(item.Host, "/\\\x00") || strings.ContainsFunc(item.Host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return invalid("host", "must be a TCP host without a URL or port")
		}
		if strings.HasPrefix(item.Host, "[") && strings.HasSuffix(item.Host, "]") {
			item.Host = strings.TrimSuffix(strings.TrimPrefix(item.Host, "["), "]")
		}
		if strings.Contains(item.Host, ":") && net.ParseIP(item.Host) == nil {
			return invalid("host", "must not include a port")
		}
		if strings.ContainsAny(item.Host, "[]") {
			return invalid("host", "must contain a valid host or IP address")
		}
		if item.Port < 1 || item.Port > 65535 {
			return invalid("port", "must be between 1 and 65535")
		}
		if strings.TrimSpace(item.User) == "" || strings.ContainsRune(item.User, 0) {
			return invalid("user", "is required and must not contain NUL")
		}
		if strings.ContainsRune(item.Database, 0) || strings.ContainsRune(item.Schema, 0) || strings.ContainsRune(item.Password, 0) {
			return invalid("database_or_password", "must not contain NUL")
		}
		if (item.ClientCert == "") != (item.ClientKey == "") {
			return invalid("client_cert", "must be configured together with client_key")
		}
		item.SSLMode = strings.ToLower(strings.TrimSpace(item.SSLMode))
		if !item.UseSSL && (item.CACert != "" || item.ClientCert != "" || item.ClientKey != "" || item.ServerName != "" || item.AllowInvalidCert || item.SSLMode != "") {
			return invalid("use_ssl", "TLS settings require use_ssl: true")
		}
		if item.Protocol != "postgresql" && item.SSLMode != "" {
			return invalid("ssl_mode", "is supported only for postgresql")
		}
		switch item.SSLMode {
		case "", "require", "verify-ca", "verify-full":
		default:
			return invalid("ssl_mode", "must be require, verify-ca or verify-full")
		}
		if item.AllowInvalidCert && (item.SSLMode == "verify-ca" || item.SSLMode == "verify-full") {
			return invalid("allow_invalid_cert", "conflicts with certificate verification")
		}
		if item.CACert != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(item.CACert)) {
			return invalid("ca_cert", "must contain PEM certificates")
		}
		if item.ClientCert != "" {
			if _, err := tls.X509KeyPair([]byte(item.ClientCert), []byte(item.ClientKey)); err != nil {
				return invalid("client_cert", "must match its PEM private key")
			}
		}
		if strings.ContainsAny(item.ServerName, "/\\\x00") || strings.ContainsFunc(item.ServerName, unicode.IsSpace) {
			return invalid("server_name", "must be a TLS server name")
		}
		if strings.Contains(item.ServerName, ":") && net.ParseIP(item.ServerName) == nil {
			return invalid("server_name", "must not include a port")
		}
		if item.UseSSL && item.ServerName == "" {
			item.ServerName = item.Host
		}
		result.Backends = append(result.Backends, item)
	}
	return result, nil
}

func validSelector(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for i := range value {
		b := value[i]
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-') {
			return false
		}
	}
	return true
}
