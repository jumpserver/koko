package webssh

import (
	"net/http"
	"strconv"
	"sync"

	socketio "github.com/googollee/go-socket.io"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"
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
	logger.Debug("start HTTP Serving ", conf.HTTPPort)
	httpServer = &http.Server{Addr: conf.BindHost + ":" + strconv.Itoa(conf.HTTPPort), Handler: nil}
	logger.Fatal(httpServer.ListenAndServe())

}
