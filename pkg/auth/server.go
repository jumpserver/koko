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

func checkAuth(ctx ssh.Context, password, publicKey string) (ok bool) {
	username := ctx.User()
	remoteAddr := strings.Split(ctx.RemoteAddr().String(), ":")[0]
	user := service.Authenticate(username, password, publicKey, remoteAddr, "T")
	authMethod := "publickey"
	action := "Accepted"
	if password != "" {
		authMethod = "password"
	}
	if user == nil {
		action = "Failed"
		ok = false
	} else {
		ctx.SetValue(cctx.ContextKeyUser, user)
	}
	logger.Infof("%s %s for %s from %s", action, authMethod, username, remoteAddr)
	return false
}

func CheckUserPassword(ctx ssh.Context, password string) bool {
	ok := checkAuth(ctx, password, "")
	return ok
}

func CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) bool {
	b := key.Marshal()
	publicKey := common.Base64Encode(string(b))
	return checkAuth(ctx, "", publicKey)
}

func CheckMFA(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool {
	return false
}
