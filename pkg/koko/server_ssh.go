package koko

import (
	"context"
	"errors"
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

func (s *Server) GetSSHAddr() string {
	cf := config.GlobalConfig
	return net.JoinHostPort(cf.BindHost, cf.SSHPort)
}
func (s *Server) GetSSHSigner() ssh.Signer {
	conf := s.GetTerminalConfig()
	singer, err := sshd.ParsePrivateKeyFromString(conf.HostKey)
	if err != nil {
		logger.Fatal(err)
	}
	return singer
}

func (s *Server) KeyboardInteractiveAuth(ctx ssh.Context,
	challenger gossh.KeyboardInteractiveChallenge) sshd.AuthStatus {
	return auth.SSHKeyboardInteractiveAuth(ctx, challenger)
}

const ctxID = "ctxID"

func (s *Server) PasswordAuth(ctx ssh.Context, password string) sshd.AuthStatus {
	ctx.SetValue(ctxID, ctx.SessionID())
	tConfig := s.GetTerminalConfig()
	if !tConfig.PasswordAuth {
		logger.Info("Core API disable password auth auth")
		return sshd.AuthFailed
	}
	sshAuthHandler := auth.SSHPasswordAndPublicKeyAuth(s.jmsService)
	return sshAuthHandler(ctx, password, "")
}

func (s *Server) PublicKeyAuth(ctx ssh.Context, key ssh.PublicKey) sshd.AuthStatus {
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

func (s *Server) NextAuthMethodsHandler(ctx ssh.Context) []string {
	return []string{nextAuthMethod}
}

func (s *Server) SFTPHandler(sess ssh.Session) {
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

func (s *Server) LocalPortForwardingCallback(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
	return config.GlobalConfig.EnableLocalPortForward
}

type localForwardChannelData struct {
	DestAddr string
	DestPort uint32

	OriginAddr string
	OriginPort uint32
}

func (s *Server) DirectTCPIPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	localD := localForwardChannelData{}
	if err := gossh.Unmarshal(newChan.ExtraData(), &localD); err != nil {
		_ = newChan.Reject(gossh.ConnectionFailed, "error parsing forward data: "+err.Error())
		return
	}

	if srv.LocalPortForwardingCallback == nil || !srv.LocalPortForwardingCallback(ctx, localD.DestAddr, localD.DestPort) {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}

	if !config.GetConf().EnableIDESupport {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled, ide support not enabled")
		return
	}
	reqId, ok := ctx.Value(ctxID).(string)
	if !ok {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}
	client := s.getIDEClient(reqId)
	if client == nil {
		_ = newChan.Reject(gossh.Prohibited, "port forwarding is disabled, cannot found alive connection")
		return
	}
	dest := net.JoinHostPort(localD.DestAddr, strconv.FormatInt(int64(localD.DestPort), 10))
	dConn, err := client.Dial("tcp", dest)
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
	logger.Infof("User %s start port forwarding from (%s) to (%s)", client.user,
		client, dest)
	defer ch.Close()
	go gossh.DiscardRequests(reqs)
	go func() {
		defer ch.Close()
		defer dConn.Close()
		_, _ = io.Copy(ch, dConn)
	}()
	_, _ = io.Copy(dConn, ch)
	logger.Infof("User %s end port forwarding from (%s) to (%s)", client.user,
		client, dest)
}

func (s *Server) ReversePortForwardingCallback(ctx ssh.Context, destinationHost string, destinationPort uint32) bool {
	return config.GlobalConfig.EnableReversePortForward
}

type remoteForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardSuccess struct {
	BindPort uint32
}

type remoteForwardCancelRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func (s *Server) RequestHandler(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (ok bool, payload []byte) {
	reqId, ok := ctx.Value(ctxID).(string)
	if !ok {
		return false, []byte("port forwarding is disabled")
	}

	switch req.Type {
	case "tcpip-forward":
		var reqPayload remoteForwardRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			// TODO: log parse failure
			return false, []byte{}
		}
		if srv.ReversePortForwardingCallback == nil || !srv.ReversePortForwardingCallback(ctx, reqPayload.BindAddr, reqPayload.BindPort) {
			return false, []byte("port forwarding is disabled")
		}

		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		client := s.getIDEClient(reqId)
		if client == nil {
			user := ctx.Value(auth.ContextKeyUser).(*model.User)
			directReq := ctx.Value(auth.ContextKeyDirectLoginFormat)
			directRequest, ok3 := directReq.(*auth.DirectLoginAssetReq)
			if !ok3 {
				return false, []byte("port forwarding is disabled, must be direct login request")
			}

			var tokenInfo *model.ConnectTokenInfo
			var err error
			if directRequest.IsToken() {
				// connection token 的方式使用 vscode 连接
				tokenInfo = directRequest.Info
				matchedType := tokenInfo.TypeName == model.ConnectAsset
				matchedProtocol := tokenInfo.SystemUserAuthInfo.Protocol == model.ProtocolSSH
				assetSupportedSSH := tokenInfo.Asset.IsSupportProtocol(model.ProtocolSSH)
				if !matchedType || !matchedProtocol || !assetSupportedSSH {
					msg := "not ssh asset connection token"
					logger.Errorf("ide support failed: %s", msg)
					return false, []byte(msg)
				}
			} else {
				tokenInfo, err = s.buildTokenInfo(user, directRequest)
				if err != nil {
					msg := "cannot build connect token"
					logger.Errorf("ide supoort failed, err:%s", err.Error())
					return false, []byte(msg)
				}
			}
			client, err = s.buildIDEClientByTokenInfo(reqId, tokenInfo)
			if err != nil {
				logger.Error(err)
				return false, []byte("port forwarding is disabled, cannot build ide client")
			}
		}

		go func() {
			s.addIDEClient(client)
			defer s.deleteIDEClient(client)

			<-ctx.Done()
			logger.Info("ide client removed, all alive forward will be closed by default")
		}()
		ln, err := client.Listen("tcp", addr)
		if err != nil {
			return false, []byte("port forwarding is disabled, cannot connect peer")
		}

		go func() {
			client.AddForward(addr, ln)
			defer client.RemoveForward(addr)
			<-ctx.Done()
			logger.Info("ide port forward removed")
		}()

		_, destPortStr, _ := net.SplitHostPort(ln.Addr().String())
		destPort, _ := strconv.Atoi(destPortStr)
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					// TODO: log accept failure
					break
				}
				originAddr, orignPortStr, _ := net.SplitHostPort(c.RemoteAddr().String())
				originPort, _ := strconv.Atoi(orignPortStr)
				payload := gossh.Marshal(&remoteForwardChannelData{
					DestAddr:   reqPayload.BindAddr,
					DestPort:   uint32(destPort),
					OriginAddr: originAddr,
					OriginPort: uint32(originPort),
				})
				conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
				go func() {
					ch, reqs, err := conn.OpenChannel(sshd.ChannelForwardedTCPIP, payload)
					if err != nil {
						// TODO: log failure to open channel
						logger.Error(err)
						c.Close()
						return
					}
					go gossh.DiscardRequests(reqs)
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(ch, c)
					}()
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(c, ch)
					}()
				}()
			}
		}()
		return true, gossh.Marshal(&remoteForwardSuccess{uint32(destPort)})

	case "cancel-tcpip-forward":
		reqId, ok := ctx.Value(ctxID).(string)
		if !ok {
			return false, []byte("port forwarding is disabled")
		}
		if !config.GetConf().EnableIDESupport {
			return false, []byte("port forwarding is disabled, ide support not enabled")
		}
		client := s.getIDEClient(reqId)
		if client == nil {
			return false, []byte("port forwarding is disabled, cannot found alive connection")
		}

		var reqPayload remoteForwardCancelRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			// TODO: log parse failure
			return false, []byte{}
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))

		ln := client.GetForward(addr)
		if ln != nil {
			ln.Close()
			client.RemoveForward(addr)
		}

		return true, nil
	default:
		return false, nil
	}
}

func (s *Server) SessionHandler(sess ssh.Session) {
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
			s.ideSupport(sess, user, directReq)
			return
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

	s.ideSupport(sess, user, directReq)
}

func (s *Server) ideSupport(sess ssh.Session, user *model.User, directReq interface{}) {
	if !config.GetConf().EnableIDESupport {
		utils.IgnoreErrWriteWindowTitle(sess, "ide support not enabled.\n")
		return
	}
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
				logger.Errorf("ide support failed: %s", msg)
				return
			}
			s.proxyIDEByTokenInfo(sess, tokenInfo)
			return
		}
		tokenInfo, err := s.buildTokenInfo(user, directRequest)
		if err != nil {
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
		s.proxyIDEByTokenInfo(sess, tokenInfo)
	}
}

func (s *Server) buildTokenInfo(user *model.User, directRequest *auth.DirectLoginAssetReq) (*model.ConnectTokenInfo, error) {
	selectedAssets, err := s.getMatchedAssetsByDirectReq(user, directRequest)
	if err != nil {
		logger.Error(err)
		return nil, err
	}
	if len(selectedAssets) != 1 {
		msg := fmt.Sprintf(i18n.T("Must be unique asset for %s"), directRequest.AssetInfo)
		logger.Error(msg)
		return nil, errors.New(msg)
	}
	asset := selectedAssets[0]
	selectedSysUsers, err := s.getMatchedSystemUsers(user, directRequest, asset)
	if err != nil {
		logger.Error(err)
		return nil, err
	}
	if len(selectedSysUsers) != 1 {
		msg := fmt.Sprintf(i18n.T("Must be unique system user for %s"), directRequest.SysUserInfo)
		logger.Error(msg)
		return nil, errors.New(msg)
	}
	systemUser := selectedSysUsers[0]

	systemUserAuthInfo, err := s.jmsService.GetSystemUserAuthById(systemUser.ID, asset.ID,
		user.ID, user.Username)
	if err != nil {
		logger.Errorf("Get system user auth failed: %s", err)
		return nil, err
	}
	permInfo, err := s.jmsService.ValidateAssetConnectPermission(user.ID,
		asset.ID, systemUser.ID)
	if err != nil {
		logger.Errorf("Get asset Permission info err: %s", err)
		return nil, err
	}
	var domain *model.Domain
	if asset.Domain != "" {
		domainInfo, err := s.jmsService.GetDomainGateways(asset.Domain)
		if err != nil {
			logger.Errorf("Get system user auth failed: %s", err)
			return nil, err
		}
		domain = &domainInfo
	}

	return &model.ConnectTokenInfo{
		User:               user,
		Asset:              &asset,
		SystemUserAuthInfo: &systemUserAuthInfo,
		Domain:             domain,
		ExpiredAt:          permInfo.ExpireAt,
	}, nil
}

func (s *Server) proxyIDEByTokenInfo(sess ssh.Session, tokenInfo *model.ConnectTokenInfo) {
	reqId, ok := sess.Context().Value(ctxID).(string)
	if !ok {
		logger.Error("Not found ctxID")
		utils.IgnoreErrWriteString(sess, "not found ctx id")
		return
	}

	var err error
	client := s.getIDEClient(reqId)
	if client == nil {
		client, err = s.buildIDEClientByTokenInfo(reqId, tokenInfo)
		if err != nil {
			logger.Errorf("build IDE client failed, err:%s\n", err.Error())
			utils.IgnoreErrWriteString(sess, err.Error())
			return
		}
	}

	if len(sess.Command()) != 0 {
		err = s.proxyCommand(sess, client)
	} else {
		err = s.proxyShell(sess, client)
	}

	if err != nil {
		utils.IgnoreErrWriteString(sess, err.Error())
	}
}

func (s *Server) buildIDEClientByTokenInfo(reqId string, tokenInfo *model.ConnectTokenInfo) (*IDEClient, error) {
	asset := tokenInfo.Asset
	systemUserAuthInfo := tokenInfo.SystemUserAuthInfo
	domain := tokenInfo.Domain
	sshAuthOpts := buildSSHClientOptions(asset, systemUserAuthInfo, domain)
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		return nil, err
	}

	perm := model.Permission{Actions: tokenInfo.Actions}
	permInfo := model.ExpireInfo{
		HasPermission: perm.EnableConnect(),
		ExpireAt:      tokenInfo.ExpiredAt,
	}

	return &IDEClient{
		SSHClient:  sshClient,
		reqId:      reqId,
		user:       tokenInfo.User,
		tokenInfo:  tokenInfo,
		expireInfo: &permInfo,
	}, nil
}

func (s *Server) proxyCommand(sess ssh.Session,
	client *IDEClient) error {
	goSess, err := client.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return err
	}
	defer goSess.Close()
	defer client.ReleaseSession(goSess)

	if client.session == nil {
		retSession := model.Session{
			ID:           common.UUID(),
			User:         client.tokenInfo.User.String(),
			SystemUser:   client.tokenInfo.SystemUserAuthInfo.String(),
			LoginFrom:    "ST", // means SSH Terminal
			RemoteAddr:   sess.RemoteAddr().String(),
			Protocol:     client.tokenInfo.SystemUserAuthInfo.Protocol,
			UserID:       client.tokenInfo.User.ID,
			SystemUserID: client.tokenInfo.SystemUserAuthInfo.ID,
			Asset:        client.tokenInfo.Asset.Hostname,
			AssetID:      client.tokenInfo.Asset.ID,
			OrgID:        client.tokenInfo.Asset.OrgID,
		}

		if err := s.jmsService.CreateSession(retSession); err != nil {
			logger.Errorf("Create command session err: %s", err)
			return err
		}

		ctx, cancel := context.WithCancel(sess.Context())
		proxy.AddCommandSession(retSession.ID, cancel)

		go func() {
			defer func() {
				if err := s.jmsService.SessionFinished(retSession.ID, sdkcommon.NewNowUTCTime()); err != nil {
					logger.Errorf("finish session err: %s", err)
				}
				proxy.RemoveCommandSession(retSession.ID)
			}()
			<-ctx.Done()
		}()
		client.session = &retSession
	}

	rawStr := sess.RawCommand()
	if strings.HasPrefix(rawStr, "scp") {
		// since the initialization loggic of vscode remote plugin relies on the scp command,
		// it cannot be disabled
		logger.Warnf("found scp command %s", rawStr)
	}

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
	stdout, err := goSess.StdoutPipe()
	if err != nil {
		logger.Errorf("Get SSH session StdoutPipe failed: %s", err)
		return err
	}
	stderr, err := goSess.StderrPipe()
	if err != nil {
		logger.Errorf("Get SSH session StderrPipe failed: %s", err)
		return err
	}
	reader := io.MultiReader(stdout, stderr)
	go func() {
		var outResult strings.Builder
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
		now := time.Now()

		err := cmdStorage.BulkSave([]*model.Command{&model.Command{
			SessionID:   client.session.ID,
			OrgID:       client.session.OrgID,
			Input:       input,
			Output:      output,
			User:        client.tokenInfo.User.String(),
			Server:      client.tokenInfo.Asset.String(),
			SystemUser:  client.tokenInfo.SystemUserAuthInfo.String(),
			Timestamp:   now.Unix(),
			DateCreated: now.UTC(),
			RiskLevel:   model.NormalLevel,
		}})
		if err != nil {
			logger.Errorf("Create command err: %s", err)
		}
	}()
	err = goSess.Run(rawStr)
	if err != nil {
		logger.Errorf("SSH conn[%s] user(%s) exec command on remote(%s) failed, cmd: %s, err: %s",
			client.reqId, client.user, client, rawStr, err)
		return err
	}
	logger.Infof("SSH conn[%s] user(%s) exec command on remote(%s) successs, cmd: %s",
		client.reqId, client.user, client, rawStr)

	go s.checkExpire(sess, client)

	// the reason for returning immediately here is that
	// exec type requests need to end the session immediately
	return nil
}

func (s *Server) proxyShell(sess ssh.Session, client *IDEClient) error {
	goSess, err := client.AcquireSession()
	if err != nil {
		logger.Errorf("Get SSH session failed: %s", err)
		return err
	}
	defer goSess.Close()
	defer client.ReleaseSession(goSess)
	stdout, err := goSess.StdoutPipe()
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
	logger.Infof("SSH conn[%s] user(%s) request shell on remote(%s) success",
		client.reqId, client.user, client)

	go func() {
		_, _ = io.Copy(stdin, sess)
		logger.Infof("SSH conn[%s] user(%s) request shell on remote(%s) stdin end",
			client.reqId, client.user, client)
	}()
	go func() {
		_, _ = io.Copy(sess, stdout)
		logger.Infof("SSH conn[%s] user(%s) request shell on remote(%s) stdout end",
			client.reqId, client.user, client)
	}()

	// the shell type requests are long-connected, so there is no need to end the session immediately
	return s.checkExpire(sess, client)
}

func (s *Server) checkExpire(sess ssh.Session, client *IDEClient) error {
	oldReq := s.getIDEClient(client.reqId)
	if oldReq != nil {
		// session alreay exist, no need to double check
		return nil
	}
	s.addIDEClient(client)
	defer func() {
		client.Close()
		s.deleteIDEClient(client)
	}()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-sess.Context().Done():
			logger.Infof("SSH conn[%s] user(%s) to remote(%s) session done",
				client.reqId, client.user, client)
			return nil
		case now := <-ticker.C:
			if client.expireInfo.IsExpired(now) {
				logger.Infof("SSH conn[%s] user(%s) to remote(%s) session exit, cause permission expired",
					client.reqId, client.user, client)
				return nil
			}
			logger.Debugf("SSH conn[%s] user(%s) to remote(%s) session still alive",
				client.reqId, client.user, client)
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

func (s *Server) getMatchedAssetsByDirectReq(user *model.User, req *auth.DirectLoginAssetReq) ([]model.Asset, error) {
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

func (s *Server) getMatchedSystemUsers(user *model.User, req *auth.DirectLoginAssetReq,
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
