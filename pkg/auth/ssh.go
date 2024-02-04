package auth

import (
	"errors"
	"net"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver/koko/pkg/config"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type SSHAuthFunc func(ctx ssh.Context, password, publicKey string) (res ssh.AuthResult)

func SSHPasswordAndPublicKeyAuth(jmsService *service.JMService) SSHAuthFunc {
	return func(ctx ssh.Context, password, publicKey string) (res ssh.AuthResult) {
		if password == "" && publicKey == "" {
			logger.Errorf("SSH conn[%s] no password and publickey", ctx.SessionID())
			return ssh.AuthFailed
		}
		remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
		username := ctx.User()
		if req, ok := parseDirectLoginReq(jmsService, ctx); ok {
			if req.IsToken() && req.Authenticate(password) {
				ctx.SetValue(ContextKeyUser, &req.ConnectToken.User)
				logger.Infof("SSH conn[%s] %s for %s from %s", ctx.SessionID(),
					actionAccepted, username, remoteAddr)
				return ssh.AuthSuccessful
			}
			username = req.User()
		}
		authMethod := "publickey"
		action := actionAccepted
		res = ssh.AuthFailed
		if password != "" {
			authMethod = "password"
		}
		newClient := jmsService.CloneClient()
		var accessKey model.AccessKey
		conf := config.GetConf()
		_ = accessKey.LoadFromFile(conf.AccessKeyFilePath)
		userClient := service.NewUserClient(
			service.UserClientUsername(username),
			service.UserClientRemoteAddr(remoteAddr),
			service.UserClientLoginType("T"),
			service.UserClientHttpClient(&newClient),
			service.UserClientSvcSignKey(accessKey),
		)
		userAuthClient := &UserAuthClient{
			UserClient:  userClient,
			authOptions: make(map[string]authOptions),
		}
		ctx.SetValue(ContextKeyClient, userAuthClient)
		userAuthClient.SetOption(service.UserClientPassword(password),
			service.UserClientPublicKey(publicKey))
		logger.Infof("SSH conn[%s] authenticating user %s %s", ctx.SessionID(), username, authMethod)
		user, authStatus := userAuthClient.Authenticate(ctx)
		switch authStatus {
		case authMFARequired:
			action = actionPartialAccepted
			res = ssh.AuthPartiallySuccessful
			ctx.SetValue(ContextKeyAuthStatus, authMFARequired)
		case authSuccess:
			res = ssh.AuthSuccessful
			ctx.SetValue(ContextKeyUser, &user)
		case authConfirmRequired:
			action = actionPartialAccepted
			res = ssh.AuthPartiallySuccessful
			ctx.SetValue(ContextKeyAuthStatus, authConfirmRequired)
		default:
			action = actionFailed
		}
		logger.Infof("SSH conn[%s] %s %s for %s from %s", ctx.SessionID(),
			action, authMethod, username, remoteAddr)
		return
	}
}

func SSHKeyboardInteractiveAuth(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (res ssh.AuthResult) {
	if value, ok := ctx.Value(ContextKeyAuthFailed).(*bool); ok && *value {
		return ssh.AuthFailed
	}
	// 2 steps auth must have a partial success method
	if val := ctx.Value(ContextKeyPartialSuccessMethod); val == nil {
		logger.Errorf("SSH conn[%s] user %s Mfa Auth failed: not found partial success method.",
			ctx.SessionID(), ctx.User())
		return ssh.AuthFailed
	}

	username := GetUsernameFromSSHCtx(ctx)
	res = ssh.AuthFailed
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
		res = ssh.AuthSuccessful
	}
	return
}

func SSHAuthLogCallback(ctx ssh.Context, method string, err error) {
	if err == nil {
		logger.Errorf("SSH conn[%s] auth method %s success", ctx.SessionID(), method)
		return
	}
	if errors.Is(err, gossh.ErrPartialSuccess) {
		ctx.SetValue(ContextKeyPartialSuccessMethod, method)
		logger.Infof("SSH conn[%s] auth method %s partially success", ctx.SessionID(), method)
	} else {
		logger.Errorf("SSH conn[%s] auth method %s failed: %s", ctx.SessionID(), method, err)
	}
}

const (
	ContextKeyUser   = "CONTEXT_USER"
	ContextKeyClient = "CONTEXT_CLIENT"

	ContextKeyAuthStatus = "CONTEXT_AUTH_STATUS"

	ContextKeyAuthFailed = "CONTEXT_AUTH_FAILED"

	ContextKeyDirectLoginFormat = "CONTEXT_DIRECT_LOGIN_FORMAT"

	ContextKeyPartialSuccessMethod = "CONTEXT_PARTIAL_SUCCESS_METHOD"
)

type DirectLoginAssetReq struct {
	Username        string
	Protocol        string
	AccountUsername string
	AssetTarget     string
	ConnectToken    *model.ConnectToken
}

func (d *DirectLoginAssetReq) Authenticate(password string) bool {
	return d.ConnectToken.Value == password
}

func (d *DirectLoginAssetReq) IsToken() bool {
	return d.ConnectToken != nil
}

func (d *DirectLoginAssetReq) User() string {
	if d.IsToken() && d.ConnectToken.User.ID != "" {
		return d.ConnectToken.User.Username
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

const (
	sshProtocolLen  = 3
	withProtocolLen = 4
)

func parseUserFormatBySeparator(s, Separator string) (DirectLoginAssetReq, bool) {
	authInfos := strings.Split(s, Separator)
	var req DirectLoginAssetReq
	switch len(authInfos) {
	case sshProtocolLen:
		req = DirectLoginAssetReq{
			Username:        authInfos[0],
			Protocol:        model.ProtocolSSH,
			AccountUsername: authInfos[1],
			AssetTarget:     authInfos[2],
		}
	case withProtocolLen:
		req = DirectLoginAssetReq{
			Username:        authInfos[0],
			Protocol:        authInfos[1],
			AccountUsername: authInfos[2],
			AssetTarget:     authInfos[3],
		}
	default:
		return DirectLoginAssetReq{}, false

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
		if connectToken, err := jmsService.GetConnectTokenInfo(token); err == nil {
			req := DirectLoginAssetReq{ConnectToken: &connectToken,
				Protocol: connectToken.Protocol}
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
