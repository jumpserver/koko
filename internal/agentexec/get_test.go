package agentexec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func getJournal(t *testing.T) (*Journal, []Event) {
	t.Helper()
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	if err := j.PutAction(Action{ID: "action", SessionID: "session", Kind: "execute_command", Status: "queued"}, "private-capability"); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"running", "running", "succeeded"} {
		if err := j.Append("action", state, map[string]any{"encoding": "base64", "data": "aGVsbG8="}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := j.Pending(3)
	if err != nil || len(events) != 3 {
		t.Fatal("missing fixture evidence", err)
	}
	return j, events
}

func TestGetRechecksCoreWhenFlushAcknowledgesBetweenSnapshots(t *testing.T) {
	j, events := getJournal(t)
	calls := 0
	r := &Runtime{journal: j, core: coreFunc(func(_ context.Context, _ string, _ any, out any) error {
		calls++
		value := Action{ID: "action", SessionID: "session", Kind: "execute_command", Status: "running"}
		if calls == 1 {
			// Core's response snapshot precedes a successful outbox flush.
			for _, event := range events {
				if err := j.Ack(event); err != nil {
					return err
				}
			}
		} else {
			value.Status, value.LastSequence, value.Events = "succeeded", 3, events
		}
		return respond(out, value)
	})}
	result, err := r.Get(context.Background(), Principal{ContextToken: "owner"}, "action", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(result.Events) != 3 || result.EvidencePending || result.NextOffset != nil || result.AuditStatus != "delivered" {
		t.Fatalf("lost acknowledged output: %#v", result)
	}
	for i, event := range result.Events {
		if event.Sequence != uint64(i+1) || event.SessionToken != "" || event.Payload["data"] != "hello" {
			t.Fatal("returned invalid or private evidence")
		}
	}
}

func TestGetReturnsOnlyContinuousEvidenceAndRetryCursor(t *testing.T) {
	for _, prefix := range []int{0, 1} {
		t.Run(string(rune('0'+prefix)), func(t *testing.T) {
			j, events := getJournal(t)
			for _, event := range events[:2] {
				if err := j.Ack(event); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			r := &Runtime{journal: j, core: coreFunc(func(_ context.Context, _ string, _ any, out any) error {
				calls++
				return respond(out, Action{ID: "action", SessionID: "session", Kind: "execute_command", Status: "cancelled", Result: map[string]any{"source": "Core"}, LastSequence: uint64(prefix), Events: events[:prefix]})
			})}
			result, err := r.Get(context.Background(), Principal{ContextToken: "owner"}, "action", 0, 3)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || len(result.Events) != prefix || !result.EvidencePending || result.NextOffset == nil || *result.NextOffset != int64(prefix) || result.LastSequence != 3 {
				t.Fatalf("skipped a missing event: %#v", result)
			}
			if result.Status != "cancelled" || result.Result["source"] != "Core" || result.AuditStatus != "pending" {
				t.Fatal("overrode Core authority or lost pending audit state")
			}
			if prefix == 0 {
				data, _ := json.Marshal(result)
				if !strings.Contains(string(data), `"next_offset":0`) {
					t.Fatal("zero retry cursor disappeared from JSON")
				}
			}
		})
	}
}

func TestGetConnectionSuccessWaitsForCoreReadiness(t *testing.T) {
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.PutAction(Action{ID: "connection", SessionID: "session", Kind: "connect_asset", Status: "queued"}, "capability"); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("connection", "succeeded", map[string]any{"session_id": "session"}); err != nil {
		t.Fatal(err)
	}
	coreReady := false
	r := &Runtime{journal: j, core: coreFunc(func(_ context.Context, _ string, _ any, out any) error {
		value := Action{ID: "connection", SessionID: "session", Kind: "connect_asset", Status: "dispatching"}
		if coreReady {
			value.Status = "succeeded"
			value.LastSequence = 1
		}
		return respond(out, value)
	})}
	result, err := r.Get(context.Background(), Principal{ContextToken: "owner"}, "connection", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "dispatching" || !result.EvidencePending || result.NextOffset == nil || *result.NextOffset != 1 {
		t.Fatal("reported connection success before Core accepted commands")
	}
	events, _ := j.Pending(1)
	if err := j.Ack(events[0]); err != nil {
		t.Fatal(err)
	}
	coreReady = true
	result, err = r.Get(context.Background(), Principal{ContextToken: "owner"}, "connection", 1, 1)
	if err != nil || result.Status != "succeeded" || result.EvidencePending || result.NextOffset != nil {
		t.Fatal("did not finish the acknowledged connection", err)
	}
}
