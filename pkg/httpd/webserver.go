package httpd

import (
	"context"
	"log"
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
	return &Server{
		broadCaster: NewBroadcaster(),
		JmsService:  jmsService,
	}
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
	var tokenParams struct {
		Token string `form:"connectToken"`
	}
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
	s.runTokenTTY(ctx, currentUser, tokenParams.Token)
}

func (s *Server) ProcessTokenWebsocket(ctx *gin.Context) {
	var targetParams struct {
		TargetId string `form:"target_id"`
	}
	if err := ctx.ShouldBind(&targetParams); err != nil {
		logger.Errorf("Ws miss required params(target_id ) err: %s", err)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	tokenUser, err := s.JmsService.GetTokenAsset(targetParams.TargetId)
	if err != nil || tokenUser.UserID == "" {
		logger.Errorf("Token is invalid: %s", targetParams.TargetId)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	currentUser, err := s.JmsService.GetUserById(tokenUser.UserID)
	if err != nil || currentUser == nil {
		logger.Errorf("Token userID is invalid: %s", tokenUser.UserID)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	targetId := tokenUser.AssetID
	targetType := TargetTypeAsset
	s.runTTY(ctx, currentUser, targetType, targetId)
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

func (s *Server) runTokenTTY(ctx *gin.Context, currentUser *model.User, token string) {
	res, err := s.JmsService.GetConnectTokenInfo(token)
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
	/*
	 1. 校验 protocol 是否支持

	*/
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
	userConn.handler = &tty{
		ws:           &userConn,
		ConnectToken: &res,
		jmsService:   s.JmsService,
		extraParams:  ctx.Request.Form,
	}
	s.broadCaster.EnterUserWebsocket(&userConn)
	defer s.broadCaster.LeaveUserWebsocket(&userConn)
	userConn.Run()
}

func (s *Server) runTTY(ctx *gin.Context, currentUser *model.User,
	targetType, targetId string) {
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
	userConn.handler = &tty{
		ws:          &userConn,
		targetType:  targetType,
		targetId:    targetId,
		jmsService:  s.JmsService,
		extraParams: ctx.Request.Form,
	}
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
