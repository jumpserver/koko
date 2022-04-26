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

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	modelCommon "github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
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

/*
	简单校验:
		协议是否支持
		资产协议是否匹配

	API 相关
		1. 获取 系统用户 的 Auth info--> 获取认证信息
		2. 获取 授权权限---> 校验权限
		3. 获取需要的domain---> 网关信息
		4. 获取需要的过滤规则---> 获取命令过滤
		5. 获取当前的终端配置，（录像和命令存储配置)
*/

func NewServer(conn UserConnection, jmsService *service.JMService, opts ...ConnectionOption) (*Server, error) {
	connOpts := &ConnectionOptions{}
	for _, setter := range opts {
		setter(connOpts)
	}
	lang := connOpts.getLang()

	if err := srvconn.IsSupportedProtocol(connOpts.ProtocolType); err != nil {
		logger.Errorf("Conn[%s] checking protocol %s failed: %s", conn.ID(),
			connOpts.ProtocolType, err)
		var errMsg string
		switch {
		case errors.Is(err, srvconn.ErrMySQLClient), errors.Is(err, srvconn.ErrSQLServerClient),
			errors.Is(err, srvconn.ErrRedisClient), errors.Is(err, srvconn.ErrMongoDBClient),
			errors.Is(err, srvconn.ErrPostgreSQLClient), errors.Is(err, srvconn.ErrKubectlClient):
			errMsg = lang.T("%s protocol client not installed.")
			errMsg = fmt.Sprintf(errMsg, connOpts.ProtocolType)
			err = fmt.Errorf("%w: %s", ErrMissClient, err)
		default:
			errMsg = lang.T("Terminal does not support protocol %s, please use web terminal to access")
			errMsg = fmt.Sprintf(errMsg, connOpts.ProtocolType)
			err = fmt.Errorf("%w: %s", ErrUnMatchProtocol, err)
		}
		utils.IgnoreErrWriteString(conn, utils.WrapperWarn(errMsg))
		return nil, err
	}

	var (
		err          error
		filterRules  model.FilterRules
		terminalConf model.TerminalConfig

		apiSession *model.Session

		sysUserAuthInfo   *model.SystemUserAuthInfo
		suSysUserAuthInfo *model.SystemUserAuthInfo
		domainGateways    *model.Domain
		platform          *model.Platform
		perms             *model.Permission

		expireInfo *model.ExpireInfo

		checkConnectPermFunc func() (model.ExpireInfo, error)
	)

	// todo: 后续优化这里，统一授权资源获取。目前这里兼容处理 connection token 方式的连接
	sysUserAuthInfo = connOpts.predefinedSystemUserAuthInfo
	domainGateways = connOpts.predefinedDomain
	filterRules = connOpts.predefinedCmdFilterRules
	perms = connOpts.predefinedPermission

	if perms != nil && connOpts.predefinedExpiredAt > 0 {
		expireInfo = &model.ExpireInfo{
			ExpireAt:      connOpts.predefinedExpiredAt,
			HasPermission: perms.EnableConnect()}
	}

	terminalConf, err = jmsService.GetTerminalConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
	}
	userId := connOpts.user.ID
	sysId := connOpts.systemUser.ID
	var (
		assetId string
		appId   string
	)
	if connOpts.asset != nil {
		assetId = connOpts.asset.ID
	}
	if connOpts.app != nil {
		appId = connOpts.app.ID
	}

	if filterRules == nil {
		filterRules, err = jmsService.GetCommandFilterRules(userId, sysId, assetId, appId)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
	}

	// 过滤规则排序
	sort.Sort(filterRules)

	switch connOpts.ProtocolType {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB, srvconn.ProtocolPostgreSQL,
		srvconn.ProtocolK8s:
		if sysUserAuthInfo == nil {
			authInfo, err2 := jmsService.GetUserApplicationAuthInfo(connOpts.systemUser.ID, connOpts.app.ID,
				connOpts.user.ID, connOpts.user.Username)
			if err2 != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
			}
			sysUserAuthInfo = &authInfo
		}

		if domainGateways == nil && connOpts.app.Domain != "" {
			domain, err2 := jmsService.GetDomainGateways(connOpts.app.Domain)
			if err2 != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
			}
			domainGateways = &domain
		}
		checkConnectPermFunc = func() (model.ExpireInfo, error) {
			return jmsService.ValidateApplicationPermission(connOpts.user.ID,
				connOpts.app.ID, connOpts.systemUser.ID)
		}
		assetName := connOpts.app.Name
		if connOpts.k8sContainer != nil {
			assetName = connOpts.k8sContainer.K8sName(assetName)
		}
		apiSession = &model.Session{
			ID:           common.UUID(),
			User:         connOpts.user.String(),
			SystemUser:   sysUserAuthInfo.String(),
			LoginFrom:    conn.LoginFrom(),
			RemoteAddr:   conn.RemoteAddr(),
			Protocol:     connOpts.systemUser.Protocol,
			UserID:       connOpts.user.ID,
			SystemUserID: connOpts.systemUser.ID,
			Asset:        assetName,
			AssetID:      connOpts.app.ID,
			OrgID:        connOpts.app.OrgID,
		}
	default:
		if !connOpts.asset.IsSupportProtocol(connOpts.systemUser.Protocol) {
			msg := lang.T("System user <%s> and asset <%s> protocol are inconsistent.")
			msg = fmt.Sprintf(msg, connOpts.systemUser.Username, connOpts.asset.Hostname)
			utils.IgnoreErrWriteString(conn, utils.WrapperWarn(msg))
			return nil, fmt.Errorf("%w: %s", ErrUnMatchProtocol, msg)
		}
		if sysUserAuthInfo == nil {
			authInfo, err2 := jmsService.GetSystemUserAuthById(connOpts.systemUser.ID, connOpts.asset.ID,
				connOpts.user.ID, connOpts.user.Username)
			if err2 != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
			}
			sysUserAuthInfo = &authInfo
		}

		if connOpts.systemUser.SuEnabled {
			suSystemUserId := connOpts.systemUser.SuFrom
			suAuthInfo, err2 := jmsService.GetSystemUserAuthById(suSystemUserId, assetId,
				connOpts.user.ID, connOpts.user.Username)
			if err2 != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
			}
			suSysUserAuthInfo = &suAuthInfo
		}
		if domainGateways == nil && connOpts.asset.Domain != "" {
			domain, err2 := jmsService.GetDomainGateways(connOpts.asset.Domain)
			if err2 != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
			}
			domainGateways = &domain
		}
		checkConnectPermFunc = func() (model.ExpireInfo, error) {
			return jmsService.ValidateAssetConnectPermission(connOpts.user.ID,
				connOpts.asset.ID, connOpts.systemUser.ID)
		}

		assetPlatform, err2 := jmsService.GetAssetPlatform(connOpts.asset.ID)
		if err2 != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
		}
		if perms == nil {
			// 获取权限校验
			permission, err3 := jmsService.GetPermission(connOpts.user.ID, connOpts.asset.ID, connOpts.systemUser.ID)
			if err3 != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err3)
			}
			perms = &permission
		}
		platform = &assetPlatform
		apiSession = &model.Session{
			ID:           common.UUID(),
			User:         connOpts.user.String(),
			SystemUser:   sysUserAuthInfo.String(),
			LoginFrom:    conn.LoginFrom(),
			RemoteAddr:   conn.RemoteAddr(),
			Protocol:     connOpts.systemUser.Protocol,
			UserID:       connOpts.user.ID,
			SystemUserID: connOpts.systemUser.ID,
			Asset:        connOpts.asset.String(),
			AssetID:      connOpts.asset.ID,
			OrgID:        connOpts.asset.OrgID,
		}
	}

	if expireInfo == nil {
		expirePerm, err2 := checkConnectPermFunc()
		if err2 != nil {
			logger.Error(err2)
		}
		expireInfo = &expirePerm
	}

	if !expireInfo.HasPermission {
		msg := lang.T("You don't have permission login %s")
		msg = utils.WrapperWarn(fmt.Sprintf(msg, connOpts.TerminalTitle()))
		utils.IgnoreErrWriteString(conn, msg)
		return nil, ErrPermission
	}

	return &Server{
		ID:         apiSession.ID,
		UserConn:   conn,
		jmsService: jmsService,

		connOpts:           connOpts,
		systemUserAuthInfo: sysUserAuthInfo,

		suFromSystemUserAuthInfo: suSysUserAuthInfo,

		filterRules:    filterRules,
		terminalConf:   &terminalConf,
		domainGateways: domainGateways,
		expireInfo:     expireInfo,
		platform:       platform,
		permActions:    perms,
		sessionInfo:    apiSession,
		CreateSessionCallback: func() error {
			apiSession.DateStart = modelCommon.NewNowUTCTime()
			return jmsService.CreateSession(*apiSession)
		},
		ConnectedSuccessCallback: func() error {
			return jmsService.SessionSuccess(apiSession.ID)
		},
		ConnectedFailedCallback: func(err error) error {
			return jmsService.SessionFailed(apiSession.ID, err)
		},
		DisConnectedCallback: func() error {
			return jmsService.SessionDisconnect(apiSession.ID)
		},
	}, nil
}

type Server struct {
	ID         string
	UserConn   UserConnection
	jmsService *service.JMService

	connOpts *ConnectionOptions

	systemUserAuthInfo *model.SystemUserAuthInfo

	suFromSystemUserAuthInfo *model.SystemUserAuthInfo

	filterRules    []model.FilterRule
	terminalConf   *model.TerminalConfig
	domainGateways *model.Domain
	expireInfo     *model.ExpireInfo
	platform       *model.Platform
	permActions    *model.Permission

	sessionInfo *model.Session

	cacheSSHConnection *srvconn.SSHConnection

	CreateSessionCallback    func() error
	ConnectedSuccessCallback func() error
	ConnectedFailedCallback  func(err error) error
	DisConnectedCallback     func() error

	keyboardMode int32

	OnSessionInfo func(info *model.Session)

	loginTicketId string
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
	return s.expireInfo.ExpireAt < now.Unix()
}

func (s *Server) ZmodemFileTransferEvent(zinfo *zmodem.ZFileInfo, status bool) {
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolTELNET, srvconn.ProtocolSSH:
		operate := model.OperateDownload
		switch zinfo.Type() {
		case zmodem.TypeUpload:
			operate = model.OperateUpload
		case zmodem.TypeDownload:
			operate = model.OperateDownload
		}
		item := model.FTPLog{
			OrgID:      s.connOpts.asset.OrgID,
			User:       s.connOpts.user.String(),
			Hostname:   s.connOpts.asset.String(),
			SystemUser: s.systemUserAuthInfo.String(),
			RemoteAddr: s.UserConn.RemoteAddr(),
			Operate:    operate,
			Path:       zinfo.Filename(),
			DateStart:  modelCommon.NewUTCTime(zinfo.Time()),
			IsSuccess:  status,
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
	if s.permActions != nil {
		if s.permActions.EnableDownload() {
			enableDownload = true
		}
		if s.permActions.EnableUpload() {
			enableUpload = true
		}
	}
	zParser := zmodem.New()
	zParser.FileEventCallback = s.ZmodemFileTransferEvent
	parser := Parser{
		id:             s.ID,
		protocolType:   s.connOpts.ProtocolType,
		jmsService:     s.jmsService,
		cmdFilterRules: s.filterRules,
		enableDownload: enableDownload,
		enableUpload:   enableUpload,
		zmodemParser:   zParser,
		i18nLang:       s.connOpts.i18nLang,
		platform:       s.platform,
	}
	parser.initial()
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

func (s *Server) GenerateCommandItem(user, input, output string,
	riskLevel int64, createdDate time.Time) *model.Command {
	var (
		server string
		orgID  string
	)
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolTELNET, srvconn.ProtocolSSH:
		server = s.connOpts.asset.String()
		orgID = s.connOpts.asset.OrgID

	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB, srvconn.ProtocolPostgreSQL,
		srvconn.ProtocolK8s:
		server = s.connOpts.app.Name
		if s.connOpts.k8sContainer != nil {
			server = s.connOpts.k8sContainer.K8sName(server)
		}
		orgID = s.connOpts.app.OrgID
	}
	return &model.Command{
		SessionID:   s.ID,
		OrgID:       orgID,
		Server:      server,
		User:        user,
		SystemUser:  s.systemUserAuthInfo.String(),
		Input:       input,
		Output:      output,
		Timestamp:   createdDate.Unix(),
		RiskLevel:   riskLevel,
		DateCreated: createdDate.UTC(),
	}
}

func (s *Server) getUsernameIfNeed() (err error) {
	if s.systemUserAuthInfo.Username == "" {
		logger.Infof("Conn[%s] need manuel input system user username", s.UserConn.ID())
		var username string
		term := utils.NewTerminal(s.UserConn, "username: ")
		for {
			username, err = term.ReadLine()
			if err != nil {
				return err
			}
			username = strings.TrimSpace(username)
			if username != "" {
				break
			}
		}
		s.systemUserAuthInfo.Username = username
		logger.Infof("Conn[%s] get username from user input: %s", s.UserConn.ID(), username)
	}
	return
}

func (s *Server) getAuthPasswordIfNeed() (err error) {
	var line string
	if s.systemUserAuthInfo.Password == "" {
		term := utils.NewTerminal(s.UserConn, "password: ")
		if s.systemUserAuthInfo.Username != "" {
			line, err = term.ReadPassword(fmt.Sprintf("%s's password: ", s.systemUserAuthInfo.Username))
		} else {
			line, err = term.ReadPassword("password: ")
		}

		if err != nil {
			logger.Errorf("Conn[%s] get password from user err: %s", s.UserConn.ID(), err.Error())
			return err
		}
		s.systemUserAuthInfo.Password = line
		logger.Infof("Conn[%s] get password from user input", s.UserConn.ID())
	}
	return nil
}

func (s *Server) checkRequiredAuth() error {
	lang := s.connOpts.getLang()
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolK8s:
		if s.systemUserAuthInfo.Token == "" {
			msg := utils.WrapperWarn(lang.T("You get auth token failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return errors.New("no auth token")
		}
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolTELNET,
		srvconn.ProtocolSQLServer, srvconn.ProtocolMongoDB, srvconn.ProtocolPostgreSQL:
		if err := s.getUsernameIfNeed(); err != nil {
			msg := utils.WrapperWarn(lang.T("Get auth username failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return fmt.Errorf("get auth username failed: %s", err)
		}
		if err := s.getAuthPasswordIfNeed(); err != nil {
			msg := utils.WrapperWarn(lang.T("Get auth password failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return fmt.Errorf("get auth password failed: %s", err)
		}
	case srvconn.ProtocolRedis:
		if err := s.getAuthPasswordIfNeed(); err != nil {
			msg := utils.WrapperWarn(lang.T("Get auth password failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return fmt.Errorf("get auth password failed: %s", err)
		}
	case srvconn.ProtocolSSH:
		if err := s.getUsernameIfNeed(); err != nil {
			msg := utils.WrapperWarn(lang.T("Get auth username failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return err
		}
		if s.checkReuseSSHClient() {
			if cacheConn, ok := s.getCacheSSHConn(); ok {
				s.cacheSSHConnection = cacheConn
				return nil
			}
			logger.Debugf("Conn[%s] did not found cache ssh client(%s@%s)",
				s.UserConn.ID(), s.connOpts.systemUser.Name, s.connOpts.asset.Hostname)
		}

		if s.systemUserAuthInfo.PrivateKey == "" {
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
		platformMatched := s.connOpts.asset.Platform == linuxPlatform
		protocolMatched := s.connOpts.systemUser.Protocol == model.ProtocolSSH
		notSuSystemUser := !s.connOpts.systemUser.SuEnabled
		return platformMatched && protocolMatched && notSuSystemUser
	}
	return false
}

func (s *Server) getCacheSSHConn() (srvConn *srvconn.SSHConnection, ok bool) {
	lang := s.connOpts.getLang()
	keyId := srvconn.MakeReuseSSHClientKey(s.connOpts.user.ID, s.connOpts.asset.ID,
		s.connOpts.systemUser.ID, s.connOpts.asset.IP, s.systemUserAuthInfo.Username)
	sshClient, ok := srvconn.GetClientFromCache(keyId)
	if !ok {
		return nil, ok
	}
	sess, err := sshClient.AcquireSession()
	if err != nil {
		logger.Errorf("Cache ssh client new session failed: %s", err)
		return nil, false
	}
	pty := s.UserConn.Pty()
	cacheConn, err := srvconn.NewSSHConnection(sess, srvconn.SSHCharset(s.platform.Charset),
		srvconn.SSHPtyWin(srvconn.Windows{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		}), srvconn.SSHTerm(pty.Term))
	if err != nil {
		logger.Errorf("Cache ssh session failed: %s", err)
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		return nil, false
	}
	reuseMsg := fmt.Sprintf(lang.T("Reuse SSH connections (%s@%s) [Number of connections: %d]"),
		s.connOpts.systemUser.Name, s.connOpts.asset.IP, sshClient.RefCount())
	utils.IgnoreErrWriteString(s.UserConn, reuseMsg+"\r\n")
	go func() {
		_ = sess.Wait()
		sshClient.ReleaseSession(sess)
		logger.Infof("Reuse SSH client(%s) shell connection release", sshClient)
	}()
	return cacheConn, true
}

func (s *Server) createAvailableGateWay(domain *model.Domain) (*domainGateway, error) {
	var dGateway *domainGateway
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolK8s:
		dstHost, dstPort, err := ParseUrlHostAndPort(s.connOpts.app.Attrs.Cluster)
		if err != nil {
			return nil, err
		}
		dGateway = &domainGateway{
			domain:  domain,
			dstIP:   dstHost,
			dstPort: dstPort,
		}
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB, srvconn.ProtocolPostgreSQL:
		dGateway = &domainGateway{
			domain:  domain,
			dstIP:   s.connOpts.app.Attrs.Host,
			dstPort: s.connOpts.app.Attrs.Port,
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnMatchProtocol,
			s.connOpts.ProtocolType)
	}
	return dGateway, nil
}

// getSSHConn 获取ssh连接
func (s *Server) getK8sConConn(localTunnelAddr *net.TCPAddr) (srvConn srvconn.ServerConnection, err error) {
	clusterServer := s.connOpts.app.Attrs.Cluster
	if localTunnelAddr != nil {
		originUrl, err := url.Parse(clusterServer)
		if err != nil {
			return nil, err
		}
		clusterServer = ReplaceURLHostAndPort(originUrl, "127.0.0.1", localTunnelAddr.Port)
	}
	if s.connOpts.k8sContainer != nil {
		return s.getContainerConn(clusterServer)
	}
	srvConn, err = srvconn.NewK8sConnection(
		srvconn.K8sToken(s.systemUserAuthInfo.Token),
		srvconn.K8sClusterServer(clusterServer),
		srvconn.K8sUsername(s.systemUserAuthInfo.Username),
		srvconn.K8sSkipTls(true),
		srvconn.K8sPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getContainerConn(clusterServer string) (
	srvConn *srvconn.ContainerConnection, err error) {
	info := s.connOpts.k8sContainer
	token := s.systemUserAuthInfo.Token
	win := srvconn.Windows{
		Width:  s.UserConn.Pty().Window.Width,
		Height: s.UserConn.Pty().Window.Height,
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

func (s *Server) getMySQLConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.MySQLConn, err error) {
	host := s.connOpts.app.Attrs.Host
	port := s.connOpts.app.Attrs.Port
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	mysqlOpts := make([]srvconn.SqlOption, 0, 7)
	mysqlOpts = append(mysqlOpts, srvconn.SqlHost(host))
	mysqlOpts = append(mysqlOpts, srvconn.SqlPort(port))
	mysqlOpts = append(mysqlOpts, srvconn.SqlUsername(s.systemUserAuthInfo.Username))
	mysqlOpts = append(mysqlOpts, srvconn.SqlPassword(s.systemUserAuthInfo.Password))
	mysqlOpts = append(mysqlOpts, srvconn.SqlDBName(s.connOpts.app.Attrs.Database))
	mysqlOpts = append(mysqlOpts, srvconn.SqlPtyWin(srvconn.Windows{
		Width:  s.UserConn.Pty().Window.Width,
		Height: s.UserConn.Pty().Window.Height,
	}))
	if s.connOpts.params != nil && s.connOpts.params.DisableMySQLAutoHash {
		mysqlOpts = append(mysqlOpts, srvconn.MySQLDisableAutoReHash())
	}
	srvConn, err = srvconn.NewMySQLConnection(mysqlOpts...)
	return
}

func (s *Server) getRedisConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.RedisConn, err error) {
	host := s.connOpts.app.Attrs.Host
	port := s.connOpts.app.Attrs.Port
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewRedisConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.systemUserAuthInfo.Username),
		srvconn.SqlPassword(s.systemUserAuthInfo.Password),
		srvconn.SqlDBName(s.connOpts.app.Attrs.Database),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getMongoDBConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.MongoDBConn, err error) {
	host := s.connOpts.app.Attrs.Host
	port := s.connOpts.app.Attrs.Port
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewMongoDBConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.systemUserAuthInfo.Username),
		srvconn.SqlPassword(s.systemUserAuthInfo.Password),
		srvconn.SqlDBName(s.connOpts.app.Attrs.Database),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getSQLServerConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.SQLServerConn, err error) {
	host := s.connOpts.app.Attrs.Host
	port := s.connOpts.app.Attrs.Port
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewSQLServerConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.systemUserAuthInfo.Username),
		srvconn.SqlPassword(s.systemUserAuthInfo.Password),
		srvconn.SqlDBName(s.connOpts.app.Attrs.Database),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getPostgreSQLConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.PostgreSQLConn, err error) {
	host := s.connOpts.app.Attrs.Host
	port := s.connOpts.app.Attrs.Port
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewPostgreSQLConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.systemUserAuthInfo.Username),
		srvconn.SqlPassword(s.systemUserAuthInfo.Password),
		srvconn.SqlDBName(s.connOpts.app.Attrs.Database),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getSSHConn() (srvConn *srvconn.SSHConnection, err error) {
	loginSystemUser := s.systemUserAuthInfo
	if s.suFromSystemUserAuthInfo != nil {
		loginSystemUser = s.suFromSystemUserAuthInfo
	}
	key := srvconn.MakeReuseSSHClientKey(s.connOpts.user.ID, s.connOpts.asset.ID, loginSystemUser.ID,
		s.connOpts.asset.IP, loginSystemUser.Username)
	timeout := config.GlobalConfig.SSHTimeout
	sshAuthOpts := make([]srvconn.SSHClientOption, 0, 6)
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientUsername(loginSystemUser.Username))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientHost(s.connOpts.asset.IP))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPort(s.connOpts.asset.ProtocolPort(loginSystemUser.Protocol)))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPassword(loginSystemUser.Password))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientTimeout(timeout))
	if loginSystemUser.PrivateKey != "" {
		// 先使用 password 解析 PrivateKey
		if signer, err1 := gossh.ParsePrivateKeyWithPassphrase([]byte(loginSystemUser.PrivateKey),
			[]byte(loginSystemUser.Password)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
		} else {
			// 如果之前使用password解析失败，则去掉 password, 尝试直接解析 PrivateKey 防止错误的passphrase
			if signer, err1 = gossh.ParsePrivateKey([]byte(loginSystemUser.PrivateKey)); err1 == nil {
				sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
			}
		}
	}
	password := loginSystemUser.Password
	privateKey := loginSystemUser.PrivateKey
	kb := srvconn.SSHClientKeyboardAuth(func(user, instruction string,
		questions []string, echos []bool) (answers []string, err error) {
		s.setKeyBoardMode()
		termReader := utils.NewTerminal(s.UserConn, "")
		utils.IgnoreErrWriteString(s.UserConn, "\r\n")
		ans := make([]string, len(questions))
		for i := range questions {
			q := questions[i]
			termReader.SetPrompt(questions[i])
			logger.Debugf("Conn[%s] keyboard auth question [ %s ]", s.UserConn.ID(), q)
			if strings.Contains(strings.ToLower(q), "password") {
				if privateKey != "" || password != "" {
					ans[i] = password
					continue
				}
			}
			line, err2 := termReader.ReadLine()
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
	sshConnectOpts := make([]srvconn.SSHOption, 0, 6)
	sshConnectOpts = append(sshConnectOpts, srvconn.SSHCharset(s.platform.Charset))
	sshConnectOpts = append(sshConnectOpts, srvconn.SSHTerm(pty.Term))
	sshConnectOpts = append(sshConnectOpts, srvconn.SSHPtyWin(srvconn.Windows{
		Width:  pty.Window.Width,
		Height: pty.Window.Height,
	}))

	if s.suFromSystemUserAuthInfo != nil {
		/*
			suSystemUserAuthInfo 是 switch user
			systemUserAuthInfo 是最终 su 的登录用户
		*/
		suUsername := s.systemUserAuthInfo.Username
		suPassword := s.systemUserAuthInfo.Password
		suCommand := fmt.Sprintf(srvconn.LinuxSuCommand, suUsername)
		sshConnectOpts = append(sshConnectOpts, srvconn.SSHLoginToSudo(true))
		sshConnectOpts = append(sshConnectOpts, srvconn.SSHSudoCommand(suCommand))
		sshConnectOpts = append(sshConnectOpts, srvconn.SSHSudoUsername(suUsername))
		sshConnectOpts = append(sshConnectOpts, srvconn.SSHSudoPassword(suPassword))
	}
	sshConn, err := srvconn.NewSSHConnection(sess, sshConnectOpts...)
	if err != nil {
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		return nil, err
	}
	if s.suFromSystemUserAuthInfo != nil {
		lang := s.connOpts.getLang()
		msg := fmt.Sprintf(lang.T("Switched to %s"), s.systemUserAuthInfo)
		utils.IgnoreErrWriteString(s.UserConn, "\r\n")
		utils.IgnoreErrWriteString(s.UserConn, msg)
		_, _ = sshConn.Write([]byte("\r"))
		logger.Infof("Conn[%s]: su login from %s to %s", s.UserConn.ID(),
			loginSystemUser, s.systemUserAuthInfo)
	}

	go func() {
		_ = sess.Wait()
		sshClient.ReleaseSession(sess)
		logger.Infof("SSH client(%s) shell connection release", sshClient)
	}()
	return sshConn, nil

}

func (s *Server) getTelnetConn() (srvConn *srvconn.TelnetConnection, err error) {
	telnetOpts := make([]srvconn.TelnetOption, 0, 8)
	timeout := config.GlobalConfig.SSHTimeout
	pty := s.UserConn.Pty()
	cusString := s.terminalConf.TelnetRegex
	if cusString != "" {
		successPattern, err2 := regexp.Compile(cusString)
		if err2 != nil {
			logger.Errorf("Conn[%s] telnet custom regex %s compile err: %s",
				s.UserConn.ID(), cusString, err)
			return nil, err2
		}
		telnetOpts = append(telnetOpts, srvconn.TelnetCustomSuccessPattern(successPattern))
	}

	telnetOpts = append(telnetOpts, srvconn.TelnetHost(s.connOpts.asset.IP))
	telnetOpts = append(telnetOpts, srvconn.TelnetPort(s.connOpts.asset.ProtocolPort(s.systemUserAuthInfo.Protocol)))
	telnetOpts = append(telnetOpts, srvconn.TelnetUsername(s.systemUserAuthInfo.Username))
	telnetOpts = append(telnetOpts, srvconn.TelnetUPassword(s.systemUserAuthInfo.Password))
	telnetOpts = append(telnetOpts, srvconn.TelnetUTimeout(timeout))
	telnetOpts = append(telnetOpts, srvconn.TelnetPtyWin(srvconn.Windows{
		Width:  pty.Window.Width,
		Height: pty.Window.Height,
	}))
	telnetOpts = append(telnetOpts, srvconn.TelnetCharset(s.platform.Charset))
	// 获取网关配置
	proxyArgs := s.getGatewayProxyOptions()
	if proxyArgs != nil {
		telnetOpts = append(telnetOpts, srvconn.TelnetProxyOptions(proxyArgs))
	}
	return srvconn.NewTelnetConnection(telnetOpts...)
}

func (s *Server) getGatewayProxyOptions() []srvconn.SSHClientOptions {
	/*
		兼容 云平台同步资产，配置网域，但网关配置为空的情况。
	*/
	if s.domainGateways != nil && len(s.domainGateways.Gateways) != 0 {
		timeout := config.GlobalConfig.SSHTimeout
		proxyArgs := make([]srvconn.SSHClientOptions, 0, len(s.domainGateways.Gateways))
		for i := range s.domainGateways.Gateways {
			gateway := s.domainGateways.Gateways[i]
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
		return proxyArgs
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
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolSSH:
		return s.getSSHConn()
	case srvconn.ProtocolTELNET:
		return s.getTelnetConn()
	case srvconn.ProtocolK8s:
		return s.getK8sConConn(proxyAddr)
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		return s.getMySQLConn(proxyAddr)
	case srvconn.ProtocolSQLServer:
		return s.getSQLServerConn(proxyAddr)
	case srvconn.ProtocolRedis:
		return s.getRedisConn(proxyAddr)
	case srvconn.ProtocolMongoDB:
		return s.getMongoDBConn(proxyAddr)
	case srvconn.ProtocolPostgreSQL:
		return s.getPostgreSQLConn(proxyAddr)
	default:
		return nil, ErrUnMatchProtocol
	}
}

func (s *Server) sendConnectingMsg(done chan struct{}) {
	delay := 0.0
	msg := fmt.Sprintf("%s  %.1f", s.connOpts.ConnectMsg(), delay)
	utils.IgnoreErrWriteString(s.UserConn, msg)
	var activeFlag bool
	for {
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

func (s *Server) checkLoginConfirm() bool {
	opts := make([]auth.ConfirmOption, 0, 4)
	opts = append(opts, auth.ConfirmWithUser(s.connOpts.user))
	opts = append(opts, auth.ConfirmWithSystemUser(s.systemUserAuthInfo))
	var (
		targetType string
		targetId   string
	)
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB, srvconn.ProtocolPostgreSQL,
		srvconn.ProtocolK8s:
		targetType = model.AppType
		targetId = s.connOpts.app.ID
	default:
		targetId = s.connOpts.asset.ID
	}
	opts = append(opts, auth.ConfirmWithTargetType(targetType))
	opts = append(opts, auth.ConfirmWithTargetID(targetId))
	confirmSrv := auth.NewLoginConfirm(s.jmsService, opts...)
	ok := s.validateLoginConfirm(&confirmSrv, s.UserConn)
	s.loginTicketId = confirmSrv.GetTicketId()
	return ok
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
	if !s.checkLoginConfirm() {
		logger.Errorf("Conn[%s]: check login confirm failed", s.UserConn.ID())
		return
	}
	lang := s.connOpts.getLang()
	ctx, cancel := context.WithCancel(context.Background())
	sw := SwitchSession{
		ID:            s.ID,
		MaxIdleTime:   s.terminalConf.MaxIdleTime,
		keepAliveTime: 60,
		ctx:           ctx,
		cancel:        cancel,
		p:             s,
	}
	if err := s.CreateSessionCallback(); err != nil {
		msg := lang.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(s.UserConn, msg)
		logger.Errorf("Conn[%s] submit session %s to core server err: %s",
			s.UserConn.ID(), s.ID, msg)
		return
	}
	if s.loginTicketId != "" {
		msg := fmt.Sprintf("Conn[%s] create session %s ticket %s relation",
			s.UserConn.ID(), s.ID, s.loginTicketId)
		logger.Debug(msg)
		if err := s.jmsService.CreateSessionTicketRelation(s.sessionInfo.ID, s.loginTicketId); err != nil {
			logger.Errorf("%s err: %s", msg, err)
		}
	}
	AddCommonSwitch(&sw)
	defer RemoveCommonSwitch(&sw)
	defer func() {
		if err := s.DisConnectedCallback(); err != nil {
			logger.Errorf("Conn[%s] update session %s err: %+v", s.UserConn.ID(), s.ID, err)
		}
	}()
	var proxyAddr *net.TCPAddr
	if s.domainGateways != nil && len(s.domainGateways.Gateways) != 0 {
		switch s.connOpts.ProtocolType {
		case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
			srvconn.ProtocolRedis, srvconn.ProtocolMongoDB, srvconn.ProtocolK8s:
			dGateway, err := s.createAvailableGateWay(s.domainGateways)
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
		default:
		}
	}
	srvCon, err := s.getServerConn(proxyAddr)
	if err != nil {
		logger.Error(err)
		s.sendConnectErrorMsg(err)
		if err2 := s.ConnectedFailedCallback(err); err2 != nil {
			logger.Errorf("Conn[%s] update session err: %s", s.UserConn.ID(), err2)
		}
		return
	}
	defer srvCon.Close()

	logger.Infof("Conn[%s] create session %s success", s.UserConn.ID(), s.ID)
	if err2 := s.ConnectedSuccessCallback(); err2 != nil {
		logger.Errorf("Conn[%s] update session %s err: %s", s.UserConn.ID(), s.ID, err2)
	}
	if s.OnSessionInfo != nil {
		go s.OnSessionInfo(s.sessionInfo)
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
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolK8s:
		token := s.systemUserAuthInfo.Token
		if token != "" {
			tokenLen := len(token)
			showLen := tokenLen / 2
			hiddenLen := tokenLen - showLen
			msg2 := fmt.Sprintf("Try token: %s", token[:showLen]+strings.Repeat("*", hiddenLen))
			logger.Error(msg2)
		}
	default:
		password := s.systemUserAuthInfo.Password
		if password != "" {
			passwordLen := len(s.systemUserAuthInfo.Password)
			showLen := passwordLen / 2
			hiddenLen := passwordLen - showLen
			msg2 := fmt.Sprintf("Try password: %s", password[:showLen]+strings.Repeat("*", hiddenLen))
			logger.Error(msg2)
		}
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
