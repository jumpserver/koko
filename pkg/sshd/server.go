package sshd

import (
	"context"
	"net"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pires/go-proxyproto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	ChannelSession            = "session"
	ChannelDirectTCPIP        = "direct-tcpip"
	ChannelForwardedTCPIP     = "forwarded-tcpip"
	ChannelTCPIPForward       = "tcpip-forward"
	ChannelCancelTCPIPForward = "cancel-tcpip-forward"
	SubSystemSFTP             = "sftp"
)

type Server struct {
	Srv *ssh.Server
}

func (s *Server) Start() {
	logger.Infof("Start SSH server at %s", s.Srv.Addr)
	ln, err := net.Listen("tcp", s.Srv.Addr)
	if err != nil {
		logger.Fatal(err)
	}
	proxyListener := &proxyproto.Listener{Listener: ln}
	logger.Fatal(s.Srv.Serve(proxyListener))
}

func (s *Server) Stop() {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	logger.Fatal(s.Srv.Shutdown(ctx))
}

type SSHHandler interface {
	GetSSHAddr() string
	GetSSHSigner() ssh.Signer
	KeyboardInteractiveAuth(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) AuthStatus
	PasswordAuth(ctx ssh.Context, password string) AuthStatus
	PublicKeyAuth(ctx ssh.Context, key ssh.PublicKey) AuthStatus
	NextAuthMethodsHandler(ctx ssh.Context) []string
	SessionHandler(ssh.Session)
	SFTPHandler(ssh.Session)
	RequestHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (ok bool, payload []byte)
	LocalPortForwardingCallback(ctx ssh.Context, destinationHost string, destinationPort uint32) bool
	ReversePortForwardingCallback(ctx ssh.Context, destinationHost string, destinationPort uint32) bool
	DirectTCPIPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context)
}

type AuthStatus ssh.AuthResult

const (
	AuthFailed              = AuthStatus(ssh.AuthFailed)
	AuthSuccessful          = AuthStatus(ssh.AuthSuccessful)
	AuthPartiallySuccessful = AuthStatus(ssh.AuthPartiallySuccessful)
)

func NewSSHServer(handler SSHHandler) *Server {
	srv := &ssh.Server{
		LocalPortForwardingCallback:   handler.LocalPortForwardingCallback,
		ReversePortForwardingCallback: handler.ReversePortForwardingCallback,
		Addr:                          handler.GetSSHAddr(),
		KeyboardInteractiveHandler: func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) ssh.AuthResult {
			return ssh.AuthResult(handler.KeyboardInteractiveAuth(ctx, challenger))
		},
		PasswordHandler: func(ctx ssh.Context, password string) ssh.AuthResult {
			return ssh.AuthResult(handler.PasswordAuth(ctx, password))
		},
		PublicKeyHandler: func(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
			return ssh.AuthResult(handler.PublicKeyAuth(ctx, key))
		},
		NextAuthMethodsHandler: func(ctx ssh.Context) []string {
			return handler.NextAuthMethodsHandler(ctx)
		},
		HostSigners: []ssh.Signer{handler.GetSSHSigner()},
		Handler:     handler.SessionHandler,
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			SubSystemSFTP: handler.SFTPHandler,
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			ChannelSession:     ssh.DefaultSessionHandler,
			ChannelDirectTCPIP: handler.DirectTCPIPChannelHandler,
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			ChannelTCPIPForward:       handler.RequestHandler,
			ChannelCancelTCPIPForward: handler.RequestHandler,
		},
	}
	return &Server{srv}
}
