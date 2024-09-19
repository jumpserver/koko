package httpd

import (
	"html/template"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/gin-gonic/gin"

	assets "github.com/jumpserver/koko"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
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

func getUIAssetFs() http.FileSystem {
	uiAssetFs, err := fs.Sub(assets.UIFs, "ui/dist/assets")
	if err != nil {
		logger.Debugf("Get ui asset fs error: %s", err)
		return &StaticFSWrapper{
			FileSystem:   http.Dir("./ui/dist/assets"),
			FixedModTime: time.Now(),
		}
	}
	return &StaticFSWrapper{
		FileSystem:   http.FS(uiAssetFs),
		FixedModTime: time.Now(),
	}
}

func createRouter(jmsService *service.JMService, webSrv *Server) *gin.Engine {
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
	kokoGroup.StaticFS("/assets", getUIAssetFs())
	kokoGroup.StaticFileFS("/favicon.ico", "ui/dist/favicon.ico", http.FS(assets.UIFs))
	kokoGroup.GET("/health/", webSrv.HealthStatusHandler)
	wsGroup := kokoGroup.Group("/ws/")
	{
		wsGroup.Group("/terminal").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessTerminalWebsocket)

		wsGroup.Group("/elfinder").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ProcessElfinderWebsocket)

		wsGroup.Group("/chat/system").Use(
			auth.HTTPMiddleSessionAuth(jmsService)).GET("/", webSrv.ChatAIWebsocket)

	}

	connectGroup := kokoGroup.Group("/connect")
	connectGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		connectGroup.GET("/", func(ctx *gin.Context) {
			// https://github.com/gin-gonic/gin/issues/2654
			ctx.FileFromFS("ui/dist/", http.FS(assets.UIFs))
		})
	}
	shareGroup := kokoGroup.Group("/share")
	shareGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		shareGroup.GET("/:id/", func(ctx *gin.Context) {
			ctx.FileFromFS("ui/dist/", http.FS(assets.UIFs))
		})
	}

	monitorGroup := kokoGroup.Group("/monitor")
	monitorGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		monitorGroup.GET("/:id/", func(ctx *gin.Context) {
			ctx.FileFromFS("ui/dist/", http.FS(assets.UIFs))
		})
	}

	tokenGroup := kokoGroup.Group("/token")
	{
		tokenGroup.GET("/", func(ctx *gin.Context) {
			ctx.FileFromFS("ui/dist/", http.FS(assets.UIFs))
		})

		tokenGroup.GET("/:id/", func(ctx *gin.Context) {
			ctx.FileFromFS("ui/dist/", http.FS(assets.UIFs))
		})
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

	k8sGroup := kokoGroup.Group("/k8s")
	k8sGroup.Use(auth.HTTPMiddleSessionAuth(jmsService))
	{
		k8sGroup.GET("/", func(ctx *gin.Context) {
			// https://github.com/gin-gonic/gin/issues/2654
			ctx.FileFromFS("ui/dist/", http.FS(assets.UIFs))
		})
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
