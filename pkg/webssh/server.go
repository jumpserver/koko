package webssh

import (
	"net/http"
	"strconv"

	socketio "github.com/googollee/go-socket.io"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"
)

var (
	conf       = config.Conf
	httpServer *http.Server
)

func StartWebsocket() {
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
	logger.Debug("start Websocket Serving")
	httpServer = &http.Server{Addr: conf.BindHost + ":" + strconv.Itoa(conf.SSHPort), Handler: nil}
	logger.Fatal(httpServer.ListenAndServe())

}

func OnConnectHandler(s socketio.Conn) error {
	return nil

}
func OnErrorHandler(e error) {

}

func OnHostHandler(s socketio.Conn, message HostMsg) {

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
