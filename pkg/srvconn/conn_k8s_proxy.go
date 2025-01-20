package srvconn

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

var ErrNoAvailablePort = errors.New("no available port")

const (
	MinPort = 6000
	MaxPort = 65535
)

func NewKubectlProxyConn(opt *k8sOptions) *KubectlProxyConn {
	return &KubectlProxyConn{opts: opt, Id: common.UUID()}
}

func NewPortAllocator(minPort, maxPort int) *PortAllocator {
	if minPort < MinPort {
		minPort = MinPort
	}
	if maxPort > MaxPort {
		maxPort = MaxPort
	}
	ports := make([]int, maxPort-minPort+1)
	for i := range ports {
		ports[i] = minPort + i
	}
	return &PortAllocator{
		ports:     ports,
		usedPorts: make(map[int]bool),
	}
}

type PortAllocator struct {
	ports     []int
	current   int
	usedPorts map[int]bool
	mu        sync.Mutex
}

func (p *PortAllocator) Allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < len(p.ports); i++ {
		port := p.ports[p.current]
		if _, used := p.usedPorts[port]; !used {
			p.usedPorts[port] = true
			p.current = (p.current + 1) % len(p.ports)
			return port, nil
		}
		p.current = (p.current + 1) % len(p.ports)
	}

	return 0, ErrNoAvailablePort
}

func (p *PortAllocator) Release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.usedPorts, port)
}

var gloablPortAllocator = NewPortAllocator(6000, 7000)

type KubectlProxyConn struct {
	Id   string
	opts *k8sOptions

	Port       int
	proxyCmd   *exec.Cmd
	configPath string
	once       sync.Once
}

func (k *KubectlProxyConn) Close() error {
	var err error
	k.once.Do(func() {
		if k.proxyCmd != nil {
			err = k.proxyCmd.Process.Kill()
		}
		if k.configPath != "" {
			_ = os.Remove(k.configPath)
		}
		gloablPortAllocator.Release(k.Port)
		gloablTokenMaps.Delete(k.Id)
	})
	return err
}

func (k *KubectlProxyConn) Start() error {
	port, err := gloablPortAllocator.Allocate()
	if err != nil {
		return err
	}
	k.Port = port
	k.configPath, err = k.CreateKubeConfig(k.opts.ClusterServer, k.opts.Token)
	if err != nil {
		return err
	}

	/*
		kubetcl proxy --kubeconfig=path --port=port --api-prefix=/
	*/
	// if !k.opts.DEBUG {
	// 	defer func() {
	// 		_ = os.Remove(k.configPath)
	// 	}()
	// }
	logger.Infof("kubeconfig: %s", k.configPath)
	k.proxyCmd = exec.Command("kubectl", "proxy",
		fmt.Sprintf("--kubeconfig=%s", k.configPath),
		fmt.Sprintf("--port=%d", port),
		"--api-prefix=/")

	err = k.proxyCmd.Start()
	go func() {
		_ = k.proxyCmd.Wait()
		logger.Infof("kubectl proxy %s exit", k.opts.ClusterServer)
	}()
	return err
}

func (k *KubectlProxyConn) ProxyAddr() string {
	return fmt.Sprintf("http://127.0.0.1:%d", k.Port)
}

func (k *KubectlProxyConn) CreateKubeConfig(server, token string) (string, error) {
	k8sDir, err := os.MkdirTemp("", "k8s*")
	if err != nil {
		return "", err
	}
	configPath := filepath.Join(k8sDir, "config")
	configContent := fmt.Sprintf(proxyconfigTmpl, server, token)
	err = os.WriteFile(configPath, []byte(configContent), 0600)
	return configPath, err
}

func (k *KubectlProxyConn) Env() []string {
	o := k.opts
	skipTls := "true"
	if !o.IsSkipTls {
		skipTls = "false"
	}
	clusterServer := k.ProxyAddr()
	gloablTokenMaps.Store(k.Id, clusterServer)
	k8sName := strings.Trim(strconv.Quote(o.ExtraEnv["K8sName"]), "\"")
	k8sName = strings.ReplaceAll(k8sName, "`", "\\`")
	return []string{
		fmt.Sprintf("KUBECTL_USER=%s", o.Username),
		fmt.Sprintf("KUBECTL_CLUSTER=%s", "http://localhost:6000"),
		fmt.Sprintf("KUBECTL_INSECURE_SKIP_TLS_VERIFY=%s", skipTls),
		fmt.Sprintf("KUBECTL_TOKEN=%s", k.Id),
		fmt.Sprintf("WELCOME_BANNER=%s", config.KubectlBanner),
		fmt.Sprintf("K8S_NAME=%s", k8sName),
	}
}

var proxyconfigTmpl = `apiVersion: v1
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: %s
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: JumpServer-user
  name: kubernetes
current-context: kubernetes
kind: Config
preferences: {}
users:
- name: JumpServer-user
  user:
    token: %s
`
