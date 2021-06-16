package koko

import (
	"fmt"
	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
	"io"
	"net"
	"strconv"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/handler"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/sshd"
	"github.com/jumpserver/koko/pkg/utils"
)

const (
	nextAuthMethod = "keyboard-interactive"
)

func (s *server) GetSSHAddr() string {
	cf := config.GlobalConfig
	return net.JoinHostPort(cf.BindHost, cf.SSHPort)
}
func (s *server) GetSSHSigner() ssh.Signer {
	conf := s.GetTerminalConfig()
	singer, err := sshd.ParsePrivateKeyFromString(conf.HostKey)
	if err != nil {
		logger.Fatal(err)
	}
	return singer
}

func (s *server) KeyboardInteractiveAuth(ctx ssh.Context,
	challenger gossh.KeyboardInteractiveChallenge) sshd.AuthStatus {
	return auth.SSHKeyboardInteractiveAuth(ctx, challenger)
}

const ctxID = "ctxID"

func (s *server) PasswordAuth(ctx ssh.Context, password string) sshd.AuthStatus {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PasswordAuth {
		logger.Info("Core API disable password auth auth")
		return sshd.AuthFailed
	}
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	return sshAuthHandler(ctx, password, "")
}

func (s *server) PublicKeyAuth(ctx ssh.Context, key ssh.PublicKey) sshd.AuthStatus {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PublicKeyAuth {
		logger.Info("Core API disable publickey auth")
		return sshd.AuthFailed
	}
	publicKey := common.Base64Encode(string(key.Marshal()))
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	return sshAuthHandler(ctx, "", publicKey)
}

func (s *server) NextAuthMethodsHandler(ctx ssh.Context) []string {
	return []string{nextAuthMethod}
}

func (s *server) SFTPHandler(sess ssh.Session) {
	currentUser, ok := sess.Context().Value(auth.ContextKeyUser).(*model.User)
	if !ok || currentUser.ID == "" {
		logger.Errorf("SFTP User not found, exit.")
		return
	}
	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	userSftp := handler.NewSFTPHandler(s.jmsService, currentUser, host)
	handlers := sftp.Handlers{
		FileGet:  userSftp,
		FilePut:  userSftp,
		FileCmd:  userSftp,
		FileList: userSftp,
	}
	reqID := common.UUID()
	logger.Infof("SFTP request %s: Handler start", reqID)
	req := sftp.NewRequestServer(sess, handlers)
	if err := req.Serve(); err == io.EOF {
		logger.Debugf("SFTP request %s: Exited session.", reqID)
	} else if err != nil {
		logger.Errorf("SFTP request %s: Server completed with error %s", reqID, err)
	}
	_ = req.Close()
	userSftp.Close()
	logger.Infof("SFTP request %s: Handler exit.", reqID)
}

func (s *server) LocalPortForwardingPermission(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
	return config.GlobalConfig.EnableLocalPortForward
}
func (s *server) DirectTCPIPChannelHandler(ctx ssh.Context, newChan gossh.NewChannel, destAddr string) {
	if !config.GetConf().EnableVscodeSupport {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}
	reqId, ok := ctx.Value(ctxID).(string)
	if !ok {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}
	vsReq := s.getVSCodeReq(reqId)
	if vsReq == nil {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}
	dConn, err := vsReq.client.Dial("tcp", destAddr)
	if err != nil {
		_ = newChan.Reject(gossh.ConnectionFailed, err.Error())
		return
	}
	defer dConn.Close()
	ch, reqs, err := newChan.Accept()
	if err != nil {
		_ = dConn.Close()
		_ = newChan.Reject(gossh.ConnectionFailed, err.Error())
		return
	}
	logger.Infof("User %s start port forwarding from (%s) to (%s)", vsReq.user,
		vsReq.client, destAddr)
	defer ch.Close()
	go gossh.DiscardRequests(reqs)
	go func() {
		defer ch.Close()
		defer dConn.Close()
		_, _ = io.Copy(ch, dConn)
	}()
	_, _ = io.Copy(dConn, ch)
	logger.Infof("User %s end port forwarding from (%s) to (%s)", vsReq.user,
		vsReq.client, destAddr)
}

func (s *server) SessionHandler(sess ssh.Session) {
	user, ok := sess.Context().Value(auth.ContextKeyUser).(*model.User)
	if !ok || user.ID == "" {
		logger.Errorf("SSH User %s not found, exit.", sess.User())
		utils.IgnoreErrWriteString(sess, "Not auth user.\n")
		return
	}
	termConf := s.GetTerminalConfig()
	directReq := sess.Context().Value(auth.ContextKeyDirectLoginFormat)
	if pty, winChan, isPty := sess.Pty(); isPty {
		if directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq); ok3 {
			directSrv, err := handler.NewDirectHandler(sess, s.jmsService,
				handler.DirectAssetIP(directRequest.AssetIP),
				handler.DirectUser(user),
				handler.DirectTerminalConf(&termConf),
				handler.DirectSystemUsername(directRequest.SysUsername),
			)
			if err != nil {
				logger.Errorf("User %s direct request err: %s", user.Name, err)
				return
			}
			directSrv.Dispatch()
			return
		}

		interactiveSrv := handler.NewInteractiveHandler(sess, user, s.jmsService, termConf)
		logger.Infof("User %s request pty %s", sess.User(), pty.Term)
		go interactiveSrv.WatchWinSizeChange(winChan)
		interactiveSrv.Dispatch()
		return
	}
	if !config.GetConf().EnableVscodeSupport {
		utils.IgnoreErrWriteString(sess, "No PTY requested.\n")
		return
	}
	if directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq); ok3 {
		selectedAssets, err := s.jmsService.GetUserPermAssetsByIP(user.ID, directRequest.AssetIP)
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		if len(selectedAssets) != 1 {
			msg := fmt.Sprintf(i18n.T("Must be unique asset for %s"), directRequest.AssetIP)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		selectSysUsers, err := s.jmsService.GetSystemUsersByUserIdAndAssetId(user.ID, selectedAssets[0].ID)
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}

		matched := make([]model.SystemUser, 0, len(selectSysUsers))
		for i := range selectSysUsers {
			if selectSysUsers[i].Username == directRequest.SysUsername {
				matched = append(matched, selectSysUsers[i])
			}
		}
		if len(matched) != 1 {
			msg := fmt.Sprintf(i18n.T("Must be unique system user for %s"), directRequest.SysUsername)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		s.proxyVscode(sess, user, selectedAssets[0], matched[0])
	}

}

func (s *server) proxyVscode(sess ssh.Session, user *model.User, asset model.Asset,
	systemUser model.SystemUser) {
	ctxId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		return
	}
	systemUserAuthInfo, err := s.jmsService.GetSystemUserAuthById(systemUser.ID, asset.ID,
		user.ID, user.Username)
	if err != nil {
		logger.Errorf("Get system user auth failed: %s", err)
		return
	}
	var domainGateways *model.Domain
	if asset.Domain != "" {
		domainInfo, err := s.jmsService.GetDomainGateways(asset.Domain)
		if err != nil {
			logger.Errorf("Get system user auth failed: %s", err)
			return
		}
		domainGateways = &domainInfo
	}
	timeout := config.GlobalConfig.SSHTimeout
	sshAuthOpts := make([]srvconn.SSHClientOption, 0, 7)
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientUsername(systemUserAuthInfo.Username))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientHost(asset.IP))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPort(asset.ProtocolPort(systemUserAuthInfo.Protocol)))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPassword(systemUserAuthInfo.Password))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientTimeout(timeout))
	if systemUserAuthInfo.PrivateKey != "" {
		// 先使用 password 解析 PrivateKey
		if signer, err1 := gossh.ParsePrivateKeyWithPassphrase([]byte(systemUserAuthInfo.PrivateKey),
			[]byte(systemUserAuthInfo.Password)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
		} else {
			// 如果之前使用password解析失败，则去掉 password, 尝试直接解析 PrivateKey 防止错误的passphrase
			if signer, err1 = gossh.ParsePrivateKey([]byte(systemUserAuthInfo.PrivateKey)); err1 == nil {
				sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
			}
		}
	}

	if domainGateways != nil {
		proxyArgs := make([]srvconn.SSHClientOptions, 0, len(domainGateways.Gateways))
		for i := range domainGateways.Gateways {
			gateway := domainGateways.Gateways[i]
			proxyArg := srvconn.SSHClientOptions{
				Host:       gateway.IP,
				Port:       strconv.Itoa(gateway.Port),
				Username:   gateway.Username,
				Password:   gateway.Password,
				PrivateKey: gateway.PrivateKey,
				Timeout:    timeout,
			}
			proxyArgs = append(proxyArgs, proxyArg)
		}
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientProxyClient(proxyArgs...))
	}
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get SSH Client failed: %s", err)
		return
	}
	defer sshClient.Close()
	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return
	}
	defer goSess.Close()
	stdOut, err := goSess.StdoutPipe()
	if err != nil {
		logger.Errorf("Get SSH session StdoutPipe failed: %s", err)
		return
	}
	stdin, err := goSess.StdinPipe()
	if err != nil {
		logger.Errorf("Get SSH session StdinPipe failed: %s", err)
		return
	}
	err = goSess.Shell()
	if err != nil {
		logger.Errorf("Get SSH session shell failed: %s", err)
		return
	}
	logger.Infof("User %s start vscode request to %s", user, sshClient)
	vsReq := &vscodeReq{
		reqId:  ctxId,
		user:   user,
		client: sshClient,
	}
	s.addVSCodeReq(vsReq)
	defer s.deleteVSCodeReq(vsReq)
	go func() {
		defer goSess.Close()
		defer sess.Close()
		_, _ = io.Copy(stdin, sess)
	}()
	go func() {
		defer goSess.Close()
		defer sess.Close()
		_, _ = io.Copy(sess, stdOut)
	}()
	<-sess.Context().Done()
	sshClient.ReleaseSession(goSess)

	logger.Infof("User %s end vscode request %s", user, sshClient)
}
