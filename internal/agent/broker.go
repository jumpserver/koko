package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentauth"
)

const (
	maxToolCallsPerSession = 4096
	maxUpdatesPerToolCall  = 128
)

var (
	errToolResultConflict = errors.New("tool result conflicts with an accepted result")
	errToolResultGap      = errors.New("tool result sequence is not contiguous")
	errToolResultLimit    = errors.New("tool result registry limit reached")
	errToolResultUnknown  = errors.New("tool result does not match a pending tool call")
)

type safeToolResult struct {
	RunID      string                 `json:"run_id"`
	ToolCallID string                 `json:"tool_call_id"`
	Sequence   uint64                 `json:"seq"`
	Done       bool                   `json:"done"`
	Status     string                 `json:"status"`
	Result     json.RawMessage        `json:"result,omitempty"`
	Error      *agentapi.JSONRPCError `json:"error,omitempty"`
}

type toolResultState struct {
	runID        string
	lastSeq      uint64
	done         bool
	cancelled    bool
	fingerprints map[uint64]string
	updates      []safeToolResult
	notify       chan struct{}
}

type toolResultRegistry struct {
	mu    sync.Mutex
	calls map[string]*toolResultState
}

func newToolResultRegistry() *toolResultRegistry {
	return &toolResultRegistry{calls: make(map[string]*toolResultState)}
}

// accept serializes durable append and state advancement. A retry only becomes
// duplicate after the first result is safely in the JSONL event store.
func (r *toolResultRegistry) accept(
	request agentapi.ToolResultRequest,
	appendEvent func(safeToolResult) (agentapi.Event, error),
) (duplicate bool, cursor uint64, err error) {
	result := toSafeToolResult(request)
	fingerprint, err := agentauth.HashValue(result)
	if err != nil {
		return false, 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.calls[request.ID]
	if state == nil {
		return false, 0, errToolResultUnknown
	}
	if state.runID != request.RunID {
		return false, 0, errToolResultConflict
	}
	if state.cancelled {
		return true, 0, nil
	}
	if accepted, ok := state.fingerprints[request.Sequence]; ok {
		if accepted != fingerprint {
			return false, 0, errToolResultConflict
		}
		return true, 0, nil
	}
	if state.done {
		return false, 0, errToolResultConflict
	}
	if request.Sequence != state.lastSeq+1 {
		return false, 0, errToolResultGap
	}
	if len(state.fingerprints) >= maxUpdatesPerToolCall {
		return false, 0, errToolResultLimit
	}
	event, err := appendEvent(result)
	if err != nil {
		return false, 0, err
	}
	state.lastSeq = request.Sequence
	state.done = request.Done
	state.fingerprints[request.Sequence] = fingerprint
	state.updates = append(state.updates, result)
	close(state.notify)
	state.notify = make(chan struct{})
	return false, event.Sequence, nil
}

func (r *toolResultRegistry) restore(result safeToolResult) error {
	fingerprint, err := agentauth.HashValue(result)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.calls[result.ToolCallID]
	if state == nil {
		if len(r.calls) >= maxToolCallsPerSession {
			return errToolResultLimit
		}
		state = &toolResultState{
			runID: result.RunID, fingerprints: make(map[uint64]string),
			notify: make(chan struct{}),
		}
		r.calls[result.ToolCallID] = state
	}
	if state.runID != result.RunID {
		return errToolResultConflict
	}
	if result.Sequence != state.lastSeq+1 {
		return errToolResultGap
	}
	state.lastSeq = result.Sequence
	state.done = result.Done
	state.fingerprints[result.Sequence] = fingerprint
	// A restarted active run is interrupted, so historical payloads need not be
	// queued for a model that no longer exists.
	return nil
}

func (r *toolResultRegistry) next(
	ctx context.Context,
	toolCallID string,
	after uint64,
) (safeToolResult, error) {
	for {
		r.mu.Lock()
		state := r.calls[toolCallID]
		if state != nil {
			for index, update := range state.updates {
				if update.Sequence > after {
					state.updates = append(state.updates[:index], state.updates[index+1:]...)
					r.mu.Unlock()
					return update, nil
				}
			}
			if state.done {
				r.mu.Unlock()
				return safeToolResult{}, io.EOF
			}
			notify := state.notify
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return safeToolResult{}, ctx.Err()
			case <-notify:
			}
			continue
		}
		r.mu.Unlock()
		return safeToolResult{}, fmt.Errorf("tool call %q has no result channel", toolCallID)
	}
}

func (r *toolResultRegistry) begin(runID, toolCallID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if runID == "" || toolCallID == "" {
		return errToolResultUnknown
	}
	if _, exists := r.calls[toolCallID]; exists {
		return fmt.Errorf("tool call %q already exists", toolCallID)
	}
	if len(r.calls) >= maxToolCallsPerSession {
		return errToolResultLimit
	}
	r.calls[toolCallID] = &toolResultState{
		runID: runID, fingerprints: make(map[uint64]string), notify: make(chan struct{}),
	}
	return nil
}

func (r *toolResultRegistry) abandon(runID, toolCallID string) {
	r.mu.Lock()
	state := r.calls[toolCallID]
	if state != nil && state.runID == runID && state.lastSeq == 0 {
		delete(r.calls, toolCallID)
		close(state.notify)
	}
	r.mu.Unlock()
}

func (r *toolResultRegistry) cancel(toolCallID string) {
	r.mu.Lock()
	state := r.calls[toolCallID]
	if state != nil && !state.done {
		state.done = true
		state.cancelled = true
		close(state.notify)
		state.notify = make(chan struct{})
	}
	r.mu.Unlock()
}

func toSafeToolResult(request agentapi.ToolResultRequest) safeToolResult {
	return safeToolResult{
		RunID: request.RunID, ToolCallID: request.ID,
		Sequence: request.Sequence, Done: request.Done, Status: request.Status,
		Result: append(json.RawMessage(nil), request.Result...), Error: request.Error,
	}
}
