package koko

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

func TokenCheck(jmsService *service.JMService) {
	// Do something
	sessions := session.GetSessions()
	tokens := make(map[string]service.TokenStatus, len(sessions))
	for _, s := range sessions {
		ret, ok := tokens[s.TokenId]
		if ok {
			continue
		}
		ret, err := jmsService.CheckTokenStatus(s.TokenId)
		if err != nil && ret.Code == "" {
			logger.Errorf("Check token status failed: %s", err)
			continue
		}
		tokens[s.TokenId] = ret
		_ = s.HandleEvent(&session.Event{})
	}
}
