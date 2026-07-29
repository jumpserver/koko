package sshd

import (
	"sync"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshx11"
)

// sessionContext isolates values written while handling a session channel.
// gliderlabs/ssh normally shares one context between all session channels on a
// transport, while an X11 request belongs to exactly one session channel.
type sessionContext struct {
	ssh.Context

	mu     sync.RWMutex
	values map[interface{}]interface{}
}

func newSessionContext(parent ssh.Context) *sessionContext {
	return &sessionContext{
		Context: parent,
		values:  make(map[interface{}]interface{}),
	}
}

func (c *sessionContext) Value(key interface{}) interface{} {
	c.mu.RLock()
	value, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		return value
	}
	return c.Context.Value(key)
}

func (c *sessionContext) SetValue(key, value interface{}) {
	c.mu.Lock()
	c.values[key] = value
	c.mu.Unlock()
}

type x11SessionChannel struct {
	gossh.NewChannel
	ctx ssh.Context
}

func (c *x11SessionChannel) Accept() (gossh.Channel, <-chan *gossh.Request, error) {
	channel, requests, err := c.NewChannel.Accept()
	if err != nil {
		return nil, nil, err
	}

	filtered := make(chan *gossh.Request)
	go filterX11Requests(c.ctx, requests, filtered)
	return channel, filtered, nil
}

func filterX11Requests(ctx ssh.Context, requests <-chan *gossh.Request,
	filtered chan<- *gossh.Request) {
	defer close(filtered)
	for req := range requests {
		if req.Type != sshx11.RequestType {
			filtered <- req
			continue
		}

		if _, exists := sshx11.RequestFromContext(ctx); exists {
			_ = req.Reply(false, nil)
			logger.Infof("SSH conn[%s] rejected duplicate X11 forwarding request", ctx.SessionID())
			continue
		}
		x11Request, err := sshx11.ParseRequest(req.Payload)
		if err != nil {
			_ = req.Reply(false, nil)
			logger.Infof("SSH conn[%s] rejected invalid X11 forwarding request", ctx.SessionID())
			continue
		}

		sshx11.SetRequest(ctx, x11Request)
		_ = req.Reply(true, nil)
		logger.Infof("SSH conn[%s] accepted pending X11 forwarding request", ctx.SessionID())
	}
}

func x11SessionHandler(srv *ssh.Server, conn *gossh.ServerConn,
	newChannel gossh.NewChannel, parentCtx ssh.Context) {
	ctx := newSessionContext(parentCtx)
	wrappedChannel := &x11SessionChannel{
		NewChannel: newChannel,
		ctx:        ctx,
	}
	ssh.DefaultSessionHandler(srv, conn, wrappedChannel, ctx)
}
