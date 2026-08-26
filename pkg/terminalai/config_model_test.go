package terminalai

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	appconfig "github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

func TestConfigPrefersChatAIType(t *testing.T) {
	t.Setenv(providerEnvName, provider.NameOpenAI)
	config := NewConfig(model.TerminalConfig{ChatAIType: provider.NameDeepSeek})
	if config.Provider.Name != provider.NameDeepSeek {
		t.Fatalf("provider = %q, want ChatAIType", config.Provider.Name)
	}
	config = NewConfig(model.TerminalConfig{})
	if config.Provider.Name != provider.NameOpenAI {
		t.Fatalf("provider = %q, want environment fallback", config.Provider.Name)
	}
	if config.Provider.Store || config.Provider.ReasoningMode != provider.ReasoningAuto ||
		config.MemorySessions != 10 || config.AuditEnabled {
		t.Fatalf("unexpected Terminal AI defaults: %#v", config)
	}
}

func TestConfigPrefersNewChatAISettings(t *testing.T) {
	enabled := true
	config := NewConfigFromSettings(Settings{
		Enabled:   &enabled,
		Provider:  "openai_compatible",
		BaseURL:   "https://api.example.com/v1",
		APIKey:    "new-key",
		Model:     "gpt-4.1",
		GptApiKey: "legacy-key",
		GptModel:  "legacy-model",
	})
	if config.Provider.Name != "openai_compatible" || config.Provider.APIKey != "new-key" ||
		config.Provider.Model != "gpt-4.1" || config.Provider.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("unexpected new Chat AI config: %#v", config.Provider)
	}
}

func TestConfigDisablesChatAI(t *testing.T) {
	enabled := false
	config := NewConfigFromSettings(Settings{
		Enabled: &enabled,
		APIKey:  "key",
		Model:   "gpt-4.1",
	})
	if config.Provider.APIKey != "" || config.Provider.Model != "" {
		t.Fatalf("disabled Chat AI should not expose credentials: %#v", config.Provider)
	}
	config = NewConfigFromSettings(Settings{
		Method: "iframe",
		APIKey: "key",
		Model:  "gpt-4.1",
	})
	if config.Provider.APIKey != "" || config.Provider.Model != "" {
		t.Fatalf("iframe Chat AI should not expose credentials: %#v", config.Provider)
	}
}

func TestConfigEnablesAuditExplicitly(t *testing.T) {
	previous := appconfig.GlobalConfig
	appconfig.GlobalConfig = &appconfig.Config{TerminalAIAuditEnabled: true}
	t.Cleanup(func() { appconfig.GlobalConfig = previous })

	if config := NewConfig(model.TerminalConfig{}); !config.AuditEnabled {
		t.Fatal("Terminal AI audit should be enabled by configuration")
	}
}
