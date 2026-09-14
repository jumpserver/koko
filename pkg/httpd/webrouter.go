package httpd

import (
	"net/http/pprof"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion"
	"github.com/jumpserver/koko/pkg/lion/middleware"
)

func createRouter(
	jmsService *service.JMService,
	webSrv *Server,
	lionRuntime *lion.Runtime,
) *gin.Engine {
	if lionRuntime != nil {
		webSrv.lionMonitor = lionRuntime
	}
	if config.GlobalConfig.LogLevel != "DEBUG" {
		gin.SetMode(gin.ReleaseMode)
	}
	eng := gin.New()
	eng.Use(gin.Recovery())
	eng.Use(gin.Logger())
	kokoGroup := eng.Group("/koko")
	kokoGroup.GET("/health/", webSrv.HealthStatusHandler)
	wsGroup := kokoGroup.Group("/ws/")
	{
		wsGroup.Group("/terminal").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessTerminalWebsocket)
		wsGroup.Group("/monitor").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessMonitorWebsocket)

		wsGroup.Group("/elfinder").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessElfinderWebsocket)

		wsGroup.Group("/sftp").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessSftpWebsocket)

	}

	apiGroup := kokoGroup.Group("/api")
	apiGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		apiGroup.POST("/connect-ticket/", webSrv.CreateConnectTicket)
		apiGroup.GET("/monitor/:sid/", middleware.CORS(), webSrv.MonitorComponent)
	}
	elfinderGroup := kokoGroup.Group("/elfinder")
	elfinderGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		elfinderGroup.Any("/connector/:host/", webSrv.SftpHostConnectorView)
	}
	if lionRuntime != nil {
		lionRuntime.RegisterRoutes(eng)
	}

	debugGroup := eng.Group("/debug/pprof")
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
	return eng
}
