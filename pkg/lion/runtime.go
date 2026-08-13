package lion

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ginCookie "github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver-dev/sdk-go/service/panda"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/middleware"
	"github.com/jumpserver/koko/pkg/lion/session"
	"github.com/jumpserver/koko/pkg/lion/tunnel"
	"github.com/jumpserver/koko/pkg/logger"
)

type Runtime struct {
	jmsService    *service.JMService
	tunnelService *tunnel.GuacamoleTunnelServer
	startedAt     time.Time

	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
}

func NewRuntime(jmsService *service.JMService) *Runtime {
	cache := &tunnel.GuaTunnelCacheManager{GuaTunnelCache: newGuaTunnelCache()}
	pandaClientFactory := newPandaClientFactory(config.GetConf())
	tunnelService := &tunnel.GuacamoleTunnelServer{
		Cache:      cache,
		JmsService: jmsService,
		SessionService: &session.Server{
			JmsService:         jmsService,
			PandaClient:        pandaClientFactory(config.GetConf().PandaHost),
			PandaClientFactory: pandaClientFactory,
		},
	}
	return &Runtime{
		jmsService:    jmsService,
		tunnelService: tunnelService,
		startedAt:     time.Now(),
	}
}

func (r *Runtime) RegisterRoutes(engine *gin.Engine) {
	lionGroup := engine.Group("/lion")
	lionGroup.Use(middleware.CORS())
	lionGroup.OPTIONS("/*path", func(ctx *gin.Context) {
		ctx.Status(http.StatusNoContent)
	})
	cookieStore := ginCookie.NewStore([]byte(common.RandomStr(32)))
	lionGroup.Use(middleware.GinSessionAuth(cookieStore))
	lionGroup.GET("/health/", r.healthStatus)

	tokenGroup := lionGroup.Group("/token")
	tokenGroup.Use(middleware.SessionAuth(r.jmsService))
	tokenTunnels := tokenGroup.Group("/tunnels")
	tokenTunnels.GET("/:tid/streams/:index/:filename", r.tunnelService.DownloadFile)
	tokenTunnels.POST("/:tid/streams/:index/:filename", r.tunnelService.UploadFile)

	wsGroup := lionGroup.Group("/ws")
	wsGroup.Group("/connect").Use(
		middleware.JmsCookieAuth(r.jmsService)).GET("/", r.tunnelService.Connect)
	wsGroup.Group("/monitor").Use(
		middleware.JmsCookieAuth(r.jmsService)).GET("/", r.tunnelService.Monitor)
	wsGroup.Group("/share").Use(
		middleware.JmsCookieAuth(r.jmsService)).GET("/", r.tunnelService.Share)
	wsGroup.Group("/token").Use(
		middleware.SessionAuth(r.jmsService)).GET("/", r.tunnelService.Connect)

	apiGroup := lionGroup.Group("/api")
	apiGroup.Use(middleware.JmsCookieAuth(r.jmsService))
	apiGroup.GET("/tunnels/:tid/streams/:index/:filename", r.tunnelService.DownloadFile)
	apiGroup.POST("/tunnels/:tid/streams/:index/:filename", r.tunnelService.UploadFile)
	apiGroup.POST("/share/", r.tunnelService.CreateShare)
	apiGroup.POST("/share/remove/", r.tunnelService.DeleteShare)
	apiGroup.POST("/share/:id/", r.tunnelService.GetShare)
}

func (r *Runtime) Start(parent context.Context) {
	r.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		r.cancel = cancel
		r.recoverRemainFiles(ctx)
		go r.runCleanDrive(ctx)
		go r.runTokenCheck(ctx)
	})
}

func (r *Runtime) Stop() {
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		for _, connection := range r.tunnelService.Cache.GetActiveConnections() {
			connection.Close()
		}
		if closer, ok := r.tunnelService.Cache.GuaTunnelCache.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logger.Errorf("Close Lion tunnel cache failed: %s", err)
			}
		}
	})
}

func (r *Runtime) ActiveSessionIDs() []string {
	return r.tunnelService.Cache.RangeActiveSessionIds()
}

func (r *Runtime) HandleTask(task *model.TerminalTask) (bool, error) {
	connection := r.tunnelService.Cache.GetBySessionId(task.Args)
	if connection == nil {
		return false, nil
	}
	return true, connection.HandleTask(task)
}

func (r *Runtime) healthStatus(ctx *gin.Context) {
	ctx.JSON(200, gin.H{
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(r.startedAt).Minutes(),
	})
}

func newGuaTunnelCache() tunnel.GuaTunnelCache {
	cfg := config.GetConf()
	if strings.EqualFold(cfg.ShareRoomType, config.ShareTypeRedis) {
		existingFile := func(path string) string {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
			return ""
		}
		redisCache, err := tunnel.NewGuaTunnelRedisCache(tunnel.Config{
			Addr:     net.JoinHostPort(cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DBIndex:  cfg.RedisDBIndex,

			SentinelsHost:    cfg.RedisSentinelHosts,
			SentinelPassword: cfg.RedisSentinelPassword,

			UseSSL:  cfg.RedisUseSSL,
			SSLCa:   existingFile(filepath.Join(cfg.CertsFolderPath, "redis_ca.crt")),
			SSLCert: existingFile(filepath.Join(cfg.CertsFolderPath, "redis_client.crt")),
			SSLKey:  existingFile(filepath.Join(cfg.CertsFolderPath, "redis_client.key")),
		})
		if err == nil {
			return redisCache
		}
		logger.Errorf("Initialize Lion Redis room cache failed, using local room cache: %s", err)
	}
	return tunnel.NewLocalTunnelLocalCache()
}

func newPandaClient(cfg config.Config) *panda.Client {
	return newPandaClientFactory(cfg)(cfg.PandaHost)
}

func newPandaClientFactory(cfg config.Config) func(string) *panda.Client {
	if !cfg.EnablePanda {
		return func(string) *panda.Client { return nil }
	}
	var key model.AccessKey
	if err := key.LoadFromFile(cfg.AccessKeyFilePath); err != nil {
		logger.Errorf("Create panda client failed: loading access key err %s", err)
		return func(string) *panda.Client { return nil }
	}
	return func(pandaHost string) *panda.Client {
		return panda.NewClient(pandaHost, key, cfg.IgnoreVerifyCerts)
	}
}

func (r *Runtime) runCleanDrive(ctx context.Context) {
	cfg := config.GetConf()
	if cfg.CleanDriveScheduleTime < 1 {
		logger.Info("Lion clean drive folder task disabled")
		return
	}

	ticker := time.NewTicker(time.Duration(cfg.CleanDriveScheduleTime) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			folders, err := os.ReadDir(cfg.DrivePath)
			if err != nil {
				logger.Errorf("Read Lion drive folder failed: %s", err)
				continue
			}
			activeUsers := r.tunnelService.Cache.RangeActiveUserIds()
			for _, folder := range folders {
				if _, ok := activeUsers[folder.Name()]; ok {
					continue
				}
				path := filepath.Join(cfg.DrivePath, folder.Name())
				if err = os.RemoveAll(path); err != nil {
					logger.Errorf("Remove Lion drive folder %s failed: %s", path, err)
				}
			}
		}
	}
}

func (r *Runtime) runTokenCheck(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkActiveTokens()
		}
	}
}

func (r *Runtime) checkActiveTokens() {
	connections := r.tunnelService.Cache.GetActiveConnections()
	tokens := make(map[string]model.TokenCheckStatus, len(connections))
	for _, connection := range connections {
		tokenID := connection.Sess.AuthInfo.Id
		status, ok := tokens[tokenID]
		if !ok {
			var err error
			status, err = r.jmsService.CheckTokenStatus(tokenID)
			if err != nil && status.Code == "" {
				logger.Errorf("Check Lion token status failed: %s", err)
				continue
			}
			tokens[tokenID] = status
		}
		handleTokenStatus(connection, &status)
	}
}

func handleTokenStatus(connection *tunnel.Connection, status *model.TokenCheckStatus) {
	task := model.TerminalTask{Args: status.Detail}
	if status.Code == model.CodePermOk {
		task.Name = model.TaskPermValid
	} else {
		task.Name = model.TaskPermExpired
	}
	if err := connection.HandleTask(&task); err != nil {
		logger.Errorf("Handle Lion token status task failed: %s", err)
	}
}
