package service

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) CheckUserCookie(cookies map[string]string) (user *model.User, err error) {
	client := s.authClient.Clone()
	for k, v := range cookies {
		client.SetCookie(k, v)
	}
	_, err = client.Get(UserProfileURL, &user)
	return
}
