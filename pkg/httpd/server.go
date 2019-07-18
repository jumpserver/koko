package httpd

import (
	"net"
	"net/http"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/kataras/neffos"
	"github.com/kataras/neffos/gorilla"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var (
	httpServer *http.Server
)

var wsEvents = neffos.Namespaces{
	"ssh": neffos.Events{
		neffos.OnNamespaceConnected:  OnNamespaceConnected,
		neffos.OnNamespaceDisconnect: OnNamespaceDisconnect,
		neffos.OnRoomJoined: func(c *neffos.NSConn, msg neffos.Message) error {
			return nil
		},
		neffos.OnRoomLeft: func(c *neffos.NSConn, msg neffos.Message) error {
			return nil
		},

		"data":   OnDataHandler,
		"resize": OnResizeHandler,
		"host":   OnHostHandler,
		"logout": OnLogoutHandler,
		"token":  OnTokenHandler,
	},
	"elfinder": neffos.Events{
		neffos.OnNamespaceConnected:  OnELFinderConnect,
		neffos.OnNamespaceDisconnect: OnELFinderDisconnect,
	},
}

func StartHTTPServer() {
	conf := config.GetConf()
	sshWs := neffos.New(gorilla.DefaultUpgrader, wsEvents)
	sshWs.IDGenerator = func(w http.ResponseWriter, r *http.Request) string {
		return neffos.DefaultIDGenerator(w, r)
	}
	sshWs.OnUpgradeError = func(err error) {
	}

	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))
	router.PathPrefix("/coco/static/").Handler(http.StripPrefix("/static/", fs))

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
