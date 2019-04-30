package proxy

import (
	"context"
	"github.com/ibuler/ssh"

	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
)

type ProxyServer struct {
	Session    ssh.Session
	User       *model.User
	Asset      *model.Asset
	SystemUser *model.SystemUser
}

func (p *ProxyServer) getSystemUserAuthOrManualSet() {
	info := service.GetSystemUserAssetAuthInfo(p.SystemUser.Id, p.Asset.Id)
	if p.SystemUser.LoginMode == model.LoginModeManual ||
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

func (p *ProxyServer) Proxy(ctx context.Context) {
	if !p.checkProtocol() {
		return
	}
	conn := SSHConnection{
		Host:     "127.0.0.1",
		Port:     "1337",
		User:     "root",
		Password: "MyRootPW123",
	}
	ptyReq, _, ok := p.Session.Pty()
	if !ok {
		logger.Error("Pty not ok")
		return
	}
	err := conn.Connect(ptyReq.Window.Height, ptyReq.Window.Width, ptyReq.Term)
	if err != nil {
		return
	}
	rules, err := service.GetSystemUserFilterRules("")
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	parser := &Parser{
		filterRules: rules,
	}
	parser.Initial()
	sw := Switch{
		userSession: p.Session,
		serverConn:  &conn,
		parser:      parser,
	}
	sw.Bridge(ctx)
}
