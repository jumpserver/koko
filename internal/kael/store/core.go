package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/component"
	"github.com/jumpserver/koko/internal/kael/ports"
)

const (
	coreStorePageSize       = 1000
	coreStoreCommitAttempts = 3
	coreStoreMaxRecordBytes = 8 * 1024 * 1024
)

type coreRuntimeStoreClient interface {
	AppendRuntimeStore(string, uint64, bool, string) (uint64, error)
	LoadRuntimeStoreContext(context.Context, uint64, int) (component.RuntimeStorePage, error)
}

type corePersistence struct {
	mu                   sync.Mutex
	client               coreRuntimeStoreClient
	revision             uint64
	recordsSinceSnapshot int
	poisoned             error
}

func NewCore(client *component.Client, runtimeRoot string, options ...RetentionOptions) (*Memory, error) {
	if client == nil {
		return nil, fmt.Errorf("Core runtime store client is required")
	}
	core, loadedCore, err := openCorePersistence(client)
	if err != nil {
		return nil, err
	}
	terminal, loadedTerminal, err := openJSONLRoot(filepath.Join(runtimeRoot, "terminal"), options...)
	if err != nil {
		return nil, fmt.Errorf("open Terminal AI runtime store: %w", err)
	}
	closeTerminal := true
	defer func() {
		if closeTerminal {
			_ = terminal.Close()
		}
	}()

	coreState := corePersistableState(loadedCore)
	rewriteCoreHistory := !reflect.DeepEqual(loadedCore, coreState)
	loadedTerminal = terminalPersistableState(loadedTerminal)
	terminalState := loadedTerminal
	if terminal.records == 0 {
		terminalState = mergeMemoryStates(
			terminalPersistableState(loadedCore),
			loadedTerminal,
		)
		if !reflect.DeepEqual(loadedTerminal, terminalState) {
			if err = terminal.Commit(loadedTerminal, terminalState); err != nil {
				return nil, fmt.Errorf("migrate Terminal AI runtime state: %w", err)
			}
		}
	}
	if rewriteCoreHistory {
		if err = core.ReplaceSnapshot(coreState); err != nil {
			return nil, fmt.Errorf("compact Core conversation history: %w", err)
		}
	}

	if err = core.BackfillAudit(mergeMemoryStates(coreState, terminalState)); err != nil {
		return nil, fmt.Errorf("backfill Core audit history: %w", err)
	}
	now := time.Now().UTC()
	coreState, err = recoverPersistence(core, coreState, now)
	if err != nil {
		return nil, fmt.Errorf("recover Core runtime state: %w", err)
	}
	previousTerminal := terminalState
	terminalState, err = recoverPersistence(terminal, terminalState, now)
	if err != nil {
		return nil, fmt.Errorf("recover Terminal AI runtime state: %w", err)
	}
	if err = core.commitWithAudit(coreState, coreState, auditChanges(previousTerminal, terminalState)); err != nil {
		return nil, fmt.Errorf("audit recovered Terminal AI state: %w", err)
	}
	closeTerminal = false
	return &Memory{
		state:       mergeMemoryStates(coreState, terminalState),
		persistence: &splitPersistence{core: core, terminal: terminal},
	}, nil
}

func recoverPersistence(persistence statePersistence, state *memoryState, now time.Time) (*memoryState, error) {
	next := state.clone()
	recoverProcessLocalState(next, now)
	if reflect.DeepEqual(state, next) {
		return state, nil
	}
	if err := persistence.Commit(state, next); err != nil {
		return nil, err
	}
	return next, nil
}

func openCorePersistence(client *component.Client) (*corePersistence, *memoryState, error) {
	state := newMemoryState()
	after := uint64(0)
	recordsSinceSnapshot := 0
	for {
		page, err := client.LoadRuntimeStore(after, coreStorePageSize)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range page.Results {
			if item.Revision <= after {
				return nil, nil, fmt.Errorf("Core runtime store returned non-increasing revision %d", item.Revision)
			}
			if item.Revision != after+1 && !item.Snapshot {
				return nil, nil, fmt.Errorf("Core runtime store skipped from revision %d to non-snapshot revision %d", after, item.Revision)
			}
			payload, err := decodeCoreRecord(item.Record)
			if err != nil {
				return nil, nil, fmt.Errorf("decode Core runtime store revision %d: %w", item.Revision, err)
			}
			if item.Snapshot != (payload.Snapshot != nil) {
				return nil, nil, fmt.Errorf("Core runtime store revision %d has inconsistent snapshot metadata", item.Revision)
			}
			if payload.Snapshot != nil {
				state = payload.Snapshot.memoryState()
				recordsSinceSnapshot = 0
			}
			if payload.Delta != nil {
				applyPersistentDelta(state, *payload.Delta)
				recordsSinceSnapshot++
			}
			after = item.Revision
		}
		if page.HasMore {
			if len(page.Results) == 0 {
				return nil, nil, fmt.Errorf("Core runtime store pagination did not advance")
			}
			continue
		}
		if page.Revision != after {
			return nil, nil, fmt.Errorf("Core runtime store load ended at revision %d, expected %d", after, page.Revision)
		}
		return &corePersistence{client: client, revision: after, recordsSinceSnapshot: recordsSinceSnapshot}, state, nil
	}
}

func decodeCoreRecord(line string) (journalPayload, error) {
	line = strings.TrimSuffix(line, "\n")
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return journalPayload{}, fmt.Errorf("runtime store record must be one non-empty line")
	}
	var record journalRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return journalPayload{}, fmt.Errorf("runtime store record is invalid: %w", err)
	}
	if record.Version != journalVersion {
		return journalPayload{}, fmt.Errorf("runtime store record version %d is unsupported", record.Version)
	}
	payload, err := decodeJournalPayload(record)
	if err != nil {
		return journalPayload{}, err
	}
	if (payload.Snapshot == nil) == (payload.Delta == nil) {
		return journalPayload{}, fmt.Errorf("runtime store record must contain exactly one snapshot or delta")
	}
	return payload, nil
}

func (p *corePersistence) Commit(previous, next *memoryState) error {
	return p.commitWithAudit(previous, next, auditChanges(previous, next))
}

func (p *corePersistence) commitWithAudit(previous, next *memoryState, audit *auditProjection) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned != nil {
		return p.unavailableLocked()
	}
	previous = corePersistableState(previous)
	next = corePersistableState(next)
	delta := diffPersistentState(previous, next)
	if delta.empty() && audit == nil {
		return nil
	}
	line, err := encodeCoreJournalRecord(journalPayload{Delta: &delta}, audit)
	if err != nil {
		return err
	}
	return p.appendLocked(false, line)
}

// ReplaceSnapshot immediately replaces an older Core runtime snapshot with the
// durable conversation-history projection. This is used once when loading a
// store written by a version that persisted process-local runtime state.
func (p *corePersistence) ReplaceSnapshot(state *memoryState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned != nil {
		return p.unavailableLocked()
	}
	projected := persistentStateFromMemory(corePersistableState(state))
	line, err := encodeJournalRecord(journalPayload{Snapshot: &projected})
	if err != nil {
		return err
	}
	return p.appendLocked(true, line)
}

func (p *corePersistence) appendLocked(snapshot bool, line []byte) error {
	if len(line) > coreStoreMaxRecordBytes {
		return fmt.Errorf("%w: Core history record is %d bytes, limit is %d", errJournalRecordTooLarge, len(line), coreStoreMaxRecordBytes)
	}
	record := strings.TrimSuffix(string(line), "\n")
	commitID := uuid.NewString()
	var revision uint64
	var err error
	uncertainSeen := false
	for attempt := 0; attempt < coreStoreCommitAttempts; attempt++ {
		revision, err = p.client.AppendRuntimeStore(commitID, p.revision, snapshot, record)
		if err == nil {
			break
		}
		if errors.Is(err, component.ErrRuntimeStoreRevisionConflict) {
			return p.poisonLocked(fmt.Errorf("runtime store revision conflict: %v", err))
		}
		uncertain := errors.Is(err, component.ErrRuntimeStoreCommitUncertain)
		unavailable := errors.Is(err, component.ErrRuntimeStoreUnavailable)
		if !uncertain && !unavailable {
			if uncertainSeen {
				return p.poisonLocked(fmt.Errorf("%w: retry after uncertain outcome failed: %v", component.ErrRuntimeStoreCommitUncertain, err))
			}
			return err
		}
		uncertainSeen = uncertainSeen || uncertain
		if attempt+1 == coreStoreCommitAttempts {
			if uncertainSeen {
				if !uncertain {
					err = fmt.Errorf("%w: last retry failed before sending: %v", component.ErrRuntimeStoreCommitUncertain, err)
				}
				return p.poisonLocked(err)
			}
			// No attempt could have committed. Leave the store usable for
			// the next call after Core recovers.
			return fmt.Errorf("%w: %w", ports.ErrUnavailable, err)
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
	}
	p.revision = revision
	if snapshot {
		p.recordsSinceSnapshot = 0
	} else {
		p.recordsSinceSnapshot++
	}
	return nil
}

// corePersistableState is the durable, user-visible conversation history stored
// in Core. It keeps questions, finalized assistant answers (including result
// cards), and artifacts attached to those messages. Process-local execution
// state is deliberately excluded: runs cannot be resumed safely after restart,
// and panels, contexts, registrations, events, deliveries, approvals, tool
// records, and runtime audits are not required to render conversation history.
// Terminal AI continues to use its separate local JSONL store.
func corePersistableState(state *memoryState) *memoryState {
	result := newMemoryState()
	durableConversations := make(map[string]struct{})
	for _, message := range state.messages {
		if message.Role != "user" {
			continue
		}
		conversation, exists := state.conversations[message.ConversationID]
		if exists && conversation.Profile != "terminal" {
			durableConversations[message.ConversationID] = struct{}{}
		}
	}
	for id := range durableConversations {
		result.conversations[id] = cloneStoredValue(state.conversations[id])
	}

	durableMessages := make(map[string]struct{})
	for id, message := range state.messages {
		if _, durable := durableConversations[message.ConversationID]; !durable {
			continue
		}
		if message.Role != "user" && !finalAssistantMessage(message.Role, message.Status) {
			continue
		}
		result.messages[id] = cloneStoredValue(message)
		durableMessages[id] = struct{}{}
	}
	for id, artifact := range state.artifacts {
		if _, durable := durableMessages[artifact.MessageID]; durable {
			result.artifacts[id] = cloneStoredValue(artifact)
		}
	}
	return result
}

func finalAssistantMessage(role, status string) bool {
	if role != "assistant" {
		return false
	}
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminalPersistableState(state *memoryState) *memoryState {
	result := state.clone()
	nonTerminalConversations := make(map[string]struct{})
	for id, conversation := range result.conversations {
		if conversation.Profile != "terminal" {
			nonTerminalConversations[id] = struct{}{}
		}
	}
	return removeConversationSubgraphs(result, nonTerminalConversations)
}

func removeConversationSubgraphs(result *memoryState, conversations map[string]struct{}) *memoryState {
	for id := range conversations {
		delete(result.conversations, id)
	}
	if len(conversations) == 0 {
		return result
	}

	transientMessages := make(map[string]struct{})
	for id, message := range result.messages {
		if _, transient := conversations[message.ConversationID]; transient {
			transientMessages[id] = struct{}{}
			delete(result.messages, id)
		}
	}
	for id, artifact := range result.artifacts {
		if _, transient := transientMessages[artifact.MessageID]; transient {
			delete(result.artifacts, id)
		}
	}

	transientPanels := make(map[string]struct{})
	for id, panel := range result.panels {
		if _, transient := conversations[panel.ConversationID]; transient {
			transientPanels[id] = struct{}{}
			delete(result.panels, id)
		}
	}
	for id, snapshot := range result.contexts {
		if _, transient := transientPanels[snapshot.PanelSessionID]; transient {
			delete(result.contexts, id)
		}
	}
	for id, registration := range result.registrations {
		if _, transient := transientPanels[registration.PanelSessionID]; transient {
			delete(result.registrations, id)
		}
	}

	transientRuns := make(map[string]struct{})
	for id, run := range result.runs {
		if _, transient := conversations[run.ConversationID]; transient {
			transientRuns[id] = struct{}{}
			delete(result.runs, id)
		}
	}
	for id, call := range result.modelCalls {
		if _, transient := transientRuns[call.RunID]; transient {
			delete(result.modelCalls, id)
		}
	}

	transientToolCalls := make(map[string]struct{})
	for id, call := range result.toolCalls {
		_, transientConversation := conversations[call.ConversationID]
		_, transientRun := transientRuns[call.RunID]
		_, transientPanel := transientPanels[call.PanelSessionID]
		if transientConversation || transientRun || transientPanel {
			transientToolCalls[id] = struct{}{}
			delete(result.toolCalls, id)
		}
	}
	for id, value := range result.toolResults {
		_, transientCall := transientToolCalls[value.ToolCallID]
		_, transientRun := transientRuns[value.RunID]
		_, transientPanel := transientPanels[value.PanelSessionID]
		if transientCall || transientRun || transientPanel {
			delete(result.toolResults, id)
		}
	}

	transientApprovals := make(map[string]struct{})
	for id, approval := range result.approvals {
		_, transientConversation := conversations[approval.ConversationID]
		_, transientRun := transientRuns[approval.RunID]
		_, transientPanel := transientPanels[approval.PanelSessionID]
		_, transientCall := transientToolCalls[approval.ToolCallID]
		if transientConversation || transientRun || transientPanel || transientCall {
			transientApprovals[id] = struct{}{}
			delete(result.approvals, id)
		}
	}

	transientEvents := make(map[string]struct{})
	for id, value := range result.events {
		if _, transient := conversations[value.ConversationID]; transient {
			transientEvents[id] = struct{}{}
			delete(result.events, id)
		}
	}
	for id := range conversations {
		delete(result.eventHighWater, id)
	}
	for id, delivery := range result.deliveries {
		_, transientConversation := conversations[delivery.ConversationID]
		_, transientPanel := transientPanels[delivery.PanelSessionID]
		_, transientRun := transientRuns[delivery.RunID]
		_, transientMessage := transientMessages[delivery.MessageID]
		_, transientCall := transientToolCalls[delivery.ToolCallID]
		_, transientApproval := transientApprovals[delivery.ApprovalID]
		_, transientEvent := transientEvents[delivery.EventID]
		if transientConversation || transientPanel || transientRun || transientMessage || transientCall || transientApproval || transientEvent {
			delete(result.deliveries, id)
		}
	}
	for id := range transientPanels {
		delete(result.deliveryHighWater, id)
	}

	for id, audit := range result.audits {
		_, transientConversation := conversations[audit.ConversationID]
		_, transientPanel := transientPanels[audit.PanelSessionID]
		_, transientRun := transientRuns[audit.RunID]
		_, transientCall := transientToolCalls[audit.ToolCallID]
		_, transientApproval := transientApprovals[audit.ApprovalID]
		if transientConversation || transientPanel || transientRun || transientCall || transientApproval {
			delete(result.audits, id)
		}
	}
	return result
}

func (p *corePersistence) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.poisoned != nil {
		return p.unavailableLocked()
	}
	page, err := p.client.LoadRuntimeStoreContext(ctx, p.revision, 1)
	if err != nil {
		return fmt.Errorf("check Core runtime store: %w", err)
	}
	if page.Revision != p.revision || len(page.Results) > 0 || page.HasMore {
		return p.poisonLocked(fmt.Errorf("Core runtime store advanced from local revision %d to %d", p.revision, page.Revision))
	}
	return nil
}

func (p *corePersistence) poisonLocked(err error) error {
	if p.poisoned == nil {
		p.poisoned = err
	}
	return p.unavailableLocked()
}

func (p *corePersistence) unavailableLocked() error {
	return fmt.Errorf("%w: Core runtime store requires restart: %v", ports.ErrUnavailable, p.poisoned)
}

func (p *corePersistence) RuntimeMetrics() map[string]int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]int64{
		"runtime_store_snapshot_disabled":      1,
		"runtime_store_revision":               int64(p.revision),
		"runtime_store_records_since_snapshot": int64(p.recordsSinceSnapshot),
	}
}

func (p *corePersistence) Close() error { return nil }
