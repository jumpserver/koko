package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultMCPMaxSessions     = 0
	DefaultMCPMaxPending      = 64
	DefaultMCPMaxRunning      = 20
	DefaultMCPQueueTimeout    = 1200
	DefaultMCPMaxRequests     = 32
	DefaultMCPControlRequests = 4
	DefaultMCPJournalMaxSize  = 1024 // MiB
)

func (c *Config) mcpLimits() []struct {
	name    string
	value   *int
	minimum int
} {
	return []struct {
		name    string
		value   *int
		minimum int
	}{
		{"MCP_MAX_SESSIONS", &c.MCPMaxSessions, 0},
		{"MCP_MAX_PENDING", &c.MCPMaxPending, 1},
		{"MCP_MAX_RUNNING", &c.MCPMaxRunning, 1},
		{"MCP_QUEUE_TIMEOUT", &c.MCPQueueTimeout, 1},
		{"MCP_MAX_REQUESTS", &c.MCPMaxRequests, 1},
		{"MCP_CONTROL_REQUESTS", &c.MCPControlRequests, 1},
		{"MCP_JOURNAL_MAX_SIZE", &c.MCPJournalMaxSize, 16},
	}
}

func (c *Config) loadMCPLimits(values *viper.Viper) error {
	for _, limit := range c.mcpLimits() {
		if !values.IsSet(limit.name) {
			continue
		}
		value, ok := values.Get(limit.name).(int)
		if text, isString := values.Get(limit.name).(string); isString {
			var err error
			value, err = strconv.Atoi(text)
			ok = err == nil
		}
		if !ok || value < limit.minimum {
			return fmt.Errorf("%s must be an integer >= %d", limit.name, limit.minimum)
		}
		*limit.value = value
	}
	return nil
}

func (c *Config) ValidateMCP() error {
	for _, limit := range c.mcpLimits() {
		if *limit.value < limit.minimum {
			return fmt.Errorf("%s must be an integer >= %d", limit.name, limit.minimum)
		}
	}
	if c.MCPMaxRequests > int(^uint(0)>>1)-c.MCPControlRequests ||
		int64(c.MCPQueueTimeout) > int64((1<<63-1)/time.Second) ||
		int64(c.MCPJournalMaxSize) > (1<<63-1)>>20 {
		return fmt.Errorf("MCP capacity configuration exceeds supported integer ranges")
	}
	return nil
}
