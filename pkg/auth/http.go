package auth

import (
	"fmt"
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
			logger.Errorf("Check user cookie failed: %+v %s", cookies, err.Error())
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
		switch ctx.ClientIP() {
		case "127.0.0.1", "localhost", "::1":
			return
		default:
			_ = ctx.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid host %s", ctx.ClientIP()))
			return
		}
	}
}
