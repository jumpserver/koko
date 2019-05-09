package proxy

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/config"
	"cocogo/pkg/i18n"
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

func (p *ProxyServer) getServerConn() (srvConn ServerConnection, err error) {
	srvConn = &ServerSSHConnection{
		host:     p.Asset.Ip,
		port:     strconv.Itoa(p.Asset.Port),
		user:     p.SystemUser.UserName,
		password: p.SystemUser.Password,
		timeout:  config.Conf.SSHTimeout,
	}
	pty, _, ok := p.Session.Pty()
	if !ok {
		logger.Error("User not request Pty")
		return
	}
	done := make(chan struct{})
	go p.sendConnectingMsg(done)
	err = srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
	_, _ = io.WriteString(p.Session, "\r\n")
	close(done)
	return
}

func (p *ProxyServer) sendConnectingMsg(done chan struct{}) {
	delay := 0.0
	msg := fmt.Sprintf(i18n.T("Connecting to %s@%s  %.1f"), p.SystemUser.UserName, p.Asset.Ip, delay)
	_, _ = io.WriteString(p.Session, msg)
	for int(delay) < config.Conf.SSHTimeout {
		select {
		case <-done:
			return
		default:
			delayS := fmt.Sprintf("%.1f", delay)
			data := strings.Repeat("\x08", len(delayS)) + delayS
			_, _ = io.WriteString(p.Session, data)
			time.Sleep(100 * time.Millisecond)
			delay += 0.1
		}
	}
}

func (p *ProxyServer) Proxy() {
	if !p.checkProtocol() {
		return
	}
	srvConn, err := p.getServerConn()
	if err != nil {
		logger.Errorf("Connect host error: %s\n", err)
		return
	}

	userConn := &UserSSHConnection{Session: p.Session, winch: make(chan ssh.Window)}
	sw := NewSwitch(userConn, srvConn)
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.Id)
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	sw.parser.SetCMDFilterRules(cmdRules)
	replayRecorder := NewReplyRecord(sw.Id)
	sw.parser.SetReplayRecorder(replayRecorder)
	_ = sw.Bridge()
	_ = srvConn.Close()
}
