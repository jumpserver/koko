package httpd

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LeeEirc/elfinder"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

const (
	defaultBufferSize = 1024
)

var upGrader = websocket.Upgrader{
	ReadBufferSize:  defaultBufferSize,
	WriteBufferSize: defaultBufferSize,
	Subprotocols:    []string{"JMS-KOKO"},
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewServer() *server {
	return &server{
		broadCaster: NewBroadcaster(),
	}
}

type server struct {
	broadCaster *broadcaster
	eng         *gin.Engine
	srv         *http.Server
}

func (s *server) Start() {
	conf := config.GetConf()
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	eng := registerHandlers(s)
	srv := &http.Server{
		Addr:    addr,
		Handler: eng,
	}
	s.eng = eng
	s.srv = srv
	go s.broadCaster.Start()
	logger.Info("Start HTTP server at ", addr)
	log.Print(s.srv.ListenAndServe())
}

func (s *server) Stop() {
	ctx, cancelFunc := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancelFunc()
	if s.srv != nil {
		_ = s.srv.Shutdown(ctx)
	}
}

func (s *server) middleSessionAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if s.checkSessionValid(ctx) {
			ctx.Next()
			return
		}
		loginUrl := fmt.Sprintf("/core/auth/login/?next=%s", url.QueryEscape(ctx.Request.URL.RequestURI()))
		ctx.Redirect(http.StatusFound, loginUrl)
		ctx.Abort()
	}
}

func (s *server) middleDebugAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		host, _, _ := net.SplitHostPort(c.Request.Host)
		switch host {
		case "127.0.0.1", "localhost":
			return
		default:
			_ = c.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid host %s", c.Request.Host))
			return
		}
	}
}

func (s *server) checkSessionValid(ctx *gin.Context) bool {
	var (
		csrfToken string
		sessionid string
		err       error
		user      *model.User
	)

	if csrfToken, err = ctx.Cookie("csrftoken"); err != nil {
		logger.Errorf("Get cookie csrftoken err: %s", err)
		return false
	}
	if sessionid, err = ctx.Cookie("sessionid"); err != nil {
		logger.Errorf("Get cookie sessionid err: %s", err)
		return false
	}
	user, err = service.CheckUserCookie(sessionid, csrfToken)
	if err != nil {
		logger.Errorf("Check user session err: %s", err)
		return false
	}
	ctx.Set(ginCtxUserKey, user)
	return true
}

func (s *server) sftpHostConnectorView(ctx *gin.Context) {
	var sid string
	switch ctx.Request.Method {
	case http.MethodGet:
		sid = ctx.Query("sid")
	case http.MethodPost:
		sid = ctx.PostForm("sid")
	default:
		ctx.AbortWithStatus(http.StatusMethodNotAllowed)
		return
	}
	if sid == "" {
		logger.Errorf("Invalid elfinder request url %s from ip %s", ctx.Request.URL, ctx.ClientIP())
		ctx.String(http.StatusBadRequest, "invalid elfinder request")
		return
	}
	var userV *UserVolume
	if wsCon := s.broadCaster.GetUserWebsocket(sid); wsCon != nil {
		handler := wsCon.GetHandler()
		switch handler.Name() {
		case WebFolderName:
			userV = handler.(*webFolder).GetVolume()
		}
	}
	if userV == nil {
		logger.Errorf("Ws(%s) already closed request url %s from ip %s",
			sid, ctx.Request.URL, ctx.ClientIP())
		ctx.String(http.StatusBadRequest, "ws already disconnected")
		return
	}
	logger.Infof("Elfinder %s connected again.", sid)
	conf := config.GetConf()
	maxSize := common.ConvertSizeToBytes(conf.ZipMaxSize)
	options := map[string]string{
		"ZipMaxSize": strconv.Itoa(maxSize),
		"ZipTmpPath": conf.ZipTmpPath,
	}
	conn := elfinder.NewElFinderConnectorWithOption([]elfinder.Volume{userV}, options)
	conn.ServeHTTP(ctx.Writer, ctx.Request)
}

func (s *server) processTerminalWebsocket(ctx *gin.Context) {
	var (
		userValue   interface{}
		currentUser *model.User
		targetType  string
		targetId    string
		ok          bool

		systemUserId string // optional
	)

	userValue, ok = ctx.Get(ginCtxUserKey)
	if !ok {
		logger.Errorf("Ws has no valid user from ip %s", ctx.ClientIP())
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	currentUser = userValue.(*model.User)

	targetType, ok = ctx.GetQuery("type")
	if !ok || targetType == "" {
		logger.Error("Ws miss required params (type).")
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	targetId, ok = ctx.GetQuery("target_id")
	if !ok || targetId == "" {
		logger.Error("Ws miss required params (target_id).")
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	systemUserId, _ = ctx.GetQuery("system_user_id")
	s.runTTY(ctx, currentUser, targetType, targetId, systemUserId)
}

func (s *server) processTokenWebsocket(ctx *gin.Context) {
	tokenId, _ := ctx.GetQuery("target_id")
	tokenUser := service.GetTokenAsset(tokenId)
	if tokenUser.UserID == "" {
		logger.Errorf("Token is invalid: %s", tokenId)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	currentUser := service.GetUserDetail(tokenUser.UserID)
	if currentUser == nil {
		logger.Errorf("Token userID is invalid: %s", tokenUser.UserID)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	targetType := TargetTypeAsset
	targetId := strings.ToLower(tokenUser.AssetID)
	systemUserId := tokenUser.SystemUserID
	s.runTTY(ctx, currentUser, targetType, targetId, systemUserId)
}

func (s *server) processElfinderWebsocket(ctx *gin.Context) {
	var (
		userValue   interface{}
		currentUser *model.User
		targetId    string
		ok          bool
	)
	if userValue, ok = ctx.Get(ginCtxUserKey); !ok {
		logger.Errorf("Ws has no valid user from ip %s", ctx.ClientIP())
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	currentUser = userValue.(*model.User)
	if targetId, ok = ctx.GetQuery("target_id"); !ok {
		logger.Error("Ws miss required params (target_id).")
		ctx.AbortWithStatus(http.StatusEarlyHints)
		return
	}
	wsSocket, err := s.Upgrade(ctx)
	if err != nil {
		logger.Errorf("Websocket upgrade err: %s", err)
		ctx.String(http.StatusBadRequest, "Websocket upgrade err %s", err)
		return
	}
	defer wsSocket.Close()

	userConn := UserWebsocket{
		Uuid:           uuid.NewV4().String(),
		webSrv:         s,
		conn:           wsSocket,
		ctx:            ctx.Copy(),
		messageChannel: make(chan *Message, 10),
		user:           currentUser,
	}

	userConn.handler = &webFolder{
		ws:       &userConn,
		targetId: targetId,
		done:     make(chan struct{}),
	}

	s.broadCaster.EnterUserWebsocket(&userConn)
	defer s.broadCaster.LeaveUserWebsocket(&userConn)
	userConn.Run()
}

func (s *server) Upgrade(ctx *gin.Context) (*ws.Socket, error) {
	underWsCon, err := upGrader.Upgrade(ctx.Writer, ctx.Request, ctx.Writer.Header())
	if err != nil {
		return nil, err
	}
	wsSocket := ws.NewSocket(underWsCon, ctx.Request)
	//设置 websocket 协议层面对应的ping和pong 处理方法
	underWsCon.SetPingHandler(func(appData string) error {
		return wsSocket.WritePong([]byte(appData), maxWriteTimeOut)
	})
	underWsCon.SetPongHandler(func(appData string) error {
		return wsSocket.WritePing([]byte(appData), maxWriteTimeOut)
	})
	return wsSocket, nil
}

func (s *server) runTTY(ctx *gin.Context, currentUser *model.User,
	targetType, targetId, SystemUserID string) {
	wsSocket, err := s.Upgrade(ctx)
	if err != nil {
		logger.Errorf("Websocket upgrade err: %s", err)
		ctx.String(http.StatusBadRequest, "Websocket upgrade err %s", err)
		return
	}
	defer wsSocket.Close()

	userConn := UserWebsocket{
		Uuid:           uuid.NewV4().String(),
		webSrv:         s,
		conn:           wsSocket,
		ctx:            ctx.Copy(),
		messageChannel: make(chan *Message, 10),
		user:           currentUser,
	}
	userConn.handler = &tty{
		ws:           &userConn,
		targetType:   targetType,
		targetId:     targetId,
		systemUserId: SystemUserID,
	}
	s.broadCaster.EnterUserWebsocket(&userConn)
	defer s.broadCaster.LeaveUserWebsocket(&userConn)
	userConn.Run()
}

func (s *server) statusHandler(ctx *gin.Context) {
	status := make(map[string]interface{})
	status["timestamp"] = time.Now().UTC()
	ctx.JSON(http.StatusOK, status)
}

func registerHandlers(s *server) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	eng := gin.New()
	eng.Use(gin.Recovery())
	eng.Use(gin.Logger())
	eng.LoadHTMLGlob("./templates/**/*")
	rootGroup := eng.Group("")
	s.healthHandlers(rootGroup)
	s.debugHandlers(rootGroup)

	kokoGroup := rootGroup.Group("/koko")
	kokoGroup.Static("/static/", "./static")
	{
		s.websocketHandlers(kokoGroup)
		s.terminalHandlers(kokoGroup)
		s.tokenHandlers(kokoGroup)
		s.elfinderHandlers(kokoGroup)
	}
	return eng
}

func (s *server) websocketHandlers(router *gin.RouterGroup) {
	wsGroup := router.Group("/ws/")

	wsGroup.Group("/terminal").Use(
		s.middleSessionAuth()).GET("/", s.processTerminalWebsocket)

	wsGroup.Group("/elfinder").Use(
		s.middleSessionAuth()).GET("/", s.processElfinderWebsocket)

	wsGroup.Group("/token").GET("/", s.processTokenWebsocket)
}

func (s *server) terminalHandlers(router *gin.RouterGroup) {
	terminalGroup := router.Group("/terminal")
	terminalGroup.Use(s.middleSessionAuth())
	{
		terminalGroup.GET("/", func(ctx *gin.Context) {
			ctx.HTML(http.StatusOK, "terminal.html", nil)
		})
	}
}

func (s *server) tokenHandlers(router *gin.RouterGroup) {
	tokenGroup := router.Group("/token")
	{
		tokenGroup.GET("/", func(ctx *gin.Context) {
			ctx.HTML(http.StatusOK, "terminal.html", nil)
		})
	}
}

func (s *server) elfinderHandlers(router *gin.RouterGroup) {
	elfindlerGroup := router.Group("/elfinder")
	elfindlerGroup.Use(s.middleSessionAuth())
	{
		elfindlerGroup.GET("/sftp/", func(ctx *gin.Context) {
			ctx.HTML(http.StatusOK, "file_manager.html", "_")
		})
		elfindlerGroup.GET("/sftp/:host/", func(ctx *gin.Context) {
			hostId := ctx.Param("host")
			if _, err := uuid.FromString(hostId); err != nil {
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			ctx.HTML(http.StatusOK, "file_manager.html", hostId)
		})
		elfindlerGroup.Any("/connector/:host/", s.sftpHostConnectorView)
	}
}

func (s *server) healthHandlers(router *gin.RouterGroup) {
	router.GET("/status/", s.statusHandler)
}

func (s *server) debugHandlers(router *gin.RouterGroup) {
	debugGroup := router.Group("/debug/pprof")
	debugGroup.Use(s.middleDebugAuth())
	{
		debugGroup.GET("/", gin.WrapF(pprof.Index))
		debugGroup.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		debugGroup.GET("/profile", gin.WrapF(pprof.Profile))
		debugGroup.POST("/symbol", gin.WrapF(pprof.Symbol))
		debugGroup.GET("/symbol", gin.WrapF(pprof.Symbol))
		debugGroup.GET("/trace", gin.WrapF(pprof.Trace))
		debugGroup.GET("/allocs", gin.WrapF(pprof.Handler("allocs").ServeHTTP))
		debugGroup.GET("/block", gin.WrapF(pprof.Handler("block").ServeHTTP))
		debugGroup.GET("/goroutine", gin.WrapF(pprof.Handler("goroutine").ServeHTTP))
		debugGroup.GET("/heap", gin.WrapF(pprof.Handler("heap").ServeHTTP))
		debugGroup.GET("/mutex", gin.WrapF(pprof.Handler("mutex").ServeHTTP))
		debugGroup.GET("/threadcreate", gin.WrapF(pprof.Handler("threadcreate").ServeHTTP))
	}
}

var webServer = NewServer()

func StartHTTPServer() {
	webServer.Start()
}

func StopHTTPServer() {
	webServer.Stop()
}
