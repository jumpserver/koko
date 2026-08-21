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

func TestConfigEnablesAuditExplicitly(t *testing.T) {
	previous := appconfig.GlobalConfig
	appconfig.GlobalConfig = &appconfig.Config{TerminalAIAuditEnabled: true}
	t.Cleanup(func() { appconfig.GlobalConfig = previous })

	if config := NewConfig(model.TerminalConfig{}); !config.AuditEnabled {
		t.Fatal("Terminal AI audit should be enabled by configuration")
	}
}
