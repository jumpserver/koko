package terminalai

import (
	"context"
	"testing"

	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

type contextFallbackProvider struct {
	tiers       []provider.ContextTier
	compactions []provider.ContextTier
}

func (p *contextFallbackProvider) Info() provider.ProviderInfo {
	return provider.ProviderInfo{}
}

func (p *contextFallbackProvider) Complete(
	_ context.Context,
	request provider.CompletionRequest,
) (provider.CompletionResult, error) {
	p.tiers = append(p.tiers, request.Tier)
	if request.Tier == provider.ContextFull {
		return provider.CompletionResult{}, provider.NewOutputError(
			provider.ErrorContextOverflow, "context overflow",
		)
	}
	return provider.CompletionResult{Content: "ok"}, nil
}

func (p *contextFallbackProvider) CompactState(tier provider.ContextTier) {
	p.compactions = append(p.compactions, tier)
}

func TestCompleteFallsBackToCompactContext(t *testing.T) {
	modelProvider := &contextFallbackProvider{}
	client := &ModelClient{provider: modelProvider}
	result, err := client.completeWithFallback(
		context.Background(),
		func(tier provider.ContextTier) provider.CompletionRequest {
			return provider.CompletionRequest{Tier: tier}
		},
	)
	if err != nil || result.Content != "ok" {
		t.Fatalf("completion = %q, %v", result.Content, err)
	}
	if len(modelProvider.tiers) != 2 ||
		modelProvider.tiers[0] != provider.ContextFull ||
		modelProvider.tiers[1] != provider.ContextCompact {
		t.Fatalf("context tiers = %v", modelProvider.tiers)
	}
	if len(modelProvider.compactions) != 1 ||
		modelProvider.compactions[0] != provider.ContextCompact {
		t.Fatalf("provider compactions = %v", modelProvider.compactions)
	}
}
