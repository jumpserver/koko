package httpd

import (
	"github.com/kataras/neffos"
	"github.com/kataras/neffos/gorilla"
	"net"
	"net/http"
	"path/filepath"

	"github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var (
	httpServer *http.Server
)

func StartHTTPServer() {
	conf := config.GetConf()
	sshWs := neffos.New(gorilla.DefaultUpgrader, sshEvents)
	sshWs.IDGenerator = func(w http.ResponseWriter, r *http.Request) string {
		return neffos.DefaultIDGenerator(w, r)
	}
	sshWs.OnUpgradeError = func(err error) {
	}

	server, err := socketio.NewServer(nil)
	if err != nil {
		logger.Fatal(err)
	}
	server.OnConnect("/elfinder", OnELFinderConnect)
	server.OnDisconnect("/elfinder", OnELFinderDisconnect)
	server.OnError("/elfiner", OnErrorHandler)
	server.OnDisconnect("", SocketDisconnect)
	server.OnError("", OnErrorHandler)

	go server.Serve()
	defer server.Close()

	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	router.Handle("/socket.io/", sshWs)
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
	clients.DeleteClient(s.ID())
	logger.Debug("clean disconnect")
}
