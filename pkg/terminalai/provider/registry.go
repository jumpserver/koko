package provider

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

var registry = struct {
	sync.RWMutex
	factories map[string]Factory
}{factories: make(map[string]Factory)}

func init() {
	mustRegister(NameGPT, newCompatibleProvider)
	mustRegister(NameOpenAI, newOpenAIProvider)
	mustRegister(NameDeepSeek, newDeepSeekProvider)
	mustRegister("deepseek", newDeepSeekProvider)
}

func Register(name string, factory Factory) error {
	name = NormalizeName(name)
	if name == "" {
		return fmt.Errorf("terminal AI provider name is required")
	}
	if factory == nil {
		return fmt.Errorf("terminal AI provider %q factory is required", name)
	}
	registry.Lock()
	defer registry.Unlock()
	if _, exists := registry.factories[name]; exists {
		return fmt.Errorf("terminal AI provider %q is already registered", name)
	}
	registry.factories[name] = factory
	return nil
}

func New(config Config) (Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("terminal AI model is not configured")
	}
	config = NormalizeConfig(config)
	registry.RLock()
	factory, exists := registry.factories[config.Name]
	registry.RUnlock()
	if !exists {
		factory = newCompatibleProvider
	}
	result, err := factory(config)
	if err != nil {
		return nil, fmt.Errorf("initialize terminal AI provider %q: %w", config.Name, err)
	}
	if result == nil {
		return nil, fmt.Errorf("terminal AI provider %q factory returned nil", config.Name)
	}
	return result, nil
}

func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func NormalizeConfig(config Config) Config {
	config.Name = NormalizeName(config.Name)
	if config.Name == "" {
		config.Name = NameGPT
	}
	config.Model = strings.TrimSpace(config.Model)
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	if isDeepSeekBaseURL(config.BaseURL) {
		config.Name = NameDeepSeek
	}
	config.Proxy = strings.TrimSpace(config.Proxy)
	config.ToolCallMode = strings.ToLower(strings.TrimSpace(config.ToolCallMode))
	if config.ToolCallMode == "" {
		config.ToolCallMode = ToolCallAuto
	}
	switch config.ToolCallMode {
	case ToolCallAuto, ToolCallEnabled, ToolCallDisabled:
	default:
		config.ToolCallMode = ToolCallAuto
	}
	config.ReasoningMode = strings.ToLower(strings.TrimSpace(config.ReasoningMode))
	if config.ReasoningMode == "" {
		config.ReasoningMode = ReasoningAuto
	}
	switch config.ReasoningMode {
	case ReasoningOff, ReasoningAuto, ReasoningOn:
	default:
		config.ReasoningMode = ReasoningAuto
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.ContextSoftLimitPercent <= 0 || config.ContextSoftLimitPercent >= 100 {
		config.ContextSoftLimitPercent = 80
	}
	contextTokens, outputTokens := ModelLimits(config.Name, config.Model)
	if config.ContextWindowTokens <= 0 {
		config.ContextWindowTokens = contextTokens
	}
	if config.MaxOutputTokens <= 0 {
		config.MaxOutputTokens = outputTokens
	}
	return config
}

func isDeepSeekBaseURL(value string) bool {
	baseURL, err := url.Parse(value)
	return err == nil && strings.EqualFold(baseURL.Hostname(), "api.deepseek.com")
}

func ModelLimits(name, model string) (int64, int64) {
	name = NormalizeName(name)
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case name == NameDeepSeek || name == "deepseek":
		if strings.Contains(model, "v4") {
			return 1_000_000, 384_000
		}
		if model == "deepseek-reasoner" {
			return 65_536, 32_768
		}
		return 65_536, 8_192
	case strings.HasPrefix(model, "gpt-5.6"),
		strings.HasPrefix(model, "gpt-5.5"):
		return 1_050_000, 128_000
	case strings.HasPrefix(model, "gpt-5.4-mini"),
		strings.HasPrefix(model, "gpt-5.4-nano"):
		return 400_000, 128_000
	case strings.HasPrefix(model, "gpt-5.4"):
		return 1_050_000, 128_000
	case strings.HasPrefix(model, "gpt-5.3"),
		strings.HasPrefix(model, "gpt-5.2"),
		strings.HasPrefix(model, "gpt-5.1"),
		strings.HasPrefix(model, "gpt-5"):
		return 400_000, 128_000
	case strings.HasPrefix(model, "gpt-4.1"):
		return 1_000_000, 32_768
	case strings.HasPrefix(model, "gpt-4o"):
		return 128_000, 16_384
	default:
		return 128_000, 8_192
	}
}

func mustRegister(name string, factory Factory) {
	if err := Register(name, factory); err != nil {
		panic(err)
	}
}
