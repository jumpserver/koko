package koko

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/handler"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
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
		logger.Fatalf("Parse Terminal private key failed: %s\n", err)
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
	addr, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	directReq := sess.Context().Value(auth.ContextKeyDirectLoginFormat)
	var sftpHandler *handler.SftpHandler
	termConf := s.GetTerminalConfig()
	if directRequest, ok2 := directReq.(*auth.DirectLoginAssetReq); ok2 {
		opts := buildDirectRequestOptions(currentUser, directRequest)
		opts = append(opts, handler.DirectConnectSftpMode(true))

		opts = append(opts, handler.DirectTerminalConf(&termConf))
		directSrv, err := handler.NewDirectHandler(sess, s.jmsService, opts...)
		if err != nil {
			logger.Errorf("User %s direct sftp request err: %s", currentUser.Name, err)
			return
		}
		sftpHandler = directSrv.NewSFTPHandler()
	}
	if sftpHandler == nil {
		sftpHandler = handler.NewSFTPHandler(s.jmsService, currentUser, addr)
	}
	handlers := sftp.Handlers{
		FileGet:  sftpHandler,
		FilePut:  sftpHandler,
		FileCmd:  sftpHandler,
		FileList: sftpHandler,
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
	sftpHandler.Close()
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
			opts := buildDirectRequestOptions(user, directRequest)
			opts = append(opts, handler.DirectTerminalConf(&termConf))
			directSrv, err := handler.NewDirectHandler(sess, s.jmsService, opts...)
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
		utils.IgnoreErrWriteWindowTitle(sess, termConf.HeaderTitle)
		return
	}

	if directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq); ok3 {
		if directRequest.IsToken() {
			// connection token 的方式使用 vscode 连接
			tokenInfo := directRequest.ConnectToken
			matchedProtocol := tokenInfo.Protocol == model.ProtocolSSH
			assetSupportedSSH := tokenInfo.Asset.IsSupportProtocol(model.ProtocolSSH)
			if !matchedProtocol || !assetSupportedSSH {
				msg := "not ssh asset connection token"
				utils.IgnoreErrWriteString(sess, msg)
				logger.Errorf("vscode failed: %s", msg)
				return
			}
			s.proxyTokenInfo(sess, tokenInfo)
			return
		}
		selectedAssets, err := s.getMatchedAssetsByDirectReq(user, directRequest)
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		if len(selectedAssets) != 1 {
			msg := fmt.Sprintf(i18n.T("Must be unique asset for %s"), directRequest.AssetInfo)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}

		selectAccounts, err := s.getMatchedAccounts(user, directRequest, selectedAssets[0])
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		if len(selectAccounts) != 1 {
			msg := fmt.Sprintf(i18n.T("Must be unique account for %s"), directRequest.AccountInfo)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		selectAccount := selectAccounts[0]
		if strings.HasPrefix(selectAccount.Username, "@INPUT") {
			msg := fmt.Sprintf(i18n.T("Must be auto login account for %s"), directRequest.AccountInfo)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		s.proxyDirectRequest(sess, user, selectedAssets[0], selectAccount, directRequest.Protocol)
	}

}

func (s *server) proxyDirectRequest(sess ssh.Session, user *model.User, asset model.Asset,
	permAccount model.PermAccount, protocol string) {
	// todo: 禁用 非 ssh 的协议
	connectInfo, err := s.jmsService.CreateSuperConnectToken(&service.SuperConnectTokenReq{
		UserId:        user.ID,
		AssetId:       asset.ID,
		Account:       permAccount.Name,
		Protocol:      protocol,
		ConnectMethod: model.ProtocolSSH,
	})
	if err != nil {
		logger.Errorf("Create super connect token err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}

	connectToken, err := s.jmsService.GetConnectTokenInfo(connectInfo.ID)
	if err != nil {
		logger.Errorf("Get super connect token err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	s.proxyTokenInfo(sess, &connectToken)
}

func (s *server) proxyTokenInfo(sess ssh.Session, tokeInfo *model.ConnectToken) {
	ctxId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		utils.IgnoreErrWriteString(sess, "not found ctx id")
		return
	}
	asset := tokeInfo.Asset
	account := tokeInfo.Account
	var gateways []model.Gateway
	// todo：domain 再优化
	if tokeInfo.Gateway != nil {
		gateways = []model.Gateway{*tokeInfo.Gateway}
	}

	sshAuthOpts := buildSSHClientOptions(&asset, &account, gateways)
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get SSH Client failed: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	defer sshClient.Close()
	if len(sess.Command()) != 0 {
		s.proxyAssetCommand(sess, sshClient)
		return
	}

	if !config.GetConf().EnableVscodeSupport {
		utils.IgnoreErrWriteString(sess, "No support vscode like requested.\n")
		return
	}

	vsReq := &vscodeReq{
		reqId:      ctxId,
		user:       &tokeInfo.User,
		client:     sshClient,
		expireInfo: tokeInfo.ExpireAt,
	}
	if err = s.proxyVscodeShell(sess, vsReq, sshClient); err != nil {
		utils.IgnoreErrWriteString(sess, err.Error())
	}
}

func (s *server) proxyAssetCommand(sess ssh.Session, sshClient *srvconn.SSHClient) {
	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return
	}
	defer goSess.Close()
	defer sshClient.ReleaseSession(goSess)
	goSess.Stdin = sess
	goSess.Stdout = sess
	goSess.Stderr = sess
	// todo: 禁用 scp 命令，增加会话记录
	err = goSess.Run(sess.RawCommand())
	if err != nil {
		logger.Errorf("Run command failed: %s", err)
	}
}

func (s *server) proxyVscodeShell(sess ssh.Session, vsReq *vscodeReq, sshClient *srvconn.SSHClient) error {
	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return err
	}
	defer goSess.Close()
	defer sshClient.ReleaseSession(goSess)
	stdOut, err := goSess.StdoutPipe()
	if err != nil {
		logger.Errorf("Get SSH session StdoutPipe failed: %s", err)
		return err
	}
	stdin, err := goSess.StdinPipe()
	if err != nil {
		logger.Errorf("Get SSH session StdinPipe failed: %s", err)
		return err
	}
	err = goSess.Shell()
	if err != nil {
		logger.Errorf("Get SSH session shell failed: %s", err)
		return err
	}
	logger.Infof("User %s start vscode request to %s", vsReq.user, sshClient)

	s.addVSCodeReq(vsReq)
	defer s.deleteVSCodeReq(vsReq)
	go func() {
		_, _ = io.Copy(stdin, sess)
		logger.Infof("User %s vscode request %s stdin end", vsReq.user, sshClient)
	}()
	go func() {
		_, _ = io.Copy(sess, stdOut)
		logger.Infof("User %s vscode request %s stdOut end", vsReq.user, sshClient)
	}()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-sess.Context().Done():
			logger.Infof("SSH conn[%s] User %s end vscode request %s as session done",
				vsReq.reqId, vsReq.user, sshClient)
			return nil
		case now := <-ticker.C:
			if vsReq.expireInfo.IsExpired(now) {
				logger.Infof("SSH conn[%s] User %s end vscode request %s as permission has expired",
					vsReq.reqId, vsReq.user, sshClient)
				return nil
			}
			logger.Debugf("SSH conn[%s] user %s vscode request still alive", vsReq.reqId, vsReq.user)
		}
	}
}

func buildSSHClientOptions(asset *model.Asset, account *model.Account,
	gateways []model.Gateway) []srvconn.SSHClientOption {
	timeout := config.GlobalConfig.SSHTimeout
	sshAuthOpts := make([]srvconn.SSHClientOption, 0, 7)
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientUsername(account.Username))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientHost(asset.Address))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPort(asset.ProtocolPort(model.ProtocolSSH)))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientTimeout(timeout))
	if account.IsSSHKey() {
		if signer, err1 := gossh.ParsePrivateKey([]byte(account.Secret)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
		} else {
			logger.Errorf("Parse account %s private key failed: %s", account.Username, err1)
		}
	} else {
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPassword(account.Secret))
	}

	if len(gateways) > 0 {
		proxyArgs := make([]srvconn.SSHClientOptions, 0, len(gateways))
		for i := range gateways {
			gateway := gateways[i]
			loginAccount := gateway.Account
			port := gateway.Protocols.GetProtocolPort(model.ProtocolSSH)
			proxyArg := srvconn.SSHClientOptions{
				Host:     gateway.Address,
				Port:     strconv.Itoa(port),
				Username: loginAccount.Username,
				Timeout:  timeout,
			}
			if loginAccount.IsSSHKey() {
				proxyArg.PrivateKey = loginAccount.Secret
			} else {
				proxyArg.Password = loginAccount.Secret
			}
			proxyArgs = append(proxyArgs, proxyArg)
		}
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientProxyClient(proxyArgs...))
	}
	return sshAuthOpts
}

func (s *server) getMatchedAssetsByDirectReq(user *model.User, req *auth.DirectLoginAssetReq) ([]model.Asset, error) {
	assets, err := s.jmsService.GetUserPermAssetsByIP(user.ID, req.AssetInfo)
	if err != nil {
		logger.Errorf("Get asset failed: %s", err)
		return nil, fmt.Errorf("match asset failed: %s", i18n.T("Core API failed"))
	}
	sshAssets := make([]model.Asset, 0, len(assets))
	for i := range assets {
		if assets[i].IsSupportProtocol(req.Protocol) {
			sshAssets = append(sshAssets, assets[i])
		}
	}
	if len(sshAssets) == 0 {
		return nil, fmt.Errorf("match asset failed: %s", i18n.T("No found ssh protocol supported"))
	}
	return sshAssets, nil
}

func (s *server) getMatchedAccounts(user *model.User, req *auth.DirectLoginAssetReq,
	asset model.Asset) ([]model.PermAccount, error) {
	accounts, err := s.jmsService.GetAccountsByUserIdAndAssetId(user.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get account failed: %s", err)
		return nil, err
	}
	matchFunc := func(account *model.PermAccount, name string) bool {
		return account.Username == name
	}
	matched := make([]model.PermAccount, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if matchFunc(&account, req.AccountInfo) {
			matched = append(matched, account)
		}
	}
	return matched, nil
}

func buildDirectRequestOptions(userInfo *model.User, directRequest *auth.DirectLoginAssetReq) []handler.DirectOpt {
	opts := make([]handler.DirectOpt, 0, 7)
	opts = append(opts, handler.DirectTargetAsset(directRequest.AssetInfo))
	opts = append(opts, handler.DirectUser(userInfo))
	opts = append(opts, handler.DirectTargetAccount(directRequest.AccountInfo))
	opts = append(opts, handler.DirectConnectProtocol(directRequest.Protocol))
	if directRequest.IsToken() {
		opts = append(opts, handler.DirectFormatType(handler.FormatToken))
		opts = append(opts, handler.DirectConnectToken(directRequest.ConnectToken))
	}
	return opts
}
