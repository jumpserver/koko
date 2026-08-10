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

type latencyTrace struct {
	events []map[string]any
}

func (t *latencyTrace) Record(event string, payload any) {
	if event == provider.TraceLatency {
		values, _ := payload.(map[string]any)
		t.events = append(t.events, values)
	}
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
	trace := &latencyTrace{}
	client := &ModelClient{
		provider: modelProvider,
		config:   Config{Provider: provider.Config{Trace: trace}},
	}
	ctx := provider.WithLatencyTaskID(context.Background(), "task-1")
	result, err := client.completeWithFallback(
		ctx,
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
	if len(trace.events) != 4 {
		t.Fatalf("latency events = %d, want 4", len(trace.events))
	}
	for _, event := range trace.events {
		if event["taskId"] != "task-1" || event["durationMs"] == nil {
			t.Fatalf("latency event = %#v", event)
		}
	}
}
