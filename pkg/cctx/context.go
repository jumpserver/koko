package cctx

import (
	"context"

	"github.com/gliderlabs/ssh"

	"cocogo/pkg/model"
)

type contextKey struct {
	name string
}

var (
	ContextKeyUser       = &contextKey{"user"}
	ContextKeyAsset      = &contextKey{"asset"}
	ContextKeySystemUser = &contextKey{"systemUser"}
	ContextKeySSHSession = &contextKey{"sshSession"}
	ContextKeyLocalAddr  = &contextKey{"localAddr"}
	ContextKeySSHCtx     = &contextKey{"sshCtx"}
)

type Context interface {
	context.Context
	User() *model.User
	Asset() *model.Asset
	SystemUser() *model.SystemUser
	SSHSession() *ssh.Session
	SSHCtx() *ssh.Context
	SetValue(key, value interface{})
}

// Context coco内部使用的Context
type CocoContext struct {
	context.Context
}

// user 返回当前连接的用户model
func (ctx *CocoContext) User() *model.User {
	return ctx.Value(ContextKeyUser).(*model.User)
}

func (ctx *CocoContext) Asset() *model.Asset {
	return ctx.Value(ContextKeyAsset).(*model.Asset)
}

func (ctx *CocoContext) SystemUser() *model.SystemUser {
	return ctx.Value(ContextKeySystemUser).(*model.SystemUser)
}

func (ctx *CocoContext) SSHSession() *ssh.Session {
	return ctx.Value(ContextKeySSHSession).(*ssh.Session)
}

func (ctx *CocoContext) SSHCtx() *ssh.Context {
	return ctx.Value(ContextKeySSHCtx).(*ssh.Context)
}

func (ctx *CocoContext) SetValue(key, value interface{}) {
	ctx.Context = context.WithValue(ctx.Context, key, value)
}

func applySessionMetadata(ctx *CocoContext, sess ssh.Session) {
	ctx.SetValue(ContextKeySSHSession, &sess)
	ctx.SetValue(ContextKeySSHCtx, sess.Context())
	ctx.SetValue(ContextKeyLocalAddr, sess.LocalAddr())
}

func NewContext(sess ssh.Session) (*CocoContext, context.CancelFunc) {
	sshCtx, cancel := context.WithCancel(sess.Context())
	ctx := &CocoContext{sshCtx}
	applySessionMetadata(ctx, sess)
	return ctx, cancel
}
