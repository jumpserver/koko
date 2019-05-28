package httpd

import (
	"cocogo/pkg/model"
	"context"
	"github.com/gliderlabs/ssh"
	"github.com/googollee/go-socket.io"
)

type contextKey struct {
	name string
}

var (
	ContextKeyUser       = &contextKey{"user"}
	ContextKeyAsset      = &contextKey{"asset"}
	ContextKeySystemUser = &contextKey{"systemUser"}
	ContextKeyLocalAddr  = &contextKey{"localAddr"}
	ContextKeyConnection = &contextKey{"connection"}
	ContextKeyClient     = &contextKey{"client"}
)

type WSContext interface {
	context.Context
	User() *model.User
	Asset() *model.Asset
	SystemUser() *model.SystemUser
	SSHSession() *ssh.Session
	SSHCtx() *ssh.Context
	SetValue(key, value interface{})
}

type WebSocketContext struct {
	context.Context
}

// user 返回当前连接的用户model
func (ctx *WebSocketContext) User() *model.User {
	return ctx.Value(ContextKeyUser).(*model.User)
}

func (ctx *WebSocketContext) Asset() *model.Asset {
	return ctx.Value(ContextKeyAsset).(*model.Asset)
}

func (ctx *WebSocketContext) SystemUser() *model.SystemUser {
	return ctx.Value(ContextKeySystemUser).(*model.SystemUser)
}

func (ctx *WebSocketContext) Connection() *WebConn {
	return ctx.Value(ContextKeyConnection).(*WebConn)
}

func (ctx *WebSocketContext) Client() *Client {
	return ctx.Value(ContextKeyClient).(*Client)
}

func (ctx *WebSocketContext) SetValue(key, value interface{}) {
	ctx.Context = context.WithValue(ctx.Context, key, value)
}

func applySessionMetadata(ctx *WebSocketContext, sess ssh.Session) {
	ctx.SetValue(ContextKeyLocalAddr, sess.LocalAddr())
}

func NewContext(s socketio.Conn) (*WebSocketContext, context.CancelFunc) {
	parent, cancel := context.WithCancel(context.Background())
	ctx := &WebSocketContext{parent}
	return ctx, cancel
}
