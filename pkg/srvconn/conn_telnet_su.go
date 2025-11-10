package srvconn

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

func LoginToTelnetSu(sc *TelnetConnection) error {
	cfg := sc.cfg.suCfg
	suService, err := NewSuService(cfg, sc)
	if err != nil {
		return err
	}
	return suService.RunSwitchUser()
}

/*

切换用户的执行流程

一、根据系统不同，切换用户执行流程不同
	Linux 系统 sudo 执行流程
	1、执行 su - username; exit (这里的 exit 是为了退出 sudo)
	2、等待密码输入的 prompt (如果是 root 切换普通 可能直接切换成功)

	Cisco 交换机 切换执行流程
	1、执行 enable
	2、等待密码输入的 prompt

	Huawei 交换机 切换执行流程
	1、执行 super 15 (这里的 15 是 user privilege level)
	2、等待密码输入的 prompt

	H3C 交换机 切换执行流程
	1、执行 super level-15 (这里的 15 是 user privilege level)
	2、等待输入 username
	3、等待输入 password

二、等待匹配成功提示字符，如果匹配到失败提示字符，就返回密码错误失败
三、如果成功，返回 切换的提示信息，并通过 \r 换行

关于成功提示符:
Linux 和 Cisco 交换机的成功提示符中，包含
 Huawei:  [root@HUAWEI-xxx]


*/

func NewSuService(cfg *SuConfig, srv io.ReadWriteCloser) (*SuSwitchService, error) {
	successReg, err := regexp.Compile(cfg.SuccessPattern())
	if err != nil {
		return nil, fmt.Errorf("success pattern %s compile failed: %s", cfg.SuccessPattern(), err)
	}
	passwordReg, err := regexp.Compile(cfg.PasswordMatchPattern())
	if err != nil {
		return nil, fmt.Errorf("password pattern %s compile failed: %s", cfg.PasswordMatchPattern(), err)
	}
	usernameReg, err := regexp.Compile(cfg.UsernameMatchPattern())
	if err != nil {
		return nil, fmt.Errorf("username pattern %s compile failed: %s", cfg.UsernameMatchPattern(), err)
	}
	failedPattern := createFailedPattern()
	failedReg, err := regexp.Compile(failedPattern)
	if err != nil {
		return nil, fmt.Errorf("failed pattern %s compile failed: %s", failedPattern, err)
	}
	suService := SuSwitchService{
		cfg:            cfg,
		SrvConn:        srv,
		successRegexp:  successReg,
		usernameRegexp: usernameReg,
		passwordRegexp: passwordReg,
		failureRegexp:  failedReg,
	}
	return &suService, nil
}

type SuSwitchService struct {
	cfg         *SuConfig
	execCommand func()

	SrvConn io.ReadWriteCloser

	successRegexp  *regexp.Regexp
	usernameRegexp *regexp.Regexp
	passwordRegexp *regexp.Regexp
	failureRegexp  *regexp.Regexp

	inputAuthOnce bool
	needAuthOnce  bool
}

func (s *SuSwitchService) RunSwitchUser() error {
	s.runSwitchCommand()
	resultChan := make(chan error, 1)
	go s.loginUsernameOrPassword(resultChan)
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()
	select {
	case ret := <-resultChan:
		return ret
	case <-ticker.C:
	}
	return ErrorTimeout
}

func (s *SuSwitchService) runSwitchCommand() {
	if s.execCommand != nil {
		s.execCommand()
	} else {
		cmd := s.cfg.SuCommand()
		_, _ = s.SrvConn.Write([]byte(cmd + "\r"))
		s.needAuthOnce = true
	}
}

func (s *SuSwitchService) loginUsernameOrPassword(resultChan chan<- error) {
	buf := make([]byte, 8192)
	var recStr bytes.Buffer
	for {
		nr, err2 := s.SrvConn.Read(buf)
		if err2 != nil {
			resultChan <- err2
			return
		}
		recStr.Write(buf[:nr])
		status := s.handleResult(recStr.Bytes())
		switch status {
		case StatusSuccess:
			// 成功后，结束切换
			resultChan <- nil
			return
		case StatusMatch:
			// 匹配到了，清空缓存
			recStr.Reset()
			logger.Debug("Sudo step result matched and rest")
			continue
		case StatusFailed:
			resultChan <- fmt.Errorf("failed login: %s", recStr.String())
		case StatusUnMatch:
		default:

		}
		logger.Debugf("Sudo step result do not match any: %s", recStr.String())
		// 没有匹配到，继续等待
		time.Sleep(time.Millisecond * 100)
	}
}

func (s *SuSwitchService) handleResult(p []byte) matchStatus {
	newBytes := bytes.ReplaceAll(p, []byte("\r"), []byte("\n"))
	newBytes = bytes.ReplaceAll(newBytes, []byte("\n\n"), []byte("\n"))
	lineBytes := bytes.Split(newBytes, []byte("\n"))

	if s.usernameRegexp != nil && s.usernameRegexp.Match(p) {
		for _, line := range lineBytes {
			if s.usernameRegexp.Match(line) {
				_, _ = s.SrvConn.Write([]byte(s.cfg.SudoUsername + "\r"))
				logger.Debugf("Su switch step username pattern ok: %s", p)
				return StatusMatch
			}
		}
	}
	if s.passwordRegexp != nil && !s.inputAuthOnce {
		for _, line := range lineBytes {
			if s.passwordRegexp.Match(line) {
				_, _ = s.SrvConn.Write([]byte(s.cfg.SudoPassword + "\r"))
				s.inputAuthOnce = true
				logger.Debugf("Su switch step password pattern ok: %s", p)
				return StatusMatch
			}
		}
	}
	if s.needAuthOnce && s.inputAuthOnce {
		if s.failureRegexp != nil {
			for _, line := range lineBytes {
				if s.failureRegexp.Match(line) {
					logger.Debugf("Su switch step failed pattern ok: %s", p)
					return StatusFailed
				}
			}
		}
	}
	if s.successRegexp != nil {
		if s.needAuthOnce && !s.inputAuthOnce {
			logger.Debug("Su switch step need auth once but not input password")
			return StatusUnMatch
		}
		for _, line := range lineBytes {
			if s.successRegexp.Match(line) {
				logger.Debugf("Su switch step success pattern ok: %s", p)
				return StatusSuccess
			}
		}
	}
	return StatusUnMatch
}

type matchStatus int

const (
	StatusUnMatch matchStatus = 1
	StatusMatch   matchStatus = 2
	StatusSuccess matchStatus = 3
	StatusFailed  matchStatus = 4
)
