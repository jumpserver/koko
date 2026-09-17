package httpd

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

type graphicalShare interface {
	Share(*gin.Context)
}

func (s *Server) ProcessShareWebsocket(ctx *gin.Context) {
	query := ctx.Request.URL.Query()
	component := query.Get("component")
	if (component != "koko" && component != "lion") || query.Get("target_id") == "" || query.Get("code") == "" {
		ctx.AbortWithStatus(http.StatusBadRequest)
		return
	}
	// The component only selects the transport. Core authorizes the actual session.
	query.Del("token")
	query.Set("type", TargetTypeShare)
	ctx.Request.URL.RawQuery = query.Encode()
	if component == "lion" {
		if s.lionShare == nil {
			ctx.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		ctx.Set(config.GinCtxUserKey, ctx.MustGet(auth.ContextKeyUser))
		s.lionShare.Share(ctx)
		return
	}

	userConn, err := s.UpgradeUserWsConn(ctx)
	if err != nil {
		logger.Errorf(WebsocketErrorf, err)
		return
	}
	defer userConn.conn.Close()
	userConn.envelopeProtocol = true
	record, err := auth.JoinShareRoom(ctx, s.apiClient)
	if err != nil {
		userConn.SendErrMessage(err.Error())
		return
	}
	defer func() {
		if err := s.apiClient.FinishShareRoom(record.ID); err != nil {
			logger.Errorf("Conn[%s] finish share room err: %s", userConn.Uuid, err)
		}
	}()
	userConn.ctx.Set(auth.ContextKeyShareRecord, record)
	s.runTTY(userConn)
}
