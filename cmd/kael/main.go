package main

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

	"github.com/jumpserver/koko/internal/kael/api"
	"github.com/jumpserver/koko/internal/kael/component"
	"github.com/jumpserver/koko/internal/kael/config"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/identity"
	kaellogger "github.com/jumpserver/koko/internal/kael/logger"
	"github.com/jumpserver/koko/internal/kael/platformgateway"
	"github.com/jumpserver/koko/internal/kael/ports"
	agentruntime "github.com/jumpserver/koko/internal/kael/runtime"
	"github.com/jumpserver/koko/internal/kael/service"
	"github.com/jumpserver/koko/internal/kael/store"
	"go.uber.org/zap"
)

var (
	Version    = "dev"
	Buildstamp = "unknown"
	Githash    = "unknown"
	Goversion  = "unknown"
)

func main() {
	configPath := flag.String("f", "", "path to the Kael YAML configuration")
	showVersion := flag.Bool("version", false, "print build information")
	flag.Parse()
	if *showVersion {
		fmt.Printf("kael %s (%s, %s, %s)\n", Version, Githash, Buildstamp, Goversion)
		return
	}
	settings, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}
	logger, err := kaellogger.New(settings.LogDirPath, settings.LogLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create logger:", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()
	if err = run(settings, logger); err != nil {
		logger.Fatal("kael stopped", zap.Error(err))
	}
}

func run(settings config.Config, logger *zap.Logger) error {
	tlsVerify := !settings.IgnoreVerifyCerts
	componentClient, err := component.Connect(component.Options{CoreURL: settings.CoreHost, TLSVerify: tlsVerify, Timeout: settings.HTTPRequestTimeout, Name: settings.Name, BootstrapToken: settings.BootstrapToken, AccessKeyFile: settings.AccessKeyFilePath, Logger: logger})
	if err != nil {
		return err
	}
	engine, err := agentruntime.NewHarness(context.Background(), settings.CodexBinary, filepath.Join(settings.RuntimeDataFolderPath, "harness"), componentClient.ModelConfig)
	if err != nil {
		return err
	}
	defer engine.Close()
	retention := store.RetentionOptions{KeepDays: settings.TerminalAIKeepDays, MaxBytes: settings.TerminalAIMaxBytes, MinFreeBytes: settings.TerminalAIMinFreeBytes}
	runtimeStore, err := store.NewCore(componentClient, settings.RuntimeDataFolderPath, retention)
	if err != nil {
		return err
	}
	defer runtimeStore.Close()
	authenticator := identity.NewCoreAuthenticator(settings.CoreHost, tlsVerify, 15*time.Second)
	bus := event.NewBus()
	var capability ports.CapabilityProvider
	if settings.PlatformGatewayEnabled {
		capability, err = platformgateway.New(platformgateway.Config{CoreURL: settings.CoreHost, CoreTLSVerify: tlsVerify, CACert: settings.PlatformCACert, ClientCert: settings.PlatformClientCert, ClientKey: settings.PlatformClientKey, AllowedMethods: settings.PlatformAllowedMethods, Timeout: settings.PlatformTimeout, MaxResponse: settings.PlatformMaxResponse, OpenAPILoader: componentClient.OpenAPISchema})
		if err != nil {
			return err
		}
	}
	runtimeService, err := service.New(service.Options{Store: runtimeStore, Engine: engine, Bus: bus, Logger: logger, InstanceID: settings.Name, Workers: 4, ToolResultTimeout: settings.ToolResultTimeout, ArtifactDir: settings.ArtifactFolderPath, Capability: capability, StorageKind: "core", StorageDurable: true})
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(context.Background(), 2*time.Minute)
	err = runtimeService.Start(startup)
	cancelStartup()
	if err != nil {
		return err
	}
	defer runtimeService.Close()
	apiServer, err := api.New(api.Options{Service: runtimeService, Authenticator: authenticator, Origin: identity.NewOriginVerifier(settings.AllowedOrigins, settings.TrustForwardedHeaders), Logger: logger})
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: fmt.Sprintf("%s:%d", settings.BindHost, settings.HTTPPort), Handler: apiServer.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1024 * 1024}
	cancelRequests := bindServerContext(httpServer)
	defer cancelRequests()
	serverError := make(chan error, 1)
	go func() { serverError <- httpServer.ListenAndServe() }()
	heartbeatContext, cancelHeartbeat := context.WithCancel(context.Background())
	defer cancelHeartbeat()
	go componentClient.RunHeartbeat(heartbeatContext, logger)
	logger.Info("kael started", zap.String("address", httpServer.Addr), zap.String("version", Version), zap.String("component", settings.Name), zap.String("model", engine.Info().Model), zap.String("storage", "core"), zap.String("terminal_storage", "jsonl"))
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var serveErr error
	select {
	case <-signals.Done():
		stop()
		logger.Info("kael stopping")
	case serveErr = <-serverError:
	}
	cancelRequests()
	cancelHeartbeat()
	shutdownErr := shutdownHTTPServer(httpServer)
	runtimeService.Close()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return shutdownErr
}

func bindServerContext(server *http.Server) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	return cancel
}

func shutdownHTTPServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		_ = server.Close()
	}
	return err
}
