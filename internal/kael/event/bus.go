package event

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

const maxJavaScriptSafeInteger = uint64(9007199254740991)

type References struct {
	ConversationID string
	RunID          string
	MessageID      string
	ToolCallID     string
	ApprovalID     string
}

type Bus struct {
	mu        sync.RWMutex
	listeners map[string]map[chan struct{}]struct{}
}

func NewBus() *Bus { return &Bus{listeners: make(map[string]map[chan struct{}]struct{})} }

func (b *Bus) Subscribe(panelID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.listeners[panelID] == nil {
		b.listeners[panelID] = make(map[chan struct{}]struct{})
	}
	b.listeners[panelID][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.listeners[panelID], ch)
		if len(b.listeners[panelID]) == 0 {
			delete(b.listeners, panelID)
		}
		b.mu.Unlock()
	}
}

func (b *Bus) Notify(panelIDs ...string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, panelID := range panelIDs {
		if _, ok := seen[panelID]; ok {
			continue
		}
		seen[panelID] = struct{}{}
		for listener := range b.listeners[panelID] {
			select {
			case listener <- struct{}{}:
			default:
			}
		}
	}
}

func Project(tx ports.Tx, eventType, aggregateType, aggregateID, audience string, references References, payload any, panels []domain.PanelSession, now time.Time) (*domain.DomainEvent, []domain.PanelDelivery, error) {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > domain.MaxEventPayloadBytes {
		return nil, nil, fmt.Errorf("event payload is invalid")
	}
	event := &domain.DomainEvent{ID: uuid.NewString(), ConversationID: references.ConversationID, RunID: references.RunID, MessageID: references.MessageID, ToolCallID: references.ToolCallID, ApprovalID: references.ApprovalID, AggregateType: aggregateType, AggregateID: aggregateID, Type: eventType, SchemaVersion: "1", Payload: raw, CreatedAt: now}
	if err = tx.CreateEvent(event); err != nil {
		return nil, nil, err
	}
	deliveries := make([]domain.PanelDelivery, 0, len(panels))
	for index := range panels {
		panel, panelErr := tx.PanelInternal(panels[index].ID, true)
		if panelErr != nil {
			return nil, nil, panelErr
		}
		if panel.NextSequence >= maxJavaScriptSafeInteger {
			return nil, nil, fmt.Errorf("panel delivery sequence exhausted")
		}
		panel.NextSequence++
		panel.UpdatedAt = now
		if err = tx.SavePanel(panel); err != nil {
			return nil, nil, err
		}
		delivery := domain.PanelDelivery{
			ID: uuid.NewString(), PanelSessionID: panel.ID, Sequence: panel.NextSequence, EventID: event.ID,
			Audience: audience, Type: eventType, SchemaVersion: "1", ConversationID: references.ConversationID,
			RunID: references.RunID, MessageID: references.MessageID, ToolCallID: references.ToolCallID,
			ApprovalID: references.ApprovalID, Payload: append(json.RawMessage(nil), raw...), CreatedAt: now,
		}
		if err = tx.CreateDelivery(&delivery); err != nil {
			return nil, nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return event, deliveries, nil
}
