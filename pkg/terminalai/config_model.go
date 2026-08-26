package terminalai

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	appconfig "github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

const (
	providerEnvName = "TERMINAL_AI_PROVIDER"
	toolCallEnvName = "TERMINAL_AI_TOOL_CALL"
)

type Config struct {
	Provider               provider.Config
	AuditEnabled           bool
	MemoryRoot             string
	MemorySessions         int
	MaxModelRequests       int
	HistoryCheckpointBytes int
}

func NewConfig(modelConfig model.TerminalConfig) Config {
	name := strings.TrimSpace(modelConfig.ChatAIType)
	if name == "" {
		name = strings.TrimSpace(os.Getenv(providerEnvName))
	}
	if name == "" {
		name = provider.NameGPT
	}
	providerConfig := provider.NormalizeConfig(provider.Config{
		Name: name, APIKey: modelConfig.GptApiKey,
		BaseURL: modelConfig.GptBaseUrl, Model: modelConfig.GptModel,
		Proxy: modelConfig.GptProxy, ToolCallMode: os.Getenv(toolCallEnvName),
		ReasoningMode: provider.ReasoningAuto,
		Store:         false, NativeCompaction: false,
		ContextSoftLimitPercent: 80, RequestTimeout: 5 * time.Minute,
	})
	return Config{
		Provider:     providerConfig,
		AuditEnabled: appconfig.GetConf().TerminalAIAuditEnabled,
		MemoryRoot: filepath.Join(
			appconfig.GetConf().DataFolderPath, "terminal_ai", "memory",
		),
		MemorySessions: 10, MaxModelRequests: 30,
		HistoryCheckpointBytes: 32 * 1024,
	}
}
