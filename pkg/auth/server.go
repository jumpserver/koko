package auth

import (
	"cocogo/pkg/i18n"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/common"
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
)

var mfaInstruction = i18n.T("Please enter 6 digits.")
var mfaQuestion = i18n.T("[MFA auth]: ")

var contentKeyMFASeed = "MFASeed"

func checkAuth(ctx ssh.Context, password, publicKey string) (res ssh.AuthResult) {
	username := ctx.User()
	remoteAddr := strings.Split(ctx.RemoteAddr().String(), ":")[0]
	resp, err := service.Authenticate(username, password, publicKey, remoteAddr, "T")
	authMethod := "publickey"
	action := "Accepted"
	res = ssh.AuthFailed
	if password != "" {
		authMethod = "password"
	}
	if err != nil {
		action = "Failed"
	} else if resp.Seed != "" && resp.Token == "" {
		ctx.SetValue(contentKeyMFASeed, resp.Seed)
		res = ssh.AuthPartiallySuccessful
	} else {
		res = ssh.AuthSuccessful
	}
	logger.Infof("%s %s for %s from %s", action, authMethod, username, remoteAddr)
	return res
}

func CheckUserPassword(ctx ssh.Context, password string) ssh.AuthResult {
	res := checkAuth(ctx, password, "")
	return res
}

func CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
	b := key.Marshal()
	publicKey := common.Base64Encode(string(b))
	return checkAuth(ctx, "", publicKey)
}

func CheckMFA(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) ssh.AuthResult {
	username := ctx.User()
	answers, err := challenger(username, mfaInstruction, []string{mfaQuestion}, []bool{true})
	if err != nil {
		return ssh.AuthFailed
	}
	if len(answers) != 0 {
		return ssh.AuthFailed
	}
	mfaCode := answers[0]
	seed, ok := ctx.Value(contentKeyMFASeed).(string)
	if !ok {
		logger.Error("Mfa Auth failed, may be user password or publickey auth failed")
		return ssh.AuthFailed
	}
	resp, err := service.CheckUserOTP(seed, mfaCode)
	if err != nil {
		logger.Error("Mfa Auth failed: ", err)
		return ssh.AuthFailed
	}
	if resp.Token != "" {
		return ssh.AuthSuccessful
	}
	return ssh.AuthFailed
}

func CheckUserNeedMFA(ctx ssh.Context) (methods []string) {
	username := ctx.User()
	user, err := service.GetUserByUsername(username)
	if err != nil {
		return
	}
	if user.OTPLevel > 0 {
		return []string{"keyboard-interactive"}
	}
	return
}
