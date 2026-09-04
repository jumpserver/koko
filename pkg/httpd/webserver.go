package httpd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LeeEirc/elfinder"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/lion"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	defaultBufferSize = 1024
	WebsocketErrorf   = "Websocket upgrade err: %s"
)

func normalizeHost(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	host, _, err := net.SplitHostPort(h)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}

func hostMatches(allowedHost, originHost string) bool {
	allowedName := normalizeHost(allowedHost)
	originName := normalizeHost(originHost)
	if allowedName == "" || originName == "" {
		return false
	}
	return allowedName == originName
}

func isInternalHost(host string) bool {
	host = normalizeHost(host)
	if host == "localhost" {
		return true
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

func domainsAllowOrigin(originHost, domains string) bool {
	for _, domain := range strings.Split(domains, ",") {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if domain == "*" || hostMatches(domain, originHost) {
			return true
		}
	}
	return false
}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	// 允许非浏览器，客户端访问
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := normalizeHost(u.Host)
	reqHost := normalizeHost(r.Host)
	if hostMatches(originHost, reqHost) {
		return true
	}
	if isInternalHost(originHost) {
		return true
	}

	return domainsAllowOrigin(originHost, config.GetConf().DOMAINS)
}

var upGrader = websocket.Upgrader{
	ReadBufferSize:  defaultBufferSize,
	WriteBufferSize: defaultBufferSize,
	Subprotocols:    []string{"JMS-KOKO"},
	CheckOrigin:     checkOrigin,
}

func NewServer(
	jmsService *service.JMService,
	lionRuntime *lion.Runtime,
) *Server {
	srv := &Server{broadCaster: NewBroadcaster(), apiClient: jmsService}
	eng := createRouter(jmsService, srv, lionRuntime)
	conf := config.GetConf()
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	srv.Srv = &http.Server{Addr: addr, Handler: eng}
	return srv
}

type Server struct {
	broadCaster *broadcaster
	Srv         *http.Server
	apiClient   *service.JMService
}

func (s *Server) Start() {
	go s.broadCaster.Start()
	logger.Info("Start HTTP Server at ", s.Srv.Addr)
	log.Print(s.Srv.ListenAndServe())
}

func (s *Server) Stop() {
	ctx, cancelFunc := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancelFunc()
	if s.Srv != nil {
		_ = s.Srv.Shutdown(ctx)
	}
}

func (s *Server) SftpHostConnectorView(ctx *gin.Context) {
	var params struct {
		Sid string `form:"sid"`
	}
	switch ctx.Request.Method {
	case http.MethodGet, http.MethodPost:
		if err := ctx.ShouldBind(&params); err != nil {
			logger.Errorf("Invalid elfinder request url %s from ip %s",
				ctx.Request.URL, ctx.ClientIP())
			ctx.String(http.StatusBadRequest, "invalid elfinder request")
			return
		}
	default:
		ctx.AbortWithStatus(http.StatusMethodNotAllowed)
		return
	}
	var userV *UserVolume
	if wsCon := s.broadCaster.GetUserWebsocket(params.Sid); wsCon != nil {
		handler := wsCon.GetHandler()
		switch handler.Name() {
		case WebFolderName:
			userV = handler.(*webFolder).GetVolume()
		}
	}
	if userV == nil {
		logger.Errorf("Ws(%s) already closed request url %s from ip %s",
			params.Sid, ctx.Request.URL, ctx.ClientIP())
		ctx.String(http.StatusBadRequest, "ws already disconnected")
		return
	}
	logger.Infof("Elfinder ws %s connected again.", params.Sid)
	conf := config.GetConf()
	maxSize := common.ConvertSizeToBytes(conf.ZipMaxSize)
	options := map[string]string{
		"ZipMaxSize": strconv.Itoa(maxSize),
		"ZipTmpPath": conf.ZipTmpPath,
	}
	conn := elfinder.NewElFinderConnectorWithOption([]elfinder.Volume{userV}, options)
	conn.ServeHTTP(ctx.Writer, ctx.Request)
}

func (s *Server) ProcessTerminalWebsocket(ctx *gin.Context) {
	userConn, err := s.UpgradeUserWsConn(ctx)
	if err != nil {
		logger.Errorf(WebsocketErrorf, err)
		return
	}
	userConn.envelopeProtocol = true
	s.runTTY(userConn)
}

func (s *Server) ProcessElfinderWebsocket(ctx *gin.Context) {
	userConn, err := s.UpgradeUserWsConn(ctx)
	if err != nil {
		logger.Errorf(WebsocketErrorf, err)
		return
	}
	userConn.handler = &webFolder{
		ws:   userConn,
		done: make(chan struct{}),
	}
	s.broadCaster.EnterUserWebsocket(userConn)
	defer s.broadCaster.LeaveUserWebsocket(userConn)
	userConn.Run()
}

func (s *Server) ProcessSftpWebsocket(ctx *gin.Context) {
	userConn, err := s.UpgradeUserWsConn(ctx)
	if err != nil {
		logger.Errorf(WebsocketErrorf, err)
		return
	}
	userConn.handler = &webSftp{
		ws:   userConn,
		done: make(chan struct{}),
	}
	s.broadCaster.EnterUserWebsocket(userConn)
	defer s.broadCaster.LeaveUserWebsocket(userConn)
	userConn.Run()
}

func (s *Server) UpgradeUserWsConn(ctx *gin.Context) (*UserWebsocket, error) {
	underWsCon, err := upGrader.Upgrade(ctx.Writer, ctx.Request, ctx.Writer.Header())
	if err != nil {
		return nil, err
	}
	wsSocket := ws.NewSocket(underWsCon, ctx.Request)

	apiClient := s.apiClient.Copy()
	langCode := config.GetConf().LanguageCode
	for key, value := range auth.RequestAuthHeaders(ctx.Request) {
		apiClient.SetHeader(key, value)
	}
	if acceptLang := ctx.GetHeader("Accept-Language"); acceptLang != "" {
		apiClient.SetHeader("Accept-Language", acceptLang)
		langCode = ParseAcceptLanguageCode(acceptLang)
	}
	if cookieLang, err2 := ctx.Cookie("django_language"); err2 == nil {
		apiClient.SetCookie("django_language", cookieLang)
		langCode = cookieLang
	}

	//设置 websocket 协议层面对应的ping和pong 处理方法
	underWsCon.SetPingHandler(func(appData string) error {
		logger.Debugf("Websocket ping %s", appData)
		return wsSocket.WritePong([]byte(appData), maxWriteTimeOut)
	})
	underWsCon.SetPongHandler(func(appData string) error {
		logger.Debugf("Websocket pong %s", appData)
		return wsSocket.WritePing([]byte(appData), maxWriteTimeOut)
	})

	userValue := ctx.MustGet(auth.ContextKeyUser)
	currentUser := userValue.(*model.User)
	setting := s.getPublicSetting()
	userConn := &UserWebsocket{
		Uuid:           common.UUID(),
		conn:           wsSocket,
		ctx:            ctx.Copy(),
		messageChannel: make(chan *Message, 10),
		user:           currentUser,
		setting:        &setting,
		apiClient:      apiClient,
		langCode:       langCode,
	}
	return userConn, nil
}

func (s *Server) runTTY(userConn *UserWebsocket) {
	ttyHandler := &tty{
		ws: userConn,
	}
	userConn.handler = ttyHandler
	s.broadCaster.EnterUserWebsocket(userConn)
	defer s.broadCaster.LeaveUserWebsocket(userConn)
	userConn.Run()
}

var upTime = time.Now()

func (s *Server) HealthStatusHandler(ctx *gin.Context) {
	status := make(map[string]interface{})
	now := time.Now()
	status["timestamp"] = now.UTC()
	status["uptime"] = now.Sub(upTime).String()
	ctx.JSON(http.StatusOK, status)
}

func (s *Server) CreateConnectTicket(ctx *gin.Context) {
	var payload struct {
		TokenID string `json:"token_id"`
		OrgID   string `json:"org_id"`
	}

	if err := ctx.ShouldBindJSON(&payload); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"detail": fmt.Sprintf("invalid request body: %s", err),
		})
		return
	}

	userValue := ctx.MustGet(auth.ContextKeyUser)
	currentUser := userValue.(*model.User)
	headers := auth.RequestAuthHeaders(ctx.Request)
	ticket := auth.ConnectTickets.Create(currentUser, headers, payload.TokenID, payload.OrgID)

	ctx.JSON(http.StatusCreated, gin.H{
		"ticket":     ticket.ID,
		"token_id":   ticket.TokenID,
		"org_id":     ticket.OrgID,
		"expires_at": ticket.ExpiresAt.UTC(),
		"expires_in": int(time.Until(ticket.ExpiresAt).Seconds()),
	})
}

func (s *Server) GenerateViewMeta(targetId string) (meta ViewPageMata) {
	meta.ID = targetId
	setting, err := s.apiClient.GetPublicSetting()
	if err != nil {
		logger.Errorf("Get core api public setting err: %s", err)
	}
	meta.IconURL = setting.Interface.Favicon
	return
}

func (s *Server) getPublicSetting() model.PublicSetting {
	setting, err := s.apiClient.GetPublicSetting()
	if err != nil {
		logger.Errorf("Get Public setting err: %s", err)
	}
	return setting
}

func ParseAcceptLanguageCode(language string) string {
	// en,zh-TW;q=0.9,zh-CN;q=0.8,zh;q=0.7
	// 解析出第一个语言代码
	if language == "" {
		return "zh-CN"
	}
	languages := strings.SplitN(language, ";", 2)
	lang := strings.TrimSpace(languages[0])
	languages = strings.SplitN(lang, ",", 2)
	return languages[0]
}
