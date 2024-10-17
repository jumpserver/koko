package koko

import (
	"time"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
)

// RunConnectTokensCheck every 5 minutes check token status
func RunConnectTokensCheck(jmsService *service.JMService) {
	apiClient := jmsService.Copy()
	for {
		time.Sleep(5 * time.Minute)
		sessions := session.GetSessions()
		tokens := make(map[string]model.TokenCheckStatus, len(sessions))
		for _, s := range sessions {
			ret, ok := tokens[s.TokenId]
			if ok {
				handleTokenCheck(s, &ret)
				continue
			}
			apiClient.SetCookie("django_language", s.LangCode)
			ret, err := apiClient.CheckTokenStatus(s.TokenId)
			if err != nil && ret.Code == "" {
				logger.Errorf("Check token status failed: %s", err)
				continue
			}
			tokens[s.TokenId] = ret
			handleTokenCheck(s, &ret)
		}
	}
}

func handleTokenCheck(session *session.Session, tokenStatus *model.TokenCheckStatus) {
	var task model.TerminalTask
	switch tokenStatus.Code {
	case model.CodePermOk:
		task = model.TerminalTask{
			Name: model.TaskPermValid,
			Args: tokenStatus.Detail,
		}
	default:
		task = model.TerminalTask{
			Name: model.TaskPermExpired,
			Args: tokenStatus.Detail,
		}
	}
	if err := session.HandleTask(&task); err != nil {
		logger.Errorf("Handle token check task failed: %s", err)
	}

}
