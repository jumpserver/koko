package koko

import (
	"net"
	"net/http"
	"net/http/pprof"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"

)

func registerWebHandlers(jmsService *service.JMService, webSrv *httpd.Server) {
	if config.GlobalConfig.LogLevel != "DEBUG" {
		gin.SetMode(gin.ReleaseMode)
	}
	eng := gin.New()

	eng.Use(gin.Recovery())
	eng.Use(gin.Logger())
	eng.LoadHTMLGlob("./templates/**/*")
	rootGroup := eng.Group("")
	kokoGroup := rootGroup.Group("/koko")
	kokoGroup.Static("/static/", "./static")
	kokoGroup.GET("/health/", webSrv.HealthStatusHandler)

	wsGroup := kokoGroup.Group("/ws/")
	{
		wsGroup.Group("/terminal").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessTerminalWebsocket)

		wsGroup.Group("/elfinder").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessElfinderWebsocket)

		wsGroup.Group("/token").GET("/", webSrv.ProcessTokenWebsocket)
	}

	terminalGroup := kokoGroup.Group("/terminal")
	terminalGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		terminalGroup.GET("/", func(ctx *gin.Context) {
			ctx.HTML(http.StatusOK, "terminal.html", nil)
		})
	}

	tokenGroup := kokoGroup.Group("/token")
	{

		tokenGroup.GET("/", func(ctx *gin.Context) {
			ctx.HTML(http.StatusOK, "terminal.html", nil)
		})
	}

	elfindlerGroup := kokoGroup.Group("/elfinder")
	elfindlerGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		elfindlerGroup.GET("/sftp/", func(ctx *gin.Context) {
			ctx.HTML(http.StatusOK, "file_manager.html", "_")
		})
		elfindlerGroup.GET("/sftp/:host/", func(ctx *gin.Context) {
			hostId := ctx.Param("host")
			if ok := common.ValidUUIDString(hostId); !ok {
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			ctx.HTML(http.StatusOK, "file_manager.html", hostId)
		})
		elfindlerGroup.Any("/connector/:host/", webSrv.SftpHostConnectorView)
	}

	debugGroup := rootGroup.Group("/debug/pprof")
	debugGroup.Use(auth.HTTPMiddleDebugAuth())
	{
		debugGroup.GET("/", gin.WrapF(pprof.Index))
		debugGroup.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		debugGroup.GET("/profile", gin.WrapF(pprof.Profile))
		debugGroup.POST("/symbol", gin.WrapF(pprof.Symbol))
		debugGroup.GET("/symbol", gin.WrapF(pprof.Symbol))
		debugGroup.GET("/trace", gin.WrapF(pprof.Trace))
		debugGroup.GET("/allocs", gin.WrapF(pprof.Handler("allocs").ServeHTTP))
		debugGroup.GET("/block", gin.WrapF(pprof.Handler("block").ServeHTTP))
		debugGroup.GET("/goroutine", gin.WrapF(pprof.Handler("goroutine").ServeHTTP))
		debugGroup.GET("/heap", gin.WrapF(pprof.Handler("heap").ServeHTTP))
		debugGroup.GET("/mutex", gin.WrapF(pprof.Handler("mutex").ServeHTTP))
		debugGroup.GET("/threadcreate", gin.WrapF(pprof.Handler("threadcreate").ServeHTTP))
	}
	conf := config.GetConf()
	addr := net.JoinHostPort(conf.BindHost, conf.HTTPPort)
	webSrv.Srv = &http.Server{
		Addr:    addr,
		Handler: eng,
	}
}
