package standalone

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumpserver/koko/pkg/chen/core"
)

func TestPrivateConfigAndLocalSession(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "backends.yml")
	config := "version: 1\nbackends:\n  - {selector: local, protocol: pg, host: localhost, port: 5432, user: test, password: private-password}"
	if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configFile, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configFile, []string{"local"}); err == nil {
		t.Fatal("accepted a publicly readable backend config")
	}
	if err := os.Chmod(configFile, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configFile, []string{"local"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	provider, err := NewProvider(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	g, err := provider.Open(ctx, "local", "127.0.0.1:1234")
	if err != nil || g.Config.Dialect != "postgresql" {
		t.Fatalf("open local session: %v", err)
	}
	if _, err := provider.AuthorizeCommand(ctx, g, core.AuthorizationRequest{SQL: "SELECT 1"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Record(ctx, g, core.AuditRecord{ID: "query-1", SQL: "SELECT 'private-password'"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := provider.Authorize(ctx, g, "SELECT 1"); !errors.Is(err, core.ErrClosed) {
		t.Fatalf("authorization after close: %v", err)
	}
	records, err := os.ReadDir(dir)
	if err != nil || len(records) != 3 {
		t.Fatalf("expected open, query and close audit records: %v (%d records)", err, len(records))
	}
	data, err := os.ReadFile(filepath.Join(dir, records[1].Name()))
	if err != nil || !strings.Contains(string(data), "SELECT '[redacted]'") || strings.Contains(string(data), "private-password") {
		t.Fatalf("query audit did not redact connection credentials: %v", err)
	}
}

func TestSelectedBackends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backends.yml")
	data := "version: 1\nbackends:\n"
	for _, name := range []string{"first", "second", "third"} {
		data += "  - {selector: " + name + ", protocol: pg, host: localhost, port: 5432, user: test}\n"
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{nil, {""}, {"missing"}, {"first", "first"}} {
		if _, err := Load(path, names); err == nil {
			t.Fatalf("accepted selectors %q", names)
		}
	}
	selected := []string{"second", "first"}
	cfg, err := Load(path, selected)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewProvider(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range selected {
		g, err := provider.Open(t.Context(), name, "")
		if err != nil || g.AssetID != name {
			t.Fatalf("open selected backend %q: %v", name, err)
		}
	}
	if _, err := provider.Open(t.Context(), "third", ""); err == nil || err.Error() != "selector_not_found" {
		t.Fatalf("open unselected backend: %v", err)
	}
	invalid := strings.Replace(data, "third, protocol: pg", "third, protocol: unsupported", 1)
	if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, selected); err == nil {
		t.Fatal("ignored invalid configuration for an unselected backend")
	}
}
