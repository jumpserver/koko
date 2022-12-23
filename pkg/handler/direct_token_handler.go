package handler

import (
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
)

func (d *DirectHandler) LoginConnectToken() {
	connectToken := d.opts.tokenInfo
	i18nLang := d.i18nLang
	proxyOpts := make([]proxy.ConnectionOption, 0, 10)
	proxyOpts = append(proxyOpts, proxy.ConnectTokenAuthInfo(connectToken))
	proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(i18nLang))
	srv, err := proxy.NewServer(d.wrapperSess, d.jmsService, proxyOpts...)
	if err != nil {
		logger.Error(err)
		return
	}
	srv.Proxy()
	logger.Infof("Request %s: token %s proxy end", d.wrapperSess.Uuid, connectToken.Id)

}
