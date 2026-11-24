package chen

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/jumpserver/koko/pkg/chen/engine"
	"github.com/jumpserver/koko/pkg/chen/standalone"
)

// RunCommand handles Chen's standalone workspace and HTTP probes before Koko
// initializes a managed terminal account.
func RunCommand(args []string, version string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: koko chen {single|healthcheck|readycheck|version}")
	}
	switch args[0] {
	case "version", "--version":
		if len(args) != 1 {
			return errors.New("version does not accept arguments")
		}
		_, err := fmt.Fprintf(output, "Chen %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return err
	case "healthcheck", "readycheck":
		return probe(args[0], args[1:])
	case "single", "standalone":
		return runSingle(args[1:])
	default:
		return fmt.Errorf("unknown Chen command %q", args[0])
	}
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func probe(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	listen := flags.String("listen", envDefault("CHEN_HTTP_ADDR", net.JoinHostPort("127.0.0.1", envDefault("HTTPD_PORT", "5000"))), "HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected %s argument", command)
	}
	host, port, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	path := engine.PathPrefix + "/healthy"
	if command == "readycheck" {
		path = engine.PathPrefix + "/ready"
	}
	client := http.Client{
		Transport:     &http.Transport{Proxy: nil},
		Timeout:       3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + path)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Chen %s returned %s", command, response.Status)
	}
	return nil
}

func runSingle(args []string) error {
	flags := flag.NewFlagSet("koko chen single", flag.ContinueOnError)
	listen := flags.String("listen", envDefault("CHEN_HTTP_ADDR", "127.0.0.1:8082"), "HTTP listen address")
	stateDir := flags.String("state-dir", envDefault("CHEN_STATE_DIR", "data/chen-single"), "Private workspace and audit directory")
	origins := flags.String("allowed-origins", os.Getenv("CHEN_ALLOWED_ORIGINS"), "Additional exact trusted origins, comma separated")
	backends := flags.String("backends", "standalone_backends.yml", "Private database YAML file")
	flags.StringVar(backends, "b", *backends, "Alias for --backends")
	insecure := flags.Bool("allow-insecure-backend-file", false, "Allow group/other permissions on the database file")
	webDir := flags.String("web-dir", "", "Generated standalone Luna frontend directory")
	query := flags.Duration("query-timeout", 0, "Maximum queued plus running time per execution, 0 disables")
	idle := flags.String("transaction-idle-timeout", envDefault("CHEN_TRANSACTION_IDLE_TIMEOUT", "5m"), "Maximum idle time for a manual transaction, 0 disables")
	rows := flags.Int("max-rows", 10000, "Maximum retained rows per result set")
	bytes := flags.Int("max-result-bytes", 0, "Maximum retained row data bytes per session, 0 disables")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected standalone argument")
	}
	idleTimeout, err := time.ParseDuration(*idle)
	if err != nil {
		return err
	}
	opts, err := options(*stateDir, *origins, *query, idleTimeout, *rows, *bytes)
	if err != nil {
		return err
	}
	opts.SingleMode = true
	backendsConfig, err := standalone.Load(*backends, standalone.LoadOptions{AllowInsecurePermissions: *insecure})
	if err != nil {
		return err
	}
	provider, err := standalone.NewProvider(backendsConfig, filepath.Join(*stateDir, "audit"))
	if err != nil {
		return err
	}
	app := engine.New(provider, opts)
	handler, closeWeb, err := webHandler(app.Handler(), *listen, *webDir)
	if err != nil {
		app.Close()
		return err
	}
	defer closeWeb()
	server := http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()
	failure := make(chan error, 1)
	go func() { failure <- server.ListenAndServe() }()
	var serveErr error
	select {
	case serveErr = <-failure:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	closeErr := app.CloseContext(shutdown)
	if err := server.Shutdown(shutdown); err != nil {
		closeErr = errors.Join(closeErr, err, server.Close())
	}
	return errors.Join(serveErr, closeErr)
}
