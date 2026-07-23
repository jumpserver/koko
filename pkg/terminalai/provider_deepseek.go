package terminalai

const (
	deepSeekDefaultBaseURL = "https://api.deepseek.com"
	deepSeekMaxTokens      = 2048
)

type deepSeekProvider struct {
	*openAICompatibleProvider
}

func newDeepSeekProvider(config ProviderConfig) (Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = deepSeekDefaultBaseURL
	}
	provider, err := newOpenAICompatibleProvider(config)
	if err != nil {
		return nil, err
	}
	provider.maxTokens = deepSeekMaxTokens
	provider.extraFields = map[string]any{
		"thinking": map[string]string{"type": "disabled"},
	}
	return &deepSeekProvider{openAICompatibleProvider: provider}, nil
}
