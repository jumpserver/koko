package store

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
)

// Audit is a public, versioned JSON projection alongside the opaque runtime
// payload. Core applies it in the same transaction as the signed journal commit.
type auditProjection struct {
	Version       int                 `json:"version"`
	Conversations []auditConversation `json:"conversations"`
	Messages      []auditMessage      `json:"messages"`
}

type auditConversation struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"org_id"`
	SubjectID       string    `json:"subject_id"`
	SubjectName     string    `json:"subject_name"`
	SubjectUsername string    `json:"subject_username"`
	Title           string    `json:"title"`
	Assistant       string    `json:"assistant"`
	Profile         string    `json:"profile"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"date_created"`
	UpdatedAt       time.Time `json:"date_updated"`
}

type auditMessage struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	Status         string    `json:"status"`
	Error          string    `json:"error"`
	InputTokens    int64     `json:"input_tokens"`
	OutputTokens   int64     `json:"output_tokens"`
	CreatedAt      time.Time `json:"date_created"`
}

func conversationAudit(value domain.Conversation) auditConversation {
	return auditConversation{value.ID, value.OrganizationID, value.SubjectID, value.SubjectName, value.SubjectUsername, value.Title, value.Assistant, value.Profile, value.Status, value.CreatedAt, value.UpdatedAt}
}

func messageAudit(value domain.Message) auditMessage {
	return auditMessage{value.ID, value.ConversationID, value.Role, value.Content, value.Status, value.ErrorDetail, value.InputTokens, value.OutputTokens, value.CreatedAt}
}

func auditChanges(previous, next *memoryState) *auditProjection {
	questioned := make(map[string]bool)
	for _, message := range next.messages {
		if message.Role == "user" {
			questioned[message.ConversationID] = true
		}
	}
	conversations := make(map[string]auditConversation)
	result := &auditProjection{Version: 1, Conversations: []auditConversation{}, Messages: []auditMessage{}}
	for id, value := range next.conversations {
		if !questioned[id] {
			continue
		}
		prior, exists := previous.conversations[id]
		if !exists || conversationAudit(prior) != conversationAudit(value) {
			conversations[id] = conversationAudit(value)
		}
	}
	for id, value := range next.messages {
		conversation, exists := next.conversations[value.ConversationID]
		if !exists || !questioned[value.ConversationID] || value.OrganizationID != conversation.OrganizationID {
			continue
		}
		if value.Role != "user" && !finalAssistantMessage(value.Role, value.Status) {
			continue
		}
		prior, exists := previous.messages[id]
		if exists && messageAudit(prior) == messageAudit(value) {
			continue
		}
		conversations[conversation.ID] = conversationAudit(conversation)
		result.Messages = append(result.Messages, messageAudit(value))
	}
	for _, value := range conversations {
		result.Conversations = append(result.Conversations, value)
	}
	sort.Slice(result.Conversations, func(i, j int) bool { return result.Conversations[i].ID < result.Conversations[j].ID })
	sort.Slice(result.Messages, func(i, j int) bool { return result.Messages[i].ID < result.Messages[j].ID })
	if len(result.Conversations) == 0 && len(result.Messages) == 0 {
		return nil
	}
	return result
}

func encodeCoreJournalRecord(payload journalPayload, audit *auditProjection) ([]byte, error) {
	line, err := encodeJournalRecord(payload)
	if err != nil || audit == nil {
		return line, err
	}
	var record map[string]json.RawMessage
	if err = json.Unmarshal(line, &record); err != nil {
		return nil, err
	}
	record["audit"], err = json.Marshal(audit)
	if err != nil {
		return nil, err
	}
	line, err = json.Marshal(record)
	return append(line, '\n'), err
}

// Backfill on startup also makes an interrupted upgrade resumable. Chunks use
// the existing commit IDs/revision checks, and Core upserts stable resource IDs.
func (p *corePersistence) BackfillAudit(state *memoryState) error {
	audit := auditChanges(newMemoryState(), state)
	if audit == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(audit.Conversations) > 0 || len(audit.Messages) > 0 {
		batch := &auditProjection{Version: 1, Conversations: []auditConversation{}, Messages: []auditMessage{}}
		if len(audit.Conversations) > 0 {
			n := min(100, len(audit.Conversations))
			batch.Conversations, audit.Conversations = audit.Conversations[:n], audit.Conversations[n:]
		} else {
			n := min(50, len(audit.Messages))
			batch.Messages, audit.Messages = audit.Messages[:n], audit.Messages[n:]
		}
		line, err := encodeCoreJournalRecord(journalPayload{Delta: &persistentDelta{}}, batch)
		if err != nil {
			return err
		}
		if err = p.appendLocked(false, line); err != nil {
			return err
		}
	}
	return nil
}
