package auth

import (
	"cocogo/pkg/model"
	"fmt"
	"strings"

	"github.com/ibuler/ssh"
	gossh "golang.org/x/crypto/ssh"

	"cocogo/pkg/cctx"
	"cocogo/pkg/common"
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
)

func checkAuth(ctx ssh.Context, password, publicKey string) (res ssh.AuthResult) {
	username := ctx.User()
	remoteAddr := strings.Split(ctx.RemoteAddr().String(), ":")[0]
	user, err := service.Authenticate(username, password, publicKey, remoteAddr, "T")
	authMethod := "publickey"
	action := "Accepted"
	res = ssh.AuthFailed
	if password != "" {
		authMethod = "password"
	}
	if err != nil {
		action = "Failed"
	} else {
		ctx.SetValue(cctx.ContextKeyUser, user)
		res = ssh.AuthPartiallySuccessful
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
	answers, err := challenger("admin", "> ", []string{"MFA"}, []bool{true})
	if err != nil {
		return ssh.AuthFailed
	}
	fmt.Println(answers)

	//ok := checkAuth(ctx, "admin", "")
	ctx.SetValue(cctx.ContextKeyUser, &model.User{Username: "admin", Name: "admin"})
	return ssh.AuthSuccessful
}
