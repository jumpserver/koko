package httpd

import (
	"net/http"
	"strconv"
	"sync"

	socketio "github.com/googollee/go-socket.io"

	"cocogo/pkg/config"
	"cocogo/pkg/logger"
)

var (
	httpServer *http.Server
	conns      = &connections{container: make(map[string]*WebConn), mu: new(sync.RWMutex)}
)

func StartHTTPServer() {
	conf := config.GetConf()
	server, err := socketio.NewServer(nil)
	if err != nil {
		logger.Fatal(err)
	}
	server.OnConnect("/ssh", TestOnConnectHandler)
	server.OnError("/ssh", OnErrorHandler)
	server.OnEvent("/ssh", "host", TestOnHostHandler)
	server.OnEvent("/ssh", "token", OnTokenHandler)
	server.OnEvent("/ssh", "data", TestOnDataHandler)
	server.OnEvent("/ssh", "resize", TestOnResizeHandler)
	server.OnEvent("/ssh", "logout", TestOnLogoutHandler)

	go server.Serve()
	defer server.Close()

	http.Handle("/socket.io/", server)
	logger.Debug("start HTTP Serving ", conf.HTTPPort)
	httpServer = &http.Server{Addr: conf.BindHost + ":" + strconv.Itoa(conf.HTTPPort), Handler: nil}
	logger.Fatal(httpServer.ListenAndServe())
}
