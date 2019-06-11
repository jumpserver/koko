package httpd

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/LeeEirc/elfinder"
	socketio "github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"

	"koko/pkg/cctx"
	"koko/pkg/logger"
	"koko/pkg/model"
	"koko/pkg/service"
)

func AuthDecorator(handler http.HandlerFunc) http.HandlerFunc {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		cookies := strings.Split(request.Header.Get("Cookie"), ";")
		var csrfToken string
		var sessionid string
		var remoteIP string
		for _, line := range cookies {
			if strings.Contains(line, "csrftoken") {
				csrfToken = strings.Split(line, "=")[1]
			}
			if strings.Contains(line, "sessionid") {
				sessionid = strings.Split(line, "=")[1]
			}
		}
		user, err := service.CheckUserCookie(sessionid, csrfToken)
		if err != nil {
			loginUrl := fmt.Sprintf("/users/login/?next=%s", request.URL.Path)
			http.Redirect(responseWriter, request, loginUrl, http.StatusFound)
			return
		}
		xForwardFors := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
		if len(xForwardFors) >= 1 {
			remoteIP = xForwardFors[0]
		} else {
			remoteIP = strings.Split(request.RemoteAddr, ":")[0]
		}
		ctx := context.WithValue(request.Context(), cctx.ContextKeyUser, user)
		ctx = context.WithValue(ctx, cctx.ContextKeyRemoteAddr, remoteIP)
		handler(responseWriter, request.WithContext(ctx))
	}
}

func OnELFinderConnect(s socketio.Conn) error {
	data := EmitSidMsg{Sid: s.ID()}
	s.Emit("data", data)
	return nil
}

func OnELFinderDisconnect(s socketio.Conn, msg string) {
	logger.Debug("disconnect: ", s.ID())
	logger.Debug("disconnect msg ", msg)
	removeUserVolume(s.ID())
}

func sftpHostFinder(wr http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	tmpl := template.Must(template.ParseFiles("./templates/elfinder/file_manager.html"))
	hostID := vars["host"]
	_ = tmpl.Execute(wr, hostID)
}

func sftpFinder(wr http.ResponseWriter, req *http.Request) {
	tmpl := template.Must(template.ParseFiles("./templates/elfinder/file_manager.html"))
	_ = tmpl.Execute(wr, "_")
}

func sftpHostConnectorView(wr http.ResponseWriter, req *http.Request) {
	user := req.Context().Value(cctx.ContextKeyUser).(*model.User)
	remoteIP := req.Context().Value(cctx.ContextKeyRemoteAddr).(string)
	switch req.Method {
	case "GET":
		if err := req.ParseForm(); err != nil {
			http.Error(wr, err.Error(), http.StatusBadRequest)
			return
		}
	case "POST":
		err := req.ParseMultipartForm(32 << 20)
		if err != nil {
			http.Error(wr, err.Error(), http.StatusBadRequest)
			return
		}
	}
	sid := req.Form.Get("sid")
	userV, ok := GetUserVolume(sid)
	if !ok {
		userV = NewUserVolume(user, remoteIP)
		addUserVolume(sid, userV)
	}
	logger.Debugf("sid: %s", sid)
	logger.Debug(userVolumes)
	con := elfinder.NewElFinderConnector([]elfinder.Volume{userV})
	con.ServeHTTP(wr, req)
}
