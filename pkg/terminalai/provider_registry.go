package terminalai

import (
	"fmt"
	"strings"
	"sync"
)

var providerRegistry = struct {
	sync.RWMutex
	factories map[string]ProviderFactory
}{
	factories: make(map[string]ProviderFactory),
}

func init() {
	mustRegisterProvider(ProviderOpenAI, newOpenAIProvider)
	mustRegisterProvider(ProviderDeepSeek, newDeepSeekProvider)
}

func RegisterProvider(name string, factory ProviderFactory) error {
	name = normalizeProviderName(name)
	if name == "" {
		return fmt.Errorf("terminal AI provider name is required")
	}
	if factory == nil {
		return fmt.Errorf("terminal AI provider %q factory is required", name)
	}
	providerRegistry.Lock()
	defer providerRegistry.Unlock()
	if _, exists := providerRegistry.factories[name]; exists {
		return fmt.Errorf("terminal AI provider %q is already registered", name)
	}
	providerRegistry.factories[name] = factory
	return nil
}

func NewProvider(config ProviderConfig) (Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" ||
		strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("terminal AI model is not configured")
	}
	config.Name = normalizeProviderName(config.Name)
	if config.Name == "" {
		config.Name = ProviderOpenAI
	}
	config.Model = strings.TrimSpace(config.Model)
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.Proxy = strings.TrimSpace(config.Proxy)
	config.ToolCallMode = strings.ToLower(strings.TrimSpace(config.ToolCallMode))
	if config.ToolCallMode == "" {
		config.ToolCallMode = ToolCallAuto
	}
	switch config.ToolCallMode {
	case ToolCallAuto, ToolCallEnabled, ToolCallDisabled:
	default:
		return nil, fmt.Errorf(
			"terminal AI tool call mode %q is invalid", config.ToolCallMode,
		)
	}

	providerRegistry.RLock()
	factory, exists := providerRegistry.factories[config.Name]
	providerRegistry.RUnlock()
	if !exists {
		return nil, fmt.Errorf(
			"terminal AI provider %q is not registered", config.Name,
		)
	}
	provider, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf(
			"initialize terminal AI provider %q: %w", config.Name, err,
		)
	}
	if provider == nil {
		return nil, fmt.Errorf(
			"terminal AI provider %q factory returned nil", config.Name,
		)
	}
	return provider, nil
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func mustRegisterProvider(name string, factory ProviderFactory) {
	if err := RegisterProvider(name, factory); err != nil {
		panic(err)
	}
}
