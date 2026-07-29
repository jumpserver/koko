package sshx11

import (
	"context"
	"errors"

	gliderssh "github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/model"
	gossh "golang.org/x/crypto/ssh"
)

const (
	// PlatformMetaKey is the platform extension field used to explicitly allow
	// X11 forwarding. Missing fields and values other than the boolean true are
	// intentionally treated as disabled.
	PlatformMetaKey = "x11_forwarding_enabled"

	RequestType = "x11-req"
	ChannelType = "x11"
)

var ErrInvalidRequest = errors.New("invalid SSH X11 forwarding request")

// Request is the x11-req payload defined by RFC 4254 section 6.3.1.
//
// OpenSSH does not put the -X/-Y trust choice in a separate SSH field. The
// originating client maintains that state together with its local X11
// authentication data, so the authentication tuple must be forwarded without
// modification and the return channel must go back to the same client.
type Request struct {
	SingleConnection bool
	AuthProtocol     string
	AuthCookie       string
	ScreenNumber     uint32
}

type requestPayload struct {
	SingleConnection bool
	AuthProtocol     string
	AuthCookie       string
	ScreenNumber     uint32
}

func ParseRequest(payload []byte) (Request, error) {
	var parsed requestPayload
	if err := gossh.Unmarshal(payload, &parsed); err != nil {
		return Request{}, ErrInvalidRequest
	}
	return Request(parsed), nil
}

func (r Request) Marshal() []byte {
	return gossh.Marshal(requestPayload(r))
}

func PlatformEnabled(platform model.Platform) bool {
	value, ok := platform.MetaData[PlatformMetaKey]
	if !ok {
		return false
	}
	enabled, ok := value.(bool)
	return ok && enabled
}

type requestContextKey struct{}

type contextValueSetter interface {
	SetValue(key, value interface{})
}

func SetRequest(ctx contextValueSetter, req Request) {
	ctx.SetValue(requestContextKey{}, req)
}

func RequestFromContext(ctx context.Context) (Request, bool) {
	if ctx == nil {
		return Request{}, false
	}
	req, ok := ctx.Value(requestContextKey{}).(Request)
	return req, ok
}

func ClientConnectionFromContext(ctx context.Context) (gossh.Conn, bool) {
	if ctx == nil {
		return nil, false
	}
	conn, ok := ctx.Value(gliderssh.ContextKeyConn).(gossh.Conn)
	return conn, ok
}
