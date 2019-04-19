package auth

import (
	"github.com/gliderlabs/ssh"

	"cocogo/pkg/common"
	"cocogo/pkg/service"
)

func CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) bool {
	username := ctx.User()
	b := key.Marshal()
	publicKeyBase64 := common.Base64Encode(string(b))
	remoteAddr := ctx.RemoteAddr().String()
	authUser, err := service.CheckAuth(username, "", publicKeyBase64, remoteAddr, "T")
	if err != nil {
		return false
	}
	ctx.SetValue("LoginUser", authUser)
	return true

}
