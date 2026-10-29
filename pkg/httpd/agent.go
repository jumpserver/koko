package httpd

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/agentexec"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

func (s *Server) initializeAgent() error {
	conf := config.GetConf()
	if !conf.MCPEnabled {
		return nil
	}
	issuer, err := agentexec.NormalizeIssuer(conf.MCPAuthorizationServer)
	if err != nil {
		return err
	}
	var key model.AccessKey
	if err := key.LoadFromFile(conf.AccessKeyFilePath); err != nil {
		return err
	}
	core, err := agentexec.NewCoreClient(conf.CoreHost, key)
	if err != nil {
		return err
	}
	runtime, err := agentexec.New(core, s.apiClient, agentexec.Options{
		Directory:   filepath.Join(conf.DataFolderPath, "agent"),
		MaxSessions: conf.MCPMaxSessions, MaxRunning: conf.MCPMaxConcurrent, MaxPending: conf.MCPMaxPending,
	})
	if err != nil {
		return err
	}
	s.agentRuntime, s.agentIssuer = runtime, issuer
	s.agentRequests = make(chan struct{}, 32)
	return nil
}

func (s *Server) startAgentRefresh() {
	if s.agentRuntime == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.agentCancel, s.agentDone = cancel, make(chan struct{})
	go func() {
		defer close(s.agentDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		s.runAgentRefresh(ctx, ticker.C)
	}()
}

func (s *Server) runAgentRefresh(ctx context.Context, ticks <-chan time.Time) {
	var previousError string
	for ctx.Err() == nil {
		err := s.refreshAgentConfig(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if message := err.Error(); message != previousError {
				logger.Errorf("Update MCP configuration: %s", message)
				previousError = message
			}
		} else if previousError != "" {
			logger.Info("MCP configuration synchronization recovered")
			previousError = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (s *Server) waitAgentRefresh(ctx context.Context) {
	if s.agentDone == nil {
		return
	}
	// The SDK fetch uses its own timeout. Do not extend HTTP's shutdown deadline;
	// a late response is discarded by refreshAgentConfig after cancellation.
	select {
	case <-s.agentDone:
	case <-ctx.Done():
	}
}

func (s *Server) refreshAgentConfig(ctx context.Context) error {
	var config struct {
		PublicURL *string `json:"MCP_PUBLIC_URL"`
	}
	_, err := s.apiClient.WithResponse(&config).GetTerminalConfig()
	if err != nil {
		// SDK errors may include the component response, including storage keys.
		return errors.New("could not fetch MCP configuration from Core")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if config.PublicURL == nil {
		return errors.New("Core component configuration is missing MCP_PUBLIC_URL")
	}
	return s.applyAgentResource(*config.PublicURL)
}

func (s *Server) applyAgentResource(resource string) error {
	current := s.agentHTTP.Load()
	if resource == "" {
		s.agentHTTP.Store(nil)
		if current != nil {
			logger.Info("MCP is awaiting configuration; accepted operations and audit storage are retained")
		}
		return nil
	}
	if current != nil && current.Resource == resource {
		return nil
	}
	module, err := agentexec.NewHTTP(s.agentRuntime, resource, s.agentIssuer)
	if err != nil {
		return err
	}
	for _, path := range agentPaths(module.Path) {
		for _, route := range s.agentRoutes {
			if strings.TrimSuffix(path, "/") == strings.TrimSuffix(route.Path, "/") {
				return errors.New("MCP_PUBLIC_URL conflicts with an existing Koko route")
			}
			if index := strings.IndexAny(route.Path, ":*"); index >= 0 && strings.HasPrefix(path, route.Path[:index]) {
				return errors.New("MCP_PUBLIC_URL conflicts with an existing Koko route")
			}
		}
	}
	s.agentHTTP.Store(module)
	logger.Info("MCP configuration applied from Core")
	return nil
}

func agentPaths(base string) [5]string {
	return [5]string{base, base + "/files/upload", base + "/files/download",
		base + "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource" + base}
}

func (s *Server) registerAgentRoutes(router *gin.Engine) {
	if s.agentRuntime == nil {
		return
	}
	// Existing routes retain priority. The live Gin routing tree is never changed.
	s.agentRoutes = router.Routes()
	router.NoRoute(func(c *gin.Context) {
		module := s.agentHTTP.Load()
		base := "/koko/mcp"
		if module != nil {
			base = module.Path
		}
		for index, path := range agentPaths(base) {
			if c.Request.URL.Path != path {
				continue
			}
			c.Header("Cache-Control", "no-store")
			if module == nil {
				c.String(http.StatusServiceUnavailable, "MCP is awaiting configuration")
				return
			}
			// Keep one limit across configuration generations and in-flight requests.
			select {
			case s.agentRequests <- struct{}{}:
				defer func() { <-s.agentRequests }()
			default:
				c.Header("Retry-After", "2")
				c.String(http.StatusTooManyRequests, "MCP request capacity reached")
				return
			}
			c.Status(http.StatusOK)
			if index >= 3 {
				module.Metadata.ServeHTTP(c.Writer, c.Request)
			} else {
				request := c.Request.Clone(c.Request.Context())
				request.RemoteAddr = c.ClientIP()
				module.Handler.ServeHTTP(c.Writer, request)
			}
			return
		}
		http.NotFound(c.Writer, c.Request)
	})
}
