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
	"time"

	"github.com/jumpserver/koko/pkg/auth"
	gossh "golang.org/x/crypto/ssh"

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

type ConnectionOption func(options *ConnectionOptions)

type ConnectionOptions struct {
	ProtocolType string

	user       *model.User
	systemUser *model.SystemUser

	asset  *model.Asset
	dbApp  *model.DatabaseApplication
	k8sApp *model.K8sApplication
}

func ConnectUser(user *model.User) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.user = user
	}
}

func ConnectProtocolType(protocol string) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.ProtocolType = protocol
	}
}

func ConnectSystemUser(systemUser *model.SystemUser) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.systemUser = systemUser
	}
}

func ConnectAsset(asset *model.Asset) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.asset = asset
	}
}

func ConnectDBApp(dbApp *model.DatabaseApplication) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.dbApp = dbApp
	}
}

func ConnectK8sApp(k8sApp *model.K8sApplication) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.k8sApp = k8sApp
	}
}

func (opts *ConnectionOptions) TerminalTitle() string {
	title := ""
	switch opts.ProtocolType {
	case srvconn.ProtocolTELNET,
		srvconn.ProtocolSSH:
		title = fmt.Sprintf("%s://%s@%s",
			opts.ProtocolType,
			opts.systemUser.Username,
			opts.asset.IP)
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		title = fmt.Sprintf("%s://%s@%s",
			opts.ProtocolType,
			opts.systemUser.Username,
			opts.dbApp.Attrs.Host)
	case srvconn.ProtocolK8s:
		title = fmt.Sprintf("%s+%s",
			opts.ProtocolType,
			opts.k8sApp.Attrs.Cluster)
	}
	return title
}

func (opts *ConnectionOptions) ConnectMsg() string {
	msg := ""
	switch opts.ProtocolType {
	case srvconn.ProtocolTELNET,
		srvconn.ProtocolSSH:
		msg = fmt.Sprintf(i18n.T("Connecting to %s@%s"), opts.systemUser.Name, opts.asset.IP)
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		msg = fmt.Sprintf(i18n.T("Connecting to Database %s"), opts.dbApp)
	case srvconn.ProtocolK8s:
		msg = fmt.Sprintf(i18n.T("Connecting to Kubernetes %s"), opts.k8sApp.Attrs.Cluster)
	}
	return msg
}

var (
	ErrMissClient      = errors.New("the protocol client has not installed")
	ErrUnMatchProtocol = errors.New("the protocols are not matched")
	ErrAPIFailed       = errors.New("api failed")
	ErrPermission      = errors.New("no permission")
	ErrNoAuthInfo      = errors.New("no auth info")
)

/*
	简单校验：
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

	terminalConf, err := jmsService.GetTerminalConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
	}
	filterRules, err := jmsService.GetSystemUserFilterRules(connOpts.systemUser.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
	}
	// 过滤规则排序
	sort.Sort(model.FilterRules(filterRules))
	var (
		apiSession *model.Session

		sysUserAuthInfo *model.SystemUserAuthInfo
		domainGateways  *model.Domain
		expireInfo      *model.ExpireInfo
		platform        *model.Platform
		perms           *model.Permission
	)

	switch connOpts.ProtocolType {
	case srvconn.ProtocolTELNET, srvconn.ProtocolSSH:
		if !connOpts.asset.IsSupportProtocol(connOpts.systemUser.Protocol) {
			msg := i18n.T("System user <%s> and asset <%s> protocol are inconsistent.")
			msg = fmt.Sprintf(msg, connOpts.systemUser.Username, connOpts.asset.Hostname)
			utils.IgnoreErrWriteString(conn, utils.WrapperWarn(msg))
			return nil, fmt.Errorf("%w: %s", ErrUnMatchProtocol, msg)
		}

		authInfo, err := jmsService.GetSystemUserAuthById(connOpts.systemUser.ID, connOpts.asset.ID,
			connOpts.user.ID, connOpts.user.Username)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
		sysUserAuthInfo = &authInfo

		if connOpts.asset.Domain != "" {
			domain, err := jmsService.GetDomainGateways(connOpts.asset.Domain)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
			}
			domainGateways = &domain
		}

		permInfo, err := jmsService.ValidateAssetConnectPermission(connOpts.user.ID,
			connOpts.asset.ID, connOpts.systemUser.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
		expireInfo = &permInfo
		assetPlatform, err2 := jmsService.GetAssetPlatform(connOpts.asset.ID)
		if err2 != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err2)
		}
		// 获取权限校验
		permission, err3 := jmsService.GetPermission(connOpts.user.ID, connOpts.asset.ID, connOpts.systemUser.ID)
		if err3 != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err3)
		}
		perms = &permission
		platform = &assetPlatform
		apiSession = &model.Session{
			ID:           common.UUID(),
			User:         connOpts.user.String(),
			SystemUser:   connOpts.systemUser.String(),
			LoginFrom:    conn.LoginFrom(),
			RemoteAddr:   conn.RemoteAddr(),
			Protocol:     connOpts.systemUser.Protocol,
			UserID:       connOpts.user.ID,
			SystemUserID: connOpts.systemUser.ID,
			Asset:        connOpts.asset.String(),
			AssetID:      connOpts.asset.ID,
			OrgID:        connOpts.asset.OrgID,
		}
	case srvconn.ProtocolK8s:
		if !IsInstalledKubectlClient() {
			msg := i18n.T("%s protocol client not installed.")
			msg = fmt.Sprintf(msg, connOpts.k8sApp.TypeName)
			utils.IgnoreErrWriteString(conn, utils.WrapperWarn(msg))
			logger.Errorf("Conn[%s] %s", conn.ID(), msg)
			return nil, fmt.Errorf("%w: %s", ErrMissClient, connOpts.ProtocolType)
		}
		authInfo, err := jmsService.GetUserApplicationAuthInfo(connOpts.systemUser.ID, connOpts.k8sApp.ID,
			connOpts.user.ID, connOpts.user.Username)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
		sysUserAuthInfo = &authInfo
		if connOpts.k8sApp.Domain != "" {
			domain, err := jmsService.GetDomainGateways(connOpts.k8sApp.Domain)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
			}
			domainGateways = &domain
		}
		permInfo, err := jmsService.ValidateRemoteAppPermission(connOpts.user.ID,
			connOpts.k8sApp.ID, connOpts.systemUser.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
		expireInfo = &permInfo
		apiSession = &model.Session{
			ID:           common.UUID(),
			User:         connOpts.user.String(),
			SystemUser:   connOpts.systemUser.String(),
			LoginFrom:    conn.LoginFrom(),
			RemoteAddr:   conn.RemoteAddr(),
			Protocol:     connOpts.systemUser.Protocol,
			SystemUserID: connOpts.systemUser.ID,
			UserID:       connOpts.user.ID,
			Asset:        connOpts.k8sApp.Name,
			AssetID:      connOpts.k8sApp.ID,
			OrgID:        connOpts.k8sApp.OrgID,
		}
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		if !IsInstalledMysqlClient() {
			msg := i18n.T("Database %s protocol client not installed.")
			msg = fmt.Sprintf(msg, connOpts.dbApp.TypeName)
			utils.IgnoreErrWriteString(conn, utils.WrapperWarn(msg))
			logger.Errorf("Conn[%s] %s", conn.ID(), msg)
			return nil, fmt.Errorf("%w: %s", ErrMissClient, connOpts.ProtocolType)
		}
		authInfo, err := jmsService.GetUserApplicationAuthInfo(connOpts.systemUser.ID, connOpts.dbApp.ID,
			connOpts.user.ID, connOpts.user.Username)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
		sysUserAuthInfo = &authInfo
		if connOpts.dbApp.Domain != "" {
			domain, err := jmsService.GetDomainGateways(connOpts.dbApp.Domain)
			if err != nil {
				return nil, err
			}
			domainGateways = &domain
		}

		expirePermInfo, err := jmsService.ValidateApplicationPermission(connOpts.user.ID, connOpts.dbApp.ID, connOpts.systemUser.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrAPIFailed, err)
		}
		expireInfo = &expirePermInfo
		apiSession = &model.Session{
			ID:           common.UUID(),
			User:         connOpts.user.String(),
			SystemUser:   connOpts.systemUser.String(),
			LoginFrom:    conn.LoginFrom(),
			RemoteAddr:   conn.RemoteAddr(),
			Protocol:     connOpts.systemUser.Protocol,
			UserID:       connOpts.user.ID,
			SystemUserID: connOpts.systemUser.ID,
			Asset:        connOpts.dbApp.Name,
			AssetID:      connOpts.dbApp.ID,
			OrgID:        connOpts.dbApp.OrgID,
		}
	default:
		msg := i18n.T("Terminal only support protocol ssh/telnet, please use web terminal to access")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(conn, msg)
		logger.Errorf("Conn[%s] checking requisite failed: %s", conn.ID(), msg)
		return nil, fmt.Errorf("%w: `%s`", srvconn.ErrUnSupportedProtocol, connOpts.ProtocolType)
	}

	if !expireInfo.HasPermission {
		msg := i18n.T("You don't have permission login %s")
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

		filterRules:    filterRules,
		terminalConf:   &terminalConf,
		domainGateways: domainGateways,
		expireInfo:     expireInfo,
		platform:       platform,
		permActions:    perms,
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

	filterRules    []model.SystemUserFilterRule
	terminalConf   *model.TerminalConfig
	domainGateways *model.Domain
	expireInfo     *model.ExpireInfo
	platform       *model.Platform
	permActions    *model.Permission

	cacheSSHConnection *srvconn.SSHConnection

	CreateSessionCallback    func() error
	ConnectedSuccessCallback func() error
	ConnectedFailedCallback  func(err error) error
	DisConnectedCallback     func() error
}

func (s *Server) CheckPermissionExpired(now time.Time) bool {
	return s.expireInfo.ExpireAt < now.Unix()
}

func (s *Server) ZmodemFileTransferEvent(zinfo *ZFileInfo, status bool) {
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolTELNET, srvconn.ProtocolSSH:
		operate := model.OperateDownload
		switch zinfo.transferType {
		case TypeUpload:
			operate = model.OperateUpload
		case TypeDownload:
			operate = model.OperateDownload
		}
		item := model.FTPLog{
			OrgID:      s.connOpts.asset.OrgID,
			User:       s.connOpts.user.String(),
			Hostname:   s.connOpts.asset.Hostname,
			SystemUser: s.connOpts.systemUser.String(),
			RemoteAddr: s.UserConn.RemoteAddr(),
			Operate:    operate,
			Path:       zinfo.filename,
			DataStart:  modelCommon.NewUTCTime(zinfo.parserTime),
			IsSuccess:  status,
		}
		if err := s.jmsService.CreateFileOperationLog(item); err != nil {
			logger.Errorf("Create zmodem ftp log err: %s", err)
		}
	}
}

func (s *Server) GetFilterParser() ParseEngine {
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolSSH,
		srvconn.ProtocolTELNET, srvconn.ProtocolK8s:
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
		var zParser ZmodemParser
		zParser.setStatus(ZParserStatusNone)
		zParser.fileEventCallback = s.ZmodemFileTransferEvent
		shellParser := Parser{
			id:             s.ID,
			protocolType:   s.connOpts.ProtocolType,
			jmsService:     s.jmsService,
			cmdFilterRules: s.filterRules,
			permAction:     s.permActions,
			enableDownload: enableDownload,
			enableUpload:   enableUpload,
			zmodemParser:   &zParser,
		}
		shellParser.initial()
		return &shellParser
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		dbParser := DBParser{
			id:             s.ID,
			cmdFilterRules: s.filterRules,
		}
		dbParser.initial()
		return &dbParser
	}
	return nil
}

func (s *Server) GetReplayRecorder() *ReplyRecorder {
	r := ReplyRecorder{
		SessionID:  s.ID,
		storage:    NewReplayStorage(s.jmsService, s.terminalConf),
		jmsService: s.jmsService,
	}
	r.initial()
	return &r
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

func (s *Server) GenerateCommandItem(input, output string,
	riskLevel int64, createdDate time.Time) *model.Command {
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolTELNET, srvconn.ProtocolSSH:
		return &model.Command{
			SessionID:   s.ID,
			OrgID:       s.connOpts.asset.OrgID,
			User:        s.connOpts.user.String(),
			Server:      s.connOpts.asset.Hostname,
			SystemUser:  s.connOpts.systemUser.String(),
			Input:       input,
			Output:      output,
			Timestamp:   createdDate.Unix(),
			RiskLevel:   riskLevel,
			DateCreated: createdDate.UTC(),
		}

	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		return &model.Command{
			SessionID:   s.ID,
			OrgID:       s.connOpts.dbApp.OrgID,
			User:        s.connOpts.user.String(),
			Server:      s.connOpts.dbApp.Name,
			SystemUser:  s.connOpts.systemUser.String(),
			Input:       input,
			Output:      output,
			Timestamp:   createdDate.Unix(),
			RiskLevel:   riskLevel,
			DateCreated: createdDate.UTC(),
		}

	case srvconn.ProtocolK8s:
		return &model.Command{
			SessionID: s.ID,
			OrgID:     s.connOpts.k8sApp.OrgID,
			User:      s.connOpts.user.String(),
			Server: fmt.Sprintf("%s(%s)", s.connOpts.k8sApp.Name,
				s.connOpts.k8sApp.Attrs.Cluster),
			SystemUser:  s.connOpts.systemUser.String(),
			Input:       input,
			Output:      output,
			Timestamp:   createdDate.Unix(),
			RiskLevel:   riskLevel,
			DateCreated: createdDate.UTC(),
		}
	}
	return nil
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
	if s.systemUserAuthInfo.Password == "" {
		term := utils.NewTerminal(s.UserConn, "password: ")
		line, err := term.ReadPassword(fmt.Sprintf("%s's password: ", s.systemUserAuthInfo.Username))
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
	switch s.connOpts.ProtocolType {
	case srvconn.ProtocolK8s:
		if s.systemUserAuthInfo.Token == "" {
			msg := utils.WrapperWarn(i18n.T("You get auth token failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return errors.New("no auth token")
		}
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolTELNET:
		if err := s.getUsernameIfNeed(); err != nil {
			msg := utils.WrapperWarn(i18n.T("Get auth username failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return fmt.Errorf("get auth username failed: %s", err)
		}
		if err := s.getAuthPasswordIfNeed(); err != nil {
			msg := utils.WrapperWarn(i18n.T("Get auth password failed"))
			utils.IgnoreErrWriteString(s.UserConn, msg)
			return fmt.Errorf("get auth password failed: %s", err)
		}
	case srvconn.ProtocolSSH:
		if err := s.getUsernameIfNeed(); err != nil {
			msg := utils.WrapperWarn(i18n.T("Get auth username failed"))
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
				msg := utils.WrapperWarn(i18n.T("Get auth password failed"))
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
		return platformMatched && protocolMatched
	}
	return false
}

func (s *Server) getCacheSSHConn() (srvConn *srvconn.SSHConnection, ok bool) {
	keyId := srvconn.MakeReuseSSHClientKey(s.connOpts.user.ID, s.connOpts.asset.ID,
		s.connOpts.systemUser.ID, s.systemUserAuthInfo.Username)
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
	reuseMsg := fmt.Sprintf(i18n.T("Reuse SSH connections (%s@%s) [Number of connections: %d]"),
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
		dstHost, dstPort, err := ParseUrlHostAndPort(s.connOpts.k8sApp.Attrs.Cluster)
		if err != nil {
			return nil, err
		}
		dGateway = &domainGateway{
			domain:  domain,
			dstIP:   dstHost,
			dstPort: dstPort,
		}
	case srvconn.ProtocolMySQL,
		srvconn.ProtocolMariadb:
		dGateway = &domainGateway{
			domain:  domain,
			dstIP:   s.connOpts.dbApp.Attrs.Host,
			dstPort: s.connOpts.dbApp.Attrs.Port,
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnMatchProtocol,
			s.connOpts.ProtocolType)
	}
	return dGateway, nil
}

// getSSHConn 获取ssh连接
func (s *Server) getK8sConConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.K8sCon, err error) {
	clusterServer := s.connOpts.k8sApp.Attrs.Cluster
	if localTunnelAddr != nil {
		originUrl, err := url.Parse(clusterServer)
		if err != nil {
			return nil, err
		}
		clusterServer = ReplaceURLHostAndPort(originUrl, "127.0.0.1", localTunnelAddr.Port)
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

func (s *Server) getMysqlConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.MySQLConn, err error) {
	host := s.connOpts.dbApp.Attrs.Host
	port := s.connOpts.dbApp.Attrs.Port
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewMySQLConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.systemUserAuthInfo.Username),
		srvconn.SqlPassword(s.systemUserAuthInfo.Password),
		srvconn.SqlDBName(s.connOpts.dbApp.Attrs.Database),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getSSHConn() (srvConn *srvconn.SSHConnection, err error) {
	key := srvconn.MakeReuseSSHClientKey(s.connOpts.user.ID, s.connOpts.asset.ID, s.systemUserAuthInfo.ID,
		s.systemUserAuthInfo.Username)
	timeout := config.GlobalConfig.SSHTimeout
	sshAuthOpts := make([]srvconn.SSHClientOption, 0, 6)
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientUsername(s.systemUserAuthInfo.Username))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientHost(s.connOpts.asset.IP))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPort(s.connOpts.asset.ProtocolPort(s.systemUserAuthInfo.Protocol)))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPassword(s.systemUserAuthInfo.Password))
	sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientTimeout(timeout))
	if s.systemUserAuthInfo.PrivateKey != "" {
		// 先使用 password 解析 PrivateKey
		if signer, err1 := gossh.ParsePrivateKeyWithPassphrase([]byte(s.systemUserAuthInfo.PrivateKey),
			[]byte(s.systemUserAuthInfo.Password)); err1 == nil {
			sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
		} else {
			// 如果之前使用password解析失败，则去掉 password, 尝试直接解析 PrivateKey 防止错误的passphrase
			if signer, err1 = gossh.ParsePrivateKey([]byte(s.systemUserAuthInfo.PrivateKey)); err1 == nil {
				sshAuthOpts = append(sshAuthOpts, srvconn.SSHClientPrivateAuth(signer))
			}
		}
	}
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
		logger.Errorf("SSH client(%s) start sftp client session err %s", sshClient, err)
		return nil, err
	}
	pty := s.UserConn.Pty()
	sshConn, err := srvconn.NewSSHConnection(sess, srvconn.SSHCharset(s.platform.Charset),
		srvconn.SSHPtyWin(srvconn.Windows{
			Width:  pty.Window.Width,
			Height: pty.Window.Height,
		}), srvconn.SSHTerm(pty.Term))
	if err != nil {
		_ = sess.Close()
		sshClient.ReleaseSession(sess)
		return nil, err
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
		return s.getMysqlConn(proxyAddr)
	default:
		return nil, ErrUnMatchProtocol
	}
}

func (s *Server) sendConnectingMsg(done chan struct{}) {
	delay := 0.0
	msg := fmt.Sprintf("%s %.1f", s.connOpts.ConnectMsg(), delay)
	utils.IgnoreErrWriteString(s.UserConn, msg)
	for {
		select {
		case <-done:
			return
		default:
			delayS := fmt.Sprintf("%.1f", delay)
			data := strings.Repeat("\x08", len(delayS)) + delayS
			utils.IgnoreErrWriteString(s.UserConn, data)
			time.Sleep(100 * time.Millisecond)
			delay += 0.1
		}
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
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		targetType = model.AppType
		targetId = s.connOpts.dbApp.ID
	case srvconn.ProtocolK8s:
		targetType = model.AppType
		targetId = s.connOpts.k8sApp.ID
	default:
		targetId = s.connOpts.asset.ID
	}
	opts = append(opts, auth.ConfirmWithTargetType(targetType))
	opts = append(opts, auth.ConfirmWithTargetID(targetId))
	srv := auth.NewLoginConfirm(s.jmsService, opts...)
	return validateLoginConfirm(&srv, s.UserConn)
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
		msg := i18n.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(s.UserConn, msg)
		logger.Errorf("Conn[%s] submit session %s to core server err: %s",
			s.UserConn.ID(), s.ID, msg)
		return
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
		case srvconn.ProtocolMySQL, srvconn.ProtocolK8s, srvconn.ProtocolMariadb:
			dGateway, err := s.createAvailableGateWay(s.domainGateways)
			if err != nil {
				msg := i18n.T("Start domain gateway failed %s")
				msg = fmt.Sprintf(msg, err)
				utils.IgnoreErrWriteString(s.UserConn, utils.WrapperWarn(msg))
				logger.Error(msg)
				return
			}
			err = dGateway.Start()
			if err != nil {
				msg := i18n.T("Start domain gateway failed %s")
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
	utils.IgnoreErrWriteWindowTitle(s.UserConn, s.connOpts.TerminalTitle())
	if err = sw.Bridge(s.UserConn, srvCon); err != nil {
		logger.Error(err)
	}
}

func (s *Server) sendConnectErrorMsg(err error) {
	msg := fmt.Sprintf("%s error: %s", s.connOpts.ConnectMsg(),
		ConvertErrorToReadableMsg(err))
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
