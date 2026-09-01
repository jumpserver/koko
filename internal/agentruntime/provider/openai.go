package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

const maxLocalResponseTurns = 1

type responseTurn struct {
	input  string
	output []json.RawMessage
}

type openAIProvider struct {
	client   openai.Client
	fallback *compatibleProvider
	config   Config

	mu           sync.Mutex
	useResponses bool
	useStore     bool
	previousID   string
	turns        []responseTurn
}

func newOpenAIProvider(config Config) (Provider, error) {
	fallback, err := newCompatible(config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.ReasoningEffort) == "" {
		config.ReasoningEffort = "medium"
		fallback.config.ReasoningEffort = "medium"
	}
	fallback.info.EffectiveTransport = "responses"
	fallback.info.Capabilities.NativeCompaction = config.NativeCompaction
	return &openAIProvider{
		client: fallback.client, fallback: fallback, config: config,
		useResponses: true, useStore: config.Store,
	}, nil
}

func (p *openAIProvider) Info() ProviderInfo {
	p.mu.Lock()
	useResponses := p.useResponses
	p.mu.Unlock()
	info := p.fallback.Info()
	if useResponses {
		info.EffectiveTransport = "responses"
		info.Capabilities.NativeCompaction = p.config.NativeCompaction
	} else {
		info.EffectiveTransport = "chat-completions"
		info.Capabilities.NativeCompaction = false
	}
	return info
}

func (p *openAIProvider) CompactState(tier ContextTier) {
	p.mu.Lock()
	defer p.mu.Unlock()
	keep := len(p.turns)
	switch tier {
	case ContextCompact:
		keep = 1
	case ContextMinimal:
		keep = 0
	}
	if len(p.turns) > keep {
		p.turns = append([]responseTurn(nil), p.turns[len(p.turns)-keep:]...)
	}
	if tier != ContextFull {
		p.previousID = ""
	}
}

func (p *openAIProvider) Complete(
	ctx context.Context,
	request CompletionRequest,
) (CompletionResult, error) {
	p.mu.Lock()
	useResponses := p.useResponses
	p.mu.Unlock()
	if !useResponses {
		return p.fallback.Complete(ctx, request)
	}
	result, err := p.completeResponses(ctx, request)
	if err == nil {
		return result, nil
	}
	if IsKind(err, ErrorReasoningUnsupported) &&
		p.config.ReasoningMode == ReasoningAuto {
		p.fallback.capabilityMu.Lock()
		p.fallback.reasoning = false
		p.fallback.info.Capabilities.Reasoning = false
		p.fallback.capabilityMu.Unlock()
		trace(p.config.Trace, "provider_fallback", map[string]any{
			"provider": p.config.Name, "from": "responses_reasoning",
			"to": "responses_non_reasoning", "reason": err.Error(),
		})
		return p.Complete(ctx, request)
	}
	if IsKind(err, ErrorStateInvalid) {
		p.mu.Lock()
		storedState := p.useStore && p.previousID != ""
		p.useStore = false
		p.previousID = ""
		if !storedState {
			p.turns = nil
		}
		p.mu.Unlock()
		from := "local_reasoning_state"
		to := "explicit_context_only"
		if storedState {
			from = "stored_response_state"
			to = "local_response_replay"
		}
		trace(p.config.Trace, "provider_fallback", map[string]any{
			"provider": p.config.Name, "from": from,
			"to": to, "reason": err.Error(),
		})
		if storedState {
			return p.Complete(ctx, request)
		}
		request.ToolOutputs = nil
		return p.completeResponses(ctx, request)
	}
	structuredUnsupported := IsKind(err, ErrorStructuredUnsupported)
	toolUnsupported := IsKind(err, ErrorToolUnsupported)
	if !IsKind(err, ErrorResponsesUnsupported) && !structuredUnsupported &&
		!toolUnsupported {
		return result, err
	}
	p.mu.Lock()
	p.useResponses = false
	p.useStore = false
	p.previousID = ""
	p.mu.Unlock()
	from := "responses"
	if structuredUnsupported {
		from = "responses_structured_output"
	} else if toolUnsupported {
		from = "responses_tool_call"
	}
	trace(p.config.Trace, "provider_fallback", map[string]any{
		"provider": p.config.Name, "from": from,
		"to": "chat-completions", "reason": err.Error(),
	})
	return p.fallback.Complete(ctx, request)
}

func (p *openAIProvider) completeResponses(
	ctx context.Context,
	request CompletionRequest,
) (CompletionResult, error) {
	if err := ConsumeRequest(ctx); err != nil {
		return CompletionResult{}, err
	}
	p.mu.Lock()
	useStore := p.useStore
	previousID := p.previousID
	turns := cloneResponseTurns(p.turns)
	p.mu.Unlock()

	items := responseInput(
		turns, request.User, request.ToolOutputs, useStore && previousID != "",
	)
	params := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(p.config.Model),
		Instructions: openai.String(request.System),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam(items),
		},
		Store:           openai.Bool(useStore),
		MaxOutputTokens: openai.Int(p.config.MaxOutputTokens),
	}
	if useStore && previousID != "" {
		params.PreviousResponseID = openai.String(previousID)
	}
	reasoning := p.fallback.useReasoning(request)
	if !useStore && reasoning {
		params.Include = []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		}
	}
	if reasoning {
		params.Reasoning = shared.ReasoningParam{
			Effort:  shared.ReasoningEffort(p.reasoningEffort()),
			Context: shared.ReasoningContextAllTurns,
		}
	}
	if request.Operation == OperationAction && len(request.Tools) > 0 {
		params.Tools = responseTools(request.Tools)
		params.ParallelToolCalls = openai.Bool(false)
		if request.RequiredTool != "" {
			params.ToolChoice.OfFunctionTool = &responses.ToolChoiceFunctionParam{
				Name: request.RequiredTool,
			}
		} else {
			params.ToolChoice.OfToolChoiceMode = openai.Opt(responses.ToolChoiceOptionsAuto)
		}
	} else if request.Operation == OperationJSON {
		jsonObject := shared.NewResponseFormatJSONObjectParam()
		params.Text.Format = responses.ResponseFormatTextConfigUnionParam{
			OfJSONObject: &jsonObject,
		}
	}
	if p.config.NativeCompaction {
		threshold := p.config.ContextWindowTokens *
			int64(p.config.ContextSoftLimitPercent) / 100
		params.ContextManagement = []responses.ResponseNewParamsContextManagement{{
			Type: "compaction", CompactThreshold: openai.Int(threshold),
		}}
	}
	rawRequest, _ := json.Marshal(params)
	trace(p.config.Trace, "provider_request", map[string]any{
		"provider": p.config.Name, "transport": "responses",
		"baseURL":   observableBaseURL(p.config.BaseURL),
		"operation": request.Operation, "contextTier": request.Tier,
		"reasoning": reasoning, "store": useStore,
		"body": json.RawMessage(rawRequest),
	})

	var rawResponse *http.Response
	started := time.Now()
	response, err := p.client.Responses.New(
		ctx, params, option.WithResponseInto(&rawResponse),
	)
	p.fallback.traceProviderLatency(ctx, request, true, started, rawResponse, err)
	if err != nil {
		return CompletionResult{}, p.fallback.requestError(err, true)
	}
	result := responseResult(response, responseRequestID(rawResponse))
	trace(p.config.Trace, "provider_response", map[string]any{
		"provider": p.config.Name, "transport": "responses",
		"operation": request.Operation, "contextTier": request.Tier,
		"result": result,
	})
	if response.Status == responses.ResponseStatusIncomplete {
		kind := ErrorInvalidOutput
		if response.IncompleteDetails.Reason == "max_output_tokens" {
			kind = ErrorOutputLimit
			result.OutputTruncated = true
		}
		return result, NewOutputError(kind,
			"agent runtime Responses output is incomplete: %s",
			response.IncompleteDetails.Reason)
	}
	if response.Status != responses.ResponseStatusCompleted {
		return result, NewOutputError(ErrorInvalidOutput,
			"agent runtime Responses request ended with status %q", response.Status)
	}
	result.Content = strings.TrimSpace(response.OutputText())
	callCount := 0
	for _, item := range response.Output {
		if item.Type == "function_call" {
			callCount++
		}
	}
	if callCount > 1 {
		return result, NewOutputError(ErrorInvalidOutput,
			"agent runtime provider %s model %s returned multiple tool calls",
			p.config.Name, p.config.Model)
	}
	if result.ToolCall != nil {
		if !hasActionTool(request.Tools, result.ToolCall.Name) ||
			(request.RequiredTool != "" && result.ToolCall.Name != request.RequiredTool) {
			return result, NewOutputError(ErrorToolUnsupported,
				"agent runtime provider %s model %s returned an unexpected tool call",
				p.config.Name, p.config.Model)
		}
		if len(result.ToolCall.Arguments) == 0 {
			return result, NewOutputError(ErrorInvalidOutput,
				"agent runtime provider %s model %s returned empty tool arguments",
				p.config.Name, p.config.Model)
		}
	} else if request.RequiredTool != "" {
		return result, NewOutputError(ErrorToolUnsupported,
			"agent runtime provider %s model %s did not call required tool %q",
			p.config.Name, p.config.Model, request.RequiredTool)
	} else if result.Content == "" {
		return result, NewOutputError(ErrorInvalidOutput,
			"agent runtime provider %s model %s returned empty content",
			p.config.Name, p.config.Model)
	}
	p.mu.Lock()
	p.turns = append(p.turns, responseTurn{
		input: request.User, output: append([]json.RawMessage(nil), result.StateItems...),
	})
	if len(p.turns) > maxLocalResponseTurns {
		p.turns = append([]responseTurn(nil),
			p.turns[len(p.turns)-maxLocalResponseTurns:]...)
	}
	if useStore {
		p.previousID = response.ID
	}
	p.mu.Unlock()
	return result, nil
}

func (p *openAIProvider) reasoningEffort() string {
	if value := strings.TrimSpace(p.config.ReasoningEffort); value != "" {
		return value
	}
	return "medium"
}

func responseInput(
	turns []responseTurn,
	current string,
	outputs []ToolOutput,
	serverChained bool,
) []responses.ResponseInputItemUnionParam {
	toolOutputItems := make([]responses.ResponseInputItemUnionParam, 0, len(outputs))
	for _, output := range outputs {
		if output.CallID == "" {
			continue
		}
		toolOutputItems = append(toolOutputItems,
			responses.ResponseInputItemParamOfFunctionCallOutput(
				output.CallID, output.Output,
			),
		)
	}
	if serverChained {
		return append(toolOutputItems,
			responses.ResponseInputItemParamOfMessage(
				current, responses.EasyInputMessageRoleUser,
			),
		)
	}
	items := make([]responses.ResponseInputItemUnionParam, 0,
		len(turns)*2+len(toolOutputItems)+1)
	for _, turn := range turns {
		items = append(items, responses.ResponseInputItemParamOfMessage(
			turn.input, responses.EasyInputMessageRoleUser,
		))
		for _, raw := range turn.output {
			var item responses.ResponseInputItemUnion
			if json.Unmarshal(raw, &item) == nil {
				items = append(items, item.ToParam())
			}
		}
	}
	items = append(items, toolOutputItems...)
	return append(items, responses.ResponseInputItemParamOfMessage(
		current, responses.EasyInputMessageRoleUser,
	))
}

func responseResult(response *responses.Response, requestID string) CompletionResult {
	result := CompletionResult{
		ResponseID: response.ID, RequestID: requestID,
		Model: string(response.Model), FinishReason: string(response.Status),
		IncompleteReason: response.IncompleteDetails.Reason,
		Usage: TokenUsage{
			InputTokens:      response.Usage.InputTokens,
			OutputTokens:     response.Usage.OutputTokens,
			ReasoningTokens:  response.Usage.OutputTokensDetails.ReasoningTokens,
			CachedTokens:     response.Usage.InputTokensDetails.CachedTokens,
			CacheWriteTokens: response.Usage.InputTokensDetails.CacheWriteTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
		RawResponse: json.RawMessage(response.RawJSON()),
	}
	for _, item := range response.Output {
		raw := json.RawMessage(item.RawJSON())
		result.StateItems = append(result.StateItems, raw)
		if item.Type == "reasoning" && item.EncryptedContent != "" {
			result.ReasoningContent += item.EncryptedContent
		}
		if item.Type == "function_call" && result.ToolCall == nil {
			call := item.AsFunctionCall()
			result.ToolCall = &ToolCall{
				ID: call.CallID, Name: call.Name,
				Arguments: json.RawMessage(strings.TrimSpace(call.Arguments)),
			}
		}
	}
	return result
}

func responseTools(tools []ActionTool) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		result = append(result, responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name: tool.Name, Description: openai.String(tool.Description),
				Parameters: tool.Parameters, Strict: openai.Bool(false),
			},
		})
	}
	return result
}

func cloneResponseTurns(turns []responseTurn) []responseTurn {
	result := make([]responseTurn, len(turns))
	for index := range turns {
		result[index].input = turns[index].input
		result[index].output = append([]json.RawMessage(nil), turns[index].output...)
	}
	return result
}

var _ Provider = (*openAIProvider)(nil)
