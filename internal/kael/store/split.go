package store

import (
	"context"
	"errors"
	"fmt"
)

// splitPersistence routes Terminal AI state to local JSONL storage and keeps
// the user-visible conversation history for every other profile in Core.
type splitPersistence struct {
	core     *corePersistence
	terminal *jsonlPersistence
}

func (p *splitPersistence) Commit(previous, next *memoryState) error {
	audit := auditChanges(previous, next)
	previousTerminal := terminalPersistableState(previous)
	nextTerminal := terminalPersistableState(next)
	if err := p.terminal.Commit(previousTerminal, nextTerminal); err != nil {
		return fmt.Errorf("persist Terminal AI runtime state: %w", err)
	}
	removed := make(map[string]struct{})
	for id, conversation := range next.conversations {
		if _, exists := nextTerminal.conversations[id]; conversation.Profile == "terminal" && !exists {
			removed[id] = struct{}{}
		}
	}
	removeConversationSubgraphs(next, removed)
	if err := p.core.commitWithAudit(previous, next, audit); err != nil {
		return fmt.Errorf("persist Core runtime state: %w", err)
	}
	return nil
}

func (p *splitPersistence) Ready(ctx context.Context) error {
	if err := p.core.Ready(ctx); err != nil {
		return err
	}
	return p.terminal.Ready(ctx)
}

func (p *splitPersistence) RuntimeMetrics() map[string]int64 {
	return p.core.RuntimeMetrics()
}

func (p *splitPersistence) Close() error {
	return errors.Join(p.terminal.Close(), p.core.Close())
}

func mergeMemoryStates(states ...*memoryState) *memoryState {
	result := newMemoryState()
	for _, state := range states {
		if state == nil {
			continue
		}
		mergeStateMap(result.conversations, state.conversations)
		mergeStateMap(result.messages, state.messages)
		mergeStateMap(result.artifacts, state.artifacts)
		mergeStateMap(result.panels, state.panels)
		mergeStateMap(result.contexts, state.contexts)
		mergeStateMap(result.registrations, state.registrations)
		mergeStateMap(result.runs, state.runs)
		mergeStateMap(result.modelCalls, state.modelCalls)
		mergeStateMap(result.toolCalls, state.toolCalls)
		mergeStateMap(result.toolResults, state.toolResults)
		mergeStateMap(result.approvals, state.approvals)
		mergeStateMap(result.events, state.events)
		mergeStateMap(result.eventHighWater, state.eventHighWater)
		mergeStateMap(result.deliveries, state.deliveries)
		mergeStateMap(result.deliveryHighWater, state.deliveryHighWater)
		mergeStateMap(result.audits, state.audits)
	}
	return result
}

func mergeStateMap[K comparable, V any](target, source map[K]V) {
	for key, value := range source {
		target[key] = cloneStoredValue(value)
	}
}
