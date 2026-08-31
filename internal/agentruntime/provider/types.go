package provider

import (
	"context"
	"encoding/json"
	"time"
)

const (
	NameGPT      = "gpt"
	NameOpenAI   = "openai"
	NameDeepSeek = "deep-seek"

	ToolCallAuto     = "auto"
	ToolCallEnabled  = "true"
	ToolCallDisabled = "false"

	ReasoningOff  = "off"
	ReasoningAuto = "auto"
	ReasoningOn   = "on"
)

const TraceLatency = "latency"

type Operation string

const (
	OperationAction     Operation = "action"
	OperationJSON       Operation = "json"
	OperationText       Operation = "text"
	OperationCheckpoint Operation = "checkpoint"
)

type ContextTier string

const (
	ContextFull    ContextTier = "full"
	ContextCompact ContextTier = "compact"
	ContextMinimal ContextTier = "minimal"
)

type Config struct {
	Name                    string
	APIKey                  string
	BaseURL                 string
	Model                   string
	Proxy                   string
	ToolCallMode            string
	ReasoningMode           string
	ReasoningEffort         string
	Store                   bool
	NativeCompaction        bool
	ContextWindowTokens     int64
	MaxOutputTokens         int64
	ContextSoftLimitPercent int
	RequestTimeout          time.Duration
	Trace                   TraceSink
}

type ProviderCapabilities struct {
	StructuredOutput bool `json:"structuredOutput"`
	ToolCall         bool `json:"toolCall"`
	Streaming        bool `json:"streaming"`
	Reasoning        bool `json:"reasoning"`
	NativeCompaction bool `json:"nativeCompaction"`
}

type ProviderInfo struct {
	Name               string               `json:"name"`
	Model              string               `json:"model"`
	Capabilities       ProviderCapabilities `json:"capabilities"`
	EffectiveTransport string               `json:"-"`
}

type ActionTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type CompletionRequest struct {
	Operation     Operation
	System        string
	User          string
	Tool          *ActionTool
	Tier          ContextTier
	ReasoningMode string
}

type TokenUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	ReasoningTokens  int64 `json:"reasoningTokens,omitempty"`
	CachedTokens     int64 `json:"cachedTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
	TotalTokens      int64 `json:"totalTokens"`
}

type CompletionResult struct {
	Content          string            `json:"content"`
	FinishReason     string            `json:"finishReason,omitempty"`
	IncompleteReason string            `json:"incompleteReason,omitempty"`
	ResponseID       string            `json:"responseId,omitempty"`
	RequestID        string            `json:"requestId,omitempty"`
	Model            string            `json:"model,omitempty"`
	Usage            TokenUsage        `json:"usage"`
	ReasoningContent string            `json:"reasoningContent,omitempty"`
	StateItems       []json.RawMessage `json:"stateItems,omitempty"`
	RawResponse      json.RawMessage   `json:"rawResponse,omitempty"`
	OutputTruncated  bool              `json:"outputTruncated,omitempty"`
}

type Provider interface {
	Info() ProviderInfo
	Complete(context.Context, CompletionRequest) (CompletionResult, error)
	CompactState(ContextTier)
}

type Factory func(Config) (Provider, error)

type TraceSink interface {
	Record(string, any)
}

type latencyTaskIDKey struct{}

func WithLatencyTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, latencyTaskIDKey{}, taskID)
}

func LatencyTaskID(ctx context.Context) string {
	taskID, _ := ctx.Value(latencyTaskIDKey{}).(string)
	return taskID
}

func trace(sink TraceSink, event string, payload any) {
	if sink != nil {
		sink.Record(event, payload)
	}
}

func traceLatency(sink TraceSink, started time.Time, payload map[string]any) {
	payload["durationMs"] = float64(time.Since(started).Microseconds()) / 1000
	trace(sink, TraceLatency, payload)
}
