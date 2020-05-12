package auth

import (
	"net"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

var mfaInstruction = "Please enter 6 digits."
var mfaQuestion = "[MFA auth]: "

var confirmInstruction = "Please wait for your admin to confirm."
var confirmQuestion = "Do you want to continue [Y/n]? : "

const (
	actionAccepted        = "Accepted"
	actionFailed          = "Failed"
	actionPartialAccepted = "Partial accepted"
)

func checkAuth(ctx ssh.Context, password, publicKey string) (res ssh.AuthResult) {
	username := ctx.User()
	authMethod := "publickey"
	action := actionAccepted
	res = ssh.AuthFailed
	if password != "" {
		authMethod = "password"
	}
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	userClient, ok := ctx.Value(model.ContextKeyClient).(*service.SessionClient)
	if !ok {
		sessionClient := service.NewSessionClient(service.Username(username),
			service.RemoteAddr(remoteAddr), service.LoginType("T"))
		userClient = &sessionClient
		ctx.SetValue(model.ContextKeyClient, userClient)
	}
	userClient.SetOption(service.Password(password), service.PublicKey(publicKey))
	logger.Infof("SSH conn[%s] authenticating user %s", ctx.SessionID(), username)
	user, authStatus := userClient.Authenticate(ctx)
	switch authStatus {
	case service.AuthMFARequired:
		action = actionPartialAccepted
		res = ssh.AuthPartiallySuccessful
	case service.AuthSuccess:
		res = ssh.AuthSuccessful
		ctx.SetValue(model.ContextKeyUser, &user)
	case service.AuthConfirmRequired:
		required := true
		ctx.SetValue(model.ContextKeyConfirmRequired, &required)
		action = actionPartialAccepted
		res = ssh.AuthPartiallySuccessful
	default:
		action = actionFailed
	}
	logger.Infof("SSH conn[%s] %s %s for %s from %s", ctx.SessionID(),
		action, authMethod, username, remoteAddr)
	return

}

func CheckUserPassword(ctx ssh.Context, password string) ssh.AuthResult {
	if !config.GetConf().PasswordAuth {
		return ssh.AuthFailed
	}
	return checkAuth(ctx, password, "")
}

func CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
	if !config.GetConf().PublicKeyAuth {
		return ssh.AuthFailed
	}
	b := key.Marshal()
	publicKey := common.Base64Encode(string(b))
	return checkAuth(ctx, "", publicKey)
}

func CheckMFA(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (res ssh.AuthResult) {
	if value, ok := ctx.Value(model.ContextKeyConfirmFailed).(*bool); ok && *value {
		return ssh.AuthFailed
	}

	username := ctx.User()
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	res = ssh.AuthFailed

	var confirmAction bool
	instruction := mfaInstruction
	question := mfaQuestion

	client, ok := ctx.Value(model.ContextKeyClient).(*service.SessionClient)
	if !ok {
		logger.Errorf("SSH conn[%s] user %s Mfa Auth failed: not found session client.",
			ctx.SessionID(), username)
		return
	}
	value, ok := ctx.Value(model.ContextKeyConfirmRequired).(*bool)
	if ok && *value {
		confirmAction = true
		instruction = confirmInstruction
		question = confirmQuestion
	}
	answers, err := challenger(username, instruction, []string{question}, []bool{true})
	if err != nil || len(answers) != 1 {
		if confirmAction {
			client.CancelConfirm()
		}
		logger.Errorf("SSH conn[%s] user %s happened err: %s", ctx.SessionID(), username, err)
		return
	}
	if confirmAction {
		switch strings.TrimSpace(strings.ToLower(answers[0])) {
		case "yes", "y", "":
			logger.Infof("SSH conn[%s] checking user %s login confirm", ctx.SessionID(), username)
			user, authStatus := client.CheckConfirm(ctx)
			switch authStatus {
			case service.AuthSuccess:
				res = ssh.AuthSuccessful
				ctx.SetValue(model.ContextKeyUser, &user)
				logger.Infof("SSH conn[%s] checking user %s login confirm success", ctx.SessionID(), username)
				return
			}
		case "no", "n":
			logger.Infof("SSH conn[%s] user %s cancel login", ctx.SessionID(), username)
			client.CancelConfirm()
		default:
			return
		}
		failed := true
		ctx.SetValue(model.ContextKeyConfirmFailed, &failed)
		logger.Infof("SSH conn[%s] checking user %s login confirm failed", ctx.SessionID(), username)
		return
	}
	mfaCode := answers[0]
	logger.Infof("SSH conn[%s] checking user %s mfa code", ctx.SessionID(), username)
	user, authStatus := client.CheckUserOTP(ctx, mfaCode)
	switch authStatus {
	case service.AuthSuccess:
		res = ssh.AuthSuccessful
		ctx.SetValue(model.ContextKeyUser, &user)
		logger.Infof("SSH conn[%s] %s MFA for %s from %s", ctx.SessionID(),
			actionAccepted, username, remoteAddr)
	case service.AuthConfirmRequired:
		res = ssh.AuthPartiallySuccessful
		required := true
		ctx.SetValue(model.ContextKeyConfirmRequired, &required)
		logger.Infof("SSH conn[%s] %s MFA for %s from %s", ctx.SessionID(),
			actionPartialAccepted, username, remoteAddr)
	default:
		logger.Errorf("SSH conn[%s] %s MFA for %s from %s", ctx.SessionID(),
			actionFailed, username, remoteAddr)
	}
	return
}

func MFAAuthMethods(ctx ssh.Context) (methods []string) {
	return []string{"keyboard-interactive"}
}
