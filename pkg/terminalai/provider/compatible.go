package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type compatibleProvider struct {
	client openai.Client
	config Config
	info   ProviderInfo

	capabilityMu sync.RWMutex
	toolCall     bool
	reasoning    bool
	structured   bool
	legacyMax    bool
	omitEffort   bool
	extraFields  func(bool) map[string]any
}

func newCompatibleProvider(config Config) (Provider, error) {
	return newCompatible(config)
}

func newCompatible(config Config) (*compatibleProvider, error) {
	client, err := newOpenAIClient(config)
	if err != nil {
		return nil, err
	}
	return &compatibleProvider{
		client: client, config: config,
		info: ProviderInfo{
			Name: config.Name, Model: config.Model,
			EffectiveTransport: "chat-completions",
			Capabilities: ProviderCapabilities{
				StructuredOutput: true,
				ToolCall:         config.ToolCallMode != ToolCallDisabled,
				Reasoning:        config.ReasoningMode != ReasoningOff,
			},
		},
		toolCall:   config.ToolCallMode != ToolCallDisabled,
		reasoning:  config.ReasoningMode != ReasoningOff,
		structured: true,
	}, nil
}

func newOpenAIClient(config Config) (openai.Client, error) {
	httpClient, err := newHTTPClient(config)
	if err != nil {
		return openai.Client{}, err
	}
	options := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	}
	if config.BaseURL != "" {
		options = append(options, option.WithBaseURL(config.BaseURL))
	}
	return openai.NewClient(options...), nil
}

func (p *compatibleProvider) Info() ProviderInfo {
	p.capabilityMu.RLock()
	defer p.capabilityMu.RUnlock()
	info := p.info
	info.Capabilities.ToolCall = p.toolCall
	info.Capabilities.Reasoning = p.reasoning
	info.Capabilities.StructuredOutput = p.structured
	return info
}

func (p *compatibleProvider) CompactState(ContextTier) {}

func (p *compatibleProvider) Complete(
	ctx context.Context,
	request CompletionRequest,
) (CompletionResult, error) {
	reasoning := p.useReasoning(request)
	result, err := p.complete(ctx, request, reasoning)
	if err == nil || !IsKind(err, ErrorReasoningUnsupported) ||
		p.config.ReasoningMode != ReasoningAuto {
		return result, err
	}
	p.capabilityMu.Lock()
	p.reasoning = false
	p.info.Capabilities.Reasoning = false
	p.capabilityMu.Unlock()
	trace(p.config.Trace, "provider_fallback", map[string]any{
		"provider": p.config.Name, "from": "reasoning", "to": "non_reasoning",
		"reason": err.Error(),
	})
	return p.complete(ctx, request, false)
}

func (p *compatibleProvider) useReasoning(request CompletionRequest) bool {
	mode := strings.ToLower(strings.TrimSpace(request.ReasoningMode))
	if mode == ReasoningOff {
		return false
	}
	p.capabilityMu.RLock()
	enabled := p.reasoning
	p.capabilityMu.RUnlock()
	if !enabled {
		return false
	}
	if mode == ReasoningOn || p.config.ReasoningMode == ReasoningOn {
		return true
	}
	return p.config.ReasoningMode == ReasoningAuto &&
		request.Operation == OperationAction
}

func (p *compatibleProvider) complete(
	ctx context.Context,
	request CompletionRequest,
	reasoning bool,
) (CompletionResult, error) {
	if request.Operation == OperationAction && request.Tool != nil {
		return p.completeAction(ctx, request, reasoning)
	}
	if request.Operation == OperationJSON {
		return p.completeJSONChat(ctx, request, reasoning)
	}
	return p.completeChat(ctx, request, reasoning,
		openai.ChatCompletionNewParamsResponseFormatUnion{})
}

func (p *compatibleProvider) completeAction(
	ctx context.Context,
	request CompletionRequest,
	reasoning bool,
) (CompletionResult, error) {
	p.capabilityMu.RLock()
	useToolCall := p.toolCall
	p.capabilityMu.RUnlock()
	if !useToolCall {
		return p.completeActionJSON(ctx, request, reasoning)
	}
	result, err := p.completeTool(ctx, request, reasoning)
	if err == nil || p.config.ToolCallMode != ToolCallAuto ||
		!IsKind(err, ErrorToolUnsupported) {
		return result, err
	}
	p.capabilityMu.Lock()
	p.toolCall = false
	p.info.Capabilities.ToolCall = false
	p.capabilityMu.Unlock()
	trace(p.config.Trace, "provider_fallback", map[string]any{
		"provider": p.config.Name, "from": "tool_call", "to": "json",
		"reason": err.Error(),
	})
	return p.completeActionJSON(ctx, request, reasoning)
}

func (p *compatibleProvider) completeActionJSON(
	ctx context.Context,
	request CompletionRequest,
	reasoning bool,
) (CompletionResult, error) {
	schema, err := json.Marshal(request.Tool.Parameters)
	if err != nil {
		return CompletionResult{}, fmt.Errorf(
			"encode terminal AI action %q schema: %w", request.Tool.Name, err,
		)
	}
	request.System = fmt.Sprintf(
		"%s\nReturn only one JSON object containing the arguments for action %q. "+
			"It must match this JSON Schema exactly. Object-valued fields must "+
			"be JSON objects, never JSON-encoded strings:\n%s",
		request.System, request.Tool.Name, schema,
	)
	return p.completeJSONChat(ctx, request, reasoning)
}

func (p *compatibleProvider) completeJSONChat(
	ctx context.Context,
	request CompletionRequest,
	reasoning bool,
) (CompletionResult, error) {
	p.capabilityMu.RLock()
	structured := p.structured
	p.capabilityMu.RUnlock()
	var format openai.ChatCompletionNewParamsResponseFormatUnion
	if structured {
		jsonFormat := shared.NewResponseFormatJSONObjectParam()
		format.OfJSONObject = &jsonFormat
	}
	result, err := p.completeChat(ctx, request, reasoning, format)
	if err == nil || !structured || !IsKind(err, ErrorStructuredUnsupported) {
		return result, err
	}
	p.capabilityMu.Lock()
	p.structured = false
	p.info.Capabilities.StructuredOutput = false
	p.capabilityMu.Unlock()
	trace(p.config.Trace, "provider_fallback", map[string]any{
		"provider": p.config.Name, "from": "structured_output",
		"to": "prompt_json", "reason": err.Error(),
	})
	return p.completeChat(ctx, request, reasoning,
		openai.ChatCompletionNewParamsResponseFormatUnion{})
}

func (p *compatibleProvider) completeTool(
	ctx context.Context,
	request CompletionRequest,
	reasoning bool,
) (CompletionResult, error) {
	if err := ConsumeRequest(ctx); err != nil {
		return CompletionResult{}, err
	}
	tool := request.Tool
	params := openai.ChatCompletionNewParams{
		Model: p.config.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(request.System), openai.UserMessage(request.User),
		},
		ParallelToolCalls: openai.Bool(false),
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name: tool.Name, Description: openai.String(tool.Description),
				Parameters: shared.FunctionParameters(tool.Parameters),
			}),
		},
		ToolChoice: openai.ToolChoiceOptionFunctionToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: tool.Name},
		),
	}
	p.configureChatParams(&params, reasoning)
	p.traceRequest(request, params, reasoning)
	var rawResponse *http.Response
	started := time.Now()
	response, err := p.client.Chat.Completions.New(
		ctx, params, option.WithResponseInto(&rawResponse),
	)
	p.traceProviderLatency(ctx, request, false, started, rawResponse, err)
	if err != nil {
		return CompletionResult{}, p.requestError(err, false)
	}
	result := chatResult(response, responseRequestID(rawResponse))
	p.traceResponse(request, result)
	if err := validateChatFinish(result); err != nil {
		return result, err
	}
	if len(response.Choices) == 0 {
		return result, NewOutputError(ErrorInvalidOutput,
			"terminal AI provider %s model %s returned no choices",
			p.config.Name, p.config.Model)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].Type != "function" ||
		calls[0].Function.Name != tool.Name {
		return result, NewOutputError(ErrorToolUnsupported,
			"terminal AI provider %s model %s returned an unexpected tool call",
			p.config.Name, p.config.Model)
	}
	result.Content = strings.TrimSpace(calls[0].Function.Arguments)
	if result.Content == "" {
		return result, NewOutputError(ErrorInvalidOutput,
			"terminal AI provider %s model %s returned empty tool arguments",
			p.config.Name, p.config.Model)
	}
	return result, nil
}

func (p *compatibleProvider) completeChat(
	ctx context.Context,
	request CompletionRequest,
	reasoning bool,
	format openai.ChatCompletionNewParamsResponseFormatUnion,
) (CompletionResult, error) {
	if err := ConsumeRequest(ctx); err != nil {
		return CompletionResult{}, err
	}
	params := openai.ChatCompletionNewParams{
		Model: p.config.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(request.System), openai.UserMessage(request.User),
		},
		ResponseFormat: format,
	}
	p.configureChatParams(&params, reasoning)
	p.traceRequest(request, params, reasoning)
	var rawResponse *http.Response
	started := time.Now()
	response, err := p.client.Chat.Completions.New(
		ctx, params, option.WithResponseInto(&rawResponse),
	)
	p.traceProviderLatency(ctx, request, false, started, rawResponse, err)
	if err != nil {
		return CompletionResult{}, p.requestError(err, false)
	}
	result := chatResult(response, responseRequestID(rawResponse))
	p.traceResponse(request, result)
	if err := validateChatFinish(result); err != nil {
		return result, err
	}
	if len(response.Choices) == 0 {
		return result, NewOutputError(ErrorInvalidOutput,
			"terminal AI provider %s model %s returned no choices",
			p.config.Name, p.config.Model)
	}
	result.Content = strings.TrimSpace(response.Choices[0].Message.Content)
	if result.Content == "" {
		return result, NewOutputError(ErrorInvalidOutput,
			"terminal AI provider %s model %s returned empty content",
			p.config.Name, p.config.Model)
	}
	return result, nil
}

func (p *compatibleProvider) configureChatParams(
	params *openai.ChatCompletionNewParams,
	reasoning bool,
) {
	if reasoning {
		if p.legacyMax {
			params.MaxTokens = openai.Int(p.config.MaxOutputTokens)
		} else {
			params.MaxCompletionTokens = openai.Int(p.config.MaxOutputTokens)
		}
		if !p.omitEffort {
			params.ReasoningEffort = shared.ReasoningEffort(p.reasoningEffort())
		}
	} else {
		params.Temperature = openai.Float(0.1)
		if usesMaxCompletionTokens(p.config.Model) && !p.legacyMax {
			params.MaxCompletionTokens = openai.Int(p.config.MaxOutputTokens)
		} else {
			params.MaxTokens = openai.Int(p.config.MaxOutputTokens)
		}
	}
	if p.extraFields != nil {
		params.SetExtraFields(p.extraFields(reasoning))
	}
}

func (p *compatibleProvider) reasoningEffort() string {
	if value := strings.TrimSpace(p.config.ReasoningEffort); value != "" {
		return value
	}
	return "medium"
}

func usesMaxCompletionTokens(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "o1") ||
		strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4")
}

func (p *compatibleProvider) traceRequest(
	request CompletionRequest,
	params openai.ChatCompletionNewParams,
	reasoning bool,
) {
	raw, _ := json.Marshal(params)
	trace(p.config.Trace, "provider_request", map[string]any{
		"provider": p.config.Name, "transport": "chat-completions",
		"baseURL":   observableBaseURL(p.config.BaseURL),
		"operation": request.Operation, "contextTier": request.Tier,
		"reasoning": reasoning, "body": json.RawMessage(raw),
	})
}

func (p *compatibleProvider) traceResponse(
	request CompletionRequest,
	result CompletionResult,
) {
	trace(p.config.Trace, "provider_response", map[string]any{
		"provider": p.config.Name, "transport": "chat-completions",
		"operation": request.Operation, "contextTier": request.Tier,
		"result": result,
	})
}

func (p *compatibleProvider) traceProviderLatency(
	ctx context.Context,
	request CompletionRequest,
	responsesAPI bool,
	started time.Time,
	response *http.Response,
	requestErr error,
) {
	transport := "chat-completions"
	if responsesAPI {
		transport = "responses"
	}
	payload := map[string]any{
		"layer": "provider", "stage": "http_request",
		"provider": p.config.Name, "model": p.config.Model,
		"transport": transport, "baseURL": observableBaseURL(p.config.BaseURL),
		"operation": request.Operation, "contextTier": request.Tier,
		"outcome": "success",
	}
	if taskID := LatencyTaskID(ctx); taskID != "" {
		payload["taskId"] = taskID
	}
	if requestErr != nil {
		payload["outcome"] = "error"
	}
	if response != nil {
		payload["statusCode"] = response.StatusCode
		payload["requestId"] = responseRequestID(response)
	}
	traceLatency(p.config.Trace, started, payload)
}

func chatResult(response *openai.ChatCompletion, requestID string) CompletionResult {
	result := CompletionResult{
		ResponseID: response.ID, RequestID: requestID, Model: response.Model,
		Usage: TokenUsage{
			InputTokens:      response.Usage.PromptTokens,
			OutputTokens:     response.Usage.CompletionTokens,
			ReasoningTokens:  response.Usage.CompletionTokensDetails.ReasoningTokens,
			CachedTokens:     response.Usage.PromptTokensDetails.CachedTokens,
			CacheWriteTokens: response.Usage.PromptTokensDetails.CacheWriteTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
		RawResponse: json.RawMessage(response.RawJSON()),
	}
	if len(response.Choices) > 0 {
		result.FinishReason = string(response.Choices[0].FinishReason)
		result.OutputTruncated = result.FinishReason == "length"
		var raw struct {
			Choices []struct {
				Message struct {
					ReasoningContent string `json:"reasoning_content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptCacheHitTokens  int64 `json:"prompt_cache_hit_tokens"`
				PromptCacheMissTokens int64 `json:"prompt_cache_miss_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(result.RawResponse, &raw) == nil && len(raw.Choices) > 0 {
			result.ReasoningContent = raw.Choices[0].Message.ReasoningContent
			if result.Usage.CachedTokens == 0 {
				result.Usage.CachedTokens = raw.Usage.PromptCacheHitTokens
			}
			if result.Usage.CacheWriteTokens == 0 {
				result.Usage.CacheWriteTokens = raw.Usage.PromptCacheMissTokens
			}
		}
	}
	return result
}

func validateChatFinish(result CompletionResult) error {
	switch result.FinishReason {
	case "length":
		return NewOutputError(ErrorOutputLimit,
			"terminal AI model output was truncated at the token or context limit")
	case "content_filter", "insufficient_system_resource":
		return NewOutputError(ErrorInvalidOutput,
			"terminal AI model stopped with finish reason %q", result.FinishReason)
	}
	return nil
}

func (p *compatibleProvider) requestError(err error, responsesAPI bool) error {
	requestErr := &RequestError{Err: fmt.Errorf(
		"terminal AI provider %s model %s request failed: %w",
		p.config.Name, p.config.Model, err,
	)}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		requestErr.StatusCode = apiErr.StatusCode
		requestErr.Code = apiErr.Code
		requestErr.Param = apiErr.Param
		if apiErr.Response != nil {
			requestErr.RequestID = apiErr.Response.Header.Get("x-request-id")
		}
		requestErr.Kind = classifyAPIError(apiErr, responsesAPI)
		requestErr.Retryable = requestErr.Kind == ErrorRateLimited ||
			requestErr.Kind == ErrorServer
		trace(p.config.Trace, "provider_error", map[string]any{
			"provider": p.config.Name, "transport": map[bool]string{true: "responses", false: "chat-completions"}[responsesAPI],
			"statusCode": apiErr.StatusCode, "code": apiErr.Code,
			"param": apiErr.Param, "requestId": requestErr.RequestID,
			"kind": requestErr.Kind, "body": json.RawMessage(apiErr.RawJSON()),
		})
		return requestErr
	}
	if errors.Is(err, context.Canceled) {
		requestErr.Kind = ErrorCancelled
	} else if errors.Is(err, context.DeadlineExceeded) {
		requestErr.Kind = ErrorNetwork
		requestErr.Retryable = true
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) {
			requestErr.Kind = ErrorNetwork
			requestErr.Retryable = netErr.Timeout() || netErr.Temporary()
		}
	}
	trace(p.config.Trace, "provider_error", map[string]any{
		"provider": p.config.Name,
		"transport": map[bool]string{
			true: "responses", false: "chat-completions",
		}[responsesAPI],
		"kind": requestErr.Kind, "error": requestErr.Error(),
	})
	return requestErr
}

func classifyAPIError(err *openai.Error, responsesAPI bool) ErrorKind {
	value := strings.ToLower(strings.Join([]string{
		err.Param, err.Code, err.Type, err.Message,
	}, " "))
	if err.StatusCode == http.StatusTooManyRequests {
		return ErrorRateLimited
	}
	if err.StatusCode >= http.StatusInternalServerError {
		if responsesAPI && err.StatusCode == http.StatusNotImplemented {
			return ErrorResponsesUnsupported
		}
		return ErrorServer
	}
	if containsAny(value, "context_length", "context length", "context window",
		"maximum context", "too many tokens", "input tokens") {
		return ErrorContextOverflow
	}
	if containsAny(value, "previous_response_id", "previous response", "reasoning item",
		"encrypted reasoning", "encrypted_content") &&
		containsAny(value, "invalid", "expired", "not found", "missing") {
		return ErrorStateInvalid
	}
	if responsesAPI && (err.StatusCode == http.StatusMethodNotAllowed ||
		(err.StatusCode == http.StatusNotFound &&
			!containsAny(value, "model", "deployment")) ||
		containsAny(value, "unknown endpoint", "unsupported endpoint", "responses api")) {
		return ErrorResponsesUnsupported
	}
	if containsAny(value, "reasoning_effort", "reasoning effort", "thinking") &&
		containsAny(value, "unsupported", "not support", "unknown", "invalid", "not allowed") {
		return ErrorReasoningUnsupported
	}
	if containsAny(value, "response_format", "json_schema", "structured output") &&
		containsAny(value, "unsupported", "not support", "unknown", "invalid", "not allowed") {
		return ErrorStructuredUnsupported
	}
	if containsAny(value, "tool", "function", "parallel_tool_calls") &&
		containsAny(value, "unsupported", "not support", "unknown", "invalid", "not allowed") {
		return ErrorToolUnsupported
	}
	return ""
}

func containsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
