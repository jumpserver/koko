package httpd

import (
	"github.com/googollee/go-socket.io"
	"net"
	"net/http"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"
)

var (
	httpServer *http.Server
)

func StartHTTPServer() {
	conf := config.GetConf()
	server, err := socketio.NewServer(nil)
	if err != nil {
		logger.Fatal(err)
	}
	server.OnConnect("/ssh", OnConnectHandler)
	server.OnDisconnect("/ssh", OnDisconnect)
	server.OnError("/ssh", OnErrorHandler)
	server.OnEvent("/ssh", "host", OnHostHandler)
	server.OnEvent("/ssh", "token", OnTokenHandler)
	server.OnEvent("/ssh", "data", OnDataHandler)
	server.OnEvent("/ssh", "resize", OnResizeHandler)
	server.OnEvent("/ssh", "logout", OnLogoutHandler)

	go server.Serve()
	defer server.Close()

	http.Handle("/socket.io/", server)
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	logger.Debug("Start HTTP server at ", addr)
	httpServer = &http.Server{Addr: addr, Handler: nil}
	logger.Fatal(httpServer.ListenAndServe())
}
