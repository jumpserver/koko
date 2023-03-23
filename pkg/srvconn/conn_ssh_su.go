package srvconn

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

func LoginToSSHSu(sc *SSHConnection) error {
	cfg := sc.options.suUserConfig
	sudoCommand := cfg.SuCommand()
	password := cfg.SuPassword()
	successPattern := cfg.SuccessPattern()
	passwordPattern := cfg.PasswordMatchPattern()
	steps := make([]stepItem, 0, 2)
	if cfg.MethodType == SuMethodSu {
		steps = append(steps,
			stepItem{
				Input:           sudoCommand,
				ExpectPattern:   passwordPattern,
				FinishedPattern: successPattern,
				execCommand: func() error {
					return sc.session.Start(sudoCommand)
				},
			})
	} else {
		_ = sc.session.Shell()
		steps = append(steps, stepItem{
			Input:           sudoCommand,
			ExpectPattern:   passwordPattern,
			FinishedPattern: successPattern,
		})
	}

	steps = append(steps,
		stepItem{
			Input:           password,
			ExpectPattern:   successPattern,
			FinishedPattern: successPattern,
		},
	)
	for i := 0; i < len(steps); i++ {
		finished, err := executeStep(&steps[i], sc)
		if err != nil {
			return err
		}
		if finished {
			break
		}
	}
	return nil
}

func executeStep(step *stepItem, sc io.ReadWriteCloser) (bool, error) {
	return step.Execute(sc)
}

/*

1、执行 su | enable | system-view  相关命令
2、等待密码输入 prompt (可能直接切换成功)

3、输入密码
4、等待成功提示

完成

*/

type stepItem struct {
	Input           string
	ExpectPattern   string
	execCommand     func() error
	FinishedPattern string
}

func (s *stepItem) Execute(sc io.ReadWriteCloser) (bool, error) {
	resultChan := make(chan *ExecuteResult, 1)
	matchReg, err := regexp.Compile(s.ExpectPattern)
	if err != nil {
		logger.Errorf("Su step expect pattern %s compile failed: %s", s.ExpectPattern, err)
	}
	successReg, err := regexp.Compile(s.FinishedPattern)
	if err != nil {
		logger.Errorf("Su step success pattern %s compile failed: %s", s.FinishedPattern, err)
	}
	if s.execCommand != nil {
		_ = s.execCommand()
	} else {
		_, _ = sc.Write([]byte(s.Input + "\r\n"))
	}
	go func() {
		buf := make([]byte, 8192)
		var recStr strings.Builder
		for {
			nr, err2 := sc.Read(buf)
			if err2 != nil {
				resultChan <- &ExecuteResult{Err: err2}
				return
			}
			recStr.Write(buf[:nr])
			result := strings.TrimSpace(recStr.String())
			if successReg != nil && successReg.MatchString(result) {
				resultChan <- &ExecuteResult{Finished: true}
				logger.Debugf("Sudo step success pattern ok: %s", result)
				return
			}
			if matchReg != nil && matchReg.MatchString(result) {
				resultChan <- &ExecuteResult{}
				logger.Debugf("Sudo step match pattern ok: %s", result)
				return
			}
			logger.Debugf("Sudo step result do not match any: %s", result)
		}
	}()
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()
	select {
	case ret := <-resultChan:
		return ret.Finished, ret.Err
	case <-ticker.C:
	}
	return false, ErrorTimeout
}

type ExecuteResult struct {
	Finished bool
	Err      error
}

func createLinuxSuccessPattern(username string) string {
	pattern := fmt.Sprintf("%s@", username)
	pattern = fmt.Sprintf("(?i)%s|%s|%s", pattern,
		normalUserMark, superUserMark)
	return pattern
}

func createCiscoSuccessPattern(username string) string {
	return fmt.Sprintf("%s|%s", normalUserMark, superUserMark)
}

const (
	normalUserMark = "\\s*\\$"
	superUserMark  = "\\s*#"
)

const (
	/*
		Linux 相关
	*/

	LinuxSuCommand = "su - %s; exit"

	/*
		Cisco 相关
	*/

	SuCommandEnable = "enable"

	/*
		huawei 相关
	*/

	//SuCommandSystemView = "system-view"

	/*
	 \b: word boundary 即: 匹配某个单词边界
	*/

	passwordMatchPattern = "(?i)\\bpassword\\b|密码"
)

var ErrorTimeout = errors.New("time out")

type SUMethodType string

const (
	SuMethodSu         SUMethodType = "su"
	SuMethodEnable     SUMethodType = "enable"
	SuMethodSystemView SUMethodType = "system-view"
)

func NewSuMethodType(suMethod string) SUMethodType {
	method := strings.ToLower(suMethod)
	switch method {
	case "enable":
		return SuMethodEnable
	case "system-view":
		return SuMethodSystemView
	case "sudo", "su":
		return SuMethodSu
	}
	return SuMethodSu
}

type SuConfig struct {
	MethodType   SUMethodType
	SudoUsername string
	SudoPassword string
}

func (s SuConfig) SuCommand() string {
	switch s.MethodType {
	case SuMethodEnable:
		return SuCommandEnable
	//case SuMethodSystemView:
	//	return SuCommandSystemView
	default:

	}
	return fmt.Sprintf(LinuxSuCommand, s.SudoUsername)
}

func (s SuConfig) SuUsername() string {
	return s.SudoUsername
}

func (s SuConfig) SuPassword() string {
	return s.SudoPassword
}

func (s SuConfig) PasswordMatchPattern() string {
	return passwordMatchPattern
}

func (s SuConfig) SuccessPattern() string {
	switch s.MethodType {
	case SuMethodEnable:
		return createCiscoSuccessPattern(s.SudoUsername)
	default:

	}
	return createLinuxSuccessPattern(s.SudoUsername)
}
