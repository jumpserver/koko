package koko

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"path/filepath"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/gorilla/mux"

	gorillaws "github.com/gorilla/websocket"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/handler"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/kataras/neffos"
	"github.com/kataras/neffos/gorilla"
	"github.com/pires/go-proxyproto"
	gossh "golang.org/x/crypto/ssh"
)

type Application struct {
	Conf *config.Config

	httpd    *http.Server
	neffosWs *neffos.Server
	sshd     *ssh.Server

	ctx        context.Context
	cancelFunc context.CancelFunc
}

func (a *Application) Run() {
	a.Initial()
	fmt.Println(time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Koko Version %s, more see https://www.jumpserver.org\n", Version)
	fmt.Println("Quit the server with CONTROL-C.")
	go a.startHttpServer()
	a.startSSHServer()
}

func (a *Application) startSSHServer() {
	logger.Infof("Start SSH server at %s", a.sshd.Addr)
	ln, err := net.Listen("tcp", a.sshd.Addr)
	if err != nil {
		logger.Fatal(err)
	}
	proxyListener := &proxyproto.Listener{Listener: ln}
	if err = a.sshd.Serve(proxyListener); err != nil {
		logger.Errorf("stop ssh err: %s", err)
	}
}

func (a *Application) startHttpServer() {
	logger.Info("Start HTTP server at ", a.httpd.Addr)
	if err := a.httpd.ListenAndServe(); err != nil {
		logger.Errorf("stop http server err: %s", err)
	}

}

func (a *Application) routers() *mux.Router {
	a.neffosWs = a.createWebsocket()
	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(a.Conf.RootPath, "static")))
	subRouter := router.PathPrefix("/koko/").Subrouter()
	subRouter.PathPrefix("/static/").Handler(http.StripPrefix("/koko/static/", fs))
	subRouter.Handle("/room/{roomID}/", a.AuthDecorator(a.roomHandler))
	subRouter.Handle("/ws/", a.neffosWs)

	elfinderRouter := subRouter.PathPrefix("/elfinder/").Subrouter()
	elfinderRouter.HandleFunc("/sftp/{host}/", a.AuthDecorator(a.sftpHostFinder))
	elfinderRouter.HandleFunc("/sftp/", a.AuthDecorator(a.sftpFinder))
	elfinderRouter.HandleFunc("/sftp/connector/{host}/",
		a.AuthDecorator(a.sftpHostConnectorView),
	).Methods("GET", "POST")

	router.HandleFunc("/status/", a.statusHandler)

	// add pprof hander
	pprofRouter(router)
	return router
}

func (a *Application) createWebsocket() *neffos.Server {
	var upgrader = gorilla.Upgrader(gorillaws.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	})
	var Timeout = time.Duration(60)

	var wsEvents = neffos.WithTimeout{
		ReadTimeout:  Timeout * time.Second,
		WriteTimeout: Timeout * time.Second,
		Namespaces: neffos.Namespaces{
			"ssh": neffos.Events{
				neffos.OnNamespaceConnected:  a.OnNamespaceConnected,
				neffos.OnNamespaceDisconnect: a.OnNamespaceDisconnect,
				neffos.OnRoomJoined: func(c *neffos.NSConn, msg neffos.Message) error {
					return nil
				},
				neffos.OnRoomLeft: func(c *neffos.NSConn, msg neffos.Message) error {
					return nil
				},

				"data":   a.OnDataHandler,
				"resize": a.OnResizeHandler,
				"host":   a.OnHostHandler,
				"logout": a.OnLogoutHandler,
				"token":  a.OnTokenHandler,
				"ping":   a.OnPingHandler,

				"shareRoom": a.OnShareRoom,
			},
			"elfinder": neffos.Events{
				neffos.OnNamespaceConnected:  a.OnELFinderConnect,
				neffos.OnNamespaceDisconnect: a.OnELFinderDisconnect,
				"ping":                       a.OnPingHandler,
			},
		},
	}
	neffosWs := neffos.New(upgrader, wsEvents)

	neffosWs.IDGenerator = func(w http.ResponseWriter, r *http.Request) string {
		return neffos.DefaultIDGenerator(w, r)
	}
	neffosWs.OnUpgradeError = a.neffosOnUpgradeError
	neffosWs.OnConnect = a.neffosOnConnect
	neffosWs.OnDisconnect = a.neffosOnDisconnect
	return neffosWs

}

func (a *Application) createHttpServer() {
	router := a.routers()
	addr := net.JoinHostPort(a.Conf.BindHost, a.Conf.HTTPPort)
	a.httpd = &http.Server{Addr: addr, Handler: router,
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
		IdleTimeout:  time.Second * 70,}
}

func (a *Application) createSshServer() {
	signer, err := gossh.ParsePrivateKey([]byte(a.Conf.HostKey))
	if err != nil {
		logger.Fatal("Load host key error: ", err)
	}
	addr := net.JoinHostPort(a.Conf.BindHost, a.Conf.SSHPort)
	sshd := &ssh.Server{
		Addr:                       addr,
		KeyboardInteractiveHandler: a.CheckMFA,
		PasswordHandler:            a.CheckUserPassword,
		PublicKeyHandler:           a.CheckUserPublicKey,
		NextAuthMethodsHandler:     a.MFAAuthMethods,
		HostSigners:                []ssh.Signer{signer},
		Handler:                    a.SessionHandler,
		SubsystemHandlers:          map[string]ssh.SubsystemHandler{},
	}
	sshd.SetSubsystemHandler("sftp", a.SftpHandler)
	a.sshd = sshd
}

func (a *Application) Close() {
	if a.neffosWs != nil {
		a.neffosWs.Close()
	}
	if a.httpd != nil {
		_ = a.httpd.Close()
	}
	if a.sshd != nil {
		_ = a.sshd.Close()
	}
}



func (a *Application) Initial() {
	a.ctx, a.cancelFunc = context.WithCancel(context.Background())
	handler.Initial()
	a.createHttpServer()
	a.createSshServer()
}

func pprofRouter(router *mux.Router) {
	debugRouter := router.PathPrefix("/debug/pprof").Subrouter()
	debugRouter.HandleFunc("/", pprof.Index)
	debugRouter.HandleFunc("/cmdline", pprof.Cmdline)
	debugRouter.HandleFunc("/profile", pprof.Profile)
	debugRouter.HandleFunc("/symbol", pprof.Symbol)
	debugRouter.HandleFunc("/trace", pprof.Trace)
	debugRouter.HandleFunc("/block", pprof.Handler("block").ServeHTTP)
	debugRouter.HandleFunc("/goroutine", pprof.Handler("goroutine").ServeHTTP)
	debugRouter.HandleFunc("/heap", pprof.Handler("heap").ServeHTTP)
	debugRouter.HandleFunc("/mutex", pprof.Handler("mutex").ServeHTTP)
	debugRouter.HandleFunc("/threadcreate", pprof.Handler("threadcreate").ServeHTTP)
}
