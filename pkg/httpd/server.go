package httpd

/*
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
	sshWs.OnUpgradeError = neffosOnUpgradeError
	sshWs.OnConnect = neffosOnConnect
	sshWs.OnDisconnect = neffosOnDisconnect

	router := mux.NewRouter()
	fs := http.FileServer(http.Dir(filepath.Join(conf.RootPath, "static")))

	subRouter := router.PathPrefix("/koko/").Subrouter()
	subRouter.PathPrefix("/static/").Handler(http.StripPrefix("/koko/static/", fs))
	subRouter.Handle("/ws/", sshWs)
	subRouter.Handle("/room/{roomID}/", AuthDecorator(roomHandler))

	elfinderRouter := subRouter.PathPrefix("/elfinder/").Subrouter()
	elfinderRouter.HandleFunc("/sftp/{host}/", AuthDecorator(sftpHostFinder))
	elfinderRouter.HandleFunc("/sftp/", AuthDecorator(sftpFinder))
	elfinderRouter.HandleFunc("/sftp/connector/{host}/",
		AuthDecorator(sftpHostConnectorView),
	).Methods("GET", "POST")

	router.HandleFunc("/status/", statusHandler)

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

func statusHandler(wr http.ResponseWriter, req *http.Request) {
	status := make(map[string]interface{})
	data := websocketManager.GetWebsocketData()
	status["websocket"] = data
	wr.Header().Set("Content-Type", "application/json")
	jsonData, _ := json.Marshal(status)
	_, _ = wr.Write(jsonData)
}
*/