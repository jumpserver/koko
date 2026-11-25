package config

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	CodexBinary            string
	Name                   string
	CoreHost               string
	BootstrapToken         string
	BindHost               string
	HTTPPort               int
	IgnoreVerifyCerts      bool
	HTTPRequestTimeout     time.Duration
	ToolResultTimeout      time.Duration
	LogLevel               string
	LogDirPath             string
	AllowedOrigins         []string
	TrustForwardedHeaders  bool
	AccessKeyFilePath      string
	ArtifactFolderPath     string
	RuntimeDataFolderPath  string
	TerminalAIKeepDays     int
	TerminalAIMaxBytes     int64
	TerminalAIMinFreeBytes int64
	PlatformGatewayEnabled bool
	PlatformCACert         string
	PlatformClientCert     string
	PlatformClientKey      string
	PlatformAllowedMethods map[string]bool
	PlatformTimeout        time.Duration
	PlatformMaxResponse    int64
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	setDefaults(v)
	if err := readConfig(v, path); err != nil {
		return Config{}, err
	}
	for _, section := range []string{"core", "identity", "artifact", "runtime", "platform_gateway"} {
		if v.InConfig(section) {
			return Config{}, fmt.Errorf("nested config section %q is not supported; use flat Koko-style keys", section)
		}
	}
	root, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("resolve working directory: %w", err)
	}
	dataFolder := filepath.Join(root, "data")
	result := Config{
		CodexBinary:            strings.TrimSpace(v.GetString("CODEX_BINARY")),
		Name:                   strings.TrimSpace(v.GetString("NAME")),
		CoreHost:               strings.TrimRight(strings.TrimSpace(v.GetString("CORE_HOST")), "/"),
		BootstrapToken:         strings.TrimSpace(v.GetString("BOOTSTRAP_TOKEN")),
		BindHost:               strings.TrimSpace(v.GetString("BIND_HOST")),
		HTTPPort:               v.GetInt("HTTPD_PORT"),
		IgnoreVerifyCerts:      v.GetBool("IGNORE_VERIFY_CERTS"),
		HTTPRequestTimeout:     time.Duration(v.GetInt("HTTP_REQUEST_TIMEOUT")) * time.Second,
		ToolResultTimeout:      v.GetDuration("TOOL_RESULT_TIMEOUT"),
		LogLevel:               strings.ToUpper(strings.TrimSpace(v.GetString("LOG_LEVEL"))),
		LogDirPath:             filepath.Join(dataFolder, "logs"),
		AllowedOrigins:         v.GetStringSlice("ALLOWED_ORIGINS"),
		TrustForwardedHeaders:  v.GetBool("TRUST_FORWARDED_HEADERS"),
		AccessKeyFilePath:      filepath.Join(dataFolder, "keys", ".access_key"),
		ArtifactFolderPath:     filepath.Join(dataFolder, "artifacts"),
		RuntimeDataFolderPath:  dataFolder,
		TerminalAIKeepDays:     v.GetInt("TERMINAL_AI_KEEP_DAYS"),
		TerminalAIMaxBytes:     v.GetInt64("TERMINAL_AI_MAX_BYTES"),
		TerminalAIMinFreeBytes: v.GetInt64("TERMINAL_AI_MIN_FREE_BYTES"),
		PlatformGatewayEnabled: v.GetBool("PLATFORM_GATEWAY_ENABLED"),
		PlatformCACert:         v.GetString("PLATFORM_CA_CERT"),
		PlatformClientCert:     v.GetString("PLATFORM_CLIENT_CERT"),
		PlatformClientKey:      v.GetString("PLATFORM_CLIENT_KEY"),
		PlatformAllowedMethods: make(map[string]bool),
		PlatformTimeout:        v.GetDuration("PLATFORM_TIMEOUT"),
		PlatformMaxResponse:    v.GetInt64("PLATFORM_MAX_RESPONSE_BYTES"),
	}
	for _, method := range v.GetStringSlice("PLATFORM_ALLOWED_METHODS") {
		result.PlatformAllowedMethods[strings.ToUpper(strings.TrimSpace(method))] = true
	}
	if err = result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func readConfig(v *viper.Viper, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("KAEL_CONFIG_FILE"))
	}
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("read config %q: %w", path, err)
		}
		return nil
	}
	for _, candidate := range []string{"config.yml", "config.yaml", ".config.yml", ".config.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			v.SetConfigFile(candidate)
			if err = v.ReadInConfig(); err != nil {
				return fmt.Errorf("read config %q: %w", candidate, err)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect config %q: %w", candidate, err)
		}
	}
	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("CODEX_BINARY", "codex")
	v.SetDefault("NAME", defaultName())
	v.SetDefault("CORE_HOST", "http://127.0.0.1:8080")
	v.SetDefault("BIND_HOST", "0.0.0.0")
	v.SetDefault("HTTPD_PORT", 8083)
	v.SetDefault("HTTP_REQUEST_TIMEOUT", 30)
	v.SetDefault("TOOL_RESULT_TIMEOUT", "45s")
	v.SetDefault("LOG_LEVEL", "INFO")
	v.SetDefault("TERMINAL_AI_KEEP_DAYS", 7)
	v.SetDefault("TERMINAL_AI_MAX_BYTES", int64(1<<30))
	v.SetDefault("TERMINAL_AI_MIN_FREE_BYTES", int64(1<<30))
	v.SetDefault("PLATFORM_GATEWAY_ENABLED", true)
	v.SetDefault("PLATFORM_ALLOWED_METHODS", []string{"GET", "POST", "PUT", "PATCH", "DELETE"})
	v.SetDefault("PLATFORM_TIMEOUT", "15s")
	v.SetDefault("PLATFORM_MAX_RESPONSE_BYTES", 1024*1024)
}

func defaultName() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	suffix := make([]byte, 7)
	for i := range suffix {
		index, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		suffix[i] = letters[index.Int64()]
	}
	hostname, _ := os.Hostname()
	hostname = fmt.Sprintf("%s-%s", hostname, suffix)
	if serverHostname, ok := os.LookupEnv("SERVER_HOSTNAME"); ok {
		hostname = fmt.Sprintf("%s-%s", serverHostname, hostname)
	}
	value := []rune("[Kael]-" + hostname)
	if len(value) > 128 {
		value = append(value[:64], value[len(value)-64:]...)
	}
	return string(value)
}

func (c Config) Validate() error {
	if c.ToolResultTimeout < time.Second || c.ToolResultTimeout > 10*time.Minute {
		return fmt.Errorf("TOOL_RESULT_TIMEOUT must be between 1s and 10m")
	}
	if c.Name == "" || c.BindHost == "" || c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("component name or listen address is invalid")
	}
	if !validEndpoint(c.CoreHost) {
		return fmt.Errorf("CORE_HOST must be an HTTP/HTTPS endpoint without embedded credentials")
	}
	if c.HTTPRequestTimeout < time.Second || c.HTTPRequestTimeout > 5*time.Minute {
		return fmt.Errorf("HTTP_REQUEST_TIMEOUT must be between 1 and 300 seconds")
	}
	for _, origin := range c.AllowedOrigins {
		if !validOrigin(origin) {
			return fmt.Errorf("ALLOWED_ORIGINS contains an invalid HTTP/HTTPS origin")
		}
	}
	if c.TerminalAIKeepDays < 1 || c.TerminalAIKeepDays > 3650 || c.TerminalAIMaxBytes < 256<<20 || c.TerminalAIMinFreeBytes < 64<<20 {
		return fmt.Errorf("TERMINAL_AI_KEEP_DAYS must be 1..3650, TERMINAL_AI_MAX_BYTES at least 256 MiB, and TERMINAL_AI_MIN_FREE_BYTES at least 64 MiB")
	}
	if !c.PlatformGatewayEnabled {
		return fmt.Errorf("PLATFORM_GATEWAY_ENABLED must be true because the default general assistant requires the Platform Gateway")
	}
	if c.PlatformTimeout <= 0 || c.PlatformMaxResponse < 1 {
		return fmt.Errorf("Platform Gateway configuration is incomplete")
	}
	if (c.PlatformClientCert == "") != (c.PlatformClientKey == "") {
		return fmt.Errorf("PLATFORM_CLIENT_CERT and PLATFORM_CLIENT_KEY must be configured together")
	}
	return nil
}

func validEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validOrigin(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil && (parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}
