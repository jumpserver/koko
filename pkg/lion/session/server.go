package session

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver/koko/pkg/lion/gateway"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver-dev/sdk-go/service/panda"
	"github.com/jumpserver-dev/sdk-go/storage"
)

const (
	TypeRDP       = "rdp"
	TypeVNC       = "vnc"
	TypeRemoteApp = "remoteapp"

	connectApplet = "applet"

	connectVirtualAPP = "virtual_app"
)

const loginFrom = "WT"

var (
	ErrAPIService      = errors.New("connect API core err")
	ErrPandaAPIService = errors.New("connect Panda API core err")
	//ErrUnSupportedType     = errors.New("unsupported type")

	ErrUnSupportedProtocol = errors.New("unsupported protocol")
	ErrPermissionDeny      = errors.New("permission deny")
)

type Server struct {
	JmsService *service.JMService

	PandaClient        *panda.Client
	PandaClientFactory func(string) *panda.Client
}

func (s *Server) pandaClientFor(provider *model.VirtualAppProvider) *panda.Client {
	if provider != nil && provider.ServiceURL != "" && s.PandaClientFactory != nil {
		return s.PandaClientFactory(provider.ServiceURL)
	}
	return s.PandaClient
}

func providerSSHTarget(provider *model.VirtualAppProvider) *model.Gateway {
	return &model.Gateway{
		ID:        provider.Host.ID,
		Name:      provider.Name,
		Address:   provider.Host.Address,
		Protocols: provider.Host.Protocols,
		Account:   provider.Account,
	}
}

func pandaAPIForwardAddress(serviceURL string) (string, *url.URL, error) {
	parsed, err := url.Parse(serviceURL)
	if err != nil || parsed.Hostname() == "" {
		return "", nil, fmt.Errorf("invalid Panda service URL %q", serviceURL)
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort("127.0.0.1", port), parsed, nil
}

func (s *Server) startPandaAPIForward(provider *model.VirtualAppProvider) (*gateway.DomainGateway, string, error) {
	dstAddr, parsedURL, err := pandaAPIForwardAddress(provider.ServiceURL)
	if err != nil {
		return nil, "", err
	}
	forwarder := &gateway.DomainGateway{
		DstAddr:         dstAddr,
		SelectedGateway: provider.Gateway,
		Destination:     providerSSHTarget(provider),
	}
	if err = forwarder.Start(); err != nil {
		return nil, "", err
	}
	parsedURL.Host = forwarder.GetListenAddr().String()
	return forwarder, parsedURL.String(), nil
}

func ParseWidthAndHeight(ctx *gin.Context, connectToken *model.ConnectToken) (int, int) {
	var width, height int
	if guacWidth, ok := ctx.GetQuery("GUAC_WIDTH"); ok {
		if widthInt, err := strconv.Atoi(guacWidth); err == nil && widthInt > 0 {
			width = widthInt
		}
	}
	if guacHeight, ok := ctx.GetQuery("GUAC_HEIGHT"); ok {
		if heightInt, err := strconv.Atoi(guacHeight); err == nil && heightInt > 0 {
			height = heightInt
		}
	}
	opts := connectToken.ConnectOptions
	resolution := strings.ToLower(opts.Resolution)
	switch resolution {
	case "":
	case "auto":
	default:
		resolutions := strings.Split(resolution, "x")
		if len(resolutions) == 2 {
			widthStr := resolutions[0]
			heightStr := resolutions[1]
			if widthInt, err1 := strconv.Atoi(widthStr); err1 == nil && widthInt > 0 {
				width = widthInt
			}
			if heightInt, err1 := strconv.Atoi(heightStr); err1 == nil && heightInt > 0 {
				height = heightInt
			}
		}
	}
	return width, height
}

func (s *Server) CreatByToken(ctx *gin.Context, token string) (TunnelSession, error) {
	connectToken, err := s.JmsService.GetConnectTokenInfo(token, false)
	if err != nil {
		msg := err.Error()
		logger.Errorf("Get connect token err: %s", err.Error())
		if connectToken.Error != "" {
			msg = connectToken.Error
		}
		return TunnelSession{}, fmt.Errorf("%w: %s", ErrAPIService, msg)
	}
	cfg, err := s.JmsService.GetTerminalConfig()
	if err != nil {
		return TunnelSession{}, fmt.Errorf("%w: %s", ErrAPIService, err.Error())
	}
	if !connectToken.Actions.EnableConnect() {
		return TunnelSession{}, ErrPermissionDeny
	}
	opts := make([]TunnelOption, 0, 10)
	opts = append(opts, ConnectTokenAuthInfo(&connectToken))
	opts = append(opts, WithProtocol(connectToken.Protocol))
	opts = append(opts, WithUser(&connectToken.User))
	opts = append(opts, WithActions(connectToken.Actions))
	opts = append(opts, WithExpireInfo(connectToken.ExpireAt))
	opts = append(opts, WithAsset(&connectToken.Asset))
	opts = append(opts, WithAccount(&connectToken.Account))
	opts = append(opts, WithPlatform(&connectToken.Platform))
	opts = append(opts, WithGateway(connectToken.Gateway))
	opts = append(opts, WithTerminalConfig(&cfg))
	switch connectToken.ConnectMethod.Type {
	case connectApplet:
		appletOptions, err1 := s.JmsService.GetConnectTokenAppletOption(token)
		if err1 != nil {
			msg := err1.Error()
			logger.Errorf("Get applet option err: %s", err1.Error())
			if appletOptions.Error != "" {
				msg = appletOptions.Error
			}
			return TunnelSession{}, fmt.Errorf("%w: %s", ErrAPIService, msg)
		}
		appletOpt := &appletOptions
		opts = append(opts, WithAppletOption(appletOpt))
		logger.Infof("Connect applet(%s) use host(%s) account (%s)", connectToken.Asset.String(),
			appletOpt.Host.String(), appletOpt.Account.String())
		// 连接发布机，需要使用发布机的网关
		opts = append(opts, WithGateway(appletOptions.Gateway))
		// 替换成 发布机的 platform 信息
		opts = append(opts, WithPlatform(appletOptions.Platform))
	case connectVirtualAPP:
		virtualApp, err1 := s.JmsService.GetConnectTokenVirtualAppOption(token)
		if err1 != nil {
			msg := err1.Error()
			logger.Errorf("Get virtual app err: %s", err1.Error())
			if virtualApp.Error != "" {
				msg = virtualApp.Error
			}
			return TunnelSession{}, fmt.Errorf("%w: %s", ErrAPIService, msg)
		}
		width, height := ParseWidthAndHeight(ctx, &connectToken)
		appOpt := model.VirtualAppOption{
			ImageName:      virtualApp.ImageName,
			ImageProtocol:  virtualApp.ImageProtocol,
			ImagePort:      virtualApp.ImagePort,
			DesktopWidth:   width,
			DesktopHeight:  height,
			ConnectionMode: "",
		}
		if virtualApp.Provider != nil {
			appOpt.ConnectionMode = virtualApp.Provider.ConnectionMode
		}
		var pandaAPIGateway *gateway.DomainGateway
		pandaClient := s.pandaClientFor(virtualApp.Provider)
		if virtualApp.Provider != nil && virtualApp.Provider.ConnectionMode == "ssh" {
			var forwardedURL string
			pandaAPIGateway, forwardedURL, err1 = s.startPandaAPIForward(virtualApp.Provider)
			if err1 != nil {
				return TunnelSession{}, fmt.Errorf("%w: start Panda API SSH forward: %s", ErrPandaAPIService, err1)
			}
			if s.PandaClientFactory == nil {
				pandaAPIGateway.Stop()
				return TunnelSession{}, fmt.Errorf("%w: Panda client factory is not configured", ErrPandaAPIService)
			}
			pandaClient = s.PandaClientFactory(forwardedURL)
		}
		if pandaClient == nil {
			if pandaAPIGateway != nil {
				pandaAPIGateway.Stop()
			}
			return TunnelSession{}, fmt.Errorf("%w: Panda client is not configured", ErrPandaAPIService)
		}
		virtualContainer, err2 := pandaClient.CreateContainer(token, appOpt)
		if err2 != nil {
			if pandaAPIGateway != nil {
				pandaAPIGateway.Stop()
			}
			return TunnelSession{}, fmt.Errorf("%w: %s", ErrPandaAPIService, err2.Error())
		}
		logger.Infof("Create container %s success", virtualContainer.ContainerId)
		opts = append(opts, WithVirtualAppOption(&virtualContainer))
		opts = append(opts, WithVirtualAppClient(pandaClient))
		opts = append(opts, WithVirtualAppAPIGateway(pandaAPIGateway))
		logger.Infof("Connect applet(%s) use virtual app %s", connectToken.Asset.String(),
			virtualContainer.String())
		if virtualApp.Provider != nil && virtualApp.Provider.ConnectionMode == "ssh" {
			opts = append(opts, WithGateway(virtualApp.Provider.Gateway))
			opts = append(opts, WithGatewayTarget(providerSSHTarget(virtualApp.Provider)))
		} else {
			// Legacy and direct providers remain directly reachable by guacd.
			opts = append(opts, WithGateway(nil))
		}

	default:
		if _, err1 := s.JmsService.GetConnectTokenInfo(token, true); err1 != nil {
			logger.Errorf("Try to expire connect token err: %s", err1.Error())
		}
	}
	return s.Create(ctx, opts...)
}

func ConnectTokenAuthInfo(authInfo *model.ConnectToken) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.authInfo = authInfo
	}
}
func WithActions(actions model.Actions) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.Actions = actions
	}
}
func WithExpireInfo(expireInfo model.ExpireInfo) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.ExpireInfo = expireInfo
	}
}

func WithProtocol(protocol string) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.Protocol = protocol
	}
}

func WithAsset(asset *model.Asset) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.Asset = asset
	}
}

func WithAccount(account *model.Account) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.Account = account
	}
}

func WithPlatform(platform *model.Platform) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.Platform = platform
	}
}

func WithGateway(gateway *model.Gateway) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.Gateway = gateway
	}
}

func WithGatewayTarget(target *model.Gateway) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.GatewayTarget = target
	}
}

func WithVirtualAppAPIGateway(apiGateway *gateway.DomainGateway) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.virtualAppAPIGateway = apiGateway
	}
}

func WithTerminalConfig(cfg *model.TerminalConfig) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.TerminalConfig = cfg
	}
}

func WithAppletOption(appletOpt *model.AppletOption) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.appletOpt = appletOpt
	}
}

func WithVirtualAppOption(virtualAppOpt *model.VirtualAppContainer) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.virtualAppOPt = virtualAppOpt
	}
}

func WithVirtualAppClient(client *panda.Client) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.virtualAppClient = client
	}
}

func WithUser(user *model.User) TunnelOption {
	return func(tunnel *tunnelOption) {
		tunnel.User = user
	}
}

type tunnelOption struct {
	Protocol      string
	User          *model.User
	Asset         *model.Asset
	Account       *model.Account
	Platform      *model.Platform
	Domain        *model.Domain
	Gateway       *model.Gateway
	GatewayTarget *model.Gateway
	Actions       model.Actions
	ExpireInfo    model.ExpireInfo

	authInfo             *model.ConnectToken
	TerminalConfig       *model.TerminalConfig
	appletOpt            *model.AppletOption
	virtualAppOPt        *model.VirtualAppContainer
	virtualAppClient     *panda.Client
	virtualAppAPIGateway *gateway.DomainGateway
}

type TunnelOption func(*tunnelOption)

func (s *Server) Create(ctx *gin.Context, opts ...TunnelOption) (sess TunnelSession, err error) {
	opt := &tunnelOption{}
	for _, setter := range opts {
		setter(opt)
	}
	defer func() {
		if err == nil {
			return
		}
		if opt.virtualAppAPIGateway != nil {
			defer opt.virtualAppAPIGateway.Stop()
		}
		if opt.virtualAppOPt != nil && opt.virtualAppClient != nil {
			if err2 := opt.virtualAppClient.ReleaseContainer(opt.virtualAppOPt.ContainerId); err2 != nil {
				logger.Errorf("Release virtual app container after creating session failed: %s", err2)
			}
		}
	}()
	var targetType string
	sessionProtocol := opt.Protocol
	switch opt.authInfo.ConnectMethod.Type {
	case connectApplet, connectVirtualAPP:
		targetType = TypeRemoteApp
	default:
		switch opt.Protocol {
		case TypeRDP:
			targetType = TypeRDP
		case TypeVNC:
			targetType = TypeVNC
		default:
			if opt.appletOpt == nil {
				return TunnelSession{}, fmt.Errorf("%w: %s", ErrUnSupportedProtocol, opt.Protocol)
			}
			targetType = TypeRemoteApp
		}
	}
	sessionAssetName := opt.Asset.String()
	sess, err = s.CreateRDPAndVNCSession(opt)
	if err != nil {
		return TunnelSession{}, err
	}
	perm := opt.Actions.Permission()
	sess.AppletOpts = opt.appletOpt
	sess.VirtualAppOpts = opt.virtualAppOPt
	sess.GatewayTarget = opt.GatewayTarget
	sess.AuthInfo = opt.authInfo
	comment := ""
	if opt.appletOpt != nil {
		sess.RemoteApp = &opt.appletOpt.Applet
		comment = fmt.Sprintf(appletCommentTmpl,
			opt.appletOpt.Host.String(),
			opt.appletOpt.Account.String(),
			opt.appletOpt.Applet.Name)
	}

	sess.User = opt.User
	sess.ExpireInfo = opt.ExpireInfo
	sess.Permission = &perm
	sess.Account = opt.Account
	sess.ActionPerm = NewActionPermission(&perm, targetType, opt.authInfo.ClipboardPolicy)
	jmsSession := model.Session{
		ID:         sess.ID,
		User:       sess.User.String(),
		Asset:      sessionAssetName,
		Account:    sess.Account.String(),
		LoginFrom:  loginFrom,
		RemoteAddr: ctx.ClientIP(),
		Protocol:   sessionProtocol,
		DateStart:  sess.Created,
		OrgID:      sess.Asset.OrgID,
		UserID:     sess.User.ID,
		AssetID:    sess.Asset.ID,
		AccountID:  opt.Account.ID,
		Comment:    comment,
		Type:       model.NORMALType,
	}
	sess.ModelSession = &jmsSession
	sess.ConnectedCallback = s.RegisterConnectedCallback(jmsSession)
	sess.ConnectedFailedCallback = s.RegisterConnectedFailedCallback(jmsSession)
	sess.DisConnectedCallback = s.RegisterDisConnectedCallback(jmsSession)
	sess.ReleaseAppletAccount = func() error {
		if opt.virtualAppAPIGateway != nil {
			defer opt.virtualAppAPIGateway.Stop()
		}
		if opt.appletOpt != nil {
			return s.JmsService.ReleaseAppletAccount(opt.appletOpt.ID)
		}
		if opt.virtualAppOPt != nil {
			if opt.virtualAppClient == nil {
				return errors.New("Panda client is not configured")
			}
			return opt.virtualAppClient.ReleaseContainer(opt.virtualAppOPt.ContainerId)
		}
		return nil

	}
	return
}

func (s *Server) CreateRDPAndVNCSession(opt *tunnelOption) (TunnelSession, error) {
	account := opt.Account
	newSession := TunnelSession{
		ID:             common.UUID(),
		Protocol:       opt.Protocol,
		Created:        common.NewNowUTCTime(),
		User:           opt.User,
		Asset:          opt.Asset,
		Platform:       opt.Platform,
		TerminalConfig: opt.TerminalConfig,
		Gateway:        opt.Gateway,

		DisplayAccount: &model.Account{
			BaseAccount: model.BaseAccount{
				Name:       account.Name,
				Username:   account.Username,
				Secret:     "",
				SecretType: account.SecretType}},
	}
	return newSession, nil
}

func (s *Server) RegisterConnectedCallback(sess model.Session) func() error {
	return func() error {
		_, err := s.JmsService.CreateSession(sess)
		return err
	}
}

func (s *Server) RegisterConnectedSuccessCallback(sess model.Session) func() error {
	return func() error {
		_, err := s.JmsService.SessionSuccess(sess.ID)
		return err
	}
}

func (s *Server) RegisterConnectedFailedCallback(sess model.Session) func(err error) error {
	return func(err error) error {
		_, err1 := s.JmsService.SessionFailed(sess.ID, err)
		return err1
	}
}

func (s *Server) RegisterDisConnectedCallback(sess model.Session) func() error {
	return func() error {
		_, err1 := s.JmsService.SessionDisconnect(sess.ID)
		return err1
	}
}

const ReplayFileNameSuffix = ".replay.gz"

func (s *Server) RecordLifecycleLog(sid string, event model.LifecycleEvent, logObj model.SessionLifecycleLog) {
	if err := s.JmsService.RecordSessionLifecycleLog(sid, event, logObj); err != nil {
		logger.Errorf("Record session %s lifecycle %s log err: %s", sid, event, err)
	}
}

func (s *Server) GetFilterParser(tunnel *TunnelSession) ParseEngine {
	winParser := Parser{
		id:         tunnel.ID,
		jmsService: s.JmsService,
	}
	winParser.initial()
	return &winParser
}

func (s *Server) GetCommandRecorder(tunnel *TunnelSession) *CommandRecorder {
	cmdR := CommandRecorder{
		sessionID:  tunnel.ID,
		storage:    storage.NewCommandStorage(s.JmsService, tunnel.TerminalConfig),
		queue:      make(chan *model.Command, 10),
		closed:     make(chan struct{}),
		jmsService: s.JmsService,
	}
	go cmdR.record()
	return &cmdR
}

func (s *Server) GenerateCommandItem(tunnel *TunnelSession, user, input, output string, item *ExecutedCommand) *model.Command {
	server := tunnel.Asset.String()
	createdDate := item.CreatedDate
	return &model.Command{
		SessionID:   tunnel.ID,
		OrgID:       tunnel.Asset.OrgID,
		Server:      server,
		User:        user,
		Account:     tunnel.Account.String(),
		Input:       input,
		Output:      output,
		Timestamp:   createdDate.Unix(),
		RiskLevel:   int64(item.RiskLevel),
		DateCreated: createdDate.UTC(),
	}
}

func (s *Server) AuditFileOperation(fileLog model.FTPLog) {
	if err := s.JmsService.CreateFileOperationLog(fileLog); err != nil {
		logger.Errorf("Audit file operation err: %s", err)
	}
}

func ValidReplayDirname(dirname string) bool {
	_, err := time.Parse(recordDirTimeFormat, dirname)
	return err == nil
}

const appletCommentTmpl = `
AppletHost: %s
Account: %s
Applet：%s
`
