package terminalai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	ProviderEnvName      = "TERMINAL_AI_PROVIDER"
	ToolCallEnvName      = "TERMINAL_AI_TOOL_CALL"
	ProviderOpenAI       = "openai"
	ProviderDeepSeek     = "deepseek"
	ToolCallAuto         = "auto"
	ToolCallEnabled      = "true"
	ToolCallDisabled     = "false"
	modelRequestTimeout  = 2 * time.Minute
	maxModelRequests     = 30
	errModelRequestLimit = "terminal AI model request budget exhausted"
)

type ProviderConfig struct {
	Name         string
	APIKey       string
	BaseURL      string
	Model        string
	Proxy        string
	ToolCallMode string
}

type ProviderCapabilities struct {
	StructuredOutput bool `json:"structuredOutput"`
	ToolCall         bool `json:"toolCall"`
	Streaming        bool `json:"streaming"`
}

type ProviderInfo struct {
	Name         string               `json:"name"`
	Model        string               `json:"model"`
	Capabilities ProviderCapabilities `json:"capabilities"`
}

type Provider interface {
	Info() ProviderInfo
	CompleteJSON(ctx context.Context, system, user string) (string, error)
	CompleteAction(
		ctx context.Context,
		system, user string,
		tool ActionTool,
	) (string, error)
	CompleteText(ctx context.Context, system, user string) (string, error)
}

type ProviderFactory func(ProviderConfig) (Provider, error)

type ActionTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ProviderRequestError struct {
	Err             error
	StatusCode      int
	Retryable       bool
	ToolUnsupported bool
}

func (e *ProviderRequestError) Error() string {
	return e.Err.Error()
}

func (e *ProviderRequestError) Unwrap() error {
	return e.Err
}

type ModelOutputError struct {
	Err error
}

func (e *ModelOutputError) Error() string {
	return e.Err.Error()
}

func (e *ModelOutputError) Unwrap() error {
	return e.Err
}

type modelRequestBudget struct {
	limit int64
	used  atomic.Int64
}

type modelRequestBudgetKey struct{}

func withModelRequestBudget(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, modelRequestBudgetKey{}, &modelRequestBudget{
		limit: int64(limit),
	})
}

func consumeModelRequest(ctx context.Context) error {
	budget, _ := ctx.Value(modelRequestBudgetKey{}).(*modelRequestBudget)
	if budget == nil {
		return nil
	}
	if budget.used.Add(1) <= budget.limit {
		return nil
	}
	return errors.New(errModelRequestLimit)
}

func modelRequestUsage(ctx context.Context) int {
	budget, _ := ctx.Value(modelRequestBudgetKey{}).(*modelRequestBudget)
	if budget == nil {
		return 0
	}
	return int(budget.used.Load())
}

func newProviderHTTPClient(proxy string) (*http.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if value := strings.TrimSpace(proxy); value != "" {
		proxyURL, err := url.Parse(value)
		if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			return nil, fmt.Errorf("terminal AI proxy URL is invalid")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   modelRequestTimeout,
	}, nil
}
