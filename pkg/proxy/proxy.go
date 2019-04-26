package proxy

import (
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
	"github.com/ibuler/ssh"

	"cocogo/pkg/sdk"
)

type ProxyServer struct {
	sess       ssh.Session
	User       *sdk.User
	Asset      *sdk.Asset
	SystemUser *sdk.SystemUser
}

func (p *ProxyServer) getSystemUserAuthOrManualSet() {
	info := service.GetSystemUserAssetAuthInfo(p.SystemUser.Id, p.Asset.Id)
	if p.SystemUser.LoginMode == sdk.LoginModeManual ||
		(p.SystemUser.Password == "" && p.SystemUser.PrivateKey == "") {
		logger.Info("Get password fom user input")
	}
	p.SystemUser.Password = info.Password
	p.SystemUser.PrivateKey = info.PrivateKey
}

func (p *ProxyServer) checkProtocol() bool {
	return true
}

func (p *ProxyServer) getSystemUserUsernameIfNeed() {

}

func (p *ProxyServer) validatePermission() bool {
	return true
}

func (p *ProxyServer) getServerConn() {

}

func (p *ProxyServer) sendConnectingMsg() {

}

func (p *ProxyServer) Proxy() {
	if !p.checkProtocol() {
		return
	}

}
