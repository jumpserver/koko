package context

import (
	"context"
	"github.com/gliderlabs/ssh"

	"cocogo/pkg/model"
)

type UserContext struct {
	context.Context

	SessionCtx ssh.Context
	User       model.User
	Asset      sdk.Asset
	SystemUser model.SystemUser
}
