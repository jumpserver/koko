package proxy

import (
	"context"
	"fmt"
	"io"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
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

	ctx    context.Context
	cancel context.CancelFunc
}

func (s *DBSwitchSession) Initial() {
	s.ID = uuid.NewV4().String()
	s.DateStart = common.CurrentUTCTime()
	s.MaxIdleTime = config.GetConf().MaxIdleTime
	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (s *DBSwitchSession) Terminate() {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	s.cancel()
}

// postBridge 桥接结束以后执行操作
func (s *DBSwitchSession) postBridge() {
	s.DateEnd = common.CurrentUTCTime()
	s.finished = true
}

// Bridge 桥接两个链接
func (s *DBSwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	var (
		userInChan chan []byte
		srvInChan  chan []byte
		done       chan struct{}
	)

	userInChan = make(chan []byte, 1)
	srvInChan = make(chan []byte, 1)
	done = make(chan struct{})

	defer func() {
		close(done)
		_ = userConn.Close()
		_ = srvConn.Close()
		s.postBridge()
	}()

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
			logger.Debugf("Session idle more than %d minutes, disconnect: %s", s.MaxIdleTime, s.ID)
			msg = utils.WrapperWarn(msg)
			utils.IgnoreErrWriteString(userConn, "\n\r"+msg)
			return
		// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Database connection terminated by administrator")
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
		case p, ok := <-srvInChan:
			if !ok {
				return
			}
			_, _ = userConn.Write(p)
		// 经过parse处理的user数据，发给server
		case p, ok := <-userInChan:
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
		"id":          s.ID,
		"user":        fmt.Sprintf("%s (%s)", s.p.User.Name, s.p.User.Username),
		"login_from":  s.p.UserConn.LoginFrom(),
		"remote_addr": s.p.UserConn.RemoteAddr(),
		"is_finished": s.finished,
		"date_start":  s.DateStart,
		"date_end":    dataEnd,
		"user_id":     s.p.User.ID,
	}
}

func (s *DBSwitchSession) LoopReadFromUser(done chan struct{}, userConn UserConnection, inChan chan<- []byte) {
	defer logger.Infof("Session %s: read from user done", s.ID)
	s.LoopRead(done, userConn, inChan)
}

func (s *DBSwitchSession) LoopReadFromSrv(done chan struct{}, srvConn srvconn.ServerConnection, inChan chan<- []byte) {
	defer logger.Infof("Session %s: read from srv done", s.ID)
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
				logger.Debug("reader loop break done.")
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
