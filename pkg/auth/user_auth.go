package auth

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type authOptions struct {
	MFAType string
	Url     string
}

type UserAuthClient struct {
	*service.UserClient

	authOptions map[string]authOptions

	mfaTypes []string
}

func (u *UserAuthClient) Authenticate(ctx context.Context) (user model.User, authStatus StatusAuth) {
	authStatus = authFailed
	resp, err := u.UserClient.GetAPIToken()
	if err != nil {
		logger.Errorf("User %s Authenticate err: %s", u.Opts.Username, err)
		return
	}
	unsupportedMfaTypes := map[string]bool{
		"face": true,
		"FACE": true,
	}
	if resp.Err != "" {
		switch resp.Err {
		case ErrLoginConfirmWait:
			logger.Infof("User %s login need confirmation", u.Opts.Username)
			authStatus = authConfirmRequired
		case ErrMFARequired:
			u.mfaTypes = nil
			for _, choiceType := range resp.Data.Choices {
				if _, ok := unsupportedMfaTypes[choiceType]; ok {
					logger.Infof("User %s login need MFA, skip %s as it not supported", u.Opts.Username,
						choiceType)
					continue
				}

				u.authOptions[choiceType] = authOptions{
					MFAType: choiceType,
					Url:     resp.Data.Url,
				}
				u.mfaTypes = append(u.mfaTypes, choiceType)
			}
			logger.Infof("User %s login need MFA", u.Opts.Username)
			if len(u.mfaTypes) == 0 {
				logger.Warnf("User %s login need MFA, but no MFA options", u.Opts.Username)
			}
			authStatus = authMFARequired
		default:
			logger.Errorf("User %s login err: %s", u.Opts.Username, resp.Err)
		}
		return
	}
	if resp.Token != "" {
		return resp.User, authSuccess
	}
	return
}

func (u *UserAuthClient) CheckUserOTP(ctx context.Context, MFAType string, code string) (user model.User, authStatus StatusAuth) {
	authStatus = authFailed
	authData, ok := u.authOptions[MFAType]
	if !ok {
		logger.Errorf("User %s use %s check MFA not found", u.Opts.Username, MFAType)
		return
	}
	data := map[string]interface{}{
		"code":        code,
		"remote_addr": u.Opts.RemoteAddr,
		"login_type":  u.Opts.LoginType,
		"type":        authData.MFAType,
	}

	resp, err := u.UserClient.SendOTPRequest(&service.OTPRequest{
		ReqURL:  authData.Url,
		ReqBody: data,
	})
	if err != nil {
		logger.Errorf("User %s use %s check MFA err: %s", u.Opts.Username, authData.MFAType, err)
		return
	}
	if resp.Err != "" {
		logger.Errorf("User %s use %s check MFA err: %s", u.Opts.Username, authData.MFAType, resp.Err)
		return
	}
	if resp.Msg == "ok" {
		logger.Infof("User %s check MFA success, check if need admin confirm", u.Opts.Username)
		return u.Authenticate(ctx)
	}
	logger.Errorf("User %s failed to use %s check MFA", u.Opts.Username, authData.MFAType)
	return
}

func (u *UserAuthClient) GetMFAOptions() []string {
	return u.mfaTypes
}

func (u *UserAuthClient) CheckMFAAuth(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (ok bool) {
	username := u.Opts.Username
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	opts := u.mfaTypes
	count := 0
	var selectedMFAType string
	switch len(opts) {
	case 0:
		logger.Errorf("User %s has no MFA options", username)
		warningMsg := "No MFA options, please visit website to update your Multi-factor authentication."
		_, err := challenger(username, warningMsg, []string{"exit now"}, []bool{true})
		if err != nil {
			logger.Errorf("user %s happened err: %s", username, err)
			return
		}
		ctx.SetValue(ContextKeyAuthStatus, authFailed)
		return false

	case 1:
		// 仅有一个 option, 直接跳过选择界面
		selectedMFAType = opts[0]
	default:
		question := CreateSelectOptionsQuestion(opts)
	loop:
		for {
			if count > 3 {
				logger.Errorf("user %s select MFA type failed", username)
				return
			}
			answers, err := challenger(username, mfaSelectInstruction, []string{question}, []bool{true})
			if err != nil {
				logger.Errorf("user %s happened err: %s", username, err)
				return
			}
			count++
			if len(answers) == 1 {
				num, err2 := strconv.Atoi(answers[0])
				if err2 != nil {
					logger.Errorf("SSH conn[%s] user %s input wrong answer: %s", ctx.SessionID(), username, err2)
					continue
				}
				optIndex := num - 1
				if optIndex < 0 || optIndex >= len(opts) {
					logger.Errorf("SSH conn[%s] user %s input wrong index: %d", ctx.SessionID(), username, num)
					continue
				}
				selectedMFAType = opts[optIndex]
				break loop
			}

		}
	}
	if err := u.SelectMFAChoice(selectedMFAType); err != nil {
		logger.Errorf("SSH conn[%s] select MFA choice %s failed: %s", ctx.SessionID(), selectedMFAType, err)
		return
	}
	question := fmt.Sprintf(mfaOptionQuestion, strings.ToUpper(selectedMFAType))
	var code string
	for {
		answers, err := challenger(username, mfaOptionInstruction, []string{question}, []bool{true})
		if err != nil {
			logger.Errorf("SSH conn[%s] user %s happened err: %s", ctx.SessionID(), username, err)
			return
		}
		if len(answers) == 1 && answers[0] != "" {
			code = answers[0]
			break
		}
	}
	user, authStatus := u.CheckUserOTP(ctx, selectedMFAType, code)
	switch authStatus {
	case authSuccess:
		ctx.SetValue(ContextKeyUser, &user)
		ok = true
		logger.Infof("SSH conn[%s] %s MFA for %s from %s", ctx.SessionID(),
			actionAccepted, username, remoteAddr)
	case authConfirmRequired:
		logger.Infof("SSH conn[%s] %s MFA for %s from %s as login confirm", ctx.SessionID(),
			actionPartialAccepted, username, remoteAddr)
		ctx.SetValue(ContextKeyAuthStatus, authConfirmRequired)
		ok = u.CheckConfirmAuth(ctx, challenger)
	default:
		logger.Errorf("SSH conn[%s] %s MFA for %s from %s", ctx.SessionID(),
			actionFailed, username, remoteAddr)
	}
	return
}

func (u *UserAuthClient) CheckConfirmAuth(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (ok bool) {
	username := u.Opts.Username
	logger.Infof("SSH conn[%s] checking user %s login confirm", ctx.SessionID(), username)
	var waitCheck bool
loop:
	for {
		answers, err := challenger(username, confirmInstruction, []string{confirmQuestion}, []bool{true})
		if err != nil {
			u.CancelConfirm()
			logger.Errorf("SSH conn[%s] user %s happened err: %s", ctx.SessionID(), username, err)
			return
		}
		if len(answers) == 1 {
			switch strings.TrimSpace(strings.ToLower(answers[0])) {
			case "yes", "y", "":
				waitCheck = true
				break loop
			case "no", "n":
				waitCheck = false
				break loop
			default:
				continue
			}
		}
	}
	if !waitCheck {
		logger.Infof("SSH conn[%s] user %s cancel login", ctx.SessionID(), username)
		u.CancelConfirm()
		failed := true
		ctx.SetValue(ContextKeyAuthFailed, &failed)
		logger.Infof("SSH conn[%s] checking user %s login confirm failed", ctx.SessionID(), username)
		return
	}
	user, authStatus := u.CheckConfirm(ctx)
	switch authStatus {
	case authSuccess:
		ctx.SetValue(ContextKeyUser, &user)
		logger.Infof("SSH conn[%s] checking user %s login confirm success", ctx.SessionID(), username)
		ok = true
	default:
		failed := true
		ctx.SetValue(ContextKeyAuthFailed, &failed)
		logger.Infof("SSH conn[%s] checking user %s login confirm failed", ctx.SessionID(), username)
	}
	return
}

const (
	ErrLoginConfirmWait     = "login_confirm_wait"
	ErrLoginConfirmRejected = "login_confirm_rejected"
	ErrMFARequired          = "mfa_required"
)

func (u *UserAuthClient) CheckConfirm(ctx context.Context) (user model.User, authStatus StatusAuth) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Errorf("User %s exit and cancel confirmation", u.Opts.Username)
			u.CancelConfirm()
			return
		case <-t.C:
			resp, err := u.UserClient.CheckConfirmAuthStatus()
			if err != nil {
				logger.Errorf("User %s check confirm err: %s", u.Opts.Username, err)
				return
			}
			if resp.Err != "" {
				switch resp.Err {
				case ErrLoginConfirmWait:
					logger.Infof("User %s still wait confirm", u.Opts.Username)
					continue
				case ErrLoginConfirmRejected:
					logger.Infof("User %s confirmation was rejected by admin", u.Opts.Username)
				default:
					logger.Infof("User %s confirmation was rejected by err: %s", u.Opts.Username, resp.Err)
				}
				return
			}
			if resp.Msg == "ok" {
				logger.Infof("User %s confirmation was accepted", u.Opts.Username)
				return u.Authenticate(ctx)
			}
		}
	}
}

func (u *UserAuthClient) CancelConfirm() {
	err := u.UserClient.CancelConfirmAuth()
	if err != nil {
		logger.Errorf("Cancel User %s confirmation err: %s", u.Opts.Username, err)
		return
	}
	logger.Infof("Cancel User %s confirmation success", u.Opts.Username)
}

type StatusAuth int64

const (
	authSuccess StatusAuth = iota + 1
	authFailed
	authMFARequired
	authConfirmRequired
)
