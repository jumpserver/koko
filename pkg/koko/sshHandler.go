package koko

import (
	"github.com/gliderlabs/ssh"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/handler"
	gossh "golang.org/x/crypto/ssh"
)

func (a *Application) SftpHandler(sess ssh.Session) {
	handler.SftpHandler(sess)
}

func (a *Application) SessionHandler(sess ssh.Session) {
	handler.SessionHandler(sess)
}

func (a *Application) CheckMFA(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) (res ssh.AuthResult) {
	return auth.CheckMFA(ctx, challenger)
}

func (a *Application) CheckUserPassword(ctx ssh.Context, password string) ssh.AuthResult {
	return auth.CheckUserPassword(ctx, password)
}

func (a *Application) CheckUserPublicKey(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
	return auth.CheckUserPublicKey(ctx, key)
}

func (a *Application) MFAAuthMethods(ctx ssh.Context) (methods []string) {
	return []string{"keyboard-interactive"}
}
