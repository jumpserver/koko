package service

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

func (s *JMService) CreateFileOperationLog(data model.FTPLog) (err error) {
	_, err = s.authClient.Post(FTPLogListURL, data, nil)
	return
}

func (s *JMService) PushSessionCommand(commands []*model.Command) (err error) {
	_, err = s.authClient.Post(SessionCommandURL, commands, nil)
	return
}


func (s *JMService) NotifyCommand(commands []*model.Command) (err error) {
	_, err = s.authClient.Post(NotificationCommandURL, commands, nil)
	return
}