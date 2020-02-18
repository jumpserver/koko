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

type DBSwitchSession struct {
	ID string
	p  *DBProxyServer

	DateStart string
	DateEnd   string
	finished  bool

	MaxIdleTime time.Duration

	cmdRules []model.SystemUserFilterRule

	ctx    context.Context
	cancel context.CancelFunc
}

func (s *DBSwitchSession) Initial() {
	s.ID = uuid.NewV4().String()
	s.DateStart = common.CurrentUTCTime()
	s.MaxIdleTime = config.GetConf().MaxIdleTime
	s.cmdRules = make([]model.SystemUserFilterRule, 0)
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (s *DBSwitchSession) Terminate() {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	s.cancel()
	logger.Infof("DBSession %s: receive terminate from admin", s.ID)
}

func (s *DBSwitchSession) SessionID() string {
	return s.ID
}

func (s *DBSwitchSession) recordCommand(cmdRecordChan chan [3]string) {
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
func (s *DBSwitchSession) generateCommandResult(command [3]string) *model.Command {
	var input string
	var output string
	var isHighRisk bool
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
	switch command[2] {
	case model.HighRiskFlag:
		isHighRisk = true
	default:
		isHighRisk = false
	}
	return &model.Command{
		SessionID:  s.ID,
		OrgID:      s.p.Database.OrgID,
		Input:      input,
		Output:     output,
		User:       fmt.Sprintf("%s (%s)", s.p.User.Name, s.p.User.Username),
		Server:     s.p.Database.Name,
		SystemUser: s.p.SystemUser.Username,
		Timestamp:  time.Now().Unix(),
		IsHighRisk: isHighRisk,
	}
}

func (s *DBSwitchSession) SetFilterRules(cmdRules []model.SystemUserFilterRule) {
	if len(cmdRules) > 0 {
		s.cmdRules = cmdRules
	}
}

// postBridge 桥接结束以后执行操作
func (s *DBSwitchSession) postBridge() {
	s.DateEnd = common.CurrentUTCTime()
	s.finished = true
}

// Bridge 桥接两个链接
func (s *DBSwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	var (
		parser         DBParser
		replayRecorder ReplyRecorder

		userInChan chan []byte
		srvInChan  chan []byte
		done       chan struct{}
	)
	parser = newDBParser(s.ID)
	replayRecorder = NewReplyRecord(s.ID)

	userInChan = make(chan []byte, 1)
	srvInChan = make(chan []byte, 1)
	done = make(chan struct{})
	// 设置parser的命令过滤规则
	parser.SetCMDFilterRules(s.cmdRules)

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
			msg := fmt.Sprintf(i18n.T("Database connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
			logger.Infof("DB Session idle more than %d minutes, disconnect: %s", s.MaxIdleTime, s.ID)
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Database connection terminated by administrator")
			msg = utils.WrapperWarn(msg)
			logger.Infof("DBSession %s: %s", s.ID, msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 监控窗口大小变化
		case win, ok := <-winCh:
			if !ok {
				return
			}
			_ = srvConn.SetWinSize(win.Height, win.Width)
			logger.Debugf("DB Window server change: %d*%d", win.Height, win.Width)
		// 经过parse处理的server数据，发给user
		case p, ok := <-srvOutChan:
			if !ok {
				return
			}
			nw, _ := userConn.Write(p)
			replayRecorder.Record(p[:nw])
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

func (s *DBSwitchSession) MapData() map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":             s.ID,
		"user":           fmt.Sprintf("%s (%s)", s.p.User.Name, s.p.User.Username),
		"asset":          s.p.Database.Name,
		"org_id":         s.p.Database.OrgID,
		"login_from":     s.p.UserConn.LoginFrom(),
		"system_user":    s.p.SystemUser.Username,
		"protocol":       s.p.SystemUser.Protocol,
		"remote_addr":    s.p.UserConn.RemoteAddr(),
		"is_finished":    s.finished,
		"date_start":     s.DateStart,
		"date_end":       dataEnd,
		"user_id":        s.p.User.ID,
		"asset_id":       s.p.Database.ID,
		"system_user_id": s.p.SystemUser.ID,
	}
}

func (s *DBSwitchSession) LoopReadFromUser(done chan struct{}, userConn UserConnection, inChan chan<- []byte) {
	defer logger.Infof("DB Session %s: read from user done", s.ID)
	s.LoopRead(done, userConn, inChan)
}

func (s *DBSwitchSession) LoopReadFromSrv(done chan struct{}, srvConn srvconn.ServerConnection, inChan chan<- []byte) {
	defer logger.Infof("DB Session %s: read from srv done", s.ID)
	s.LoopRead(done, srvConn, inChan)
}

func (s *DBSwitchSession) LoopRead(done chan struct{}, read io.Reader, inChan chan<- []byte) {
loop:
	for {
		buf := make([]byte, 1024)
		nr, err := read.Read(buf)
		if nr > 0 {
			select {
			case <-done:
				logger.Debug("DB session reader loop break done.")
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
