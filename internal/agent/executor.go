package agent

import (
	"context"
	"errors"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentauth"
	"github.com/jumpserver/koko/internal/agentruntime"
)

type ModelFactory = agentruntime.ModelFactory

var errRuntimeUnavailable = errors.New("agent runtime is unavailable")

type modelRuntime struct {
	runtime *agentruntime.Runtime
}

func newModelRuntime(session *agentSession, factory ModelFactory) (*modelRuntime, error) {
	profile, ok := runtimeProfilePolicyFor(session.profile)
	if !ok {
		return nil, errRuntimeUnavailable
	}
	runtime, err := agentruntime.New(
		agentruntime.Config{
			Profile: session.profile, TrustedProfileInstructions: profile.instructions,
			Context: session.context,
			Tools:   session.tools, MaxRounds: 20,
		},
		factory,
		agentruntime.Callbacks{
			Started:        session.startRun,
			History:        session.runHistory,
			EmitModelEvent: session.emitModelEvent,
			CallTool:       session.callTool,
			Complete:       session.completeRun,
			Fail:           session.failRun,
		},
	)
	if err != nil {
		return nil, errRuntimeUnavailable
	}
	return &modelRuntime{runtime: runtime}, nil
}

func (r *modelRuntime) start(
	runID, messageID, question string,
	metadata map[string]any,
) error {
	return r.runtime.Start(runID, messageID, question, metadata)
}

func (r *modelRuntime) cancelRun(runID string) bool {
	return r.runtime.Cancel(runID)
}

func (r *modelRuntime) close() {
	r.runtime.Close()
}

func (s *agentSession) callTool(
	ctx context.Context,
	request agentruntime.ToolRequest,
) (agentruntime.ToolObservation, error) {
	if err := ctx.Err(); err != nil {
		return agentruntime.ToolObservation{}, err
	}
	tool, ok := s.tool(request.ToolName)
	if !ok {
		return agentruntime.ToolObservation{}, errToolUnavailable
	}
	toolCallID, err := randomID()
	if err != nil {
		return agentruntime.ToolObservation{}, err
	}
	canonicalArguments, err := agentauth.CanonicalJSON(request.Arguments)
	if err != nil {
		return agentruntime.ToolObservation{}, err
	}
	argsHash, err := agentauth.HashRawJSON(canonicalArguments)
	if err != nil {
		return agentruntime.ToolObservation{}, err
	}
	if s.needsApproval(tool, request.ApprovalRequired) {
		digest, _ := agentauth.HashValue(map[string]any{
			"resource_session_id": s.resourceID,
			"run_id":              request.RunID, "tool_call_id": toolCallID,
			"tool_name": tool.Name, "args_hash": argsHash,
			"revision": s.revision,
		})
		approved, approvalErr := s.awaitApproval(
			ctx, request.RunID, toolCallID, digest, tool.Name,
			canonicalArguments, request.ApprovalSummary, request.ModelDurationMS,
		)
		if approvalErr != nil {
			return agentruntime.ToolObservation{}, approvalErr
		}
		if !approved {
			return agentruntime.ToolObservation{
				ToolCallID: toolCallID, ToolName: tool.Name, Status: agentruntime.ToolStatusRejected,
				Error: &agentapi.JSONRPCError{
					Code: -32001, Message: "User rejected the tool call.",
				},
			}, nil
		}
	}
	if err = s.publishToolCall(
		request.RunID, request.MessageID, toolCallID, tool.Name, canonicalArguments,
		request.ModelDurationMS,
	); err != nil {
		return agentruntime.ToolObservation{}, err
	}
	var (
		after       uint64
		observation = agentruntime.ToolObservation{
			ToolCallID: toolCallID, ToolName: tool.Name,
		}
	)
	for {
		result, waitErr := s.results.next(ctx, toolCallID, after)
		if waitErr != nil {
			return observation, waitErr
		}
		after = result.Sequence
		observation.Status = result.Status
		observation.Result = result.Result
		observation.Error = result.Error
		if result.Done {
			return observation, nil
		}
	}
}
