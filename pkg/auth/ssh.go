package auth

import (
	"net"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshd"
)

type SSHAuthFunc func(ctx ssh.Context, password, publicKey string) (res sshd.AuthStatus)

func SSHPasswordAndPublicKeyAuth(jmsService *service.JMService) SSHAuthFunc {
	return func(ctx ssh.Context, password, publicKey string) (res sshd.AuthStatus) {
		remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
		username := ctx.User()
		if req, ok := parseDirectLoginReq(jmsService, ctx); ok {
			if req.IsToken() && req.Authenticate(password) {
				ctx.SetValue(ContextKeyUser, req.Info.User)
				logger.Infof("SSH conn[%s] %s for %s from %s", ctx.SessionID(),
					actionAccepted, ctx.User(), remoteAddr)
				return sshd.AuthSuccessful
			}
			username = req.User()
		}
		authMethod := "publickey"
		action := actionAccepted
		res = sshd.AuthFailed
		if password != "" {
			authMethod = "password"
		}
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
	Info        *model.ConnectTokenInfo
}

func (d *DirectLoginAssetReq) IsUUIDString() bool {
	for _, item := range []string{d.SysUserInfo, d.AssetInfo} {
		if !common.ValidUUIDString(item) {
			return false
		}
	}
	return true
}

func (d *DirectLoginAssetReq) Authenticate(password string) bool {
	return d.Info.Secret == password
}

func (d *DirectLoginAssetReq) IsToken() bool {
	return d.Info != nil
}

func (d *DirectLoginAssetReq) User() string {
	if d.IsToken() && d.Info.User != nil {
		return d.Info.User.Username
	}
	return d.Username
}

const (
	SeparatorATSign   = "@"
	SeparatorHashMark = "#"

	/*
		格式为: JMS-{token}

	*/
	tokenPrefix = "JMS-"
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

func parseDirectLoginReq(jmsService *service.JMService, ctx ssh.Context) (*DirectLoginAssetReq, bool) {
	if req, ok := ctx.Value(ContextKeyDirectLoginFormat).(*DirectLoginAssetReq); ok {
		return req, true
	}
	if req, ok := parseJMSTokenLoginReq(jmsService, ctx); ok {
		ctx.SetValue(ContextKeyDirectLoginFormat, req)
		return req, true
	}
	if req, ok := parseUsernameFormatReq(ctx); ok {
		ctx.SetValue(ContextKeyDirectLoginFormat, req)
		return req, true
	}
	return nil, false
}

func parseJMSTokenLoginReq(jmsService *service.JMService, ctx ssh.Context) (*DirectLoginAssetReq, bool) {
	if strings.HasPrefix(ctx.User(), tokenPrefix) {
		token := strings.TrimPrefix(ctx.User(), tokenPrefix)
		if resp, err := jmsService.GetConnectTokenAuth(token); err == nil {
			req := DirectLoginAssetReq{Info: &resp.Info}
			return &req, true
		} else {
			logger.Errorf("Check user token %s failed: %s", ctx.User(), err)
		}
	}
	return nil, false
}

func parseUsernameFormatReq(ctx ssh.Context) (*DirectLoginAssetReq, bool) {
	if req, ok := ParseDirectUserFormat(ctx.User()); ok {
		return &req, true
	}
	return nil, false
}
