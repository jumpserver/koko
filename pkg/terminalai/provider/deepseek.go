package provider

import (
	"context"
	"strings"
)

const deepSeekDefaultBaseURL = "https://api.deepseek.com"

type deepSeekProvider struct {
	*compatibleProvider
}

func newDeepSeekProvider(config Config) (Provider, error) {
	if config.BaseURL == "" {
		config.BaseURL = deepSeekDefaultBaseURL
	}
	if strings.TrimSpace(config.ReasoningEffort) == "" {
		config.ReasoningEffort = "high"
	}
	compatible, err := newCompatible(config)
	if err != nil {
		return nil, err
	}
	compatible.legacyMax = true
	compatible.info.EffectiveTransport = "deepseek-chat-completions"
	compatible.info.Capabilities.Reasoning = config.ReasoningMode != ReasoningOff
	legacyReasoner := strings.EqualFold(config.Model, "deepseek-reasoner")
	if legacyReasoner {
		compatible.omitEffort = true
	} else {
		compatible.extraFields = func(reasoning bool) map[string]any {
			mode := "disabled"
			if reasoning {
				mode = "enabled"
			}
			return map[string]any{"thinking": map[string]string{"type": mode}}
		}
	}
	return &deepSeekProvider{compatibleProvider: compatible}, nil
}

func (p *deepSeekProvider) Complete(
	ctx context.Context,
	request CompletionRequest,
) (CompletionResult, error) {
	reasoning := p.useReasoning(request)
	var result CompletionResult
	var err error
	if reasoning && request.Operation == OperationAction && request.Tool != nil {
		result, err = p.completeActionJSON(ctx, request, true)
	} else {
		result, err = p.complete(ctx, request, reasoning)
	}
	if err == nil || !IsKind(err, ErrorReasoningUnsupported) ||
		p.config.ReasoningMode != ReasoningAuto {
		return result, err
	}
	p.capabilityMu.Lock()
	p.reasoning = false
	p.info.Capabilities.Reasoning = false
	p.capabilityMu.Unlock()
	trace(p.config.Trace, "provider_fallback", map[string]any{
		"provider": p.config.Name, "from": "deepseek_thinking",
		"to": "non_reasoning_compatible", "reason": err.Error(),
	})
	return p.complete(ctx, request, false)
}
