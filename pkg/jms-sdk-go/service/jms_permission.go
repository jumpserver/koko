package service

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) ValidateJoinSessionPermission(userId, sessionId string) (result model.ValidateResult, err error) {
	data := map[string]string{
		"user_id":    userId,
		"session_id": sessionId,
	}
	_, err = s.authClient.Post(JoinRoomValidateURL, data, &result)
	return
}
