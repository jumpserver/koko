package chen

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/engine"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

type Runtime struct {
	app      *engine.Server
	provider *core.CoreProvider
	cancel   context.CancelFunc
	done     chan struct{}
}

func options(stateDir, origins string, query, idle time.Duration, rows, bytes int) (engine.Options, error) {
	if stateDir == "" || rows <= 0 || bytes < 0 || query < 0 || idle < 0 {
		return engine.Options{}, errors.New("invalid Chen state directory or resource limits")
	}
	if idle == 0 {
		idle = -1 // The engine uses a negative value to disable idle rollback.
	}
	opts := engine.Options{StateDir: stateDir, QueryTimeout: query, TransactionIdleTimeout: idle, MaxRows: rows, MaxResultBytes: bytes}
	for origin := range strings.SplitSeq(origins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			opts.AllowedOrigins = append(opts.AllowedOrigins, origin)
		}
	}
	return opts, nil
}

func NewRuntime(client *service.JMService, conf config.Config) (*Runtime, error) {
	opts, err := options(conf.ChenStateDir, conf.ChenAllowedOrigins, conf.ChenQueryTimeout, conf.ChenTransactionIdleTimeout, conf.ChenMaxRows, conf.ChenMaxResultBytes)
	if err != nil {
		return nil, err
	}
	provider, err := core.NewCoreProvider(client, core.Options{
		SpoolDir: filepath.Join(opts.StateDir, "outbox"),
		OnError:  func(err error) { logger.Errorf("Chen: %v", err) },
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{app: engine.New(provider, opts), provider: provider, done: make(chan struct{})}, nil
}

func (r *Runtime) RegisterRoutes(router *gin.Engine) {
	router.Any(engine.PathPrefix+"/*path", gin.WrapH(r.app.Handler()))
}

func (r *Runtime) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		r.provider.Run(ctx)
	}()
}

func (r *Runtime) SetControlAvailable(available bool) {
	r.provider.SetControlAvailable(available)
}

func (r *Runtime) Stop() {
	// Keep Core delivery and the state-directory lock until all sessions have
	// stopped their database workers and persisted their final audit records.
	go func() {
		if err := r.app.CloseContext(context.Background()); err != nil {
			logger.Errorf("Close Chen sessions: %v", err)
		}
		r.cancel()
	}()
	select {
	case <-r.done:
	case <-time.After(27 * time.Second):
		logger.Warn("Chen shutdown timed out; pending audit records will retry on startup")
	}
}
