package proxy

import (
	"cocogo/pkg/logger"
	"cocogo/pkg/sdk"
	"cocogo/pkg/service"
	"fmt"
	"github.com/ibuler/ssh"
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
	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>Proxy")
	ptyReq, winCh, ok := p.Session.Pty()
	if !ok {
		logger.Error("Pty not ok")
		return
	}
	conn := SSHConnection{
		Host:     "192.168.244.185",
		Port:     "22",
		User:     "root",
		Password: "redhat",
	}
	err := conn.Connect(ptyReq.Window.Height, ptyReq.Window.Width, ptyReq.Term)
	if err != nil {
		return
	}

	go func() {
		for {
			select {
			case win, ok := <-winCh:
				if !ok {
					return
				}
				err := conn.SetWinSize(win.Height, win.Width)
				if err != nil {
					logger.Error("windowChange err: ", win)
					return
				}
				logger.Info("windowChange: ", win)
			}
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		writer := conn.Writer()
		for {
			fmt.Println("Start read from user session")
			nr, err := p.Session.Read(buf)
			fmt.Printf("get ddata from user: %s\n", buf)
			if err != nil {
				logger.Error("...............")
			}
			writer.Write(buf[:nr])
		}
	}()

	go func() {
		buf := make([]byte, 1024)
		reader := conn.Reader()
		fmt.Printf("Go func stdout pip")
		for {
			fmt.Printf("Start read from server\n")
			nr, err := reader.Read(buf)
			fmt.Printf("Read data from server: %s\n", buf)
			if err != nil {
				logger.Error("Read error")
			}
			p.Session.Write(buf[:nr])
		}
	}()

	conn.Session.Wait()
}
