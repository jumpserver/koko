package webssh

import (
	"net/http"
	"strconv"

	socketio "github.com/googollee/go-socket.io"
	uuid "github.com/satori/go.uuid"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"

	"cocogo/pkg/service"
	"strings"
	"sync"
)

var (
	conf       = config.Conf
	httpServer *http.Server
	cons       = &connections{container: make(map[string]*WebConn), mu: new(sync.RWMutex)}
)

func StartHTTPServer() {
	server, err := socketio.NewServer(nil)
	if err != nil {
		logger.Fatal(err)
	}
	server.OnConnect("/ssh", OnConnectHandler)
	server.OnError("/ssh", OnErrorHandler)
	server.OnEvent("/ssh", "host", OnHostHandler)
	server.OnEvent("/ssh", "token", OnTokenHandler)
	server.OnEvent("/ssh", "data", OnDataHandler)
	server.OnEvent("/ssh", "resize", OnResizeHandler)
	server.OnEvent("/ssh", "logout", OnLogoutHandler)

	go server.Serve()
	defer server.Close()

	http.Handle("/socket.io/", server)
	logger.Debug("start HTTP Serving")
	httpServer = &http.Server{Addr: conf.BindHost + ":" + strconv.Itoa(conf.HTTPPort), Handler: nil}
	logger.Fatal(httpServer.ListenAndServe())

}

func OnConnectHandler(s socketio.Conn) error {
	// 首次连接 1.获取当前用户的信息
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

}

func OnHostHandler(s socketio.Conn, message HostMsg) {
	// secret 	uuid string
	//assetID := message.Uuid
	//systemUserId := message.UserID
	secret := message.Secret
	//width, height := message.Size[0], message.Size[1]

	clientID := uuid.NewV4().String()
	//asset := service.GetAsset(assetID)
	//systemUser := service.GetSystemUser(systemUserId)
	s.Emit("room", map[string]string{"room": clientID, "secret": secret})
}

func OnTokenHandler(s socketio.Conn, message TokenMsg) {

}

func OnDataHandler(s socketio.Conn, message DataMsg) {

}

func OnResizeHandler(s socketio.Conn, message ReSizeMsg) {

}

func OnLogoutHandler(s socketio.Conn, message string) {
	// message: room

}
