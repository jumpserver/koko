package httpd

import (
	"net"
	"net/http"
	"path/filepath"

	socketio "github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"

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

	server.OnConnect("/elfinder", OnELFinderConnect)
	server.OnDisconnect("/elfinder", OnELFinderDisconnect)
	server.OnDisconnect("", SocketDisconnect)

	go server.Serve()
	defer server.Close()

	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	router.Handle("/socket.io/", server)
	router.HandleFunc("/coco/elfinder/sftp/{host}/", AuthDecorator(sftpHostFinder))
	router.HandleFunc("/coco/elfinder/sftp/", AuthDecorator(sftpFinder))
	router.HandleFunc("/coco/elfinder/sftp/connector/{host}/",
		AuthDecorator(sftpHostConnectorView)).Methods("GET", "POST")

	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	logger.Debug("Start HTTP server at ", addr)
	httpServer = &http.Server{Addr: addr, Handler: router}
	logger.Fatal(httpServer.ListenAndServe())
}

func StopHTTPServer() {
	_ = httpServer.Close()
}

func SocketDisconnect(s socketio.Conn, msg string) {
	removeUserVolume(s.ID())
	conns.DeleteWebConn(s.ID())
	logger.Debug("clean disconnect")
}
