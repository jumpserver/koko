package webssh

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gliderlabs/ssh"
	socketio "github.com/googollee/go-socket.io"
	uuid "github.com/satori/go.uuid"

	"cocogo/pkg/logger"
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
		user := service.CheckUserCookie(sessionid, csrfToken)
		if user.ID == "" {
			// Todo: 构建login的url
			http.Redirect(responseWriter, request, "", http.StatusFound)
			return
		}
	}
}

func OnConnectHandler(s socketio.Conn) error {
	// 首次连接 1.获取当前用户的信息
	logger.Debug("OnConnectHandler")
	cookies := strings.Split(s.RemoteHeader().Get("Cookie"), ";")
	var csrfToken string
	var sessionid string
	var remoteIP string
	for _, line := range cookies {
		if strings.Contains(line, "csrftoken") {
			csrfToken = strings.Split(line, "=")[1]
		}
		if strings.Contains(line, "sessionid") {
			sessionid = strings.Split(line, "=")[1]
		}
	}
	user := service.CheckUserCookie(sessionid, csrfToken)
	logger.Debug(user)
	remoteAddrs := s.RemoteHeader().Get("X-Forwarded-For")
	if remoteAddrs == "" {
		remoteIP = s.RemoteAddr().String()
	} else {
		remoteIP = strings.Split(remoteAddrs, ",")[0]
	}
	conn := &WebConn{Cid: s.ID(), Sock: s, Addr: remoteIP, User: user}
	cons.AddWebConn(s.ID(), conn)
	return nil

}

func OnErrorHandler(e error) {
	logger.Debug("OnError trigger")
	logger.Debug(e)
}

func OnHostHandler(s socketio.Conn, message HostMsg) {
	// secret 	uuid string
	logger.Debug("OnHost trigger")
	winSiz := ssh.Window{Height: 24, Width: 80}
	assetID := message.Uuid
	systemUserId := message.UserID
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
	asset := service.GetAsset(assetID)
	systemUser := service.GetSystemUser(systemUserId)

	if asset.Id == "" || systemUser.Id == "" {
		return
	}

	userR, userW := io.Pipe()

	conn := cons.GetWebConn(s.ID())
	clientConn := Client{Uuid: clientID, Cid: conn.Cid, user: conn.User,
		WinChan: make(chan ssh.Window, 100), Conn: s, UserRead: userR, UserWrite: userW}
	clientConn.WinChan <- winSiz
	conn.AddClient(clientID, &clientConn)

	// Todo: 构建proxy server 启动goroutine

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
	con := cons.GetWebConn(s.ID())
	con.User = currentUser

	asset := service.GetAsset(tokenUser.AssetId)
	systemUser := service.GetSystemUser(tokenUser.SystemUserId)

	if asset.Id == "" || systemUser.Id == "" {
		return
	}

	userR, userW := io.Pipe()
	conn := cons.GetWebConn(s.ID())
	clientConn := Client{Uuid: clientID, Cid: conn.Cid, user: conn.User,
		WinChan: make(chan ssh.Window, 100), Conn: s, UserRead: userR, UserWrite: userW}
	clientConn.WinChan <- winSiz
	conn.AddClient(clientID, &clientConn)

	// Todo: 构建proxy server 启动goroutine
}

func OnDataHandler(s socketio.Conn, message DataMsg) {
	logger.Debug("OnData trigger")
	cid := message.Room
	webconn := cons.GetWebConn(s.ID())
	client := webconn.GetClient(cid)
	_, _ = client.UserWrite.Write([]byte(message.Data))
}

func OnResizeHandler(s socketio.Conn, message ReSizeMsg) {
	winSize := ssh.Window{Height: message.Height, Width: message.Width}
	logger.Debugf("On resize event trigger: %s*%s", message.Width, message.Height)
	con := cons.GetWebConn(s.ID())
	con.SetWinSize(winSize)
}

func OnLogoutHandler(s socketio.Conn, message string) {
	logger.Debug("OnLogout trigger")
	webConn := cons.GetWebConn(s.ID())
	client := webConn.GetClient(message)
	_ = client.Close()
}
