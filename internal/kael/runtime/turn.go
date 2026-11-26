package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/model"
	"github.com/jumpserver/koko/internal/kael/policy"
)

type toolReply struct {
	Content []map[string]any `json:"contentItems"`
	Success bool             `json:"success"`
}

func replyText(text string, success bool) toolReply {
	return toolReply{Content: []map[string]any{{"type": "inputText", "text": text}}, Success: success}
}

type toolReceipt struct {
	fingerprint string
	reply       toolReply
}

type turnEvent struct {
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	ItemID    string          `json:"itemId"`
	Delta     string          `json:"delta"`
	CallID    string          `json:"callId"`
	Tool      string          `json:"tool"`
	Namespace string          `json:"namespace"`
	Arguments json.RawMessage `json:"arguments"`
	Item      struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Turn struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  struct {
			Info json.RawMessage `json:"codexErrorInfo"`
		} `json:"error"`
	} `json:"turn"`
	TokenUsage struct {
		Total tokenUsage `json:"total"`
	} `json:"tokenUsage"`
}

func runTurn(ctx context.Context, session *harnessSession, turnID string, input Input, contracts map[string]toolContract, callbacks Callbacks) (completion Completion, err error) {
	baseline := session.usage
	texts := map[string]string{}
	lastItem := ""
	receipts := map[string]toolReceipt{}
	writes := map[string]bool{}
	calls := 0
	finalResult := false
	emit := func(id, text string) error {
		if text == "" {
			return nil
		}
		if lastItem != "" && lastItem != id {
			text = "\n\n" + text
		}
		if len(completion.Answer)+len(text) > domain.MaxMessageBytes {
			return fmt.Errorf("Codex answer exceeds message limit")
		}
		if err := callbacks.MessageDelta(text); err != nil {
			return err
		}
		completion.Answer += text
		lastItem = id
		return nil
	}
	for {
		if ctx.Err() != nil {
			return completion, ctx.Err()
		}
		msg, readErr := session.process.next(ctx)
		if readErr != nil {
			return completion, readErr
		}
		if msg.Method == "" {
			continue
		}
		var e turnEvent
		if json.Unmarshal(msg.Params, &e) != nil {
			return completion, fmt.Errorf("Codex emitted an invalid event")
		}
		if e.ThreadID != "" && e.ThreadID != session.thread {
			return completion, fmt.Errorf("Codex event crossed thread boundary")
		}
		if e.TurnID != "" && e.TurnID != turnID {
			return completion, fmt.Errorf("Codex event crossed turn boundary")
		}
		if len(msg.ID) > 0 {
			if msg.Method == "item/tool/requestUserInput" {
				if e.ThreadID != session.thread || e.TurnID != turnID {
					return completion, fmt.Errorf("Codex question crossed execution boundary")
				}
				// No answer has been supplied by the user. Decline the built-in form;
				// the developer policy directs questions to the normal Luna chat surface.
				if err = session.process.write(map[string]any{"id": msg.ID, "result": map[string]any{"answers": map[string]any{}}}); err != nil {
					return completion, err
				}
				continue
			}
			if msg.Method != "item/tool/call" {
				_ = session.process.write(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "Only registered Kael capabilities are available. Ask the user in your final answer."}})
				return completion, fmt.Errorf("Codex requested an unsupported host capability")
			}
			if e.ThreadID != session.thread || e.TurnID != turnID || e.CallID == "" || e.Namespace != "" {
				return completion, fmt.Errorf("Codex tool binding is invalid")
			}
			fingerprint := digest([]any{e.Tool, e.Arguments})
			if receipt, ok := receipts[e.CallID]; ok {
				if receipt.fingerprint != fingerprint {
					return completion, fmt.Errorf("Codex reused a tool call id with different arguments")
				}
				if err = session.process.write(map[string]any{"id": msg.ID, "result": receipt.reply}); err != nil {
					return completion, err
				}
				continue
			}
			calls++
			if calls > domain.MaxToolCalls {
				return completion, fmt.Errorf("Codex tool call budget exhausted")
			}
			reply := replyText("Tool is unavailable in this run's registered capability snapshot.", false)
			if contract, ok := contracts[e.Tool]; ok {
				arguments, validationErr := canonicalArguments(e.Arguments)
				if validationErr == nil {
					validationErr = validateJSON(contract.input, arguments)
				}
				switch {
				case finalResult:
					reply = replyText("The final proposal decision was recorded. Make no further calls; explain the result without claiming it was saved or executed.", false)
				case validationErr != nil:
					reply = replyText("Arguments failed the registered JSON schema: "+bounded(validationErr.Error(), 1024), false)
				default:
					writeKey := toolFingerprint(contract.registration, arguments)
					if writes[writeKey] {
						reply = replyText("Duplicate write blocked. The prior operation may already have executed. Inspect actual state before proposing any different action.", false)
					} else {
						observation, callErr := callbacks.CallTool(ctx, contract.registration, arguments, 0)
						if callErr != nil {
							return completion, callErr
						}
						risk := observation.Risk
						if risk == "" {
							risk, _ = policy.InvocationPolicy(contract.registration, arguments)
						}
						if risk != "read" {
							writes[writeKey] = true
						}
						if observation.Status == "success" && contract.output != nil && len(observation.Result) > 0 {
							if err = validateJSON(contract.output, observation.Result); err != nil {
								return completion, fmt.Errorf("tool output failed registered schema validation")
							}
						}
						result := map[string]any{"status": observation.Status}
						if len(observation.Result) > 0 {
							result["result"] = observation.Result
						}
						if len(observation.Error) > 0 {
							result["error"] = observation.Error
						}
						finalResult = contract.registration.Annotations().FinalResult
						if finalResult {
							result["instruction"] = "The proposal decision is final. Explain this recorded result and stop. Applied to an editor does not mean saved or executed."
						}
						raw, encodeErr := json.Marshal(result)
						if encodeErr != nil {
							return completion, encodeErr
						}
						if len(raw) > domain.MaxToolResultBytes {
							return completion, fmt.Errorf("tool result exceeds runtime size limit")
						}
						reply = replyText(string(raw), observation.Status == "success")
					}
				}
			}
			receipts[e.CallID] = toolReceipt{fingerprint: fingerprint, reply: reply}
			if err = session.process.write(map[string]any{"id": msg.ID, "result": reply}); err != nil {
				return completion, err
			}
			continue
		}
		switch msg.Method {
		case "item/agentMessage/delta":
			if e.ThreadID != session.thread || e.TurnID != turnID || e.ItemID == "" {
				return completion, fmt.Errorf("Codex text binding is invalid")
			}
			texts[e.ItemID] += e.Delta
			if err = emit(e.ItemID, e.Delta); err != nil {
				return completion, err
			}
		case "item/completed":
			if e.Item.Type == "agentMessage" {
				previous := texts[e.Item.ID]
				if !strings.HasPrefix(e.Item.Text, previous) {
					return completion, fmt.Errorf("Codex final message disagrees with streamed text")
				}
				if err = emit(e.Item.ID, strings.TrimPrefix(e.Item.Text, previous)); err != nil {
					return completion, err
				}
				texts[e.Item.ID] = e.Item.Text
			}
		case "thread/tokenUsage/updated":
			session.usage = e.TokenUsage.Total
			completion.Usage = model.Usage{InputTokens: max(0, session.usage.Input-baseline.Input), OutputTokens: max(0, session.usage.Output-baseline.Output), CachedTokens: max(0, session.usage.Cached-baseline.Cached), ReasoningTokens: max(0, session.usage.Reasoning-baseline.Reasoning)}
		case "turn/completed":
			if e.Turn.ID != turnID {
				return completion, fmt.Errorf("Codex completion crossed turn boundary")
			}
			if e.Turn.Status == "interrupted" {
				return completion, context.Canceled
			}
			if e.Turn.Status != "completed" {
				return completion, codexFailure(e.Turn.Error.Info)
			}
			if strings.TrimSpace(completion.Answer) == "" {
				return completion, &model.ProviderError{Kind: model.ErrorInvalidOutput, Message: "Codex completed without an answer"}
			}
			completion.FinishReason = "stop"
			if finalResult {
				completion.FinishReason = "final_result"
			}
			return completion, nil
		}
	}
}

func codexFailure(info json.RawMessage) error {
	kind := model.ErrorUnavailable
	value := string(info)
	switch {
	case strings.Contains(value, "usageLimitExceeded"):
		kind = model.ErrorRateLimit
	case strings.Contains(value, "contextWindowExceeded"):
		kind = model.ErrorInvalidRequest
	case strings.Contains(value, "401") || strings.Contains(value, "403"):
		kind = model.ErrorAuthentication
	}
	return &model.ProviderError{Kind: kind, Message: "Codex agent turn failed; check provider and runtime configuration"}
}
