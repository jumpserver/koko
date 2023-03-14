package httpd

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/LeeEirc/elfinder"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
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

func NewServer(jmsService *service.JMService) *Server {
	srv := &Server{
		broadCaster: NewBroadcaster(),
		JmsService:  jmsService,
	}

	eng := createRouter(jmsService, srv)
	conf := config.GetConf()
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	srv.Srv = &http.Server{
		Addr:    addr,
		Handler: eng,
	}
	return srv
}

type Server struct {
	broadCaster *broadcaster
	Srv         *http.Server
	JmsService  *service.JMService
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

func (s *Server) ProcessTerminalWebsocket(ctx *gin.Context) {
	var tokenParams WsParams
	if err := ctx.ShouldBind(&tokenParams); err != nil {
		logger.Errorf("Ws miss required params( token ) err: %s", err)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	userValue, ok := ctx.Get(auth.ContextKeyUser)
	if !ok {
		logger.Errorf("Ws has no valid user from ip %s", ctx.ClientIP())
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	currentUser := userValue.(*model.User)

	s.runTTY(ctx, currentUser, &tokenParams)
}

func (s *Server) ProcessElfinderWebsocket(ctx *gin.Context) {
	var (
		userValue   interface{}
		currentUser *model.User
		targetId    string
		ok          bool
	)
	if userValue, ok = ctx.Get(auth.ContextKeyUser); !ok {
		logger.Errorf("Ws has no valid user from ip %s", ctx.ClientIP())
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	currentUser = userValue.(*model.User)
	if targetId, ok = ctx.GetQuery("target_id"); !ok {
		logger.Error("Ws miss required params (target_id).")
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	var params struct {
		TokenId string `form:"token"`
		AssetId string `form:"asset"`
	}
	if err := ctx.ShouldBind(&params); err != nil {
		logger.Errorf("Ws miss required params (token or asset) err: %s", err)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}

	wsSocket, err := s.Upgrade(ctx)
	if err != nil {
		logger.Errorf("Websocket upgrade err: %s", err)
		ctx.String(http.StatusBadRequest, "Websocket upgrade err %s", err)
		return
	}
	defer wsSocket.Close()
	setting := s.getPublicSetting()
	userConn := UserWebsocket{
		Uuid:           common.UUID(),
		webSrv:         s,
		conn:           wsSocket,
		ctx:            ctx.Copy(),
		messageChannel: make(chan *Message, 10),
		user:           currentUser,
		setting:        &setting,
	}

	userConn.handler = &webFolder{
		ws:         &userConn,
		targetId:   targetId,
		assetId:    params.AssetId,
		tokenId:    params.TokenId,
		done:       make(chan struct{}),
		jmsService: s.JmsService,
	}

	s.broadCaster.EnterUserWebsocket(&userConn)
	defer s.broadCaster.LeaveUserWebsocket(&userConn)
	userConn.Run()
}

func (s *Server) Upgrade(ctx *gin.Context) (*ws.Socket, error) {
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

func (s *Server) runTTY(ctx *gin.Context, currentUser *model.User, params *WsParams) {
	wsSocket, err := s.Upgrade(ctx)
	if err != nil {
		logger.Errorf("Websocket upgrade err: %s", err)
		ctx.String(http.StatusBadRequest, "Websocket upgrade err %s", err)
		return
	}
	setting := s.getPublicSetting()
	userConn := UserWebsocket{
		Uuid:           common.UUID(),
		webSrv:         s,
		conn:           wsSocket,
		ctx:            ctx.Copy(),
		messageChannel: make(chan *Message, 10),
		user:           currentUser,
		setting:        &setting,
	}
	ttyHandler := &tty{
		ws:          &userConn,
		targetType:  params.TargetType,
		targetId:    params.TargetID,
		jmsService:  s.JmsService,
		extraParams: ctx.Request.Form,
	}
	if params.Token != "" {
		res, err := s.JmsService.GetConnectTokenInfo(params.Token)
		if err != nil {
			logger.Errorf("Get connect token info err: %s", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if res.Code != "" {
			logger.Errorf("Token is invalid: %s", res.Detail)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ttyHandler.ConnectToken = &res
	}
	userConn.handler = ttyHandler

	s.broadCaster.EnterUserWebsocket(&userConn)
	defer s.broadCaster.LeaveUserWebsocket(&userConn)
	userConn.Run()
}

func (s *Server) HealthStatusHandler(ctx *gin.Context) {
	status := make(map[string]interface{})
	status["timestamp"] = time.Now().UTC()
	ctx.JSON(http.StatusOK, status)
}

func (s *Server) GenerateViewMeta(targetId string) (meta ViewPageMata) {
	meta.ID = targetId
	setting, err := s.JmsService.GetPublicSetting()
	if err != nil {
		logger.Errorf("Get core api public setting err: %s", err)
	}
	meta.IconURL = setting.LogoURLS.Favicon
	return
}

func (s *Server) getPublicSetting() model.PublicSetting {
	setting, err := s.JmsService.GetPublicSetting()
	if err != nil {
		logger.Errorf("Get Public setting err: %s", err)
	}
	return setting
}
