package handler

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
	"github.com/jumpserver/koko/pkg/i18n"
	modelCommon "github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

const ctxID = "ctxID"

func (s *Server) PasswordAuth(ctx ssh.Context, password string) ssh.AuthResult {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PasswordAuth {
		logger.Info("Core API disable password auth auth")
		return ssh.AuthFailed
	}
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	return sshAuthHandler(ctx, password, "")
}

func (s *Server) PublicKeyAuth(ctx ssh.Context, key ssh.PublicKey) ssh.AuthResult {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PublicKeyAuth {
		logger.Info("Core API disable publickey auth")
		return ssh.AuthFailed
	}
	publicKey := common.Base64Encode(string(key.Marshal()))
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	return sshAuthHandler(ctx, "", publicKey)
}

func (s *Server) SFTPHandler(sess ssh.Session) {
	currentUser, ok := sess.Context().Value(auth.ContextKeyUser).(*model.User)
	if !ok || currentUser.ID == "" {
		logger.Errorf("SFTP User not found, exit.")
		return
	}
	addr, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	directReq := sess.Context().Value(auth.ContextKeyDirectLoginFormat)
	var sftpHandler *SftpHandler
	termConf := s.GetTerminalConfig()
	if directRequest, ok2 := directReq.(*auth.DirectLoginAssetReq); ok2 {
		opts := buildDirectRequestOptions(currentUser, directRequest)
		opts = append(opts, DirectConnectSftpMode(true))

		opts = append(opts, DirectTerminalConf(&termConf))
		directSrv, err := NewDirectHandler(sess, s.jmsService, opts...)
		if err != nil {
			logger.Errorf("User %s direct sftp request err: %s", currentUser.Name, err)
			return
		}
		sftpHandler = directSrv.NewSFTPHandler()
	}
	if sftpHandler == nil {
		sftpHandler = NewSFTPHandler(s.jmsService, currentUser, addr)
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

func (s *Server) LocalPortForwardingPermission(ctx ssh.Context, dstHost string, dstPort uint32) bool {
	logger.Debugf("LocalPortForwardingPermission: %s %s %d", ctx.User(), dstHost, dstPort)
	return config.GlobalConfig.EnableLocalPortForward
}

func (s *Server) DirectTCPIPChannelHandler(ctx ssh.Context, newChan gossh.NewChannel, destAddr string) {
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

func (s *Server) SessionHandler(sess ssh.Session) {
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
			opts = append(opts, DirectTerminalConf(&termConf))
			directSrv, err := NewDirectHandler(sess, s.jmsService, opts...)
			if err != nil {
				logger.Errorf("User %s direct request err: %s", user.Name, err)
				return
			}
			directSrv.Dispatch()
			return
		}

		interactiveSrv := NewInteractiveHandler(sess, user, s.jmsService, termConf)
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
			msg := fmt.Sprintf(i18n.T("Must be unique asset for %s"), directRequest.AssetIP)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		matchedProtocol := directRequest.Protocol == model.ProtocolSSH
		assetSupportedSSH := selectedAssets[0].IsSupportProtocol(model.ProtocolSSH)
		if !matchedProtocol || !assetSupportedSSH {
			msg := "not ssh asset connection"
			utils.IgnoreErrWriteString(sess, msg)
			logger.Errorf("Direct Request ssh failed: %s", msg)
			return
		}

		selectAccounts, err := s.getMatchedAccounts(user, directRequest, selectedAssets[0])
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		if len(selectAccounts) != 1 {
			msg := fmt.Sprintf(i18n.T("Must be unique account for %s"), directRequest.AccountUsername)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		selectAccount := selectAccounts[0]
		switch selectAccount.Username {
		case "@INPUT", "@USER":
			msg := fmt.Sprintf(i18n.T("Must be auto login account for %s"), directRequest.AccountUsername)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		default:
			s.proxyDirectRequest(sess, user, selectedAssets[0], selectAccount)
		}
	}

}

func (s *Server) proxyDirectRequest(sess ssh.Session, user *model.User, asset model.Asset,
	permAccount model.PermAccount) {
	//  仅支持 ssh 的协议资产
	req := &service.SuperConnectTokenReq{
		UserId:        user.ID,
		AssetId:       asset.ID,
		Account:       permAccount.Name,
		Protocol:      model.ProtocolSSH,
		ConnectMethod: model.ProtocolSSH,
	}
	connectToken, err := s.jmsService.CreateConnectTokenAndGetAuthInfo(req)
	if err != nil {
		logger.Errorf("Create super connect token err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	s.proxyTokenInfo(sess, &connectToken)
}

func (s *Server) proxyTokenInfo(sess ssh.Session, tokeInfo *model.ConnectToken) {
	ctxId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		utils.IgnoreErrWriteString(sess, "not found ctx id")
		return
	}
	asset := tokeInfo.Asset
	account := tokeInfo.Account
	var gateways []model.Gateway
	// todo：domain
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
		s.proxyAssetCommand(sess, sshClient, tokeInfo)
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
	if err = s.proxyVscodeShell(sess, vsReq, sshClient, tokeInfo); err != nil {
		utils.IgnoreErrWriteString(sess, err.Error())
	}
}

func (s *Server) proxyAssetCommand(sess ssh.Session, sshClient *srvconn.SSHClient,
	tokeInfo *model.ConnectToken) {
	rawStr := sess.RawCommand()
	if strings.HasPrefix(rawStr, "scp") {
		logger.Errorf("Not support scp command %s", rawStr)
		utils.IgnoreErrWriteString(sess, "Not support scp command")
		return
	}
	// todo 命令过滤规则，命令复核规则
	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	session := model.Session{
		User:       tokeInfo.User.String(),
		Asset:      tokeInfo.Asset.String(),
		Account:    tokeInfo.Account.String(),
		Protocol:   tokeInfo.Protocol,
		RemoteAddr: host,
		DateStart:  modelCommon.NewNowUTCTime(),
		OrgID:      tokeInfo.OrgId,
		UserID:     tokeInfo.User.ID,
		AssetID:    tokeInfo.Asset.ID,
		Type:       model.COMMANDType,
	}
	retSession, err := s.jmsService.CreateSession(session)
	if err != nil {
		logger.Errorf("Create command session err: %s", err)
		return
	}

	defer func() {
		if err2 := s.jmsService.SessionFinished(retSession.ID, modelCommon.NewNowUTCTime()); err2 != nil {
			logger.Errorf("Create tunnel session err: %s", err)
		}
	}()

	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return
	}
	defer goSess.Close()
	defer sshClient.ReleaseSession(goSess)
	goSess.Stdin = sess
	out, err := goSess.StdoutPipe()
	if err != nil {
		logger.Errorf("Get SSH session stdout failed: %s", err)
		return
	}
	errOut, err := goSess.StderrPipe()
	if err != nil {
		logger.Errorf("Get SSH session stderr failed: %s", err)
		return
	}
	reader := io.MultiReader(out, errOut)
	go func() {
		now := time.Now()
		var outResult strings.Builder
		maxSize := 1024
		cmd := model.Command{
			SessionID:   retSession.ID,
			OrgID:       retSession.OrgID,
			Input:       rawStr,
			User:        retSession.User,
			Server:      retSession.Asset,
			Account:     retSession.Account,
			Timestamp:   now.Unix(),
			DateCreated: now,
		}
		buf := make([]byte, 1024)
		for {
			n, err := reader.Read(buf)
			if err != nil {
				if err != io.EOF {
					logger.Errorf("Read ssh session output failed: %s", err)
				}
				break
			}
			_, _ = sess.Write(buf[:n])
			maxSize -= n
			if maxSize >= 0 {
				_, _ = outResult.Write(buf[:n])
			}
		}
		cmd.Output = outResult.String()
		// todo: 上传到对应存储设置
		if err := s.jmsService.PushSessionCommand([]*model.Command{&cmd}); err != nil {
			logger.Errorf("Create command err: %s", err)
		}
	}()
	err = goSess.Run(rawStr)
	if err != nil {
		logger.Errorf("User %s Run command %s failed: %s",
			tokeInfo.User.String(), rawStr, err)
	}
}

func (s *Server) proxyVscodeShell(sess ssh.Session, vsReq *vscodeReq, sshClient *srvconn.SSHClient,
	tokeInfo *model.ConnectToken) error {
	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	session := model.Session{
		User:       tokeInfo.User.String(),
		Asset:      tokeInfo.Asset.String(),
		Account:    tokeInfo.Account.String(),
		Protocol:   tokeInfo.Protocol,
		RemoteAddr: host,
		DateStart:  modelCommon.NewNowUTCTime(),
		LoginFrom:  model.LoginFromSSH,
		OrgID:      tokeInfo.OrgId,
		UserID:     tokeInfo.User.ID,
		AssetID:    tokeInfo.Asset.ID,
		Type:       model.TUNNELType,
	}
	retSession, err := s.jmsService.CreateSession(session)
	if err != nil {
		logger.Errorf("Create tunnel session err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return err
	}

	defer func() {
		if err2 := s.jmsService.SessionFinished(retSession.ID, modelCommon.NewNowUTCTime()); err2 != nil {
			logger.Errorf("Create tunnel session err: %s", err)
		}
	}()

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

func (s *Server) getMatchedAssetsByDirectReq(user *model.User, req *auth.DirectLoginAssetReq) ([]model.Asset, error) {
	assets, err := s.jmsService.GetUserPermAssetsByIP(user.ID, req.AssetIP)
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

func (s *Server) getMatchedAccounts(user *model.User, req *auth.DirectLoginAssetReq,
	asset model.Asset) ([]model.PermAccount, error) {
	accounts, err := s.jmsService.GetAccountsByUserIdAndAssetId(user.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get account failed: %s", err)
		return nil, err
	}
	matched := GetMatchedAccounts(accounts, req.AccountUsername)
	return matched, nil
}

func buildDirectRequestOptions(user *model.User, directRequest *auth.DirectLoginAssetReq) []DirectOpt {
	opts := make([]DirectOpt, 0, 7)
	opts = append(opts, DirectTargetAsset(directRequest.AssetIP))
	opts = append(opts, DirectUser(user))
	opts = append(opts, DirectTargetAccount(directRequest.AccountUsername))
	opts = append(opts, DirectConnectProtocol(directRequest.Protocol))
	if directRequest.IsToken() {
		opts = append(opts, DirectFormatType(FormatToken))
		opts = append(opts, DirectConnectToken(directRequest.ConnectToken))
	}
	return opts
}
