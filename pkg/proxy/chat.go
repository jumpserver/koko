package proxy

import (
	"fmt"
	"github.com/jumpserver/koko/pkg/common"
	modelCommon "github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
	"strings"
	"time"
)

type ChatReplyRecorder struct {
	*ReplyRecorder
}

func (rh *ChatReplyRecorder) WriteInput(inputStr string) {
	currentTime := time.Now()
	formattedTime := currentTime.Format("2006-01-02 15:04:05")
	inputStr = fmt.Sprintf("[%s]#: %s", formattedTime, inputStr)
	rh.Record([]byte(inputStr))
}

func (rh *ChatReplyRecorder) WriteOutput(outputStr string) {
	wrappedText := rh.wrapText(outputStr)
	outputStr = "\r\n" + wrappedText + "\r\n"
	rh.Record([]byte(outputStr))

}

func (rh *ChatReplyRecorder) wrapText(text string) string {
	var wrappedTextBuilder strings.Builder
	words := strings.Fields(text)
	currentLineLength := 0

	for _, word := range words {
		wordLength := len(word)

		if currentLineLength+wordLength > rh.Writer.Width {
			wrappedTextBuilder.WriteString("\r\n" + word + " ")
			currentLineLength = wordLength + 1
		} else {
			wrappedTextBuilder.WriteString(word + " ")
			currentLineLength += wordLength + 1
		}
	}

	return wrappedTextBuilder.String()
}

func NewChatJMSServer(
	user, ip, userID, langCode string,
	jmsService *service.JMService, conf *model.TerminalConfig) (*ChatJMSServer, error) {
	accountInfo, err := jmsService.GetAccountChat()
	if err != nil {
		logger.Errorf("Get account chat info error: %s", err)
		return nil, err
	}

	id := common.UUID()

	apiSession := &model.Session{
		ID:         id,
		User:       user,
		LoginFrom:  model.LoginFromWeb,
		RemoteAddr: ip,
		Protocol:   model.ActionALL,
		Asset:      accountInfo.Asset.Name,
		Account:    accountInfo.Name,
		AccountID:  accountInfo.ID,
		AssetID:    accountInfo.Asset.ID,
		UserID:     userID,
		OrgID:      "00000000-0000-0000-0000-000000000004",
		Type:       model.NORMALType,
		LangCode:   langCode,
		DateStart:  modelCommon.NewNowUTCTime(),
	}

	_, err2 := jmsService.CreateSession(*apiSession)
	if err2 != nil {
		return nil, err2
	}

	chat := &ChatJMSServer{
		JmsService: jmsService,
		Session:    apiSession,
		Conf:       conf,
	}

	chat.CmdR = chat.GetCommandRecorder()
	chat.Replay = chat.GetReplayRecorder()

	if err1 := jmsService.RecordSessionLifecycleLog(id, model.AssetConnectSuccess,
		model.EmptyLifecycleLog); err1 != nil {
		logger.Errorf("Record session activity log err: %s", err1)
	}

	return chat, nil
}

type ChatJMSServer struct {
	JmsService *service.JMService
	Session    *model.Session
	CmdR       *CommandRecorder
	Replay     *ChatReplyRecorder
	Conf       *model.TerminalConfig
}

func (s *ChatJMSServer) GenerateCommandItem(user, input, output string) *model.Command {
	createdDate := time.Now()
	return &model.Command{
		SessionID:   s.Session.ID,
		OrgID:       s.Session.OrgID,
		Input:       input,
		Output:      output,
		User:        user,
		Server:      s.Session.Asset,
		Account:     s.Session.Account,
		Timestamp:   createdDate.Unix(),
		RiskLevel:   model.NormalLevel,
		DateCreated: createdDate.UTC(),
	}
}

func (s *ChatJMSServer) GetReplayRecorder() *ChatReplyRecorder {
	info := &ReplyInfo{
		Width:     200,
		Height:    200,
		TimeStamp: time.Now(),
	}
	recorder, err := NewReplayRecord(s.Session.ID, s.JmsService,
		NewReplayStorage(s.JmsService, s.Conf),
		info)
	if err != nil {
		logger.Error(err)
	}

	return &ChatReplyRecorder{recorder}
}

func (s *ChatJMSServer) GetCommandRecorder() *CommandRecorder {
	cmdR := CommandRecorder{
		sessionID:  s.Session.ID,
		storage:    NewCommandStorage(s.JmsService, s.Conf),
		queue:      make(chan *model.Command, 10),
		closed:     make(chan struct{}),
		jmsService: s.JmsService,
	}
	go cmdR.record()
	return &cmdR
}

func (s *ChatJMSServer) Close(msg string) {
	session.RemoveSessionById(s.Session.ID)
	if err := s.JmsService.SessionFinished(s.Session.ID, modelCommon.NewNowUTCTime()); err != nil {
		logger.Errorf("finish session %s: %v", s.Session.ID, err)
	}

	s.CmdR.End()
	s.Replay.End()

	logObj := model.SessionLifecycleLog{Reason: msg, User: s.Session.User}
	err := s.JmsService.RecordSessionLifecycleLog(s.Session.ID, model.AssetConnectFinished, logObj)
	if err != nil {
		logger.Errorf("record session lifecycle log %s: %v", s.Session.ID, err)
		return
	}
}
