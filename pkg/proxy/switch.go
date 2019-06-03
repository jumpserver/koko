package proxy

import (
	"cocogo/pkg/srvconn"
	"context"
	"fmt"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"

	"cocogo/pkg/config"
	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/utils"
)

func NewSwitchSession(p *ProxyServer) (sw *SwitchSession) {
	sw = &SwitchSession{p: p}
	sw.Initial()
	return sw
}

type SwitchSession struct {
	Id string
	p  *ProxyServer

	DateStart  string
	DateEnd    string
	DateActive time.Time
	finished   bool

	MaxIdleTime time.Duration

	cmdRecorder    *CommandRecorder
	replayRecorder *ReplyRecorder
	parser         *Parser

	userTran Transport
	srvTran  Transport

	ctx    context.Context
	cancel context.CancelFunc
}

func (s *SwitchSession) Initial() {
	s.Id = uuid.NewV4().String()
	s.DateStart = time.Now().UTC().Format("2006-01-02 15:04:05 +0000")
	s.MaxIdleTime = config.GetConf().MaxIdleTime
	s.cmdRecorder = NewCommandRecorder(s.Id)
	s.replayRecorder = NewReplyRecord(s.Id)
	s.parser = newParser()
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (s *SwitchSession) Terminate() {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	s.cancel()
}

func (s *SwitchSession) recordCommand() {
	for command := range s.parser.cmdRecordChan {
		if command[0] == "" && command[1] == "" {
			continue
		}
		cmd := s.generateCommandResult(command)
		s.cmdRecorder.Record(cmd)
	}
}

// generateCommandResult 生成命令结果
func (s *SwitchSession) generateCommandResult(command [2]string) *model.Command {
	var input string
	var output string
	if len(command[0]) > 128 {
		input = command[0][:128]
	} else {
		input = command[0]
	}
	i := strings.LastIndexByte(command[1], '\r')
	if i < 0 {
		output = command[1]
	} else if i > 0 && i < 1024 {
		output = command[1][:i]
	} else {
		output = command[1][:1024]
	}

	return &model.Command{
		SessionId:  s.Id,
		OrgId:      s.p.Asset.OrgID,
		Input:      input,
		Output:     output,
		User:       s.p.User.Username,
		Server:     s.p.Asset.Hostname,
		SystemUser: s.p.SystemUser.Username,
		Timestamp:  time.Now().Unix(),
	}
}

// postBridge 桥接结束以后执行操作
func (s *SwitchSession) postBridge() {
	s.DateEnd = time.Now().UTC().Format("2006-01-02 15:04:05 +0000")
	s.finished = true
	s.parser.Close()
	s.replayRecorder.End()
	s.cmdRecorder.End()
	_ = s.userTran.Close()
	_ = s.srvTran.Close()
}

// SetFilterRules 设置命令过滤规则
func (s *SwitchSession) SetFilterRules(cmdRules []model.SystemUserFilterRule) {
	s.parser.SetCMDFilterRules(cmdRules)
}

// Bridge 桥接两个链接
func (s *SwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	winCh := userConn.WinCh()
	// 将ReadWriter转换为Channel读写
	s.srvTran = NewDirectTransport(s.Id, srvConn)
	s.userTran = NewDirectTransport(s.Id, userConn)

	defer func() {
		logger.Info("Session bridge done: ", s.Id)
		s.postBridge()
	}()

	// 处理数据流
	go s.parser.ParseStream()
	// 记录命令
	go s.recordCommand()
	for {
		select {
		// 检测是否超过最大空闲时间
		case <-time.After(s.MaxIdleTime * time.Minute):
			msg := fmt.Sprintf(i18n.T("Connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(s.userTran, "\n\r"+msg)
			return
		// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Terminated by administrator")
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 监控窗口大小变化
		case win := <-winCh:
			_ = srvConn.SetWinSize(win.Height, win.Width)
			logger.Debugf("Window server change: %d*%d", win.Height, win.Width)
		// Server发来数据流入parser中
		case p, ok := <-s.srvTran.Chan():
			if !ok {
				return
			}
			s.parser.srvInputChan <- p
		// Server流入parser数据，经处理发给用户
		case p, ok := <-s.parser.srvOutputChan:
			if !ok {
				return
			}
			nw, _ := s.userTran.Write(p)
			if !s.parser.IsRecvState() {
				s.replayRecorder.Record(p[:nw])
			}
		// User发来的数据流流入parser
		case p, ok := <-s.userTran.Chan():
			if !ok {
				return
			}
			s.parser.userInputChan <- p
		// User发来的数据经parser处理，发给Server
		case p, ok := <-s.parser.userOutputChan:
			if !ok {
				return
			}
			_, err = s.srvTran.Write(p)
		}
	}
}

func (s *SwitchSession) MapData() map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":          s.Id,
		"user":        s.p.User.Name,
		"asset":       s.p.Asset.Hostname,
		"org_id":      s.p.Asset.OrgID,
		"login_from":  s.p.UserConn.LoginFrom(),
		"system_user": s.p.SystemUser.Username,
		"remote_addr": s.p.UserConn.RemoteAddr(),
		"is_finished": s.finished,
		"date_start":  s.DateStart,
		"date_end":    dataEnd,
	}
}
