package srvconn

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

func LoginToSu(sc *SSHConnection) error {
	successPattern := createSuccessParttern(sc.options.sudoUsername)
	steps := make([]stepItem, 0, 2)
	steps = append(steps,
		stepItem{
			Input:           sc.options.sudoCommand,
			ExpectPattern:   passwordMatchPattern,
			FinishedPattern: successPattern,
			IsCommand:       true,
		},
		stepItem{
			Input:           sc.options.sudoPassword,
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

func executeStep(step *stepItem, sc *SSHConnection) (bool, error) {
	return step.Execute(sc)
}

const (
	SU     = "su"
	ENABLE = "enable"
	OTHER  = "other"
)

const (
	LinuxSuCommand = "su - %s; exit"

	SwitchSuCommand = "enable"

	passwordMatchPattern = "(?i)password|密码"
)

var ErrorTimeout = errors.New("time out")

type stepItem struct {
	Input           string
	ExpectPattern   string
	IsCommand       bool
	FinishedPattern string
}

func (s *stepItem) Execute(sc *SSHConnection) (bool, error) {
	resultChan := make(chan *ExecuteResult, 1)
	matchReg, err := regexp.Compile(s.ExpectPattern)
	if err != nil {
		logger.Error(err)
	}
	successReg, err := regexp.Compile(s.FinishedPattern)
	if err != nil {
		logger.Error(err)
	}
	if s.IsCommand {
		_ = sc.session.Start(s.Input)
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
			if matchReg != nil && matchReg.MatchString(result) {
				resultChan <- &ExecuteResult{}
				return
			}
			if successReg != nil && successReg.MatchString(result) {
				resultChan <- &ExecuteResult{Finished: true}
				return
			}
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

func createSuccessParttern(username string) string {
	pattern := fmt.Sprintf("%s@", username)
	pattern = fmt.Sprintf("(?i)%s|%s|%s", pattern,
		normalUserMark, superUserMark)
	return pattern
}

const (
	normalUserMark = "\\s*\\$"
	superUserMark  = "\\s*#"
)
