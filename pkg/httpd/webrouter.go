package httpd

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver-dev/sdk-go/service"
	assets "github.com/jumpserver/koko"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion"
	"github.com/jumpserver/koko/pkg/logger"
)

func getStaticFS() http.FileSystem {
	staticFs, err := fs.Sub(assets.StaticFs, "static")
	if err != nil {
		logger.Debugf("Get static fs error: %s", err)
		staticDir := http.Dir("./static/")
		return &StaticFSWrapper{
			FileSystem:   staticDir,
			FixedModTime: time.Now(),
		}
	}
	return &StaticFSWrapper{
		FileSystem:   http.FS(staticFs),
		FixedModTime: time.Now(),
	}

}

func createRouter(
	jmsService *service.JMService,
	webSrv *Server,
	lionRuntime *lion.Runtime,
) *gin.Engine {
	if config.GlobalConfig.LogLevel != "DEBUG" {
		gin.SetMode(gin.ReleaseMode)
	}
	eng := gin.New()
	eng.Use(gin.Recovery())
	eng.Use(gin.Logger())
	kokoGroup := eng.Group("/koko")
	templ := template.Must(template.New("").ParseFS(assets.TemplateFs,
		"templates/elfinder/*.html"))
	eng.SetHTMLTemplate(templ)
	kokoGroup.StaticFS("/static/", getStaticFS())
	kokoGroup.GET("/health/", webSrv.HealthStatusHandler)
	wsGroup := kokoGroup.Group("/ws/")
	{
		wsGroup.Group("/terminal").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessTerminalWebsocket)

		wsGroup.Group("/elfinder").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessElfinderWebsocket)

		wsGroup.Group("/sftp").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessSftpWebsocket)

	}

	apiGroup := kokoGroup.Group("/api")
	apiGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		apiGroup.POST("/connect-ticket/", webSrv.CreateConnectTicket)
	}
	elfinderGroup := kokoGroup.Group("/elfinder")
	elfinderGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		elfinderGroup.GET("/sftp/", func(ctx *gin.Context) {
			metaData := webSrv.GenerateViewMeta("_")
			ctx.HTML(http.StatusOK, "file_manager.html", metaData)
		})
		elfinderGroup.GET("/sftp/:host/", func(ctx *gin.Context) {
			hostId := ctx.Param("host")
			if ok := common.ValidUUIDString(hostId); !ok {
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			metaData := webSrv.GenerateViewMeta(hostId)
			ctx.HTML(http.StatusOK, "file_manager.html", metaData)
		})
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
