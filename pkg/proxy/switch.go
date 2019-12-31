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

	DateStart string
	DateEnd   string
	finished  bool

	MaxIdleTime time.Duration

	cmdRules []model.SystemUserFilterRule

	ctx    context.Context
	cancel context.CancelFunc
}

func (s *SwitchSession) Initial() {
	s.ID = uuid.NewV4().String()
	s.DateStart = common.CurrentUTCTime()
	s.MaxIdleTime = config.GetConf().MaxIdleTime
	s.cmdRules = make([]model.SystemUserFilterRule, 0)
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (s *SwitchSession) Terminate() {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	s.cancel()
	logger.Infof("Session %s: receive terminate from admin", s.ID)
}

func (s *SwitchSession) SessionID() string {
	return s.ID
}

func (s *SwitchSession) recordCommand(cmdRecordChan chan [2]string) {
	// 命令记录
	cmdRecorder := NewCommandRecorder(s.ID)
	for command := range cmdRecordChan {
		if command[0] == "" {
			continue
		}
		cmd := s.generateCommandResult(command)
		cmdRecorder.Record(cmd)
	}
	// 关闭命令记录
	cmdRecorder.End()
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
}

// SetFilterRules 设置命令过滤规则
func (s *SwitchSession) SetFilterRules(cmdRules []model.SystemUserFilterRule) {
	if len(cmdRules) > 0 {
		s.cmdRules = cmdRules
	}
}

// Bridge 桥接两个链接
func (s *SwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	var (
		parser         Parser
		replayRecorder ReplyRecorder

		userInChan chan []byte
		srvInChan  chan []byte
		done       chan struct{}
	)

	parser = newParser(s.ID)
	replayRecorder = NewReplyRecord(s.ID)

	userInChan = make(chan []byte, 1)
	srvInChan = make(chan []byte, 1)
	done = make(chan struct{})
	// 设置parser的命令过滤规则
	parser.SetCMDFilterRules(s.cmdRules)

	// 处理数据流
	userOutChan, srvOutChan := parser.ParseStream(userInChan, srvInChan)

	defer func() {
		close(done)
		_ = userConn.Close()
		_ = srvConn.Close()
		// 关闭parser
		parser.Close()
		// 关闭录像
		replayRecorder.End()
		s.postBridge()
	}()

	// 记录命令
	go s.recordCommand(parser.cmdRecordChan)
	go s.LoopReadFromSrv(done, srvConn, srvInChan)
	go s.LoopReadFromUser(done, userConn, userInChan)
	winCh := userConn.WinCh()
	maxIdleTime := s.MaxIdleTime * time.Minute
	lastActiveTime := time.Now()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		// 检测是否超过最大空闲时间
		case <-tick.C:
			now := time.Now()
			outTime := lastActiveTime.Add(maxIdleTime)
			if !now.After(outTime) {
				continue
			}
			msg := fmt.Sprintf(i18n.T("Connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
			logger.Debugf("Session idle more than %d minutes, disconnect: %s", s.MaxIdleTime, s.ID)
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Terminated by administrator")
			msg = utils.WrapperWarn(msg)
			logger.Infof("Session %s: %s", s.ID, msg)
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
			if !parser.IsInZmodemRecvState() {
				replayRecorder.Record(p[:nw])
			}
		// 经过parse处理的user数据，发给server
		case p, ok := <-userOutChan:
			if !ok {
				return
			}
			_, err = srvConn.Write(p)
		}
		lastActiveTime = time.Now()
	}
}

func (s *SwitchSession) MapData() map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":             s.ID,
		"user":           fmt.Sprintf("%s (%s)", s.p.User.Name, s.p.User.Username),
		"asset":          s.p.Asset.Hostname,
		"org_id":         s.p.Asset.OrgID,
		"login_from":     s.p.UserConn.LoginFrom(),
		"system_user":    s.p.SystemUser.Username,
		"protocol":       s.p.SystemUser.Protocol,
		"remote_addr":    s.p.UserConn.RemoteAddr(),
		"is_finished":    s.finished,
		"date_start":     s.DateStart,
		"date_end":       dataEnd,
		"user_id":        s.p.User.ID,
		"asset_id":       s.p.Asset.ID,
		"system_user_id": s.p.SystemUser.ID,
	}
}

func (s *SwitchSession) LoopReadFromUser(done chan struct{}, userConn UserConnection, inChan chan<- []byte) {
	defer logger.Infof("Session %s: read from user done", s.ID)
	s.LoopRead(done, userConn, inChan)
}

func (s *SwitchSession) LoopReadFromSrv(done chan struct{}, srvConn srvconn.ServerConnection, inChan chan<- []byte) {
	defer logger.Infof("Session %s: read from srv done", s.ID)
	s.LoopRead(done, srvConn, inChan)
}

func (s *SwitchSession) LoopRead(done chan struct{}, read io.Reader, inChan chan<- []byte) {
loop:
	for {
		buf := make([]byte, 1024)
		nr, err := read.Read(buf)
		if nr > 0 {
			select {
			case <-done:
				break loop
			case inChan <- buf[:nr]:
			}
		}
		if err != nil {
			break
		}
	}
	close(inChan)
}
