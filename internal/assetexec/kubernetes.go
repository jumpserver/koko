package assetexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Bound local kubectl resource use independently of the other asset adapters.
var kubernetesProcesses = make(chan struct{}, 1)

type kubernetesConnection struct {
	binary, limiter, directory, namespace string
	ctx                                   context.Context
	cancel                                context.CancelFunc
}

func (c *Connection) openKubernetes(ctx context.Context, token *model.ConnectToken) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.ConnectTimeout)
	defer cancel()
	address, err := url.Parse(token.Asset.Address)
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Hostname() == "" || address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" {
		return errors.New("Kubernetes requires an HTTP(S) API server address without credentials, query or fragment")
	}
	if len(token.Account.Secret) > 64<<10 || strings.IndexFunc(token.Account.Secret, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("invalid Kubernetes account token")
	}
	namespace := "default"
	if settings, ok := token.Platform.GetProtocolSetting(srvconn.ProtocolK8s); ok {
		if value, found := settings.Setting["namespace"]; found && value != "" {
			var valid bool
			namespace, valid = value.(string)
			if !valid {
				return errors.New("invalid Kubernetes default namespace")
			}
		}
	}
	if len(validation.IsDNS1123Label(namespace)) != 0 {
		return errors.New("invalid Kubernetes default namespace")
	}
	binary, err := exec.LookPath("kubectl")
	if err != nil {
		return fmt.Errorf("%w: kubectl is unavailable", ErrUnsupported)
	}
	limiter, err := exec.LookPath("prlimit")
	if err != nil {
		return fmt.Errorf("%w: prlimit is required for bounded Kubernetes execution", ErrUnsupported)
	}
	serverName := address.Hostname()
	if token.Gateway != nil {
		port := address.Port()
		if port == "" {
			port = "443"
			if address.Scheme == "http" {
				port = "80"
			}
		}
		tunnel, err := openGatewayTunnel(ctx, token.Gateway, net.JoinHostPort(serverName, port), c.opts.ConnectTimeout)
		if err != nil {
			return err
		}
		address.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(tunnel.listener.Addr().(*net.TCPAddr).Port))
		c.cleanup = func() error { tunnel.Close(); return nil }
	}
	directory, err := os.MkdirTemp("", "koko-agent-k8s-")
	if err != nil {
		return err
	}
	lifetime, closeConnection := context.WithCancel(context.Background())
	kube := &kubernetesConnection{binary: binary, limiter: limiter, directory: directory, namespace: namespace, ctx: lifetime, cancel: closeConnection}
	c.kubernetes = kube
	// Keep the existing K8s self-signed certificate behavior when no CA is
	// supplied. A supplied CA enables verification with the original TLS name,
	// including when the network connection traverses a gateway.
	ca := []byte(token.Asset.SecretInfo.CaCert)
	config := clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"asset": {Server: address.String(), TLSServerName: serverName, CertificateAuthorityData: ca, InsecureSkipTLSVerify: len(ca) == 0}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"account": {Token: token.Account.Secret}},
		Contexts:       map[string]*clientcmdapi.Context{"session": {Cluster: "asset", AuthInfo: "account", Namespace: namespace}},
		CurrentContext: "session",
	}
	data, err := clientcmd.Write(config)
	if err != nil {
		return errors.New("could not prepare Kubernetes connection credentials")
	}
	if err = os.WriteFile(filepath.Join(directory, "config"), data, 0600); err != nil {
		return err
	}
	probeCtx, stopProbe := context.WithCancel(ctx)
	defer stopProbe()
	probe := &outputSink{remaining: 64 << 10, emit: func(string, []byte) error { return nil }, cancel: stopProbe}
	_, err = kube.run(probeCtx, []string{"version", "--output=json"}, outputWriter{probe, "stdout"}, outputWriter{probe, "stderr"})
	if err != nil {
		return errors.New("Kubernetes connection check failed; verify the API address, account token and connectivity")
	}
	return nil
}

func (k *kubernetesConnection) run(ctx context.Context, args []string, stdout, stderr io.Writer) (Result, error) {
	duration := 120 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		duration = min(time.Until(deadline), 10*time.Minute)
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	stop := context.AfterFunc(k.ctx, cancel)
	defer stop()
	select {
	case kubernetesProcesses <- struct{}{}:
		defer func() { <-kubernetesProcesses }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline)
	if remaining < time.Millisecond {
		return Result{}, context.DeadlineExceeded
	}
	args = append([]string{k.binary, "--kubeconfig=" + filepath.Join(k.directory, "config"),
		"--cache-dir=" + filepath.Join(k.directory, "cache"), "--request-timeout=" + remaining.Truncate(time.Millisecond).String()}, args...)
	args = append([]string{"--as=2147483648", "--data=134217728", "--cpu=15", "--core=0", "--nofile=128", "--fsize=1048576", "--"}, args...)
	command := exec.CommandContext(ctx, k.limiter, args...)
	command.Dir = k.directory
	command.Env = []string{"PATH=" + filepath.Dir(k.binary), "LANG=C.UTF-8", "KUBERC=off", "KUBECTL_KUBERC=false", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = 3 * time.Second
	if err := command.Start(); err != nil {
		return Result{}, err
	}
	result := Result{Dispatched: true}
	err := command.Wait()
	if command.ProcessState != nil && command.ProcessState.Exited() && ctx.Err() == nil {
		code := command.ProcessState.ExitCode()
		result.Completed, result.ExitCode = true, &code
	}
	return result, errors.Join(err, ctx.Err())
}

func (k *kubernetesConnection) close() {
	k.cancel()
	_ = os.RemoveAll(k.directory)
}

func (c *Connection) executeKubernetes(ctx context.Context, request Request, emit Emit) (Result, error) {
	if request.Cwd != "" || len(request.Env) != 0 {
		return Result{}, errors.New("cwd and env apply only to SSH")
	}
	args, err := kubernetesArguments(request.Command)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sink := &outputSink{emit: emit, remaining: c.opts.MaxOutputBytes, cancel: cancel}
	result, err := c.kubernetes.run(ctx, args, outputWriter{sink, "stdout"}, outputWriter{sink, "stderr"})
	result.Truncated, result.ContextLost = sink.truncated, c.closed.Load()
	return result, errors.Join(err, sink.err)
}
