package srvconn

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
)

type SSHClientOption func(conf *SSHClientOptions)

type SSHClientOptions struct {
	Host         string
	Port         string
	Username     string
	Password     string
	PrivateKey   string
	Passphrase   string
	Timeout      int
	keyboardAuth gossh.KeyboardInteractiveChallenge
	PrivateAuth  gossh.Signer

	proxySSHClientOptions []SSHClientOptions
}

func (cfg *SSHClientOptions) AuthMethods() []gossh.AuthMethod {
	authMethods := make([]gossh.AuthMethod, 0, 3)

	if cfg.PrivateKey != "" {
		var (
			signer gossh.Signer
			err    error
		)
		if cfg.Passphrase != "" {
			// 先使用 passphrase 解析 PrivateKey
			if signer, err = gossh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey),
				[]byte(cfg.Passphrase)); err == nil {
				authMethods = append(authMethods, gossh.PublicKeys(signer))
			}
		}
		if err != nil || cfg.Passphrase == "" {
			// 1. 如果之前使用解析失败，则去掉 passphrase，则尝试直接解析 PrivateKey 防止错误的passphrase
			// 2. 如果没有 Passphrase 则直接解析 PrivateKey
			if signer, err = gossh.ParsePrivateKey([]byte(cfg.PrivateKey)); err == nil {
				authMethods = append(authMethods, gossh.PublicKeys(signer))
			}
		}
	}
	if cfg.PrivateAuth != nil {
		authMethods = append(authMethods, gossh.PublicKeys(cfg.PrivateAuth))
	}
	if cfg.Password != "" {
		authMethods = append(authMethods, gossh.Password(cfg.Password))
	}
	if cfg.keyboardAuth != nil {
		authMethods = append(authMethods, gossh.KeyboardInteractive(cfg.keyboardAuth))
	}
	if cfg.keyboardAuth == nil && cfg.Password != "" {
		cfg.keyboardAuth = func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
			if len(questions) == 0 {
				return []string{}, nil
			}
			return []string{cfg.Password}, nil
		}
		authMethods = append(authMethods, gossh.KeyboardInteractive(cfg.keyboardAuth))
	}

	return authMethods
}

func SSHClientUsername(username string) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.Username = username
	}
}

func SSHClientPassword(password string) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.Password = password
	}
}

func SSHClientPrivateKey(privateKey string) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.PrivateKey = privateKey
	}
}

func SSHClientPassphrase(passphrase string) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.Passphrase = passphrase
	}
}

func SSHClientHost(host string) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.Host = host
	}
}

func SSHClientPort(port int) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.Port = strconv.Itoa(port)
	}
}

func SSHClientTimeout(timeout int) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.Timeout = timeout
	}
}

func SSHClientPrivateAuth(privateAuth gossh.Signer) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.PrivateAuth = privateAuth
	}
}

func SSHClientProxyClient(proxyArgs ...SSHClientOptions) SSHClientOption {
	return func(args *SSHClientOptions) {
		args.proxySSHClientOptions = proxyArgs
	}
}

func SSHClientKeyboardAuth(keyboardAuth gossh.KeyboardInteractiveChallenge) SSHClientOption {
	return func(conf *SSHClientOptions) {
		conf.keyboardAuth = keyboardAuth
	}
}

func NewSSHClient(opts ...SSHClientOption) (*SSHClient, error) {
	cfg := &SSHClientOptions{
		Host: "127.0.0.1",
		Port: "22",
	}
	for _, setter := range opts {
		setter(cfg)
	}
	return NewSSHClientWithCfg(cfg)
}

var (
	ErrNoAvailable = errors.New("no available gateway")
	ErrGatewayDial = errors.New("gateway dial addr failed")
	ErrSSHClient   = errors.New("new ssh client failed")
)

func getAvailableProxyClient(cfgs ...SSHClientOptions) (*SSHClient, error) {
	for i := range cfgs {
		if proxyClient, err := NewSSHClientWithCfg(&cfgs[i]); err == nil {
			return proxyClient, nil
		}
	}
	return nil, ErrNoAvailable
}

func NewSSHClientWithCfg(cfg *SSHClientOptions) (*SSHClient, error) {
	gosshCfg := gossh.ClientConfig{
		User:            cfg.Username,
		Auth:            cfg.AuthMethods(),
		Timeout:         time.Duration(cfg.Timeout) * time.Second,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Config:          createSSHConfig(),
	}
	destAddr := net.JoinHostPort(cfg.Host, cfg.Port)
	if len(cfg.proxySSHClientOptions) > 0 {
		proxyClient, err := getAvailableProxyClient(cfg.proxySSHClientOptions...)
		if err != nil {
			logger.Errorf("Get gateway client err: %s", err)
			return nil, err
		}
		logger.Infof("Get gateway client(%s) success ", proxyClient)
		destConn, err := proxyClient.Dial("tcp", destAddr)
		if err != nil {
			_ = proxyClient.Close()
			return nil, fmt.Errorf("%w: %s", ErrGatewayDial, err)
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(destConn, destAddr, &gosshCfg)
		if err != nil {
			_ = proxyClient.Close()
			_ = destConn.Close()
			return nil, fmt.Errorf("%w: %s", ErrSSHClient, err)
		}
		gosshClient := gossh.NewClient(proxyConn, chans, reqs)
		return &SSHClient{Cfg: cfg, Client: gosshClient,
			traceSessionMap: make(map[*gossh.Session]time.Time),
			ProxyClient:     proxyClient}, nil
	}
	gosshClient, err := gossh.Dial("tcp", destAddr, &gosshCfg)
	if err != nil {
		return nil, err
	}
	return &SSHClient{Client: gosshClient, Cfg: cfg,
		traceSessionMap: make(map[*gossh.Session]time.Time)}, nil
}

type SSHClient struct {
	*gossh.Client
	Cfg         *SSHClientOptions
	ProxyClient *SSHClient

	sync.Mutex

	traceSessionMap map[*gossh.Session]time.Time

	refCount int32
	_selfRef int32
}

func (s *SSHClient) increaseSelfRef() {
	s._selfRef++
}

func (s *SSHClient) decreaseSelfRef() {
	s._selfRef--
}

func (s *SSHClient) selfRef() int32 {
	return s._selfRef
}
func (s *SSHClient) String() string {
	return fmt.Sprintf("%s@%s:%s", s.Cfg.Username,
		s.Cfg.Host, s.Cfg.Port)
}

func (s *SSHClient) Close() error {
	if s.ProxyClient != nil {
		_ = s.ProxyClient.Close()
		logger.Infof("SSHClient(%s) proxy (%s) close", s, s.ProxyClient)
	}
	err := s.Client.Close()
	logger.Infof("SSHClient(%s) close", s)
	return err
}

func (s *SSHClient) RefCount() int32 {
	return atomic.LoadInt32(&s.refCount)
}

func (s *SSHClient) AcquireSession() (*gossh.Session, error) {
	atomic.AddInt32(&s.refCount, 1)
	sess, err := s.Client.NewSession()
	if err != nil {
		atomic.AddInt32(&s.refCount, -1)
		return nil, err
	}
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	s.traceSessionMap[sess] = time.Now()
	logger.Infof("SSHClient(%s) session add one ", s)
	return sess, nil
}

func (s *SSHClient) ReleaseSession(sess *gossh.Session) {
	atomic.AddInt32(&s.refCount, -1)
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	delete(s.traceSessionMap, sess)
	logger.Infof("SSHClient(%s) release one session remain %d", s, len(s.traceSessionMap))
}

func createSSHConfig() gossh.Config {
	var cfg gossh.Config
	cfg.SetDefaults()
	cfg.Ciphers = supportedCiphers
	cfg.KeyExchanges = append(cfg.KeyExchanges, notRecommendKeyExchanges...)
	return cfg
}

var (
	supportedCiphers = []string{
		"aes128-ctr", "aes192-ctr", "aes256-ctr",
		"aes128-gcm@openssh.com",
		"chacha20-poly1305@openssh.com",
		"arcfour256", "arcfour128", "arcfour",
		"aes128-cbc",
	}

	notRecommendCiphers = []string{
		"arcfour256", "arcfour128", "arcfour",
		"aes128-cbc", "3des-cbc",
	}

	notRecommendKeyExchanges = []string{
		"diffie-hellman-group1-sha1", "diffie-hellman-group-exchange-sha1",
		"diffie-hellman-group-exchange-sha256",
	}
)
