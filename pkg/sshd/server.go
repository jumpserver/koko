package sshd

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pires/go-proxyproto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/handler"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	sshChannelSession     = "session"
	sshChannelDirectTCPIP = "direct-tcpip"
	sshSubSystemSFTP      = "sftp"

	ChannelTCPIPForward       = "tcpip-forward"
	ChannelCancelTCPIPForward = "cancel-tcpip-forward"
	ChannelForwardedTCPIP     = "forwarded-tcpip"
)

var (
	supportedMACs = []string{"hmac-sha2-256-etm@openssh.com",
		"hmac-sha2-256", "hmac-sha1"}

	supportedKexAlgos = []string{
		"curve25519-sha256", "curve25519-sha256@libssh.org",
		"ecdh-sha2-nistp256", "ecdh-sha2-nistp384", "ecdh-sha2-nistp521",
	}
)

type Server struct {
	Srv     *ssh.Server
	Handler *handler.Server
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

func NewSSHServer(jmsService *service.JMService) *Server {
	cf := config.GlobalConfig
	addr := net.JoinHostPort(cf.BindHost, cf.SSHPort)
	termCfg, err := jmsService.GetTerminalConfig()
	if err != nil {
		logger.Fatal(err)
	}
	singer, err := ParsePrivateKeyFromString(termCfg.HostKey)
	if err != nil {
		logger.Fatalf("Parse Terminal private key failed: %s\n", err)
	}
	sshHandler := handler.NewServer(termCfg, jmsService)
	srv := &ssh.Server{
		Addr:             addr,
		PasswordHandler:  sshHandler.PasswordAuth,
		PublicKeyHandler: sshHandler.PublicKeyAuth,
		Version:          "JumpServer",
		Banner:           "Welcome to JumpServer SSH Server\n",
		HostSigners:      []ssh.Signer{singer},
		ServerConfigCallback: func(ctx ssh.Context) *gossh.ServerConfig {
			cfg := gossh.Config{MACs: supportedMACs, KeyExchanges: supportedKexAlgos}
			return &gossh.ServerConfig{Config: cfg}
		},
		Handler:                       sshHandler.SessionHandler,
		LocalPortForwardingCallback:   sshHandler.LocalPortForwardingPermission,
		ReversePortForwardingCallback: sshHandler.ReversePortForwardingPermission,
		SubsystemHandlers:             map[string]ssh.SubsystemHandler{sshSubSystemSFTP: sshHandler.SFTPHandler},
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
				sshHandler.DirectTCPIPChannelHandler(ctx, newChan, dest)
			},
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			ChannelTCPIPForward:       sshHandler.HandleSSHRequest,
			ChannelCancelTCPIPForward: sshHandler.HandleSSHRequest,
		},
	}
	return &Server{srv, sshHandler}
}

type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}
