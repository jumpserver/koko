package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

func TestMemoryTransactionRollback(t *testing.T) {
	storage := NewMemory()
	conversation := domain.Conversation{ID: "conversation-1"}
	errExpected := errors.New("stop")
	err := storage.Transaction(context.Background(), func(tx ports.Tx) error {
		if err := tx.CreateConversation(&conversation); err != nil {
			return err
		}
		return errExpected
	})
	if !errors.Is(err, errExpected) {
		t.Fatalf("unexpected transaction error: %v", err)
	}
	err = storage.View(context.Background(), func(tx ports.Tx) error {
		_, err := tx.ConversationByOrganization(conversation.ID, "")
		return err
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("rollback left conversation in store: %v", err)
	}
}

func TestMemoryClaimSerializesConversation(t *testing.T) {
	storage := NewMemory()
	now := time.Now().UTC()
	runs := []domain.Run{
		{ID: "run-1", ConversationID: "conversation-1", State: "queued", CreatedAt: now},
		{ID: "run-2", ConversationID: "conversation-1", State: "queued", CreatedAt: now.Add(time.Second)},
	}
	if err := storage.Transaction(context.Background(), func(tx ports.Tx) error {
		for index := range runs {
			if err := tx.CreateRun(&runs[index]); err != nil {
				return err
			}
		}
		claimed, err := tx.ClaimRun("kael-1", now, now.Add(time.Minute))
		if err != nil || claimed.ID != "run-1" {
			t.Fatalf("unexpected first claim: %#v %v", claimed, err)
		}
		_, err = tx.ClaimRun("kael-1", now, now.Add(time.Minute))
		return err
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("second run was claimed concurrently: %v", err)
	}
}

func TestMemoryListsConversationsByKind(t *testing.T) {
	storage := NewMemory()
	principal := domain.Principal{SubjectID: "user-1", OrganizationID: "org-1"}
	conversations := []domain.Conversation{
		{ID: "general-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Kind: "general"},
		{ID: "capability-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Kind: "capability"},
	}
	if err := storage.Transaction(context.Background(), func(tx ports.Tx) error {
		for index := range conversations {
			if err := tx.CreateConversation(&conversations[index]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.View(context.Background(), func(tx ports.Tx) error {
		values, count, err := tx.ListConversations(principal, "general", 0, 10)
		if err != nil {
			return err
		}
		if count != 1 || len(values) != 1 || values[0].ID != "general-1" {
			t.Fatalf("unexpected general conversations: %#v count=%d", values, count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryDeliveryCursorExpiresAfterRetention(t *testing.T) {
	storage := NewMemory()
	now := time.Now().UTC()
	if err := storage.Transaction(context.Background(), func(tx ports.Tx) error {
		if err := tx.CreateEvent(&domain.DomainEvent{ID: "event-1", ConversationID: "conversation-1", AggregateType: "run", AggregateID: "run-1", Type: "run.completed", SchemaVersion: "1", CreatedAt: now.Add(-time.Hour)}); err != nil {
			return err
		}
		if err := tx.CreateDelivery(&domain.PanelDelivery{ID: "delivery-1", PanelSessionID: "panel-1", Sequence: 2, EventID: "event-1", CreatedAt: now.Add(-time.Hour)}); err != nil {
			return err
		}
		return tx.Maintain(now, now.Add(-time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	if err := storage.View(context.Background(), func(tx ports.Tx) error {
		_, expired, err := tx.ListDeliveries("panel-1", 1, 10, now.Add(-time.Minute))
		if err != nil {
			return err
		}
		if !expired {
			t.Fatal("expected expired cursor")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, exists := storage.state.events["event-1"]; !exists {
		t.Fatal("domain event was removed with the SSE delivery")
	}
}
