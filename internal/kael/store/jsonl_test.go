package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

func TestJSONLStorePersistsHistoryAndInterruptsProcessState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	principal := domain.Principal{SubjectID: "user-1", OrganizationID: "org-1"}
	now := time.Now().UTC()
	options := RetentionOptions{KeepDays: 7, MaxBytes: 1 << 20}
	value, err := NewJSONL(root, options)
	if err != nil {
		t.Fatal(err)
	}
	err = value.Transaction(ctx, func(tx ports.Tx) error {
		if err := tx.CreateConversation(&domain.Conversation{ID: "conversation-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Kind: "capability", Profile: "script", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.CreatePanel(&domain.PanelSession{ID: "panel-1", ConversationID: "conversation-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Profile: "script", State: "active", LeaseExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := tx.CreateRun(&domain.Run{ID: "run-1", ConversationID: "conversation-1", PanelSessionID: "panel-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, State: "queued", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		return tx.CreateEvent(&domain.DomainEvent{ID: "event-1", ConversationID: "conversation-1", RunID: "run-1", AggregateType: "run", AggregateID: "run-1", Type: "run.queued", SchemaVersion: "1", Payload: json.RawMessage(`{"state":"queued"}`), CreatedAt: now})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = value.Close(); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "events", "conversation-1.jsonl")
	archive, err := os.OpenFile(archivePath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = archive.WriteString("{\"seq\":999,\"conversation_id\":\"conversation-1\",\"type\":\"orphan\"}\n"); err != nil {
		_ = archive.Close()
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}

	value, err = NewJSONL(root, options)
	if err != nil {
		t.Fatal(err)
	}
	defer value.Close()
	err = value.View(ctx, func(tx ports.Tx) error {
		conversation, err := tx.Conversation("conversation-1", principal, false)
		if err != nil || conversation.Status != "active" {
			t.Fatalf("conversation = %#v, err = %v", conversation, err)
		}
		panel, err := tx.Panel("panel-1", principal, false)
		if err != nil || panel.State != "expired" {
			t.Fatalf("panel = %#v, err = %v", panel, err)
		}
		run, err := tx.Run("run-1", principal, false)
		if err != nil || run.State != "interrupted" || run.ErrorCode != "process_restarted" {
			t.Fatalf("run = %#v, err = %v", run, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEventArchive(t, archivePath, 2)
}

func assertEventArchive(t *testing.T, path string, expected int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var event domain.DomainEvent
		if err = json.Unmarshal(scanner.Bytes(), &event); err != nil || event.ConversationID == "" || event.Sequence == 0 {
			t.Fatalf("invalid archived event: %s", scanner.Text())
		}
		count++
	}
	if err = scanner.Err(); err != nil || count != expected {
		t.Fatalf("archived event count = %d, err = %v", count, err)
	}
}
