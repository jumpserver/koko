package terminalai

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
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
		config.MemorySessions != 10 {
		t.Fatalf("unexpected Terminal AI defaults: %#v", config)
	}
}
