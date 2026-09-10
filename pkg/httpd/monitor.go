package httpd

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
)

type graphicalMonitor interface {
	HasSession(context.Context, string) (bool, error)
	Monitor(*gin.Context)
}

func (s *Server) monitorComponent(ctx *gin.Context, sid string) string {
	user, ok := ctx.Get(auth.ContextKeyUser)
	if !ok {
		ctx.AbortWithStatus(http.StatusUnauthorized)
		return ""
	}
	if sid == "" {
		ctx.AbortWithStatus(http.StatusBadRequest)
		return ""
	}
	permission, err := s.apiClient.ValidateJoinSessionPermission(user.(*model.User).ID, sid)
	if err != nil {
		ctx.AbortWithStatus(http.StatusBadGateway)
		return ""
	}
	if !permission.Ok {
		ctx.AbortWithStatus(http.StatusForbidden)
		return ""
	}
	if s.lionMonitor != nil {
		found, err := s.lionMonitor.HasSession(ctx.Request.Context(), sid)
		if err != nil {
			ctx.AbortWithStatus(http.StatusServiceUnavailable)
			return ""
		}
		if found {
			return "lion"
		}
	}
	return "koko"
}

func (s *Server) MonitorComponent(ctx *gin.Context) {
	if component := s.monitorComponent(ctx, ctx.Param("sid")); component != "" {
		ctx.JSON(http.StatusOK, gin.H{"component": component})
	}
}

func (s *Server) ProcessMonitorWebsocket(ctx *gin.Context) {
	query := ctx.Request.URL.Query()
	sid := query.Get("target_id")
	component := s.monitorComponent(ctx, sid)
	if component == "" {
		return
	}
	// The monitor entry always joins an existing session, regardless of client parameters.
	query.Del("token")
	query.Set("type", TargetTypeMonitor)
	query.Set("target_id", sid)
	ctx.Request.URL.RawQuery = query.Encode()
	if component == "lion" {
		ctx.Set(config.GinCtxUserKey, ctx.MustGet(auth.ContextKeyUser))
		s.lionMonitor.Monitor(ctx)
		return
	}
	s.ProcessTerminalWebsocket(ctx)
}
