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

func (s *server) middleAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if s.checkTokenValid(ctx) || s.checkSessionValid(ctx) {
			ctx.Next()
			return
		}
		loginUrl := fmt.Sprintf("/core/auth/login/?next=%s", url.QueryEscape(ctx.Request.URL.RequestURI()))
		ctx.Redirect(http.StatusFound, loginUrl)
		ctx.Abort()
	}
}

func (s *server) middleHtmlAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if targetType, ok := ctx.GetQuery("type"); ok && targetType == TokenTargetType {
			ctx.Next()
			return
		}
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

func (s *server) checkTokenValid(ctx *gin.Context) bool {
	if targetType, ok := ctx.GetQuery("type"); ok && targetType == TokenTargetType {
		token, _ := ctx.GetQuery("target_id")
		if tokenUser := service.GetTokenAsset(token); tokenUser.UserID != "" {
			if currentUser := service.GetUserDetail(tokenUser.UserID); currentUser != nil {
				ctx.Set(ginCtxUserKey, currentUser)
				ctx.Set(ginCtxTokenUserKey, &tokenUser)
				return true
			}
		}
	}
	return false
}

func (s *server) checkSessionValid(ctx *gin.Context) bool {
	cookies := strings.Split(ctx.GetHeader("Cookie"), ";")
	var csrfToken string
	var sessionid string
	for _, line := range cookies {
		if strings.Contains(line, "csrftoken") {
			csrfToken = strings.Split(line, "=")[1]
		}
		if strings.Contains(line, "sessionid") {
			sessionid = strings.Split(line, "=")[1]
		}
	}
	user, err := service.CheckUserCookie(sessionid, csrfToken)
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
		http.Error(ctx.Writer, "invalid elfinder request", http.StatusBadRequest)
		return
	}
	userV, ok := GetUserVolume(sid)
	if !ok {
		logger.Errorf("Ws(%s) already closed request url %s from ip %s",
			sid, ctx.Request.URL, ctx.ClientIP())
		http.Error(ctx.Writer, "ws already disconnected", http.StatusBadRequest)
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

func (s *server) processWebsocket(ctx *gin.Context) {
	connectType, _ := ctx.GetQuery("type")
	targetId, _ := ctx.GetQuery("target_id")
	if connectType == "" || targetId == "" {
		logger.Error("Ws miss required params (type and target_id).")
		badRequestMsg := "miss required params (type and target_id)."
		if ctx.IsWebsocket() {
			ctx.Header("Sec-Websocket-Version", "13")
		}
		ctx.String(http.StatusBadRequest, badRequestMsg)
		return
	}
	underWsCon, err := upGrader.Upgrade(ctx.Writer, ctx.Request, ctx.Writer.Header())
	if err != nil {
		logger.Errorf("Websocket upgrade err: %s", err)
		ctx.String(http.StatusBadRequest, "Websocket upgrade err %s", err)
		return
	}
	wsSocket := ws.NewSocket(underWsCon, ctx.Request)
	defer wsSocket.Close()
	//设置 websocket 协议层面对应的ping和pong 处理方法
	underWsCon.SetPingHandler(func(appData string) error {
		return wsSocket.WritePong([]byte(appData), maxWriteTimeOut)
	})
	underWsCon.SetPongHandler(func(appData string) error {
		return wsSocket.WritePing([]byte(appData), maxWriteTimeOut)
	})
	userValue, ok := ctx.Get(ginCtxUserKey)
	if !ok {
		logger.Errorf("Ws has no valid user from ip %s", ctx.ClientIP())
		return
	}
	user := userValue.(*model.User)
	conn := ttyCon{
		Uuid:           uuid.NewV4().String(),
		ctx:            ctx.Copy(),
		webSrv:         s,
		conn:           wsSocket,
		user:           user,
		targetType:     strings.ToLower(connectType),
		targetId:       strings.ToLower(targetId),
		messageChannel: make(chan *Message, 10),
	}
	s.broadCaster.ConEntering(&conn)
	defer s.broadCaster.ConLeaving(&conn)
	conn.Run()
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
	eng.GET("/status/", s.statusHandler)
	debugGroup := eng.Group("/debug/pprof")
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
	kokoGroup := eng.Group("/koko")
	kokoGroup.Static("/static/", "./static")
	terminalGroup := kokoGroup.Group("/terminal")
	terminalGroup.Use(s.middleHtmlAuth())
	terminalGroup.GET("/", func(ctx *gin.Context) {
		ctx.HTML(http.StatusOK, "terminal.html", nil)
	})

	wsGroup := kokoGroup.Group("/ws")
	wsGroup.Use(s.middleAuth())
	wsGroup.GET("/", s.processWebsocket)

	elfindlerGroup := kokoGroup.Group("/elfinder")
	elfindlerGroup.Use(s.middleAuth())
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
	return eng
}

var webServer = NewServer()

func StartHTTPServer() {
	webServer.Start()
}

func StopHTTPServer() {
	webServer.Stop()
}
