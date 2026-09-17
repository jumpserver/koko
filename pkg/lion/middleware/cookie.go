package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"

	"github.com/jumpserver-dev/sdk-go/service"
)

func JmsCookieAuth(jmsService *service.JMService) gin.HandlerFunc {
	authenticate := auth.HTTPMiddleSessionAuth(jmsService)
	return func(ctx *gin.Context) {
		authenticate(ctx)
		if ctx.IsAborted() {
			return
		}
		if user, ok := ctx.Get(auth.ContextKeyUser); ok {
			ctx.Set(config.GinCtxUserKey, user)
		}
	}
}
