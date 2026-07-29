package sshd

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/sshx11"
)

type testSSHContext struct {
	context.Context
	sync.Mutex
	values map[interface{}]interface{}
}

func newTestSSHContext() *testSSHContext {
	return &testSSHContext{
		Context: context.Background(),
		values:  make(map[interface{}]interface{}),
	}
}

func (c *testSSHContext) User() string                  { return "test" }
func (c *testSSHContext) SessionID() string             { return "session-id" }
func (c *testSSHContext) ClientVersion() string         { return "client" }
func (c *testSSHContext) ServerVersion() string         { return "server" }
func (c *testSSHContext) RemoteAddr() net.Addr          { return nil }
func (c *testSSHContext) LocalAddr() net.Addr           { return nil }
func (c *testSSHContext) Permissions() *ssh.Permissions { return &ssh.Permissions{} }

func (c *testSSHContext) SetValue(key, value interface{}) {
	c.values[key] = value
}

func (c *testSSHContext) Value(key interface{}) interface{} {
	if value, ok := c.values[key]; ok {
		return value
	}
	return c.Context.Value(key)
}

func TestSessionContextIsolatesChannelValues(t *testing.T) {
	parent := newTestSSHContext()
	key := struct{ name string }{"key"}
	parent.SetValue(key, "parent")
	first := newSessionContext(parent)
	second := newSessionContext(parent)

	first.SetValue(key, "first")
	if got := first.Value(key); got != "first" {
		t.Fatalf("first.Value() = %v, want first", got)
	}
	if got := second.Value(key); got != "parent" {
		t.Fatalf("second.Value() = %v, want parent", got)
	}
}

func TestFilterX11RequestsStoresX11AndForwardsOtherRequests(t *testing.T) {
	ctx := newSessionContext(newTestSSHContext())
	requests := make(chan *gossh.Request, 2)
	filtered := make(chan *gossh.Request, 2)
	x11Request := sshx11.Request{
		SingleConnection: true,
		AuthProtocol:     "MIT-MAGIC-COOKIE-1",
		AuthCookie:       "cookie",
		ScreenNumber:     2,
	}
	normalRequest := &gossh.Request{Type: "pty-req"}
	requests <- &gossh.Request{
		Type:    sshx11.RequestType,
		Payload: x11Request.Marshal(),
	}
	requests <- normalRequest
	close(requests)

	filterX11Requests(ctx, requests, filtered)

	got, ok := sshx11.RequestFromContext(ctx)
	if !ok {
		t.Fatal("X11 request was not stored in the session context")
	}
	if got != x11Request {
		t.Fatalf("stored X11 request = %#v, want %#v", got, x11Request)
	}
	if forwarded := <-filtered; forwarded != normalRequest {
		t.Fatalf("forwarded request = %p, want %p", forwarded, normalRequest)
	}
	if _, open := <-filtered; open {
		t.Fatal("filtered request channel was not closed")
	}
}

func TestFilterX11RequestsRejectsInvalidRequest(t *testing.T) {
	ctx := newSessionContext(newTestSSHContext())
	requests := make(chan *gossh.Request, 1)
	filtered := make(chan *gossh.Request, 1)
	requests <- &gossh.Request{
		Type:    sshx11.RequestType,
		Payload: []byte{1, 2, 3},
	}
	close(requests)

	filterX11Requests(ctx, requests, filtered)

	if _, ok := sshx11.RequestFromContext(ctx); ok {
		t.Fatal("invalid X11 request was stored")
	}
	if _, open := <-filtered; open {
		t.Fatal("invalid X11 request was forwarded")
	}
}
