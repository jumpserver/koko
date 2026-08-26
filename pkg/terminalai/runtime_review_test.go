package terminalai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCommandACLReviewRequiresUserApproval(t *testing.T) {
	for _, test := range []struct {
		name     string
		approved bool
	}{
		{name: "approved", approved: true},
		{name: "rejected", approved: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			messages := make(chan ChatMessage, 8)
			reviewStarted := make(chan struct{})
			runtime := NewRuntime(
				1, nil, nil, func([]byte) {},
				func(message ChatMessage) { messages <- message },
			)
			t.Cleanup(runtime.Close)
			runtime.approvalThreshold = 4
			runtime.SetCommandACL(
				func(string) CommandACLDecision {
					return CommandACLDecision{
						Action: "review", ACLID: "acl-1", ItemID: "item-1",
					}
				},
				func(
					_ context.Context,
					decision CommandACLDecision,
					_ string,
				) (CommandACLDecision, error) {
					close(reviewStarted)
					decision.Action = "accept"
					decision.Reviewed = true
					return decision, nil
				},
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan struct {
				proposal CommandProposal
				err      error
			}, 1)
			go func() {
				proposal, err := runtime.authorize(
					ctx, "plan-1", "execution-1",
					Step{ID: "step-1"}, 0, 1,
					CommandProposal{
						Command: "review-command", RiskLevel: 1,
						Execution: ExecutionPTY,
					},
					0,
				)
				result <- struct {
					proposal CommandProposal
					err      error
				}{proposal: proposal, err: err}
			}()

			approval := waitForCommandApproval(t, messages)
			select {
			case <-reviewStarted:
				t.Fatal("command ACL review started before user approval")
			default:
			}
			approval.Approved = test.approved
			approval.Execution = ExecutionPTY
			if err := runtime.resolveApproval(approval); err != nil {
				t.Fatalf("resolve command approval: %v", err)
			}

			select {
			case value := <-result:
				if test.approved {
					if value.err != nil {
						t.Fatalf("authorize command: %v", value.err)
					}
					if value.proposal.CommandACL == nil ||
						value.proposal.CommandACL.Action != "accept" ||
						!value.proposal.CommandACL.Reviewed {
						t.Fatalf("reviewed ACL decision = %#v", value.proposal.CommandACL)
					}
				} else if value.err == nil ||
					!strings.Contains(value.err.Error(), "approval was rejected") {
					t.Fatalf("authorization error = %v", value.err)
				}
			case <-time.After(time.Second):
				t.Fatal("command authorization did not finish")
			}
			select {
			case <-reviewStarted:
				if !test.approved {
					t.Fatal("command ACL review started after user rejection")
				}
			default:
				if test.approved {
					t.Fatal("command ACL review did not start after user approval")
				}
			}
		})
	}
}

func TestSQLMetadataApprovalCanCoverCurrentDatabaseSession(t *testing.T) {
	messages := make(chan ChatMessage, 2)
	runtime := NewRuntime(
		1, nil, nil, func([]byte) {},
		func(message ChatMessage) { messages <- message },
	)
	t.Cleanup(runtime.Close)
	result := make(chan error, 1)
	go func() {
		approved, err := runtime.authorizeMetadata(
			context.Background(), "app", SQLSchemaLookupRequest{Tables: []string{"users"}},
		)
		if err == nil && !approved {
			err = errors.New("metadata approval was rejected")
		}
		result <- err
	}()

	var data map[string]any
	deadline := time.After(time.Second)
	for data == nil {
		select {
		case message := <-messages:
			if message.Parts[0].Type == "data-metadata-approval" {
				data = message.Parts[0].Data.(map[string]any)
			}
		case <-deadline:
			t.Fatal("metadata approval was not requested")
		}
	}
	decision := metadataApprovalDecision{
		ID: data["id"].(string), Digest: data["digest"].(string),
		Decision: "approve_session",
	}
	if err := runtime.resolveMetadataApproval(decision); err != nil {
		t.Fatalf("resolve metadata approval: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("authorize metadata: %v", err)
	}
	if approved, err := runtime.authorizeMetadata(
		context.Background(), "app", SQLSchemaLookupRequest{Tables: []string{"orders"}},
	); err != nil || !approved {
		t.Fatalf("reuse session approval: %t, %v", approved, err)
	}
}

func waitForCommandApproval(
	t *testing.T,
	messages <-chan ChatMessage,
) approvalDecision {
	t.Helper()
	select {
	case message := <-messages:
		if len(message.Parts) != 1 || message.Parts[0].Type != "data-approval" {
			t.Fatalf("authorization message = %#v", message)
		}
		data, ok := message.Parts[0].Data.(map[string]any)
		if !ok {
			t.Fatalf("approval data = %#v", message.Parts[0].Data)
		}
		id, idOK := data["id"].(string)
		digest, digestOK := data["digest"].(string)
		if !idOK || !digestOK {
			t.Fatalf("approval identity = %#v", data)
		}
		return approvalDecision{ID: id, Digest: digest}
	case <-time.After(time.Second):
		t.Fatal("command approval was not requested")
		return approvalDecision{}
	}
}
