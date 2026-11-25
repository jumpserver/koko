// Package model defines provider-neutral configuration and usage values.
// Model execution belongs exclusively to the Codex harness.
package model

import (
	"context"
	"time"
)

type Capabilities struct {
	Responses        bool `json:"responses"`
	ChatCompletions  bool `json:"chat_completions"`
	NativeTools      bool `json:"native_tools"`
	StructuredOutput bool `json:"structured_output"`
	Reasoning        bool `json:"reasoning"`
	PreviousResponse bool `json:"previous_response"`
}

type Info struct {
	Provider     string       `json:"provider"`
	Model        string       `json:"model"`
	Capabilities Capabilities `json:"capabilities"`
}

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Message struct {
	Role    string
	Content string
	Parts   []ContentPart
}

type ContentPart struct {
	Type      string
	Text      string
	MediaType string
	Data      string
}

type Usage struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	CachedTokens    int64 `json:"cached_tokens,omitempty"`
}

type Result struct {
	Content      string
	FinishReason string
	RequestID    string
	Usage        Usage
}

type ErrorKind string

const (
	ErrorAuthentication ErrorKind = "authentication"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorTimeout        ErrorKind = "timeout"
	ErrorUnavailable    ErrorKind = "unavailable"
	ErrorInvalidRequest ErrorKind = "invalid_request"
	ErrorInvalidOutput  ErrorKind = "invalid_output"
)

type ProviderError struct {
	Kind      ErrorKind
	Retryable bool
	Status    int
	Message   string
	cause     error
}

func (e *ProviderError) Error() string { return e.Message }
func (e *ProviderError) Unwrap() error { return e.cause }

type Config struct {
	Provider        string
	BaseURL         string
	APIKey          string
	Model           string
	Proxy           string
	ReasoningEffort string
	Timeout         time.Duration
}

type ConfigLoader func(context.Context) (Config, error)
