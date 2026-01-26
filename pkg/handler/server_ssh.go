package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/cache"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/session"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

const ctxID = "ctxID"

func (s *Server) PasswordAuth(ctx ssh.Context, password string) error {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PasswordAuth {
		logger.Info("Core API disable password auth")
		return errors.New("password auth disabled")
	}
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	return sshAuthHandler(ctx, password, "")
}

func (s *Server) PublicKeyAuth(ctx ssh.Context, key ssh.PublicKey) error {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PublicKeyAuth {
		logger.Info("Core API disable publickey auth")
		return errors.New("publickey auth disabled")
	}
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	value := string(gossh.MarshalAuthorizedKey(key))
	return sshAuthHandler(ctx, "", value)
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
		selectedAssets, err := s.getMatchedAssetsByDirectReq(currentUser, directRequest)
		if err != nil {
			logger.Errorf("Get matched assets failed: %s", err)
			return
		}
		if directRequest.IsToken() && config.GetConf().ConnectionTokenReusable {
			tokenInfo := directRequest.ConnectToken
			key := cache.CreateAddrCacheKey(sess.RemoteAddr(), tokenInfo.Id)
			// 缓存 token 信息
			cache.TokenCacheInstance.Save(key, tokenInfo)
			defer cache.TokenCacheInstance.Recycle(key)
			logger.Infof("SFTP token key %s cached", key)
		}
		opts := buildDirectRequestOptions(currentUser, directRequest)
		opts = append(opts, DirectConnectSftpMode(true))
		opts = append(opts, DirectAssets(selectedAssets))
		opts = append(opts, DirectTerminalConf(&termConf))
		directSrv := NewDirectHandler(sess, s.jmsService, opts...)
		sftpHandler = directSrv.NewSFTPHandler()
	} else {
		sftpHandler = s.NewSftpHandler(currentUser, addr)
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

func (s *Server) NewSftpHandler(user *model.User, addr string) *SftpHandler {
	terminalCfg := s.GetTerminalConfig()
	opts := make([]srvconn.UserSftpOption, 0, 5)
	opts = append(opts, srvconn.WithUser(user))
	opts = append(opts, srvconn.WithRemoteAddr(addr))
	opts = append(opts, srvconn.WithLoginFrom(model.LoginFromSSH))
	opts = append(opts, srvconn.WithTerminalCfg(&terminalCfg))
	return &SftpHandler{
		UserSftpConn: srvconn.NewUserSftpConn(s.jmsService, opts...),
		recorder:     proxy.GetFTPFileRecorder(s.jmsService),
	}
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
	i18nLang := i18n.NewLang(user.Language, s.jmsService)
	termConf := s.GetTerminalConfig()
	directReq := sess.Context().Value(auth.ContextKeyDirectLoginFormat)
	if pty, winChan, isPty := sess.Pty(); isPty && sess.RawCommand() == "" {
		if directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq); ok3 {
			opts := buildDirectRequestOptions(user, directRequest)
			opts = append(opts, DirectTerminalConf(&termConf))
			if !directRequest.IsToken() {
				selectedAssets, err := s.getMatchedAssetsByDirectReq(user, directRequest)
				if err != nil {
					utils.IgnoreErrWriteString(sess, err.Error())
					logger.Errorf("Get matched assets failed: %s", err)
					return
				}
				opts = append(opts, DirectAssets(selectedAssets))
			}
			directSrv := NewDirectHandler(sess, s.jmsService, opts...)
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
			msg := fmt.Sprintf(i18nLang.T("Must be unique asset for %s"), directRequest.AssetTarget)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		permAssetDetail, err := s.jmsService.GetUserPermAssetDetailById(user.ID, selectedAssets[0].ID)
		if err != nil {
			msg := fmt.Sprintf(i18nLang.T("Must be unique asset for %s"), directRequest.AssetTarget)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Errorf("Get permAssetDetail failed: %s", err)
			return
		}

		matchedProtocol := directRequest.Protocol == model.ProtocolSSH
		assetSupportedSSH := permAssetDetail.SupportProtocol(model.ProtocolSSH)
		if !matchedProtocol || !assetSupportedSSH {
			msg := "not ssh asset connection"
			utils.IgnoreErrWriteString(sess, msg)
			logger.Errorf("Direct Request ssh failed: %s", msg)
			return
		}

		selectAccounts, err := s.getMatchedAccounts(user, directRequest, permAssetDetail)
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		if len(selectAccounts) != 1 {
			msg := fmt.Sprintf(i18nLang.T("Must be unique account for %s"), directRequest.AccountUsername)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		selectAccount := selectAccounts[0]
		switch selectAccount.Username {
		case "@INPUT", "@USER":
			msg := fmt.Sprintf(i18nLang.T("Must be auto login account for %s"), directRequest.AccountUsername)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		default:
			s.proxyDirectRequest(sess, user, selectedAssets[0], selectAccount)
		}
	}

}

func (s *Server) proxyDirectRequest(sess ssh.Session, user *model.User, asset model.PermAsset,
	permAccount model.PermAccount) {
	//  仅支持 ssh 的协议资产
	remoteAddr, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	req := &service.SuperConnectTokenReq{
		UserId:        user.ID,
		AssetId:       asset.ID,
		Account:       permAccount.Alias,
		Protocol:      model.ProtocolSSH,
		ConnectMethod: model.ProtocolSSH,
		RemoteAddr:    remoteAddr,
	}
	// ssh 非交互式的直连格式，不支持资产的登录复核
	tokenInfo, err := s.jmsService.CreateSuperConnectToken(req)
	if err != nil {
		msg := err.Error()
		if tokenInfo.Detail != "" {
			msg = tokenInfo.Detail
		}
		logger.Errorf("Create super connect token failed: %s", msg)
		return
	}
	connectToken, err := s.jmsService.GetConnectTokenInfo(tokenInfo.ID, true)
	if err != nil {
		logger.Errorf("Create super connect token err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	s.proxyTokenInfo(sess, &connectToken)
}

func (s *Server) proxyTokenInfo(sess ssh.Session, tokenInfo *model.ConnectToken) {
	ctxId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		utils.IgnoreErrWriteString(sess, "not found ctx id")
		return
	}
	asset := tokenInfo.Asset
	account := tokenInfo.Account
	var gateways []model.Gateway
	// todo：domain
	if tokenInfo.Gateway != nil {
		gateways = []model.Gateway{*tokenInfo.Gateway}
	}
	enableReused := config.GetConf().ReuseConnection
	reusedKey := GenerateSSHTokenResueKey(tokenInfo)

	var (
		sshClient *srvconn.SSHClient
		ok1       bool
		err1      error
	)
	if enableReused {
		sshClient, ok1 = srvconn.GetClientFromCache(reusedKey)
		if ok1 {
			logger.Infof("reused ssh client: %s", sshClient)
		}
	}
	if sshClient == nil {
		sshAuthOpts := buildSSHClientOptions(&asset, &account, gateways)
		// add Reuse ssh client
		sshClient, err1 = srvconn.NewSSHClient(sshAuthOpts...)
		if err1 != nil {
			logger.Errorf("Get SSH Client failed: %s", err1)
			utils.IgnoreErrWriteString(sess, err1.Error())
			return
		}
		if enableReused {
			srvconn.AddClientCache(reusedKey, sshClient)
		}
	}
	//defer sshClient.Close()
	vsReq := &vscodeReq{
		reqId:      ctxId,
		user:       &tokenInfo.User,
		client:     sshClient,
		expireInfo: tokenInfo.ExpireAt,
		forwards:   make(map[string]net.Listener),
	}

	go func() {
		s.addVSCodeReq(vsReq)
		defer s.deleteVSCodeReq(vsReq)
		<-sess.Context().Done()
		if sshClient.KeyId != "" {
			srvconn.ReleaseClientCacheKey(sshClient.KeyId, sshClient)
		} else {
			_ = sshClient.Close()
		}
		logger.Infof("User %s end vscode request %s", vsReq.user, sshClient)
	}()
	if len(sess.Command()) != 0 {
		s.proxyAssetCommand(sess, sshClient, tokenInfo)
		return
	}

	if !config.GetConf().EnableVscodeSupport {
		utils.IgnoreErrWriteString(sess, "No support vscode like requested.\n")
		return
	}

	if err := s.proxyVscodeShell(sess, vsReq, sshClient, tokenInfo); err != nil {
		utils.IgnoreErrWriteString(sess, err.Error())
	}
}

func IsScpCommand(rawStr string) bool {
	rawCommands := strings.Split(rawStr, ";")
	for _, cmd := range rawCommands {
		cmd = strings.TrimSpace(cmd)
		if strings.HasPrefix(cmd, "scp") {
			return true
		}
	}
	return false
}

func (s *Server) recordSessionLifecycle(sid string, event model.LifecycleEvent, reason string) {
	logObj := model.SessionLifecycleLog{Reason: reason}
	if err2 := s.jmsService.RecordSessionLifecycleLog(sid, event, logObj); err2 != nil {
		logger.Errorf("Record session %s lifecycle %s failed: %s", sid, event, err2)
	}
}

func (s *Server) proxyAssetCommand(sess ssh.Session, sshClient *srvconn.SSHClient,
	tokenInfo *model.ConnectToken) {
	rawStr := sess.RawCommand()
	if IsScpCommand(rawStr) {
		if !config.GetConf().EnableVscodeSupport {
			logger.Errorf("Not support scp command: %s", rawStr)
			utils.IgnoreErrWriteString(sess, "Not support scp command")
			return
		}
		// 开启了 vscode 支持，放开使用 scp 命令传输文件
		// todo: 解析 scp 数据包，获取文件信息
		logger.Infof("Execute scp command: %s", rawStr)
	} else {
		logger.Infof("Execute command: %s", rawStr)
	}

	// todo: 暂且不支持 acl 工单
	acls := tokenInfo.CommandFilterACLs
	sort.Sort(model.CommandACLs(acls))
	for i := range acls {
		acl := acls[i]
		_, action, _ := acl.Match(rawStr)
		switch action {
		case model.ActionReview:
			msg := "SSH Command not support ACL review ticket"
			utils.IgnoreErrWriteString(sess, msg)
			logger.Errorf("SSH Command not support ACL review ticket `%s`", rawStr)
			return
		case model.ActionReject:
			logger.Errorf("ACL reject execute %s ", rawStr)
			return
		default:
		}
		if action == model.ActionAccept {
			logger.Debugf("ACL accept execute %s ", rawStr)
			break
		}
	}

	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	reqSession := tokenInfo.CreateSession(host, model.LoginFromSSH, model.COMMANDType)
	respSession, err := s.jmsService.CreateSession(reqSession)
	if err != nil {
		logger.Errorf("Create command session err: %s", err)
		return
	}
	ctx, cancel := context.WithCancel(sess.Context())
	defer cancel()
	respSession.TokenId = tokenInfo.Id
	traceSession := session.NewSession(&respSession, func(task *model.TerminalTask) error {
		switch task.Name {
		case model.TaskKillSession:
			cancel()
			logger.Infof("User %s end command request %s as task kill_session",
				tokenInfo.User.String(), sshClient)
			return nil
		case model.TaskPermExpired:
			cancel()
			logger.Infof("User %s end command request %s as task permission has expired",
				tokenInfo.User.String(), sshClient)
			return nil
		case model.TaskPermValid:
			return nil

		}
		return fmt.Errorf("ssh proxy not support task: %s", task.Name)
	})
	session.AddSession(traceSession)

	defer func() {
		if _, err2 := s.jmsService.SessionFinished(respSession.ID, common.NewNowUTCTime()); err2 != nil {
			logger.Errorf("Create tunnel session err: %s", err2)
		}
		session.RemoveSession(traceSession)
	}()

	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return
	}
	s.recordSessionLifecycle(respSession.ID, model.AssetConnectSuccess, "")
	defer goSess.Close()
	defer sshClient.ReleaseSession(goSess)
	go func() {
		<-ctx.Done()
		_ = goSess.Close()
	}()

	// to fix this issue: https://github.com/ploxiln/fab-classic/issues/46
	// make pty for client when client required or command is login shell
	if pty, _, isPty := sess.Pty(); isPty &&
		(strings.Contains(rawStr, "bash --login") || strings.Contains(rawStr, "bash -l")) {
		_ = goSess.RequestPty(
			pty.Term,
			pty.Window.Width,
			pty.Window.Height,
			gossh.TerminalModes{
				gossh.ECHO:          1,     // enable echoing
				gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4 kbaud
				gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4 kbaud
			},
		)
	}

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
	stderrWriter := sess.Stderr()
	recordBuf := utils.NewMaxSizeBuffer(1024)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		outRecorderWriter := io.MultiWriter(sess, recordBuf)
		_, _ = io.Copy(outRecorderWriter, out)
		logger.Debugf("User %s finished session stdout", tokenInfo.User.String())
	}()

	go func() {
		defer wg.Done()
		errRecorderWriter := io.MultiWriter(stderrWriter, recordBuf)
		_, _ = io.Copy(errRecorderWriter, errOut)
		logger.Debugf("User %s finished session stderr", tokenInfo.User.String())
	}()
	now := time.Now()
	err = goSess.Run(rawStr)
	if err != nil {
		logger.Errorf("User %s Run command %s failed: %s",
			tokenInfo.User.String(), rawStr, err)
		var exitErr *gossh.ExitError
		if errors.As(err, &exitErr) {
			exitCode := exitErr.ExitStatus()
			if err1 := sess.Exit(exitCode); err1 != nil {
				logger.Errorf("Create sess exit code %d err: %s", exitCode, err1)
			}
		}
	}
	wg.Wait()
	cmd := model.Command{
		SessionID:   respSession.ID,
		OrgID:       respSession.OrgID,
		Input:       rawStr,
		User:        respSession.User,
		Server:      respSession.Asset,
		Account:     respSession.Account,
		Timestamp:   now.Unix(),
		DateCreated: now,
	}
	outResult := recordBuf.String()
	cmd.Output = strings.ReplaceAll(outResult, "\x00", "")
	termCfg := s.GetTerminalConfig()
	cmdStorage := proxy.NewCommandStorage(s.jmsService, &termCfg)
	if err2 := cmdStorage.BulkSave([]*model.Command{&cmd}); err2 != nil {
		logger.Errorf("Create command err: %s", err2)
	}
	reason := string(model.ReasonErrConnectDisconnect)
	s.recordSessionLifecycle(respSession.ID, model.AssetConnectFinished, reason)
}

func (s *Server) proxyVscodeShell(sess ssh.Session, vsReq *vscodeReq, sshClient *srvconn.SSHClient,
	tokenInfo *model.ConnectToken) error {
	host, _, _ := net.SplitHostPort(sess.RemoteAddr().String())
	reqSession := tokenInfo.CreateSession(host, model.LoginFromSSH, model.TUNNELType)
	respSession, err := s.jmsService.CreateSession(reqSession)
	if err != nil {
		logger.Errorf("Create tunnel session err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return err
	}
	ctx, cancel := context.WithCancel(sess.Context())
	defer cancel()
	traceSession := session.NewSession(&respSession, func(task *model.TerminalTask) error {
		switch task.Name {
		case model.TaskKillSession:
			cancel()
			logger.Infof("User %s end vscode request %s as task kill_session", vsReq.user, sshClient)
			return nil
		case model.TaskPermExpired:
			cancel()
			logger.Infof("User %s end vscode request %s as permission has expired", vsReq.user, sshClient)
			return nil
		case model.TaskPermValid:
			return nil

		}
		return fmt.Errorf("ssh proxy not support task: %s", task.Name)
	})
	session.AddSession(traceSession)
	defer func() {
		if _, err2 := s.jmsService.SessionFinished(respSession.ID, common.NewNowUTCTime()); err2 != nil {
			logger.Errorf("Create tunnel session err: %s", err2)
		}
		session.RemoveSession(traceSession)
	}()

	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return err
	}
	s.recordSessionLifecycle(respSession.ID, model.AssetConnectSuccess, "")
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
		s.recordSessionLifecycle(respSession.ID, model.AssetConnectFinished, err.Error())
		return err
	}
	logger.Infof("User %s start vscode request to %s", vsReq.user, sshClient)

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
		case <-ctx.Done():
			logger.Infof("SSH conn[%s] User %s end vscode request %s as session done",
				vsReq.reqId, vsReq.user, sshClient)
			reason := string(model.ReasonErrConnectDisconnect)
			s.recordSessionLifecycle(respSession.ID, model.AssetConnectFinished, reason)
			return nil
		case now := <-ticker.C:
			if vsReq.expireInfo.IsExpired(now) {
				logger.Infof("SSH conn[%s] User %s end vscode request %s as permission has expired",
					vsReq.reqId, vsReq.user, sshClient)
				reason := string(model.ReasonErrPermissionExpired)
				s.recordSessionLifecycle(respSession.ID, model.AssetConnectFinished, reason)
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

func (s *Server) getMatchedAssetsByDirectReq(user *model.User, req *auth.DirectLoginAssetReq) ([]model.PermAsset, error) {
	var getUserPermAssets func() ([]model.PermAsset, error)
	if common.IsUUID(req.AssetTarget) {
		getUserPermAssets = func() ([]model.PermAsset, error) {
			return s.jmsService.GetUserPermAssetsById(user.ID, req.AssetTarget)
		}
	} else {
		getUserPermAssets = func() ([]model.PermAsset, error) {
			return s.jmsService.GetUserPermAssetsByIP(user.ID, req.AssetTarget)
		}
	}
	i18nLang := i18n.NewLang(user.Language, s.jmsService)
	assets, err := getUserPermAssets()
	if err != nil {
		logger.Errorf("Get user %s perm asset failed: %s", user.String(), err)
		return nil, fmt.Errorf("match asset failed: %s", i18nLang.T("Core API failed"))
	}
	if len(assets) == 0 {
		logger.Infof("User %s no perm for asset %s", user.String(), req.AssetTarget)
		return nil, fmt.Errorf("match asset failed: %s", i18nLang.T("No found asset"))
	}
	return assets, nil
}

func (s *Server) getMatchedAccounts(user *model.User, req *auth.DirectLoginAssetReq,
	permAssetDetail model.PermAssetDetail) ([]model.PermAccount, error) {
	matched := GetMatchedAccounts(permAssetDetail.PermedAccounts, req.AccountUsername)
	return matched, nil
}

func buildDirectRequestOptions(user *model.User, directRequest *auth.DirectLoginAssetReq) []DirectOpt {
	opts := make([]DirectOpt, 0, 7)
	opts = append(opts, DirectUser(user))
	opts = append(opts, DirectTargetAccount(directRequest.AccountUsername))
	opts = append(opts, DirectConnectProtocol(directRequest.Protocol))
	if directRequest.IsToken() {
		opts = append(opts, DirectFormatType(FormatToken))
		opts = append(opts, DirectConnectToken(directRequest.ConnectToken))
	}
	return opts
}

func (s *Server) buildConnectToken(ctx ssh.Context, user *model.User, req *auth.DirectLoginAssetReq) (*model.ConnectToken, error) {
	selectedAssets, err := s.getMatchedAssetsByDirectReq(user, req)
	if err != nil {
		return nil, err
	}
	i18nLang := i18n.NewLang(user.Language, s.jmsService)
	if len(selectedAssets) != 1 {
		msg := fmt.Sprintf(i18nLang.T("Must be unique asset for %s"), req.AssetTarget)
		return nil, errors.New(msg)
	}
	permAssetDetail, err := s.jmsService.GetUserPermAssetDetailById(user.ID, selectedAssets[0].ID)
	if err != nil {
		msg := fmt.Sprintf(i18nLang.T("Must be unique asset for %s"), req.AssetTarget)
		logger.Errorf("Get permAssetDetail failed: %s", err)
		return nil, errors.New(msg)
	}

	matchedProtocol := req.Protocol == model.ProtocolSSH
	assetSupportedSSH := permAssetDetail.SupportProtocol(model.ProtocolSSH)
	if !matchedProtocol || !assetSupportedSSH {
		msg := "not ssh asset connection"
		logger.Errorf("Direct Request ssh failed: %s", msg)
		return nil, errors.New(msg)
	}

	selectAccounts, err := s.getMatchedAccounts(user, req, permAssetDetail)
	if err != nil {
		return nil, err
	}
	if len(selectAccounts) != 1 {
		msg := fmt.Sprintf(i18nLang.T("Must be unique account for %s"), req.AccountUsername)
		logger.Error(msg)
		return nil, errors.New(msg)
	}
	selectAccount := selectAccounts[0]
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	sessReq := &service.SuperConnectTokenReq{
		UserId:        user.ID,
		AssetId:       permAssetDetail.ID,
		Account:       selectAccount.Alias,
		Protocol:      model.ProtocolSSH,
		ConnectMethod: model.ProtocolSSH,
		RemoteAddr:    remoteAddr,
	}
	// ssh 非交互式的直连格式，不支持资产的登录复核
	tokenInfo, err := s.jmsService.CreateSuperConnectToken(sessReq)
	if err != nil {
		msg := err.Error()
		if tokenInfo.Detail != "" {
			msg = tokenInfo.Detail
		}
		logger.Errorf("Create super connect token failed: %s", msg)
		return nil, err
	}
	connectToken, err := s.jmsService.GetConnectTokenInfo(tokenInfo.ID, true)
	if err != nil {
		logger.Errorf("Create super connect token err: %s", err)
		return nil, err
	}
	return &connectToken, nil
}

func (s *Server) buildSSHClient(tokenInfo *model.ConnectToken) (*srvconn.SSHClient, error) {
	asset := tokenInfo.Asset
	account := tokenInfo.Account
	var gateways []model.Gateway
	if tokenInfo.Gateway != nil {
		gateways = []model.Gateway{*tokenInfo.Gateway}
	}
	sshAuthOpts := buildSSHClientOptions(&asset, &account, gateways)
	// add reuse ssh client
	enableReused := config.GetConf().ReuseConnection
	reusedKey := GenerateSSHTokenResueKey(tokenInfo)
	if enableReused {
		if client, ok := srvconn.GetClientFromCache(reusedKey); ok {
			logger.Infof("Reused ssh client key: %s", reusedKey)
			return client, nil
		}
	}
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get SSH Client failed: %s", err)
		return sshClient, err
	}
	if enableReused {
		srvconn.AddClientCache(reusedKey, sshClient)
	}
	return sshClient, nil
}

func GenerateSSHTokenResueKey(tokenInfo *model.ConnectToken) string {
	userId := tokenInfo.User.ID
	assetId := tokenInfo.Asset.ID
	ip := tokenInfo.Asset.Address
	port := tokenInfo.Asset.ProtocolPort("ssh")
	accountUsername := tokenInfo.Account.Username
	return fmt.Sprintf("SSHD_%s_%s_%s_%d_%s",
		userId, assetId, ip, port, accountUsername)
}
