package auth

import (
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/cctx"
	"cocogo/pkg/common"
	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
)

var mfaInstruction = i18n.T("Please enter 6 digits.")
var mfaQuestion = i18n.T("[MFA auth]: ")

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
	remoteAddr := strings.Split(ctx.RemoteAddr().String(), ":")[0]

	resp, err := service.Authenticate(username, password, publicKey, remoteAddr, "T")
	if err != nil {
		action = actionFailed
		logger.Infof("%s %s for %s from %s", action, authMethod, username, remoteAddr)
		return
	}
	if resp != nil {
		switch resp.User.OTPLevel {
		case 0:
			res = ssh.AuthSuccessful
		case 1, 2:
			action = actionPartialAccepted
			res = ssh.AuthPartiallySuccessful
		default:
		}
		ctx.SetValue(cctx.ContextKeyUser, resp.User)
		ctx.SetValue(cctx.ContextKeySeed, resp.Seed)
		ctx.SetValue(cctx.ContextKeyToken, resp.Token)
	}
	logger.Infof("%s %s for %s from %s", action, authMethod, username, remoteAddr)
	return res
}

func CheckUserPassword(ctx ssh.Context, password string) ssh.AuthResult {
	return checkAuth(ctx, password, "")
}

func CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
	b := key.Marshal()
	publicKey := common.Base64Encode(string(b))
	return checkAuth(ctx, "", publicKey)
}

func CheckMFA(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (res ssh.AuthResult) {
	username := ctx.User()
	remoteAddr := strings.Split(ctx.RemoteAddr().String(), ":")[0]
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
	seed, ok := ctx.Value(cctx.ContextKeySeed).(string)
	if !ok {
		logger.Error("Mfa Auth failed, may be user password or publickey auth failed")
		return
	}
	resp, err := service.CheckUserOTP(seed, mfaCode)
	if err != nil {
		logger.Error("Mfa Auth failed: ", err)
		return
	}
	if resp.Token != "" {
		res = ssh.AuthSuccessful
		return
	}
	return
}

func CheckUserNeedMFA(ctx ssh.Context) (methods []string) {
	return []string{"keyboard-interactive"}
}
