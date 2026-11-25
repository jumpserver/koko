package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDefaultName(t *testing.T) {
	hostname, _ := os.Hostname()
	for _, tc := range []struct {
		name       string
		serverHost string
		wantPrefix string
	}{
		{name: "unset", wantPrefix: "[Kael]-" + hostname + "-"},
		{name: "empty", wantPrefix: "[Kael]--" + hostname + "-"},
		{name: "set", serverHost: "server", wantPrefix: "[Kael]-server-" + hostname + "-"},
		{name: "long unicode", serverHost: strings.Repeat("界", 128), wantPrefix: "[Kael]-" + strings.Repeat("界", 57)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SERVER_HOSTNAME", tc.serverHost)
			if tc.name == "unset" {
				if err := os.Unsetenv("SERVER_HOSTNAME"); err != nil {
					t.Fatal(err)
				}
			}
			name := defaultName()
			wantLength := utf8.RuneCountInString(tc.wantPrefix) + 7
			if wantLength > 128 {
				wantLength = 128
				tc.wantPrefix = string([]rune(tc.wantPrefix)[:64])
			}
			if tc.name == "long unicode" {
				wantLength = 128
				tail := []rune(tc.serverHost + "-" + hostname + "-")
				if !strings.HasSuffix(name[:len(name)-7], string(tail[len(tail)-57:])) {
					t.Fatalf("truncated name lost its tail: %q", name)
				}
			}
			if !utf8.ValidString(name) || utf8.RuneCountInString(name) != wantLength || !strings.HasPrefix(name, tc.wantPrefix) {
				t.Fatalf("unexpected default name: %q", name)
			}
			for _, char := range name[len(name)-7:] {
				if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z') {
					t.Fatalf("random suffix contains a non-letter: %q", name)
				}
			}
		})
	}
}

func TestLoadFlatKokoStyleEnvironment(t *testing.T) {
	t.Setenv("CORE_HOST", "https://core.example.test")
	t.Setenv("BOOTSTRAP_TOKEN", "bootstrap-secret")
	t.Setenv("NAME", "kael-test")
	t.Setenv("BIND_HOST", "127.0.0.1")
	t.Setenv("HTTPD_PORT", "9083")
	t.Setenv("IGNORE_VERIFY_CERTS", "true")
	t.Setenv("HTTP_REQUEST_TIMEOUT", "45")

	settings, err := Load(writeConfig(t, "{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if settings.CoreHost != "https://core.example.test" || settings.BootstrapToken != "bootstrap-secret" || settings.Name != "kael-test" || settings.BindHost != "127.0.0.1" || settings.HTTPPort != 9083 || !settings.IgnoreVerifyCerts || settings.HTTPRequestTimeout != 45*time.Second {
		t.Fatalf("flat environment was not loaded: %#v", settings)
	}
}

func TestLoadDiscoversFlatConfigAndDerivesDataPaths(t *testing.T) {
	dir := t.TempDir()
	content := "CORE_HOST: https://core.example.test\nNAME: kael-test\nHTTPD_PORT: 9083\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KAEL_CONFIG_FILE", "")
	t.Setenv("CORE_HOST", "")
	t.Setenv("NAME", "")
	t.Chdir(dir)

	settings, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if settings.CoreHost != "https://core.example.test" || settings.Name != "kael-test" || settings.HTTPPort != 9083 || settings.AccessKeyFilePath != filepath.Join(dir, "data", "keys", ".access_key") || settings.ArtifactFolderPath != filepath.Join(dir, "data", "artifacts") || settings.RuntimeDataFolderPath != filepath.Join(dir, "data") {
		t.Fatalf("unexpected flat config: %#v", settings)
	}
}

func TestLoadUsesExplicitFlatConfig(t *testing.T) {
	path := writeConfig(t, "CORE_HOST: https://core.example.test\n")
	t.Setenv("KAEL_CONFIG_FILE", path)
	t.Setenv("CORE_HOST", "")

	settings, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if settings.CoreHost != "https://core.example.test" {
		t.Fatalf("unexpected Core host: %s", settings.CoreHost)
	}
}

func TestLoadRejectsNestedConfig(t *testing.T) {
	if _, err := Load(writeConfig(t, "core:\n  url: https://core.example.test\n")); err == nil {
		t.Fatal("nested config was accepted")
	}
}

func TestTerminalHistoryRetentionConfig(t *testing.T) {
	settings, err := Load(writeConfig(t, "{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if settings.TerminalAIKeepDays != 7 || settings.TerminalAIMaxBytes != 1<<30 || settings.TerminalAIMinFreeBytes != 1<<30 {
		t.Fatal("unexpected terminal history defaults")
	}
	t.Setenv("TERMINAL_AI_KEEP_DAYS", "14")
	t.Setenv("TERMINAL_AI_MAX_BYTES", "536870912")
	t.Setenv("TERMINAL_AI_MIN_FREE_BYTES", "134217728")
	settings, err = Load(writeConfig(t, "{}\n"))
	if err != nil || settings.TerminalAIKeepDays != 14 || settings.TerminalAIMaxBytes != 512<<20 || settings.TerminalAIMinFreeBytes != 128<<20 {
		t.Fatalf("terminal history settings were not loaded: %v", err)
	}
	for _, key := range []string{"TERMINAL_AI_KEEP_DAYS", "TERMINAL_AI_MAX_BYTES", "TERMINAL_AI_MIN_FREE_BYTES"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "0")
			if _, err := Load(writeConfig(t, "{}\n")); err == nil {
				t.Fatal("unbounded terminal history setting was accepted")
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
