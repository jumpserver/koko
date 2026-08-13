package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/gateway"
	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/lion/middleware"
	"github.com/jumpserver/koko/pkg/lion/proxy"
	"github.com/jumpserver/koko/pkg/lion/session"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

const (
	defaultBufferSize = 1024
)

var upGrader = websocket.Upgrader{
	ReadBufferSize:  defaultBufferSize,
	WriteBufferSize: defaultBufferSize,
	Subprotocols:    []string{"guacamole"},
	CheckOrigin:     middleware.OriginAllowed,
}

type GuacamoleTunnelServer struct {
	JmsService     *service.JMService
	Cache          *GuaTunnelCacheManager
	SessionService *session.Server
}

func (g *GuacamoleTunnelServer) getClientInfo(ctx *gin.Context, token *model.ConnectToken) guacd.ClientInformation {
	info := guacd.NewClientInformation()
	if supportImages, ok := ctx.GetQueryArray("GUAC_IMAGE"); ok {
		info.ImageMimetypes = supportImages
	}
	if supportAudios, ok := ctx.GetQueryArray("GUAC_AUDIO"); ok {
		info.AudioMimetypes = supportAudios
	}
	if supportVideos, ok := ctx.GetQueryArray("GUAC_VIDEO"); ok {
		info.VideoMimetypes = supportVideos
	}
	width, height := session.ParseWidthAndHeight(ctx, token)
	if width > 0 && height > 0 {
		info.OptimalScreenWidth = width
		info.OptimalScreenHeight = height
	}
	if dpi, ok := ctx.GetQuery("GUAC_DPI"); ok {
		if dpiInt, err := strconv.Atoi(dpi); err == nil && dpiInt > 0 {
			info.OptimalResolution = dpiInt
		}
	}
	if keyboardLayout, ok := ctx.GetQuery("GUAC_KEYBOARD"); ok {
		info.KeyboardLayout = keyboardLayout
	}
	if timezone, ok := ctx.GetQuery("GUAC_TIMEZONE"); ok {
		info.SetTimezone(timezone)
	}
	return info
}

func (g *GuacamoleTunnelServer) Connect(ctx *gin.Context) {
	ws, err := upGrader.Upgrade(ctx.Writer, ctx.Request, ctx.Writer.Header())
	if err != nil {
		logger.Errorf("Websocket Upgrade err: %+v", err)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	defer ws.Close()

	tokenId, ok := ctx.GetQuery("TOKEN_ID")
	if !ok {
		logger.Error("No TOKEN id params")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrBadParams.String()))
		return
	}
	tunnelSession, err := g.SessionService.CreatByToken(ctx, tokenId)
	if err != nil {
		logger.Errorf("Create token session err: %+v", err)
		errIns := NewJMSGuacamoleError(1006, err.Error())
		_ = ws.WriteMessage(websocket.TextMessage, []byte(errIns.String()))
		return
	}
	defer func() {
		if err2 := tunnelSession.ReleaseAppletAccount(); err2 != nil {
			logger.Errorf("Release account failed: %s", err2)
		}
		if config.GetConf().DriveScope == config.DriverScopeSession {
			// clean driver path
			driveRootPath := config.GetConf().DrivePath
			sessionDrivePath := filepath.Join(driveRootPath, tunnelSession.ID)
			logger.Debugf("Remove drive folder %s", sessionDrivePath)
			if err3 := os.RemoveAll(sessionDrivePath); err3 != nil {
				logger.Errorf("Remove drive folder %s err: %+v", sessionDrivePath, err3)
			}
		}
	}()
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		logger.Error("No auth user found")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAuthUser.String()))
		return
	}
	user := userItem.(*model.User)
	if user.ID != tunnelSession.User.ID {
		logger.Error("No valid auth user found")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAuthUser.String()))
		return
	}
	sessionId := tunnelSession.ID
	logger.Infof("User %s start to connect session %s", user, sessionId)
	if err = tunnelSession.ConnectedCallback(); err != nil {
		logger.Errorf("Session connect callback err %v", err)
		errIns := NewJMSGuacamoleError(1006, err.Error())
		_ = ws.WriteMessage(websocket.TextMessage, []byte(errIns.String()))
		return
	}
	p, _ := json.Marshal(tunnelSession)
	ins := NewJmsEventInstruction("session", string(p))
	if err = ws.WriteMessage(websocket.TextMessage, []byte(ins.String())); err != nil {
		logger.Errorf("Write session message err: %+v", err)
		return
	}
	if tunnelSession.AuthInfo.Ticket != nil {
		if err2 := g.JmsService.CreateSessionTicketRelation(tunnelSession.ID,
			tunnelSession.AuthInfo.Ticket.ID); err2 != nil {
			logger.Errorf("Create session %s ticket relation failed: %s", tunnelSession.ID, err2)
		}
	}
	if tunnelSession.AuthInfo.FaceMonitorToken != "" {
		faceMonitorToken := tunnelSession.AuthInfo.FaceMonitorToken
		faceReq := service.JoinFaceMonitorRequest{
			FaceMonitorToken: faceMonitorToken,
			SessionId:        sessionId,
		}
		logger.Infof("Session %s join face monitor", tunnelSession.ID)
		if err1 := g.JmsService.JoinFaceMonitor(faceReq); err1 != nil {
			logger.Errorf("Session %s join face monitor err: %s", tunnelSession.ID, err1)
		}
	}

	info := g.getClientInfo(ctx, tunnelSession.AuthInfo)

	conf := tunnelSession.GuaConfiguration()
	for argName, argValue := range info.ExtraConfig() {
		conf.SetParameter(argName, argValue)
	}
	if tunnelSession.Gateway != nil || tunnelSession.GatewayTarget != nil {
		dstAddr := net.JoinHostPort(conf.GetParameter(guacd.Hostname),
			conf.GetParameter(guacd.Port))
		domainGateway := gateway.DomainGateway{
			DstAddr:         dstAddr,
			SelectedGateway: tunnelSession.Gateway,
			Destination:     tunnelSession.GatewayTarget,
		}
		if err = domainGateway.Start(); err != nil {
			logger.Errorf("Start domain gateway err: %+v", err)
			_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrGatewayFailed.String()))
			if err = tunnelSession.ConnectedFailedCallback(err); err != nil {
				logger.Errorf("Update session connect status failed %+v", err)
			}
			if err = tunnelSession.DisConnectedCallback(); err != nil {
				logger.Errorf("Session DisConnectedCallback err: %+v", err)
			}
			return
		}
		defer domainGateway.Stop()
		localAddr := domainGateway.GetListenAddr()
		conf.SetParameter(guacd.Hostname, localAddr.IP.String())
		conf.SetParameter(guacd.Port, strconv.Itoa(localAddr.Port))
		logger.Infof("Start SSH forwarder %s listen on %s:%d", domainGateway.Name(),
			localAddr.IP.String(), localAddr.Port)
	}

	var tunnel *guacd.Tunnel
	guacdAddr := config.GetConf().SelectGuacdAddr()

	tunnel, err = guacd.NewTunnel(guacdAddr, conf, info)
	if err != nil {
		logger.Errorf("Connect tunnel err: %+v", err)
		msg := fmt.Sprintf("Connect guacd server %s failed", guacdAddr)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrGuacamoleServer.String()))
		if err = tunnelSession.ConnectedFailedCallback(err); err != nil {
			logger.Errorf("Update session connect status failed %+v", err)
		}
		if err = tunnelSession.DisConnectedCallback(); err != nil {
			logger.Errorf("Session DisConnectedCallback err: %+v", err)
		}
		reason := model.SessionLifecycleLog{Reason: msg}
		g.RecordLifecycleLog(sessionId, model.AssetConnectFinished, reason)
		return
	}
	defer tunnel.Close()
	g.RecordLifecycleLog(sessionId, model.AssetConnectSuccess, model.EmptyLifecycleLog)

	logger.Infof("Session[%s] use resolution (%d*%d)",
		sessionId, info.OptimalScreenWidth, info.OptimalScreenHeight)
	meta := MetaShareUserMessage{
		ShareId:    user.ID,
		UserId:     user.ID,
		User:       user.String(),
		Created:    time.Now().UTC().String(),
		RemoteAddr: ctx.ClientIP(),
		Primary:    true,
		Writable:   true,
	}
	conn := Connection{
		guacdAddr:   guacdAddr,
		Sess:        &tunnelSession,
		guacdTunnel: tunnel,
		Service:     g.SessionService,
		ws:          ws,
		done:        make(chan struct{}),
		Cache:       g.Cache,
		meta:        &meta,

		currentOnlineUsers: make(map[string]MetaShareUserMessage),
	}
	outFilter := OutputStreamInterceptingFilter{
		acknowledgeBlobs: true,
		tunnel:           &conn,
		streams:          map[string]OutStreamResource{},
	}
	inputFilter := InputStreamInterceptingFilter{
		tunnel:  &conn,
		streams: map[string]*InputStreamResource{},
	}
	conn.outputFilter = &outFilter
	conn.inputFilter = &inputFilter
	conn.clipboardFilter = newClipboardPolicyFilter(tunnelSession.ActionPerm)
	logger.Infof("Session[%s] connect success", sessionId)
	g.Cache.Add(&conn)
	replayRecorder := &ReplayRecorder{
		tunnelSession: &tunnelSession,
		SessionId:     tunnelSession.ID,
		guacdAddr:     guacdAddr,
		conf:          NewReplayConfiguration(&conf, tunnel.UUID()),
		info:          info.Clone(),
		newPartChan:   make(chan struct{}, 1),
		MaxSize:       config.GetConf().ReplayMaxSize,
		apiClient:     g.JmsService,
		currentIndex:  0,
	}
	childCtx, cancel := context.WithCancel(ctx)
	replayRecorder.Start(childCtx)
	defer func() {
		cancel()
		logger.Infof("replayRecorder[%s] stop", sessionId)
		replayRecorder.Stop()

	}()
	_ = conn.Run(ctx)
	g.Cache.Delete(&conn)
	if err = tunnelSession.DisConnectedCallback(); err != nil {
		logger.Errorf("Session DisConnectedCallback err: %+v", err)
	}
	logger.Infof("Session[%s] disconnect", sessionId)
}

func (g *GuacamoleTunnelServer) RecordLifecycleLog(sid string, event model.LifecycleEvent,
	logObj model.SessionLifecycleLog) {
	if err := g.JmsService.RecordSessionLifecycleLog(sid, event, logObj); err != nil {
		logger.Errorf("Record session %s lifecycle %s log err: %s", sid, event, err)
	}
}

func (g *GuacamoleTunnelServer) DownloadFile(ctx *gin.Context) {
	tid := ctx.Param("tid")
	index := ctx.Param("index")
	filename := ctx.Param("filename")
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		logger.Error("Download file but not user authorized")
		ctx.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	user := userItem.(*model.User)
	if tun := g.Cache.Get(tid); tun != nil && tun.Sess.User.ID == user.ID {
		recorder := proxy.GetFTPFileRecorder(g.JmsService)
		fileLog := model.FTPLog{
			ID:         common.UUID(),
			User:       tun.Sess.User.String(),
			Asset:      tun.Sess.Asset.String(),
			OrgID:      tun.Sess.Asset.OrgID,
			Account:    tun.Sess.Account.String(),
			RemoteAddr: ctx.ClientIP(),
			Operate:    model.OperateDownload,
			Path:       filename,
			DateStart:  common.NewNowUTCTime(),
			Session:    tun.Sess.ID,
		}
		ctx.Writer.Header().Set("content-disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		out := OutStreamResource{
			streamIndex: index,
			mediaType:   "",
			writer:      ctx.Writer,
			ctx:         ctx.Request.Context(),
			done:        make(chan struct{}),
			ftpLog:      &fileLog,
			recorder:    recorder,
		}
		tun.outputFilter.addOutStream(out)
		if err := out.Wait(); err != nil {
			logger.Errorf("Session[%s] download file %s err: %s", tun, filename, err)
			ctx.JSON(http.StatusBadRequest, ErrorResponse(err))
			g.SessionService.AuditFileOperation(fileLog)
			recorder.RemoveFtpLog(fileLog.ID)
			return
		}
		fileLog.IsSuccess = true
		g.SessionService.AuditFileOperation(fileLog)
		recorder.FinishFTPFile(fileLog.ID)
		logger.Infof("Session[%s] download file %s success", tun, filename)
		return
	}
	ctx.AbortWithStatus(http.StatusNotFound)
}

func (g *GuacamoleTunnelServer) UploadFile(ctx *gin.Context) {
	tid := ctx.Param("tid")
	index := ctx.Param("index")
	filename := ctx.Param("filename")
	form, err := ctx.MultipartForm()
	if err != nil {
		logger.Errorf("Upload file err: %s", err)
		ctx.JSON(http.StatusBadRequest, ErrorResponse(err))
		return
	}
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		logger.Error("Upload file but not user authorized")
		ctx.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	user := userItem.(*model.User)
	if tun := g.Cache.Get(tid); tun != nil && tun.Sess.User.ID == user.ID {
		logger.Infof("User %s upload file %s", user, filename)
		recorder := proxy.GetFTPFileRecorder(g.JmsService)
		fileLog := model.FTPLog{
			ID:         common.UUID(),
			User:       tun.Sess.User.String(),
			Asset:      tun.Sess.Asset.String(),
			OrgID:      tun.Sess.Asset.OrgID,
			Account:    tun.Sess.Account.String(),
			RemoteAddr: ctx.ClientIP(),
			Operate:    model.OperateUpload,
			Path:       filename,
			DateStart:  common.NewNowUTCTime(),
			Session:    tun.Sess.ID,
		}
		files := form.File["file"]
		for _, file := range files {
			fdReader, err := file.Open()
			if err != nil {
				return
			}
			stream := InputStreamResource{
				streamIndex: index,
				reader:      fdReader,
				done:        make(chan struct{}),
			}
			tun.inputFilter.addInputStream(&stream)
			stream.Wait()
			if err := stream.WaitErr(); err != nil {
				logger.Errorf("Session[%s] upload file %s err: %s", tun, filename, err)
				g.SessionService.AuditFileOperation(fileLog)
				continue
			}
			logger.Infof("Session[%s] upload file %s success", tun, filename)
			fileLog.IsSuccess = true
			g.SessionService.AuditFileOperation(fileLog)
			_, _ = fdReader.(io.Seeker).Seek(0, io.SeekStart)
			if err1 := recorder.Record(&fileLog, fdReader); err1 != nil {
				logger.Errorf("Record file %s err: %s", filename, err1)
			}
			_ = fdReader.Close()
		}
		return
	}
	logger.Infof("No session tunnel found")
	ctx.AbortWithStatus(http.StatusNotFound)
}

func (g *GuacamoleTunnelServer) Monitor(ctx *gin.Context) {
	ws, err := upGrader.Upgrade(ctx.Writer, ctx.Request, ctx.Writer.Header())
	if err != nil {
		logger.Errorf("Websocket Upgrade err: %+v", err)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	defer ws.Close()
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAuthUser.String()))
		return
	}
	user := userItem.(*model.User)
	if user.ID == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAuthUser.String()))
		return
	}
	sessionId, ok := ctx.GetQuery("SESSION_ID")
	if !ok {
		logger.Error("No session param found")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrBadParams.String()))
		return
	}

	result, err := g.JmsService.ValidateJoinSessionPermission(user.ID, sessionId)
	if err != nil {
		logger.Errorf("Validate join session err: %s", err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAPIFailed.String()))
		return
	}
	if !result.Ok {
		logger.Errorf("Validate join session failed : %s", result.Msg)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrPermission.String()))
		return
	}

	tunnelCon := g.Cache.GetMonitorTunnelerBySessionId(sessionId)
	if tunnelCon == nil {
		logger.Error("No session tunnel found")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrNoSession.String()))
		return
	}
	defer tunnelCon.Close()

	ints := guacd.NewInstruction(INTERNALDATAOPCODE, tunnelCon.UUID())
	_ = ws.WriteMessage(websocket.TextMessage, []byte(ints.String()))
	conn := MonitorCon{
		Id:          sessionId,
		guacdTunnel: tunnelCon,
		ws:          ws,
		Service:     g,
		User:        user,
	}
	logger.Infof("User %s start to monitor session %s", user, sessionId)
	logObj := model.SessionLifecycleLog{User: user.String()}
	g.RecordLifecycleLog(sessionId, model.AdminJoinMonitor, logObj)
	defer func() {
		g.RecordLifecycleLog(sessionId, model.AdminExitMonitor, logObj)
	}()
	_ = conn.Run(ctx.Request.Context())
	g.Cache.RemoveMonitorTunneler(sessionId, tunnelCon)
	logger.Infof("User %s stop to monitor session %s", user, sessionId)
}

func (g *GuacamoleTunnelServer) CreateShare(ctx *gin.Context) {
	var params struct {
		SessionId   string   `json:"session_id"`
		ExpiredTime int      `json:"expired_time"`
		Users       []string `json:"users"`
		ActionPerm  string   `json:"action_perm"`
	}
	if err := ctx.BindJSON(&params); err != nil {
		logger.Errorf("Bind share params err: %s", err)
		ctx.JSON(http.StatusBadRequest, ErrorResponse(err))
		return
	}
	shareReq := model.SharingSessionRequest{
		SessionID:  params.SessionId,
		ExpireTime: params.ExpiredTime,
		Users:      params.Users,
		ActionPerm: params.ActionPerm,
	}
	logger.Debugf("Create share room %v", shareReq)
	if resp, err := g.JmsService.CreateShareRoom(shareReq); err != nil {
		logger.Errorf("Create share room err: %s", err)
		ctx.JSON(http.StatusBadRequest, ErrorResponse(err))
	} else {
		ctx.JSON(http.StatusOK, resp)
	}
}

func (g *GuacamoleTunnelServer) GetShare(ctx *gin.Context) {
	var params struct {
		Code string `json:"code"`
	}
	if err := ctx.BindJSON(&params); err != nil {
		logger.Errorf("Bind share params err: %s", err)
		ctx.JSON(http.StatusBadRequest, ErrorResponse(err))
		return
	}
	shareId := ctx.Param("id")
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		err1 := fmt.Errorf("not auth user")
		ctx.JSON(http.StatusBadRequest, ErrorResponse(err1))
		return
	}
	user := userItem.(*model.User)
	data := model.SharePostData{
		ShareId:    shareId,
		Code:       params.Code,
		UserId:     user.ID,
		RemoteAddr: ctx.ClientIP(),
	}
	apiClient := g.JmsService.Copy()
	if acceptLang := ctx.GetHeader("Accept-Language"); acceptLang != "" {
		apiClient.SetHeader("Accept-Language", acceptLang)
	}
	if cookieLang, err2 := ctx.Cookie("django_language"); err2 == nil {
		apiClient.SetCookie("django_language", cookieLang)
	}
	recordRet, err := apiClient.JoinShareRoom(data)
	if err != nil {
		logger.Errorf("Validate join session err: %s", err)
		errResponse := ErrorResponse(err)
		if recordRet.Err != nil {
			logger.Errorf("Join share room err: %s", recordRet.Err)
			msg := fmt.Errorf("%+v", recordRet.Err)
			errResponse = ErrorResponse(msg)
		}
		ctx.JSON(http.StatusBadRequest, errResponse)
		return
	}
	if recordRet.Err != nil {
		logger.Errorf("Join share room err: %s", recordRet.Err)
		ctx.JSON(http.StatusBadRequest, recordRet.Err)
		return
	}
	ctx.JSON(http.StatusOK, recordRet)
}

func (g *GuacamoleTunnelServer) Share(ctx *gin.Context) {
	ws, err := upGrader.Upgrade(ctx.Writer, ctx.Request, ctx.Writer.Header())
	if err != nil {
		logger.Errorf("Websocket Upgrade err: %+v", err)
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	defer ws.Close()
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAuthUser.String()))
		return
	}
	user := userItem.(*model.User)
	if user.ID == "" {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrAuthUser.String()))
		return
	}
	shareId, ok := ctx.GetQuery("SHARE_ID")
	if !ok {
		logger.Error("No share id params")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrBadParams.String()))
		return
	}
	recordId, ok := ctx.GetQuery("RECORD_ID")
	if !ok {
		logger.Error("No record id params")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrBadParams.String()))
		return
	}
	sessionId, ok := ctx.GetQuery("SESSION_ID")
	if !ok {
		logger.Error("No SESSION_ID found")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrBadParams.String()))
		return
	}
	writable := false
	writePem, _ := ctx.GetQuery("Writable")
	writable = strings.EqualFold(writePem, "true")

	logger.Debugf("User %s start to share session %s", user, sessionId)
	tunnelCon := g.Cache.GetMonitorTunnelerBySessionId(sessionId)
	if tunnelCon == nil {
		logger.Error("No session tunnel found")
		_ = ws.WriteMessage(websocket.TextMessage, []byte(ErrNoSession.String()))
		return
	}
	defer tunnelCon.Close()

	logObj := model.SessionLifecycleLog{User: user.String()}
	g.RecordLifecycleLog(sessionId, model.UserJoinSession, logObj)
	defer func() {
		g.RecordLifecycleLog(sessionId, model.UserLeaveSession, logObj)
	}()
	ints := guacd.NewInstruction(INTERNALDATAOPCODE, tunnelCon.UUID())
	_ = ws.WriteMessage(websocket.TextMessage, []byte(ints.String()))
	meta := MetaShareUserMessage{
		ShareId:    shareId,
		SessionId:  sessionId,
		UserId:     user.ID,
		User:       user.String(),
		Created:    time.Now().UTC().String(),
		RemoteAddr: ctx.ClientIP(),
		Primary:    false,
		Writable:   writable,
	}
	conn := MonitorCon{
		Id:          sessionId,
		guacdTunnel: tunnelCon,
		ws:          ws,
		Service:     g,
		User:        user,
		Meta:        &meta,
	}
	logger.Infof("User %s start to share session %s", user, sessionId)
	_ = conn.Run(ctx.Request.Context())
	g.Cache.RemoveMonitorTunneler(sessionId, tunnelCon)
	logger.Infof("User %s stop to share session %s", user, sessionId)
	if err1 := g.JmsService.FinishShareRoom(recordId); err1 != nil {
		logger.Errorf("Finish share room err: %s", err1)
	}
}

func (g *GuacamoleTunnelServer) DeleteShare(ctx *gin.Context) {
	var params MetaShareUserMessage
	if err := ctx.BindJSON(&params); err != nil {
		logger.Errorf("Bind delete share params err: %s", err)
		ctx.JSON(http.StatusBadRequest, ErrorResponse(err))
		return
	}
	userItem, ok := ctx.Get(config.GinCtxUserKey)
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "msg": "not auth user"})
		return
	}
	user := userItem.(*model.User)
	if user.ID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"ok": false, "msg": "not auth user"})
		return
	}
	var removeData struct {
		User string               `json:"user"`
		Meta MetaShareUserMessage `json:"meta"`
	}
	removeData.User = user.String()
	removeData.Meta = params
	jsonData, _ := json.Marshal(removeData)
	logger.Infof("User %s remove share session %s", user, params.SessionId)
	g.Cache.BroadcastSessionEvent(params.SessionId, &Event{
		Type: ShareRemoveUser,
		Data: jsonData,
	})
	ctx.JSON(http.StatusOK, gin.H{"ok": true})
}
