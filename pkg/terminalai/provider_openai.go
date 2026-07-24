package terminalai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type openAICompatibleProvider struct {
	client       openai.Client
	info         ProviderInfo
	toolCallMode string
	toolMu       sync.RWMutex
	toolCall     bool
	maxTokens    int64
	extraFields  map[string]any
}

type openAIProvider struct {
	*openAICompatibleProvider
}

func newOpenAIProvider(config ProviderConfig) (Provider, error) {
	provider, err := newOpenAICompatibleProvider(config)
	if err != nil {
		return nil, err
	}
	return &openAIProvider{openAICompatibleProvider: provider}, nil
}

func newOpenAICompatibleProvider(
	config ProviderConfig,
) (*openAICompatibleProvider, error) {
	httpClient, err := newProviderHTTPClient(config.Proxy)
	if err != nil {
		return nil, err
	}
	options := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	}
	if config.BaseURL != "" {
		options = append(options, option.WithBaseURL(config.BaseURL))
	}
	return &openAICompatibleProvider{
		client: openai.NewClient(options...),
		info: ProviderInfo{
			Name:  config.Name,
			Model: config.Model,
			Capabilities: ProviderCapabilities{
				StructuredOutput: true,
				ToolCall:         config.ToolCallMode != ToolCallDisabled,
				Streaming:        false,
			},
		},
		toolCallMode: config.ToolCallMode,
		toolCall:     config.ToolCallMode != ToolCallDisabled,
	}, nil
}

func (p *openAICompatibleProvider) Info() ProviderInfo {
	p.toolMu.RLock()
	defer p.toolMu.RUnlock()
	info := p.info
	info.Capabilities.ToolCall = p.toolCall
	return info
}

func (p *openAICompatibleProvider) CompleteJSON(
	ctx context.Context, system, user string,
) (string, error) {
	format := shared.NewResponseFormatJSONObjectParam()
	return p.complete(ctx, system, user,
		openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &format,
		},
	)
}

func (p *openAICompatibleProvider) CompleteAction(
	ctx context.Context,
	system, user string,
	tool ActionTool,
) (string, error) {
	p.toolMu.RLock()
	useToolCall := p.toolCall
	mode := p.toolCallMode
	p.toolMu.RUnlock()
	if !useToolCall {
		return p.CompleteJSON(ctx, system, user)
	}
	content, err := p.completeTool(ctx, system, user, tool)
	if err == nil {
		return content, nil
	}
	var requestErr *ProviderRequestError
	if mode != ToolCallAuto || !errors.As(err, &requestErr) ||
		!requestErr.ToolUnsupported {
		return "", err
	}
	p.toolMu.Lock()
	p.toolCall = false
	p.toolMu.Unlock()
	return p.CompleteJSON(ctx, system, user)
}

func (p *openAICompatibleProvider) CompleteText(
	ctx context.Context, system, user string,
) (string, error) {
	return p.complete(
		ctx, system, user,
		openai.ChatCompletionNewParamsResponseFormatUnion{},
	)
}

func (p *openAICompatibleProvider) completeTool(
	ctx context.Context,
	system, user string,
	tool ActionTool,
) (string, error) {
	if err := consumeModelRequest(ctx); err != nil {
		return "", err
	}
	params := openai.ChatCompletionNewParams{
		Model: p.info.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
		Temperature:       openai.Float(0.1),
		ParallelToolCalls: openai.Bool(false),
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:        tool.Name,
				Description: openai.String(tool.Description),
				Parameters:  shared.FunctionParameters(tool.Parameters),
			}),
		},
		ToolChoice: openai.ToolChoiceOptionFunctionToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: tool.Name},
		),
	}
	p.configureParams(&params)
	response, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", p.requestError(err)
	}
	if len(response.Choices) == 0 {
		return "", &ModelOutputError{Err: fmt.Errorf(
			"terminal AI provider %s model %s returned no choices",
			p.info.Name, p.info.Model,
		)}
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		return "", &ModelOutputError{Err: fmt.Errorf(
			"terminal AI provider %s model %s returned %d tool calls; exactly one is required",
			p.info.Name, p.info.Model, len(calls),
		)}
	}
	call := calls[0]
	if call.Type != "function" || call.Function.Name != tool.Name {
		return "", &ModelOutputError{Err: fmt.Errorf(
			"terminal AI provider %s model %s returned an unexpected tool call",
			p.info.Name, p.info.Model,
		)}
	}
	return strings.TrimSpace(call.Function.Arguments), nil
}

func (p *openAICompatibleProvider) complete(
	ctx context.Context,
	system, user string,
	responseFormat openai.ChatCompletionNewParamsResponseFormatUnion,
) (string, error) {
	if err := consumeModelRequest(ctx); err != nil {
		return "", err
	}
	params := openai.ChatCompletionNewParams{
		Model: p.info.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(user),
		},
		Temperature:    openai.Float(0.1),
		ResponseFormat: responseFormat,
	}
	p.configureParams(&params)
	response, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", p.requestError(err)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf(
			"terminal AI provider %s model %s returned no choices",
			p.info.Name, p.info.Model,
		)
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

func (p *openAICompatibleProvider) configureParams(
	params *openai.ChatCompletionNewParams,
) {
	if p.maxTokens > 0 {
		params.MaxTokens = openai.Int(p.maxTokens)
	}
	if len(p.extraFields) > 0 {
		params.SetExtraFields(p.extraFields)
	}
}

func (p *openAICompatibleProvider) requestError(err error) error {
	requestErr := &ProviderRequestError{Err: fmt.Errorf(
		"terminal AI provider %s model %s request failed: %w",
		p.info.Name, p.info.Model, err,
	)}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		requestErr.StatusCode = apiErr.StatusCode
		requestErr.Retryable = apiErr.StatusCode == http.StatusTooManyRequests ||
			apiErr.StatusCode >= http.StatusInternalServerError
		requestErr.ToolUnsupported = toolCallUnsupported(apiErr)
		return requestErr
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		requestErr.Retryable = netErr.Timeout() || netErr.Temporary()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		requestErr.Retryable = true
	}
	return requestErr
}

func toolCallUnsupported(err *openai.Error) bool {
	if err.StatusCode != http.StatusBadRequest &&
		err.StatusCode != http.StatusNotFound &&
		err.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	value := strings.ToLower(strings.Join([]string{
		err.Param, err.Code, err.Type, err.Message,
	}, " "))
	if !strings.Contains(value, "tool") &&
		!strings.Contains(value, "function") &&
		!strings.Contains(value, "parallel") {
		return false
	}
	for _, marker := range []string{
		"unsupported", "not support", "unknown", "unrecognized",
		"not allowed", "invalid parameter",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Param), "tool") ||
		strings.Contains(strings.ToLower(err.Param), "parallel")
}
