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
		p.useStore = false
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
		return p.completeResponses(ctx, request)
	}
	structuredUnsupported := IsKind(err, ErrorStructuredUnsupported)
	if !IsKind(err, ErrorResponsesUnsupported) && !structuredUnsupported {
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

	items := responseInput(turns, request.User, useStore && previousID != "")
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
	if request.Operation == OperationAction && request.Tool != nil {
		format := responses.ResponseFormatTextConfigParamOfJSONSchema(
			request.Tool.Name, request.Tool.Parameters,
		)
		if format.OfJSONSchema != nil {
			format.OfJSONSchema.Strict = openai.Bool(true)
			format.OfJSONSchema.Description = openai.String(request.Tool.Description)
		}
		params.Text.Format = format
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
			"terminal AI Responses output is incomplete: %s",
			response.IncompleteDetails.Reason)
	}
	if response.Status != responses.ResponseStatusCompleted {
		return result, NewOutputError(ErrorInvalidOutput,
			"terminal AI Responses request ended with status %q", response.Status)
	}
	result.Content = strings.TrimSpace(response.OutputText())
	if result.Content == "" {
		return result, NewOutputError(ErrorInvalidOutput,
			"terminal AI provider %s model %s returned empty content",
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
	serverChained bool,
) []responses.ResponseInputItemUnionParam {
	if serverChained {
		return []responses.ResponseInputItemUnionParam{
			responses.ResponseInputItemParamOfMessage(
				current, responses.EasyInputMessageRoleUser,
			),
		}
	}
	items := make([]responses.ResponseInputItemUnionParam, 0, len(turns)*2+1)
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
