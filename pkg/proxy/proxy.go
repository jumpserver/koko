package proxy

import (
	"fmt"
	"time"

	"github.com/ibuler/ssh"
	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/logger"
	"cocogo/pkg/sdk"
	"cocogo/pkg/service"
)

type ProxyServer struct {
	Session    ssh.Session
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
	conn := SSHConnection{
		Host:     "192.168.244.143",
		Port:     "22",
		User:     "root",
		Password: "redhat",
	}
	_, err := conn.Connect()
	if err != nil {
		return
	}
	ptyReq, _, ok := p.Session.Pty()
	if !ok {
		logger.Error("Pty not ok")
		return
	}

	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	modes := gossh.TerminalModes{
		gossh.ECHO:          1,     // enable echoing
		gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	err = conn.Session.RequestPty("xterm", ptyReq.Window.Height, ptyReq.Window.Width, modes)
	if err != nil {
		logger.Errorf("Request pty error: %s", err)
		return
	}

	go func() {
		buf := make([]byte, 1024)
		writer, err := conn.Session.StdinPipe()
		if err != nil {
			return
		}
		for {
			nr, err := p.Session.Read(buf)
			if err != nil {
				writer.Write(buf[:nr])
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		reader, err := conn.Reader()
		if err != nil {
			return
		}
		for {
			nr, err := reader.Read(buf)
			if err != nil {
				logger.Error("Read error")
			}
			p.Session.Write(buf[:nr])
		}
	}()

	time.Sleep(time.Second * 20)
}
