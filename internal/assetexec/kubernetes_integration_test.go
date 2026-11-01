package assetexec

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestKubernetesNamespaceRBACAndOutputLimit(t *testing.T) {
	fixture := os.Getenv("KOKO_AGENT_KUBERNETES_TEST_CONFIG")
	if fixture == "" {
		t.Skip("isolated Kubernetes fixture not configured")
	}
	var config struct {
		Server, Token, CA, Namespace string
		ForbiddenNamespace           string `json:"forbidden_namespace"`
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	token := &model.ConnectToken{Id: "isolated-kubernetes", User: model.User{ID: "test-user"}, Protocol: "k8s",
		Actions: model.Actions{{Value: model.ActionConnect}},
		Account: model.Account{BaseAccount: model.BaseAccount{Secret: config.Token, SecretType: model.LabelValue{Value: "token"}}},
		Asset: model.Asset{ID: "test-cluster", Address: config.Server, Protocols: []model.Protocol{{Name: "k8s", Port: 443}},
			SecretInfo: model.SecretInfo{CaCert: config.CA}}}
	token.Platform.Protocols = model.PlatformProtocols{{Protocol: model.Protocol{Name: "k8s"}, Setting: map[string]any{"namespace": config.Namespace}}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := Open(ctx, token, Options{ConnectTimeout: 5 * time.Second, MaxOutputBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	directory := conn.kubernetes.directory
	if info, err := os.Stat(filepath.Join(directory, "config")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("Kubernetes credentials must be private")
	}
	if conn.ExecutionHints()["default_namespace"] != config.Namespace {
		t.Fatal("connected namespace is missing from execution hints")
	}
	for _, check := range []struct {
		command string
		success bool
	}{
		{"kubectl get configmap/mcp-execution -o json", true},
		{"kubectl describe configmap/mcp-execution --show-events=false", true},
		{"kubectl get configmaps -n " + config.ForbiddenNamespace, false},
		{"kubectl logs pod/missing --tail=20", false},
	} {
		var stdout, stderr strings.Builder
		result, err := conn.Execute(ctx, Request{Command: check.command}, func(stream string, data []byte) error {
			if stream == "stderr" {
				_, _ = stderr.Write(data)
			} else {
				_, _ = stdout.Write(data)
			}
			return nil
		})
		if !result.Dispatched || !result.Completed || result.ExitCode == nil || result.StopConfirmed ||
			check.success != (err == nil && *result.ExitCode == 0) {
			t.Fatalf("incorrect process evidence for %s: %+v %v", check.command, result, err)
		}
		if check.success && !strings.Contains(stdout.String(), "bounded-kubernetes-output") || !check.success && stderr.Len() == 0 {
			t.Fatalf("missing expected stdout/stderr for %s", check.command)
		}
		if strings.Contains(stdout.String()+stderr.String(), config.Token) {
			t.Fatal("account credential leaked into output")
		}
	}
	conn.opts.MaxOutputBytes = 16
	var received int
	result, err := conn.Execute(ctx, Request{Command: "kubectl get configmap/mcp-execution -o json"}, func(_ string, data []byte) error {
		received += len(data)
		return nil
	})
	if !errors.Is(err, ErrOutputLimit) || received != 16 || !result.Truncated || result.Completed || result.StopConfirmed {
		t.Fatalf("output limit lost execution uncertainty: %+v %v (%d bytes)", result, err, received)
	}
	_ = conn.Close()
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("connection credentials survived closure")
	}
}

func TestKubernetesCancellationDoesNotConfirmRemoteStop(t *testing.T) {
	binary, err := exec.LookPath("kubectl")
	if err != nil {
		t.Skip("kubectl not installed")
	}
	limiter, err := exec.LookPath("prlimit")
	if err != nil {
		t.Skip("prlimit not installed")
	}
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	lifetime, closeConnection := context.WithCancel(context.Background())
	kube := &kubernetesConnection{binary: binary, limiter: limiter, directory: t.TempDir(), ctx: lifetime, cancel: closeConnection}
	defer kube.close()
	config := clientcmdapi.Config{Clusters: map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
		Contexts: map[string]*clientcmdapi.Context{"test": {Cluster: "test"}}, CurrentContext: "test"}
	data, err := clientcmd.Write(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(kube.directory, "config"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		result, _ := kube.run(ctx, []string{"version", "--output=json"}, io.Discard, io.Discard)
		done <- result
	}()
	select {
	case <-started:
		cancel()
	case <-ctx.Done():
		t.Fatal("kubectl did not contact the isolated API")
	}
	select {
	case result := <-done:
		if !result.Dispatched || result.Completed || result.ExitCode != nil || result.StopConfirmed {
			t.Fatalf("local process cancellation confirmed remote execution: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kubectl cancellation exceeded its budget")
	}
}
