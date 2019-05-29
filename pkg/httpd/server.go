package httpd

import (
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"github.com/googollee/go-engine.io"
	"github.com/googollee/go-socket.io"
	"github.com/satori/go.uuid"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var (
	httpServer *http.Server
	conns      = &connections{container: make(map[string]*WebConn), mu: new(sync.RWMutex)}
)

type UUIDSessionIDGenerator struct {
}

func (u *UUIDSessionIDGenerator) NewID() string {
	return strings.Split(uuid.NewV4().String(), "-")[4]
}

func StartHTTPServer() {
	conf := config.GetConf()
	option := engineio.Options{}
	server, err := socketio.NewServer(&option)
	if err != nil {
		logger.Fatal(err)
	}
	server.OnConnect("/ssh", OnConnectHandler)
	server.OnDisconnect("/ssh", OnDisconnect)
	server.OnError("/ssh", OnErrorHandler)
	server.OnEvent("/ssh", "host", OnHostHandler)
	//server.OnEvent("/ssh", "token", OnTokenHandler)
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
