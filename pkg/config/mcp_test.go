package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestMCPCapacityConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	conf := getDefaultConfig()
	if err := conf.ValidateMCP(); err != nil {
		t.Fatal(err)
	}
	if conf.MCPMaxSessions != 0 {
		t.Fatal("MCP sessions should have no count limit by default")
	}
	t.Setenv("MCP_MAX_SESSIONS", "1024")
	environment := viper.New()
	environment.AutomaticEnv()
	if err := conf.loadMCPLimits(environment); err != nil || conf.MCPMaxSessions != 1024 {
		t.Fatal("MCP capacity environment override was not loaded", err)
	}
	file := viper.New()
	file.SetConfigType("yaml")
	if err := file.ReadConfig(strings.NewReader("MCP_MAX_PENDING: 512\nMCP_MAX_RUNNING: 128\nMCP_QUEUE_TIMEOUT: 30\n")); err != nil {
		t.Fatal(err)
	}
	if err := conf.loadMCPLimits(file); err != nil || conf.ValidateMCP() != nil || conf.MCPMaxPending != 512 || conf.MCPMaxRunning != 128 || conf.MCPQueueTimeout != 30 {
		t.Fatal("MCP capacity file override was not loaded", err)
	}
	file.Set("MCP_MAX_SESSIONS", 0)
	if err := conf.loadMCPLimits(file); err != nil || conf.MCPMaxSessions != 0 {
		t.Fatal("zero did not disable the MCP session count limit", err)
	}
	for _, value := range []any{0, -1, true, 1.5, "unlimited"} {
		file.Set("MCP_MAX_PENDING", value)
		if err := conf.loadMCPLimits(file); err == nil {
			t.Fatalf("invalid MCP capacity accepted: %v", value)
		}
	}
}
