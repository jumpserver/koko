package srvconn

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/utils"
)

var (
	InValidToken = errors.New("invalid token")

	_ ServerConnection = (*K8sCon)(nil)
)

const (
	k8sInitFilename = "init-kubectl.sh"

	checkTokenCommand = `kubectl --insecure-skip-tls-verify=%s --token=%s --server=%s auth can-i get pods`
)

func isValidK8sUserToken(o *k8sOptions) bool {
	skipVerifyTls := "true"
	token := o.Token
	server := o.ClusterServer
	if !o.IsSkipTls {
		skipVerifyTls = "false"
	}
	c := exec.Command("bash", "-c",
		fmt.Sprintf(checkTokenCommand, skipVerifyTls, token, server))
	out, err := c.CombinedOutput()
	if err != nil {
		logger.Info(err)
	}
	result := strings.TrimSpace(string(out))
	switch strings.ToLower(result) {
	case "yes", "no":
		logger.Info("K8sCon check token success")
		return true
	}
	logger.Errorf("K8sCon check token err: %s", result)
	return false
}

func NewK8sCon(ops ...K8sOption) *K8sCon {
	args := &k8sOptions{
		Username:      os.Getenv("USER"),
		ClusterServer: "https://127.0.0.1:8443",
		Token:         "",
		IsSkipTls:     true,
		ExtraEnv:      map[string]string{},
	}
	for _, setter := range ops {
		setter(args)
	}
	return &K8sCon{options: args}
}

type K8sCon struct {
	options   *k8sOptions
	ptyFD     *os.File
	onceClose sync.Once
	cmd       *exec.Cmd
}

func (c *K8sCon) Connect() (err error) {
	if !isValidK8sUserToken(c.options) {
		return InValidToken
	}
	cmd, ptyFD, err := connectK8s(c)
	go func() {
		err = cmd.Wait()
		if err != nil {
			logger.Errorf("K8sCon command exit err: %s", err)
		}
		if ptyFD != nil {
			_ = ptyFD.Close()
		}
		logger.Info("K8sCon connect closed.")
		var wstatus syscall.WaitStatus
		_, err = syscall.Wait4(-1, &wstatus, 0, nil)
	}()
	if err != nil {
		logger.Errorf("K8sCon pty start err: %s", err)
		return fmt.Errorf("K8sCon start local pty err: %s", err)
	}

	logger.Infof("Connect K8s cluster server %s success ", c.options.ClusterServer)
	c.cmd = cmd
	c.ptyFD = ptyFD
	return
}

func (c *K8sCon) Read(p []byte) (int, error) {
	return c.ptyFD.Read(p)
}

func (c *K8sCon) Write(p []byte) (int, error) {
	return c.ptyFD.Write(p)
}

func (c *K8sCon) SetWinSize(w, h int) error {
	win := pty.Winsize{
		Rows: uint16(h),
		Cols: uint16(w),
	}
	logger.Infof("K8sCon conn windows size change %d*%d", h, w)
	return pty.Setsize(c.ptyFD, &win)
}

func (c *K8sCon) KeepAlive() error {
	return nil
}

func (c *K8sCon) Close() (err error) {
	c.onceClose.Do(func() {
		if c.ptyFD == nil {
			return
		}
		_ = c.ptyFD.Close()
		err = c.cmd.Process.Signal(os.Kill)
	})
	return
}

type k8sOptions struct {
	ClusterServer string // https://172.16.10.51:8443
	Username      string // user 系统用户名
	Token         string // 授权token
	IsSkipTls     bool
	ExtraEnv      map[string]string
}

func (o *k8sOptions) Env() []string {
	token, err := utils.Encrypt(o.Token, config.CipherKey)
	if err != nil {
		logger.Errorf("Encrypt k8s token err: %s", err)
		token = o.Token
	}
	skipTls := "true"
	if !o.IsSkipTls {
		skipTls = "false"
	}
	return []string{
		fmt.Sprintf("KUBECTL_USER=%s", o.Username),
		fmt.Sprintf("KUBECTL_CLUSTER=%s", o.ClusterServer),
		fmt.Sprintf("KUBECTL_INSECURE_SKIP_TLS_VERIFY=%s", skipTls),
		fmt.Sprintf("K8S_ENCRYPTED_TOKEN=%s", token),
		fmt.Sprintf("WELCOME_BANNER=%s", config.KubectlBanner),
	}
}
func connectK8s(con *K8sCon) (cmd *exec.Cmd, ptyFD *os.File, err error) {
	return connectK8sWithNamespace(con.options.Env())
}

func connectK8sWithNamespace(envs []string) (cmd *exec.Cmd, ptyFD *os.File, err error) {
	pwd, _ := os.Getwd()
	shPath := filepath.Join(pwd, k8sInitFilename)
	args := []string{
		"--fork",
		"--pid",
		"--mount-proc",
		shPath,
	}
	cmd = exec.Command("unshare", args...)
	cmd.Env = envs
	ptyFD, err = pty.Start(cmd)
	return
}

type K8sOption func(*k8sOptions)

func K8sUsername(username string) K8sOption {
	return func(args *k8sOptions) {
		args.Username = username
	}
}

func K8sToken(token string) K8sOption {
	return func(args *k8sOptions) {
		args.Token = token
	}
}

func K8sClusterServer(clusterServer string) K8sOption {
	return func(args *k8sOptions) {
		args.ClusterServer = clusterServer
	}
}

func K8sExtraEnvs(envs map[string]string) K8sOption {
	return func(args *k8sOptions) {
		args.ExtraEnv = envs
	}
}

func K8sSkipTls(isSkipTls bool) K8sOption {
	return func(args *k8sOptions) {
		args.IsSkipTls = isSkipTls
	}
}
