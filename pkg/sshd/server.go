package sshd

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pires/go-proxyproto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	sshChannelSession     = "session"
	sshChannelDirectTCPIP = "direct-tcpip"
	sshSubSystemSFTP      = "sftp"
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
	LocalPortForwardingPermission(ctx ssh.Context, destinationHost string, destinationPort uint32) bool
	DirectTCPIPChannelHandler(ctx ssh.Context, newChan gossh.NewChannel, destAddr string)
}

type AuthStatus ssh.AuthResult

const (
	AuthFailed              = AuthStatus(ssh.AuthFailed)
	AuthSuccessful          = AuthStatus(ssh.AuthSuccessful)
	AuthPartiallySuccessful = AuthStatus(ssh.AuthPartiallySuccessful)
)

func NewSSHServer(handler SSHHandler) *Server {
	srv := &ssh.Server{
		LocalPortForwardingCallback: func(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
			return handler.LocalPortForwardingPermission(ctx, destinationHost, destinationPort)
		},
		Addr: handler.GetSSHAddr(),
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
			sshSubSystemSFTP: handler.SFTPHandler,
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			sshChannelSession: ssh.DefaultSessionHandler,
			sshChannelDirectTCPIP: func(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
				localD := localForwardChannelData{}
				if err := gossh.Unmarshal(newChan.ExtraData(), &localD); err != nil {
					_ = newChan.Reject(gossh.ConnectionFailed, "error parsing forward data: "+err.Error())
					return
				}

				if srv.LocalPortForwardingCallback == nil || !srv.LocalPortForwardingCallback(ctx, localD.DestAddr, localD.DestPort) {
					_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
					return
				}
				dest := net.JoinHostPort(localD.DestAddr, strconv.FormatInt(int64(localD.DestPort), 10))
				handler.DirectTCPIPChannelHandler(ctx, newChan, dest)
			},
		},
	}
	return &Server{srv}
}

type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}
