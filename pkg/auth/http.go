package auth

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

func HTTPMiddleSessionAuth(jmsService *service.JMService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var (
			err  error
			user *model.User
		)
		reqCookies := ctx.Request.Cookies()
		var cookies = make(map[string]string)
		for _, cookie := range reqCookies {
			cookies[cookie.Name] = cookie.Value
		}
		user, err = jmsService.CheckUserCookie(cookies)
		if err != nil {
			logger.Error("Check user cookie failed")
			loginUrl := fmt.Sprintf("/core/auth/login/?next=%s", url.QueryEscape(ctx.Request.URL.RequestURI()))
			ctx.Redirect(http.StatusFound, loginUrl)
			ctx.Abort()
			return
		}
		ctx.Set(ContextKeyUser, user)
	}
}

func HTTPMiddleDebugAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		host, _, err := net.SplitHostPort(ctx.Request.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() ||
			ctx.GetHeader("X-Forwarded-For") != "" || ctx.GetHeader("X-Real-IP") != "" ||
			ctx.GetHeader("Forwarded") != "" {
			ctx.AbortWithStatus(http.StatusForbidden)
			return
		}
	}
}
