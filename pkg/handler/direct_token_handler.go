package handler

import (
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/utils"
)

func (d *DirectHandler) LoginConnectToken() {
	tokenInfo := d.opts.tokenInfo
	user := tokenInfo.User
	systemUserAuthInfo := tokenInfo.SystemUserAuthInfo
	domain := tokenInfo.Domain
	filterRules := tokenInfo.CmdFilterRules
	expiredAt := tokenInfo.ExpiredAt

	sysId := systemUserAuthInfo.ID
	systemUserDetail, err := d.jmsService.GetSystemUserById(sysId)
	if err != nil {
		utils.IgnoreErrWriteString(d.sess, err.Error())
		logger.Error(err)
		return
	}

	proxyOpts := make([]proxy.ConnectionOption, 0, 8)
	proxyOpts = append(proxyOpts, proxy.ConnectUser(user))
	proxyOpts = append(proxyOpts, proxy.ConnectProtocol(systemUserDetail.Protocol))
	proxyOpts = append(proxyOpts, proxy.ConnectAsset(tokenInfo.Asset))
	proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(d.i18nLang))

	proxyOpts = append(proxyOpts, proxy.ConnectDomain(domain))
	proxyOpts = append(proxyOpts, proxy.ConnectFilterRules(filterRules))
	proxyOpts = append(proxyOpts, proxy.ConnectExpired(expiredAt))

	srv, err := proxy.NewServer(d.wrapperSess, d.jmsService, proxyOpts...)
	if err != nil {
		logger.Error(err)
		return
	}
	srv.Proxy()
	logger.Infof("Request %s: token %s proxy end", d.wrapperSess.Uuid, tokenInfo.Id)

}
