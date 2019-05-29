package httpd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/googollee/go-socket.io"
	"github.com/satori/go.uuid"

	"cocogo/pkg/logger"
	"cocogo/pkg/proxy"
	"cocogo/pkg/service"
)

func AuthDecorator(handler http.HandlerFunc) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		cookies := strings.Split(request.Header.Get("Cookie"), ";")
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
		_, err := service.CheckUserCookie(sessionid, csrfToken)
		if err != nil {
			http.Redirect(responseWriter, request, "", http.StatusFound)
		}
	}
}

func OnConnectHandler(s socketio.Conn) error {
	// 首次连接 1.获取当前用户的信息
	logger.Debug("OnConnectHandler")
	cookies := strings.Split(s.RemoteHeader().Get("Cookie"), ";")
	var csrfToken, sessionID, remoteIP string
	for _, line := range cookies {
		if strings.Contains(line, "csrftoken") {
			csrfToken = strings.Split(line, "=")[1]
		}
		if strings.Contains(line, "sessionid") {
			sessionID = strings.Split(line, "=")[1]
		}
	}
	user, err := service.CheckUserCookie(sessionID, csrfToken)
	if err != nil {
		return errors.New("user is not authenticated")
	}
	remoteAddr := s.RemoteHeader().Get("X-Forwarded-For")
	if remoteAddr == "" {
		remoteIP = s.RemoteAddr().String()
	} else {
		remoteIP = strings.Split(remoteAddr, ",")[0]
	}
	logger.Infof("%s connect websocket from %s\n", user.Username, remoteIP)
	conn := newWebConn(s.ID(), s, remoteIP, user)
	ctx := WebContext{User: user, Connection: conn}
	s.SetContext(ctx)
	conns.AddWebConn(s.ID(), conn)
	logger.Info("On Connect handler end")
	s.Emit("3")
	return nil
}

func OnErrorHandler(e error) {
	logger.Debug("OnError trigger")
	logger.Debug(e)
}

func OnHostHandler(s socketio.Conn, message HostMsg) {
	// secret 	uuid string
	logger.Debug("OnHost trigger")
	win := ssh.Window{Height: 24, Width: 80}
	assetID := message.Uuid
	systemUserId := message.UserID
	secret := message.Secret
	width, height := message.Size[0], message.Size[1]
	if width != 0 {
		win.Width = width
	}
	if height != 0 {
		win.Height = height
	}
	clientID := uuid.NewV4().String()
	emitMsg := EmitRoomMsg{clientID, secret}
	s.Emit("room", emitMsg)
	asset := service.GetAsset(assetID)
	systemUser := service.GetSystemUser(systemUserId)

	if asset.Id == "" || systemUser.Id == "" {
		return
	}

	ctx := s.Context().(WebContext)
	userR, userW := io.Pipe()
	conn := conns.GetWebConn(s.ID())
	clientConn := &Client{
		Uuid: clientID, Cid: conn.Cid, user: conn.User,
		WinChan: make(chan ssh.Window, 100), Conn: s,
		UserRead: userR, UserWrite: userW,
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	clientConn.WinChan <- win
	conn.AddClient(clientID, clientConn)
	proxySrv := proxy.ProxyServer{UserConn: clientConn, User: ctx.User, Asset: &asset, SystemUser: &systemUser}
	go proxySrv.Proxy()

}

func OnTokenHandler(s socketio.Conn, message TokenMsg) {
	logger.Debug("OnToken trigger")
	winSiz := ssh.Window{Height: 24, Width: 80}
	token := message.Token
	secret := message.Secret
	width, height := message.Size[0], message.Size[1]
	if width != 0 {
		winSiz.Width = width
	}
	if height != 0 {
		winSiz.Height = height
	}
	clientID := uuid.NewV4().String()
	emitMs := EmitRoomMsg{clientID, secret}
	s.Emit("room", emitMs)

	// check token

	if token == "" || secret == "" {
		msg := fmt.Sprintf("Token or secret is None: %s %s", token, secret)
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		s.Emit("data", dataMsg)
		s.Emit("disconnect")
	}
	tokenUser := service.GetTokenAsset(token)
	logger.Debug(tokenUser)
	if tokenUser.UserId == "" {
		msg := "Token info is none, maybe token expired"
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		s.Emit("data", dataMsg)
		s.Emit("disconnect")
	}

	currentUser := service.GetUserProfile(tokenUser.UserId)
	con := conns.GetWebConn(s.ID())
	con.User = currentUser

	asset := service.GetAsset(tokenUser.AssetId)
	systemUser := service.GetSystemUser(tokenUser.SystemUserId)

	if asset.Id == "" || systemUser.Id == "" {
		return
	}

	userR, userW := io.Pipe()
	conn := conns.GetWebConn(s.ID())
	clientConn := Client{
		Uuid: clientID, Cid: conn.Cid, user: conn.User,
		WinChan: make(chan ssh.Window, 100), Conn: s,
		UserRead: userR, UserWrite: userW, Closed: false,
	}
	clientConn.WinChan <- winSiz
	conn.AddClient(clientID, &clientConn)

	// Todo: 构建proxy server 启动goroutine
}

func OnDataHandler(s socketio.Conn, message DataMsg) {
	logger.Debug("OnData trigger")
	cid := message.Room
	webconn := conns.GetWebConn(s.ID())
	client := webconn.GetClient(cid)
	if client == nil {
		return
	}
	_, _ = client.UserWrite.Write([]byte(message.Data))
}

func OnResizeHandler(s socketio.Conn, message ResizeMsg) {
	winSize := ssh.Window{Height: message.Height, Width: message.Width}
	logger.Debugf("On resize event trigger: %d*%d", message.Width, message.Height)
	conn := conns.GetWebConn(s.ID())
	conn.SetWinSize(winSize)
}

func OnLogoutHandler(s socketio.Conn, message string) {
	logger.Debug("OnLogout trigger")
	logger.Debugf("Msg: %s\n", message)
	webConn := conns.GetWebConn(s.ID())
	if webConn == nil {
		logger.Error("No conn found")
		return
	}
	client := webConn.GetClient(message)
	if client == nil {
		logger.Error("No client found")
		return
	}
	_ = client.Close()
}

func OnDisconnect(s socketio.Conn, msg string) {
	logger.Debug("OnDisconnect trigger")
}
