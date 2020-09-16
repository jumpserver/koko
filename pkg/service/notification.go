package service

import (
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

func NotifyCommand(commands []*model.Command) (err error) {
	_, err = authClient.Post(NotificationCommandURL, commands, nil)
	if err != nil {
		logger.Errorf("Notify command alert err: %s", err)
	}
	return
}
