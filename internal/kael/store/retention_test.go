package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

func retentionStore(t *testing.T) (*Memory, *jsonlPersistence, string) {
	t.Helper()
	root := t.TempDir()
	s, err := NewJSONL(root, RetentionOptions{KeepDays: 7, MaxBytes: 1 << 20, MinFreeBytes: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	p := s.persistence.(*jsonlPersistence)
	p.freeBytes = func(string) (int64, error) { return 1 << 30, nil }
	return s, p, root
}

func addHistory(t *testing.T, s *Memory, id, profile string, at time.Time) {
	t.Helper()
	err := s.Transaction(context.Background(), func(tx ports.Tx) error {
		if err := tx.CreateConversation(&domain.Conversation{ID: id, Profile: profile, CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		if err := tx.CreateMessage(&domain.Message{ID: id, ConversationID: id, Role: "user", Status: "completed", Content: "question", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"content": strings.Repeat("q", 16*1024)})
		return tx.CreateEvent(&domain.DomainEvent{ID: id, ConversationID: id, Type: "message.created", Payload: payload, CreatedAt: at})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRetentionPrunesStateAndArchiveWithoutResurrection(t *testing.T) {
	s, p, root := retentionStore(t)
	old := time.Now().Add(-8 * 24 * time.Hour)
	addHistory(t, s, "expired", "terminal", old)
	addHistory(t, s, "live", "terminal", old)
	addHistory(t, s, "other", "general", old)
	err := s.Transaction(context.Background(), func(tx ports.Tx) error {
		if err := tx.CreatePanel(&domain.PanelSession{ID: "live", ConversationID: "live", State: "active", LeaseExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		if err := tx.CreateRun(&domain.Run{ID: "expired", ConversationID: "expired", State: "completed"}); err != nil {
			return err
		}
		if err := tx.CreateToolCall(&domain.ToolCall{ID: "expired", ConversationID: "expired", RunID: "expired"}); err != nil {
			return err
		}
		return tx.CreateToolResult(&domain.ToolResult{ID: "expired", ToolCallID: "expired", RunID: "expired"})
	})
	if err != nil {
		t.Fatal(err)
	}
	p.prunedAt = time.Time{}
	if err = s.Transaction(context.Background(), func(ports.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(s.state.conversations) != 2 || len(s.state.messages) != 2 || len(s.state.events) != 2 || len(s.state.runs)+len(s.state.toolCalls)+len(s.state.toolResults) != 0 {
		t.Fatal("retention did not remove exactly the expired terminal subgraph")
	}
	archive := filepath.Join(root, "events", "expired.jsonl")
	if _, err = os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("expired archive still exists: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after snapshot replacement but before archive deletion.
	if err = os.WriteFile(archive, []byte("orphan\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewJSONL(root, p.retention)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, exists := reopened.state.conversations["expired"]; exists {
		t.Fatal("expired conversation resurrected")
	}
	if _, err = os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("orphan archive survived restart: %v", err)
	}
}

func TestRetentionEvictsOldestBeforeAgeLimit(t *testing.T) {
	s, p, _ := retentionStore(t)
	addHistory(t, s, "oldest", "terminal", time.Now().Add(-time.Hour))
	addHistory(t, s, "recent", "terminal", time.Now())
	addHistory(t, s, "running", "terminal", time.Now().Add(-2*time.Hour))
	if err := s.Transaction(context.Background(), func(tx ports.Tx) error {
		return tx.CreateRun(&domain.Run{ID: "running", ConversationID: "running", State: "running"})
	}); err != nil {
		t.Fatal(err)
	}
	p.retention.MaxBytes = p.diskBytes * 10 / 9
	p.prunedAt = time.Time{}
	if err := s.Transaction(context.Background(), func(ports.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, exists := s.state.conversations["oldest"]; exists {
		t.Fatal("oldest inactive conversation was not evicted")
	}
	if _, exists := s.state.conversations["running"]; !exists {
		t.Fatal("active run history was evicted")
	}
}

func TestStorageCapacityRejectsBeforeWritingAndRecovers(t *testing.T) {
	for _, reason := range []string{"disk", "quota"} {
		t.Run(reason, func(t *testing.T) {
			s, p, _ := retentionStore(t)
			options := p.retention
			if reason == "disk" {
				p.freeBytes = func(string) (int64, error) { return 0, nil }
			} else {
				p.retention.MaxBytes = 1
			}
			create := func(tx ports.Tx) error {
				return tx.CreateConversation(&domain.Conversation{ID: "new", Profile: "terminal"})
			}
			if err := s.Transaction(context.Background(), create); !errors.Is(err, ports.ErrCapacity) {
				t.Fatalf("expected capacity rejection, got %v", err)
			}
			if len(s.state.conversations) != 0 || p.size != 0 || p.poisoned != nil {
				t.Fatal("rejected write mutated state, disk, or poisoned the adapter")
			}
			p.retention = options
			p.freeBytes = func(string) (int64, error) { return 1 << 30, nil }
			if err := s.Transaction(context.Background(), create); err != nil {
				t.Fatalf("write did not recover without restart: %v", err)
			}
		})
	}
}

func TestRetentionCleanupCanUseDiskReserve(t *testing.T) {
	s, p, _ := retentionStore(t)
	addHistory(t, s, "old", "terminal", time.Now().Add(-8*24*time.Hour))
	p.retention.MinFreeBytes = 1 << 20
	p.freeBytes = func(string) (int64, error) { return 64 << 10, nil }
	p.prunedAt = time.Time{}
	if err := s.Transaction(context.Background(), func(ports.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(s.state.conversations) != 0 {
		t.Fatal("low disk reserve prevented cleanup")
	}
}

func TestSplitRetentionPublishesPrunedState(t *testing.T) {
	s, p, _ := retentionStore(t)
	addHistory(t, s, "old", "terminal", time.Now().Add(-8*24*time.Hour))
	// Core has no history changes in this transaction and needs no network call.
	s.state.conversations["core"] = domain.Conversation{ID: "core", Profile: "general"}
	s.state.messages["core"] = domain.Message{ID: "core", ConversationID: "core", Role: "user"}
	s.persistence = &splitPersistence{core: &corePersistence{}, terminal: p}
	p.prunedAt = time.Time{}
	if err := s.Transaction(context.Background(), func(ports.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, exists := s.state.conversations["old"]; exists {
		t.Fatal("split store retained the pruned terminal conversation in memory")
	}
	if _, exists := s.state.messages["core"]; !exists {
		t.Fatal("terminal retention removed Core history")
	}
}
