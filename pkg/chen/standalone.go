package chen

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jumpserver/koko/pkg/chen/engine"
	"github.com/jumpserver/koko/pkg/chen/standalone"
)

// StandaloneOptions configures a local SQL workspace for explicitly selected databases.
type StandaloneOptions struct {
	Selectors []string
	listen    string
	backends  string
	webDir    string
	engine    engine.Options
}

func (o *StandaloneOptions) RegisterFlags(flags *flag.FlagSet) {
	flags.Func("selector", "Enable a local database by name without Core (repeatable)", func(value string) error {
		if value == "" {
			return errors.New("expected a database selector")
		}
		o.Selectors = append(o.Selectors, value)
		return nil
	})
	flags.StringVar(&o.listen, "listen", "127.0.0.1:8082", "Local SQL HTTP listen address")
	flags.StringVar(&o.backends, "backends", "standalone_backends.yml", "Private database configuration file")
	flags.StringVar(&o.webDir, "web-dir", "", "Generated Luna static directory (optional for API-only use)")
	flags.StringVar(&o.engine.StateDir, "state-dir", "data/chen-single", "Directory for local audit records and exports")
	flags.DurationVar(&o.engine.QueryTimeout, "query-timeout", 2*time.Minute, "Maximum queued and running time per execution")
	flags.DurationVar(&o.engine.TransactionIdleTimeout, "transaction-idle-timeout", 5*time.Minute, "Maximum idle time for a manual transaction")
	flags.IntVar(&o.engine.MaxSessions, "max-sessions", 16, "Maximum concurrent workspace sessions")
	flags.IntVar(&o.engine.MaxConsoles, "max-consoles", 8, "Maximum consoles per session")
	flags.IntVar(&o.engine.MaxRows, "max-rows", 10000, "Maximum retained rows per result set")
	flags.IntVar(&o.engine.MaxResultBytes, "max-result-bytes", 16<<20, "Maximum retained result bytes per session")
}

func (o StandaloneOptions) Run() error {
	if o.engine.MaxSessions <= 0 || o.engine.MaxConsoles <= 0 {
		return errors.New("max-sessions and max-consoles must be positive")
	}
	opts, err := normalizeOptions(o.engine, "")
	if err != nil {
		return err
	}
	opts.SingleMode = true
	cfg, err := standalone.Load(o.backends, o.Selectors)
	if err != nil {
		return err
	}
	provider, err := standalone.NewProvider(cfg, filepath.Join(opts.StateDir, "audit"))
	if err != nil {
		return err
	}
	app := engine.New(provider, opts)
	handler, closeWeb, err := webHandler(app.Handler(), o.listen, o.webDir)
	if err != nil {
		app.Close()
		return err
	}
	defer closeWeb()
	listener, err := net.Listen("tcp", o.listen)
	if err != nil {
		app.Close()
		return err
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second,
		IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "Koko standalone SQL workspace listening on %s\n", listener.Addr())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	var serveErr error
	select {
	case serveErr = <-done:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Closing active sessions releases SSE streams before draining the HTTP server.
	closeErr := app.CloseContext(shutdown)
	if err := server.Shutdown(shutdown); err != nil {
		closeErr = errors.Join(closeErr, err, server.Close())
	}
	return errors.Join(serveErr, closeErr)
}
