package auth

import (
	"net"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshd"
)

type SSHAuthFunc func(ctx ssh.Context, password, publicKey string) (res sshd.AuthStatus)

func SSHPasswordAndPublicKeyAuth(jmsService *service.JMService) SSHAuthFunc {
	return func(ctx ssh.Context, password, publicKey string) (res sshd.AuthStatus) {
		username := GetUsernameFromSSHCtx(ctx)
		authMethod := "publickey"
		action := actionAccepted
		res = sshd.AuthFailed
		if password != "" {
			authMethod = "password"
		}
		remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
		userAuthClient, ok := ctx.Value(ContextKeyClient).(*UserAuthClient)
		if !ok {
			newClient := jmsService.CloneClient()

			userClient := service.NewUserClient(
				service.UserClientUsername(username),
				service.UserClientRemoteAddr(remoteAddr),
				service.UserClientLoginType("T"),
				service.UserClientHttpClient(&newClient),
			)
			userAuthClient = &UserAuthClient{
				UserClient:  userClient,
				authOptions: make(map[string]authOptions),
			}
			ctx.SetValue(ContextKeyClient, userAuthClient)
		}
		userAuthClient.SetOption(service.UserClientPassword(password),
			service.UserClientPublicKey(publicKey))
		logger.Infof("SSH conn[%s] authenticating user %s %s", ctx.SessionID(), username, authMethod)
		user, authStatus := userAuthClient.Authenticate(ctx)
		switch authStatus {
		case authMFARequired:
			action = actionPartialAccepted
			res = sshd.AuthPartiallySuccessful
			ctx.SetValue(ContextKeyAuthStatus, authMFARequired)
		case authSuccess:
			res = sshd.AuthSuccessful
			ctx.SetValue(ContextKeyUser, &user)
		case authConfirmRequired:
			action = actionPartialAccepted
			res = sshd.AuthPartiallySuccessful
			ctx.SetValue(ContextKeyAuthStatus, authConfirmRequired)
		default:
			action = actionFailed
		}
		logger.Infof("SSH conn[%s] %s %s for %s from %s", ctx.SessionID(),
			action, authMethod, username, remoteAddr)
		return
	}
}

func SSHKeyboardInteractiveAuth(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (res sshd.AuthStatus) {
	if value, ok := ctx.Value(ContextKeyAuthFailed).(*bool); ok && *value {
		return sshd.AuthFailed
	}
	username := GetUsernameFromSSHCtx(ctx)
	res = sshd.AuthFailed
	client, ok := ctx.Value(ContextKeyClient).(*UserAuthClient)
	if !ok {
		logger.Errorf("SSH conn[%s] user %s Mfa Auth failed: not found session client.",
			ctx.SessionID(), username)
		return
	}
	status, ok2 := ctx.Value(ContextKeyAuthStatus).(StatusAuth)
	if !ok2 {
		logger.Errorf("SSH conn[%s] user %s unknown auth", ctx.SessionID(), username)
		return
	}
	var checkAuth func(ssh.Context, gossh.KeyboardInteractiveChallenge) bool
	switch status {
	case authConfirmRequired:
		checkAuth = client.CheckConfirmAuth
	case authMFARequired:
		checkAuth = client.CheckMFAAuth
	}
	if checkAuth != nil && checkAuth(ctx, challenger) {
		res = sshd.AuthSuccessful
	}
	return
}

const (
	ContextKeyUser   = "CONTEXT_USER"
	ContextKeyClient = "CONTEXT_CLIENT"

	ContextKeyAuthStatus = "CONTEXT_AUTH_STATUS"

	ContextKeyAuthFailed = "CONTEXT_AUTH_FAILED"

	ContextKeyDirectLoginFormat = "CONTEXT_DIRECT_LOGIN_FORMAT"
)

type DirectLoginAssetReq struct {
	Username    string
	SysUserInfo string
	AssetInfo   string
}

func (d *DirectLoginAssetReq) IsUUIDString() bool {
	for _, item := range []string{d.SysUserInfo, d.AssetInfo} {
		if !common.ValidUUIDString(item) {
			return false
		}
	}
	return true
}

const (
	SeparatorATSign   = "@"
	SeparatorHashMark = "#"
)

func parseUserFormatBySeparator(s, Separator string) (DirectLoginAssetReq, bool) {
	authInfos := strings.Split(s, Separator)
	if len(authInfos) != 3 {
		return DirectLoginAssetReq{}, false
	}
	req := DirectLoginAssetReq{
		Username:    authInfos[0],
		SysUserInfo: authInfos[1],
		AssetInfo:   authInfos[2],
	}
	return req, true
}

func ParseDirectUserFormat(s string) (DirectLoginAssetReq, bool) {
	for _, separator := range []string{SeparatorATSign, SeparatorHashMark} {
		if req, ok := parseUserFormatBySeparator(s, separator); ok {
			return req, true
		}
	}
	return DirectLoginAssetReq{}, false
}

func GetUsernameFromSSHCtx(ctx ssh.Context) string {
	if directReq, ok := ctx.Value(ContextKeyDirectLoginFormat).(*DirectLoginAssetReq); ok {
		return directReq.Username
	}
	username := ctx.User()
	if req, ok := ParseDirectUserFormat(username); ok {
		username = req.Username
		ctx.SetValue(ContextKeyDirectLoginFormat, &req)
	}
	return username
}
