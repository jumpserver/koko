package srvconn

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/jumpserver/koko/pkg/logger"
)

var (
	_ ServerConnection = (*ContainerConnection)(nil)

	ErrNotFoundCommand = errors.New("not found command")

	ErrNotFoundShell = errors.New("not found any shell")
)

func NewContainerConnection(opts ...ContainerOption) (*ContainerConnection, error) {
	var opt ContainerOptions
	for _, setter := range opts {
		setter(&opt)
	}
	if opt.win == nil {
		opt.win = &remotecommand.TerminalSize{
			Width:  80,
			Height: 40,
		}
	}
	k8sCfg := opt.K8sCfg()
	client, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return nil, err
	}
	shell, err := FindAvailableShell(&opt)
	if err != nil {
		return nil, err
	}
	winSizeChan := make(chan *remotecommand.TerminalSize, 10)
	done := make(chan struct{})
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	slaver := slaveStream{
		r:           stdinReader,
		w:           stdoutWriter,
		winSizeChan: winSizeChan,
		done:        done,
	}
	con := ContainerConnection{
		opt:          opt,
		shell:        shell,
		slaver:       &slaver,
		winSizeChan:  winSizeChan,
		done:         done,
		stdoutReader: stdoutReader,
		stdinWriter:  stdinWriter,
	}
	con.winSizeChan <- opt.win
	go func() {
		if err2 := execContainerShell(client, k8sCfg, &con); err2 != nil {
			logger.Error(err2)
		}
		_ = con.Close()
		logger.Infof("K8s %s exec shell exit", con.opt.String())
	}()
	return &con, nil
}

type ContainerConnection struct {
	opt   ContainerOptions
	shell string

	slaver      *slaveStream
	winSizeChan chan *remotecommand.TerminalSize

	stdinWriter  io.WriteCloser
	stdoutReader io.ReadCloser

	done chan struct{}

	once sync.Once
}

func (c *ContainerConnection) Read(p []byte) (int, error) {
	return c.stdoutReader.Read(p)
}

func (c *ContainerConnection) Write(p []byte) (int, error) {
	return c.stdinWriter.Write(p)
}

func (c *ContainerConnection) Close() error {
	c.once.Do(func() {
		_, _ = c.stdinWriter.Write([]byte("\r\nexit\r\n"))
		_ = c.stdinWriter.Close()
		_ = c.stdoutReader.Close()
		close(c.done)
		logger.Infof("K8s %s connection close", c.opt.String())
	})
	return nil
}

func (c *ContainerConnection) KeepAlive() error {
	return nil
}

func (c *ContainerConnection) SetWinSize(w, h int) error {
	size := &remotecommand.TerminalSize{
		Width:  uint16(w),
		Height: uint16(h),
	}
	select {
	case c.winSizeChan <- size:
	case <-c.done:
		return nil
	}
	return nil
}

type ContainerOption func(*ContainerOptions)

type ContainerOptions struct {
	Host          string
	Token         string
	PodName       string
	Namespace     string
	ContainerName string
	IsSkipTls     bool
	win           *remotecommand.TerminalSize
}

func (o ContainerOptions) K8sCfg() *rest.Config {
	kubeConf := &rest.Config{
		Host:        o.Host,
		BearerToken: o.Token,
	}
	if o.IsSkipTls {
		kubeConf.Insecure = true
	}
	return kubeConf
}

func (o ContainerOptions) String() string {
	return fmt.Sprintf("(%s)-(%s)-(%s)", o.Namespace,
		o.PodName, o.ContainerName)
}

func ContainerHost(host string) ContainerOption {
	return func(opt *ContainerOptions) {
		opt.Host = host
	}
}
func ContainerToken(token string) ContainerOption {
	return func(opt *ContainerOptions) {
		opt.Token = token
	}
}

func ContainerPodName(name string) ContainerOption {
	return func(opt *ContainerOptions) {
		opt.PodName = name
	}
}

func ContainerNamespace(namespace string) ContainerOption {
	return func(opt *ContainerOptions) {
		opt.Namespace = namespace
	}
}

func ContainerName(container string) ContainerOption {
	return func(opt *ContainerOptions) {
		opt.ContainerName = container
	}
}

func ContainerSkipTls(isSkipTls bool) ContainerOption {
	return func(args *ContainerOptions) {
		args.IsSkipTls = isSkipTls
	}
}

func ContainerPtyWin(win Windows) ContainerOption {
	return func(args *ContainerOptions) {
		args.win = &remotecommand.TerminalSize{
			Width:  uint16(win.Width),
			Height: uint16(win.Height),
		}
	}
}

func FindAvailableShell(opt *ContainerOptions) (shell string, err error) {
	shells := []string{"bash", "sh", "powershell", "cmd"}
	for i := range shells {
		if err = HasShellInContainer(opt, shells[i]); err == nil {
			return shells[i], nil
		}
	}
	return "", ErrNotFoundShell
}

func HasShellInContainer(opt *ContainerOptions, shell string) error {
	container := opt.ContainerName
	podName := opt.PodName
	namespace := opt.Namespace
	kubeConf := opt.K8sCfg()
	client, err := kubernetes.NewForConfig(kubeConf)
	if err != nil {
		return err
	}
	command := []string{"which", shell}
	validateChecker := func(result string) error {
		if !strings.HasSuffix(result, shell) {
			return fmt.Errorf("%w: %s %s", ErrNotFoundCommand, result, shell)
		}
		return nil
	}
	switch shell {
	case "cmd":
		command = []string{"where", "cmd"}
		validateChecker = func(result string) error {
			if !strings.HasSuffix(result, "cmd.exe") {
				return fmt.Errorf("%w: %s cmd.exe", ErrNotFoundCommand, result)
			}
			return nil
		}
	case "powershell":
		command = []string{"Get-Command", "powershell"}
		validateChecker = func(result string) error {
			if !strings.Contains(result, "powershell.exe") {
				return fmt.Errorf("%w: %s powershell.exe", ErrNotFoundCommand, result)
			}
			return nil
		}
	}
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Container: container,
		Command:   command,
		Stdout:    true,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(kubeConf, "POST", req.URL())
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &buf,
		Tty:    false,
	})
	if err != nil {
		return err
	}
	result := strings.TrimSpace(buf.String())
	buf.Reset()
	return validateChecker(result)
}

type slaveStream struct {
	r           io.ReadCloser
	w           io.WriteCloser
	winSizeChan chan *remotecommand.TerminalSize
	done        chan struct{}
}

func (s *slaveStream) Read(p []byte) (int, error) {
	return s.r.Read(p)
}

func (s *slaveStream) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

func (s *slaveStream) Next() *remotecommand.TerminalSize {
	select {
	case size := <-s.winSizeChan:
		return size
	case <-s.done:
		return nil
	}
}

func execContainerShell(k8sClient *kubernetes.Clientset, k8sCfg *rest.Config, c *ContainerConnection) error {
	req := k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(c.opt.PodName).
		Namespace(c.opt.Namespace).
		SubResource("exec")
	req.VersionedParams(&v1.PodExecOptions{
		Container: c.opt.ContainerName,
		Command:   []string{c.shell},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(k8sCfg, "POST", req.URL())
	if err != nil {
		return err
	}
	streamOption := remotecommand.StreamOptions{
		Stdin:             c.slaver,
		Stdout:            c.slaver,
		Stderr:            c.slaver,
		TerminalSizeQueue: c.slaver,
		Tty:               true,
	}
	// 这个 stream 是阻塞的方法
	err = exec.Stream(streamOption)
	return err
}
