package httpd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/sashabaranov/go-openai"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/srvconn"
)

var _ Handler = (*chat)(nil)

type chat struct {
	ws *UserWebsocket

	// conversations: map[conversationID]*AIConversation
	conversations sync.Map

	term *model.TerminalConfig
}

func (h *chat) Name() string {
	return ChatName
}

func (h *chat) CleanUp() { h.cleanupAll() }

func (h *chat) CheckValidation() error {
	return nil
}

func (h *chat) HandleMessage(msg *Message) {
	if msg.Interrupt {
		h.interrupt(msg.Id)
		return
	}

	conv, err := h.getOrCreateConversation(msg)
	if err != nil {
		h.sendError(msg.Id, err.Error())
		return
	}
	conv.Question = msg.Data
	conv.NewDialogue = true

	go h.runChat(conv)
}

func (h *chat) getOrCreateConversation(msg *Message) (*AIConversation, error) {
	if msg.Id != "" {
		if v, ok := h.conversations.Load(msg.Id); ok {
			return v.(*AIConversation), nil
		}
		return nil, fmt.Errorf("conversation %s not found", msg.Id)
	}

	conv := &AIConversation{
		Id:      common.UUID(),
		Prompt:  msg.Prompt,
		Context: make([]QARecord, 0),
	}
	h.conversations.Store(conv.Id, conv)
	return conv, nil
}

func (h *chat) runChat(conv *AIConversation) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := srvconn.NewOpenAIClient(
		h.term.GptApiKey, h.term.GptBaseUrl, h.term.GptProxy,
	)

	// Keep the last 8 contexts
	if len(conv.Context) > 8 {
		conv.Context = conv.Context[len(conv.Context)-8:]
	}
	messages := buildChatMessages(conv)

	chatModel := conv.Model
	if conv.Model == "" {
		chatModel = h.term.GptModel
	}

	conn := &srvconn.OpenAIConn{
		Id:          conv.Id,
		Client:      client,
		Prompt:      conv.Prompt,
		Model:       chatModel,
		Question:    conv.Question,
		Context:     messages,
		AnswerCh:    make(chan string),
		DoneCh:      make(chan string),
		IsReasoning: false,
		Type:        h.term.ChatAIType,
	}

	// 启动 streaming
	go conn.Chat(&conv.InterruptCurrentChat)

	h.streamResponses(ctx, conv, conn)
}

func buildChatMessages(conv *AIConversation) []openai.ChatCompletionMessage {
	chatMessages := make([]openai.ChatCompletionMessage, 0, len(conv.Context)*2)
	for _, r := range conv.Context {
		chatMessages = append(chatMessages,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: r.Question},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: r.Answer},
		)
	}
	return chatMessages
}

func (h *chat) streamResponses(
	ctx context.Context, conv *AIConversation, conn *srvconn.OpenAIConn,
) {
	msgID := common.UUID()
	for {
		select {
		case <-ctx.Done():
			h.sendError(conv.Id, "chat timeout")
			return
		case ans := <-conn.AnswerCh:
			h.sendMessage(conv.Id, msgID, ans, "message", conn.IsReasoning)
		case ans := <-conn.DoneCh:
			h.sendMessage(conv.Id, msgID, ans, "finish", false)
			h.finalizeConversation(conv, ans)
			return
		}
	}
}

func (h *chat) finalizeConversation(conv *AIConversation, fullAnswer string) {
	runes := []rune(fullAnswer)
	snippet := fullAnswer
	if len(runes) > 100 {
		snippet = string(runes[:100])
	}
	conv.Context = append(conv.Context, QARecord{Question: conv.Question, Answer: snippet})

}

func (h *chat) sendMessage(
	convID, msgID, content, typ string, reasoning bool,
) {
	msg := ChatGPTMessage{
		Content:     content,
		ID:          msgID,
		CreateTime:  time.Now(),
		Type:        typ,
		Role:        openai.ChatMessageRoleAssistant,
		IsReasoning: reasoning,
	}
	data, _ := json.Marshal(msg)
	h.ws.SendMessage(&Message{Id: convID, Type: "message", Data: string(data)})
}

func (h *chat) sendError(convID, errMsg string) {
	h.endConversation(convID, "error", errMsg)
}

func (h *chat) endConversation(convID, typ, msg string) {

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("panic while sending message to session %s: %v", convID, r)
		}
	}()

	h.conversations.Delete(convID)
	h.ws.SendMessage(&Message{Id: convID, Type: typ, Data: msg})
}

func (h *chat) interrupt(convID string) {
	if v, ok := h.conversations.Load(convID); ok {
		v.(*AIConversation).InterruptCurrentChat = true
	}
}

func (h *chat) cleanupAll() {
	h.conversations.Range(func(key, _ interface{}) bool {
		h.endConversation(key.(string), "close", "")
		return true
	})
}
