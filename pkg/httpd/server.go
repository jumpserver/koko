package httpd

import (
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"net/http/pprof"

	"github.com/gorilla/mux"
	gorillaws "github.com/gorilla/websocket"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/kataras/neffos"
	"github.com/kataras/neffos/gorilla"
)

var (
	httpServer *http.Server
	Timeout    = time.Duration(60)
)

var upgrader = gorilla.Upgrader(gorillaws.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
})

var wsEvents = neffos.WithTimeout{
	ReadTimeout:  Timeout * time.Second,
	WriteTimeout: Timeout * time.Second,
	Namespaces: neffos.Namespaces{
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
			"ping":   OnPingHandler,
		},
		"elfinder": neffos.Events{
			neffos.OnNamespaceConnected:  OnELFinderConnect,
			neffos.OnNamespaceDisconnect: OnELFinderDisconnect,
			"ping":                       OnPingHandler,
		},
	},
}

func StartHTTPServer() {
	conf := config.GetConf()
	sshWs := neffos.New(upgrader, wsEvents)
	sshWs.IDGenerator = func(w http.ResponseWriter, r *http.Request) string {
		return neffos.DefaultIDGenerator(w, r)
	}
	sshWs.OnUpgradeError = func(err error) {
		if ok := neffos.IsTryingToReconnect(err); ok {
			logger.Debug("A client was tried to reconnect")
			return
		}
		logger.Error("ERROR: ", err)
	}
	sshWs.OnConnect = func(c *neffos.Conn) error {
		if c.WasReconnected() {
			logger.Debugf("Connection %s reconnected, with tries: %d", c.ID(), c.ReconnectTries)
		} else {
			logger.Debugf("A new ws %s connection arrive", c.ID())
		}
		return nil
	}
	sshWs.OnDisconnect = func(c *neffos.Conn) {
		logger.Debug("Ws connection disconnect")
	}

	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))

	subRouter := router.PathPrefix("/koko/").Subrouter()
	subRouter.PathPrefix("/static/").Handler(http.StripPrefix("/koko/static/", fs))
	subRouter.Handle("/ws/", sshWs)

	elfinderRouter := subRouter.PathPrefix("/elfinder/").Subrouter()
	elfinderRouter.HandleFunc("/sftp/{host}/", AuthDecorator(sftpHostFinder))
	elfinderRouter.HandleFunc("/sftp/", AuthDecorator(sftpFinder))
	elfinderRouter.HandleFunc("/sftp/connector/{host}/",
		AuthDecorator(sftpHostConnectorView),
	).Methods("GET", "POST")

	//router.PathPrefix("/coco/static/").Handler(http.StripPrefix("/coco/static/", fs))

	//router.Handle("/socket.io/", sshWs)
	//router.HandleFunc("/coco/elfinder/sftp/{host}/", AuthDecorator(sftpHostFinder))
	//router.HandleFunc("/coco/elfinder/sftp/", AuthDecorator(sftpFinder))
	//router.HandleFunc("/coco/elfinder/sftp/connector/{host}/",
	//	AuthDecorator(sftpHostConnectorView)).Methods("GET", "POST")
	if strings.ToUpper(conf.LogLevel) == "DEBUG" {
		router.PathPrefix("/debug/pprof/").HandlerFunc(pprof.Index)
	}
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	logger.Info("Start HTTP server at ", addr)
	httpServer = &http.Server{Addr: addr, Handler: router}
	logger.Fatal(httpServer.ListenAndServe())
}

func StopHTTPServer() {
	_ = httpServer.Close()
}
