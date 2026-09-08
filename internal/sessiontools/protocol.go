package sessiontools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MCPProtocolVersion = 1
	MCPToolsetRevision = 1
	MCPWireVersion     = "2026-07-28"

	MCPAgentMetaKey      = "com.jumpserver/agent"
	MCPServerInfoMetaKey = "io.modelcontextprotocol/serverInfo"

	MCPManifestFrame     = "mcp.manifest"
	MCPRequestFrame      = "mcp.request"
	MCPResponseFrame     = "mcp.response"
	MCPCancelFrame       = "mcp.cancel"
	MCPCancelResultFrame = "mcp.cancel_result"

	maxMCPIdentifierBytes = 128
	maxMCPRequestBytes    = 256 * 1024
	maxMCPActiveCalls     = 4
	maxMCPCompletedCalls  = 128
	maxMCPTextResultBytes = 4 * 1024
)

type MCPToolDefinition struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description,omitempty"`
	Icons        []ToolIcon     `json:"icons,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations,omitempty"`
	Meta         map[string]any `json:"_meta,omitempty"`
}

type MCPManifest struct {
	Version           int                 `json:"version"`
	ResourceSessionID string              `json:"resource_session_id"`
	Profile           string              `json:"profile"`
	Context           ContextSnapshot     `json:"context,omitempty"`
	Revision          int                 `json:"revision"`
	Tools             []MCPToolDefinition `json:"tools"`
}

type MCPToolHandler interface {
	Definition() MCPToolDefinition
	Call(context.Context, json.RawMessage) (any, error)
}

type mcpToolCloser interface {
	Close() error
}

type MCPRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type MCPResponse struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      string       `json:"id"`
	Result  any          `json:"result,omitempty"`
	Error   *MCPRPCError `json:"error,omitempty"`
}

type MCPTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MCPCallToolResult is the standard tools/call result forwarded by Luna.
type MCPCallToolResult struct {
	Content           []MCPTextContent `json:"content"`
	StructuredContent any              `json:"structuredContent,omitempty"`
	IsError           bool             `json:"isError,omitempty"`
	Meta              map[string]any   `json:"_meta"`
}

type MCPOutbound struct {
	Type string
	Data []byte
}

type mcpRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      string           `json:"id"`
	Method  string           `json:"method"`
	Params  mcpRequestParams `json:"params"`
}

type mcpRequestParams struct {
	Name      string                     `json:"name"`
	Arguments json.RawMessage            `json:"arguments"`
	Meta      map[string]json.RawMessage `json:"_meta"`
}

type mcpCancelRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  mcpCancelParams `json:"params"`
}

type mcpCancelParams struct {
	RequestID string                     `json:"requestId,omitempty"`
	Reason    string                     `json:"reason,omitempty"`
	Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
}

type mcpAgentBinding struct {
	ResourceSessionID string `json:"resource_session_id"`
	ToolCallID        string `json:"tool_call_id"`
	Revision          int    `json:"revision,omitempty"`
}

type mcpCancelResponse struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      string       `json:"id"`
	Result  any          `json:"result,omitempty"`
	Error   *MCPRPCError `json:"error,omitempty"`
}

type mcpActiveCall struct {
	request     mcpRequest
	cancel      context.CancelFunc
	fingerprint string
	cancelled   bool
}

type mcpCompletedCall struct {
	fingerprint string
	requestID   string
	response    []byte
	cancelled   bool
}

type MCPDispatcherOptions struct {
	ResourceSessionID string
	Profile           string
	Context           ContextSnapshot
	Handlers          []MCPToolHandler
	Emit              func(MCPOutbound)
}

// MCPDispatcher validates and executes calls for one resource session. It is
// deliberately connection-bound: no agent/model state is kept here.
type MCPDispatcher struct {
	resourceSessionID string
	emit              func(MCPOutbound)
	manifest          MCPManifest
	handlers          map[string]MCPToolHandler
	lifetimeCtx       context.Context
	cancelLife        context.CancelFunc

	mu             sync.Mutex
	emitMu         sync.Mutex
	closed         bool
	active         map[string]*mcpActiveCall
	completed      map[string]mcpCompletedCall
	completedOrder []string
	wg             sync.WaitGroup
}

func NewMCPDispatcher(
	ctx context.Context,
	options MCPDispatcherOptions,
) (*MCPDispatcher, error) {
	resourceID := strings.TrimSpace(options.ResourceSessionID)
	if err := validateMCPIdentifier("resource_session_id", resourceID); err != nil {
		return nil, err
	}
	if options.Emit == nil {
		return nil, errors.New("MCP emitter is required")
	}
	if options.Profile != "terminal" && options.Profile != "file" {
		return nil, errors.New("MCP profile must be terminal or file")
	}
	handlers := make(map[string]MCPToolHandler, len(options.Handlers))
	definitions := make([]MCPToolDefinition, 0, len(options.Handlers))
	for _, handler := range options.Handlers {
		if handler == nil {
			return nil, errors.New("MCP tool handler is nil")
		}
		definition := handler.Definition()
		if err := validateMCPIdentifier("tool name", definition.Name); err != nil {
			return nil, err
		}
		if _, exists := handlers[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate MCP tool %q", definition.Name)
		}
		handlers[definition.Name] = handler
		definitions = append(definitions, definition)
	}
	manifest := MCPManifest{
		Version: MCPProtocolVersion, ResourceSessionID: resourceID,
		Profile: options.Profile, Context: options.Context,
		Revision: MCPToolsetRevision,
		Tools:    definitions,
	}
	lifetimeCtx, cancelLife := context.WithCancel(ctx)
	return &MCPDispatcher{
		resourceSessionID: resourceID,
		emit:              options.Emit,
		manifest:          manifest,
		handlers:          handlers,
		lifetimeCtx:       lifetimeCtx,
		cancelLife:        cancelLife,
		active:            make(map[string]*mcpActiveCall),
		completed:         make(map[string]mcpCompletedCall),
	}, nil
}

func (d *MCPDispatcher) AnnounceManifest() error {
	payload, err := json.Marshal(d.manifest)
	if err != nil {
		return fmt.Errorf("marshal MCP manifest: %w", err)
	}
	d.emit(MCPOutbound{Type: MCPManifestFrame, Data: payload})
	return nil
}

func (d *MCPDispatcher) HandleRequest(data []byte) error {
	var request mcpRequest
	if err := decodeMCPMessage(data, &request); err != nil {
		return err
	}
	if request.JSONRPC != "2.0" || request.Method != "tools/call" {
		return errors.New("invalid MCP tools/call request")
	}
	if err := validateMCPIdentifier("JSON-RPC id", request.ID); err != nil {
		return err
	}
	params := request.Params
	agentBinding, err := decodeMCPAgentBinding(params.Meta, true)
	if err != nil {
		return err
	}
	if agentBinding.ResourceSessionID != d.resourceSessionID {
		return errors.New("MCP resource session does not match")
	}
	if err = validateMCPIdentifier("tool_call_id", agentBinding.ToolCallID); err != nil {
		return err
	}
	if agentBinding.Revision != MCPToolsetRevision {
		return errors.New("MCP toolset revision does not match")
	}
	if err := validateMCPIdentifier("tool name", params.Name); err != nil {
		return err
	}
	if !isJSONObject(params.Arguments) {
		return errors.New("MCP tool arguments must be a JSON object")
	}
	handler := d.handlers[params.Name]
	if handler == nil {
		return fmt.Errorf("unknown MCP tool %q", params.Name)
	}
	fingerprint := requestFingerprint(request)
	callKey := d.resourceSessionID + "\x00" + agentBinding.ToolCallID
	// Serialize state transitions that can emit a response with cancellation
	// acknowledgements. This guarantees that a response either reaches the
	// transport before a late cancellation is acknowledged, or is suppressed
	// after an accepted cancellation.
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return errors.New("MCP dispatcher is closed")
	}
	if completed, ok := d.completed[callKey]; ok {
		d.mu.Unlock()
		if completed.fingerprint != fingerprint {
			return errors.New("MCP tool_call_id was reused with different parameters")
		}
		if completed.cancelled {
			return nil
		}
		d.emit(MCPOutbound{Type: MCPResponseFrame, Data: completed.response})
		return nil
	}
	if active, ok := d.active[callKey]; ok {
		d.mu.Unlock()
		if active.fingerprint != fingerprint {
			return errors.New("MCP tool_call_id is active with different parameters")
		}
		return nil
	}
	if len(d.active) >= maxMCPActiveCalls {
		d.mu.Unlock()
		return errors.New("too many active MCP tool calls")
	}
	callCtx, cancel := context.WithCancel(d.lifetimeCtx)
	call := &mcpActiveCall{
		request: request, cancel: cancel, fingerprint: fingerprint,
	}
	d.active[callKey] = call
	d.wg.Add(1)
	d.mu.Unlock()
	go d.execute(callCtx, callKey, call, handler)
	return nil
}

func (d *MCPDispatcher) HandleCancel(data []byte) error {
	var request mcpCancelRequest
	if err := decodeMCPMessage(data, &request); err != nil {
		return err
	}
	if request.JSONRPC != "2.0" {
		return errors.New("invalid MCP cancellation")
	}
	if request.Method != "notifications/cancelled" {
		return errors.New("invalid MCP cancellation method")
	}
	if request.ID != "" {
		return errors.New("MCP cancellation notification must not have an id")
	}
	params := request.Params
	if err := validateMCPIdentifier("cancel requestId", params.RequestID); err != nil {
		return err
	}
	agentBinding, err := decodeMCPAgentBinding(params.Meta, true)
	if err != nil {
		return err
	}
	if agentBinding.ResourceSessionID != d.resourceSessionID {
		return errors.New("MCP resource session does not match")
	}
	if err = validateMCPIdentifier("tool_call_id", agentBinding.ToolCallID); err != nil {
		return err
	}
	if agentBinding.Revision != MCPToolsetRevision {
		return errors.New("MCP toolset revision does not match")
	}
	callKey := d.resourceSessionID + "\x00" + agentBinding.ToolCallID
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	d.mu.Lock()
	call := d.active[callKey]
	completedCall, completed := d.completed[callKey]
	if call == nil && !completed {
		d.mu.Unlock()
		return errors.New("MCP cancellation target was not found")
	}
	if (call != nil && call.request.ID != params.RequestID) ||
		(completed && completedCall.requestID != params.RequestID) {
		d.mu.Unlock()
		return errors.New("MCP cancellation requestId does not match the tool call")
	}
	completedWasCancelled := completedCall.cancelled
	if call != nil {
		call.cancelled = true
		call.cancel()
	}
	if call == nil && completed {
		// A late cancellation cannot retract a response that was already queued,
		// but it must prevent any transport-level replay after the acknowledgement.
		completedCall.cancelled = true
		d.completed[callKey] = completedCall
	}
	d.mu.Unlock()
	state := "completed"
	if call != nil {
		state = "cancelling"
	} else if completedWasCancelled {
		state = "cancelled"
	}
	response := mcpCancelResponse{
		JSONRPC: "2.0", ID: params.RequestID,
		Result: map[string]any{
			"resource_session_id": d.resourceSessionID,
			"tool_call_id":        agentBinding.ToolCallID,
			"state":               state,
		},
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	d.emit(MCPOutbound{Type: MCPCancelResultFrame, Data: payload})
	return nil
}

func (d *MCPDispatcher) execute(
	ctx context.Context,
	callKey string,
	call *mcpActiveCall,
	handler MCPToolHandler,
) {
	defer d.wg.Done()
	result, callErr := handler.Call(ctx, call.request.Params.Arguments)
	response := MCPResponse{JSONRPC: "2.0", ID: call.request.ID}
	toolResult, resultPayload := newMCPCallToolResult(result, callErr)
	if len(resultPayload) == 0 || len(resultPayload) > MaxToolResultBytes {
		toolResult, resultPayload = newMCPCallToolResult(
			nil, errors.New("MCP tool result exceeded the response limit"),
		)
	}
	response.Result = toolResult
	payload, err := json.Marshal(response)
	if err != nil {
		return
	}
	d.emitMu.Lock()
	defer d.emitMu.Unlock()
	d.mu.Lock()
	cancelled := call.cancelled
	delete(d.active, callKey)
	d.rememberCompletedLocked(callKey, mcpCompletedCall{
		fingerprint: call.fingerprint,
		requestID:   call.request.ID, response: payload, cancelled: cancelled,
	})
	closed := d.closed
	d.mu.Unlock()
	if !closed && !cancelled {
		d.emit(MCPOutbound{Type: MCPResponseFrame, Data: payload})
	}
}

func (d *MCPDispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.cancelLife()
	for _, call := range d.active {
		call.cancel()
	}
	d.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	for _, handler := range d.handlers {
		if closer, ok := handler.(mcpToolCloser); ok {
			_ = closer.Close()
		}
	}
}

func (d *MCPDispatcher) rememberCompletedLocked(
	key string,
	call mcpCompletedCall,
) {
	if _, exists := d.completed[key]; !exists {
		d.completedOrder = append(d.completedOrder, key)
	}
	d.completed[key] = call
	for len(d.completedOrder) > maxMCPCompletedCalls {
		oldest := d.completedOrder[0]
		d.completedOrder = d.completedOrder[1:]
		delete(d.completed, oldest)
	}
}

func decodeMCPMessage(data []byte, output any) error {
	if len(data) == 0 || len(data) > maxMCPRequestBytes {
		return errors.New("invalid MCP message size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode MCP message: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("MCP message has trailing data")
	}
	return nil
}

func validateMCPIdentifier(name, value string) error {
	if strings.TrimSpace(value) != value || value == "" ||
		len(value) > maxMCPIdentifierBytes || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("invalid MCP %s", name)
	}
	return nil
}

func isJSONObject(value json.RawMessage) bool {
	value = bytes.TrimSpace(value)
	if len(value) < 2 || value[0] != '{' || value[len(value)-1] != '}' {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil
}

func decodeMCPAgentBinding(
	meta map[string]json.RawMessage,
	requireRevision bool,
) (mcpAgentBinding, error) {
	var binding mcpAgentBinding
	raw := meta[MCPAgentMetaKey]
	if len(raw) == 0 {
		return binding, errors.New("MCP agent binding metadata is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&binding); err != nil {
		return binding, fmt.Errorf("decode MCP agent binding: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return binding, errors.New("MCP agent binding has trailing data")
	}
	if requireRevision && binding.Revision == 0 {
		return binding, errors.New("MCP agent binding revision is required")
	}
	var version string
	if json.Unmarshal(
		meta["io.modelcontextprotocol/protocolVersion"], &version,
	) != nil || version != MCPWireVersion {
		return binding, errors.New("MCP wire protocol version does not match")
	}
	var capabilities map[string]any
	if json.Unmarshal(
		meta["io.modelcontextprotocol/clientCapabilities"], &capabilities,
	) != nil || capabilities == nil {
		return binding, errors.New("MCP client capabilities metadata is required")
	}
	if clientInfo := meta["io.modelcontextprotocol/clientInfo"]; len(clientInfo) > 0 {
		var value map[string]any
		if json.Unmarshal(clientInfo, &value) != nil || value == nil {
			return binding, errors.New("MCP client info metadata is invalid")
		}
	}
	return binding, nil
}

func newMCPCallToolResult(
	value any,
	callErr error,
) (MCPCallToolResult, json.RawMessage) {
	result := MCPCallToolResult{
		Meta: map[string]any{
			MCPServerInfoMetaKey: map[string]any{
				"name": "jumpserver-koko", "version": "1",
			},
		},
	}
	if callErr != nil {
		message := callErr.Error()
		if len(message) > 4096 {
			message = message[:4096]
		}
		result.IsError = true
		if errors.Is(callErr, context.DeadlineExceeded) {
			result.Meta[MCPAgentMetaKey] = map[string]any{"status": "timeout", "code": "tool_timeout"}
		} else if errors.Is(callErr, context.Canceled) {
			result.Meta[MCPAgentMetaKey] = map[string]any{"status": "cancelled", "code": "tool_cancelled"}
		}
		result.Content = []MCPTextContent{{Type: "text", Text: message}}
		payload, _ := json.Marshal(result)
		return result, payload
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return newMCPCallToolResult(nil, errors.New("MCP tool result is not serializable"))
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err = decoder.Decode(&normalized); err != nil {
		return newMCPCallToolResult(nil, errors.New("MCP tool result is not serializable"))
	}
	normalized = normalizeJSSafeNumbers(normalized)
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return newMCPCallToolResult(nil, errors.New("MCP tool result is not serializable"))
	}
	textResult := string(encoded)
	if len(textResult) > maxMCPTextResultBytes {
		textResult = "Tool completed successfully; the complete result is available in structuredContent."
	}
	result.Content = []MCPTextContent{{Type: "text", Text: textResult}}
	if structured, ok := normalized.(map[string]any); ok && structured != nil {
		result.StructuredContent = structured
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return MCPCallToolResult{}, nil
	}
	return result, payload
}

func normalizeJSSafeNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		raw := typed.String()
		const safeIntegerLimit = "9007199254740991"
		if strings.ContainsAny(raw, ".eE") {
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsInf(parsed, 0) ||
				(math.Trunc(parsed) == parsed && math.Abs(parsed) > 9007199254740991) {
				return raw
			}
			return typed
		}
		digits := raw
		if strings.HasPrefix(digits, "-") {
			digits = digits[1:]
		}
		if digits == "" {
			return typed
		}
		for _, digit := range digits {
			if digit < '0' || digit > '9' {
				return typed
			}
		}
		digits = strings.TrimLeft(digits, "0")
		if digits == "" {
			return typed
		}
		if len(digits) > len(safeIntegerLimit) ||
			(len(digits) == len(safeIntegerLimit) && digits > safeIntegerLimit) {
			return raw
		}
		return typed
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeJSSafeNumbers(item)
		}
		return typed
	case []any:
		for index := range typed {
			typed[index] = normalizeJSSafeNumbers(typed[index])
		}
		return typed
	default:
		return value
	}
}

func requestFingerprint(request mcpRequest) string {
	value, err := HashValue(request)
	if err != nil {
		return ""
	}
	return value
}
