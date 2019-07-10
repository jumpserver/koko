package proxy

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewSwitchSession(p *ProxyServer) (sw *SwitchSession) {
	sw = &SwitchSession{p: p}
	sw.Initial()
	return sw
}

type SwitchSession struct {
	ID string
	p  *ProxyServer

	DateStart  string
	DateEnd    string
	DateActive time.Time
	finished   bool

	MaxIdleTime time.Duration

	cmdRecorder    *CommandRecorder
	replayRecorder *ReplyRecorder
	parser         *Parser

	ctx    context.Context
	cancel context.CancelFunc
}

func (s *SwitchSession) Initial() {
	s.ID = uuid.NewV4().String()
	s.DateStart = common.CurrentUTCTime()
	s.MaxIdleTime = config.GetConf().MaxIdleTime
	s.cmdRecorder = NewCommandRecorder(s.ID)
	s.replayRecorder = NewReplyRecord(s.ID)
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
		if command[0] == "" {
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
	if i <= 0 {
		output = command[1]
	} else if i > 0 && i < 1024 {
		output = command[1][:i]
	} else {
		output = command[1][:1024]
	}

	return &model.Command{
		SessionID:  s.ID,
		OrgID:      s.p.Asset.OrgID,
		Input:      input,
		Output:     output,
		User:       fmt.Sprintf("%s (%s)", s.p.User.Name, s.p.User.Username),
		Server:     s.p.Asset.Hostname,
		SystemUser: s.p.SystemUser.Username,
		Timestamp:  time.Now().Unix(),
	}
}

// postBridge 桥接结束以后执行操作
func (s *SwitchSession) postBridge() {
	s.DateEnd = common.CurrentUTCTime()
	s.finished = true
	s.parser.Close()
	s.replayRecorder.End()
	s.cmdRecorder.End()
}

// SetFilterRules 设置命令过滤规则
func (s *SwitchSession) SetFilterRules(cmdRules []model.SystemUserFilterRule) {
	s.parser.SetCMDFilterRules(cmdRules)
}

// Bridge 桥接两个链接
func (s *SwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	winCh := userConn.WinCh()
	defer func() {
		logger.Info("Session bridge done: ", s.ID)
		_ = userConn.Close()
		_ = srvConn.Close()
		s.postBridge()
	}()

	userInChan := make(chan []byte, 10)
	srvInChan := make(chan []byte, 10)

	// 处理数据流
	userOutChan, srvOutChan := s.parser.ParseStream(userInChan, srvInChan)
	// 记录命令
	go s.recordCommand()
	go LoopRead(userConn, userInChan)
	go LoopRead(srvConn, srvInChan)

	for {
		select {
		// 检测是否超过最大空闲时间
		case <-time.After(s.MaxIdleTime * time.Minute):
			msg := fmt.Sprintf(i18n.T("Connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Terminated by administrator")
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 监控窗口大小变化
		case win, ok := <-winCh:
			if !ok {
				return
			}
			_ = srvConn.SetWinSize(win.Height, win.Width)
			logger.Debugf("Window server change: %d*%d", win.Height, win.Width)
		// 经过parse处理的server数据，发给user
		case p, ok := <-srvOutChan:
			if !ok {
				return
			}
			nw, _ := userConn.Write(p)
			if !s.parser.IsInZmodemRecvState() {
				s.replayRecorder.Record(p[:nw])
			}
		// 经过parse处理的user数据，发给server
		case p, ok := <-userOutChan:
			if !ok {
				return
			}
			_, err = srvConn.Write(p)
		}
	}
}

func (s *SwitchSession) MapData() map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":          s.ID,
		"user":        fmt.Sprintf("%s (%s)", s.p.User.Name, s.p.User.Username),
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

func LoopRead(read io.Reader, inChan chan<- []byte) {
	defer logger.Debug("loop read end")
	for {
		buf := make([]byte, 1024)
		nr, err := read.Read(buf)
		if err != nil {
			break
		}
		if nr > 0 {
			inChan <- buf[:nr]
		}
	}
	close(inChan)
}
