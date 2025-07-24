package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
	"github.com/jumpserver/koko/pkg/zmodem"
)

var (
	ErrMissClient      = errors.New("the protocol client has not installed")
	ErrUnMatchProtocol = errors.New("the protocols are not matched")
	ErrAPIFailed       = errors.New("api failed")
	ErrPermission      = errors.New("no permission")
	ErrNoAuthInfo      = errors.New("no auth info")
)

const localIP = "127.0.0.1"

func NewServer(conn UserConnection, jmsService *service.JMService, opts ...ConnectionOption) (*Server, error) {
	connOpts := &ConnectionOptions{}
	for _, setter := range opts {
		setter(connOpts)
	}
	lang := connOpts.getLang()
	protocol := connOpts.authInfo.Protocol
	asset := connOpts.authInfo.Asset
	account := connOpts.authInfo.Account
	user := connOpts.authInfo.User
	if err := srvconn.IsSupportedProtocol(protocol); err != nil {
		logger.Errorf("Conn[%s] checking protocol %s failed: %s", conn.ID(),
			protocol, err)
		var errMsg string
		switch {
		case errors.As(err, &srvconn.ErrNoClient{}):
			errMsg = lang.T("%s protocol client not installed.")
			errMsg = fmt.Sprintf(errMsg, protocol)
			err = fmt.Errorf("%w: %s", ErrMissClient, err)
		default:
			errMsg = lang.T("HandleTask does not support protocol %s, please use web terminal to access")
			errMsg = fmt.Sprintf(errMsg, protocol)
			err = fmt.Errorf("%w: %s", ErrUnMatchProtocol, err)
		}
		utils.IgnoreErrWriteString(conn, utils.WrapperWarn(errMsg))
		return nil, err
	}
	if !asset.IsSupportProtocol(protocol) {
		msg := lang.T("Account <%s> and asset <%s> protocol are inconsistent.")
		msg = fmt.Sprintf(msg, account.Username, asset.Address)
		utils.IgnoreErrWriteString(conn, utils.WrapperWarn(msg))
		return nil, fmt.Errorf("%w: %s", ErrUnMatchProtocol, msg)
	}
	terminalConf, err := jmsService.GetTerminalConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
	}
	assetName := asset.String()
	if connOpts.k8sContainer != nil {
		assetName = connOpts.k8sContainer.K8sName(asset.Name)
	}

	apiSession := &model.Session{
		ID:         common.UUID(),
		User:       user.String(),
		Account:    account.String(),
		LoginFrom:  model.LabelField(conn.LoginFrom()),
		RemoteAddr: conn.RemoteAddr(),
		Protocol:   protocol,
		UserID:     user.ID,
		Asset:      assetName,
		AssetID:    asset.ID,
		AccountID:  account.ID,
		OrgID:      connOpts.authInfo.OrgId,
		Type:       model.NORMALType,
		TokenId:    connOpts.authInfo.Id,
		LangCode:   connOpts.i18nLang,
	}

	if !connOpts.authInfo.Actions.EnableConnect() {
		msg := lang.T("You don't have permission login %s")
		msg = utils.WrapperWarn(fmt.Sprintf(msg, connOpts.TerminalTitle()))
		utils.IgnoreErrWriteString(conn, msg)
		return nil, ErrPermission
	}

	return &Server{
		ID:            apiSession.ID,
		UserConn:      conn,
		jmsService:    jmsService,
		connOpts:      connOpts,
		account:       &account,
		suFromAccount: account.SuFrom,
		terminalConf:  &terminalConf,
		gateway:       connOpts.authInfo.Gateway,
		sessionInfo:   apiSession,
		CreateSessionCallback: func() error {
			apiSession.DateStart = common.NewNowUTCTime()
			_, err2 := jmsService.CreateSession(*apiSession)
			return err2
		},
		ConnectedFailedCallback: func(err error) error {
			_, err1 := jmsService.SessionFailed(apiSession.ID, err)
			return err1
		},
		DisConnectedCallback: func() error {
			_, err2 := jmsService.SessionDisconnect(apiSession.ID)
			return err2
		},
	}, nil
}

type Server struct {
	ID         string
	UserConn   UserConnection
	jmsService *service.JMService

	connOpts *ConnectionOptions

	account *model.Account

	suFromAccount *model.BaseAccount

	terminalConf *model.TerminalConfig
	gateway      *model.Gateway

	sessionInfo *model.Session

	cacheSSHConnection *srvconn.SSHConnection

	CreateSessionCallback   func() error
	ConnectedFailedCallback func(err error) error
	DisConnectedCallback    func() error

	keyboardMode int32

	OnSessionInfo func(info *SessionInfo)

	BroadcastEvent func(event *exchange.RoomMessage)
}

type SessionInfo struct {
	Session *model.Session    `json:"session"`
	Perms   *model.Permission `json:"permission"`

	BackspaceAsCtrlH *bool `json:"backspaceAsCtrlH,omitempty"`
	CtrlCAsCtrlZ     bool  `json:"ctrlCAsCtrlZ"`

	ThemeName string `json:"themeName"`
}

func (s *Server) IsKeyboardMode() bool {
	return atomic.LoadInt32(&s.keyboardMode) == 1
}

func (s *Server) setKeyBoardMode() {
	atomic.StoreInt32(&s.keyboardMode, 1)
}

func (s *Server) resetKeyboardMode() {
	atomic.StoreInt32(&s.keyboardMode, 0)
}

func (s *Server) CheckPermissionExpired(now time.Time) bool {
	return s.connOpts.authInfo.ExpireAt.IsExpired(now)
}

func (s *Server) ZmodemFileTransferEvent(zinfo *zmodem.ZFileInfo, status bool) {
	protocol := s.connOpts.authInfo.Protocol
	asset := s.connOpts.authInfo.Asset
	user := s.connOpts.authInfo.User
	switch protocol {
	case srvconn.ProtocolTELNET, srvconn.ProtocolSSH:
		operate := model.OperateDownload
		switch zinfo.Type() {
		case zmodem.TypeUpload:
			operate = model.OperateUpload
		case zmodem.TypeDownload:
			operate = model.OperateDownload
		}
		item := model.FTPLog{
			ID:         common.UUID(),
			OrgID:      asset.OrgID,
			User:       user.String(),
			Asset:      asset.String(),
			Account:    s.account.String(),
			RemoteAddr: s.UserConn.RemoteAddr(),
			Operate:    operate,
			Path:       zinfo.Filename(),
			DateStart:  common.NewUTCTime(zinfo.Time()),
			IsSuccess:  status,
			Session:    s.sessionInfo.ID,
		}
		if err := s.jmsService.CreateFileOperationLog(item); err != nil {
			logger.Errorf("Create zmodem ftp log err: %s", err)
		}
	}
}

func (s *Server) GetFilterParser() *Parser {
	var (
		enableUpload   bool
		enableDownload bool
	)
	actions := s.connOpts.authInfo.Actions
	if actions.EnableDownload() {
		enableDownload = true
	}
	if actions.EnableUpload() {
		enableUpload = true
	}
	zParser := zmodem.New()
	zParser.FileEventCallback = s.ZmodemFileTransferEvent
	protocol := s.connOpts.authInfo.Protocol
	filterRules := s.connOpts.authInfo.CommandFilterACLs
	platform := s.connOpts.authInfo.Platform
	// 过滤规则排序
	sort.Sort(model.CommandACLs(filterRules))
	pty := s.UserConn.Pty()
	parser := Parser{
		id:             s.ID,
		protocolType:   protocol,
		jmsService:     s.jmsService,
		cmdFilterACLs:  filterRules,
		enableDownload: enableDownload,
		enableUpload:   enableUpload,
		zmodemParser:   zParser,
		i18nLang:       s.connOpts.i18nLang,
		platform:       &platform,
	}
	parser.initial(pty.Window.Width, pty.Window.Height)
	return &parser
}

func (s *Server) GetReplayRecorder() *ReplyRecorder {
	pty := s.UserConn.Pty()
	info := &ReplyInfo{
		Width:     pty.Window.Width,
		Height:    pty.Window.Height,
		TimeStamp: time.Now(),
	}
	recorder, err := NewReplayRecord(s.ID, s.jmsService,
		NewReplayStorage(s.jmsService, s.terminalConf),
		info)
	if err != nil {
		logger.Error(err)
	}
	return recorder
}

func (s *Server) GetCommandRecorder() *CommandRecorder {
	cmdR := CommandRecorder{
		sessionID:  s.ID,
		storage:    NewCommandStorage(s.jmsService, s.terminalConf),
		queue:      make(chan *model.Command, 10),
		closed:     make(chan struct{}),
		jmsService: s.jmsService,
	}
	go cmdR.record()
	return &cmdR
}

func (s *Server) GenerateCommandItem(user, input, output string, item *ExecutedCommand) *model.Command {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	server := asset.String()
	switch protocol {
	case srvconn.ProtocolK8s:
		server = asset.Name
		if s.connOpts.k8sContainer != nil {
			server = s.connOpts.k8sContainer.K8sName(server)
		}
	}
	createdDate := item.CreatedDate
	return &model.Command{
		SessionID:   s.ID,
		OrgID:       asset.OrgID,
		Server:      server,
		User:        user,
		Account:     s.account.String(),
		Input:       input,
		Output:      output,
		Timestamp:   createdDate.Unix(),
		RiskLevel:   item.RiskLevel,
		DateCreated: createdDate.UTC(),

		CmdFilterAclId: item.CmdFilterACLId,
		CmdGroupId:     item.CmdGroupId,
	}
}

func (s *Server) getAuthPasswordIfNeed() (err error) {
	var line string
	if s.account.Secret == "" {
		vt := term.NewTerminal(s.UserConn, "password: ")
		line, err = vt.ReadPassword(fmt.Sprintf("%s's password: ", s.account.String()))

		if err != nil {
			logger.Errorf("Conn[%s] get password from user err: %s", s.UserConn.ID(), err.Error())
			return err
		}
		s.account.Secret = line
		logger.Infof("Conn[%s] get password from user input", s.UserConn.ID())
	}
	return nil
}

func (s *Server) checkRequiredAuth() error {
	lang := s.connOpts.getLang()
	protocol := s.connOpts.authInfo.Protocol
	asset := s.connOpts.authInfo.Asset
	loginAccount := s.account
	switch protocol {
	case srvconn.ProtocolK8s:
		if s.account.Secret == "" {
			msg := utils.WrapperWarn(lang.T("You get auth token failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return errors.New("no auth token")
		}
	case srvconn.ProtocolTELNET, srvconn.ProtocolClickHouse,
		srvconn.ProtocolMongoDB,

		srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolSQLServer, srvconn.ProtocolPostgresql,
		srvconn.ProtocolRedis, srvconn.ProtocolOracle:
		if err := s.getAuthPasswordIfNeed(); err != nil {
			msg := utils.WrapperWarn(lang.T("Get auth password failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return fmt.Errorf("get auth password failed: %s", err)
		}
	case srvconn.ProtocolSSH:
		if s.checkReuseSSHClient() {
			if cacheConn, ok := s.getCacheSSHConn(); ok {
				s.cacheSSHConnection = cacheConn
				return nil
			}
			logger.Debugf("Conn[%s] did not found cache ssh client(%s@%s)",
				s.UserConn.ID(), loginAccount.Name, asset.Name)
		}

		if s.account.Secret == "" {
			if err := s.getAuthPasswordIfNeed(); err != nil {
				msg := utils.WrapperWarn(lang.T("Get auth password failed"))
				utils.IgnoreErrWriteString(s.UserConn, msg)
				return err
			}
		}
	default:
		return ErrNoAuthInfo
	}
	return nil
}

const (
	linuxPlatform = "Linux"
)

func (s *Server) checkReuseSSHClient() bool {
	if config.GetConf().ReuseConnection {
		platform := s.connOpts.authInfo.Platform
		protocol := s.connOpts.authInfo.Protocol
		platformMatched := strings.EqualFold(platform.Type.Value, linuxPlatform)
		protocolMatched := protocol == model.ProtocolSSH
		notSuSystemUser := s.suFromAccount == nil
		return platformMatched && protocolMatched && notSuSystemUser
	}
	return false
}

func (s *Server) getCacheSSHConn() (srvConn *srvconn.SSHConnection, ok bool) {
	lang := s.connOpts.getLang()
	asset := s.connOpts.authInfo.Asset
	user := s.connOpts.authInfo.User
	loginAccount := s.account
	key := srvconn.MakeReuseSSHClientKey(user.ID, asset.ID,
		loginAccount.ID, asset.Address, loginAccount.HashId())
	sshClient, ok := srvconn.GetClientFromCache(key)
	if !ok {
		return nil, ok
	}
	sess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Cache ssh client new session failed: %s", err)
		return nil, false
	}
	pty := s.UserConn.Pty()
	charset := s.getCharset()
	cacheConn, err := srvconn.NewSSHConnection(sess, srvconn.SSHCharset(charset),
		srvconn.SSHPtyWin(srvconn.Windows{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		}), srvconn.SSHTerm(pty.Term))
	if err != nil {
		logger.Errorf("Cache ssh session failed: %s", err)
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		srvconn.ReleaseClientCacheKey(key, sshClient)
		return nil, false
	}
	reuseMsg := fmt.Sprintf(lang.T("Reuse SSH connections (%s@%s) [Number of connections: %d]"),
		loginAccount.Name, asset.Address, sshClient.RefCount())
	utils.IgnoreErrWriteString(s.UserConn, reuseMsg+"\r\n")
	go func() {
		_ = sess.Wait()
		sshClient.ReleaseSession(sess)
		logger.Infof("Reuse SSH client(%s) shell connection release", sshClient)
		srvconn.ReleaseClientCacheKey(key, sshClient)
	}()
	return cacheConn, true
}

func (s *Server) createAvailableGateWay() (*domainGateway, error) {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	dstIP := asset.Address
	dstPort := asset.ProtocolPort(protocol)
	if protocol == srvconn.ProtocolK8s {
		dstHost, dstPort1, err := ParseUrlHostAndPort(asset.Address)
		if err != nil {
			return nil, err
		}
		dstIP = dstHost
		dstPort = dstPort1
	}
	dGateway := &domainGateway{
		dstIP:           dstIP,
		dstPort:         dstPort,
		selectedGateway: s.gateway,
	}
	return dGateway, nil
}

// getSSHConn 获取ssh连接
func (s *Server) getK8sConConn(localTunnelAddr *net.TCPAddr) (srvConn srvconn.ServerConnection, err error) {
	asset := s.connOpts.authInfo.Asset
	clusterServer := asset.Address
	if localTunnelAddr != nil {
		originUrl, err1 := url.Parse(clusterServer)
		if err1 != nil {
			return nil, err1
		}
		clusterServer = ReplaceURLHostAndPort(originUrl, localIP, localTunnelAddr.Port)
	}
	if s.connOpts.k8sContainer != nil {
		return s.getContainerConn(clusterServer)
	}
	srvConn, err = srvconn.NewK8sConnection(
		srvconn.K8sToken(s.account.Secret),
		srvconn.K8sClusterServer(clusterServer),
		srvconn.K8sUsername(s.account.Username),
		srvconn.K8sSkipTls(true),
		srvconn.K8sPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
		srvconn.K8sExtraEnvs(map[string]string{
			"K8sName": asset.Name,
		}),
	)
	return
}

func (s *Server) getContainerConn(clusterServer string) (
	srvConn *srvconn.ContainerConnection, err error) {
	info := s.connOpts.k8sContainer
	token := s.account.Secret
	pty := s.UserConn.Pty()
	win := srvconn.Windows{
		Width:  pty.Window.Width,
		Height: pty.Window.Height,
	}
	opts := make([]srvconn.ContainerOption, 0, 5)
	opts = append(opts, srvconn.ContainerHost(clusterServer))
	opts = append(opts, srvconn.ContainerToken(token))
	opts = append(opts, srvconn.ContainerName(info.Container))
	opts = append(opts, srvconn.ContainerPodName(info.PodName))
	opts = append(opts, srvconn.ContainerNamespace(info.Namespace))
	opts = append(opts, srvconn.ContainerSkipTls(true))
	opts = append(opts, srvconn.ContainerPtyWin(win))
	srvConn, err = srvconn.NewContainerConnection(opts...)
	return
}

func (s *Server) getRedisConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.RedisConn, err error) {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	platform := s.connOpts.authInfo.Platform
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	if localTunnelAddr != nil {
		host = localIP
		port = localTunnelAddr.Port
	}
	username := s.account.Username
	isAuthUsername := false
	if platfromProtocol, ok := platform.GetProtocolSetting("redis"); ok {
		protocolSetting := platfromProtocol.GetSetting()
		isAuthUsername = protocolSetting.AuthUsername
	}
	if s.account.IsNull() || !isAuthUsername {
		username = ""
	}
	srvConn, err = srvconn.NewRedisConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(username),
		srvconn.SqlPassword(s.account.Secret),
		srvconn.SqlDBName(asset.SpecInfo.DBName),
		srvconn.SqlUseSSL(asset.SpecInfo.UseSSL),
		srvconn.SqlCaCert(asset.SecretInfo.CaCert),
		srvconn.SqlClientCert(asset.SecretInfo.ClientCert),
		srvconn.SqlCertKey(asset.SecretInfo.ClientKey),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getMongoDBConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.MongoDBConn, err error) {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	if localTunnelAddr != nil {
		host = localIP
		port = localTunnelAddr.Port
	}
	platform := s.connOpts.authInfo.Platform

	authSource := ""
	connectionOpts := ""
	if platfromProtocol, ok := platform.GetProtocolSetting("mongodb"); ok {
		protocolSetting := platfromProtocol.GetSetting()
		authSource = protocolSetting.AuthSource
		connectionOpts = protocolSetting.ConnectionOpts
	}

	srvConn, err = srvconn.NewMongoDBConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.account.Username),
		srvconn.SqlPassword(s.account.Secret),
		srvconn.SqlDBName(asset.SpecInfo.DBName),
		srvconn.SqlUseSSL(asset.SpecInfo.UseSSL),
		srvconn.SqlCaCert(asset.SecretInfo.CaCert),
		srvconn.SqlCertKey(asset.SecretInfo.ClientKey),
		srvconn.SqlAllowInvalidCert(asset.SpecInfo.AllowInvalidCert),
		srvconn.SqlAuthSource(authSource),
		srvconn.SqlConnectionOptions(connectionOpts),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getSSHConn() (srvConn *srvconn.SSHConnection, err error) {
	loginAccount := s.account.GetBaseAccount()
	if s.suFromAccount != nil {
		loginAccount = s.suFromAccount
	}
	platform := s.connOpts.authInfo.Platform
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	user := s.connOpts.authInfo.User
	key := srvconn.MakeReuseSSHClientKey(user.ID, asset.ID,
		loginAccount.ID, asset.Address, loginAccount.HashId())
	timeout := config.GlobalConfig.SSHTimeout
	sshAuthOpts := make([]srvconn.SSHClientOption, 0, 6)
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientUsername(loginAccount.Username))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientHost(asset.Address))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPort(asset.ProtocolPort(protocol)))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientTimeout(timeout))
	if loginAccount.IsSSHKey() {
		if signer, err1 := gossh.ParsePrivateKey([]byte(loginAccount.Secret)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
		}
	} else {
		if !isPlatform(&platform, "MFA") {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPassword(loginAccount.Secret))
		}
	}

	password := loginAccount.Secret
	kb := srvconn.SSHClientKeyboardAuth(func(user, instruction string,
		questions []string, echos []bool) (answers []string, err error) {
		s.setKeyBoardMode()
		vt := term.NewTerminal(s.UserConn, "")
		utils.IgnoreErrWriteString(s.UserConn, "\r\n")
		ans := make([]string, len(questions))
		for i := range questions {
			q := questions[i]
			vt.SetPrompt(questions[i])
			logger.Debugf("Conn[%s] keyboard auth question [ %s ]", s.UserConn.ID(), q)
			if strings.Contains(strings.ToLower(q), "password") {
				if password != "" {
					ans[i] = password
					continue
				}
			}
			line, err2 := vt.ReadLine()
			if err2 != nil {
				logger.Errorf("Conn[%s] keyboard auth read err: %s", s.UserConn.ID(), err2)
			}
			ans[i] = line
		}
		s.resetKeyboardMode()
		return ans, nil
	})
	sshAuthOpts = append(sshAuthOpts, kb)
	// 获取网关配置
	proxyArgs := s.getGatewayProxyOptions()
	if proxyArgs != nil {
		sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientProxyClient(proxyArgs...))
	}
	sshClient, err := srvconn.NewSSHClient(sshAuthOpts...)
	if err != nil {
		logger.Errorf("Get new ssh client err: %s", err)
		return nil, err
	}
	srvconn.AddClientCache(key, sshClient)
	sess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("SSH client(%s) start session err %s", sshClient, err)
		return nil, err
	}

	pty := s.UserConn.Pty()
	charset := s.getCharset()
	sshConnectOpts := make([]srvconn.SSHOption, 0, 6)
	sshConnectOpts = append(sshConnectOpts, srvconn.SSHCharset(charset))
	sshConnectOpts = append(sshConnectOpts, srvconn.SSHTerm(pty.Term))
	sshConnectOpts = append(sshConnectOpts, srvconn.SSHPtyWin(srvconn.Windows{
		Width:  pty.Window.Width,
		Height: pty.Window.Height,
	}))

	if s.suFromAccount != nil {
		/*
			suFromAccount 是 switch user
			account 是最终 su 的登录用户
		*/
		suUsername := s.account.Username
		suPassword := s.account.Secret
		sudoType := srvconn.SuMethodSu
		if platform.SuMethod != nil {
			sudoType = srvconn.NewSuMethodType(platform.SuMethod.Value)
		}
		cfg := srvconn.SuConfig{
			MethodType:   sudoType,
			SudoUsername: suUsername,
			SudoPassword: suPassword,
		}
		sshConnectOpts = append(sshConnectOpts, srvconn.SSHSudoConfig(&cfg))
	}
	sshConn, err := srvconn.NewSSHConnection(sess, sshConnectOpts...)
	if err != nil {
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		srvconn.ReleaseClientCacheKey(key, sshClient)
		return nil, err
	}
	if s.suFromAccount != nil {
		lang := s.connOpts.getLang()
		msg := fmt.Sprintf(lang.T("Switched to %s"), s.account)
		utils.IgnoreErrWriteString(s.UserConn, "\r\n")
		utils.IgnoreErrWriteString(s.UserConn, msg)
		_, _ = sshConn.Write([]byte("\r"))
		logger.Infof("Conn[%s]: su login from %s to %s", s.UserConn.ID(),
			loginAccount, s.account)
	}
	go func() {
		_ = sess.Wait()
		sshClient.ReleaseSession(sess)
		logger.Infof("SSH client(%s) shell connection release", sshClient)
		srvconn.ReleaseClientCacheKey(key, sshClient)
	}()
	return sshConn, nil
}

func (s *Server) getTelnetConn() (srvConn *srvconn.TelnetConnection, err error) {
	loginAccount := s.account.GetBaseAccount()
	if s.suFromAccount != nil {
		loginAccount = s.suFromAccount
	}
	telnetOpts := make([]srvconn.TelnetOption, 0, 8)
	timeout := config.GlobalConfig.SSHTimeout
	pty := s.UserConn.Pty()
	protocol := s.connOpts.authInfo.Protocol
	asset := s.connOpts.authInfo.Asset
	platform := s.connOpts.authInfo.Platform

	usernamePrompt := ""
	passwordPrompt := ""
	successPrompt := ""
	if platfromProtocol, ok := platform.GetProtocolSetting(protocol); ok {
		protocolSetting := platfromProtocol.GetSetting()
		usernamePrompt = strings.TrimSpace(protocolSetting.TelnetUsernamePrompt)
		passwordPrompt = strings.TrimSpace(protocolSetting.TelnetPasswordPrompt)
		successPrompt = strings.TrimSpace(protocolSetting.TelnetSuccessPrompt)
	}

	if usernamePrompt != "" {
		usernamePattern, err1 := regexp.Compile(usernamePrompt)
		if err1 != nil {
			logger.Errorf("Conn[%s] telnet username regex %s compile err: %s",
				s.UserConn.ID(), usernamePrompt, err)
			return nil, err
		}
		telnetOpts = append(telnetOpts, srvconn.TelnetCustomUsernamePattern(usernamePattern))
	}
	if passwordPrompt != "" {
		passwordPattern, err1 := regexp.Compile(passwordPrompt)
		if err1 != nil {
			logger.Errorf("Conn[%s] telnet password regex %s compile err: %s",
				s.UserConn.ID(), passwordPrompt, err)
			return nil, err
		}
		telnetOpts = append(telnetOpts, srvconn.TelnetCustomPasswordPattern(passwordPattern))
	}
	if successPrompt != "" {
		successPattern, err1 := regexp.Compile(successPrompt)
		if err1 != nil {
			logger.Errorf("Conn[%s] telnet success regex %s compile err: %s",
				s.UserConn.ID(), successPrompt, err)
			return nil, err
		}
		telnetOpts = append(telnetOpts, srvconn.TelnetCustomSuccessPattern(successPattern))
	}

	telnetOpts = append(telnetOpts, srvconn.TelnetHost(asset.Address))
	telnetOpts = append(telnetOpts, srvconn.TelnetPort(asset.ProtocolPort(protocol)))
	telnetOpts = append(telnetOpts, srvconn.TelnetUsername(loginAccount.Username))
	telnetOpts = append(telnetOpts, srvconn.TelnetUPassword(loginAccount.Secret))
	telnetOpts = append(telnetOpts, srvconn.TelnetUTimeout(timeout))
	telnetOpts = append(telnetOpts, srvconn.TelnetPtyWin(srvconn.Windows{
		Width:  pty.Window.Width,
		Height: pty.Window.Height,
	}))
	charset := s.getCharset()
	telnetOpts = append(telnetOpts, srvconn.TelnetCharset(charset))
	// 获取网关配置
	proxyArgs := s.getGatewayProxyOptions()
	if proxyArgs != nil {
		telnetOpts = append(telnetOpts, srvconn.TelnetProxyOptions(proxyArgs))
	}
	if s.suFromAccount != nil {
		suUsername := s.account.Username
		suPassword := s.account.Secret
		sudoType := srvconn.SuMethodSu
		if platform.SuMethod != nil {
			sudoType = srvconn.NewSuMethodType(platform.SuMethod.Value)
		}
		cfg := srvconn.SuConfig{
			MethodType:   sudoType,
			SudoUsername: suUsername,
			SudoPassword: suPassword,
		}
		telnetOpts = append(telnetOpts, srvconn.TelnetSuConfig(&cfg))
	}
	tcon, err := srvconn.NewTelnetConnection(telnetOpts...)
	if err != nil {
		return tcon, err
	}
	if s.suFromAccount != nil {
		lang := s.connOpts.getLang()
		msg := fmt.Sprintf(lang.T("Switched to %s"), s.account)
		utils.IgnoreErrWriteString(s.UserConn, "\r\n")
		utils.IgnoreErrWriteString(s.UserConn, msg)
		_, _ = tcon.Write([]byte("\r"))
		logger.Infof("Conn[%s]: su login from %s to %s", s.UserConn.ID(),
			loginAccount, s.account)
	}
	return tcon, nil
}

func (s *Server) getGatewayProxyOptions() []srvconn.SSHClientOptions {
	// 仅有一个网关的情况
	if s.gateway != nil {
		timeout := config.GlobalConfig.SSHTimeout
		port := s.gateway.Protocols.GetProtocolPort(model.ProtocolSSH)
		loginAccount := s.gateway.Account
		proxyArg := srvconn.SSHClientOptions{
			Host:     s.gateway.Address,
			Port:     strconv.Itoa(port),
			Username: s.gateway.Account.Username,
			Timeout:  timeout,
		}
		if loginAccount.IsSSHKey() {
			proxyArg.PrivateKey = s.gateway.Account.Secret
		} else {
			proxyArg.Password = s.gateway.Account.Secret
		}
		return []srvconn.SSHClientOptions{proxyArg}
	}
	return nil
}

func (s *Server) getServerConn(proxyAddr *net.TCPAddr) (srvconn.ServerConnection, error) {
	if s.cacheSSHConnection != nil {
		return s.cacheSSHConnection, nil
	}
	done := make(chan struct{})
	defer func() {
		utils.IgnoreErrWriteString(s.UserConn, "\r\n")
		close(done)
	}()
	go s.sendConnectingMsg(done)
	protocol := s.connOpts.authInfo.Protocol
	switch protocol {
	case srvconn.ProtocolSSH:
		return s.getSSHConn()
	case srvconn.ProtocolTELNET:
		return s.getTelnetConn()
	case srvconn.ProtocolK8s:
		return s.getK8sConConn(proxyAddr)
	case srvconn.ProtocolRedis:
		return s.getRedisConn(proxyAddr)
	case srvconn.ProtocolMongoDB:
		return s.getMongoDBConn(proxyAddr)
	case srvconn.ProtocolMySQL,
		srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql,
		srvconn.ProtocolSQLServer,
		srvconn.ProtocolClickHouse,
		srvconn.ProtocolOracle:
		return s.getUSQLConn(proxyAddr)
	default:
		return nil, ErrUnMatchProtocol
	}
}

func (s *Server) sendConnectingMsg(done chan struct{}) {
	delay := 0.0
	maxDelay := 5 * 60.0 // 最多执行五分钟
	msg := fmt.Sprintf("%s  %.1f", s.connOpts.ConnectMsg(), delay)
	utils.IgnoreErrWriteString(s.UserConn, msg)
	var activeFlag bool
	for delay < maxDelay {
		select {
		case <-done:
			return
		default:
			if s.IsKeyboardMode() {
				activeFlag = true
				break
			}
			if activeFlag {
				utils.IgnoreErrWriteString(s.UserConn, utils.CharClear)
				msg = fmt.Sprintf("%s  %.1f", s.connOpts.ConnectMsg(), delay)
				utils.IgnoreErrWriteString(s.UserConn, msg)
				activeFlag = false
				break
			}
			delayS := fmt.Sprintf("%.1f", delay)
			data := strings.Repeat("\x08", len(delayS)) + delayS
			utils.IgnoreErrWriteString(s.UserConn, data)
		}
		time.Sleep(100 * time.Millisecond)
		delay += 0.1
	}
}

func (s *Server) getCharset() string {
	platform := s.connOpts.authInfo.Platform
	tokenConnOpts := s.connOpts.authInfo.ConnectOptions
	charset := platform.Charset.Value
	if tokenConnOpts.Charset != nil {
		useCharset := strings.ToLower(*tokenConnOpts.Charset)
		logger.Debugf("Conn[%s] set charset %s", s.UserConn.ID(), useCharset)
		switch useCharset {
		case "utf-8", "utf8":
			charset = common.UTF8
		case "gbk":
			charset = common.GBK
		case "gb2312":
			charset = common.GB2312
		case "ios-8859-1", "ascii":
			charset = common.ISOLatin1
		default:
		}
	}
	return charset
}

func (s *Server) Proxy() {
	if err := s.checkRequiredAuth(); err != nil {
		logger.Errorf("Conn[%s]: check basic auth failed: %s", s.UserConn.ID(), err)
		return
	}
	defer func() {
		if s.cacheSSHConnection != nil {
			_ = s.cacheSSHConnection.Close()
		}
	}()
	lang := s.connOpts.getLang()
	ctx, cancel := context.WithCancel(context.Background())
	maxIdleTime := s.terminalConf.MaxIdleTime
	maxSessionTime := time.Now().Add(time.Duration(s.terminalConf.MaxSessionTime) * time.Hour)
	sw := SwitchSession{
		ID:            s.ID,
		MaxIdleTime:   maxIdleTime,
		keepAliveTime: 60,
		ctx:           ctx,
		cancel:        cancel,
		p:             s,
		notifyMsgChan: make(chan *exchange.RoomMessage, 1),

		MaxSessionTime: maxSessionTime,
	}
	if err := s.CreateSessionCallback(); err != nil {
		msg := lang.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(s.UserConn, msg)
		logger.Errorf("Conn[%s] submit session %s to core server err: %s %s",
			s.UserConn.ID(), s.ID, msg, err)
		return
	}
	if s.connOpts.authInfo.Ticket != nil {
		reviewTicketId := s.connOpts.authInfo.Ticket.ID
		msg := fmt.Sprintf("Conn[%s] create session %s ticket %s relation",
			s.UserConn.ID(), s.ID, reviewTicketId)
		logger.Info(msg)
		if err := s.jmsService.CreateSessionTicketRelation(s.sessionInfo.ID, reviewTicketId); err != nil {
			logger.Errorf("%s err: %s", msg, err)
		}
	}
	if s.connOpts.authInfo.FaceMonitorToken != "" {
		faceMonitorToken := s.connOpts.authInfo.FaceMonitorToken
		faceReq := service.JoinFaceMonitorRequest{
			FaceMonitorToken: faceMonitorToken,
			SessionId:        s.sessionInfo.ID,
		}
		logger.Infof("Conn[%s] join face monitor %s", s.UserConn.ID(), faceMonitorToken)
		if err := s.jmsService.JoinFaceMonitor(faceReq); err != nil {
			logger.Errorf("Conn[%s] join face monitor err: %s", s.UserConn.ID(), err)
		}
	}

	traceSession := session.NewSession(sw.p.sessionInfo, func(task *model.TerminalTask) error {
		switch task.Name {
		case model.TaskKillSession:
			sw.Terminate(task.Kwargs.TerminatedBy)
		case model.TaskLockSession:
			sw.PauseOperation(task.Kwargs.CreatedByUser)
		case model.TaskUnlockSession:
			sw.ResumeOperation(task.Kwargs.CreatedByUser)
		case model.TaskPermExpired:
			sw.PermBecomeExpired(task.Name, task.Args)
		case model.TaskPermValid:
			sw.PermBecomeValid(task.Name, task.Args)
		default:
			return fmt.Errorf("ssh session unknown task %s", task.Name)
		}
		return nil
	})
	session.AddSession(traceSession)
	defer session.RemoveSession(traceSession)
	defer func() {
		if err := s.DisConnectedCallback(); err != nil {
			logger.Errorf("Conn[%s] update session %s err: %+v", s.UserConn.ID(), s.ID, err)
		}
	}()
	var proxyAddr *net.TCPAddr
	if s.gateway != nil {
		protocol := s.connOpts.authInfo.Protocol
		switch protocol {
		case srvconn.ProtocolSSH, srvconn.ProtocolTELNET:
			// ssh 和 telnet 协议不需要本地启动代理
		default:
			dGateway, err := s.createAvailableGateWay()
			if err != nil {
				msg := lang.T("Start domain gateway failed %s")
				msg = fmt.Sprintf(msg, err)
				utils.IgnoreErrWriteString(s.UserConn, utils.WrapperWarn(msg))
				logger.Error(msg)
				return
			}
			err = dGateway.Start()
			if err != nil {
				msg := lang.T("Start domain gateway failed %s")
				msg = fmt.Sprintf(msg, err)
				utils.IgnoreErrWriteString(s.UserConn, utils.WrapperWarn(msg))
				logger.Error(msg)
				return
			}
			defer dGateway.Stop()
			proxyAddr = dGateway.GetListenAddr()
		}
	}
	srvCon, err := s.getServerConn(proxyAddr)
	if err != nil {
		logger.Error(err)
		s.sendConnectErrorMsg(err)
		if err2 := s.ConnectedFailedCallback(err); err2 != nil {
			logger.Errorf("Conn[%s] update session err: %s", s.UserConn.ID(), err2)
		}
		errLog := model.SessionLifecycleLog{Reason: err.Error()}
		if err1 := s.jmsService.RecordSessionLifecycleLog(s.sessionInfo.ID, model.AssetConnectFinished,
			errLog); err1 != nil {
			logger.Errorf("Conn[%s] record session activity log err: %s", s.UserConn.ID(), err1)
		}
		return
	}
	defer srvCon.Close()
	if err1 := s.jmsService.RecordSessionLifecycleLog(s.sessionInfo.ID, model.AssetConnectSuccess,
		model.EmptyLifecycleLog); err1 != nil {
		logger.Errorf("Conn[%s] record session activity log err: %s", s.UserConn.ID(), err1)
	}

	logger.Infof("Conn[%s] create session %s success", s.UserConn.ID(), s.ID)
	if s.OnSessionInfo != nil {
		actions := s.connOpts.authInfo.Actions
		tokenConnOpts := s.connOpts.authInfo.ConnectOptions
		ctrlCAsCtrlZ := false
		isK8s := s.connOpts.authInfo.Protocol == srvconn.ProtocolK8s
		isNotPod := s.connOpts.k8sContainer == nil
		if isK8s && isNotPod {
			ctrlCAsCtrlZ = true
		}
		perm := actions.Permission()
		info := SessionInfo{
			Session: s.sessionInfo,
			Perms:   &perm,

			BackspaceAsCtrlH: tokenConnOpts.BackspaceAsCtrlH,
			CtrlCAsCtrlZ:     ctrlCAsCtrlZ,
			ThemeName:        tokenConnOpts.TerminalThemeName,
		}
		go s.OnSessionInfo(&info)
	}
	utils.IgnoreErrWriteWindowTitle(s.UserConn, s.connOpts.TerminalTitle())
	if err = sw.Bridge(s.UserConn, srvCon); err != nil {
		logger.Error(err)
	}
}

func (s *Server) sendConnectErrorMsg(err error) {
	msg := fmt.Sprintf("%s error: %s", s.connOpts.ConnectMsg(),
		s.ConvertErrorToReadableMsg(err))
	utils.IgnoreErrWriteString(s.UserConn, msg)
	utils.IgnoreErrWriteString(s.UserConn, utils.CharNewLine)
	logger.Error(msg)
	protocol := s.connOpts.authInfo.Protocol
	password := s.account.Secret
	if password != "" {
		passwordLen := len(s.account.Secret)
		showLen := passwordLen / 2
		hiddenLen := passwordLen - showLen
		var msg2 string
		if protocol == srvconn.ProtocolK8s {
			msg2 = fmt.Sprintf("Try token: %s", password[:showLen]+strings.Repeat("*", hiddenLen))
		} else {
			msg2 = fmt.Sprintf("Try password: %s", password[:showLen]+strings.Repeat("*", hiddenLen))
		}
		logger.Error(msg2)
	}

}

func ParseUrlHostAndPort(clusterAddr string) (host string, port int, err error) {
	clusterUrl, err := url.Parse(clusterAddr)
	if err != nil {
		return "", 0, err
	}
	// URL host 是包含port的结果
	hostAndPort := strings.Split(clusterUrl.Host, ":")
	var (
		dstHost string
		dstPort int
	)
	dstHost = hostAndPort[0]
	switch len(hostAndPort) {
	case 2:
		dstPort, err = strconv.Atoi(hostAndPort[1])
		if err != nil {
			return "", 0, fmt.Errorf("%w: %s", ErrInvalidPort, err)
		}
	default:
		switch clusterUrl.Scheme {
		case "https":
			dstPort = 443
		default:
			dstPort = 80
		}
	}
	return dstHost, dstPort, nil
}

var ErrInvalidPort = errors.New("invalid port")
