package koko

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

func (a *Application) roomHandler(wr http.ResponseWriter, req *http.Request) {
	httpd.RoomHandler(wr, req)
}

func (a *Application) sftpHostFinder(wr http.ResponseWriter, req *http.Request) {
	httpd.SftpHostFinder(wr, req)
}

func (a *Application) sftpFinder(wr http.ResponseWriter, req *http.Request) {
	httpd.SftpFinder(wr, req)
}

func (a *Application) sftpHostConnectorView(wr http.ResponseWriter, req *http.Request) {
	httpd.SftpHostConnectorView(wr, req)
}

func (a *Application) AuthDecorator(handler http.HandlerFunc) http.HandlerFunc {
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
		ctx := context.WithValue(request.Context(), model.ContextKeyUser, user)
		ctx = context.WithValue(ctx, model.ContextKeyRemoteAddr, remoteIP)
		handler(responseWriter, request.WithContext(ctx))
	}
}

func (a *Application) statusHandler(wr http.ResponseWriter, req *http.Request) {
	httpd.StatusHandler(wr, req)
}
