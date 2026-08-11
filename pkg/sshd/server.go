package sshd

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pires/go-proxyproto"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/handler"
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

type Server struct {
	Srv     *ssh.Server
	Handler *handler.Server
}

func (s *Server) Start() {
	logger.Infof("Start SSH server at %s", s.Srv.Addr)
	ln, err := net.Listen("tcp", s.Srv.Addr)
	if err != nil {
		logger.Errorf("Start SSH server failed: %s", err)
		return
	}
	proxyListener := &proxyproto.Listener{Listener: ln}
	if err = s.Srv.Serve(proxyListener); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		logger.Errorf("SSH server stopped unexpectedly: %s", err)
	}
}

func (s *Server) Stop() {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	if err := s.Srv.Shutdown(ctx); err != nil {
		logger.Errorf("Stop SSH server failed: %s", err)
	}
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
		HostSigners:      []ssh.Signer{singer},
		MaxSessions:      int32(cf.SshMaxSessions),
		ServerConfigCallback: func(ctx ssh.Context) *gossh.ServerConfig {
			cfg := DefaultSSHSrvConfig()
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

func DefaultSSHSrvConfig() gossh.Config {
	cfg := gossh.Config{}
	cfg.SetDefaults()
	macs := cfg.MACs
	keyExchanges := cfg.KeyExchanges
	insecureKeyExchangeMaps := map[string]struct{}{
		gossh.InsecureKeyExchangeDH1SHA1:   {},
		gossh.InsecureKeyExchangeDH14SHA1:  {},
		gossh.InsecureKeyExchangeDHGEXSHA1: {},
	}
	insecureMacs := map[string]struct{}{
		gossh.InsecureHMACSHA196: {},
	}
	filterMacs := make([]string, 0, len(macs))
	for _, mac := range macs {
		if _, ok := insecureMacs[mac]; ok {
			continue
		}
		filterMacs = append(filterMacs, mac)
	}
	filterKeyExchanges := make([]string, 0, len(keyExchanges))
	for _, key := range keyExchanges {
		if _, ok := insecureKeyExchangeMaps[key]; ok {
			continue
		}
		filterKeyExchanges = append(filterKeyExchanges, key)
	}
	cfg.MACs = filterMacs
	cfg.KeyExchanges = filterKeyExchanges
	return cfg
}
