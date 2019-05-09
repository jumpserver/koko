package auth

import (
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/cctx"
	"cocogo/pkg/common"
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
)

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
		res = ssh.AuthFailed
	}
	if resp != nil {
		switch resp.User.IsMFA {
		case 0:
			res = ssh.AuthSuccessful
		case 1:
			res = ssh.AuthPartiallySuccessful
		case 2:
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
	res := checkAuth(ctx, password, "")
	return res
}

func CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
	b := key.Marshal()
	publicKey := common.Base64Encode(string(b))
	return checkAuth(ctx, "", publicKey)
}

func CheckMFA(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) ssh.AuthResult {
	answers, err := challenger(ctx.User(), "Please enter 6 digits.", []string{"[MFA auth]: "}, []bool{true})
	if err != nil {
		return ssh.AuthFailed
	}
	seed := ctx.Value(cctx.ContextKeySeed).(string)
	code := answers[0]
	res, err := service.AuthenticateMFA(seed, code, "T")
	if err != nil || res != nil {
		return ssh.AuthFailed
	}

	//ok := checkAuth(ctx, "admin", "")
	return ssh.AuthSuccessful
}
