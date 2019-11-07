package auth

import (
	"net"

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
	userClient := service.NewSessionClient(service.Username(username),
		service.Password(password), service.PublicKey(publicKey),
		service.RemoteAddr(remoteAddr), service.LoginType("T"))
	user, authStatus := userClient.Authenticate(ctx)

	switch authStatus {
	case service.AuthMFARequired:
		ctx.SetValue(model.ContextKeyClient, &userClient)
		action = actionPartialAccepted
		res = ssh.AuthPartiallySuccessful
	case service.AuthSuccess:
		res = ssh.AuthSuccessful
		ctx.SetValue(model.ContextKeyUser, &user)

	default:
		action = actionFailed
	}
	logger.Infof("%s %s for %s from %s", action, authMethod, username, remoteAddr)
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
	username := ctx.User()
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	res = ssh.AuthFailed
	defer func() {
		authMethod := "MFA"
		if res == ssh.AuthSuccessful {
			action := actionAccepted
			logger.Infof("%s %s for %s from %s", action, authMethod, username, remoteAddr)
		} else {
			action := actionFailed
			logger.Errorf("%s %s for %s from %s", action, authMethod, username, remoteAddr)
		}
	}()
	answers, err := challenger(username, mfaInstruction, []string{mfaQuestion}, []bool{true})
	if err != nil || len(answers) != 1 {
		return
	}
	mfaCode := answers[0]
	client, ok := ctx.Value(model.ContextKeyClient).(*service.SessionClient)
	if !ok {
		logger.Errorf("User %s Mfa Auth failed: not found session client.", username, )
		return
	}
	user, authStatus := client.CheckUserOTP(ctx, mfaCode)
	switch authStatus {
	case service.AuthSuccess:
		res = ssh.AuthSuccessful
		ctx.SetValue(model.ContextKeyUser, &user)
		logger.Infof("User %s Mfa Auth success", username)
	default:
		logger.Errorf("User %s Mfa Auth failed", username)
	}
	return
}

func MFAAuthMethods(ctx ssh.Context) (methods []string) {
	return []string{"keyboard-interactive"}
}
