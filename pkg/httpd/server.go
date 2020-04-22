package httpd

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/pprof"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/kataras/neffos"
)

var (
	httpServer *http.Server
	Timeout    = time.Duration(60)
)

func StartHTTPServer() {
	conf := config.GetConf()
	sshWs := neffos.New(upgrader, wsEvents)
	sshWs.IDGenerator = func(w http.ResponseWriter, r *http.Request) string {
		return neffos.DefaultIDGenerator(w, r)
	}
	sshWs.OnUpgradeError = NeffosOnUpgradeError
	sshWs.OnConnect = NeffosOnConnect
	sshWs.OnDisconnect = NeffosOnDisconnect

	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))

	subRouter := router.PathPrefix("/koko/").Subrouter()
	subRouter.PathPrefix("/static/").Handler(http.StripPrefix("/koko/static/", fs))
	subRouter.Handle("/ws/", sshWs)
	subRouter.Handle("/room/{roomID}/", AuthDecorator(RoomHandler))

	elfinderRouter := subRouter.PathPrefix("/elfinder/").Subrouter()
	elfinderRouter.HandleFunc("/sftp/{host}/", AuthDecorator(SftpHostFinder))
	elfinderRouter.HandleFunc("/sftp/", AuthDecorator(SftpFinder))
	elfinderRouter.HandleFunc("/sftp/connector/{host}/",
		AuthDecorator(SftpHostConnectorView),
	).Methods("GET", "POST")

	router.HandleFunc("/status/", StatusHandler)

	if strings.ToUpper(conf.LogLevel) == "DEBUG" {
		router.PathPrefix("/debug/pprof/").HandlerFunc(pprof.Index)
	}
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	logger.Info("Start HTTP server at ", addr)
	httpServer = &http.Server{Addr: addr, Handler: router,}
	logger.Fatal(httpServer.ListenAndServe())
}

func StopHTTPServer() {
	_ = httpServer.Close()
}

func StatusHandler(wr http.ResponseWriter, req *http.Request) {
	status := make(map[string]interface{})
	data := websocketManager.GetWebsocketData()
	status["websocket"] = data
	wr.Header().Set("Content-Type", "application/json")
	jsonData, _ := json.Marshal(status)
	_, _ = wr.Write(jsonData)
}
