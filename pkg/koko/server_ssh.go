package koko

import (
	"context"
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
	sdkcommon "github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
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

	directReq := sess.Context().Value(auth.ContextKeyDirectLoginFormat)
	if pty, winChan, isPty := sess.Pty(); isPty {
		// PyCharm use command with pty to initialize remote development environment,
		// so let this request execute first
		if len(sess.Command()) != 0 {
			goto direct
		}
		termConf := s.GetTerminalConfig()
		if directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq); ok3 {
			opts := make([]handler.DirectOpt, 0, 5)
			opts = append(opts, handler.DirectTargetAsset(directRequest.AssetInfo))
			opts = append(opts, handler.DirectUser(user))
			opts = append(opts, handler.DirectTerminalConf(&termConf))
			opts = append(opts, handler.DirectTargetSystemUser(directRequest.SysUserInfo))
			if directRequest.IsUUIDString() {
				opts = append(opts, handler.DirectFormatType(handler.FormatUUID))
			}
			if directRequest.IsToken() {
				opts = append(opts, handler.DirectFormatType(handler.FormatToken))
				opts = append(opts, handler.DirectConnectToken(directRequest.Info))
			}
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

direct:
	if directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq); ok3 {
		if directRequest.IsToken() {
			// connection token 的方式使用 vscode 连接
			tokenInfo := directRequest.Info
			matchedType := tokenInfo.TypeName == model.ConnectAsset
			matchedProtocol := tokenInfo.SystemUserAuthInfo.Protocol == model.ProtocolSSH
			assetSupportedSSH := tokenInfo.Asset.IsSupportProtocol(model.ProtocolSSH)
			if !matchedType || !matchedProtocol || !assetSupportedSSH {
				msg := "not ssh asset connection token"
				utils.IgnoreErrWriteString(sess, msg)
				logger.Errorf("vscode failed: %s", msg)
				return
			}
			s.proxyVscodeByTokenInfo(sess, tokenInfo)
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
		selectSysUsers, err := s.getMatchedSystemUsers(user, directRequest, selectedAssets[0])
		if err != nil {
			logger.Error(err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		if len(selectSysUsers) != 1 {
			msg := fmt.Sprintf(i18n.T("Must be unique system user for %s"), directRequest.SysUserInfo)
			utils.IgnoreErrWriteString(sess, msg)
			logger.Error(msg)
			return
		}
		s.proxyVscode(sess, user, selectedAssets[0], selectSysUsers[0])
	}

}

func (s *server) proxyVscode(sess ssh.Session, user *model.User, asset model.Asset,
	systemUser model.SystemUser) {
	ctxId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		utils.IgnoreErrWriteString(sess, "not found ctx id")
		return
	}
	systemUserAuthInfo, err := s.jmsService.GetSystemUserAuthById(systemUser.ID, asset.ID,
		user.ID, user.Username)
	if err != nil {
		logger.Errorf("Get system user auth failed: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	permInfo, err := s.jmsService.ValidateAssetConnectPermission(user.ID,
		asset.ID, systemUser.ID)
	if err != nil {
		logger.Errorf("Get asset Permission info err: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	var domain *model.Domain
	if asset.Domain != "" {
		domainInfo, err := s.jmsService.GetDomainGateways(asset.Domain)
		if err != nil {
			logger.Errorf("Get system user auth failed: %s", err)
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		domain = &domainInfo
	}
	sshAuthOpts := buildSSHClientOptions(&asset, &systemUserAuthInfo, domain)
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get SSH Client failed: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	defer sshClient.Close()
	if len(sess.Command()) != 0 {
		s.proxyDirectCommand(
			sess,
			sshClient,
			&model.ConnectTokenInfo{
				User:               user,
				SystemUserAuthInfo: &systemUserAuthInfo,
				Asset:              &asset,
			},
		)
		return
	}

	if !config.GetConf().EnableVscodeSupport {
		utils.IgnoreErrWriteString(sess, "No support vscode like requested.\n")
		return
	}
	vsReq := &vscodeReq{
		reqId:      ctxId,
		user:       user,
		client:     sshClient,
		expireInfo: &permInfo,
	}
	if err = s.proxyVscodeShell(sess, vsReq, sshClient); err != nil {
		utils.IgnoreErrWriteString(sess, err.Error())
	}
}

func (s *server) proxyVscodeByTokenInfo(sess ssh.Session, tokenInfo *model.ConnectTokenInfo) {
	ctxId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		utils.IgnoreErrWriteString(sess, "not found ctx id")
		return
	}
	asset := tokenInfo.Asset
	systemUserAuthInfo := tokenInfo.SystemUserAuthInfo
	domain := tokenInfo.Domain
	sshAuthOpts := buildSSHClientOptions(asset, systemUserAuthInfo, domain)
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get SSH Client failed: %s", err)
		utils.IgnoreErrWriteString(sess, err.Error())
		return
	}
	defer sshClient.Close()
	if len(sess.Command()) != 0 {
		s.proxyDirectCommand(sess, sshClient, tokenInfo)
		return
	}

	if !config.GetConf().EnableVscodeSupport {
		utils.IgnoreErrWriteString(sess, "No support vscode like requested.\n")
		return
	}

	perm := model.Permission{Actions: tokenInfo.Actions}
	permInfo := model.ExpireInfo{
		HasPermission: perm.EnableConnect(),
		ExpireAt:      tokenInfo.ExpiredAt,
	}
	vsReq := &vscodeReq{
		reqId:      ctxId,
		user:       tokenInfo.User,
		client:     sshClient,
		expireInfo: &permInfo,
	}
	if err = s.proxyVscodeShell(sess, vsReq, sshClient); err != nil {
		utils.IgnoreErrWriteString(sess, err.Error())
	}
}

func (s *server) proxyDirectCommand(sess ssh.Session, sshClient *srvconn.SSHClient,
	token *model.ConnectTokenInfo) {
	rawStr := sess.RawCommand()
	// scp command can not be disabled for vscode initial logic
	// if strings.HasPrefix(rawStr, "scp") {
	// 	logger.Errorf("Not support scp command %s", rawStr)
	// 	utils.IgnoreErrWriteString(sess, "Not support scp command")
	// 	return
	// }
	retSession := &model.Session{
		ID:           common.UUID(),
		User:         token.User.String(),
		SystemUser:   token.SystemUserAuthInfo.String(),
		LoginFrom:    "ST", // means SSH Terminal
		RemoteAddr:   sess.RemoteAddr().String(),
		Protocol:     token.SystemUserAuthInfo.Protocol,
		UserID:       token.User.ID,
		SystemUserID: token.SystemUserAuthInfo.ID,
		Asset:        token.Asset.Hostname,
		AssetID:      token.Asset.ID,
		OrgID:        token.Asset.OrgID,
	}
	err := s.jmsService.CreateSession(*retSession)
	if err != nil {
		logger.Errorf("Create command session err: %s", err)
		return
	}
	ctx, cancel := context.WithCancel(sess.Context())
	defer cancel()
	proxy.AddCommandSession(retSession.ID, cancel)

	defer func() {
		if err2 := s.jmsService.SessionFinished(retSession.ID, sdkcommon.NewNowUTCTime()); err2 != nil {
			logger.Errorf("finish command session err: %s", err)
		}
		proxy.RemoveCommandSession(retSession.ID)
	}()

	goSess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return
	}
	defer goSess.Close()
	defer sshClient.ReleaseSession(goSess)
	go func() {
		<-ctx.Done()
		_ = goSess.Close()
	}()

	// to fix this issue: https://github.com/ploxiln/fab-classic/issues/46
	// make pty for client when client required or command is login shell
	if pty, _, isPty := sess.Pty(); isPty ||
		(strings.Contains(rawStr, "bash --login") || strings.Contains(rawStr, "bash -l")) {
		goSess.RequestPty(
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
	reader := io.MultiReader(out, errOut)
	go func() {
		var outResult strings.Builder
		now := time.Now()
		maxSize := 1024
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
		// trim for database column length
		var input, output string
		if len(rawStr) > 128 {
			input = rawStr[:128]
		} else {
			input = rawStr
		}
		i := strings.LastIndexByte(outResult.String(), '\r')
		if i <= 0 {
			output = outResult.String()
		} else if i > 0 && i < 1024 {
			output = outResult.String()[:i]
		} else {
			output = outResult.String()[:1024]
		}
		termCfg := s.GetTerminalConfig()
		cmdStorage := proxy.NewCommandStorage(s.jmsService, &termCfg)
		if err2 := cmdStorage.BulkSave([]*model.Command{&model.Command{
			SessionID:   retSession.ID,
			OrgID:       retSession.OrgID,
			Input:       input,
			Output:      output,
			User:        retSession.User,
			Server:      retSession.Asset,
			SystemUser:  token.SystemUserAuthInfo.String(),
			Timestamp:   now.Unix(),
			DateCreated: now.UTC(),
			RiskLevel:   model.NormalLevel,
		}}); err2 != nil {
			logger.Errorf("Create command err: %s", err2)
		}
	}()
	err = goSess.Run(rawStr)
	if err != nil {
		logger.Errorf("User %s Run command %s failed: %s",
			token.User.String(), rawStr, err)
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

func buildSSHClientOptions(asset *model.Asset, systemUserAuthInfo *model.SystemUserAuthInfo,
	domain *model.Domain) []srvconn.SSHClientOption {
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

	if domain != nil && len(domain.Gateways) > 0 {
		proxyArgs := make([]srvconn.SSHClientOptions, 0, len(domain.Gateways))
		for i := range domain.Gateways {
			gateway := domain.Gateways[i]
			proxyArg := srvconn.SSHClientOptions{
				Host:       gateway.IP,
				Port:       strconv.Itoa(gateway.Port),
				Username:   gateway.Username,
				Password:   gateway.Password,
				Passphrase: gateway.Password, // 兼容 带密码的private_key,
				PrivateKey: gateway.PrivateKey,
				Timeout:    timeout,
			}
			proxyArgs = append(proxyArgs, proxyArg)
		}
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientProxyClient(proxyArgs...))
	}
	return sshAuthOpts
}

func (s *server) getMatchedAssetsByDirectReq(user *model.User, req *auth.DirectLoginAssetReq) ([]model.Asset, error) {
	if req.IsUUIDString() {
		asset, err := s.jmsService.GetAssetById(req.AssetInfo)
		if err != nil {
			logger.Errorf("Get asset failed: %s", err)
			return nil, fmt.Errorf("match asset failed: %s", i18n.T("Core API failed"))
		}
		return []model.Asset{asset}, nil
	}
	assets, err := s.jmsService.GetUserPermAssetsByIP(user.ID, req.AssetInfo)
	if err != nil {
		logger.Errorf("Get asset failed: %s", err)
		return nil, fmt.Errorf("match asset failed: %s", i18n.T("Core API failed"))
	}
	return assets, nil
}

func (s *server) getMatchedSystemUsers(user *model.User, req *auth.DirectLoginAssetReq,
	asset model.Asset) ([]model.SystemUser, error) {
	if req.IsUUIDString() {
		systemUser, err := s.jmsService.GetSystemUserById(req.SysUserInfo)
		if err != nil {
			logger.Errorf("Get systemUser failed: %s", err)
			return nil, fmt.Errorf("match systemuser failed: %s", i18n.T("Core API failed"))
		}
		return []model.SystemUser{systemUser}, nil
	}
	systemUsers, err := s.jmsService.GetSystemUsersByUserIdAndAssetId(user.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get systemUser failed: %s", err)
		return nil, fmt.Errorf("match systemuser failed: %s", i18n.T("Core API failed"))
	}
	matched := make([]model.SystemUser, 0, len(systemUsers))
	for i := range systemUsers {
		compareUsername := systemUsers[i].Username

		if systemUsers[i].UsernameSameWithUser {
			// 此为动态系统用户，系统用户名和登录用户名相同
			compareUsername = user.Username
		}
		if compareUsername == req.SysUserInfo {
			matched = append(matched, systemUsers[i])
		}
	}
	return matched, nil
}
