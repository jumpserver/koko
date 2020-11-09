package srvconn

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

var sshManager = &SSHManager{data: make(map[string]*UserSSHClient)}

var (
	supportedCiphers = []string{
		"aes128-ctr", "aes192-ctr", "aes256-ctr",
		"aes128-gcm@openssh.com",
		"chacha20-poly1305@openssh.com",
		"arcfour256", "arcfour128", "arcfour",
		"aes128-cbc",
		"3des-cbc"}

	supportedKexAlgos = []string{
		"diffie-hellman-group1-sha1",
		"diffie-hellman-group14-sha1", "ecdh-sha2-nistp256", "ecdh-sha2-nistp521",
		"ecdh-sha2-nistp384", "curve25519-sha256@libssh.org"}

	supportedHostKeyAlgos = []string{
		"ssh-rsa-cert-v01@openssh.com", "ssh-dss-cert-v01@openssh.com", "ecdsa-sha2-nistp256-cert-v01@openssh.com",
		"ecdsa-sha2-nistp384-cert-v01@openssh.com", "ecdsa-sha2-nistp521-cert-v01@openssh.com",
		"ssh-ed25519-cert-v01@openssh.com",
		"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
		"ssh-rsa", "ssh-dss",
		"ssh-ed25519",
	}
)

type sshClient struct {
	client      *gossh.Client
	proxyConn   gossh.Conn
	proxyClient *gossh.Client
	username    string

	ref int
	key string
	mu  *sync.RWMutex

	closed chan struct{}

	config *SSHClientConfig
}

func (s *sshClient) RefCount() int {
	if s.isClosed() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ref
}

func (s *sshClient) NewSession() (*gossh.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	s.ref++
	return sess, nil
}

func (s *sshClient) NewSFTPClient() (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		_ = sess.Close()
		return nil, err
	}
	pw, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	pr, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	client, err := sftp.NewClientPipe(pr, pw)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	s.ref++
	return client, err
}

func (s *sshClient) Close() error {
	if s.isClosed() {
		return nil
	}
	s.mu.Lock()
	s.ref--
	var needClosed bool
	if s.ref <= 0 {
		needClosed = true
	}
	s.mu.Unlock()
	if needClosed {
		return s.close()
	}
	return nil
}

func (s *sshClient) close() error {
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	if s.key != "" {
		deleteClientFromCache(s.key, s)
	}
	s.ref = 0
	if s.proxyConn != nil {
		_ = s.proxyConn.Close()
	}
	if s.proxyClient != nil {
		_ = s.proxyClient.Close()
	}
	logger.Infof("Success to close SSH client(%s)", s.config)
	return s.client.Close()
}

func (s *sshClient) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *sshClient) String() string {
	return s.config.String()
}

type SSHClientConfig struct {
	Host           string        `json:"host"`
	Port           string        `json:"port"`
	User           string        `json:"user"`
	Password       string        `json:"password"`
	PrivateKey     string        `json:"private_key"`
	PrivateKeyPath string        `json:"private_key_path"`
	Timeout        time.Duration `json:"timeout"`
	Proxy          []*SSHClientConfig

	proxyConn gossh.Conn

	proxyClient *gossh.Client
}

func (sc *SSHClientConfig) Config() (config *gossh.ClientConfig, err error) {
	authMethods := make([]gossh.AuthMethod, 0)
	if sc.Password != "" {
		authMethods = append(authMethods, gossh.Password(sc.Password))
		authMethods = append(authMethods, gossh.KeyboardInteractive(sc.keyboardInteractivePassword(sc.Password)))
	}
	if sc.PrivateKeyPath != "" {
		if pubkey, err := GetPubKeyFromFile(sc.PrivateKeyPath); err != nil {
			err = fmt.Errorf("parse private key from file error: %s", err)
			return config, err
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(pubkey))
		}
	}
	if sc.PrivateKey != "" {
		if signer, err := gossh.ParsePrivateKeyWithPassphrase([]byte(sc.PrivateKey), []byte(sc.Password)); err != nil {
			err = fmt.Errorf("parse private key error: %s", err)
			logger.Error(err.Error())
		} else {
			authMethods = append(authMethods, gossh.PublicKeys(signer))
		}
	}
	config = &gossh.ClientConfig{
		User:              sc.User,
		Auth:              authMethods,
		HostKeyCallback:   gossh.InsecureIgnoreHostKey(),
		Config:            gossh.Config{Ciphers: supportedCiphers, KeyExchanges: supportedKexAlgos},
		Timeout:           sc.Timeout,
		HostKeyAlgorithms: supportedHostKeyAlgos,
	}
	return config, nil
}

func (sc *SSHClientConfig) keyboardInteractivePassword(password string) gossh.KeyboardInteractiveChallenge {
	return func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
		if len(questions) == 0 {
			return []string{}, nil
		}
		logger.Infof("Host %s use keyboard-Interactive auth method login", sc.Host)
		return []string{password}, nil
	}
}

func (sc *SSHClientConfig) DialProxy() (client *gossh.Client, err error) {
	for _, p := range sc.Proxy {
		client, err = p.Dial()
		if err == nil {
			logger.Debugf("Connect proxy host %s:%s success", p.Host, p.Port)
			return
		} else {
			logger.Errorf("Connect proxy host %s:%s error: %s", p.Host, p.Port, err)
		}
	}
	return
}

func (sc *SSHClientConfig) Dial() (client *gossh.Client, err error) {
	cfg, err := sc.Config()
	if err != nil {
		return
	}
	if len(sc.Proxy) > 0 {
		logger.Debugf("Dial host proxy first")
		proxyClient, err := sc.DialProxy()
		if err != nil {
			err = errors.New("connect proxy host error 1: " + err.Error())
			logger.Error("Connect proxy host error 1: ", err.Error())
			return client, err
		}
		proxySock, err := proxyClient.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port))
		if err != nil {
			err = fmt.Errorf("tcp connect host %s:%s error 2: %s", sc.Host, sc.Port, err.Error())
			logger.Errorf("Tcp connect host %s:%s error 2: %s", sc.Host, sc.Port, err.Error())
			return client, err
		}
		proxyConn, chans, reqs, err := gossh.NewClientConn(proxySock, net.JoinHostPort(sc.Host, sc.Port), cfg)
		if err != nil {
			err = fmt.Errorf("ssh connect host %s:%s error 3: %s", sc.Host, sc.Port, err.Error())
			logger.Errorf("SSH Connect host %s:%s error 3: %s", sc.Host, sc.Port, err.Error())
			return client, err
		}
		sc.proxyConn = proxyConn
		sc.proxyClient = proxyClient
		client = gossh.NewClient(proxyConn, chans, reqs)
	} else {
		logger.Debugf("Dial host %s:%s", sc.Host, sc.Port)
		client, err = gossh.Dial("tcp", net.JoinHostPort(sc.Host, sc.Port), cfg)
		if err != nil {
			return
		}
	}
	return client, nil
}

func (sc *SSHClientConfig) String() string {
	return fmt.Sprintf("%s@%s:%s", sc.User, sc.Host, sc.Port)
}

func MakeConfig(asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (conf *SSHClientConfig) {
	proxyConfigs := make([]*SSHClientConfig, 0)
	// 如果有网关则从网关中连接
	if asset.Domain != "" {
		gateways := service.GetAssetGateways(asset.ID)
		if len(gateways) > 0 {
			for _, gateway := range gateways {
				proxyConfigs = append(proxyConfigs, &SSHClientConfig{
					Host:       gateway.IP,
					Port:       strconv.Itoa(gateway.Port),
					User:       gateway.Username,
					Password:   gateway.Password,
					PrivateKey: gateway.PrivateKey,
					Timeout:    timeout,
				})
			}
		}
	}

	conf = &SSHClientConfig{
		Host:       asset.IP,
		Port:       strconv.Itoa(asset.ProtocolPort("ssh")),
		User:       systemUser.Username,
		Password:   systemUser.Password,
		PrivateKey: systemUser.PrivateKey,
		Timeout:    timeout,
		Proxy:      proxyConfigs,
	}
	return
}

func newClient(asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration) (client *sshClient, err error) {
	sshConfig := MakeConfig(asset, systemUser, timeout)
	conn, err := sshConfig.Dial()
	if err != nil {
		return nil, err
	}
	closed := make(chan struct{})
	client = &sshClient{client: conn, proxyConn: sshConfig.proxyConn,
		proxyClient: sshConfig.proxyClient,
		username:    systemUser.Username,
		mu:          new(sync.RWMutex),
		config:      sshConfig,
		closed:      closed}
	return client, nil
}

func NewClient(user *model.User, asset *model.Asset, systemUser *model.SystemUser, timeout time.Duration,
	useCache bool) (client *sshClient, err error) {

	client, err = newClient(asset, systemUser, timeout)
	if err == nil && useCache {
		key := MakeReuseSSHClientKey(user, asset, systemUser)
		setClientCache(key, client)
	}
	return
}

func searchSSHClientFromCache(prefixKey string) (client *sshClient, ok bool) {
	return sshManager.searchSSHClientFromCache(prefixKey)
}

func GetClientFromCache(key string) (client *sshClient, ok bool) {
	return sshManager.getClientFromCache(key)
}

func setClientCache(key string, client *sshClient) {
	client.key = key
	sshManager.AddClientCache(key, client)
}

func deleteClientFromCache(key string, c *sshClient) {
	sshManager.deleteClientFromCache(key, c)
}

func MakeReuseSSHClientKey(user *model.User, asset *model.Asset, systemUser *model.SystemUser) string {
	return fmt.Sprintf("%s_%s_%s_%s", user.ID, asset.ID, systemUser.ID, systemUser.Username)
}
