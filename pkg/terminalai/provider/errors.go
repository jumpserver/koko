package provider

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorContextOverflow       ErrorKind = "context_overflow"
	ErrorOutputLimit           ErrorKind = "output_limit"
	ErrorResponsesUnsupported  ErrorKind = "responses_unsupported"
	ErrorReasoningUnsupported  ErrorKind = "reasoning_unsupported"
	ErrorToolUnsupported       ErrorKind = "tool_unsupported"
	ErrorStructuredUnsupported ErrorKind = "structured_output_unsupported"
	ErrorStateInvalid          ErrorKind = "state_invalid"
	ErrorInvalidOutput         ErrorKind = "invalid_output"
	ErrorRateLimited           ErrorKind = "rate_limited"
	ErrorServer                ErrorKind = "server_error"
	ErrorNetwork               ErrorKind = "network_error"
	ErrorCancelled             ErrorKind = "cancelled"
)

type RequestError struct {
	Err        error
	Kind       ErrorKind
	StatusCode int
	Code       string
	Param      string
	RequestID  string
	Retryable  bool
}

func (e *RequestError) Error() string {
	if e == nil || e.Err == nil {
		return "terminal AI provider request failed"
	}
	return e.Err.Error()
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type OutputError struct {
	Err  error
	Kind ErrorKind
}

func (e *OutputError) Error() string {
	if e == nil || e.Err == nil {
		return "terminal AI provider returned invalid output"
	}
	return e.Err.Error()
}

func (e *OutputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewOutputError(kind ErrorKind, format string, args ...any) error {
	return &OutputError{Err: fmt.Errorf(format, args...), Kind: kind}
}

func IsKind(err error, kind ErrorKind) bool {
	var requestErr *RequestError
	if errors.As(err, &requestErr) && requestErr.Kind == kind {
		return true
	}
	var outputErr *OutputError
	return errors.As(err, &outputErr) && outputErr.Kind == kind
}

func IsRetryable(err error) bool {
	var requestErr *RequestError
	return errors.As(err, &requestErr) && requestErr.Retryable
}
