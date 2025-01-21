package srvconn

import (
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

var k8sProxyDirname = "k8s_proxy"

func GetK8sProxyDir() string {
	pwd, _ := os.Getwd()
	dirPath := filepath.Join(pwd, k8sProxyDirname)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		_ = os.Mkdir(dirPath, 0700)
	}
	return dirPath
}

func NewKubectlProxyConn(opt *k8sOptions) *KubectlProxyConn {
	return &KubectlProxyConn{opts: opt, Id: common.UUID()}
}

type KubectlProxyConn struct {
	Id   string
	opts *k8sOptions

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
		gloablTokenMaps.Delete(k.Id)
		_ = os.Remove(k.UnixPath())
	})
	return err
}

func (k *KubectlProxyConn) Start() error {
	var err error
	k.configPath, err = k.CreateKubeConfig(k.opts.ClusterServer, k.opts.Token)
	if err != nil {
		return err
	}

	/*
		kubetcl proxy --kubeconfig=path --unix-socket=port --api-prefix=/
	*/
	logger.Infof("kubeconfig: %s", k.configPath)
	k.proxyCmd = exec.Command("kubectl", "proxy",
		"--disable-filter=true",
		fmt.Sprintf("--kubeconfig=%s", k.configPath),
		fmt.Sprintf("--unix-socket=%s", k.UnixPath()),
		"--api-prefix=/")

	err = k.proxyCmd.Start()
	go func() {
		_ = k.proxyCmd.Wait()
		logger.Infof("kubectl proxy id %s %s exit", k.Id, k.opts.ClusterServer)
	}()
	return err
}

func (k *KubectlProxyConn) UnixPath() string {
	k8sDir := GetK8sProxyDir()
	return filepath.Join(k8sDir, fmt.Sprintf("proxy-%s.sock", k.Id))
}

func (k *KubectlProxyConn) CreateKubeConfig(server, token string) (string, error) {
	k8sDir := GetK8sProxyDir()
	configPath := filepath.Join(k8sDir, fmt.Sprintf("config-%s", k.Id))
	configContent := fmt.Sprintf(proxyconfigTmpl, server, token)
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	return configPath, err
}

func (k *KubectlProxyConn) Env() []string {
	o := k.opts
	skipTls := "true"
	if !o.IsSkipTls {
		skipTls = "false"
	}
	clusterServer := k.UnixPath()
	gloablTokenMaps.Store(k.Id, clusterServer)
	k8sName := strings.Trim(strconv.Quote(o.ExtraEnv["K8sName"]), "\"")
	k8sName = strings.ReplaceAll(k8sName, "`", "\\`")
	return []string{
		fmt.Sprintf("KUBECTL_USER=%s", o.Username),
		fmt.Sprintf("KUBECTL_CLUSTER=%s", k8sReverseProxyURL),
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
