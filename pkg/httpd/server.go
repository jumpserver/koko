package httpd

import (
	"net/http"
	"path/filepath"
	"sync"

	socketio "github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"

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
	server.OnConnect("/ssh", OnConnectHandler)
	server.OnDisconnect("/ssh", OnDisconnect)
	server.OnError("/ssh", OnErrorHandler)
	server.OnEvent("/ssh", "host", OnHostHandler)
	server.OnEvent("/ssh", "token", OnTokenHandler)
	server.OnEvent("/ssh", "data", OnDataHandler)
	server.OnEvent("/ssh", "resize", OnResizeHandler)
	server.OnEvent("/ssh", "logout", OnLogoutHandler)

	server.OnConnect("/elfinder", OnElfinderConnect)
	server.OnDisconnect("/elfinder", OnElfinderDisconnect)

	go server.Serve()
	defer server.Close()

	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))
	router := mux.NewRouter()

	router.Handle("/socket.io/", server)
	router.Handle("/coco/elfinder/sftp/{host}/", http.HandlerFunc(sftpHostFinder))
	router.Handle("/coco/elfinder/sftp/", http.HandlerFunc(sftpFinder))
	router.Handle("/coco/elfinder/sftp/connector/{host}/",
		http.HandlerFunc(sftpHostConnectorView)).Methods("GET", "POST")

	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	logger.Debug("start HTTP Serving ", conf.HTTPPort)
	httpServer = &http.Server{Addr: conf.BindHost + ":" + conf.HTTPPort, Handler: router}
	logger.Fatal(httpServer.ListenAndServe())
}
