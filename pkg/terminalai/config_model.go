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

// Settings is the Chat AI payload from JumpServer terminal config.
type Settings struct {
	Enabled    *bool  `json:"CHAT_AI_ENABLED"`
	Method     string `json:"CHAT_AI_METHOD"`
	Provider   string `json:"CHAT_AI_PROVIDER"`
	BaseURL    string `json:"CHAT_AI_BASE_URL"`
	APIKey     string `json:"CHAT_AI_API_KEY"`
	Proxy      string `json:"CHAT_AI_PROXY"`
	Model      string `json:"CHAT_AI_MODEL"`
	ChatAIType string `json:"CHAT_AI_TYPE"`
	GptBaseUrl string `json:"GPT_BASE_URL"`
	GptApiKey  string `json:"GPT_API_KEY"`
	GptProxy   string `json:"GPT_PROXY"`
	GptModel   string `json:"GPT_MODEL"`
}

func NewConfig(modelConfig model.TerminalConfig) Config {
	return NewConfigFromSettings(Settings{
		Provider: modelConfig.ChatAIType,
		BaseURL:  modelConfig.GptBaseUrl,
		APIKey:   modelConfig.GptApiKey,
		Proxy:    modelConfig.GptProxy,
		Model:    modelConfig.GptModel,
	})
}

func NewConfigFromSettings(settings Settings) Config {
	apiKey := firstNonEmpty(settings.APIKey, settings.GptApiKey)
	modelName := firstNonEmpty(settings.Model, settings.GptModel)
	baseURL := firstNonEmpty(settings.BaseURL, settings.GptBaseUrl)
	proxy := firstNonEmpty(settings.Proxy, settings.GptProxy)
	name := firstNonEmpty(settings.Provider, settings.ChatAIType)
	if disabledChatAI(settings) {
		apiKey, modelName, baseURL, proxy, name = "", "", "", "", ""
	}
	return newProviderConfig(name, apiKey, baseURL, modelName, proxy)
}

func newProviderConfig(name, apiKey, baseURL, modelName, proxy string) Config {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(os.Getenv(providerEnvName))
	}
	if name == "" {
		name = provider.NameGPT
	}
	providerConfig := provider.NormalizeConfig(provider.Config{
		Name: name, APIKey: apiKey,
		BaseURL: baseURL, Model: modelName,
		Proxy: proxy, ToolCallMode: os.Getenv(toolCallEnvName),
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

func disabledChatAI(settings Settings) bool {
	if settings.Enabled != nil && !*settings.Enabled {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(settings.Method)) {
	case "iframe", "embed":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
