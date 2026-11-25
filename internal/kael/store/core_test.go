package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jumpserver/koko/internal/kael/component"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

type runtimeStoreClientStub func(string, uint64, bool, string) (uint64, error)

func (c runtimeStoreClientStub) AppendRuntimeStore(id string, revision uint64, snapshot bool, record string) (uint64, error) {
	return c(id, revision, snapshot, record)
}

func (runtimeStoreClientStub) LoadRuntimeStoreContext(context.Context, uint64, int) (component.RuntimeStorePage, error) {
	return component.RuntimeStorePage{}, nil
}

func TestCoreStoreCommitRecovery(t *testing.T) {
	for _, test := range []struct {
		name     string
		errors   []error
		poisoned bool
	}{
		{"refused", []error{component.ErrRuntimeStoreUnavailable, component.ErrRuntimeStoreUnavailable, component.ErrRuntimeStoreUnavailable}, false},
		{"lost_then_success", []error{component.ErrRuntimeStoreCommitUncertain, nil}, false},
		{"lost_then_refused", []error{component.ErrRuntimeStoreCommitUncertain, component.ErrRuntimeStoreUnavailable, component.ErrRuntimeStoreUnavailable}, true},
		{"revision_conflict", []error{component.ErrRuntimeStoreRevisionConflict}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var commitID, payload string
			p := &corePersistence{client: runtimeStoreClientStub(func(id string, revision uint64, snapshot bool, record string) (uint64, error) {
				if calls == 0 {
					commitID, payload = id, record
				}
				if id != commitID || record != payload || revision != 0 || snapshot {
					t.Fatal("retry changed commit identity or payload")
				}
				err := test.errors[calls]
				calls++
				return 1, err
			})}
			memory := &Memory{state: newMemoryState(), persistence: p}
			commit := func(tx ports.Tx) error {
				if err := tx.CreateConversation(&domain.Conversation{ID: "conversation"}); err != nil {
					return err
				}
				return tx.CreateMessage(&domain.Message{ID: "message", ConversationID: "conversation", Role: "user"})
			}
			ctx := context.Background()
			err := memory.Transaction(ctx, commit)
			failed := test.errors[len(test.errors)-1] != nil
			if calls != len(test.errors) || (p.poisoned != nil) != test.poisoned {
				t.Fatalf("unexpected commit outcome: attempts=%d poisoned=%v", calls, p.poisoned)
			}
			if failed {
				if !errors.Is(err, ports.ErrUnavailable) || p.revision != 0 || len(memory.state.messages) != 0 {
					t.Fatalf("failed transaction did not roll back: %v", err)
				}
				if test.poisoned {
					if err := memory.Ready(ctx); !errors.Is(err, ports.ErrUnavailable) {
						t.Fatalf("uncertain store became ready: %v", err)
					}
					return
				}
				p.client = runtimeStoreClientStub(func(_ string, revision uint64, _ bool, _ string) (uint64, error) {
					return revision + 1, nil
				})
				if err := memory.Ready(ctx); err != nil {
					t.Fatal(err)
				}
				err = memory.Transaction(ctx, commit)
			}
			if err != nil || p.revision != 1 || len(memory.state.messages) != 1 {
				t.Fatalf("transaction did not recover exactly once: %v", err)
			}
		})
	}
}
