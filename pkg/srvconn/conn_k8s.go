package srvconn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/localcommand"
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

func IsValidK8sUserToken(o *k8sOptions) bool {
	k8sCfg := o.K8sCfg()
	client, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		logger.Errorf("K8sCon check token err: %s", err)
		return false
	}
	spec := v1.TokenReviewSpec{Token: o.Token}
	resp, err := client.AuthenticationV1().TokenReviews().Create(context.TODO(),
		&v1.TokenReview{Spec: spec}, metav1.CreateOptions{})
	if err != nil {
		logger.Errorf("K8sCon check token pods err: %s", err)
		return false
	}
	logger.Debugf("K8sCon check token resp: %+v", resp)
	return resp.Status.Authenticated
}

func NewK8sConnection(ops ...K8sOption) (*K8sCon, error) {
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
	if !IsValidK8sUserToken(args) {
		return nil, InValidToken
	}
	_, err := utils.Encrypt(args.Token, config.CipherKey)
	if err != nil {
		return nil, fmt.Errorf("%w: encrypt k8s token failed %s", InValidToken, err)
	}

	lcmd, err := startK8SLocalCommand(args)
	if err != nil {
		logger.Errorf("K8sCon start local pty err: %s", err)
		return nil, fmt.Errorf("K8sCon start local pty err: %w", err)
	}
	err = lcmd.SetWinSize(args.win.Width, args.win.Height)
	if err != nil {
		_ = lcmd.Close()
		return nil, err
	}
	return &K8sCon{options: args, LocalCommand: lcmd}, nil
}

type K8sCon struct {
	options *k8sOptions
	*localcommand.LocalCommand
}

func (k *K8sCon) KeepAlive() error {
	return nil
}

type k8sOptions struct {
	ClusterServer string // https://172.16.10.51:8443
	Username      string // user 系统用户名
	Token         string // 授权token
	IsSkipTls     bool
	ExtraEnv      map[string]string

	win Windows
}

func (o *k8sOptions) K8sCfg() *rest.Config {
	kubeConf := &rest.Config{
		Host:        o.ClusterServer,
		BearerToken: o.Token,
	}
	if o.IsSkipTls {
		kubeConf.Insecure = true
	}
	return kubeConf
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

func startK8SLocalCommand(args *k8sOptions) (*localcommand.LocalCommand, error) {
	pwd, _ := os.Getwd()
	shPath := filepath.Join(pwd, k8sInitFilename)
	argv := []string{
		"--fork",
		"--pid",
		"--mount-proc",
		shPath,
	}
	return localcommand.New("unshare", argv, localcommand.WithEnv(args.Env()))
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

func K8sPtyWin(win Windows) K8sOption {
	return func(args *k8sOptions) {
		args.win = win
	}
}
