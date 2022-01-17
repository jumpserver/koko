package handler

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
)

func (u *UserSelectHandler) proxyApp(app model.Application) {

	systemUsers, err := u.h.jmsService.GetUserApplicationSystemUsers(u.user.ID, app.ID)
	if err != nil {
		return
	}
	highestSystemUsers := selectHighestPrioritySystemUsers(systemUsers)
	selectedSystemUser, ok := u.h.chooseSystemUser(highestSystemUsers)
	if !ok {
		logger.Infof("User %s don't select systemUser", u.user.Name)
		return
	}
	i18nLang := u.h.i18nLang
	srv, err := proxy.NewServer(u.h.sess, u.h.jmsService,
		proxy.ConnectProtocolType(selectedSystemUser.Protocol),
		proxy.ConnectI18nLang(i18nLang),
		proxy.ConnectApp(&app),
		proxy.ConnectSystemUser(&selectedSystemUser),
		proxy.ConnectUser(u.user),
	)
	if err != nil {
		logger.Error(err)
	}
	srv.Proxy()
	logger.Infof("Request %s: application %s proxy end", u.h.sess.Uuid, app.Name)

}
