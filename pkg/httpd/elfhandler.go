package httpd

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/LeeEirc/elfinder"
	socketio "github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"

	"cocogo/pkg/cctx"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
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
	u := s.URL()
	sid := u.Query().Get("sid")
	data := EmitSidMsg{Sid: sid}
	s.Emit("data", data)
	return nil
}

func OnELFinderDisconnect(s socketio.Conn, msg string) {
	u := s.URL()
	sid := u.Query().Get("sid")
	log.Println("disconnect: ", sid)
	log.Println("disconnect msg ", msg)
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
	logger.Debugf("user: %s; remote ip: %s; create connector", user.Name, remoteIP)
	con := elfinder.NewElFinderConnector([]elfinder.Volume{&elfinder.DefaultVolume})
	con.ServeHTTP(wr, req)
}
