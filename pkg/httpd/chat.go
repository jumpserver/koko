package httpd

import (
	"encoding/json"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/sashabaranov/go-openai"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

var _ Handler = (*chat)(nil)

type chat struct {
	ws *UserWebsocket

	conversationMap sync.Map

	termConf *model.TerminalConfig
}

func (h *chat) Name() string {
	return ChatName
}

func (h *chat) CleanUp() {
	h.CleanConversationMap()
}

func (h *chat) CheckValidation() error {
	return nil
}

func (h *chat) HandleMessage(msg *Message) {
	conversationID := msg.Id
	conversation := &AIConversation{}

	if conversationID == "" {
		id := common.UUID()
		conversation = &AIConversation{
			Id:                   id,
			Prompt:               msg.Prompt,
			HistoryRecords:       make([]string, 0),
			InterruptCurrentChat: false,
		}

		// T000 Currently a websocket connection only retains one conversation
		h.CleanConversationMap()
		h.conversationMap.Store(id, conversation)
	} else {
		c, ok := h.conversationMap.Load(conversationID)
		if !ok {
			logger.Errorf("Ws[%s] conversation %s not found", h.ws.Uuid, conversationID)
			h.sendErrorMessage(conversationID, "conversation not found")
			return
		}
		conversation = c.(*AIConversation)
	}

	if msg.Interrupt {
		conversation.InterruptCurrentChat = true
		return
	}

	openAIParam := &OpenAIParam{
		AuthToken: h.termConf.GptApiKey,
		BaseURL:   h.termConf.GptBaseUrl,
		Proxy:     h.termConf.GptProxy,
		Model:     h.termConf.GptModel,
		Prompt:    conversation.Prompt,
	}
	conversation.HistoryRecords = append(conversation.HistoryRecords, msg.Data)
	go h.chat(openAIParam, conversation)
}

func (h *chat) chat(
	chatGPTParam *OpenAIParam, conversation *AIConversation,
) string {
	doneCh := make(chan string)
	answerCh := make(chan string)
	defer close(doneCh)
	defer close(answerCh)

	c := srvconn.NewOpenAIClient(
		chatGPTParam.AuthToken,
		chatGPTParam.BaseURL,
		chatGPTParam.Proxy,
	)

	openAIConn := &srvconn.OpenAIConn{
		Id:       conversation.Id,
		Client:   c,
		Prompt:   chatGPTParam.Prompt,
		Model:    chatGPTParam.Model,
		Contents: conversation.HistoryRecords,
		AnswerCh: answerCh,
		DoneCh:   doneCh,
	}

	go openAIConn.Chat(&conversation.InterruptCurrentChat)
	return h.processChatMessages(openAIConn)
}

func (h *chat) processChatMessages(
	openAIConn *srvconn.OpenAIConn,
) string {
	messageID := common.UUID()
	id := openAIConn.Id
	for {
		select {
		case answer := <-openAIConn.AnswerCh:
			h.sendSessionMessage(id, answer, messageID, "message")
		case answer := <-openAIConn.DoneCh:
			h.sendSessionMessage(id, answer, messageID, "finish")
			return answer
		}
	}
}

func (h *chat) sendSessionMessage(id, answer, messageID, messageType string) {
	message := ChatGPTMessage{
		Content:    answer,
		ID:         messageID,
		CreateTime: time.Now(),
		Type:       messageType,
		Role:       openai.ChatMessageRoleAssistant,
	}
	data, _ := json.Marshal(message)
	msg := Message{
		Id:   id,
		Type: "message",
		Data: string(data),
	}
	h.ws.SendMessage(&msg)
}

func (h *chat) sendErrorMessage(id, message string) {
	msg := Message{
		Id:   id,
		Type: "error",
		Data: message,
	}
	h.ws.SendMessage(&msg)
}

func (h *chat) CleanConversationMap() {
	h.conversationMap.Range(func(key, value interface{}) bool {
		h.conversationMap.Delete(key)
		return true
	})
}
