package srvconn

import (
	"errors"
	"fmt"
	"strings"
)

func LoginToSSHSu(sc *SSHConnection) error {
	cfg := sc.options.suConfig
	suService, err := NewSuService(cfg, sc)
	if err != nil {
		return err
	}
	switch cfg.MethodType {
	case SuMethodSu, SuMethodSudo,
		SuMethodOnlySudo, SuMethodOnlySu:
		startCmd := cfg.SuCommand()
		suService.execCommand = func() {
			_ = sc.session.Start(startCmd)
		}
	default:
		_ = sc.session.Shell()
	}
	return suService.RunSwitchUser()
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

func createHuaweiH3CSuccessPattern(username string) string {
	return huaweiH3CPs1Mark
}

const (
	normalUserMark = "\\s*\\$"
	superUserMark  = "\\s*#"
)

const (
	/*
	   huawei、h3c 的终端提示符
	*/
	huaweiH3CPs1Mark = "^<.*>"
)

const (
	/*
		Linux 相关
	*/

	LinuxSuCommand = "su - %s; exit"

	LinuxSudoCommand = "sudo su - %s; exit"

	LinuxOnlySuCommand = "su %s; exit"

	LinuxOnlySudoCommand = "sudo su %s; exit"

	/*
		Cisco 相关
	*/

	SuCommandEnable = "enable"

	/*
		huawei 相关
	*/

	SuCommandSuper = "super 15"

	/*
		h3c super 相关
	*/

	SuCommandSuperH3C = "super level-15"

	/*
	 \b: word boundary 即: 匹配某个单词边界
	*/

	passwordMatchPattern = "(?i)\\bpassword\\b\\s*|密码|password\\s*[:：]\\s*"

	usernameMatchPattern = "(?i)username:?\\s*$|name:?\\s*$|用户名:?\\s*$"
)

// 收集完善切换用户失败的提示信息

var switchPasswordFailures = []string{
	"password has not been set",
	"wrong\\s*passwords",
	"bad\\s*secrets",
	"access\\s*denied",
	"authentication\\s*failure",
	"invalid\\s*password",
}

func createFailedPattern() string {
	allFailure := strings.Join(switchPasswordFailures, "|")
	return fmt.Sprintf("(?i)%s", allFailure)
}

var ErrorTimeout = errors.New("i/o timeout")

type SUMethodType string

const (
	SuMethodSudo       SUMethodType = "sudo"
	SuMethodSu         SUMethodType = "su"
	SuMethodOnlySudo   SUMethodType = "only_sudo"
	SuMethodOnlySu     SUMethodType = "only_su"
	SuMethodEnable     SUMethodType = "enable"
	SuMethodSuper      SUMethodType = "super"
	SuMethodSuperLevel SUMethodType = "super_level"
)

func NewSuMethodType(suMethod string) SUMethodType {
	method := strings.ToLower(suMethod)
	switch method {
	case "enable":
		return SuMethodEnable
	case "super":
		return SuMethodSuper
	case "super_level":
		return SuMethodSuperLevel
	case "su":
		return SuMethodSu
	case "sudo":
		return SuMethodSudo
	case "only_sudo":
		return SuMethodOnlySudo
	case "only_su":
		return SuMethodOnlySu
	default:

	}
	return SuMethodSu
}

type SuConfig struct {
	MethodType   SUMethodType
	SudoUsername string
	SudoPassword string
}

func (s *SuConfig) SuCommand() string {
	switch s.MethodType {
	case SuMethodEnable:
		return SuCommandEnable
	case SuMethodSuper:
		return SuCommandSuper
	case SuMethodSuperLevel:
		return SuCommandSuperH3C
	case SuMethodSudo:
		return fmt.Sprintf(LinuxSudoCommand, s.SudoUsername)
	case SuMethodOnlySudo:
		return fmt.Sprintf(LinuxOnlySudoCommand, s.SudoUsername)
	case SuMethodOnlySu:
		return fmt.Sprintf(LinuxOnlySuCommand, s.SudoUsername)
	default:

	}
	return fmt.Sprintf(LinuxSuCommand, s.SudoUsername)
}

func (s *SuConfig) UsernameMatchPattern() string {
	return usernameMatchPattern
}

func (s *SuConfig) PasswordMatchPattern() string {
	return passwordMatchPattern
}

func (s *SuConfig) SuccessPattern() string {
	switch s.MethodType {
	case SuMethodEnable:
		return createCiscoSuccessPattern(s.SudoUsername)
	case SuMethodSuper, SuMethodSuperLevel:
		return createHuaweiH3CSuccessPattern(s.SudoUsername)
	default:

	}
	return createLinuxSuccessPattern(s.SudoUsername)
}
